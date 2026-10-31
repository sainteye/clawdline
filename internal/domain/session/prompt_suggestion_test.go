package session

import (
	"os"
	"strings"
	"testing"
)

// The frame and SGR transitions were captured from Claude Code 2.1.314 against
// a loopback fixture provider. The cursor reverses the first glyph, while the
// remaining unaccepted text is dim. Tab removes that dim attribute.
func promptFrame(input string) string {
	return "\x1b[38;2;136;136;136m────────────────────────\n\x1b[39m❯\u00a0" + input + "\n\x1b[38;2;136;136;136m────────────────────────\n\x1b[39m  ? for shortcuts\n"
}

func TestClaudePromptSuggestionRequiresNativeDimText(t *testing.T) {
	const want = "請幫我執行測試並整理結果"
	native := promptFrame("\x1b[7m請\x1b[0;2m幫我執行測試並整理結果\x1b[0m    ")
	if got := ClaudePromptSuggestion(native); got != want {
		t.Fatalf("native suggestion = %q, want %q", got, want)
	}
	for name, input := range map[string]string{
		"accepted draft":     promptFrame(want + "\x1b[7m \x1b[0m"),
		"no color":           Plain(native),
		"mixed actual draft": promptFrame("typed \x1b[2mthen dim\x1b[22m"),
		"ordinary answer":    "\x1b[2m" + want + "\x1b[22m",
		"initial example":    promptFrame("\x1b[2mTry \"write a test\"\x1b[22m"),
		"native truncation":  promptFrame("\x1b[7m請\x1b[0;2m幫我執行測試並整理…\x1b[0m"),
		"empty":              promptFrame(""),
		"partial multiline":  promptFrame("\x1b[2mfirst\n  second\x1b[22m"),
		"unframed chooser":   "❯ \x1b[2mYes\x1b[22m\n No\n",
		"unknown control":    promptFrame("\x1b[2mfirst\x1b[1Csecond\x1b[22m"),
	} {
		t.Run(name, func(t *testing.T) {
			if got := ClaudePromptSuggestion(input); got != "" {
				t.Fatalf("not a proven suggestion: %q", got)
			}
		})
	}
	if got := ClaudePromptSuggestion(promptFrame("\x1b[2mrun the tests\x1b[22m")); got != "run the tests" {
		t.Fatalf("unfocused suggestion = %q", got)
	}
	if got := ClaudePromptSuggestion(native + "\n" + promptFrame("typed")); got != "" {
		t.Fatalf("older frame won: %q", got)
	}
	if got := ClaudePromptSuggestion(strings.ReplaceAll(native, "\x1b[0;2m", "\x1b[2m")); got != want {
		t.Fatalf("combined cursor style = %q", got)
	}
}

func TestClaudePromptSuggestionNativeCaptures(t *testing.T) {
	for file, want := range map[string]string{"claude-prompt-suggestion.ansi": "請幫我執行測試並整理結果", "claude-prompt-draft-no-color.ansi": ""} {
		data, err := os.ReadFile("testdata/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if got := ClaudePromptSuggestion(string(data)); got != want {
			t.Errorf("%s: %q, want %q", file, got, want)
		}
	}
}
