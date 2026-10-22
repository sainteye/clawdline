package nextconfig

import (
	"encoding/json"
	"testing"
)

func TestAntigravityDefaultsAreIndependent(t *testing.T) {
	v := Values{Raw: map[string]json.RawMessage{}}
	if DefaultModel(v, "antigravity") != "" {
		t.Fatal("absent model must use native default")
	}
	v.Raw["antigravity_default_model"] = json.RawMessage(`"native-synthetic"`)
	if DefaultModel(v, "antigravity") != "native-synthetic" {
		t.Fatal("native model lost")
	}
	if DefaultModel(v, "codex") != "" || DefaultReasoningEffort(v) != "" {
		t.Fatal("cross-provider fallback")
	}
	for _, invalid := range []string{`"INVALID MODEL"`, `42`, `null`} {
		v.Raw["antigravity_default_model"] = json.RawMessage(invalid)
		if DefaultModel(v, "antigravity") != "" {
			t.Fatal("invalid model admitted")
		}
	}
}

func TestAntigravityEffortNeverUsesCodexVocabulary(t *testing.T) {
	for _, effort := range []string{"low", "medium", "high", "xhigh", "max"} {
		v := Values{Raw: map[string]json.RawMessage{"antigravity_default_effort": json.RawMessage(`"` + effort + `"`)}}
		if DefaultAntigravityEffort(v) != effort {
			t.Fatalf("lost %s", effort)
		}
		if DefaultReasoningEffort(v) != "" {
			t.Fatal("native effort leaked into Codex")
		}
		key, ok := SettableByName("antigravity_default_effort")
		if !ok || !key.Check(effort) || key.Check("ultra") {
			t.Fatal("wrong effort validator")
		}
	}
	for _, raw := range []string{`"ultra"`, `42`, `null`, `""`} {
		v := Values{Raw: map[string]json.RawMessage{"antigravity_default_effort": json.RawMessage(raw)}}
		if DefaultAntigravityEffort(v) != "" {
			t.Fatal("invalid native effort admitted")
		}
	}
}

func TestAntigravityDefaultWritePreservesOtherProviders(t *testing.T) {
	f := Open(t.TempDir())
	if _, err := f.Set(map[string]any{"codex_default_model": "synthetic-codex", "codex_default_effort": "xhigh", "claude_default_model": "synthetic-claude"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := f.Set(map[string]any{"antigravity_default_model": "native-synthetic", "antigravity_default_effort": "medium"}); err != nil {
			t.Fatal(err)
		}
	}
	v, err := Open(f.dir).Read()
	if err != nil {
		t.Fatal(err)
	}
	if DefaultModel(v, "codex") != "synthetic-codex" || DefaultModel(v, "claude") != "synthetic-claude" || DefaultReasoningEffort(v) != "xhigh" || DefaultModel(v, "antigravity") != "native-synthetic" || DefaultAntigravityEffort(v) != "medium" {
		t.Fatal("provider default changed across repeated write and reopen")
	}
}
