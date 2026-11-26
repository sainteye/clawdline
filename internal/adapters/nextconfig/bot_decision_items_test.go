package nextconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBotDecisionItemsIsOffUnlessTheFileTurnsItOn(t *testing.T) {
	dir := t.TempDir()
	f := Open(dir)
	for _, c := range []struct {
		file string
		want bool
	}{
		{"", false},
		{`{"bot_decision_items":false}`, false},
		{`{"bot_decision_items":"yes"}`, false},
		{`{"bot_decision_items":true}`, true},
	} {
		if c.file != "" {
			if err := os.WriteFile(filepath.Join(dir, FileName), []byte(c.file), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		v, err := f.Read()
		if err != nil {
			t.Fatal(err)
		}
		if got := v.BotDecisionItems(); got != c.want {
			t.Errorf("%q: %t, want %t", c.file, got, c.want)
		}
	}
	// Settings → Workflow writes it, so it is a settable boolean and nothing else.
	if s, settable := SettableByName("bot_decision_items"); !settable || s.Kind != "bool" {
		t.Fatalf("bot_decision_items is not a settable boolean: %+v %t", s, settable)
	}
}
