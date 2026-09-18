package orchestrator

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// W5: the coordination plane. Every test here names the failure it closes and
// has a control that makes the same check red (DG-8).

const (
	askA = "a0000000-0000-4000-8000-00000000000a"
	askB = "b0000000-0000-4000-8000-00000000000b"
	askC = "c0000000-0000-4000-8000-00000000000c"
)

func ask(t *testing.T, b *Broker, ctx context.Context, req LeaseRequest) LeaseAnswer {
	t.Helper()
	if req.Holder == "" {
		req.Holder = "test " + req.RequestID[:1]
	}
	a, err := b.Acquire(ctx, req)
	if err != nil {
		t.Fatalf("acquire %s: %v", req.RequestID[:1], err)
	}
	return a
}

// cutover A10: a second verification asked for while the first holds the slot
// queues — it is not granted beside it — and gets the slot once the first
// gives it back.
func TestTheCompileSlotQueuesTheSecondAsker(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }

	first := ask(t, b, ctx, LeaseRequest{Resource: ResourceCompile, RequestID: askA})
	if first.State != "granted" {
		t.Fatalf("the first ask on a free slot: %+v", first)
	}
	second := ask(t, b, ctx, LeaseRequest{Resource: ResourceCompile, RequestID: askB})
	if second.State != "queued" || second.Position != 1 || second.HoldReason != "holder_proving" {
		t.Fatalf("the second ask while the slot is held was %+v, want queued at 1", second)
	}
	// Still queued while the holder renews, however long the build takes.
	for i := 0; i < 10; i++ {
		now = now.Add(20 * time.Second)
		if _, err := b.Renew(ctx, LeaseOwner{Resource: ResourceCompile, RequestID: askA}); err != nil {
			t.Fatalf("renew: %v", err)
		}
		if again := ask(t, b, ctx, LeaseRequest{Resource: ResourceCompile, RequestID: askB}); again.State != "queued" {
			t.Fatalf("after %s of a renewing holder the waiter was %+v", time.Duration(i+1)*20*time.Second, again)
		}
	}
	// Somebody who is not the holder cannot release it.
	if _, err := b.Release(ctx, LeaseOwner{Resource: ResourceCompile, RequestID: askC}); refusalCode(err) != "not_holder" {
		t.Fatalf("a stranger's release: %v", err)
	}
	if _, err := b.Release(ctx, LeaseOwner{Resource: ResourceCompile, RequestID: askA}); err != nil {
		t.Fatal(err)
	}
	if got := ask(t, b, ctx, LeaseRequest{Resource: ResourceCompile, RequestID: askB}); got.State != "granted" {
		t.Fatalf("the waiter after the release: %+v", got)
	}
	// The old holder's renewal is refused by name: it does not hold it.
	if _, err := b.Renew(ctx, LeaseOwner{Resource: ResourceCompile, RequestID: askA}); refusalCode(err) != "lease_lost" {
		t.Fatalf("a renewal after release: %v", err)
	}
}

