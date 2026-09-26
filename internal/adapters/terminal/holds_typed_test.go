package terminal

import (
	"strings"
	"testing"
)

// An Enter pressed from a phone at words that were typed and never submitted
// goes only where it submits them to an assistant: its composer, holding
// their end or a paste placeholder. Anywhere else — an empty composer, other
// words, a shell's prompt, a chooser — it would submit or run something
// nobody sent.
func TestAnEnterFromElsewhereIsPressedOnlyAtTheWordsItNames(t *testing.T) {
	notice := strings.Repeat("x", 900) + "/v1/orchestrator/tasks/0000/completion/ack"
	cases := []struct {
		name, screen, typed string
		holds               bool
	}{
		{"claude's composer holds the words", composerScreen("please read CHILD.md"), "please read CHILD.md", true},
		{"a wrapped line still holds them", composerScreen("please read CHILD.md 0123456789abcdef\n  0123456789abcdef"),
			"please read CHILD.md 0123456789abcdef0123456789abcdef", true},
		{"claude's placeholder for a long paste", composerScreen("[Pasted text #5 +20 lines]"), notice, true},
		{"pictures alone name no words", composerScreen("[Image #1]"), "", true},
		{"codex's composer, drawn without a frame", "› please read CHILD.md\n\n  ⏎ send", "please read CHILD.md", true},
		{"an empty composer: the words went, or were cleared", composerScreen(""), "please read CHILD.md", false},
		{"other words in the composer", composerScreen("something else"), "please read CHILD.md", false},
		{"the words above an empty composer",
			strings.Join([]string{"❯ please read CHILD.md", rule, "❯ ", rule}, "\n"), "please read CHILD.md", false},
		{"a shell prompt drawn with claude's caret", "❯ please read CHILD.md", "please read CHILD.md", false},
		{"a shell", "user@host ~ % please read CHILD.md", "please read CHILD.md", false},
		{"a chooser", "❯ No, exit\n  Yes, I trust this folder\nEnter to confirm", "No, exit", false},
		{"nothing to name and nothing pasted", composerScreen("draft"), "", false},
	}
	for _, c := range cases {
		if got := HoldsTyped(c.screen, c.typed); got != c.holds {
			t.Errorf("%s: HoldsTyped = %v, want %v\n%s", c.name, got, c.holds, c.screen)
		}
	}
}
