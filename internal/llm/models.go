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
type ModelInfo struct {
	ID            string `json:"id"`
	OwnedBy       string `json:"owned_by,omitempty"`
	ContextWindow int    `json:"context_window,omitempty"`
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

func parseModelList(body []byte) ([]ModelInfo, error) {
	// OpenAI / most compatible providers.
	var wrapped struct {
		Data []struct {
			ID            string `json:"id"`
			OwnedBy       string `json:"owned_by"`
			ContextWindow int    `json:"context_window"`
			ContextLength int    `json:"context_length"`
			MaxContextLen int    `json:"max_context_length"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil && wrapped.Data != nil {
		out := make([]ModelInfo, 0, len(wrapped.Data))
		for _, m := range wrapped.Data {
			id := strings.TrimSpace(m.ID)
			if id == "" {
				continue
			}
			win := m.ContextWindow
			if win <= 0 {
				win = m.ContextLength
			}
			if win <= 0 {
				win = m.MaxContextLen
			}
			out = append(out, ModelInfo{ID: id, OwnedBy: m.OwnedBy, ContextWindow: win})
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
					out = append(out, ModelInfo{ID: s})
				}
				continue
			}
			var obj struct {
				ID            string `json:"id"`
				Name          string `json:"name"`
				Model         string `json:"model"`
				ContextWindow int    `json:"context_window"`
				ContextLength int    `json:"context_length"`
			}
			if err := json.Unmarshal(raw, &obj); err == nil {
				id := firstNonEmpty(obj.ID, obj.Name, obj.Model)
				if id == "" {
					continue
				}
				win := obj.ContextWindow
				if win <= 0 {
					win = obj.ContextLength
				}
				out = append(out, ModelInfo{ID: id, ContextWindow: win})
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