// 15924b14, 24a33139, O9: a holder whose renewal lapsed keeps the slot while
// its process is there, keeps it while nobody can say, and loses it only when
// the kernel says the process is gone — here, a different process now has
// its pid.
func TestAHolderIsReplacedOnlyOnPositiveEvidence(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	started := time.Unix(1_800_000_000, 0)
	kernel := started // what the kernel says the pid's start is
	b.ProcessStart = func(int) time.Time { return kernel }

	ask(t, b, ctx, LeaseRequest{Resource: ResourceCompile, RequestID: askA, PID: os.Getpid(), ProcessStart: started})
	now = now.Add(10 * time.Minute) // no renewal at all: a clock on the work would have expired it
	if got := ask(t, b, ctx, LeaseRequest{Resource: ResourceCompile, RequestID: askB}); got.State != "queued" {
		t.Fatalf("a lapsed renewal with the process running: %+v", got)
	}
	kernel = time.Time{} // the start time cannot be read
	got := ask(t, b, ctx, LeaseRequest{Resource: ResourceCompile, RequestID: askB})
	if got.State != "queued" || got.HoldReason != "evidence_unknown" {
		t.Fatalf("an unreadable process is not a gone one: %+v", got)
	}
	kernel = started.Add(time.Hour) // the pid now belongs to a later process
	if got := ask(t, b, ctx, LeaseRequest{Resource: ResourceCompile, RequestID: askB}); got.State != "granted" {
		t.Fatalf("a holder whose process is gone still held the slot: %+v", got)
	}
	// Control: the same lapse with no proof named at all is the renewal's own
	// word — the heartbeat-only holder is replaced.
	b2, ctx2 := newTestBroker(t)
	b2.Clock = b.Clock
	ask(t, b2, ctx2, LeaseRequest{Resource: ResourceCompile, RequestID: askA})
	now = now.Add(2 * time.Minute)
	if got := ask(t, b2, ctx2, LeaseRequest{Resource: ResourceCompile, RequestID: askB}); got.State != "granted" {
		t.Fatalf("a heartbeat-only holder that stopped renewing: %+v", got)
	}
}

// b2f25048: a waiter that stopped asking does not hold everybody behind it
// while the slot is free.
func TestASilentWaiterAtTheHeadIsPassedOver(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	ask(t, b, ctx, LeaseRequest{Resource: ResourceCompile, RequestID: askA})
	ask(t, b, ctx, LeaseRequest{Resource: ResourceCompile, RequestID: askB}) // then never asks again
	now = now.Add(10 * time.Second)
	if got := ask(t, b, ctx, LeaseRequest{Resource: ResourceCompile, RequestID: askC}); got.Position != 2 {
		t.Fatalf("behind a waiter that is still asking, C is %+v", got)
	}
	if _, err := b.Release(ctx, LeaseOwner{Resource: ResourceCompile, RequestID: askA}); err != nil {
		t.Fatal(err)
	}
	// Control: while B is still inside its window, C is not granted over it.
	if got := ask(t, b, ctx, LeaseRequest{Resource: ResourceCompile, RequestID: askC}); got.State != "queued" {
		t.Fatalf("C jumped a waiter that was still asking: %+v", got)
	}
	now = now.Add(waiterSilence + time.Second)
	if got := ask(t, b, ctx, LeaseRequest{Resource: ResourceCompile, RequestID: askC}); got.State != "granted" {
		t.Fatalf("a silent head held C back from a free slot: %+v", got)
	}
}

// D20: two roots landing into the same checkout — the second does not get the
// lease; a different checkout is a different lease.
func TestTheLandingLeaseIsOneCheckoutsAndHoldsASecondRootBack(t *testing.T) {
	b, ctx := newTestBroker(t)
	repoX, repoY := t.TempDir(), t.TempDir()
	root1, root2 := rootConversation, "4b9b8c2e-0f5a-4c43-9a8f-2d9d1c7f6e01"
	if got := ask(t, b, ctx, LeaseRequest{Resource: ResourceLanding, Checkout: repoX, RequestID: askA, Session: root1}); got.State != "granted" {
		t.Fatalf("root 1: %+v", got)
	}
	got := ask(t, b, ctx, LeaseRequest{Resource: ResourceLanding, Checkout: repoX, RequestID: askB, Session: root2})
	if got.State != "queued" || got.View.Holder == nil || got.View.Holder.Session != root1 {
		t.Fatalf("root 2 on the same checkout: %+v", got)
	}
	if other := ask(t, b, ctx, LeaseRequest{Resource: ResourceLanding, Checkout: repoY, RequestID: askC, Session: root2}); other.State != "granted" {
		t.Fatalf("another checkout: %+v", other)
	}
	// A session named by its terminal is refused by name (#36).
	_, err := b.Acquire(ctx, LeaseRequest{Resource: ResourceLanding, Checkout: repoX, RequestID: askC, Holder: "x", Session: "%1"})
	if refusalCode(err) != "session_id_is_terminal" {
		t.Fatalf("a terminal id as a session: %v", err)
	}
	// A relative checkout is not a checkout.
	if _, err := b.Acquire(ctx, LeaseRequest{Resource: ResourceLanding, Checkout: "repo", RequestID: askC, Holder: "x"}); refusalCode(err) != "bad_lease" {
		t.Fatalf("a relative checkout: %v", err)
	}
}

