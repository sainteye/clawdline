package main

import (
	"encoding/json"
	"testing"
)

func TestDictionarySchemaGeneratesGoAndTypeScript(t *testing.T) {
	for _, row := range []struct {
		name, raw, goWant, tsWant string
	}{
		{"string values", `{"type":"object","additionalProperties":{"type":"string"}}`, "map[string]string", "Record<string, string>"},
		{"JSON values", `{"type":"object","additionalProperties":true}`, "map[string]any", "Record<string, unknown>"},
	} {
		t.Run(row.name, func(t *testing.T) {
			var value schema
			if err := json.Unmarshal([]byte(row.raw), &value); err != nil {
				t.Fatal(err)
			}
			if got := goType(&value, true); got != row.goWant {
				t.Errorf("Go type = %q; want %q", got, row.goWant)
			}
			if got := tsType(&value); got != row.tsWant {
				t.Errorf("TypeScript type = %q; want %q", got, row.tsWant)
			}
		})
	}
}
