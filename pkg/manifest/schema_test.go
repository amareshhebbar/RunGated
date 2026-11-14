package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func loadSchema(t *testing.T) map[string]any {
	raw, err := os.ReadFile(filepath.Join("..", "..", "spec", "ats", "manifest.v0.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("schema is not valid json: %v", err)
	}
	return s
}

func walk(v any, path string, fn func(path string, m map[string]any)) {
	switch x := v.(type) {
	case map[string]any:
		fn(path, x)
		for k, c := range x {
			walk(c, path+"/"+k, fn)
		}
	case []any:
		for i, c := range x {
			walk(c, path, fn)
			_ = i
		}
	}
}

func enumAt(t *testing.T, s map[string]any, keys ...string) []string {
	var cur any = s
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("path %v broke at %s", keys, k)
		}
		cur = m[k]
	}
	m := cur.(map[string]any)
	var out []string
	for _, e := range m["enum"].([]any) {
		out = append(out, e.(string))
	}
	sort.Strings(out)
	return out
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func TestSchemaEnumsMatchValidator(t *testing.T) {
	s := loadSchema(t)
	props := func(keys ...string) []string { return enumAt(t, s, keys...) }
	if got := props("properties", "spec", "properties", "riskTier"); !reflect.DeepEqual(got, sorted(RiskTiers)) {
		t.Errorf("riskTier enum %v != %v", got, RiskTiers)
	}
	if got := props("$defs", "mode"); !reflect.DeepEqual(got, sorted(Modes)) {
		t.Errorf("mode enum %v != %v", got, Modes)
	}
	if got := props("properties", "spec", "properties", "tools", "items", "properties", "blastRadius"); !reflect.DeepEqual(got, sorted(BlastRadii)) {
		t.Errorf("blastRadius enum %v != %v", got, BlastRadii)
	}
	if got := props("$defs", "argRule", "properties", "type"); !reflect.DeepEqual(got, sorted(ArgTypes)) {
		t.Errorf("arg type enum %v != %v", got, ArgTypes)
	}
	if got := props("properties", "spec", "properties", "autonomy", "properties", "overrides", "items", "properties", "actionClass"); !reflect.DeepEqual(got, sorted(ActionClasses)) {
		t.Errorf("actionClass enum %v != %v", got, ActionClasses)
	}
}

func jsonNames(t reflect.Type) []string {
	var out []string
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		if tag == "" {
			continue
		}
		name := tag
		for j := 0; j < len(tag); j++ {
			if tag[j] == ',' {
				name = tag[:j]
				break
			}
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func schemaProps(t *testing.T, s map[string]any, keys ...string) []string {
	var cur any = s
	for _, k := range keys {
		cur = cur.(map[string]any)[k]
	}
	var out []string
	for k := range cur.(map[string]any)["properties"].(map[string]any) {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestSchemaFieldsMatchGoStructs(t *testing.T) {
	s := loadSchema(t)
	cases := []struct {
		name string
		typ  reflect.Type
		path []string
	}{
		{"metadata", reflect.TypeOf(Metadata{}), []string{"properties", "metadata"}},
		{"spec", reflect.TypeOf(Spec{}), []string{"properties", "spec"}},
		{"model", reflect.TypeOf(Model{}), []string{"properties", "spec", "properties", "model"}},
		{"tool", reflect.TypeOf(Tool{}), []string{"properties", "spec", "properties", "tools", "items"}},
		{"mcpServer", reflect.TypeOf(MCPServer{}), []string{"properties", "spec", "properties", "mcpServers", "items"}},
		{"egress", reflect.TypeOf(Egress{}), []string{"properties", "spec", "properties", "egress"}},
		{"limits", reflect.TypeOf(Limits{}), []string{"properties", "spec", "properties", "limits"}},
		{"risk", reflect.TypeOf(Risk{}), []string{"properties", "spec", "properties", "risk"}},
		{"thresholds", reflect.TypeOf(Thresholds{}), []string{"properties", "spec", "properties", "risk", "properties", "thresholds"}},
		{"escalation", reflect.TypeOf(Escalation{}), []string{"properties", "spec", "properties", "escalation"}},
		{"privacy", reflect.TypeOf(Privacy{}), []string{"properties", "spec", "properties", "privacy"}},
		{"dataClasses", reflect.TypeOf(DataClasses{}), []string{"properties", "spec", "properties", "dataClasses"}},
		{"argRule", reflect.TypeOf(ArgRule{}), []string{"$defs", "argRule"}},
	}
	for _, c := range cases {
		if got, want := schemaProps(t, s, c.path...), jsonNames(c.typ); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: schema properties %v do not match struct fields %v", c.name, got, want)
		}
	}
}
