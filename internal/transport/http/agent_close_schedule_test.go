package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
)

func working(terminal string) contract.CloseReason {
	return contract.CloseReason{Code: "terminal_working", Kind: "obligation", SubjectKind: "session",
		SubjectID: terminal, Mover: contract.CloseMover{Kind: "session", Self: true}}
}

func blockedAudit(terminal, authority string, reasons ...contract.CloseReason) agentCloseAudit {
	return agentCloseAudit{TerminalID: terminal, State: string(contract.CloseabilityStateBlocked),
		Reasons: reasons, Authority: authority}
}

func TestOnlyThisTurnIsTheTurnAlone(t *testing.T) {
	if !onlyThisTurn(blockedAudit("%84", "self", working("%84"))) {
		t.Fatal("self, only its own turn: should schedule")
	}
	other := contract.CloseReason{Code: "board_item_open", Kind: "obligation", SubjectID: "w1"}
	if onlyThisTurn(blockedAudit("%84", "self", working("%84"), other)) {
		t.Fatal("self with another blocker: must stay close_blocked")
	}
	if onlyThisTurn(blockedAudit("%84", "epic_owner", working("%84"))) {
		t.Fatal("an Epic owner closes only an idle Session: never scheduled")
	}
	if onlyThisTurn(blockedAudit("%84", "self", working("%85"))) {
		t.Fatal("another terminal's turn is not this one")
	}
	if onlyThisTurn(agentCloseAudit{TerminalID: "%84", State: "unknown", Authority: "self",
		Reasons: []contract.CloseReason{working("%84")}}) {
		t.Fatal("unknown is never scheduled")
	}
}

func TestARepeatedScheduleIsOneEntry(t *testing.T) {
	now := time.Unix(1000, 0)
	c := &closeSchedule{now: func() time.Time { return now }}
	first, added, err := c.add("%84", "conv", "local")
	if err != nil || !added {
		t.Fatalf("first: %v %v", added, err)
	}
	now = now.Add(time.Minute)
	again, added, err := c.add("%84", "conv", "local")
	if err != nil || added || again != first || c.count() != 1 {
		t.Fatalf("repeat: %+v added=%v err=%v count=%d", again, added, err, c.count())
	}
}

func TestTheScheduleRefusesAtItsCap(t *testing.T) {
	c := &closeSchedule{}
	for i := 0; i < maxScheduledCloses; i++ {
		if _, _, err := c.add(fmt.Sprintf("%%%d", i), "conv", "local"); err != nil {
			t.Fatalf("%d: %v", i, err)
		}
	}
	if _, _, err := c.add("%99", "conv", "local"); !errors.Is(err, errCloseScheduleFull) {
		t.Fatalf("past the cap: %v", err)
	}
	// A repeat of one already waiting is still answered.
	if _, _, err := c.add("%0", "conv", "local"); err != nil {
		t.Fatalf("repeat at the cap: %v", err)
	}
}

// sweepFake is the server's side of a sweep: an audit the test sets, and
// what was closed and recorded.
type sweepFake struct {
	audit    agentCloseAudit
	refusal  *agentCloseRefusal
	closed   []string
	recorded []string
	why      []string
}

func (f *sweepFake) do() closeSweep {
	return closeSweep{
		audit: func(context.Context, string, string) (agentCloseAudit, *agentCloseRefusal) {
			return f.audit, f.refusal
		},
		close: func(_ context.Context, sc scheduledClose) error {
			f.closed = append(f.closed, sc.Terminal)
			return nil
		},
		record: func(_ context.Context, kind, terminal string, payload map[string]any) {
			f.recorded = append(f.recorded, kind)
			if why, ok := payload["why"].(string); ok {
				f.why = append(f.why, why)
			}
		},
	}
}

