package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"showmethestory/internal/config"
)

// ModelInfo is one entry returned by GET /models.
//
// Besides the OpenAI minimum (id), providers commonly expose a display name, an
// owner/vendor used for grouping, the accepted input modalities and the
// reasoning-effort levels. All optional fields are filled when present so the
// UI can group and annotate long lists instead of showing a flat dump.
type ModelInfo struct {
	ID            string   `json:"id"`
	OwnedBy       string   `json:"owned_by,omitempty"`
	Name          string   `json:"name,omitempty"`
	Description   string   `json:"description,omitempty"`
	ContextWindow int      `json:"context_window,omitempty"`
	Input         []string `json:"input,omitempty"`
	// Reasoning is the list of effort ids the model accepts (e.g. off/low/high),
	// flattened for the UI. Empty when the model has no reasoning control.
	Reasoning []string `json:"reasoning,omitempty"`
	// ReasoningLabels maps effort id -> provider-supplied display name (e.g.
	// "high" -> "开启"). Empty when the provider sends no names.
	ReasoningLabels map[string]string `json:"reasoning_labels,omitempty"`
	// Group is the raw vendor key the UI groups by. It prefers owned_by and falls
	// back to the "<vendor>/" prefix of the id, which is the convention used by
	// aggregating gateways.
	Group string `json:"group,omitempty"`
	// GroupLabel is the human-facing group name: the Chinese vendor name when we
	// know it, otherwise the raw group key.
	GroupLabel string `json:"group_label,omitempty"`
}

// ListModels queries the provider's /models endpoint.
//
// Providers disagree on the shape, so we accept the common OpenAI form
// ({"data":[{"id":...}]}) and a bare array, and we tolerate extra fields.
// A provider that does not implement /models returns an error the UI can show;
// the user can still type a model name by hand.
func ListModels(ctx context.Context, apiCfg *config.APIConfig) ([]ModelInfo, error) {
	if apiCfg == nil {
		return nil, fmt.Errorf("API 配置为空")
	}
	base := strings.TrimSpace(apiCfg.BaseURL)
	if base == "" {
		return nil, fmt.Errorf("API Base URL 未配置")
	}
	modelsURL := resolveAPIBase(base, apiCfg.URLStrict) + "/models"

	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", modelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("构造模型列表请求失败: %w", err)
	}
	if apiCfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiCfg.APIKey)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求模型列表失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("读取模型列表失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("模型列表返回 %d: %s", resp.StatusCode, truncateForError(body))
	}

	models, err := parseModelList(body)
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("模型列表为空")
	}
	return models, nil
}

// rawModel mirrors the union of the fields OpenAI-compatible providers return.
type rawModel struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Model         string          `json:"model"`
	OwnedBy       string          `json:"owned_by"`
	Description   string          `json:"description"`
	ContextWindow int             `json:"context_window"`
	ContextLength int             `json:"context_length"`
	MaxContextLen int             `json:"max_context_length"`
	Input         []string        `json:"input"`
	Reasoning     json.RawMessage `json:"reasoning"`
}

// toModelInfo normalizes one raw entry into ModelInfo.
func (r rawModel) toModelInfo() (ModelInfo, bool) {
	// Some gateways use "model" instead of "id"; prefer whichever names the id.
	id := firstNonEmpty(r.ID, r.Model, r.Name)
	if id == "" {
		return ModelInfo{}, false
	}
	win := r.ContextWindow
	if win <= 0 {
		win = r.ContextLength
	}
	if win <= 0 {
		win = r.MaxContextLen
	}
	name := strings.TrimSpace(r.Name)
	if name == id {
		// Do not duplicate the id as a display name; the UI falls back to the id.
		name = ""
	}
	// Gateways often return the bare model name while the id carries a vendor
	// prefix ("codearts/glm-5.3-flash" vs "glm-5.3-flash"). Treat that as
	// redundant too, otherwise the UI shows the same string twice.
	if name != "" && strings.EqualFold(name, lastPathSegment(id)) {
		name = ""
	}
	group := modelGroup(id, r.OwnedBy)
	efforts, labels := parseReasoning(r.Reasoning)
	return ModelInfo{
		ID:              id,
		OwnedBy:         strings.TrimSpace(r.OwnedBy),
		Name:            name,
		Description:     strings.TrimSpace(r.Description),
		ContextWindow:   win,
		Input:           cleanStrings(r.Input),
		Reasoning:       efforts,
		ReasoningLabels: labels,
		Group:           group,
		GroupLabel:      VendorLabel(group),
	}, true
}

// modelGroup picks the label the UI groups by: the declared owner when present,
// otherwise the "<vendor>/" prefix of the id (common on aggregating gateways).
func modelGroup(id, ownedBy string) string {
	if s := strings.TrimSpace(ownedBy); s != "" {
		return s
	}
	if i := strings.Index(id, "/"); i > 0 {
		return id[:i]
	}
	return ""
}

// parseReasoningEfforts flattens the different shapes providers use for effort
// metadata into a list of effort ids. Anything unexpected yields nil.
// parseReasoning flattens the differing provider shapes for effort metadata into
// a list of effort ids plus an id -> display-name map (providers may send Chinese
// names such as "开启"/"关闭"; the UI shows the name when present).
func parseReasoning(raw json.RawMessage) ([]string, map[string]string) {
	if len(raw) == 0 {
		return nil, nil
	}
	var obj struct {
		Efforts []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"efforts"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && len(obj.Efforts) > 0 {
		ids := make([]string, 0, len(obj.Efforts))
		labels := map[string]string{}
		for _, e := range obj.Efforts {
			id := strings.TrimSpace(e.ID)
			if id == "" {
				continue
			}
			ids = append(ids, id)
			if name := strings.TrimSpace(e.Name); name != "" && name != id {
				labels[id] = name
			}
		}
		if len(ids) > 0 {
			if len(labels) == 0 {
				labels = nil
			}
			return ids, labels
		}
	}
	// Some providers send a bare array of effort names.
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return cleanStrings(arr), nil
	}
	return nil, nil
}

func cleanStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseModelList(body []byte) ([]ModelInfo, error) {
	// OpenAI / most compatible providers.
	var wrapped struct {
		Data []rawModel `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil && wrapped.Data != nil {
		out := make([]ModelInfo, 0, len(wrapped.Data))
		for _, m := range wrapped.Data {
			if info, ok := m.toModelInfo(); ok {
				out = append(out, info)
			}
		}
		return out, nil
	}

	// Bare array of ids or objects.
	var arr []json.RawMessage
	if err := json.Unmarshal(body, &arr); err == nil {
		out := make([]ModelInfo, 0, len(arr))
		for _, raw := range arr {
			var s string
			if err := json.Unmarshal(raw, &s); err == nil {
				if s = strings.TrimSpace(s); s != "" {
					out = append(out, ModelInfo{ID: s, Group: modelGroup(s, "")})
				}
				continue
			}
			var m rawModel
			if err := json.Unmarshal(raw, &m); err == nil {
				if info, ok := m.toModelInfo(); ok {
					out = append(out, info)
				}
			}
		}
		return out, nil
	}

	return nil, fmt.Errorf("无法解析模型列表响应")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func truncateForError(body []byte) string {
	s := strings.TrimSpace(string(body))
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	if s == "" {
		return "(空响应)"
	}
	return s
}
// lastPathSegment returns the part of an id after the final "/".
func lastPathSegment(id string) string {
	if i := strings.LastIndex(id, "/"); i >= 0 && i+1 < len(id) {
		return id[i+1:]
	}
	return id
}