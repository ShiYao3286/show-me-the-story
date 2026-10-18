package llm

import (
	"encoding/json"
	"testing"
)

// A gateway that returns vendor prefixes, display names, modalities and
// reasoning metadata must be parsed into groups the UI can use.
func TestParseModelListRichFields(t *testing.T) {
	body := []byte(`{
	  "object": "list",
	  "data": [
	    {
	      "id": "deepseek-account/deepseek-flash",
	      "object": "model",
	      "owned_by": "deepseek-account",
	      "name": "DeepSeek-V4.1-Flash",
	      "input": ["text", "image"],
	      "reasoning": {"efforts": [{"id":"off"},{"id":"high"}], "default":"high"}
	    },
	    {
	      "id": "codearts/glm-5.3-flash",
	      "owned_by": "codearts",
	      "name": "glm-5.3-flash",
	      "input": ["text"]
	    },
	    {
	      "id": "plain-model",
	      "name": "plain-model"
	    }
	  ]
	}`)

	models, err := parseModelList(body)
	if err != nil {
		t.Fatalf("parseModelList: %v", err)
	}
	if len(models) != 3 {
		t.Fatalf("expected 3 models, got %d", len(models))
	}

	first := models[0]
	if first.ID != "deepseek-account/deepseek-flash" {
		t.Fatalf("id not preserved: %q", first.ID)
	}
	if first.Group != "deepseek-account" {
		t.Fatalf("group should come from owned_by, got %q", first.Group)
	}
	if first.Name != "DeepSeek-V4.1-Flash" {
		t.Fatalf("display name not preserved: %q", first.Name)
	}
	if len(first.Input) != 2 || first.Input[1] != "image" {
		t.Fatalf("input modalities not parsed: %v", first.Input)
	}
	if len(first.Reasoning) != 2 || first.Reasoning[0] != "off" || first.Reasoning[1] != "high" {
		t.Fatalf("reasoning efforts not flattened: %v", first.Reasoning)
	}

	// A name equal to the id must be dropped so the UI does not show it twice.
	if models[1].Name != "" {
		t.Fatalf("name identical to id should be cleared, got %q", models[1].Name)
	}

	// No owner and no slash -> no group (UI puts these under "Other").
	if models[2].Group != "" {
		t.Fatalf("expected no group, got %q", models[2].Group)
	}
}

// Providers that omit owned_by but use "<vendor>/<model>" ids still group.
func TestModelGroupFallsBackToIDPrefix(t *testing.T) {
	body := []byte(`{"data":[
	  {"id":"mao/grok-4.5"},
	  {"id":"cs/glm-5.3"},
	  {"id":"not-grouped"}
	]}`)

	models, err := parseModelList(body)
	if err != nil {
		t.Fatalf("parseModelList: %v", err)
	}
	if models[0].Group != "mao" {
		t.Fatalf("expected group mao, got %q", models[0].Group)
	}
	if models[1].Group != "cs" {
		t.Fatalf("expected group cs, got %q", models[1].Group)
	}
	if models[2].Group != "" {
		t.Fatalf("expected no group for a bare id, got %q", models[2].Group)
	}
}

// owned_by wins over the id prefix when both are present.
func TestModelGroupPrefersOwnedBy(t *testing.T) {
	body := []byte(`{"data":[{"id":"vendor-a/model-x","owned_by":"vendor-b"}]}`)
	models, err := parseModelList(body)
	if err != nil {
		t.Fatalf("parseModelList: %v", err)
	}
	if models[0].Group != "vendor-b" {
		t.Fatalf("owned_by should win, got %q", models[0].Group)
	}
}

// Context window aliases must be accepted.
func TestParseModelListContextWindowAliases(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"context_window", `{"data":[{"id":"a","context_window":128000}]}`, 128000},
		{"context_length", `{"data":[{"id":"a","context_length":32768}]}`, 32768},
		{"max_context_length", `{"data":[{"id":"a","max_context_length":8192}]}`, 8192},
		{"none", `{"data":[{"id":"a"}]}`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			models, err := parseModelList([]byte(tc.body))
			if err != nil {
				t.Fatalf("parseModelList: %v", err)
			}
			if len(models) != 1 || models[0].ContextWindow != tc.want {
				t.Fatalf("expected window %d, got %+v", tc.want, models)
			}
		})
	}
}

