package http

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/planner"
	"github.com/sainteye/clawdline/internal/domain/copilot"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// recapRig is a book over real record files and a fake clock and fork.
type recapRig struct {
	t      *testing.T
	dir    string
	book   *recapBook
	now    time.Time
	mode   string
	mu     sync.Mutex
	forked []string
	answer func(planner.RecapTarget) (string, error)
	ended  chan string
}

func newRecapRig(t *testing.T) *recapRig {
	r := &recapRig{t: t, dir: t.TempDir(), now: time.Unix(1_800_000_000, 0), mode: copilot.RecapBackground, ended: make(chan string, 8)}
	r.answer = func(target planner.RecapTarget) (string, error) { return "recap of " + target.Conversation, nil }
	r.book = r.open()
	return r
}

func (r *recapRig) open() *recapBook {
	b := newRecapBook(r.dir, func(_ context.Context, target planner.RecapTarget) (string, error) {
		r.mu.Lock()
		r.forked = append(r.forked, target.Conversation)
		r.mu.Unlock()
		return r.answer(target)
	}, func() string { return r.mode }, func(s session.Session) string { return filepath.Join(r.dir, s.ConversationID+".jsonl") })
	b.now = func() time.Time { return r.now }
	b.done = func(conv string, _ error) { r.ended <- conv }
	return b
}

// write grows a conversation's record, as a turn does.
func (r *recapRig) write(conv, text string) {
	p := filepath.Join(r.dir, conv+".jsonl")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		r.t.Fatal(err)
	}
	f.WriteString(text + "\n")
	f.Close()
}

func (r *recapRig) observe(sessions ...session.Session) {
	r.book.Observe(context.Background(), session.Inventory{Sessions: sessions, Complete: true})
}

// wait is the one fork the last observe started.
func (r *recapRig) wait() string {
	select {
	case conv := <-r.ended:
		return conv
	case <-time.After(5 * time.Second):
		r.t.Fatal("no fork ended")
		return ""
	}
}

func (r *recapRig) none() {
	select {
	case conv := <-r.ended:
		r.t.Fatalf("an unexpected fork of %s ran", conv)
	case <-time.After(50 * time.Millisecond):
	}
}

func claudeAt(id, conv string, state session.State) session.Session {
	return session.Session{ID: id, Assistant: session.AssistantClaude, ConversationID: conv, CWD: "/w", State: state}
}

func TestARecapIsWrittenOnceAStoppedSessionHasStoodStill(t *testing.T) {
	r := newRecapRig(t)
	r.write("aaaa1111", "turn one")
	idle := claudeAt("%1", "aaaa1111", session.StateIdle)
	r.observe(idle)
	r.now = r.now.Add(recapAfterStop - time.Second)
	r.observe(idle)
	r.none()
	r.now = r.now.Add(2 * time.Second)
	r.observe(idle)
	if got := r.wait(); got != "aaaa1111" {
		t.Fatalf("forked %s", got)
	}
	rec := r.book.For(idle)
	if rec == nil || rec.Text != "recap of aaaa1111" || !rec.Current || rec.State != "idle" {
		t.Fatalf("recap = %+v", rec)
	}
	// Nothing new was said, so nothing is forked again.
	r.now = r.now.Add(time.Hour)
	r.observe(idle)
	r.none()
	// The conversation grew: the recap is no longer current, and a new one
	// is written once the Session stands still again.
	r.write("aaaa1111", "turn two")
	if rec := r.book.For(idle); rec == nil || rec.Current {
		t.Fatalf("a recap of a record that grew must not be current: %+v", rec)
	}
	r.observe(idle)
	if got := r.wait(); got != "aaaa1111" {
		t.Fatalf("forked %s", got)
	}
}

func TestAWorkingSessionIsRecappedThenRefreshedAtMostEveryTenMinutes(t *testing.T) {
	r := newRecapRig(t)
	r.write("bbbb2222", "start")
	working := claudeAt("%2", "bbbb2222", session.StateWorking)
	r.observe(working)
	r.now = r.now.Add(recapFirstWhileWorking)
	r.observe(working)
	r.wait()
	r.write("bbbb2222", "more work")
	r.now = r.now.Add(recapWhileWorking - time.Second)
	r.observe(working)
	r.none()
	r.now = r.now.Add(2 * time.Second)
	r.observe(working)
	r.wait()
}