// The line has a ceiling, and a full line says so to the asker.
func TestAFullLineRefusesTheNextAsker(t *testing.T) {
	b, ctx := newTestBroker(t)
	ask(t, b, ctx, LeaseRequest{Resource: ResourceCompile, RequestID: askA})
	for i := 0; i < LeaseQueueLimit; i++ {
		id := fmt.Sprintf("d0000000-0000-4000-8000-%012x", i)
		if got := ask(t, b, ctx, LeaseRequest{Resource: ResourceCompile, RequestID: id}); got.State != "queued" {
			t.Fatalf("waiter %d: %+v", i, got)
		}
	}
	_, err := b.Acquire(ctx, LeaseRequest{Resource: ResourceCompile, RequestID: askC, Holder: "one too many"})
	if refusalCode(err) != "queue_full" {
		t.Fatalf("past the limit: %v", err)
	}
}

// D24: a notice that goes to dead letter pushes the person once — and not
// before, and not again on the next passes.
func TestADeadLetterPushesThePersonOnce(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	b.Live = func(context.Context) []session.Session { return nil }
	type push struct{ title, body, terminal, tag string }
	var pushes []push
	var mu sync.Mutex
	done := make(chan struct{}, 4)
	b.pushed = func() { done <- struct{}{} }
	b.Push = func(_ context.Context, title, body, terminal, tag string) (int, int, error) {
		mu.Lock()
		defer mu.Unlock()
		pushes = append(pushes, push{title, body, terminal, tag})
		return 1, 0, nil
	}
	r := finished(t, b, ctx, "e1111111-1111-4111-8111-111111111111")
	for i := 0; i < AttemptLimit-1; i++ {
		b.PumpNotices(ctx)
		now = now.Add(retryCeiling + time.Second)
	}
	if len(pushes) != 0 {
		t.Fatalf("pushed before the dead letter: %+v", pushes)
	}
	for i := 0; i < 3; i++ {
		b.PumpNotices(ctx)
		now = now.Add(retryCeiling + time.Second)
	}
	after, _, _ := b.Record(ctx, r.ID)
	if after.Notice.State != NoticeDeadLetter {
		t.Fatalf("notice is %+v", after.Notice)
	}
	// The push runs off the pass that decided the dead letter.
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the dead letter's push never finished")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(pushes) != 1 || pushes[0].title != deadLetterTitle || pushes[0].tag != "dead-letter-"+r.ID ||
		!strings.Contains(pushes[0].body, r.Title) {
		t.Fatalf("dead-letter pushes: %+v", pushes)
	}
	effects, err := b.Store.Effects(ctx, EffectDeadLetterPush, r.ID)
	if err != nil || len(effects) != 1 || effects[0].State != "done" {
		t.Fatalf("the push is not one finished effect: %v %+v", err, effects)
	}
}

