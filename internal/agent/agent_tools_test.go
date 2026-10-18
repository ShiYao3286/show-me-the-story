package agent

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"showmethestory/internal/config"
	"showmethestory/internal/story"
)

// newPromptTestCtx builds the minimal AgentContext the prompt builders need.
func newPromptTestCtx(lang string) *AgentContext {
	return &AgentContext{
		Config: &config.Config{Language: lang, Story: config.StoryConfig{TargetWordsPerChapter: 3000}},
		State:  &story.Progress{},
	}
}
// Every builtin tool must produce a valid JSON Schema so providers accept the
// declaration; a malformed schema makes the whole request fail.
func TestBuildToolDeclarationsProducesValidSchemas(t *testing.T) {
	decls := buildToolDeclarations(getBuiltinTools())
	if len(decls) == 0 {
		t.Fatal("no tool declarations produced")
	}
	for _, d := range decls {
		if d.Type != "function" {
			t.Errorf("tool %q has type %q, want function", d.Function.Name, d.Type)
		}
		if d.Function.Name == "" {
			t.Error("tool with empty name")
		}
		if len(d.Function.Parameters) == 0 {
			t.Errorf("tool %q has no parameters schema", d.Function.Name)
			continue
		}
		var schema struct {
			Type       string                 `json:"type"`
			Properties map[string]interface{} `json:"properties"`
		}
		if err := json.Unmarshal(d.Function.Parameters, &schema); err != nil {
			t.Errorf("tool %q parameters are not valid JSON: %v", d.Function.Name, err)
			continue
		}
		if schema.Type != "object" {
			t.Errorf("tool %q schema type = %q, want object", d.Function.Name, schema.Type)
		}
	}
}

func TestToolParameterSchemaEmptyShorthand(t *testing.T) {
	for _, in := range []string{"", "{}", "  ", "null"} {
		raw := toolParameterSchema(in)
		var schema struct {
			Type       string                 `json:"type"`
			Properties map[string]interface{} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("toolParameterSchema(%q) invalid JSON: %v", in, err)
		}
		if schema.Type != "object" {
			t.Errorf("toolParameterSchema(%q) type = %q", in, schema.Type)
		}
		if len(schema.Properties) != 0 {
			t.Errorf("toolParameterSchema(%q) should have no properties, got %v", in, schema.Properties)
		}
	}
}

