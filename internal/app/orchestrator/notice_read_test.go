package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// Read is not handled. On 2026-10-02 a root that put off `clawdline task ack`
// until it had integrated its child was typed the same task_finished again on
// every rung of the acknowledgement ladder, each copy a turn over its whole
// context. Once the root's own record shows the notice handed to its model, it
// is never typed again; the ACK is still the receipt, and is still taken.
func TestANoticeTheRootHasReadIsNotTypedAgain(t *testing.T) {
	b, ctx := newTestBroker(t)
	start := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	now := start
	b.Clock = func() time.Time { return now }
	typed := 0
	b.Type = func(context.Context, string, string) error { typed++; return nil }
	read := false
	asked := 0
	b.NoticeRead = func(_ context.Context, root session.Session, noticeID string) (bool, error) {
		asked++
		if root.ConversationID != rootConversation {
			t.Fatalf("asked about %q, not the root", root.ConversationID)
		}
		return read, nil
	}
	r := finished(t, b, ctx, "f1e00000-0000-4000-8000-000000000001")

	b.PumpNotices(ctx)
	if typed != 1 || asked != 0 {
		t.Fatalf("the first delivery typed %d lines and asked %d times; a notice never typed cannot have been read", typed, asked)
	}
	read = true
	for _, elapsed := range []time.Duration{ackWaitBase, ackWaitBase + 4*time.Minute, 3 * time.Hour} {
		now = start.Add(elapsed)
		b.PumpNotices(ctx)
	}
	if typed != 1 {
		t.Fatalf("a notice the root had read was typed %d times", typed)
	}
	after, _, _ := b.Record(ctx, r.ID)
	n := after.Notice
	if n.State != NoticeDelivered || n.ObservedAt.IsZero() || !n.AcknowledgedAt.IsZero() || !n.NextRetryAt.IsZero() {
		t.Fatalf("read is recorded as observed and nothing else: %+v", n)
	}
	if after.Landing != nil && after.Landing.State != LandingPending {
		t.Fatalf("reading the notice settled the landing: %+v", after.Landing)
	}
	if n.Attempts != 1 {
		t.Fatalf("observing spent attempts: %d", n.Attempts)
	}
	// The receipt still closes it.
	if changed, err := b.Acknowledge(ctx, r.ID, n.ID); err != nil || !changed {
		t.Fatalf("an ACK after the read: %v %v", changed, err)
	}
	after, _, _ = b.Record(ctx, r.ID)
	if after.Notice.State != NoticeAcknowledged || !after.Notice.ObservedAt.Equal(n.ObservedAt) {
		t.Fatalf("the ACK lost when it was read: %+v", after.Notice)
	}
}

// A record that could not be read is not a record that says "read". Every way
// of not knowing types the notice again, as before this rung existed.
func TestANoticeWhoseReadCannotBeCheckedIsTypedAgain(t *testing.T) {
	for _, failure := range []error{errors.New("record unreadable"), nil} {
		b, ctx := newTestBroker(t)
		start := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
		now := start
		b.Clock = func() time.Time { return now }
		typed := 0
		b.Type = func(context.Context, string, string) error { typed++; return nil }
		b.NoticeRead = func(context.Context, session.Session, string) (bool, error) { return false, failure }
		finished(t, b, ctx, "f1e00000-0000-4000-8000-000000000002")
		b.PumpNotices(ctx)
		now = start.Add(ackWaitBase)
		b.PumpNotices(ctx)
		if typed != 2 {
			t.Fatalf("with %v: typed %d times, want 2", failure, typed)
		}
	}
}

// Nothing about "not read yet" lives in memory: a daemon restarted between two
// rungs types an unread notice again on the rung, and one already read stays
// silent. A broker over the same store directory is the restart — the store is
// reopened, and the in-memory deferrals are new.
func TestAnUnreadNoticeIsStillTypedAfterARestart(t *testing.T) {
	b, ctx := newTestBroker(t)
	start := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	now := start
	b.Clock = func() time.Time { return now }
	b.Type = func(context.Context, string, string) error { return nil }
	unread := finished(t, b, ctx, "f1e00000-0000-4000-8000-000000000003")
	seen := finished(t, b, ctx, "f1e00000-0000-4000-8000-000000000004")
	b.PumpNotices(ctx)
	// The second one is read before the daemon stops.
	b.NoticeRead = func(_ context.Context, _ session.Session, id string) (bool, error) { return id == seen.Notice.ID, nil }
	now = start.Add(ackWaitBase)
	b.PumpNotices(ctx)
	if err := b.Store.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := store.Open(b.Dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	typedFor := map[string]int{}
	restarted := &Broker{Store: st, Tasks: taskdir.New(b.Dir), Git: b.Git, Dir: b.Dir, Live: b.Live,
		Clock: func() time.Time { return now },
		NoticeRead: func(context.Context, session.Session, string) (bool, error) {
			return false, nil
		},
	}
	restarted.Type = func(_ context.Context, _ string, text string) error {
		for _, id := range []string{unread.Notice.ID, seen.Notice.ID} {
			if containsID(text, id) {
				typedFor[id]++
			}
		}
		return nil
	}
	now = start.Add(ackWaitBase + 4*time.Minute)
	restarted.PumpNotices(ctx)
	if typedFor[unread.Notice.ID] != 1 {
		t.Fatalf("the unread notice was typed %d times after the restart, want 1", typedFor[unread.Notice.ID])
	}
	if typedFor[seen.Notice.ID] != 0 {
		t.Fatalf("the read notice was typed %d times after the restart", typedFor[seen.Notice.ID])
	}
	after, _, _ := restarted.Record(ctx, seen.ID)
	if after.Notice.ObservedAt.IsZero() {
		t.Fatalf("the restart forgot the read: %+v", after.Notice)
	}
}

func containsID(text, id string) bool { return strings.Contains(text, id) }
