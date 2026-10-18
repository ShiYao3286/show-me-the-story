package agent

import (
	"encoding/json"
	"testing"

	"showmethestory/internal/config"
)

// Regression for the outline-batch failure. A model called generate_outline with
// stringified integers; the handler's json.Unmarshal failed, the error was
// ignored, BatchID stayed 0, and the request was rejected downstream with
// "batch_id 无效". The agent must repair the types before dispatch.
//
// This walks the same path executeTool uses but stops before the real handler,
// so it needs no project state.
func TestExecuteToolPathCoercesModelArguments(t *testing.T) {
	var captured json.RawMessage
	tools := []Tool{{
		Name:       "generate_outline",
		Parameters: `{"chapter_count":12,"outline_synopsis":"本批剧情梗概","mode":"append","batch_id":0,"confirm":false}`,
		Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
			captured = args
			return "ok", nil
		},
	}}

	ctx := &AgentContext{Config: &config.Config{Language: "zh"}}
	call := &ToolCall{
		Name: "generate_outline",
		// Exactly the shape a model produces when it treats every argument as a string.
		Arguments: json.RawMessage(`{"chapter_count":"12","outline_synopsis":"密室","mode":"append","batch_id":"3","confirm":"true"}`),
	}

	result, _, _ := executeTool(call, tools, ctx)
	if result != "ok" {
		t.Fatalf("tool did not run: %s", result)
	}

	var got struct {
		ChapterCount int    `json:"chapter_count"`
		Outline      string `json:"outline_synopsis"`
		Mode         string `json:"mode"`
		BatchID      int    `json:"batch_id"`
		Confirm      bool   `json:"confirm"`
	}
	if err := json.Unmarshal(captured, &got); err != nil {
		t.Fatalf("handler would still fail to decode: %v (args=%s)", err, captured)
	}
	if got.ChapterCount != 12 {
		t.Errorf("chapter_count = %d, want 12", got.ChapterCount)
	}
	if got.BatchID != 3 {
		t.Errorf("batch_id = %d, want 3", got.BatchID)
	}
	if !got.Confirm {
		t.Error("confirm = false, want true")
	}
	if got.Outline != "密室" || got.Mode != "append" {
		t.Errorf("string args corrupted: %+v", got)
	}
}

// coerceToolArguments must agree with the schema the model was shown, otherwise
// the model is asked for one type and repaired against another.
func TestCoercionMatchesDeclaredSchema(t *testing.T) {
	for _, tool := range getBuiltinTools() {
		declared := toolParamTypes(tool.Parameters)
		if len(declared) == 0 {
			continue
		}
		var params map[string]json.RawMessage
		if err := json.Unmarshal([]byte(tool.Parameters), &params); err != nil {
			continue
		}
		schemaTypes := schemaPropertyTypes(t, toolParameterSchema(tool.Parameters))
		for name := range params {
			if declared[name] != schemaTypes[name] {
				t.Errorf("tool %q param %q: coercion type %q != schema type %q",
					tool.Name, name, declared[name], schemaTypes[name])
			}
		}
	}
}

func schemaPropertyTypes(t *testing.T, raw json.RawMessage) map[string]string {
	t.Helper()
	var schema struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	out := make(map[string]string, len(schema.Properties))
	for name, prop := range schema.Properties {
		out[name] = prop.Type
	}
	return out
}