func TestToolParameterSchemaInfersTypes(t *testing.T) {
	cases := []struct {
		shorthand string
		param     string
		wantType  string
	}{
		{`{"num": "章节号"}`, "num", "integer"},
		{`{"count": "数量"}`, "count", "integer"},
		{`{"chapter_ids": "章节 ID 列表"}`, "chapter_ids", "array"},
		{`{"confirm": "是否确认，true/false"}`, "confirm", "boolean"},
		{`{"name": "角色名称"}`, "name", "string"},
		{`{"query": "搜索关键词"}`, "query", "string"},
		{`{"id": "角色ID或名称"}`, "id", "string"},
	}
	for _, tc := range cases {
		raw := toolParameterSchema(tc.shorthand)
		var schema struct {
			Properties map[string]struct {
				Type string `json:"type"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("%s: %v", tc.shorthand, err)
		}
		prop, ok := schema.Properties[tc.param]
		if !ok {
			t.Fatalf("%s: param %q missing", tc.shorthand, tc.param)
		}
		if prop.Type != tc.wantType {
			t.Errorf("%s: %q type = %q, want %q", tc.shorthand, tc.param, prop.Type, tc.wantType)
		}
	}
}

// An array property needs an items schema or strict providers reject it.
func TestToolParameterSchemaArrayHasItems(t *testing.T) {
	raw := toolParameterSchema(`{"chapter_ids": "章节 ID 列表"}`)
	var schema struct {
		Properties map[string]struct {
			Type  string                 `json:"type"`
			Items map[string]interface{} `json:"items"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	p := schema.Properties["chapter_ids"]
	if p.Type != "array" {
		t.Fatalf("type = %q, want array", p.Type)
	}
	if p.Items == nil || p.Items["type"] != "string" {
		t.Fatalf("array without items schema: %+v", p.Items)
	}
}

func TestSanitizeToolName(t *testing.T) {
	cases := map[string]string{
		"read_chapter":        "read_chapter",
		"read-chapter":        "read-chapter",
		"weird name!":         "weird_name_",
		"":                    "tool",
		strings.Repeat("a", 80): strings.Repeat("a", 64),
	}
	for in, want := range cases {
		if got := sanitizeToolName(in); got != want {
			t.Errorf("sanitizeToolName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestToNativeToolCall(t *testing.T) {
	tc := &ToolCall{Name: "read_chapter", Arguments: json.RawMessage(`{"num":3}`)}
	native := toNativeToolCall(tc)
	if native.Type != "function" {
		t.Errorf("type = %q", native.Type)
	}
	if native.Function.Name != "read_chapter" {
		t.Errorf("name = %q", native.Function.Name)
	}
	if native.Function.Arguments != `{"num":3}` {
		t.Errorf("arguments = %q", native.Function.Arguments)
	}
	if native.ID == "" {
		t.Error("id must not be empty; providers correlate tool results by it")
	}

	// Empty arguments must become an empty object, not an empty string.
	empty := toNativeToolCall(&ToolCall{Name: "x", Arguments: nil})
	if empty.Function.Arguments != "{}" {
		t.Errorf("empty arguments = %q, want {}", empty.Function.Arguments)
	}
}

func TestAgentToolsUnsupported(t *testing.T) {
	supported := []error{
		nil,
		errors.New("API 响应错误，状态码: 401, 返回内容: invalid api key"),
		errors.New("stream interrupted"),
		errors.New("context deadline exceeded"),
	}
	for _, err := range supported {
		if agentToolsUnsupported(err) {
			t.Errorf("should not treat %v as unsupported", err)
		}
	}

	unsupported := []error{
		errors.New(`API 响应错误，状态码: 400, 返回内容: {"error":"tools is not supported by this model"}`),
		errors.New(`status 400: unknown field "tools"`),
		errors.New(`unsupported parameter: tools`),
		errors.New(`unrecognized request argument: tool_choice`),
		errors.New(`function calling is not supported`),
	}
	for _, err := range unsupported {
		if !agentToolsUnsupported(err) {
			t.Errorf("should treat %v as unsupported", err)
		}
	}
}

// In native mode the prompt must stop telling the model to emit <tool_call>
// tags; otherwise models obey the prompt instead of the functions API.
func TestSystemPromptOmitsTextProtocolInNativeMode(t *testing.T) {
	ctx := newPromptTestCtx("zh")
	toolDesc := buildToolDescriptions(getBuiltinTools()[:2])

	native := buildAgentSystemPrompt(ctx, toolDesc, true)
	if strings.Contains(native, "<tool_call>") {
		t.Error("native prompt must not contain <tool_call> instructions")
	}
	if !strings.Contains(native, "functions") && !strings.Contains(native, "函数调用") {
		t.Error("native prompt should mention function calling")
	}

	text := buildAgentSystemPrompt(ctx, toolDesc, false)
	if !strings.Contains(text, "<tool_call>") {
		t.Error("text-protocol prompt should describe the <tool_call> format")
	}
}

func TestSystemPromptENOmitsTextProtocolInNativeMode(t *testing.T) {
	ctx := newPromptTestCtx("en")
	toolDesc := buildToolDescriptions(getBuiltinTools()[:2])

	native := buildAgentSystemPrompt(ctx, toolDesc, true)
	if strings.Contains(native, "<tool_call>") {
		t.Error("native EN prompt must not contain <tool_call> instructions")
	}
	text := buildAgentSystemPrompt(ctx, toolDesc, false)
	if !strings.Contains(text, "<tool_call>") {
		t.Error("text-protocol EN prompt should describe the <tool_call> format")
	}
}

// Native history must use role "tool" with a matching tool_call_id, because
// providers reject an assistant tool_calls message without its reply.
func TestNativeHistoryUsesToolRole(t *testing.T) {
	ctx := newPromptTestCtx("zh")
	ctx.APICfg = &config.APIConfig{MaxTokens: 8192, ContextBudgetTokens: 900000}
	history := []AgentStep{
		{Role: "user", Content: "读第3章"},
		{Role: "assistant", Content: "", ToolCall: &ToolCall{Name: "read_chapter", Arguments: json.RawMessage(`{"num":3}`)}},
		{Role: "tool", ToolResult: "第三章内容……"},
	}

	msgs := buildAgentMessages(ctx, "sys", "读第3章", history, "[Tool result]", nil, true)

	var sawAssistantCall, sawToolReply bool
	var callID string
	for _, m := range msgs {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			sawAssistantCall = true
			callID = m.ToolCalls[0].ID
			if m.ToolCalls[0].Function.Name != "read_chapter" {
				t.Errorf("tool name = %q", m.ToolCalls[0].Function.Name)
			}
		}
		if m.Role == "tool" {
			sawToolReply = true
			if m.ToolCallID == "" {
				t.Error("tool reply missing tool_call_id")
			}
			if callID != "" && m.ToolCallID != callID {
				t.Errorf("tool_call_id %q does not match call %q", m.ToolCallID, callID)
			}
		}
	}
	if !sawAssistantCall {
		t.Error("native history lost the assistant tool_calls entry")
	}
	if !sawToolReply {
		t.Error("native history lost the tool reply")
	}
}

// The text protocol must keep its old shape when native mode is off.
func TestTextHistoryKeepsTagForm(t *testing.T) {
	ctx := newPromptTestCtx("zh")
	ctx.APICfg = &config.APIConfig{MaxTokens: 8192, ContextBudgetTokens: 900000}
	history := []AgentStep{
		{Role: "user", Content: "读第3章"},
		{Role: "assistant", Content: "", ToolCall: &ToolCall{Name: "read_chapter", Arguments: json.RawMessage(`{"num":3}`)}},
		{Role: "tool", ToolResult: "内容"},
	}
	msgs := buildAgentMessages(ctx, "sys", "读第3章", history, "[Tool result]", nil, false)

	var sawTag, sawToolRole bool
	for _, m := range msgs {
		if strings.Contains(m.Content, "<tool_call>") {
			sawTag = true
		}
		if m.Role == "tool" {
			sawToolRole = true
		}
	}
	if !sawTag {
		t.Error("text history should still embed <tool_call> tags")
	}
	if sawToolRole {
		t.Error("text history must not use the tool role")
	}
}
