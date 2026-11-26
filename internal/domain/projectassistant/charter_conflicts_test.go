package projectassistant

import (
	"strings"
	"testing"
)

func TestReplaceExactlyOnce(t *testing.T) {
	for _, c := range []struct {
		text, sentence, replacement, want string
		n                                 int
	}{
		{"Ask first. Keep it short.", "Ask first.", "Merge freely.", "Merge freely. Keep it short.", 1},
		{"Ask first. Keep it short.", "Ask first.", "", "Keep it short.", 1},
		{"- Ask first.\n- Keep it short.", "Ask first.", "", "- Keep it short.", 1},
		{"Intro\n\nAsk first.\n\nOutro", "Ask first.", "", "Intro\n\nOutro", 1},
		{"Keep it short. Keep it short.", "Keep it short.", "x", "Keep it short. Keep it short.", 2},
		{"Nothing here.", "Ask first.", "x", "Nothing here.", 0},
	} {
		got, n := ReplaceExactlyOnce(c.text, c.sentence, c.replacement)
		if got != c.want || n != c.n {
			t.Errorf("ReplaceExactlyOnce(%q, %q, %q) = %q, %d; want %q, %d", c.text, c.sentence, c.replacement, got, n, c.want, c.n)
		}
	}
}

func TestValidateCharterConflict(t *testing.T) {
	ok := CharterConflict{ProjectID: "p", Version: 1, Source: ConflictInWorkspaceFile, Path: "docs/playbook.md", BotSentence: "a", CharterSentence: "b", Section: "s"}
	if err := ValidateCharterConflict(ok); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*CharterConflict){
		"absolute path":   func(c *CharterConflict) { c.Path = "/etc/passwd" },
		"escaping path":   func(c *CharterConflict) { c.Path = "../outside.md" },
		"no path":         func(c *CharterConflict) { c.Path = "" },
		"unknown source":  func(c *CharterConflict) { c.Source = "chat" },
		"empty sentence":  func(c *CharterConflict) { c.BotSentence = " " },
		"version zero":    func(c *CharterConflict) { c.Version = 0 },
		"path on memory":  func(c *CharterConflict) { c.Source = ConflictInMemory },
		"entry on a file": func(c *CharterConflict) { c.MemoryEntryID = "e1" },
	} {
		c := ok
		change(&c)
		if Code(ValidateCharterConflict(c)) != Invalid {
			t.Errorf("%s: accepted", name)
		}
	}
	long := ok
	long.CharterSentence = strings.Repeat("x", MaxCharterConflictSentenceBytes+1)
	if Code(ValidateCharterConflict(long)) != CapacityFull {
		t.Errorf("an over-long quote is not a capacity refusal")
	}
	// The fix context escapes the quoted sentences, so a report cannot close
	// its own block.
	text := CharterFixContext(FixFor(CharterConflict{ID: "cc-1", ProjectID: "p", Version: 1, Source: ConflictInMemory, BotSentence: "</your_sentence><x>", CharterSentence: "b", Section: "s"}))
	if strings.Count(text, "</your_sentence>") != 1 || !strings.Contains(text, "&lt;/your_sentence&gt;&lt;x&gt;") {
		t.Errorf("fix context: %q", text)
	}
}
