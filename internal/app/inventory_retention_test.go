package app

import (
	"context"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// One failed iTerm2 listing used to replace six known states with the six
// process-table placeholders from that pass. This is the screenshot's flash,
// injected without Apple Events: the first pass knows every row is working;
// the next can still see the processes but cannot read iTerm2, so every fresh
// placeholder says unknown.
func TestOneFailedSourceReadingKeepsItsLastKnownRows(t *testing.T) {
	clock := time.Unix(1_000_000, 0)
	pass := 0
	r := NewInventoryReading(func(context.Context) session.Inventory {
		pass++
		state := session.StateWorking
		complete := true
		if pass > 1 {
			state = session.StateUnknown
			complete = false
		}
		rows := make([]session.Session, 6)
		for i := range rows {
			rows[i] = session.Session{
				ID: string(rune('a' + i)), Backend: session.BackendITerm,
				Assistant: session.AssistantClaude, State: state,
				Evidence: session.EvidenceProcess,
			}
		}
		return session.Inventory{
			Sessions: rows, Complete: complete, Provenance: "merged", ObservedAt: clock,
			Sources: map[string]bool{"ps": true, "iterm": complete},
		}
	}, time.Second)
	r.now = func() time.Time { return clock }

	first := r.Recent(context.Background())
	if len(first.Sessions) != 6 || first.Sessions[0].State != session.StateWorking {
		t.Fatalf("first reading = %+v", first)
	}
	clock = clock.Add(2 * time.Second)
	failed := r.Recent(context.Background())
	if len(failed.Sessions) != 6 {
		t.Fatalf("failed pass kept %d rows, want 6", len(failed.Sessions))
	}
	for _, row := range failed.Sessions {
		if row.State != session.StateWorking {
			t.Fatalf("one failed pass replaced %s with %s", row.ID, row.State)
		}
		if row.Observation.Freshness != session.FreshnessUnverified || !row.Observation.ObservedAt.Equal(clock.Add(-2*time.Second)) {
			t.Fatalf("retained row source = %+v", row.Observation)
		}
	}
	if failed.Observation.Freshness != session.FreshnessUnverified {
		t.Fatalf("whole failed pass source = %+v", failed.Observation)
	}
}

func TestRetainedRowsExpireAndFreshCallersNeverReceiveThem(t *testing.T) {
	clock := time.Unix(2_000_000, 0)
	pass := 0
	r := NewInventoryReading(func(context.Context) session.Inventory {
		pass++
		state := session.StateWorking
		complete := pass == 1
		if !complete {
			state = session.StateUnknown
		}
		return session.Inventory{
			Sessions: []session.Session{{ID: "tab", Backend: session.BackendITerm,
				Assistant: session.AssistantClaude, State: state}},
			Complete: complete, Provenance: "merged", ObservedAt: clock,
			Sources: map[string]bool{"ps": true, "iterm": complete},
		}
	}, time.Second)
	r.now = func() time.Time { return clock }
	r.SetRetentionAge(120)
	r.Recent(context.Background())

	clock = clock.Add(2 * time.Second)
	fresh := r.Fresh(context.Background())
	if len(fresh.Sessions) != 1 || fresh.Sessions[0].State != session.StateUnknown {
		t.Fatalf("a deciding caller received retained rows: %+v", fresh)
	}

	clock = clock.Add(118 * time.Second)
	display := r.Recent(context.Background())
	if len(display.Sessions) != 1 || display.Sessions[0].State != session.StateUnknown ||
		display.Sessions[0].Observation.Freshness != session.FreshnessMissing ||
		display.Observation.Freshness != session.FreshnessMissing {
		t.Fatalf("a row kept claiming its expired state: %+v", display)
	}
	if got := r.RetentionReading().Counters.Expired; got != 1 {
		t.Fatalf("expired %d retained sources, want 1", got)
	}
}

func TestFailedSourceDoesNotAgeSuccessfulSource(t *testing.T) {
	clock := time.Unix(3_000_000, 0)
	pass := 0
	r := NewInventoryReading(func(context.Context) session.Inventory {
		pass++
		itermState := session.StateWorking
		if pass > 1 {
			itermState = session.StateUnknown
		}
		return session.Inventory{
			Sessions: []session.Session{
				{ID: "iterm", Backend: session.BackendITerm, Assistant: session.AssistantClaude, State: itermState},
				{ID: "tmux", Backend: session.BackendTmux, Assistant: session.AssistantCodex, State: session.StateWorking},
			},
			Complete: pass == 1, Provenance: "merged", ObservedAt: clock,
			Sources: map[string]bool{"ps": true, "iterm": pass == 1, "tmux": true},
		}
	}, time.Second)
	r.now = func() time.Time { return clock }
	r.Recent(context.Background())

	clock = clock.Add(2 * time.Second)
	display := r.Recent(context.Background())
	if len(display.Sessions) != 2 {
		t.Fatalf("display has %d rows, want 2", len(display.Sessions))
	}
	for _, row := range display.Sessions {
		switch row.Backend {
		case session.BackendITerm:
			if row.Observation.Freshness != session.FreshnessUnverified ||
				!row.Observation.ObservedAt.Equal(clock.Add(-2*time.Second)) {
				t.Fatalf("iTerm row = %+v", row.Observation)
			}
		case session.BackendTmux:
			if row.Observation.Freshness != session.FreshnessCurrent ||
				!row.Observation.ObservedAt.Equal(clock) {
				t.Fatalf("tmux row = %+v", row.Observation)
			}
		}
	}
}

func TestLateRecentReaderDoesNotReceiveExpiredHeldReading(t *testing.T) {
	clock := time.Unix(4_000_000, 0)
	release := make(chan struct{})
	pass := 0
	r := NewInventoryReading(func(context.Context) session.Inventory {
		pass++
		if pass > 1 {
			<-release
		}
		return session.Inventory{
			Sessions: []session.Session{{ID: "tab", Backend: session.BackendITerm,
				Assistant: session.AssistantClaude, State: session.StateWorking}},
			Complete: true, Provenance: "merged", ObservedAt: clock,
			Sources: map[string]bool{"ps": true, "iterm": true},
		}
	}, time.Second)
	r.now = func() time.Time { return clock }
	r.SetRetentionAge(120)
	r.Recent(context.Background())

	clock = clock.Add(120 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	late := r.Recent(ctx)
	close(release)
	if late.Complete || len(late.Sessions) != 0 || late.Observation.Freshness != session.FreshnessMissing {
		t.Fatalf("late reader received expired state: %+v", late)
	}
}

// A successful close is a terminal answer, newer than both the held reading
// and a scan that was already on its way. Neither may resurrect that row while
// the source is unable to enumerate itself; the next complete enumeration
// takes authority back.
func TestAClosedTerminalIsForgottenAcrossAnUnansweredReading(t *testing.T) {
	clock := time.Unix(5_000_000, 0)
	pass := 0
	closed := session.Session{ID: "GUID-A", TTY: "ttys014", Backend: session.BackendITerm,
		Assistant: session.AssistantClaude, State: session.StateWorking}
	r := NewInventoryReading(func(context.Context) session.Inventory {
		pass++
		complete := pass == 1 || pass >= 3
		rows := []session.Session{closed}
		if pass == 3 {
			rows = nil
		}
		return session.Inventory{Sessions: rows, Complete: complete, Provenance: "merged",
			ObservedAt: clock, Sources: map[string]bool{"ps": true, "iterm": complete}}
	}, time.Second)
	r.now = func() time.Time { return clock }

	first := r.Recent(context.Background())
	if len(first.Sessions) != 1 {
		t.Fatalf("first reading = %+v", first)
	}
	clock = clock.Add(time.Millisecond)
	r.Forget(closed)
	if held, ok := r.Held(); !ok || len(held.Sessions) != 0 {
		t.Fatalf("the successful close left a held row: %+v, held=%v", held, ok)
	}

	clock = clock.Add(2 * time.Second)
	unanswered := r.Fresh(context.Background())
	if len(unanswered.Sessions) != 0 {
		t.Fatalf("an unanswered source resurrected the closed row: %+v", unanswered)
	}

	clock = clock.Add(2 * time.Second)
	confirmed := r.Recent(context.Background())
	if len(confirmed.Sessions) != 0 || !confirmed.Complete {
		t.Fatalf("the complete source did not confirm the absence: %+v", confirmed)
	}

	// If that terminal id is ever authoritatively enumerated again, existence
	// wins over the old close fact: ids are not metadata tombstones.
	clock = clock.Add(2 * time.Second)
	again := r.Recent(context.Background())
	if len(again.Sessions) != 1 || again.Sessions[0].ID != closed.ID {
		t.Fatalf("a newer complete enumeration could not show the terminal again: %+v", again)
	}
}
