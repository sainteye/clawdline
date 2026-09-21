package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/git"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// W2 (docs/design-decisions.md §6): the storage primitives. Each test names
// the criterion it answers; the report says how each was seen to fail with
// the fix taken out.

const (
	w2Source = "%1"
	w2Target = "%2"
)

// w2Broker is a broker over dir whose typing appends each line to typed.log —
// the effect a crash must neither lose nor repeat.
func w2Broker(t *testing.T, dir string) (*Broker, *store.Store) {
	t.Helper()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	b := &Broker{
		Store: st,
		Tasks: taskdir.New(dir),
		Git:   git.New(),
		Dir:   dir,
		Live: func(context.Context) []session.Session {
			return []session.Session{
				{ID: w2Source, Assistant: session.AssistantClaude, ConversationID: rootConversation},
				{ID: w2Target, Assistant: session.AssistantClaude, ConversationID: "0b0b0b0b-0b0b-4b0b-8b0b-0b0b0b0b0b0b"},
			}
		},
		Type: func(_ context.Context, terminal, text string) error {
			f, err := os.OpenFile(filepath.Join(dir, "typed.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = f.WriteString(terminal + " " + text + "\n")
			return err
		},
	}
	return b, st
}

func typedLines(t *testing.T, dir string) int {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, "typed.log"))
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(body), "\n")
}

// TestW2HelperProcess is not a test. It is the process ② kills: it relays one
// message with the fault seam set to kill it at the point named, and never
// returns normally.
func TestW2HelperProcess(t *testing.T) {
	dir, point := os.Getenv("W2_HELPER_DIR"), os.Getenv("W2_HELPER_POINT")
	if dir == "" {
		return
	}
	b, _ := w2Broker(t, dir)
	b.EffectFault = func(p string, _ store.Effect) {
		if p == point {
			self, _ := os.FindProcess(os.Getpid())
			_ = self.Kill()
			select {}
		}
	}
	_, err := b.Relay(context.Background(), Message{From: w2Source, To: w2Target, Text: "hello once"}, "key-1")
	t.Fatalf("the helper was not killed at %q: relay answered %v", point, err)
}

// killedAt runs the helper and answers once the process is gone.
func killedAt(t *testing.T, dir, point string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestW2HelperProcess$", "-test.count=1")
	cmd.Env = append(os.Environ(), "W2_HELPER_DIR="+dir, "W2_HELPER_POINT="+point)
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.Success() {
		t.Fatalf("the helper was not killed (err %v):\n%s", err, out)
	}
}

// ② The process dies after the message is committed and before it is typed.
// After the restart it is typed exactly once — by the recovery, not by the
// resend — and neither another recovery nor the caller's resend types it
// again. The resend gets the answer the recovery filed (③).
func TestAMessageCommittedBeforeACrashIsTypedExactlyOnce(t *testing.T) {
	dir := t.TempDir()
	killedAt(t, dir, "committed")
	if n := typedLines(t, dir); n != 0 {
		t.Fatalf("typed %d line(s) before the crash; the fault was set before the effect", n)
	}

	b, _ := w2Broker(t, dir)
	if n := b.RecoverEffects(context.Background()); n != 1 {
		t.Fatalf("recovery settled %d effect(s), want 1", n)
	}
	if n := typedLines(t, dir); n != 1 {
		t.Fatalf("after the restart the message was typed %d time(s), want exactly 1", n)
	}
	if n := b.RecoverEffects(context.Background()); n != 0 {
		t.Fatalf("a second recovery settled %d effect(s)", n)
	}
	sent, err := b.Relay(context.Background(), Message{From: w2Source, To: w2Target, Text: "hello once"}, "key-1")
	if err != nil || !sent.Replayed {
		t.Fatalf("the resend answered %+v, %v; want the filed answer, replayed", sent, err)
	}
	if n := typedLines(t, dir); n != 1 {
		t.Fatalf("the resend typed it again: %d lines", n)
	}
}

// ② The other half: the process dies after the attempt began. Whether the
// bytes went in cannot be known, so the recovery types nothing and the
// request's receipt says `request_outcome_unknown` — the same answer on every
// resend.
func TestAMessageWhoseTypingWasInterruptedIsNeverTypedTwice(t *testing.T) {
	dir := t.TempDir()
	killedAt(t, dir, "started")
	b, _ := w2Broker(t, dir)
	b.RecoverEffects(context.Background())
	if n := typedLines(t, dir); n != 0 {
		t.Fatalf("a started, unfinished message was typed %d time(s) by the recovery", n)
	}
	for i := 0; i < 2; i++ {
		_, err := b.Relay(context.Background(), Message{From: w2Source, To: w2Target, Text: "hello once"}, "key-1")
		if refusalCode(err) != "request_outcome_unknown" {
			t.Fatalf("resend %d answered %v, want request_outcome_unknown", i, err)
		}
	}
	if n := typedLines(t, dir); n != 0 {
		t.Fatalf("a resend typed it: %d lines", n)
	}
}

