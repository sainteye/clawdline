package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func nativeFinished(t *testing.T, b *Broker, ctx context.Context, row *session.Session) Record {
	t.Helper()
	b.Live = func(context.Context) []session.Session { return []session.Session{*row} }
	b.Screen = func(context.Context, string) (string, bool) {
		t.Fatal("native notice read a terminal screen")
		return "", false
	}
	r := Record{Protocol: Protocol, ID: "c6f30000-0000-4000-8000-000000000099", Assistant: "antigravity", Title: "Synthetic native result", State: StateBriefed,
		CreatedAt: time.Now(), Claims: []string{}, Root: &RootRef{SessionID: row.ConversationID, Assistant: "antigravity"}}
	if err := b.save(ctx, r, HashSecret("s"), "task.queued"); err != nil {
		t.Fatal(err)
	}
	r, err := b.Settle(ctx, r.ID, StateSuccess, "done", nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestNativeNoticeHoldsBusyAndStopsAfterObservedTurn(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Now()
	b.Clock = func() time.Time { return now }
	row := ownedFixture(b.Dir).Row
	row.State = session.StateWorking
	r := nativeFinished(t, b, ctx, &row)
	sends := 0
	b.Type = func(context.Context, string, string) error { sends++; return nil }
	b.PumpNotices(ctx)
	held, _, _ := b.Record(ctx, r.ID)
	if sends != 0 || held.Notice.Attempts != 0 {
		t.Fatal("busy native root was prompted")
	}
	row.State = session.StateIdle
	row.Menu = &session.Menu{}
	now = now.Add(noticeHoldStep + time.Second)
	b.PumpNotices(ctx)
	if sends != 0 {
		t.Fatal("permission menu received a model input")
	}
	row.Menu = nil
	now = now.Add(noticeHoldStep + time.Second)
	b.PumpNotices(ctx)
	if sends != 1 {
		t.Fatal("idle native root was not prompted once")
	}
	b.NoticeRead = func(_ context.Context, target session.Session, id string) (bool, error) {
		if target.ID != row.ID || id != r.Notice.ID {
			t.Fatal("wrong native notice binding")
		}
		return true, nil
	}
	now = now.Add(time.Hour)
	b.PumpNotices(ctx)
	observed, _, _ := b.Record(ctx, r.ID)
	if sends != 1 || observed.Notice.ObservedAt.IsZero() {
		t.Fatal("observed native turn prompted again")
	}
}

func TestNativeNoticeUncertainWriteNeverAutomaticallyRetries(t *testing.T) {
	for _, unsent := range []bool{false, true} {
		t.Run(map[bool]string{false: "possibly-written", true: "definitely-unsent"}[unsent], func(t *testing.T) {
			b, ctx := newTestBroker(t)
			now := time.Now()
			b.Clock = func() time.Time { return now }
			row := ownedFixture(b.Dir).Row
			r := nativeFinished(t, b, ctx, &row)
			sends := 0
			b.Type = func(context.Context, string, string) error {
				sends++
				if unsent {
					return terminal.Unsent{Why: "before write"}
				}
				return errors.New("lost native response")
			}
			b.PumpNotices(ctx)
			first, _, _ := b.Record(ctx, r.ID)
			if unsent {
				if first.Notice.State == NoticeDeadLetter {
					t.Fatal("known unsent lost retry")
				}
				now = first.Notice.NextRetryAt.Add(time.Second)
				b.Type = func(context.Context, string, string) error { sends++; return nil }
				b.PumpNotices(ctx)
				if sends != 2 {
					t.Fatal("known-unsent notice did not keep its retry")
				}
				return
			}
			if first.Notice.State != NoticeDeadLetter || first.Notice.LastError.Code != "native_delivery_uncertain" {
				t.Fatalf("uncertain=%+v", first.Notice)
			}
			now = now.Add(24 * time.Hour)
			b.PumpNotices(ctx)
			b.retypeIfIdle(ctx, first, []session.Session{row}, now)
			if sends != 1 {
				t.Fatalf("uncertain prompt sent %d times", sends)
			}
		})
	}
}
