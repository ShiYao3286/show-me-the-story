package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"showmethestory/internal/llm"
)

// buildToolDeclarations converts the app's compact tool catalogue into OpenAI
// function-calling declarations.
//
// Tool.Parameters uses a shorthand such as {"num": "章节号", "feedback": "修改意见"}
// where the value is a human description, not a schema. We infer the JSON type
// from the parameter name and default everything to string, which is what the
// text protocol effectively required anyway.
func buildToolDeclarations(tools []Tool) []llm.Tool {
	out := make([]llm.Tool, 0, len(tools))
	for _, t := range tools {
		out = append(out, llm.Tool{
			Type: "function",
			Function: llm.FunctionDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  toolParameterSchema(t.Parameters),
			},
		})
	}
	return out
}

// toolParameterSchema turns the shorthand parameter map into a JSON Schema.
// Unparseable or empty input yields an object schema with no properties.
func toolParameterSchema(shorthand string) json.RawMessage {
	type schema struct {
		Type       string                 `json:"type"`
		Properties map[string]interface{} `json:"properties,omitempty"`
		Required   []string               `json:"required,omitempty"`
	}
	s := schema{Type: "object", Properties: map[string]interface{}{}}

	trimmed := strings.TrimSpace(shorthand)
	if trimmed == "" || trimmed == "{}" || trimmed == "null" {
		b, _ := json.Marshal(s)
		return b
	}

	var params map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &params); err != nil {
		b, _ := json.Marshal(s)
		return b
	}
	if len(params) == 0 {
		b, _ := json.Marshal(s)
		return b
	}

	for name, raw := range params {
		s.Properties[name] = paramProperty(name, raw)
	}

	b, _ := json.Marshal(s)
	return b
}

// paramProperty builds one JSON Schema property.
//
// The catalogue mixes two conventions, and getting the type wrong is fatal: a
// string-typed integer reaches the handler as "12", which encoding/json refuses
// to unmarshal into an int ("cannot unmarshal string into Go struct field").
//
//   - Example form: {"num": 1} or {"confirm": true} — the literal already has the
//     right JSON type, so trust it.
//   - Description form: {"num": "章节号"} — the string is human text, so the type
//     is inferred from the parameter name.
func paramProperty(name string, raw json.RawMessage) map[string]interface{} {
	// Example form: a non-string literal states the type directly.
	if t, ok := jsonLiteralType(raw); ok {
		prop := map[string]interface{}{"type": t}
		switch t {
		case "array":
			prop["items"] = map[string]interface{}{"type": "string"}
		case "object":
			// An object example cannot describe a schema; accept any object.
			prop = map[string]interface{}{"type": "object"}
		}
		return prop
	}

	// Description form: the value is human-readable text.
	var desc string
	if err := json.Unmarshal(raw, &desc); err != nil {
		desc = string(raw)
	}
	t := inferParamType(name, desc)
	prop := map[string]interface{}{
		"type":        t,
		"description": desc,
	}
	if t == "array" {
		prop["items"] = map[string]interface{}{"type": "string"}
	}
	return prop
}

// jsonLiteralType reports the JSON type of a non-string literal. It returns false
// for strings, because those carry descriptions rather than values.
func jsonLiteralType(raw json.RawMessage) (string, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return "", false
	}
	switch trimmed[0] {
	case '"':
		return "", false
	case '{':
		return "object", true
	case '[':
		return "array", true
	}
	switch trimmed {
	case "true", "false":
		return "boolean", true
	case "null":
		return "", false
	}
	// Anything else that parses as a JSON number is a number. Integers use
	// "integer" so schema-validating providers reject 1.5 for a chapter number.
	var num json.Number
	if err := json.Unmarshal(raw, &num); err == nil {
		if strings.ContainsAny(num.String(), ".eE") {
			return "number", true
		}
		return "integer", true
	}
	return "", false
}

// inferParamType guesses a JSON type from the parameter name and description.
// Getting this wrong is not fatal — models mostly follow the description — but
// correct types reduce malformed arguments such as stringified numbers.
func inferParamType(name, desc string) string {
	n := strings.ToLower(name)
	d := strings.ToLower(desc)
	for _, marker := range []string{"[]", "列表", "数组", "array", "ids"} {
		if strings.Contains(d, marker) || strings.HasSuffix(n, "_ids") || strings.HasSuffix(n, "list") {
			return "array"
		}
	}
	numberSuffixes := []string{"_num", "num", "count", "chapter", "_id", "id", "from", "to", "limit", "offset", "index", "tokens", "words", "length"}
	for _, suffix := range numberSuffixes {
		if n == suffix || strings.HasSuffix(n, suffix) {
			// "id"/"name"-style identifiers stay strings unless clearly numeric.
			if (suffix == "_id" || suffix == "id") && !strings.Contains(d, "数字") && !strings.Contains(d, "编号") {
				return "string"
			}
			return "integer"
		}
	}
	for _, marker := range []string{"true", "false", "是否", "开关"} {
		if strings.Contains(d, marker) {
			return "boolean"
		}
	}
	return "string"
}

var nonIdentifier = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// sanitizeToolName keeps function names within the [a-zA-Z0-9_-]{1,64} window
// that providers enforce.
func sanitizeToolName(name string) string {
	clean := nonIdentifier.ReplaceAllString(name, "_")
	if clean == "" {
		clean = "tool"
	}
	if len(clean) > 64 {
		clean = clean[:64]
	}
	return clean
}

// toNativeToolCall projects the persisted tool call onto the wire format. The
// id is synthesized deterministically because the text protocol never had one;
// it only has to be unique within the conversation.
func toNativeToolCall(tc *ToolCall) llm.ToolCall {
	args := strings.TrimSpace(string(tc.Arguments))
	if args == "" {
		args = "{}"
	}
	return llm.ToolCall{
		ID:   fmt.Sprintf("call_%s", sanitizeToolName(tc.Name)),
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      sanitizeToolName(tc.Name),
			Arguments: args,
		},
	}
}
