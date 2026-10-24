package session

import "testing"

// One function says which source answers for a row; the inventory reading,
// the HTTP session list and the Cloud publisher all ask it.
func TestOwningSourceIsTheSourceThatListedTheRow(t *testing.T) {
	const guid = "7A5C0000-0000-4000-8000-000000000009"
	for _, c := range []struct {
		name string
		row  Session
		want string
	}{
		{"tmux pane", Session{ID: "%8", Backend: BackendTmux, TTY: "/dev/ttys004"}, "tmux"},
		{"iTerm2 tab", Session{ID: guid, Backend: BackendITerm, TTY: "ttys003"}, "iterm"},
		// The process scan files what it finds under iTerm2 and lists it by tty.
		{"ps row under iTerm2", Session{ID: "ttys003", Backend: BackendITerm, TTY: "ttys003"}, "ps"},
		{"ps row on Linux", Session{ID: "pts/3", Backend: BackendITerm, TTY: "pts/3"}, "ps"},
		{"ps row named by its own tty", Session{ID: "console", Backend: BackendITerm, TTY: "console"}, "ps"},
		{"no backend", Session{ID: "ttys009"}, "ps"},
		{"no backend, no id shape", Session{ID: "row-1"}, "ps"},
		{"no backend, pane id", Session{ID: "%2"}, "tmux"},
		{"no backend, iTerm2 id", Session{ID: guid}, "iterm"},
		{"owned ACP", Session{ID: "owned:acp:synthetic", Backend: BackendOwned}, "owned"},
		{"owned pty", Session{ID: "owned-1", Backend: BackendOwned, TTY: "/dev/ttys010"}, ""},
	} {
		if got := OwningSource(c.row); got != c.want {
			t.Errorf("%s: owning source %q, want %q", c.name, got, c.want)
		}
	}
}