// ③ Same (scope, actor, key): the same body is the first answer, typed once;
// another body is refused; past the window the key is expired and nothing is
// typed.
func TestAResentMessageGetsTheSameReceipt(t *testing.T) {
	dir := t.TempDir()
	b, _ := w2Broker(t, dir)
	ctx := context.Background()
	now := time.Now()
	b.Clock = func() time.Time { return now }
	msg := Message{From: w2Source, To: w2Target, Text: "once"}
	first, err := b.Relay(ctx, msg, "k")
	if err != nil || first.Replayed {
		t.Fatalf("first send: %+v, %v", first, err)
	}
	now = now.Add(5 * time.Second)
	again, err := b.Relay(ctx, msg, "k")
	if err != nil || !again.Replayed || !again.At.Equal(first.At) {
		t.Fatalf("resend: %+v, %v; want the first answer (at %v), replayed", again, err, first.At)
	}
	if n := typedLines(t, dir); n != 1 {
		t.Fatalf("typed %d time(s) for one key", n)
	}
	if _, err := b.Relay(ctx, Message{From: w2Source, To: w2Target, Text: "different"}, "k"); refusalCode(err) != "idempotency_key_reused" {
		t.Fatalf("the key with another body answered %v", err)
	}
	now = now.Add(store.ReceiptWindow + time.Minute)
	if _, err := b.Relay(ctx, msg, "k"); refusalCode(err) != "receipt_expired" {
		t.Fatalf("resend past the window answered %v, want receipt_expired", err)
	}
	if n := typedLines(t, dir); n != 1 {
		t.Fatalf("typed %d time(s); an expired key must not run again", n)
	}
}

// ④ The beat reads the live rows and nothing else, and no hot path reads the
// long prose. A task's summary is kept whole, in its own table.
func TestTheBeatDoesNotReadTheHistory(t *testing.T) {
	b, ctx := newTestBroker(t)
	long := strings.Repeat("s", 5000)
	for i := 0; i < 200; i++ {
		id := "d0000000-0000-4000-8000-" + padded(i)
		r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: "old", State: StateSuccess,
			CreatedAt: time.Now(), Claims: []string{}, Instructions: long,
			Result: &taskdir.Result{Status: "success", Summary: long}}
		if err := b.save(ctx, r, HashSecret("s"), "task.success"); err != nil {
			t.Fatal(err)
		}
	}
	live := Record{Protocol: Protocol, ID: "d1111111-1111-4111-8111-111111111111", Assistant: "claude",
		Title: "live", State: StateBriefed, CreatedAt: time.Now(), Claims: []string{}, Instructions: long}
	if err := b.save(ctx, live, HashSecret("s"), "task.briefed"); err != nil {
		t.Fatal(err)
	}

	records, texts := b.Store.ReadCounts()
	b.Pass(ctx)
	r2, t2 := b.Store.ReadCounts()
	if got := r2 - records; got > 3 {
		t.Errorf("one beat read %d task records for 1 live task among 201", got)
	}
	if t2 != texts {
		t.Errorf("one beat read %d long-prose bodies", t2-texts)
	}

	if _, _, err := b.Records(ctx); err != nil {
		t.Fatal(err)
	}
	r3, t3 := b.Store.ReadCounts()
	if _, _, err := b.Records(ctx); err != nil {
		t.Fatal(err)
	}
	r4, t4 := b.Store.ReadCounts()
	if r4 != r3 || t4 != t3 || t3 != t2 {
		t.Errorf("a second list of an unchanged table read %d records and %d bodies", r4-r3, t4-t2)
	}

	raw := rawRecord(t, b.Dir, "d0000000-0000-4000-8000-"+padded(0))
	if strings.Contains(raw, long[:100]) {
		t.Error("the long prose is still inside the record the hot path decodes")
	}
	full, _, err := b.Record(ctx, "d0000000-0000-4000-8000-"+padded(0))
	if err != nil {
		t.Fatal(err)
	}
	if full.Instructions != long || full.Result == nil || full.Result.Summary != long {
		t.Error("the task's own read lost its instructions or summary, or cut them")
	}
}

