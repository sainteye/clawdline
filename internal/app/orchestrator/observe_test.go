package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/taskdir"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

func exitGoroutine() { runtime.Goexit() }

func newTestBrokerTasks(dir string) taskdir.Root { return taskdir.New(dir) }

// waitFor polls until cond holds or the deadline passes.
func waitFor(t *testing.T, limit time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A beat that panics is recovered, restarted, counted, and leaves a typed
// event behind — and keeps beating afterwards. Before the supervisor, a panic
// in a pass took the daemon down with it; a recover without a count would be
// a loop that recovers quietly, which is the Swift app's "looked alive the
// whole time" in a different shape.
func TestAPanickingBeatIsRecoveredCountedAndRecorded(t *testing.T) {
	restartBase = 10 * time.Millisecond
	t.Cleanup(func() { restartBase = time.Second })
	b, ctx := newTestBroker(t)
	b.Fault = func(pass int64) {
		if pass == 2 {
			panic("boom")
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() { b.Run(ctx, 10*time.Millisecond, nil); close(done) }()
	waitFor(t, 5*time.Second, "a restart and passes after it", func() bool {
		r := b.Beat()
		return r.Restarts == 1 && r.Passes >= 5
	})
	report := b.Beat()
	if len(report.Panics) != 1 || report.Panics[0].Value != "boom" || report.Panics[0].Pass != 2 {
		t.Fatalf("the panic was not kept as it happened: %+v", report.Panics)
	}
	if report.InPass && report.Overlaps > 0 {
		t.Errorf("the abandoned pass was counted as an overlap: %+v", report)
	}
	n, err := b.Store.EventCount(ctx, "broker.beat.panicked")
	if err != nil || n != 1 {
		t.Fatalf("broker.beat.panicked events = %d (err %v), want exactly 1", n, err)
	}
	if stalled, _ := b.Stalled(); stalled {
		t.Error("a beat that recovered is still reported stalled")
	}
	cancel()
	<-done
}

// A beat that stops returning is reported from outside it within three ticks,
// and is not "repaired" by starting a second loop beside it.
func TestAStalledBeatIsReportedAndNotDoubled(t *testing.T) {
	b, ctx := newTestBroker(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	b.Fault = func(pass int64) {
		if pass == 3 {
			<-release
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	tick := 50 * time.Millisecond
	go b.Run(ctx, tick, nil)
	waitFor(t, 2*time.Second, "the third pass to begin", func() bool { return b.Beat().Passes >= 3 })
	if stalled, _ := b.Stalled(); stalled {
		t.Fatal("stalled the moment the pass began; the alarm has no grace")
	}
	waitFor(t, 2*time.Second, "the stall to be reported", func() bool {
		stalled, _ := b.Stalled()
		return stalled
	})
	report := b.Beat()
	if report.Restarts != 0 || report.Passes != 3 || !report.InPass {
		t.Fatalf("a hung pass was restarted or doubled: %+v", report)
	}
	if report.Quiet < tick*stallFactor {
		t.Errorf("quiet %s is under three ticks", report.Quiet)
	}
}

// A beat that exits without being asked (runtime.Goexit) is a death too.
func TestABeatThatExitsIsRestarted(t *testing.T) {
	restartBase = 10 * time.Millisecond
	t.Cleanup(func() { restartBase = time.Second })
	b, ctx := newTestBroker(t)
	var once sync.Once
	b.Fault = func(pass int64) {
		once.Do(func() { exitGoroutine() })
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go b.Run(ctx, 10*time.Millisecond, nil)
	waitFor(t, 5*time.Second, "a restart after the exit", func() bool {
		r := b.Beat()
		return r.Restarts == 1 && r.Passes >= 3
	})
	if n, _ := b.Store.EventCount(ctx, "broker.beat.exited"); n != 1 {
		t.Errorf("broker.beat.exited events = %d, want 1", n)
	}
	if n, _ := b.Store.EventCount(ctx, "broker.beat.panicked"); n != 0 {
		t.Errorf("an exit was recorded as %d panic(s)", n)
	}
	if len(b.Beat().Panics) != 0 {
		t.Error("an exit is listed among the panics")
	}
}

// fiveBriefed stores five children that are running, with no result, no
// progress and a far deadline: a pass has nothing to decide about them, only
// things to observe.
func fiveBriefed(t *testing.T, b *Broker, ctx context.Context) []Record {
	t.Helper()
	out := []Record{}
	for i, id := range []string{
		"a1111111-1111-4111-8111-111111111111", "a2222222-2222-4222-8222-222222222222",
		"a3333333-3333-4333-8333-333333333333", "a4444444-4444-4444-8444-444444444444",
		"a5555555-5555-4555-8555-555555555555",
	} {
		r := Record{
			Protocol: Protocol, ID: id, Assistant: "claude", Title: "t", State: StateBriefed,
			CreatedAt: b.now(), Claims: []string{}, TimeoutMinutes: 240,
			ChildTerminalID: "%" + string(rune('a'+i)),
			Root:            &RootRef{SessionID: rootConversation, Assistant: "claude"},
		}
		if err := b.save(ctx, r, HashSecret("s"), "task.briefed"); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// The B2 criterion (docs/broker-design.md §8): five briefed tasks, a hundred
// passes in which only observations change — the children's session states
// flip, the reading's generation moves, the clock advances — and the store is
// not written once. One of the children also left a progress.json, which is
// taken the first time and only observed after that.
func TestObservationsAreNotWritten(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	tasks := fiveBriefed(t, b, ctx)
	flip := false
	b.Reading = func(context.Context) session.Inventory {
		inv := session.Inventory{Complete: true, Sessions: []session.Session{
			{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation},
		}}
		for _, r := range tasks {
			state := session.StateWorking
			if flip {
				state = session.StateIdle
			}
			inv.Sessions = append(inv.Sessions, session.Session{
				ID: r.ChildTerminalID, Assistant: session.AssistantClaude, State: state,
			})
		}
		return inv
	}
	dir := b.Tasks.Path(tasks[0].ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	note, _ := json.Marshal(map[string]string{"task_secret": "s", "note": "reading the brief"})
	if err := os.WriteFile(filepath.Join(dir, "progress.json"), note, 0o600); err != nil {
		t.Fatal(err)
	}

	first := b.Pass(ctx)
	if first.Notes != 1 {
		t.Fatalf("the first pass took %d notes, want the one progress.json", first.Notes)
	}
	writes := b.Store.Stats().Writes
	changes := b.Store.Health(ctx).Changes
	events, _ := b.Store.EventCount(ctx, "")
	generation := b.Observed().Generation

	for i := 0; i < 100; i++ {
		flip = !flip
		now = now.Add(5 * time.Second)
		b.Pass(ctx)
	}

	if got := b.Store.Stats().Writes; got != writes {
		t.Errorf("observation-only passes committed %d write(s)", got-writes)
	}
	if got := b.Store.Health(ctx).Changes; got != changes {
		t.Errorf("observation-only passes changed %d row(s)", got-changes)
	}
	if got, _ := b.Store.EventCount(ctx, ""); got != events {
		t.Errorf("observation-only passes appended %d event(s)", got-events)
	}
	seen := b.Observed()
	if seen.Generation != generation+100 {
		t.Errorf("generation moved %d, want 100: the passes did not observe", seen.Generation-generation)
	}
	for _, r := range tasks {
		e, ok := seen.Executors[r.ID]
		if !ok || e.Status != ExecutorObserved || !e.ObservedAt.Equal(now) || e.Generation != seen.Generation {
			t.Errorf("task %s executor = %+v, want observed at the last reading", r.ID[:8], e)
		}
	}
}

// Only a conclusion is written: a child unseen by two complete readings a
// minute apart becomes executor_missing, once, as an event and not as a
// record rewrite; staying missing writes nothing more; an incomplete reading
// decides nothing.
func TestAMissingExecutorIsOneEventAndNoRecordWrite(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	tasks := fiveBriefed(t, b, ctx)
	present, complete := true, true
	b.Reading = func(context.Context) session.Inventory {
		inv := session.Inventory{Complete: complete, Sessions: []session.Session{
			{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation},
		}}
		for i, r := range tasks {
			if i == 0 && !present {
				continue
			}
			inv.Sessions = append(inv.Sessions, session.Session{ID: r.ChildTerminalID, Assistant: session.AssistantClaude})
		}
		return inv
	}
	b.Pass(ctx)
	present = false
	complete = false
	now = now.Add(2 * time.Minute)
	b.Pass(ctx)
	if e, _ := b.ExecutorOf(tasks[0].ID); e.Status != ExecutorObserved {
		t.Fatalf("an incomplete reading decided %q", e.Status)
	}
	complete = true
	b.Pass(ctx)
	if e, _ := b.ExecutorOf(tasks[0].ID); e.Status != ExecutorNotSeen {
		t.Fatalf("one complete reading without it gave %q, want not_seen", e.Status)
	}
	writes := b.Store.Stats().Writes
	now = now.Add(61 * time.Second)
	b.Pass(ctx)
	if e, _ := b.ExecutorOf(tasks[0].ID); e.Status != ExecutorMissing {
		t.Fatalf("two complete readings a minute apart gave %q", e.Status)
	}
	for i := 0; i < 10; i++ {
		now = now.Add(5 * time.Second)
		b.Pass(ctx)
	}
	if n, _ := b.Store.EventCount(ctx, "task.executor."+ExecutorMissing); n != 1 {
		t.Errorf("executor_missing events = %d, want 1", n)
	}
	if got := b.Store.Stats().Writes - writes; got != 1 {
		t.Errorf("reaching and holding executor_missing cost %d writes, want the one event", got)
	}
	after, _, _ := b.Record(ctx, tasks[0].ID)
	if after.State != StateBriefed {
		t.Errorf("an observation settled the task: %q", after.State)
	}
}

// The B3 criterion: the root's tab goes away, the notice waits it out without
// typing anything, the daemon restarts in the meantime, the root comes back
// in a different tab, and the notice is typed there exactly once — and not
// again after the ACK, however long the clock runs.
func TestANoticeOutlivesAMissingRootAndARestartAndIsTypedOnce(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	rootHere := true
	rootTab := "%1"
	live := func(context.Context) []session.Session {
		if !rootHere {
			return []session.Session{{ID: "%5", Assistant: session.AssistantClaude, ConversationID: "someone-else"}}
		}
		return []session.Session{{ID: rootTab, Assistant: session.AssistantClaude, ConversationID: rootConversation}}
	}
	typed := map[string]int{}
	typer := func(_ context.Context, terminal, _ string) error { typed[terminal]++; return nil }
	b := &Broker{Store: st, Tasks: newTestBrokerTasks(dir), Dir: dir, Live: live, Clock: clock, Type: typer}
	id := "b1111111-1111-4111-8111-111111111111"
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: "t", State: StateBriefed,
		CreatedAt: now, Claims: []string{}, Root: &RootRef{SessionID: rootConversation, Assistant: "claude"}}
	if err := b.save(context.Background(), r, HashSecret("s"), "task.queued"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	rootHere = false
	settled, err := b.Settle(ctx, id, StateSuccess, "done", nil)
	if err != nil || settled.Notice == nil {
		t.Fatalf("settle: %v %+v", err, settled.Notice)
	}
	for i := 0; i < 3; i++ {
		b.PumpNotices(ctx)
		now = now.Add(time.Minute)
	}
	if len(typed) != 0 {
		t.Fatalf("typed %v while the root was gone", typed)
	}
	mid, _, _ := b.Record(ctx, id)
	if mid.Notice.Attempts != 3 || mid.Notice.LastError == nil || mid.Notice.LastError.Code != "root_missing" {
		t.Fatalf("a missing root was not recorded as three root_missing attempts: %+v", mid.Notice)
	}

	// The daemon restarts: a new broker over the same directory.
	_ = st.Close()
	st, err = store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b = &Broker{Store: st, Tasks: newTestBrokerTasks(dir), Dir: dir, Live: live, Clock: clock, Type: typer}

	rootHere, rootTab = true, "%7"
	now = now.Add(5 * time.Minute)
	for i := 0; i < 4; i++ {
		b.PumpNotices(ctx)
		now = now.Add(time.Second)
	}
	if typed["%7"] != 1 || len(typed) != 1 {
		t.Fatalf("typed %v after the root came back, want exactly once into %%7", typed)
	}
	delivered, _, _ := b.Record(ctx, id)
	if delivered.Notice.State != NoticeDelivered || delivered.Notice.Recipient != "%7" ||
		delivered.Notice.ID != settled.Notice.ID {
		t.Fatalf("after delivery the notice is %+v", delivered.Notice)
	}
	if changed, err := b.Acknowledge(ctx, id, settled.Notice.ID); err != nil || !changed {
		t.Fatalf("ack: changed=%v err=%v", changed, err)
	}
	if changed, err := b.Acknowledge(ctx, id, settled.Notice.ID); err != nil || changed {
		t.Fatalf("a second ack changed something: changed=%v err=%v", changed, err)
	}
	now = now.Add(time.Hour)
	b.PumpNotices(ctx)
	if typed["%7"] != 1 {
		t.Fatalf("typed %d times in all; an acknowledged notice was sent again", typed["%7"])
	}
	if n, _ := st.EventCount(ctx, "task.completion.delivered"); n != 1 {
		t.Errorf("delivered events = %d, want 1", n)
	}
}

// The attempt that uses the last of the budget ends the sequence then, as in
// the Swift app, rather than one rung later.
func TestTheLastFailedAttemptDeadLetters(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	b.Live = func(context.Context) []session.Session { return nil }
	r := finished(t, b, ctx, "c1111111-1111-4111-8111-111111111111")
	for i := 0; i < AttemptLimit; i++ {
		b.PumpNotices(ctx)
		now = now.Add(retryCeiling + time.Second)
	}
	after, _, _ := b.Record(ctx, r.ID)
	if after.Notice.State != NoticeDeadLetter || after.Notice.Attempts != AttemptLimit {
		t.Fatalf("after %d failures the notice is %+v", AttemptLimit, after.Notice)
	}
}

// A record rewrite may open a notice but never change one: that is the
// ledger's, and a rewrite carrying an edited notice is the lost update the
// ledger exists to rule out.
func TestARecordRewriteCannotMoveANotice(t *testing.T) {
	b, ctx := newTestBroker(t)
	r := finished(t, b, ctx, "d1111111-1111-4111-8111-111111111111")
	_, err := b.mutate(ctx, r.ID, "task.test", func(now *Record) error {
		now.Notice.State = NoticeAcknowledged
		return nil
	})
	if !errors.Is(err, errNoticeOutsideLedger) {
		t.Fatalf("a rewrite that moved a notice was accepted (err=%v)", err)
	}
	after, _, _ := b.Record(ctx, r.ID)
	if after.Notice.State != NoticePending {
		t.Errorf("notice is %q", after.Notice.State)
	}
}

// A store written by the first broker wave kept the notice inside the task's
// record. Opening it moves the notice to the ledger — same id, same state —
// and takes it out of the record, and the ACK the root was told to send still
// works.
func TestANoticeInsideAnOldRecordMovesToTheLedger(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	id := "e1111111-1111-4111-8111-111111111111"
	legacy := map[string]any{
		"clawdline_protocol": 1, "task_id": id, "assistant": "claude", "state": "success",
		"created_at": "2026-09-18T08:00:00Z", "claims": []string{},
		"root": map[string]any{"session_id": rootConversation, "assistant": "claude"},
		"notice": map[string]any{
			"notice_id": "0a1b2c3d-0000-4000-a000-000000000001", "state": "delivered", "attempts": 2,
			"created_at": "2026-09-18T08:00:00Z", "next_retry_at": "2026-09-18T08:00:10Z",
			"transport_delivered_at": "2026-09-18T08:00:05Z", "recipient": "%3",
		},
	}
	raw, _ := json.Marshal(legacy)
	if err := st.SaveBrokerTask(context.Background(), store.BrokerRow{
		ID: id, Project: "/p", Assistant: "claude", State: "success", CreatedAt: time.Now(),
		SecretHash: HashSecret("s"), Record: raw,
	}, nil); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	st, err = store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b := &Broker{Store: st, Tasks: newTestBrokerTasks(dir), Dir: dir}
	ctx := context.Background()
	r, _, err := b.Record(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if r.Notice == nil || r.Notice.ID != "0a1b2c3d-0000-4000-a000-000000000001" ||
		r.Notice.State != NoticeDelivered || r.Notice.Attempts != 2 || r.Notice.Recipient != "%3" {
		t.Fatalf("migrated notice = %+v", r.Notice)
	}
	row, _ := st.BrokerTask(ctx, id)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(row.Record, &fields)
	if _, still := fields["notice"]; still {
		t.Error("the notice is still inside the record")
	}
	if changed, err := b.Acknowledge(ctx, id, "0a1b2c3d-0000-4000-a000-000000000001"); err != nil || !changed {
		t.Fatalf("the ACK for a migrated notice: changed=%v err=%v", changed, err)
	}
}

// A note is handed to a stream only once it is stored, with its row as its
// identity.
func TestAnAcceptedNoteReachesTheStream(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "f1111111-1111-4111-8111-111111111111"
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", State: StateBriefed,
		CreatedAt: time.Now(), Claims: []string{}}
	if err := b.save(ctx, r, HashSecret("s"), "task.briefed"); err != nil {
		t.Fatal(err)
	}
	notes, cancel := b.SubscribeProgress()
	defer cancel()
	if _, err := b.Progress(ctx, id, "s", "halfway"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Progress(ctx, id, "s", "halfway"); err != nil {
		t.Fatal(err)
	}
	select {
	case n := <-notes:
		if n.TaskID != id || n.Note != "halfway" || n.Seq == 0 {
			t.Fatalf("stream got %+v", n)
		}
	case <-time.After(time.Second):
		t.Fatal("the note never reached the stream")
	}
	select {
	case n := <-notes:
		t.Fatalf("a repeated sentence was streamed again: %+v", n)
	default:
	}
	recent, err := b.RecentProgress(ctx)
	if err != nil || len(recent) != 1 {
		t.Fatalf("recent = %v (err %v)", recent, err)
	}
}

// The four-minute clock's rules — "a reading with nothing in it decides
// nothing", and D05 ③'s replacement for "any reading that saw a terminal may
// decide" — are TestTheSpawnClockAsksTheTabsOwnSource (w1_test.go).
