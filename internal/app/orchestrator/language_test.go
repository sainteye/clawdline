package orchestrator

import (
	"strings"
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

func TestCodexSessionCarriesTheBoardLanguage(t *testing.T) {
	b, ctx := newTestBroker(t)
	if _, err := nextconfig.Open(b.Dir).Set(map[string]any{"language": "zh-Hant"}); err != nil {
		t.Fatal(err)
	}
	launcher := &recordingLauncher{pane: "%92"}
	b.Launcher = launcher
	if _, err := b.openSession(ctx, t.TempDir(), "clawdline-language-test", "codex", "", "", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(launcher.line(),
		`developer_instructions="Write every Clawdline Board item title, description, and step you author in the language identified by BCP 47 tag \"zh-Hant\"."`) {
		t.Fatalf("the Codex launch does not carry the Board language:\n%s", launcher.line())
	}
}

func TestSessionLanguageFollowsClawdlineAndDefersToThePersonsClaudeSettings(t *testing.T) {
	dir := t.TempDir()
	if _, err := nextconfig.Open(dir).Set(map[string]any{"language": "zh-TW"}); err != nil {
		t.Fatal(err)
	}
	sets := false
	b := &Broker{Dir: dir, ClaudeSetsLanguage: func() bool { return sets }}
	if got := b.SessionLanguage("claude"); got != "Traditional Chinese (繁體中文)" {
		t.Fatalf("SessionLanguage(claude) = %q", got)
	}
	if got := b.SessionLanguage("codex"); got != "zh-TW" {
		t.Fatalf("SessionLanguage(codex) = %q, want zh-TW", got)
	}
	sets = true
	if got := b.SessionLanguage("claude"); got != "" {
		t.Fatalf("with the person's own setting, SessionLanguage = %q, want none", got)
	}
	if got := (&Broker{Dir: dir}).SessionLanguage("claude"); got != "" {
		t.Fatalf("with no reader, SessionLanguage = %q, want none", got)
	}
}