// The manual path: a dead letter is listed, is not re-armed unless asked by
// name, and once re-armed with the root back is delivered.
func TestADeadLetterCanBeRearmedByHand(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	here := false
	b.Live = func(context.Context) []session.Session {
		if !here {
			return nil
		}
		return []session.Session{{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation}}
	}
	typed := 0
	b.Type = func(context.Context, string, string) error { typed++; return nil }
	r := finished(t, b, ctx, "f1111111-1111-4111-8111-111111111111")
	for i := 0; i < AttemptLimit+1; i++ {
		b.PumpNotices(ctx)
		now = now.Add(retryCeiling + time.Second)
	}
	list, err := b.Completions(ctx, false)
	if err != nil || len(list) != 1 || list[0].Notice.State != NoticeDeadLetter {
		t.Fatalf("pending list: %v %+v", err, list)
	}
	if got, _, err := b.Rearm(ctx, "", false); err != nil || len(got) != 0 {
		t.Fatalf("a dead letter re-armed without being asked for: %v %v", got, err)
	}
	got, _, err := b.Rearm(ctx, r.ID, true)
	if err != nil || len(got) != 1 {
		t.Fatalf("rearm: %v %v", got, err)
	}
	mid, _, _ := b.Record(ctx, r.ID)
	if mid.Notice.State != NoticePending || mid.Notice.Attempts != 0 || mid.Notice.ID != list[0].Notice.ID {
		t.Fatalf("re-armed notice: %+v", mid.Notice)
	}
	here = true
	b.PumpNotices(ctx)
	if after, _, _ := b.Record(ctx, r.ID); after.Notice.State != NoticeDelivered || typed != 1 {
		t.Fatalf("after re-arming with the root back: %+v typed %d", after.Notice, typed)
	}
}

// A child's own notify goes through the same push, landing on its root.
func TestAChildsNotifyPushesToItsRoot(t *testing.T) {
	b, ctx := newTestBroker(t)
	var terminal string
	b.Push = func(_ context.Context, _, _, term, _ string) (int, int, error) { terminal = term; return 1, 0, nil }
	id := "a2111111-1111-4111-8111-111111111111"
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: "t", State: StateBriefed,
		CreatedAt: time.Now(), Claims: []string{}, RootTerminalID: "%1",
		Root: &RootRef{SessionID: rootConversation, Assistant: "claude"}}
	if err := b.save(ctx, r, HashSecret("s"), "task.queued"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AgentNotify(ctx, id, "s", "done", "it is done"); err != nil {
		t.Fatal(err)
	}
	if terminal != "%1" {
		t.Fatalf("the push landed on %q", terminal)
	}
	// Control: the wrong secret is refused before anything is pushed.
	terminal = ""
	if _, err := b.AgentNotify(ctx, id, "not-it", "done", "x"); err == nil || terminal != "" {
		t.Fatalf("a wrong secret: %v, pushed to %q", err, terminal)
	}
}

// File waits: the owner is told once, a second waiter joins, the release
// reaches every waiter, and a session named by its terminal is refused.
func TestAFileWaitIsToldAndReleased(t *testing.T) {
	b, ctx := newTestBroker(t)
	owner, waiter, other := rootConversation, "4b9b8c2e-0f5a-4c43-9a8f-2d9d1c7f6e01", "5c9b8c2e-0f5a-4c43-9a8f-2d9d1c7f6e02"
	menu := false
	b.Live = func(context.Context) []session.Session {
		return []session.Session{
			{ID: "%1", Assistant: session.AssistantClaude, ConversationID: owner},
			{ID: "%2", Assistant: session.AssistantClaude, ConversationID: waiter},
			{ID: "%3", Assistant: session.AssistantCodex, ConversationID: other},
		}
	}
	b.Choosing = func(_ context.Context, id string) bool { return menu && id == "%1" }
	typed := map[string][]string{}
	b.Type = func(_ context.Context, term, text string) error { typed[term] = append(typed[term], text); return nil }
	repo := t.TempDir()
	req := WaitRequest{Repository: repo, Paths: []string{"b.go", repo + "/a.go", "a.go"}, Owner: owner, Waiter: waiter,
		Reason: "I edit a.go next", ReleaseCondition: "your commit lands"}

	if _, err := b.RegisterWait(ctx, WaitRequest{Repository: repo, Paths: []string{"a.go"}, Owner: "%1", Waiter: waiter,
		Reason: "r", ReleaseCondition: "c"}); refusalCode(err) != "session_id_is_terminal" {
		t.Fatalf("an owner named by terminal: %v", err)
	}
	out, err := b.RegisterWait(ctx, req)
	if err != nil || out.Deduplicated || len(typed["%1"]) != 1 || !strings.Contains(typed["%1"][0], `"file_wait_request"`) {
		t.Fatalf("register: %v %+v typed %v", err, out, typed)
	}
	if got := strings.Join(out.Wait.Paths, ","); got != "a.go,b.go" {
		t.Fatalf("paths are keyed as %q", got)
	}
	again, err := b.RegisterWait(ctx, req)
	if err != nil || !again.Deduplicated || len(typed["%1"]) != 1 {
		t.Fatalf("the same waiter again typed a second request: %v %+v %v", err, again, typed)
	}
	menu = true
	req.Waiter = other
	if _, err := b.RegisterWait(ctx, req); refusalCode(err) != "owner_busy" {
		t.Fatalf("an owner showing a menu: %v", err)
	}
	menu = false
	if _, err := b.RegisterWait(ctx, req); err != nil {
		t.Fatalf("the second waiter once the menu is gone: %v", err)
	}
	if _, err := b.ReleaseWait(ctx, out.Wait.ID, waiter, "", ""); refusalCode(err) != "wrong_owner" {
		t.Fatalf("a waiter releasing: %v", err)
	}
	n, err := b.ReleaseWait(ctx, out.Wait.ID, owner, "abc1234", "")
	if err != nil || n != 2 || len(typed["%2"]) != 1 || len(typed["%3"]) != 1 ||
		!strings.Contains(typed["%2"][0], "abc1234") {
		t.Fatalf("release: %v %d typed %v", err, n, typed)
	}
	if open, _ := b.Waits(ctx); len(open) != 0 {
		t.Fatalf("a fully released wait is still open: %+v", open)
	}
}

