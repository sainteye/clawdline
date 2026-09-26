package projects

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeLanguageNamesOnlyWhatItKnows(t *testing.T) {
	cases := map[string]string{
		"zh-Hant":     claudeTraditionalChinese,
		"zh-TW":       claudeTraditionalChinese,
		"zh_HK.UTF-8": claudeTraditionalChinese,
		"zh-Hans":     claudeSimplifiedChinese,
		"zh-CN":       claudeSimplifiedChinese,
		"zh":          claudeSimplifiedChinese,
		"ja-JP":       "Japanese (日本語)",
		"ko":          "Korean (한국어)",
		"en-US":       "",
		"en":          "",
		"tlh":         "",
		"C":           "",
		"":            "",
	}
	for tag, want := range cases {
		if got := ClaudeLanguage(tag); got != want {
			t.Errorf("ClaudeLanguage(%q) = %q, want %q", tag, got, want)
		}
	}
}

func TestClaudeSetsLanguageReadsOnlyTheKey(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cases := []struct {
		name string
		path string
		want bool
	}{
		{"missing file", filepath.Join(dir, "absent.json"), false},
		{"no key", write("theme.json", `{"theme":"dark"}`), false},
		{"empty key", write("empty.json", `{"language":"  "}`), false},
		{"named", write("named.json", `{"language":"japanese"}`), true},
		{"unparseable", write("broken.json", `{"language":`), true},
	}
	for _, c := range cases {
		if got := ClaudeSetsLanguage(c.path); got != c.want {
			t.Errorf("%s: ClaudeSetsLanguage = %v, want %v", c.name, got, c.want)
		}
	}
}

// The language travels as JSON in a line a shell reads, so its exact spelling
// is pinned, and anything outside the closed list never reaches the line.
func TestLaunchCarriesTheResponseLanguage(t *testing.T) {
	l, err := Admit(LaunchRequest{ProjectRoot: "/p", Assistant: AssistantClaude, Model: "opus",
		Resume: "1a000000-0000-4000-8000-000000000001", Language: claudeTraditionalChinese})
	if err != nil {
		t.Fatal(err)
	}
	want := `claude --resume 1a000000-0000-4000-8000-000000000001 --model opus ` +
		`--settings '{"language":"Traditional Chinese (繁體中文)"}'`
	if got := l.ShellCommand(); got[len(got)-len(want):] != want {
		t.Fatalf("ShellCommand() = %q, want it to end %q", got, want)
	}

	for _, req := range []LaunchRequest{
		{ProjectRoot: "/p", Assistant: AssistantCodex, Language: claudeTraditionalChinese},
		{ProjectRoot: "/p", Assistant: AssistantClaude, Language: `x"}' ; rm -rf ~ ; '`},
		{ProjectRoot: "/p", Assistant: AssistantClaude, Language: "english"},
	} {
		if _, err := Admit(req); !errors.Is(err, ErrInvalidLaunch) {
			t.Errorf("Admit(%q, %q) = %v, want invalid_launch", req.Assistant, req.Language, err)
		}
	}
}