// A bare array of ids still works (some local servers do this).
func TestParseModelListBareStringArray(t *testing.T) {
	body := []byte(`["llama3.1","qwen2.5"]`)
	models, err := parseModelList(body)
	if err != nil {
		t.Fatalf("parseModelList: %v", err)
	}
	if len(models) != 2 || models[0].ID != "llama3.1" {
		t.Fatalf("bare array not parsed: %+v", models)
	}
}

// Entries without any usable identifier are skipped rather than emitted blank.
func TestParseModelListSkipsBlankEntries(t *testing.T) {
	body := []byte(`{"data":[{"object":"model"},{"id":"  "},{"id":"ok"}]}`)
	models, err := parseModelList(body)
	if err != nil {
		t.Fatalf("parseModelList: %v", err)
	}
	if len(models) != 1 || models[0].ID != "ok" {
		t.Fatalf("expected only the valid entry, got %+v", models)
	}
}

// Reasoning sent as a plain array of effort names is also accepted.
func TestParseReasoningArrayForm(t *testing.T) {
	var raw json.RawMessage = []byte(`["none","low","high"]`)
	got, labels := parseReasoning(raw)
	if len(got) != 3 || got[0] != "none" {
		t.Fatalf("array form not parsed: %v", got)
	}
	if labels != nil {
		t.Fatalf("bare array has no display names, got %v", labels)
	}
}

// Providers may send Chinese display names for effort levels; they must be kept.
func TestParseReasoningKeepsDisplayNames(t *testing.T) {
	raw := json.RawMessage(`{"efforts":[
	  {"id":"off","name":"关闭","canonical":"none"},
	  {"id":"on","name":"开启","canonical":"high"}
	]}`)
	ids, labels := parseReasoning(raw)
	if len(ids) != 2 || ids[0] != "off" || ids[1] != "on" {
		t.Fatalf("effort ids not parsed: %v", ids)
	}
	if labels["off"] != "关闭" || labels["on"] != "开启" {
		t.Fatalf("effort display names not kept: %v", labels)
	}
}

// A name identical to the id is redundant and must not be stored.
func TestParseReasoningDropsRedundantNames(t *testing.T) {
	raw := json.RawMessage(`{"efforts":[{"id":"high","name":"high"}]}`)
	_, labels := parseReasoning(raw)
	if labels != nil {
		t.Fatalf("expected no labels when name == id, got %v", labels)
	}
}

func TestParseReasoningGarbage(t *testing.T) {
	for _, in := range []string{`null`, `{}`, `"text"`, `123`, `[]`} {
		if got, _ := parseReasoning(json.RawMessage(in)); got != nil {
			t.Fatalf("expected nil for %s, got %v", in, got)
		}
	}
}

// Vendor keys are shown with their Chinese names when known, raw otherwise.
func TestVendorLabel(t *testing.T) {
	cases := map[string]string{
		"deepseek-account": "DeepSeek 官方",
		"our-free-model":   "免费模型",
		"codearts":         "华为 CodeArts",
		"ollama":           "Ollama（本地）",
		"SomeNewVendor":    "SomeNewVendor", // unknown -> unchanged
		"":                 "",
		"mao/grok-4.5":     "Mao", // falls back to the first path segment
	}
	for in, want := range cases {
		if got := VendorLabel(in); got != want {
			t.Errorf("VendorLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// GroupLabel is derived from the vendor key on every model.
func TestParseModelListFillsGroupLabel(t *testing.T) {
	body := []byte(`{"data":[{"id":"codearts/glm-5.3-flash","owned_by":"codearts"}]}`)
	models, err := parseModelList(body)
	if err != nil {
		t.Fatalf("parseModelList: %v", err)
	}
	if models[0].Group != "codearts" || models[0].GroupLabel != "华为 CodeArts" {
		t.Fatalf("group label not filled: group=%q label=%q", models[0].Group, models[0].GroupLabel)
	}
}
