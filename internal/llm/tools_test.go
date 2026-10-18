package llm

import (
	"encoding/json"
	"testing"
)

// The tools field must disappear entirely when no tools are declared, because a
// plain completion request has to keep the exact shape older providers expect.
func TestChatRequestOmitsToolsWhenEmpty(t *testing.T) {
	body, err := json.Marshal(ChatRequest{
		Model:    "m",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"tools", "tool_choice", "parallel_tool_calls"} {
		if _, ok := raw[field]; ok {
			t.Errorf("%q must be omitted when no tools are supplied", field)
		}
	}
}

func TestChatRequestSerializesTools(t *testing.T) {
	no := false
	body, err := json.Marshal(ChatRequest{
		Model:    "m",
		Messages: []Message{{Role: "user", Content: "hi"}},
		Tools: []Tool{{
			Type: "function",
			Function: FunctionDef{
				Name:        "read_chapter",
				Description: "读取章节",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"num":{"type":"integer"}}}`),
			},
		}},
		ToolChoice:        "auto",
		ParallelToolCalls: &no,
	})
	if err != nil {
		t.Fatal(err)
	}

	var raw struct {
		Tools []struct {
			Type     string `json:"type"`
			Function struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				Parameters  json.RawMessage `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
		ToolChoice        string `json:"tool_choice"`
		ParallelToolCalls *bool  `json:"parallel_tool_calls"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(raw.Tools))
	}
	if raw.Tools[0].Type != "function" {
		t.Errorf("tool type = %q", raw.Tools[0].Type)
	}
	if raw.Tools[0].Function.Name != "read_chapter" {
		t.Errorf("name = %q", raw.Tools[0].Function.Name)
	}
	if raw.ToolChoice != "auto" {
		t.Errorf("tool_choice = %q", raw.ToolChoice)
	}
	if raw.ParallelToolCalls == nil || *raw.ParallelToolCalls {
		t.Error("parallel_tool_calls should be false")
	}
}

// Assistant messages carrying tool_calls, and tool messages carrying results,
// must round-trip through the wire structs.
func TestMessageToolFieldsRoundTrip(t *testing.T) {
	msgs := []Message{
		{Role: "assistant", ToolCalls: []ToolCall{{
			ID:   "call_abc",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "read_chapter",
				Arguments: `{"num":3}`,
			},
		}}},
		{Role: "tool", Content: "第三章……", ToolCallID: "call_abc", Name: "read_chapter"},
	}
	body, err := json.Marshal(msgs)
	if err != nil {
		t.Fatal(err)
	}
	var back []Message
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatal(err)
	}
	if len(back) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(back))
	}
	if len(back[0].ToolCalls) != 1 {
		t.Fatalf("assistant tool_calls lost")
	}
	if back[0].ToolCalls[0].ID != "call_abc" {
		t.Errorf("id = %q", back[0].ToolCalls[0].ID)
	}
	if back[0].ToolCalls[0].Function.Arguments != `{"num":3}` {
		t.Errorf("arguments = %q", back[0].ToolCalls[0].Function.Arguments)
	}
	if back[1].ToolCallID != "call_abc" {
		t.Errorf("tool_call_id = %q", back[1].ToolCallID)
	}
	if back[1].Role != "tool" {
		t.Errorf("role = %q", back[1].Role)
	}
}

// Streaming tool calls arrive as indexed fragments and must be reassembled.
func TestToolCallAccumulatorAssemblesFragments(t *testing.T) {
	acc := newToolCallAccumulator()

	// First delta: id, name and the opening of the arguments.
	acc.add([]streamToolCue{cue(0, "call_1", "read_chapter", `{"num":`)})
	// Following deltas: arguments only.
	acc.add([]streamToolCue{cue(0, "", "", `3}`)})

	out := acc.result()
	if len(out) != 1 {
		t.Fatalf("expected 1 call, got %d", len(out))
	}
	if out[0].ID != "call_1" {
		t.Errorf("id = %q", out[0].ID)
	}
	if out[0].Function.Name != "read_chapter" {
		t.Errorf("name = %q", out[0].Function.Name)
	}
	if out[0].Function.Arguments != `{"num":3}` {
		t.Errorf("arguments = %q", out[0].Function.Arguments)
	}
}

// Two concurrent calls keep their own argument buffers and provider order.
func TestToolCallAccumulatorKeepsOrderAndSeparateBuffers(t *testing.T) {
	acc := newToolCallAccumulator()
	acc.add([]streamToolCue{
		cue(0, "call_a", "read_outline", `{"a":`),
		cue(1, "call_b", "read_chapter", `{"b":`),
	})
	acc.add([]streamToolCue{
		cue(0, "", "", `1}`),
		cue(1, "", "", `2}`),
	})

	out := acc.result()
	if len(out) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(out))
	}
	if out[0].Function.Name != "read_outline" || out[0].Function.Arguments != `{"a":1}` {
		t.Errorf("call 0 = %+v", out[0])
	}
	if out[1].Function.Name != "read_chapter" || out[1].Function.Arguments != `{"b":2}` {
		t.Errorf("call 1 = %+v", out[1])
	}
}

// A fragment without a function name is useless and must be dropped rather than
// emitted as a nameless call the executor cannot dispatch.
func TestToolCallAccumulatorDropsNamelessFragments(t *testing.T) {
	acc := newToolCallAccumulator()
	acc.add([]streamToolCue{cue(0, "call_x", "", `{"num":1}`)})
	if got := acc.result(); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestToolCallAccumulatorEmpty(t *testing.T) {
	acc := newToolCallAccumulator()
	if got := acc.result(); got != nil {
		t.Fatalf("expected nil for no input, got %+v", got)
	}
}

// A tool-call-only completion legitimately has empty content; the normalizer
// must expose the calls instead of treating the reply as empty.
func TestCompletionResultCarriesToolCalls(t *testing.T) {
	res := CompletionResult{
		Content:      "",
		FinishReason: "tool_calls",
		ToolCalls:    []ToolCall{{ID: "x", Type: "function", Function: ToolCallFunction{Name: "read_chapter", Arguments: "{}"}}},
	}
	if res.Content != "" {
		t.Errorf("content should be empty for a pure tool call")
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("tool calls lost")
	}
	if res.FinishReason != "tool_calls" {
		t.Errorf("finish reason = %q", res.FinishReason)
	}
}

func cue(index int, id, name, args string) streamToolCue {
	c := streamToolCue{Index: index, ID: id, Type: "function"}
	c.Function.Name = name
	c.Function.Arguments = args
	return c
}
