package app

import (
	"context"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// itermOff is a merged reading with iTerm2 scanning turned off
// (`iterm_scan=false`, D71): every source that was asked answered in full,
// and iTerm2 was not asked.
func itermOff(rows ...session.Session) session.Inventory {
	return session.Inventory{Complete: false, Sessions: rows,
		Sources:         map[string]bool{"ps": true, "tmux": true},
		DisabledSources: map[string]string{"iterm": "setting"},
		Notes:           []string{"iTerm2 scanning is turned off"}}
}

func itermRow(terminal, conversation string) session.Session {
	row := claudeRow(terminal, conversation)
	row.Backend = session.BackendITerm
	return row
}

// With iTerm2 off the record goes on following what the other sources see,
// and an iTerm2 conversation it recorded earlier is not taken as gone because
// a source nobody asked did not list it.
func TestRestoreRecordingGoesOnWhileITermIsOff(t *testing.T) {
	st := restoreStore(t)
	clock := time.Unix(10_000, 0)
	r := restoreUnder(st, "boot-now", &clock)
	ctx := context.Background()

	r.Observe(ctx, complete(claudeRow("%1", "tmux-a"),
		itermRow("AAAAAAAA-0000-4000-8000-000000000001", "iterm-a")))
	clock = clock.Add(time.Minute)
	r.Observe(ctx, itermOff(claudeRow("%2", "tmux-b")))

	rows := recorded(t, st, "boot-now")
	if _, ok := rows["tmux-b"]; !ok {
		t.Fatalf("a tmux conversation seen while iTerm2 is off was not recorded: %v", rows)
	}
	if !rows["tmux-a"].GoneAt.Equal(clock) {
		t.Fatalf("a tmux conversation tmux no longer lists is gone: %+v", rows["tmux-a"])
	}
	if !rows["iterm-a"].GoneAt.IsZero() {
		t.Fatalf("an iTerm2 conversation was taken as gone while iTerm2 was not read: %+v", rows["iterm-a"])
	}
}

// The coordinator's bearings call the sessions current when every source that
// was asked answered.
func TestBearingsSessionsAreCurrentWhileITermIsOff(t *testing.T) {
	c := &Coordinator{}
	b := c.Bearings(context.Background(), State{Seen: Seen{Inventory: itermOff()}})
	if b.SessionsFresh != "current" {
		t.Fatalf("sessions with iTerm2 off: %s (unknown %v)", b.SessionsFresh, b.Unknown)
	}
	b = c.Bearings(context.Background(), State{Seen: Seen{Inventory: session.Inventory{
		Sources: map[string]bool{"ps": true, "tmux": false}, DisabledSources: map[string]string{"iterm": "setting"}}}})
	if b.SessionsFresh != "stale" {
		t.Fatalf("a tmux that did not finish is still stale: %s", b.SessionsFresh)
	}
}
