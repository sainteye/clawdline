package app

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Send types the person's words and then one line that names the row, so the
// Session that does the work can check that row off.
func TestDirectTodoSendTextNamesTheRowOnItsLastLine(t *testing.T) {
	const line = "(Clawdline to-do td-7. When it is done: clawdline todo done td-7)"
	for _, c := range []struct{ name, text, want string }{
		{"one line", "Fix the login page", "Fix the login page\n\n" + line},
		{"trailing newline", "Fix the login page\n", "Fix the login page\n\n" + line},
		{"trailing CRLF", "Fix it\r\n\r\n", "Fix it\n\n" + line},
		{"multi-line", "Fix the login page\n- keep the logo\n- test on a phone",
			"Fix the login page\n- keep the logo\n- test on a phone\n\n" + line},
		{"leading space kept", "  indented", "  indented\n\n" + line},
		// Creation refuses empty text today (todo_text_required); a row that
		// somehow has none still says what it is rather than a blank line.
		{"empty", "", line},
		{"only whitespace", " \n", line},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := DirectTodoSendText("td-7", c.text); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestTodoPreviewIsOneLineAndCutsBetweenCharacters(t *testing.T) {
	if got := TodoPreview("Fix\nthe   login\tpage"); got != "Fix the login page" {
		t.Fatalf("folded: %q", got)
	}
	exact := strings.Repeat("字", reportOpenTodoTextLimit)
	if got := TodoPreview(exact); got != exact {
		t.Fatalf("a text at the limit was changed: %d runes", utf8.RuneCountInString(got))
	}
	got := TodoPreview(strings.Repeat("字", reportOpenTodoTextLimit+1))
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) != reportOpenTodoTextLimit || !strings.HasSuffix(got, "…") {
		t.Fatalf("cut: %d runes, valid %v, %q", utf8.RuneCountInString(got), utf8.ValidString(got), got)
	}
}