// Detached automation is its own door: it takes only a poll-only brief with
// no owner, and the owned-child door refuses one.
func TestTheDetachedDoorTakesOnlyUnattendedBriefs(t *testing.T) {
	b, _ := newTestBroker(t)
	project := t.TempDir()
	id := "a3111111-1111-4111-8111-111111111111"
	writeBrief(t, b, id, project, map[string]any{"root": map[string]any{"session_id": nil, "poll_only": true}})
	if r, err := b.readDraftAs(id, false, true); err != nil || r.Root != nil {
		t.Fatalf("a detached brief at the detached door: %v %+v", err, r.Root)
	}
	if _, err := b.readDraftAs(id, false, false); refusalCode(err) != "detached_route_required" {
		t.Fatalf("a detached brief at the owned door: %v", err)
	}
	writeBrief(t, b, id, project, nil)
	if _, err := b.readDraftAs(id, false, true); refusalCode(err) != "detached_task_required" {
		t.Fatalf("an owned brief at the detached door: %v", err)
	}
}

// D23 ③: the shipped base is projected once, and the person's local file is
// read — from this daemon's directory, else from the Swift app's — and never
// written.
func TestTheHouseRulesAreProjectedAndTheLocalOneIsNeverWritten(t *testing.T) {
	dir, legacy := t.TempDir(), t.TempDir()
	wrote, err := ProjectPolicy(dir)
	if err != nil || !wrote {
		t.Fatalf("first projection: %v %v", wrote, err)
	}
	if wrote, err := ProjectPolicy(dir); err != nil || wrote {
		t.Fatalf("an unchanged projection wrote again: %v %v", wrote, err)
	}
	if err := os.WriteFile(legacy+"/"+PolicyLocalFile, []byte("legacy local"), 0o600); err != nil {
		t.Fatal(err)
	}
	base, local, src := ReadPolicy(dir, legacy)
	if base != string(ShippedPolicy()) || local != "legacy local" || src.Local != "legacy" || src.Base != "next" {
		t.Fatalf("read %d bytes/%q from %+v", len(base), local, src)
	}
	if err := os.WriteFile(dir+"/"+PolicyLocalFile, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, local, src := ReadPolicy(dir, legacy); local != "mine" || src.Local != "next" {
		t.Fatalf("own local: %q %+v", local, src)
	}
	if _, err := os.Stat(legacy + "/" + PolicyBaseFile); !os.IsNotExist(err) {
		t.Fatalf("something was written into the legacy directory: %v", err)
	}
}