func scheduled(t *testing.T, now *time.Time) *closeSchedule {
	t.Helper()
	c := &closeSchedule{now: func() time.Time { return *now }}
	if _, _, err := c.add("%84", "conv", "local"); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTheSweepClosesOnceTheTurnEndsAndItIsSafe(t *testing.T) {
	now := time.Unix(1000, 0)
	c := scheduled(t, &now)
	f := &sweepFake{audit: blockedAudit("%84", "self", working("%84"))}
	c.sweep(context.Background(), f.do())
	if len(f.closed) != 0 || c.count() != 1 {
		t.Fatalf("still working: closed %v, waiting %d", f.closed, c.count())
	}
	f.audit = agentCloseAudit{TerminalID: "%84", State: string(contract.CloseabilityStateSafe), Authority: "self"}
	c.sweep(context.Background(), f.do())
	c.sweep(context.Background(), f.do())
	if len(f.closed) != 1 || c.count() != 0 || len(f.recorded) != 1 || f.recorded[0] != "session.closed" {
		t.Fatalf("idle and safe: closed %v, waiting %d, recorded %v", f.closed, c.count(), f.recorded)
	}
}

func TestTheSweepDropsWhatIsNowBlocked(t *testing.T) {
	now := time.Unix(1000, 0)
	c := scheduled(t, &now)
	f := &sweepFake{audit: blockedAudit("%84", "self",
		contract.CloseReason{Code: "completion_unacknowledged", Kind: "obligation", SubjectID: "t1"})}
	c.sweep(context.Background(), f.do())
	if len(f.closed) != 0 || c.count() != 0 || len(f.recorded) != 1 ||
		f.recorded[0] != "session.close_schedule_dropped" || f.why[0] != "blocked" {
		t.Fatalf("now blocked: closed %v, waiting %d, recorded %v %v", f.closed, c.count(), f.recorded, f.why)
	}
}

func TestTheSweepDropsAnExpiredTurn(t *testing.T) {
	now := time.Unix(1000, 0)
	c := scheduled(t, &now)
	f := &sweepFake{audit: blockedAudit("%84", "self", working("%84"))}
	now = now.Add(maxScheduledCloseWait - time.Second)
	c.sweep(context.Background(), f.do())
	if c.count() != 1 {
		t.Fatal("dropped before its expiry")
	}
	now = now.Add(time.Second)
	c.sweep(context.Background(), f.do())
	if len(f.closed) != 0 || c.count() != 0 || len(f.why) != 1 || f.why[0] != "expired" {
		t.Fatalf("expired: closed %v, waiting %d, why %v", f.closed, c.count(), f.why)
	}
}

func TestTheSweepDropsAPaneThatHoldsAnotherConversation(t *testing.T) {
	now := time.Unix(1000, 0)
	c := scheduled(t, &now)
	f := &sweepFake{refusal: &agentCloseRefusal{http.StatusForbidden, "close_not_yours", "not yours"}}
	c.sweep(context.Background(), f.do())
	if len(f.closed) != 0 || c.count() != 0 || len(f.why) != 1 || f.why[0] != "conversation_changed" {
		t.Fatalf("another conversation: closed %v, waiting %d, why %v", f.closed, c.count(), f.why)
	}
}

func TestTheSweepWaitsOutAnUnreadInventory(t *testing.T) {
	now := time.Unix(1000, 0)
	c := scheduled(t, &now)
	f := &sweepFake{refusal: &agentCloseRefusal{http.StatusConflict, "close_inventory_unavailable", "later"}}
	c.sweep(context.Background(), f.do())
	if c.count() != 1 || len(f.recorded) != 0 {
		t.Fatalf("unread: waiting %d, recorded %v", c.count(), f.recorded)
	}
}

// Through the route: a working Session whose evidence is also unreadable is
// unknown, and an unknown close is refused, never scheduled.
func TestAnUnknownOwnCloseIsRefusedNotScheduled(t *testing.T) {
	s, _ := ownTodoServer(t)
	status, code, body := agentCloseCode(t, s, http.MethodPost, "/v1/work/v2/agent/sessions/pane-4/close",
		`{"session_id":"10000000-0000-4000-8000-000000000004"}`)
	if status != http.StatusConflict || code != "closeability_unknown" || s.closes.count() != 0 {
		t.Fatalf("unknown: %d %s %s, waiting %d", status, code, body, s.closes.count())
	}
}
