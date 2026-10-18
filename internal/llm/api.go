package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"showmethestory/internal/config"
	"showmethestory/internal/sse"
	"strings"
	"time"
)

type ChatRequest struct {
	Model         string         `json:"model"`
	Messages      []Message      `json:"messages"`
	Stream        bool           `json:"stream,omitempty"`
	StreamOptions *streamOptions `json:"stream_options,omitempty"`
	MaxTokens     int            `json:"max_tokens,omitempty"`
	// Tools/ParallelToolCalls are omitted entirely when no tools are supplied,
	// so plain completions keep the exact request shape they had before.
	Tools             []Tool   `json:"tools,omitempty"`
	ToolChoice        string   `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool    `json:"parallel_tool_calls,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type tokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Tool is the OpenAI function-calling declaration of one callable function.
type Tool struct {
	Type     string      `json:"type"` // always "function"
	Function FunctionDef `json:"function"`
}

// FunctionDef describes a callable function.
type FunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ToolCall is one function invocation returned by the model.
type ToolCall struct {
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"` // "function"
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction carries the name and the raw JSON arguments.
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Message is one conversation turn. ToolCalls is set on assistant messages that
// requested tools; ToolCallID is set on "tool" messages that carry a result.
// Content is omitted for assistant messages that only call tools, because some
// providers reject null/empty content alongside tool_calls.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type ChatResponse struct {
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Usage *tokenUsage `json:"usage,omitempty"`
}

// CompletionResult is the normalized result of a chat completion call.
type CompletionResult struct {
	Content      string
	FinishReason string // e.g. "stop", "length", "tool_calls"
	// ToolCalls holds native function calls when the provider returned them.
	// Empty when the model answered with text only.
	ToolCalls []ToolCall
}

// A server-side output limit will not recover by retrying the same request.
type outputLimitError struct{ maxTokens int }

type contextBudgetError struct{ prompt, budget int }

func (e *contextBudgetError) Error() string {
	return fmt.Sprintf("estimated prompt size %d tokens exceeds context input budget %d; reduce project context or increase the configured context window", e.prompt, e.budget)
}

func (e *outputLimitError) Error() string {
	return fmt.Sprintf("API output truncated (finish_reason=length, requested max_tokens=%d); check provider output/reasoning limits or reduce the requested batch size", e.maxTokens)
}

func completionText(result CompletionResult, err error, maxTokens int) (string, error) {
	if err == nil && result.FinishReason == "length" {
		err = &outputLimitError{maxTokens: maxTokens}
	}
	return result.Content, err
}

func hasAPIVersionSegment(u string) bool {
	for _, seg := range strings.Split(u, "/") {
		if len(seg) >= 2 && seg[0] == 'v' && seg[1] >= '0' && seg[1] <= '9' {
			return true
		}
	}
	return false
}

// resolveChatCompletionsURL builds the POST endpoint from base_url and url_strict.
// Must stay in sync with frontend/src/lib/apiUrl.js.
func resolveChatCompletionsURL(base string, strict bool) string {
	base = strings.TrimSpace(base)
	base = strings.TrimSuffix(base, "/")
	if base == "" {
		return ""
	}
	if strings.HasSuffix(base, "/chat/completions") {
		return base
	}
	if strict {
		return base + "/chat/completions"
	}
	if hasAPIVersionSegment(base) {
		return base + "/chat/completions"
	}
	return base + "/v1/chat/completions"
}

func resolveAPIBase(base string, strict bool) string {
	u := resolveChatCompletionsURL(base, strict)
	return strings.TrimSuffix(u, "/chat/completions")
}

func normalizeURL(apiCfg *config.APIConfig) string {
	if apiCfg == nil {
		return ""
	}
	return resolveChatCompletionsURL(apiCfg.BaseURL, apiCfg.URLStrict)
}

// EnsureContextBudget fills an unset budget and clamps configured values to a
// smaller model-reported window. A deliberate smaller user limit is retained.
func EnsureContextBudget(apiCfg *config.APIConfig) {
	if apiCfg == nil {
		return
	}
	if window := FetchModelContextWindow(apiCfg); window > 0 {
		if apiCfg.ContextBudgetTokens <= 0 || apiCfg.ContextBudgetTokens > window {
			apiCfg.ContextBudgetTokens = window
		}
	} else if apiCfg.ContextBudgetTokens <= 0 {
		apiCfg.ContextBudgetTokens = config.DefaultContextBudgetTokens
	}
}

// FetchModelContextWindow 从 API 的 /models 端点获取指定模型的上下文窗口大小。
// 成功返回 context_window > 0，失败返回 0（调用方应使用默认值）。
func FetchModelContextWindow(apiCfg *config.APIConfig) int {
	if apiCfg == nil || strings.TrimSpace(apiCfg.BaseURL) == "" || strings.TrimSpace(apiCfg.Model) == "" {
		return 0
	}
	modelsURL := resolveAPIBase(apiCfg.BaseURL, apiCfg.URLStrict) + "/models/" + apiCfg.Model

	req, err := http.NewRequest("GET", modelsURL, nil)
	if err != nil {
		return 0
	}
	if apiCfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiCfg.APIKey)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0
	}

	var result struct {
		ContextWindow int `json:"context_window"`
	}
	if err := json.Unmarshal(body, &result); err != nil || result.ContextWindow <= 0 {
		return 0
	}
	return result.ContextWindow
}

