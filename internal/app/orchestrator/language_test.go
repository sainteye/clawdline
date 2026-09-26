package orchestrator

import (
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/whisper"
)

func TestDisplayLanguageFollowsSettingMachineThenCatalog(t *testing.T) {
	machine := []whisper.Answer{{Tag: "zh-TW", Source: whisper.FromAppleLangs}}
	cases := []struct {
		name, setting, catalog, want string
		machine                      []whisper.Answer
	}{
		{"setting", "en", "zh-Hant", "en", machine},
		{"auto asks machine", "auto", "zh-Hant", "zh-TW", machine},
		{"missing asks machine", "", "zh-Hant", "zh-TW", machine},
		{"catalog fallback", "auto", "zh-Hant", "zh-Hant", nil},
		{"built-in fallback", "auto", "", "zh-Hant", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := displayLanguage(c.setting, c.machine, c.catalog); got != c.want {
				t.Fatalf("displayLanguage() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestClaudeLanguageDefersToThePersonsClaudeSettings(t *testing.T) {
	dir := t.TempDir()
	if _, err := nextconfig.Open(dir).Set(map[string]any{"language": "zh-TW"}); err != nil {
		t.Fatal(err)
	}
	sets := false
	b := &Broker{Dir: dir, ClaudeSetsLanguage: func() bool { return sets }}
	if got := b.ClaudeLanguage("claude"); got != "Traditional Chinese (繁體中文)" {
		t.Fatalf("ClaudeLanguage(claude) = %q", got)
	}
	if got := b.ClaudeLanguage("codex"); got != "" {
		t.Fatalf("ClaudeLanguage(codex) = %q, want none", got)
	}
	sets = true
	if got := b.ClaudeLanguage("claude"); got != "" {
		t.Fatalf("with the person's own setting, ClaudeLanguage = %q, want none", got)
	}
	if got := (&Broker{Dir: dir}).ClaudeLanguage("claude"); got != "" {
		t.Fatalf("with no reader, ClaudeLanguage = %q, want none", got)
	}
}
