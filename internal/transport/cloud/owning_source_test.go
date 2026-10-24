package cloud

import "testing"

// The publisher's answers did not change when the rule moved to
// session.OwningSource, including the empty backend and the owned pty.
func TestPublisherOwningSourceKeepsItsAnswers(t *testing.T) {
	for _, c := range []struct {
		id   string
		row  inventoriedRow
		want string
	}{
		{"%8", inventoriedRow{backend: "tmux", tty: "/dev/ttys004"}, "tmux"},
		{"7A5C0000-0000-4000-8000-000000000009", inventoriedRow{backend: "iterm", tty: "ttys003"}, "iterm"},
		{"ttys003", inventoriedRow{backend: "iterm", tty: "ttys003"}, "ps"},
		{"ttys009", inventoriedRow{tty: "ttys009"}, "ps"},
		{"row-1", inventoriedRow{}, "ps"},
		{"owned:acp:synthetic", inventoriedRow{backend: "owned"}, "owned"},
		{"owned-1", inventoriedRow{backend: "owned", tty: "/dev/ttys010"}, "ps"},
	} {
		if got := owningSource(c.id, c.row); got != c.want {
			t.Errorf("%s %+v: owning source %q, want %q", c.id, c.row, got, c.want)
		}
	}
}