func ValidateConfig(apiCfg *config.APIConfig) error {
	if strings.TrimSpace(apiCfg.BaseURL) == "" {
		return fmt.Errorf("API Base URL 未配置")
	}
	if strings.TrimSpace(apiCfg.Model) == "" {
		return fmt.Errorf("Model 未配置")
	}
	return nil
}

func IsFatalAPIError(err error) bool {
	if err == nil {
		return false
	}
	var limit *outputLimitError
	if errors.As(err, &limit) {
		return true
	}
	var budget *contextBudgetError
	if errors.As(err, &budget) {
		return true
	}
	msg := err.Error()
	// 注意：不要把所有 "dial tcp" 都当作致命错误——
	// "dial tcp ... i/o timeout" 等临时网络故障应当重试。
	if strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") {
		return true
	}
	if strings.Contains(msg, "状态码: 401") ||
		strings.Contains(msg, "状态码: 403") ||
		strings.Contains(msg, "状态码: 404") {
		return true
	}
	if strings.Contains(msg, "context canceled") {
		return true
	}
	return false
}

// PromptInputBudget reserves output capacity and a small provider/tokenizer
// margin. Estimates deliberately use the project's conservative rune ratio.
func PromptInputBudget(apiCfg *config.APIConfig) int {
	window := config.DefaultContextBudgetTokens
	output := config.DefaultMaxTokens
	if apiCfg != nil {
		if apiCfg.ContextBudgetTokens > 0 {
			window = apiCfg.ContextBudgetTokens
		}
		if apiCfg.MaxTokens > 0 {
			output = apiCfg.MaxTokens
		}
	}
	margin := max(4096, window/20)
	return max(0, window-output-margin)
}

func validateContextBudget(apiCfg *config.APIConfig, messages []Message) error {
	prompt := EstimateTokensFromRunes(countMessageRunes(messages))
	budget := PromptInputBudget(apiCfg)
	if prompt > budget {
		return &contextBudgetError{prompt: prompt, budget: budget}
	}
	return nil
}

func CallAPI(ctx context.Context, apiCfg *config.APIConfig, system, user string) (string, error) {
	return CallAPIMessages(ctx, apiCfg, []Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	})
}