func TestOneForkRunsAtATimeAndTheOneWaitingForThePersonGoesFirst(t *testing.T) {
	r := newRecapRig(t)
	release := make(chan struct{})
	r.answer = func(target planner.RecapTarget) (string, error) {
		<-release
		return "ok", nil
	}
	for _, c := range []string{"cccc0001", "cccc0002", "cccc0003"} {
		r.write(c, "x")
	}
	rows := []session.Session{
		claudeAt("%1", "cccc0001", session.StateWorking),
		claudeAt("%2", "cccc0002", session.StateIdle),
		claudeAt("%3", "cccc0003", session.StateWaiting),
	}
	r.observe(rows...)
	r.now = r.now.Add(recapAfterStop)
	r.observe(rows...)
	r.observe(rows...)
	forkedNow := func() []string {
		r.mu.Lock()
		defer r.mu.Unlock()
		return append([]string(nil), r.forked...)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(forkedNow()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	r.observe(rows...)
	time.Sleep(50 * time.Millisecond)
	forked := forkedNow()
	if len(forked) != 1 || forked[0] != "cccc0003" {
		t.Fatalf("forked %v, want only the waiting Session", forked)
	}
	close(release)
	r.wait()
}

func TestAFailedForkWaitsBeforeItIsTriedAgain(t *testing.T) {
	r := newRecapRig(t)
	r.answer = func(planner.RecapTarget) (string, error) { return "", planner.ErrOutOfQuota }
	r.write("dddd4444", "x")
	idle := claudeAt("%4", "dddd4444", session.StateIdle)
	r.observe(idle)
	r.now = r.now.Add(recapAfterStop)
	r.observe(idle)
	r.wait()
	r.now = r.now.Add(recapRetryAfter - time.Second)
	r.observe(idle)
	r.none()
	r.now = r.now.Add(2 * time.Second)
	r.observe(idle)
	r.wait()
}

func TestRecapsAreKeptAcrossARestartAndNeverOnTheWrongConversation(t *testing.T) {
	r := newRecapRig(t)
	r.write("eeee5555", "x")
	idle := claudeAt("%5", "eeee5555", session.StateIdle)
	r.observe(idle)
	r.now = r.now.Add(recapAfterStop)
	r.observe(idle)
	r.wait()
	reopened := r.open()
	if rec := reopened.For(idle); rec == nil || rec.Text != "recap of eeee5555" || !rec.Current {
		t.Fatalf("after a restart: %+v", rec)
	}
	// The same terminal now runs another conversation.
	if rec := reopened.For(claudeAt("%5", "ffff6666", session.StateIdle)); rec != nil {
		t.Fatalf("a recap of another conversation was returned: %+v", rec)
	}
	info, err := os.Stat(filepath.Join(r.dir, recapFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the recap file must be private: %v %v", info, err)
	}
}

func TestNothingIsForkedUnlessTheBackgroundModeIsOn(t *testing.T) {
	for _, mode := range []string{copilot.RecapOff, copilot.RecapOnDemand} {
		r := newRecapRig(t)
		r.mode = mode
		r.write("abab7777", "x")
		idle := claudeAt("%7", "abab7777", session.StateIdle)
		r.observe(idle)
		r.now = r.now.Add(time.Hour)
		r.observe(idle)
		r.none()
	}
}

func TestOnDemandKeepsACurrentRecapAndForksAStaleOne(t *testing.T) {
	r := newRecapRig(t)
	r.mode = copilot.RecapOnDemand
	r.book.done = nil
	r.write("acac8888", "x")
	idle := claudeAt("%8", "acac8888", session.StateIdle)
	rec, wrote, err := r.book.Now(context.Background(), idle)
	if err != nil || !wrote || rec.Text != "recap of acac8888" {
		t.Fatalf("first: %+v %v %v", rec, wrote, err)
	}
	if _, wrote, err := r.book.Now(context.Background(), idle); err != nil || wrote {
		t.Fatalf("an unchanged conversation must reuse its recap: wrote=%v err=%v", wrote, err)
	}
	r.write("acac8888", "y")
	if _, wrote, err := r.book.Now(context.Background(), idle); err != nil || !wrote {
		t.Fatalf("a grown conversation must be forked again: wrote=%v err=%v", wrote, err)
	}
	r.mode = copilot.RecapOff
	if _, _, err := r.book.Now(context.Background(), idle); !errors.Is(err, ErrNoRecaps) {
		t.Fatalf("off: err = %v", err)
	}
}

func TestOnDemandRefusesAFourthForkAtOnce(t *testing.T) {
	r := newRecapRig(t)
	r.mode = copilot.RecapOnDemand
	r.book.done = nil
	release := make(chan struct{})
	r.answer = func(planner.RecapTarget) (string, error) { <-release; return "ok", nil }
	var wg sync.WaitGroup
	for i, c := range []string{"adad0001", "adad0002", "adad0003"} {
		r.write(c, "x")
		wg.Add(1)
		go func(s session.Session) {
			defer wg.Done()
			r.book.Now(context.Background(), s)
		}(claudeAt("%"+string(rune('a'+i)), c, session.StateIdle))
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(r.book.onDemand) < maxSessionRecapOnDemand && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	r.write("adad0004", "x")
	if _, _, err := r.book.Now(context.Background(), claudeAt("%z", "adad0004", session.StateIdle)); !errors.Is(err, ErrRecapBusy) {
		t.Fatalf("err = %v, want busy", err)
	}
	close(release)
	wg.Wait()
}