func padded(i int) string { return fmt.Sprintf("%012d", i) }

// D08's guard: an effect asked for from inside a write transaction is refused
// — a terminal typed into, a push sent — not merely discouraged.
func TestAnEffectInsideTheWriteRightIsRefused(t *testing.T) {
	dir := t.TempDir()
	b, _ := w2Broker(t, dir)
	ctx := context.Background()
	id := "e1111111-1111-4111-8111-111111111111"
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", State: StateBriefed, CreatedAt: time.Now(), Claims: []string{}}
	if err := b.save(ctx, r, HashSecret("s"), "task.briefed"); err != nil {
		t.Fatal(err)
	}
	var inside error
	if _, err := b.mutate(ctx, id, "task.test", func(r *Record) error {
		inside = b.typeLine(ctx, w2Target, "typed under the lock")
		return errUnchanged
	}); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(inside, ErrEffectInsideWrite) {
		t.Fatalf("typing inside the write answered %v, want ErrEffectInsideWrite", inside)
	}
	if n := typedLines(t, dir); n != 0 {
		t.Fatal("the line was typed")
	}
	if err := b.typeLine(ctx, w2Target, "typed outside"); err != nil || typedLines(t, dir) != 1 {
		t.Fatalf("typing outside the write failed: %v", err)
	}
}

// G14: the checkout is made after the task that owes it is durable, as an
// effect the task's creation recorded — never before the record exists.
func TestTheWorktreeIsAnEffectOfTheRecordedTask(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "base"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v %s", err, out)
		}
	}
	b, ctx := newTestBroker(t)
	id := "f1111111-1111-4111-8111-111111111111"
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", ProjectDir: repo, Isolation: IsolationWorktree,
		State: StateQueued, CreatedAt: time.Now(), Claims: []string{"a.go"}}
	w, _, err := b.planWorktree(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if dirExists(w.Path) {
		t.Fatal("planning made the checkout")
	}
	r.Worktree = w
	ids, err := b.create(ctx, r, HashSecret("s"), store.Effect{Kind: EffectWorktree, Subject: id,
		Payload: []byte(`{"repository":"` + w.Repository + `","path":"` + w.Path + `","branch":"` + w.Branch + `","base":"` + w.Base + `"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if dirExists(w.Path) {
		t.Fatal("the checkout exists before its effect ran")
	}
	res := b.runRecorded(ctx, ids)
	if len(res) != 1 || res[0].state != store.EffectDone || !dirExists(w.Path) {
		t.Fatalf("the effect ran as %+v and the checkout is there: %v", res, dirExists(w.Path))
	}
	// A second attempt — a recovery after a crash between the checkout and
	// its record — finds it made and does not fail or make another.
	again := runWorktree(ctx, b, store.Effect{Subject: id, Payload: []byte(`{"repository":"` + w.Repository +
		`","path":"` + w.Path + `","branch":"` + w.Branch + `","base":"` + w.Base + `"}`)})
	if again.state != store.EffectDone || again.outcome != "already there" {
		t.Fatalf("a second attempt answered %+v", again)
	}
}

// A task still queued whose dispatcher has provably gone can never be briefed
// — only that process held its secret — and is settled instead of holding its
// claims until its timeout.
func TestAQueuedTaskWhoseDispatcherDiedIsSettled(t *testing.T) {
	dir := t.TempDir()
	b, _ := w2Broker(t, dir)
	ctx := context.Background()
	id := "f2222222-2222-4222-8222-222222222222"
	r := Record{Protocol: Protocol, ID: id, Assistant: "claude", State: StateQueued, CreatedAt: time.Now(),
		Claims: []string{"x"}, Dispatcher: "999999:dead"}
	if err := b.save(ctx, r, HashSecret("s"), "task.queued"); err != nil {
		t.Fatal(err)
	}
	mine := "f3333333-3333-4333-8333-333333333333"
	r2 := r
	r2.ID, r2.Dispatcher = mine, b.Store.Owner()
	if err := b.save(ctx, r2, HashSecret("s"), "task.queued"); err != nil {
		t.Fatal(err)
	}
	b.Pass(ctx)
	gone, _, _ := b.Record(ctx, id)
	if gone.State != StateSpawnFailed {
		t.Errorf("a queued task whose dispatcher is gone is %q", gone.State)
	}
	still, _, _ := b.Record(ctx, mine)
	if still.State != StateQueued {
		t.Errorf("this process's own queued task was settled: %q", still.State)
	}
}