// CallAPIMessages 以完整的多轮消息数组调用 API。
// 内部优先走流式并缓冲全文，使 token 计数在等待期间也能更新；流式不可用时回退同步请求。
func CallAPIMessages(ctx context.Context, apiCfg *config.APIConfig, messages []Message) (string, error) {
	messages = applyPromptAddon(ctx, messages)
	result, err := CallAPIStreamMessages(ctx, apiCfg, messages, nil)
	if err == nil && result.Content != "" {
		return completionText(result, nil, apiCfg.MaxTokens)
	}
	if ctx.Err() != nil {
		if result.Content != "" {
			return result.Content, ctx.Err()
		}
		return "", ctx.Err()
	}
	if result.Content != "" {
		return result.Content, err
	}
	if err != nil && IsFatalAPIError(err) {
		return "", err
	}
	// Fall back only before any stream content has been received.
	syncResult, syncErr := CallAPIMessagesSync(ctx, apiCfg, messages)
	return completionText(syncResult, syncErr, apiCfg.MaxTokens)
}

// CallAPIMessagesSync 同步 HTTP 调用（仅作流式失败时的回退）。
func CallAPIMessagesSync(ctx context.Context, apiCfg *config.APIConfig, messages []Message) (CompletionResult, error) {
	if err := validateContextBudget(apiCfg, messages); err != nil {
		return CompletionResult{}, err
	}
	fullURL := normalizeURL(apiCfg)
	tracker := TaskTokensFromContext(ctx)
	tracker.beginCall(messages)

	reqBody := ChatRequest{
		Model:     apiCfg.Model,
		Messages:  messages,
		MaxTokens: apiCfg.MaxTokens,
	}

	bts, err := json.Marshal(reqBody)
	if err != nil {
		return CompletionResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, bytes.NewBuffer(bts))
	if err != nil {
		return CompletionResult{}, err
	}

	req.Header.Set("Content-Type", "application/json")
	if apiCfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiCfg.APIKey)
	}

	timeout := time.Duration(apiCfg.HTTPTimeoutSeconds) * time.Second
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return CompletionResult{}, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return CompletionResult{}, err
	}

	if resp.StatusCode != http.StatusOK {
		return CompletionResult{}, fmt.Errorf("API 响应错误，状态码: %d, 返回内容: %s", resp.StatusCode, string(bodyBytes))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(bodyBytes, &chatResp); err != nil {
		return CompletionResult{}, err
	}

	if len(chatResp.Choices) > 0 {
		content := chatResp.Choices[0].Message.Content
		if tracker != nil {
			if chatResp.Usage != nil {
				tracker.finishCall(chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens, true, messages, content)
			} else {
				tracker.finishCall(0, 0, false, messages, content)
			}
		}
		return CompletionResult{Content: content, FinishReason: chatResp.Choices[0].FinishReason}, nil
	}
	return CompletionResult{}, fmt.Errorf("接口未响应有效 Choices 文本")
}

// CallAPIMessagesSyncWithTools is the non-streaming function-calling call. It is
// the fallback used when streaming fails before producing any content.
func CallAPIMessagesSyncWithTools(ctx context.Context, apiCfg *config.APIConfig, messages []Message, decls []Tool) (CompletionResult, error) {
	if err := validateContextBudget(apiCfg, messages); err != nil {
		return CompletionResult{}, err
	}
	fullURL := normalizeURL(apiCfg)
	tracker := TaskTokensFromContext(ctx)
	tracker.beginCall(messages)

	reqBody := ChatRequest{
		Model:      apiCfg.Model,
		Messages:   messages,
		MaxTokens:  apiCfg.MaxTokens,
		Tools:      decls,
		ToolChoice: "auto",
	}
	if len(decls) > 0 {
		no := false
		reqBody.ParallelToolCalls = &no
	}

	bts, err := json.Marshal(reqBody)
	if err != nil {
		return CompletionResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, bytes.NewBuffer(bts))
	if err != nil {
		return CompletionResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiCfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiCfg.APIKey)
	}

	timeout := time.Duration(apiCfg.HTTPTimeoutSeconds) * time.Second
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return CompletionResult{}, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return CompletionResult{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return CompletionResult{}, fmt.Errorf("API 响应错误，状态码: %d, 返回内容: %s", resp.StatusCode, string(bodyBytes))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(bodyBytes, &chatResp); err != nil {
		return CompletionResult{}, err
	}
	if len(chatResp.Choices) == 0 {
		return CompletionResult{}, fmt.Errorf("接口未响应有效 Choices 文本")
	}

	choice := chatResp.Choices[0]
	if tracker != nil {
		if chatResp.Usage != nil {
			tracker.finishCall(chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens, true, messages, choice.Message.Content)
		} else {
			tracker.finishCall(0, 0, false, messages, choice.Message.Content)
		}
	}
	calls := choice.Message.ToolCalls
	for i := range calls {
		if calls[i].Type == "" {
			calls[i].Type = "function"
		}
	}
	return CompletionResult{
		Content:      choice.Message.Content,
		FinishReason: choice.FinishReason,
		ToolCalls:    calls,
	}, nil
}

