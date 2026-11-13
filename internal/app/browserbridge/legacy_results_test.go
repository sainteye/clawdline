package browserbridge

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestLegacyResultRetriesRemainAcknowledgedAfterTheCallerReturns(t *testing.T) {
	for _, op := range []string{"read", "close"} {
		t.Run(op, func(t *testing.T) {
			dir := t.TempDir()
			h, err := OpenProfiles(dir, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			var result Result
			profilesHost(t, h, "isolated", func(c Command) Result {
				result = Result{ID: c.ID, Tab: 9, OK: true, Value: json.RawMessage(`{"tab":9}`)}
				return result
			})
			policy := profilesPolicy()
			if _, err := h.DoSession(context.Background(), "one", "open", json.RawMessage(`{"url":"https://example.com"}`), policy, "isolated", true); err != nil {
				t.Fatal(err)
			}
			if _, err := h.DoSession(context.Background(), "one", op, nil, policy, "", false); err != nil {
				t.Fatal(err)
			}
			// A lost HTTP acknowledgement makes an older host resend the result
			// without a mode. That must settle without dispatching the action again.
			for _, restart := range []bool{false, true} {
				current := h
				if restart {
					current, err = OpenProfiles(dir, nil, nil)
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := current.Deliver("", []Result{result}); err != nil {
					t.Fatalf("completed %s result retry after restart=%v was refused: %v", op, restart, err)
				}
				if err := current.Deliver("", []Result{{ID: "unknown-result", OK: true}}); !errors.Is(err, WrongMode) {
					t.Fatalf("unknown result was acknowledged: %v", err)
				}
			}
		})
	}
}

func TestLateLegacyResultIsAcknowledgedWithoutRestoringTimedOutPage(t *testing.T) {
	h, err := OpenProfiles(t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.Channel("isolated")
	if err != nil {
		t.Fatal(err)
	}
	probe, cancelProbe := context.WithCancel(context.Background())
	cancelProbe()
	b.Poll(probe, HostInfo{Mode: "isolated", Version: "fixture"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := h.DoSession(ctx, "one", "open", json.RawMessage(`{"url":"https://example.com"}`), profilesPolicy(), "isolated", true)
		done <- err
	}()
	commands := b.Poll(context.Background(), HostInfo{Mode: "isolated", Version: "fixture"})
	if len(commands) != 1 {
		t.Fatalf("expected one admitted command, got %d", len(commands))
	}
	cancel()
	if err := <-done; !errors.Is(err, TimedOut) {
		t.Fatalf("caller did not time out: %v", err)
	}
	if err := h.Deliver("", []Result{{ID: commands[0].ID, Tab: 9, OK: true, Value: json.RawMessage(`{"tab":9}`)}}); err != nil {
		t.Fatalf("late legacy result was not acknowledged: %v", err)
	}
	_, status, _ := h.SelectedStatus("one", profilesPolicy())
	if len(status.Tabs) != 0 || status.Pending != 0 {
		t.Fatalf("late content restored an unconfirmed page: %+v", status)
	}
}
