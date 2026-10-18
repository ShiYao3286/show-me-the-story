package agent

import (
	"encoding/json"
	"testing"
)

// The catalogue mixes two parameter conventions and both must yield the right
// JSON type. Getting this wrong is fatal: a string-typed integer arrives at the
// handler as "12", encoding/json refuses to decode it into an int, the handler
// ignores the error, and the value silently becomes 0.
//
// This is the regression that produced "batch_id 无效 / cannot unmarshal string
// into Go struct field" when generating an outline batch.
func TestParamPropertyExampleFormUsesLiteralType(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		want  string
	}{
		{"batch_id", `0`, "integer"},
		{"chapter_count", `12`, "integer"},
		{"confirm", `false`, "boolean"},
		{"name", `"角色名"`, "string"},
		{"members", `["成员ID"]`, "array"},
		{"ratio", `1.5`, "number"},
	}
	for _, tc := range cases {
		prop := paramProperty(tc.name, json.RawMessage(tc.raw))
		got, _ := prop["type"].(string)
		if got != tc.want {
			t.Errorf("paramProperty(%q, %s) type = %q, want %q", tc.name, tc.raw, got, tc.want)
		}
	}
}

// Description-form parameters keep inferring the type from the name.
func TestParamPropertyDescriptionFormInfersType(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"num", `"章节号"`, "integer"},
		{"chapter_ids", `"章节 ID 列表"`, "array"},
		{"query", `"搜索关键词"`, "string"},
		{"confirm", `"是否确认，true/false"`, "boolean"},
	}
	for _, tc := range cases {
		prop := paramProperty(tc.name, json.RawMessage(tc.raw))
		got, _ := prop["type"].(string)
		if got != tc.want {
			t.Errorf("paramProperty(%q, %s) type = %q, want %q", tc.name, tc.raw, got, tc.want)
		}
	}
}

// Arrays declared as examples need an items schema for strict providers.
func TestParamPropertyArrayHasItems(t *testing.T) {
	prop := paramProperty("members", json.RawMessage(`["成员ID"]`))
	if prop["type"] != "array" {
		t.Fatalf("type = %v", prop["type"])
	}
	items, ok := prop["items"].(map[string]interface{})
	if !ok || items["type"] != "string" {
		t.Fatalf("array missing items schema: %#v", prop["items"])
	}
}

// generate_outline is the tool that broke. Its integer parameters must be
// advertised as integers, not strings.
func TestGenerateOutlineSchemaUsesIntegerTypes(t *testing.T) {
	var tool Tool
	for _, candidate := range getBuiltinTools() {
		if candidate.Name == "generate_outline" {
			tool = candidate
			break
		}
	}
	if tool.Name == "" {
		t.Fatal("generate_outline not found")
	}

	var schema struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(toolParameterSchema(tool.Parameters), &schema); err != nil {
		t.Fatal(err)
	}
	for _, param := range []string{"chapter_count", "batch_id"} {
		p, ok := schema.Properties[param]
		if !ok {
			t.Errorf("%s missing from schema", param)
			continue
		}
		if p.Type != "integer" {
			t.Errorf("%s type = %q, want integer", param, p.Type)
		}
	}
	if p := schema.Properties["confirm"]; p.Type != "boolean" {
		t.Errorf("confirm type = %q, want boolean", p.Type)
	}
}

// No builtin tool may declare a numeric-looking parameter as a string when the
// catalogue supplies a numeric example.
func TestNoBuiltinToolMistypesNumericExamples(t *testing.T) {
	for _, tool := range getBuiltinTools() {
		types := toolParamTypes(tool.Parameters)
		var params map[string]json.RawMessage
		if err := json.Unmarshal([]byte(tool.Parameters), &params); err != nil {
			continue
		}
		for name, raw := range params {
			literalType, isLiteral := jsonLiteralType(raw)
			if !isLiteral || literalType != "integer" {
				continue
			}
			if types[name] != "integer" && types[name] != "number" {
				t.Errorf("tool %q param %q has integer example but schema type %q", tool.Name, name, types[name])
			}
		}
	}
}