func CallAPIWithRetry(ctx context.Context, apiCfg *config.APIConfig, system, user string) string {
	retryCount := 0
	for {
		if ctx.Err() != nil {
			return ""
		}
		result, err := CallAPI(ctx, apiCfg, system, user)
		if err == nil && result != "" {
			return result
		}
		if IsFatalAPIError(err) {
			fmt.Printf(" ❌ [致命错误] %v，不再重试\n", err)
			return ""
		}

		retryCount++
		waitTime := RetryWaitTime(retryCount)
		fmt.Printf(" ⚠️ [错误] API调用失败: %v。第 %d 次重试，等待 %ds 后重试...\n", err, retryCount, waitTime)
		select {
		case <-time.After(time.Duration(waitTime) * time.Second):
		case <-ctx.Done():
			return ""
		}
	}
}

func CallAPIWithRetryLog(ctx context.Context, apiCfg *config.APIConfig, system, user string, logger *sse.LogBroadcaster) string {
	retryCount := 0
	for {
		if ctx.Err() != nil {
			return ""
		}
		result, err := CallAPI(ctx, apiCfg, system, user)
		if err == nil && result != "" {
			return result
		}
		if IsFatalAPIError(err) {
			logger.ErrorKey("log.fatal_no_retry", err)
			return ""
		}

		retryCount++
		waitTime := RetryWaitTime(retryCount)
		logger.WarnKey("log.api_retry", err, retryCount, waitTime)
		select {
		case <-time.After(time.Duration(waitTime) * time.Second):
		case <-ctx.Done():
			return ""
		}
	}
}

func RetryWaitTime(retry int) int {
	if retry > 6 {
		return 30
	}
	return retry * 5
}

