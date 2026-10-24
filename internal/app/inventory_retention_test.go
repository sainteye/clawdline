package app

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// A successful merge names a row by its terminal id. When that terminal
// source times out, ps names the same execution by tty instead. Retention
// must replace that weaker row, not append a second copy beside it.
func TestRetainedTerminalsDoNotDuplicateTheirProcessRows(t *testing.T) {
	for _, backend := range []session.Backend{session.BackendITerm, session.BackendTmux} {
		t.Run(string(backend), func(t *testing.T) {
			clock := time.Unix(6_000_000, 0)
			firstAt := clock
			complete := true
			source := session.SourceFor(backend)
			terminals := make([]session.Session, 6)
			processes := make([]session.Session, 6)
			for n := range terminals {
				tty := fmt.Sprintf("ttys%03d", n)
				terminals[n] = session.Session{ID: fmt.Sprintf("terminal-%d", n), TTY: tty,
					Backend: backend, PID: n + 10, Assistant: session.AssistantCodex,
					Label: "same title", State: session.StateWorking}
				processes[n] = terminals[n]
				processes[n].ID = tty
				processes[n].Backend = session.BackendITerm
				processes[n].State = session.StateUnknown
			}
			// A genuinely different terminal may have the same title.
			newcomer := session.Session{ID: "ttys099", TTY: "ttys099", Backend: session.BackendITerm,
				Assistant: session.AssistantCodex, Label: "same title", State: session.StateIdle}
			r := NewInventoryReading(func(context.Context) session.Inventory {
				rows := processes
				if complete {
					rows = terminals
				}
				return session.Inventory{ObservedAt: clock, Complete: complete, Provenance: "merged",
					Sources:  map[string]bool{"ps": true, source: complete},
					Sessions: append(append([]session.Session(nil), rows...), newcomer)}
			}, time.Second)
			r.now = func() time.Time { return clock }
			assertRows := func(inv session.Inventory, retained bool) {
				t.Helper()
				if len(inv.Sessions) != 7 {
					t.Fatalf("listed %d rows for 7 terminals", len(inv.Sessions))
				}
				seen := map[string]bool{}
				for _, row := range inv.Sessions {
					if seen[row.TTY] {
						t.Fatalf("terminal %s was listed twice", row.TTY)
					}
					seen[row.TTY] = true
					if row.ID == newcomer.ID {
						if row.Observation.Freshness != session.FreshnessCurrent {
							t.Fatalf("new process row lost freshness: %+v", row)
						}
						continue
					}
					if row.ID == row.TTY || row.State != session.StateWorking {
						t.Fatalf("terminal identity or state was replaced: %+v", row)
					}
					if retained && (row.Observation.Freshness != session.FreshnessUnverified ||
						!row.Observation.ObservedAt.Equal(firstAt)) {
						t.Fatalf("retained row claimed a current observation: %+v", row)
					}
				}
			}
			assertRows(r.Recent(context.Background()), false)
			complete = false
			clock = clock.Add(2 * time.Second)
			assertRows(r.Recent(context.Background()), true)
			clock = clock.Add(2 * time.Second)
			fresh := r.Fresh(context.Background())
			if len(fresh.Sessions) != 7 || fresh.Sessions[0].ID != processes[0].ID {
				t.Fatalf("decision reader received retained terminal identities: %+v", fresh)
			}
			assertRows(r.Recent(context.Background()), true)
			clock = firstAt.Add(LastGoodInventoryAgeLimit)
			expired := r.Recent(context.Background())
			if len(expired.Sessions) != 7 {
				t.Fatalf("expired shelf lost process rows: %+v", expired)
			}
			for _, row := range expired.Sessions {
				if row.ID != row.TTY {
					t.Fatalf("expired terminal remained on screen: %+v", row)
				}
			}
			complete = true
			clock = clock.Add(2 * time.Second)
			assertRows(r.Recent(context.Background()), false)
		})
	}
}

func TestRetainedRowsDoNotReplaceCurrentTerminalReadings(t *testing.T) {
	for _, previousBackend := range []session.Backend{session.BackendITerm, session.BackendTmux} {
		t.Run(string(previousBackend), func(t *testing.T) {
			clock := time.Unix(7_000_000, 0)
			previous := session.Session{ID: "previous", TTY: "ttys001", Backend: previousBackend,
				Assistant: session.AssistantCodex, State: session.StateWorking}
			// Cover both a retained process placeholder and a retained terminal.
			if previousBackend == session.BackendITerm {
				previous.ID = previous.TTY
			}
			source := rowSource(previous)
			current := session.Session{ID: "current", TTY: previous.TTY, Backend: session.BackendITerm,
				Assistant: session.AssistantCodex, State: session.StateIdle}
			r := NewInventoryReading(nil, time.Second)
			r.now = func() time.Time { return clock }
			r.retainLocked(session.Inventory{ObservedAt: clock, Complete: true,
				Sources: map[string]bool{source: true}, Sessions: []session.Session{previous}})
			clock = clock.Add(2 * time.Second)
			got := r.retainLocked(session.Inventory{ObservedAt: clock,
				Sources: map[string]bool{source: false, "iterm": true}, Sessions: []session.Session{current}})
			if len(got.Sessions) != 1 || got.Sessions[0].ID != current.ID ||
				got.Sessions[0].State != session.StateIdle ||
				got.Sessions[0].Observation.Freshness != session.FreshnessCurrent ||
				!got.Sessions[0].Observation.ObservedAt.Equal(clock) {
				t.Fatalf("retention displaced the current terminal: %+v", got)
			}
		})
	}
}

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