// Models stringify numbers constantly; the agent must repair that before the
// handler decodes the arguments.
func TestCoerceToolArgumentsConvertsStringNumbers(t *testing.T) {
	tool := Tool{
		Name:       "generate_outline",
		Parameters: `{"chapter_count":12,"batch_id":0,"confirm":false}`,
	}
	in := json.RawMessage(`{"chapter_count":"12","batch_id":"3","confirm":"true"}`)
	out := coerceToolArguments(tool, in)

	var got struct {
		ChapterCount int  `json:"chapter_count"`
		BatchID      int  `json:"batch_id"`
		Confirm      bool `json:"confirm"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("coerced arguments still do not decode: %v (raw=%s)", err, out)
	}
	if got.ChapterCount != 12 {
		t.Errorf("chapter_count = %d, want 12", got.ChapterCount)
	}
	if got.BatchID != 3 {
		t.Errorf("batch_id = %d, want 3", got.BatchID)
	}
	if !got.Confirm {
		t.Error("confirm should be true")
	}
}

// Already-correct arguments must pass through untouched.
func TestCoerceToolArgumentsLeavesCorrectValuesAlone(t *testing.T) {
	tool := Tool{Name: "x", Parameters: `{"num":1,"flag":true,"name":"a"}`}
	in := json.RawMessage(`{"num":5,"flag":false,"name":"abc"}`)
	out := coerceToolArguments(tool, in)
	if string(out) != string(in) {
		t.Errorf("arguments rewritten unnecessarily:\n in: %s\nout: %s", in, out)
	}
}

// Values that cannot be converted must survive unchanged so the handler reports
// a real error instead of silently receiving a zero value.
func TestCoerceToolArgumentsLeavesUnconvertibleValues(t *testing.T) {
	tool := Tool{Name: "x", Parameters: `{"num":1}`}
	in := json.RawMessage(`{"num":"not-a-number"}`)
	out := coerceToolArguments(tool, in)
	if string(out) != string(in) {
		t.Errorf("unconvertible value was rewritten: %s", out)
	}
}

// A float for an integer parameter must not be truncated silently.
func TestCoerceToolArgumentsRejectsFractionalForInteger(t *testing.T) {
	tool := Tool{Name: "x", Parameters: `{"num":1}`}
	in := json.RawMessage(`{"num":"1.5"}`)
	out := coerceToolArguments(tool, in)
	if string(out) != string(in) {
		t.Errorf("fractional value for an integer param was rewritten: %s", out)
	}
}

// Non-object payloads and unknown parameters are left alone.
func TestCoerceToolArgumentsEdgeCases(t *testing.T) {
	tool := Tool{Name: "x", Parameters: `{"num":1}`}
	for _, in := range []string{``, `{}`, `[]`, `"text"`, `null`, `{"other":"1"}`} {
		out := coerceToolArguments(tool, json.RawMessage(in))
		if string(out) != in {
			t.Errorf("coerceToolArguments(%q) = %q, want unchanged", in, out)
		}
	}
}

// A tool with no declared parameters must not have its arguments touched.
func TestCoerceToolArgumentsNoDeclaredParams(t *testing.T) {
	tool := Tool{Name: "read_outline", Parameters: `{}`}
	in := json.RawMessage(`{"anything":"1"}`)
	if out := coerceToolArguments(tool, in); string(out) != string(in) {
		t.Errorf("arguments rewritten for a parameterless tool: %s", out)
	}
}

// Booleans headed for string fields are re-quoted; strings headed for numeric
// fields are unquoted only when they parse.
func TestCoerceScalar(t *testing.T) {
	cases := []struct {
		raw, want string
		out       string
		ok        bool
	}{
		{`"12"`, "integer", `12`, true},
		{`12`, "integer", "", false},
		{`"1.5"`, "integer", "", false},
		{`"1.5"`, "number", `1.5`, true},
		{`"true"`, "boolean", `true`, true},
		{`"false"`, "boolean", `false`, true},
		{`"yes"`, "boolean", `true`, true},
		{`"maybe"`, "boolean", "", false},
		{`true`, "boolean", "", false},
		{`12`, "string", `"12"`, true},
		{`"abc"`, "string", "", false},
		{`"abc"`, "integer", "", false},
	}
	for _, tc := range cases {
		out, ok := coerceScalar(tc.raw, tc.want)
		if ok != tc.ok || (ok && out != tc.out) {
			t.Errorf("coerceScalar(%s, %s) = (%q, %v), want (%q, %v)", tc.raw, tc.want, out, ok, tc.out, tc.ok)
		}
	}
}