type streamDelta struct {
	Choices []struct {
		Delta struct {
			Content   string          `json:"content"`
			ToolCalls []streamToolCue `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *tokenUsage `json:"usage,omitempty"`
}

// streamToolCue is one partial tool call in a streamed delta. Providers send the
// id/name once and then piecewise argument fragments, so accumulation is keyed
// by Index.
type streamToolCue struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// toolCallAccumulator reassembles streamed tool calls.
type toolCallAccumulator struct {
	order []int
	byIdx map[int]*ToolCall
}

func newToolCallAccumulator() *toolCallAccumulator {
	return &toolCallAccumulator{byIdx: map[int]*ToolCall{}}
}

func (a *toolCallAccumulator) add(cues []streamToolCue) {
	for _, c := range cues {
		tc, ok := a.byIdx[c.Index]
		if !ok {
			tc = &ToolCall{Type: "function"}
			a.byIdx[c.Index] = tc
			a.order = append(a.order, c.Index)
		}
		if c.ID != "" {
			tc.ID = c.ID
		}
		if c.Type != "" {
			tc.Type = c.Type
		}
		if c.Function.Name != "" {
			tc.Function.Name = c.Function.Name
		}
		if c.Function.Arguments != "" {
			tc.Function.Arguments += c.Function.Arguments
		}
	}
}

// result returns the accumulated calls in the order the provider first sent
// them, dropping entries that never received a function name.
func (a *toolCallAccumulator) result() []ToolCall {
	if len(a.order) == 0 {
		return nil
	}
	out := make([]ToolCall, 0, len(a.order))
	for _, idx := range a.order {
		tc := a.byIdx[idx]
		if tc == nil || strings.TrimSpace(tc.Function.Name) == "" {
			continue
		}
		if tc.Type == "" {
			tc.Type = "function"
		}
		out = append(out, *tc)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// CallAPIStreamToolsOptions configures an agent-style streaming call.
type StreamToolsOptions struct {
	Tools []Tool
	// ToolChoice defaults to "auto" when empty.
	ToolChoice string
	// onChunk receives assistant text as it streams.
	OnChunk func(string)
}

// CallAPIStreamMessagesWithTools streams a completion that may return native
// tool calls. It is the function-calling counterpart of CallAPIStreamMessages;
// both share the same transport, budget checks and error handling.
func CallAPIStreamMessagesWithTools(ctx context.Context, apiCfg *config.APIConfig, messages []Message, opts StreamToolsOptions) (CompletionResult, error) {
	if err := validateContextBudget(apiCfg, messages); err != nil {
		return CompletionResult{}, err
	}
	fullURL := normalizeURL(apiCfg)
	tracker := TaskTokensFromContext(ctx)
	tracker.beginCall(messages)

	reqBody := ChatRequest{
		Model:         apiCfg.Model,
		Messages:      messages,
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
		MaxTokens:     apiCfg.MaxTokens,
		Tools:         opts.Tools,
	}
	if len(opts.Tools) > 0 {
		reqBody.ToolChoice = opts.ToolChoice
		if reqBody.ToolChoice == "" {
			reqBody.ToolChoice = "auto"
		}
		// One call at a time keeps the loop deterministic and avoids partial
		// multi-call replies on models that handle parallel calls poorly.
		no := false
		reqBody.ParallelToolCalls = &no
	}

	bts, err := json.Marshal(reqBody)
	if err != nil {
		return CompletionResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, bytes.NewBuffer(bts))
	if err != nil {
		return CompletionResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiCfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiCfg.APIKey)
	}

	timeout := time.Duration(apiCfg.HTTPTimeoutSeconds) * time.Second
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return CompletionResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return CompletionResult{}, fmt.Errorf("API 响应错误，状态码: %d, 返回内容: %s", resp.StatusCode, string(bodyBytes))
	}

	var fullContent strings.Builder
	calls := newToolCallAccumulator()
	reader := bufio.NewReader(resp.Body)
	var streamUsage *tokenUsage
	var finishReason string
	var readErr error
	done := false

	for readErr == nil {
		if ctx.Err() != nil {
			return CompletionResult{Content: fullContent.String(), FinishReason: finishReason}, ctx.Err()
		}
		var line string
		line, readErr = reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			done = true
			break
		}

		var delta streamDelta
		if err := json.Unmarshal([]byte(data), &delta); err != nil {
			readErr = fmt.Errorf("invalid SSE JSON: %w", err)
			break
		}
		if delta.Usage != nil {
			streamUsage = delta.Usage
		}
		if len(delta.Choices) > 0 {
			ch := delta.Choices[0]
			if ch.FinishReason != "" {
				finishReason = ch.FinishReason
			}
			if ch.Delta.Content != "" {
				fullContent.WriteString(ch.Delta.Content)
				if tracker != nil {
					tracker.updateStreamContent(fullContent.String())
				}
				if opts.OnChunk != nil {
					opts.OnChunk(ch.Delta.Content)
				}
			}
			if len(ch.Delta.ToolCalls) > 0 {
				calls.add(ch.Delta.ToolCalls)
			}
		}
	}

	result := fullContent.String()
	if tracker != nil {
		if streamUsage != nil {
			tracker.finishCall(streamUsage.PromptTokens, streamUsage.CompletionTokens, true, messages, result)
		} else {
			tracker.finishCall(0, 0, false, messages, result)
		}
	}
	completion := CompletionResult{
		Content:      result,
		FinishReason: finishReason,
		ToolCalls:    calls.result(),
	}
	if ctx.Err() != nil {
		return completion, ctx.Err()
	}
	if readErr != nil && readErr != io.EOF {
		return completion, fmt.Errorf("stream interrupted after %d bytes: %w", len(result), readErr)
	}
	if !done && finishReason == "" {
		return completion, fmt.Errorf("stream ended without finish_reason or [DONE] after %d bytes: %w", len(result), io.ErrUnexpectedEOF)
	}
	// A tool-call-only reply legitimately has no text content.
	if result == "" && len(completion.ToolCalls) == 0 {
		return completion, fmt.Errorf("流式响应为空")
	}
	return completion, nil
}

func CallAPIStream(ctx context.Context, apiCfg *config.APIConfig, system, user string, onChunk func(string)) (string, error) {
	result, err := CallAPIStreamMessages(ctx, apiCfg, []Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}, onChunk)
	return completionText(result, err, apiCfg.MaxTokens)
}

// CallAPIStreamMessages 以完整的多轮消息数组调用 API（流式）。
func CallAPIStreamMessages(ctx context.Context, apiCfg *config.APIConfig, messages []Message, onChunk func(string)) (CompletionResult, error) {
	if err := validateContextBudget(apiCfg, messages); err != nil {
		return CompletionResult{}, err
	}
	fullURL := normalizeURL(apiCfg)
	tracker := TaskTokensFromContext(ctx)
	tracker.beginCall(messages)

	reqBody := ChatRequest{
		Model:         apiCfg.Model,
		Messages:      messages,
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
		MaxTokens:     apiCfg.MaxTokens,
	}

	bts, err := json.Marshal(reqBody)
	if err != nil {
		return CompletionResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, bytes.NewBuffer(bts))
	if err != nil {
		return CompletionResult{}, err
	}

	req.Header.Set("Content-Type", "application/json")
	if apiCfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiCfg.APIKey)
	}

	timeout := time.Duration(apiCfg.HTTPTimeoutSeconds) * time.Second
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return CompletionResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return CompletionResult{}, fmt.Errorf("API 响应错误，状态码: %d, 返回内容: %s", resp.StatusCode, string(bodyBytes))
	}

	var fullContent strings.Builder
	reader := bufio.NewReader(resp.Body)
	var streamUsage *tokenUsage
	var finishReason string
	var readErr error
	done := false

	for readErr == nil {
		if ctx.Err() != nil {
			return CompletionResult{Content: fullContent.String(), FinishReason: finishReason}, ctx.Err()
		}
		var line string
		line, readErr = reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			done = true
			break
		}

		var delta streamDelta
		if err := json.Unmarshal([]byte(data), &delta); err != nil {
			readErr = fmt.Errorf("invalid SSE JSON: %w", err)
			break
		}
		if delta.Usage != nil {
			streamUsage = delta.Usage
		}
		if len(delta.Choices) > 0 {
			if delta.Choices[0].FinishReason != "" {
				finishReason = delta.Choices[0].FinishReason
			}
			if delta.Choices[0].Delta.Content != "" {
				chunk := delta.Choices[0].Delta.Content
				fullContent.WriteString(chunk)
				if tracker != nil {
					tracker.updateStreamContent(fullContent.String())
				}
				if onChunk != nil {
					onChunk(chunk)
				}
			}
		}
	}

	result := fullContent.String()
	if tracker != nil {
		if streamUsage != nil {
			tracker.finishCall(streamUsage.PromptTokens, streamUsage.CompletionTokens, true, messages, result)
		} else {
			tracker.finishCall(0, 0, false, messages, result)
		}
	}
	completion := CompletionResult{Content: result, FinishReason: finishReason}
	if ctx.Err() != nil {
		return completion, ctx.Err()
	}
	if readErr != nil && readErr != io.EOF {
		return completion, fmt.Errorf("stream interrupted after %d bytes: %w", len(result), readErr)
	}
	if !done && finishReason == "" {
		return completion, fmt.Errorf("stream ended without finish_reason or [DONE] after %d bytes: %w", len(result), io.ErrUnexpectedEOF)
	}
	if result == "" {
		return completion, fmt.Errorf("流式响应为空")
	}
	return completion, nil
}
