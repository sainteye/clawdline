package orchestrator

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline-go/internal/adapters/taskdir"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// The beat: what the broker does when nobody asked it anything.
//
// Four things, in this order, and the order is the design:
//
//  1. collect `progress.json`, because a note is the only thing that can prove
//     a child read its briefing, and the spawn clock below would otherwise kill
//     a child that is working but cannot reach loopback;
//  2. adopt a validated `result.json` its child never renamed;
//  3. collect `result.json`, which is the completion signal;
//  4. run the clocks — the four minutes to reach a prompt, and the task's own
//     timeout — and then pump the notices.

// Pulse is what one pass did, for /v1/diagnostics.
type Pulse struct {
	At        time.Time
	Watched   int
	Settled   int
	Notes     int
	Notices   int
	TimedOut  int
	SpawnFail int
	// StoreErr is why the pass could not read the store, when it could not.
	// A pass that read nothing because it could not read is not a pass that
	// found nothing, and the two must not look alike from outside.
	StoreErr string
}

// Watch runs the beat until the context is cancelled. Run is the same loop
// under a supervisor, which is how the daemon starts it.
func (b *Broker) Watch(ctx context.Context, tick time.Duration, report func(Pulse)) {
	if tick <= 0 {
		tick = 5 * time.Second
	}
	timer := time.NewTicker(tick)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			p := b.Pass(ctx)
			if report != nil {
				report(p)
			}
		}
	}
}

// Pass is one beat, exposed so a test — and a person with a debugger — can run
// exactly one.
//
// Every task in the pass is decided against **one** reading of the machine,
// taken once at the top. Asking the machine again per task was both slower —
// each reading is a process-table and terminal scan — and less honest: a
// terminal that appeared mid-pass made two tasks in one pass disagree about
// the same moment.
func (b *Broker) Pass(ctx context.Context) Pulse {
	number := b.beat.begin(b.now())
	// Recorded as finished only when it finished. A pass that panicked or
	// exited is not a pass that ended; the supervisor closes it instead
	// (observe.go), and until then it is the pass in progress.
	finished := false
	var p Pulse
	defer func() {
		if finished {
			b.beat.end(b.now(), p)
		}
	}()
	p = b.pass(ctx, number)
	finished = true
	return p
}

func (b *Broker) pass(ctx context.Context, number int64) Pulse {
	p := Pulse{At: b.now()}
	if b.Fault != nil {
		b.Fault(number)
	}
	records, err := b.records(ctx)
	if err != nil {
		p.StoreErr = err.Error()
		return p
	}
	live := []Record{}
	for _, r := range records {
		if !r.State.Terminal() {
			live = append(live, r)
		}
	}
	rd := b.read(ctx)
	for _, r := range live {
		p.Watched++
		if b.collectNote(ctx, &r) {
			p.Notes++
		}
		b.proveBriefing(ctx, rd, &r)
		if settled := b.collectResult(ctx, r); settled {
			p.Settled++
			continue
		}
		if b.runClocks(ctx, rd, r, &p) {
			continue
		}
	}
	// Observed after the pass has changed what it was going to change, so a
	// task settled above is not observed as though it were still running.
	if still, err := b.liveTasks(ctx); err == nil {
		b.observe(ctx, rd, still)
	}
	p.Notices = b.PumpNotices(ctx)
	return p
}

// collectNote reads the file a child writes when it cannot reach loopback.
//
// The secret is checked here and not by the reader: a file under a directory
// the child owns proves only that somebody who can write there wrote it, and
// this daemon's own task root is writable by this user's other processes too.
func (b *Broker) collectNote(ctx context.Context, r *Record) bool {
	note, _, ok := b.Tasks.ReadProgress(r.ID)
	if !ok {
		return false
	}
	trimmed := strings.TrimSpace(note.Note)
	// The same file read again is an observation, not a note: it was offered
	// to the store the first time it was seen, and offering it every five
	// seconds after that would open a write transaction per child per pass
	// to be told "already have it". The key includes the secret, so a file
	// rewritten with the right secret after a wrong one is looked at afresh.
	key := note.Secret + "\x00" + trimmed
	if b.observed.progressKnown(r.ID, key) {
		return false
	}
	_, hash, err := b.Record(ctx, r.ID)
	if err != nil {
		return false
	}
	if !SecretMatches(hash, note.Secret) || trimmed == "" || utf8.RuneCountInString(trimmed) > progressLimit {
		b.observed.settleProgress(r.ID, key)
		return false
	}
	had, err := b.Store.HasBrokerNote(ctx, r.ID, trimmed)
	if err != nil {
		return false
	}
	if had {
		b.observed.settleProgress(r.ID, key)
		return false
	}
	stored, added, err := b.Store.AppendBrokerNote(ctx, r.ID, trimmed)
	if err != nil {
		return false
	}
	b.observed.settleProgress(r.ID, key)
	if !added {
		return false
	}
	b.progress.publish(stored)
	if now, err := b.promote(ctx, r.ID); err == nil {
		*r = now
	}
	return true
}

// promote moves a task that has proved it read its briefing from `spawning`
// to `briefed`, and leaves every other state alone. Under mutate, so a
// promotion never lands on a task that finished in the meantime.
func (b *Broker) promote(ctx context.Context, id string) (Record, error) {
	return b.mutate(ctx, id, "task.briefed", func(r *Record) error {
		if r.State != StateSpawning && r.State != StateQueued {
			return errUnchanged
		}
		r.State = StateBriefed
		return nil
	})
}

// collectResult settles a task whose child wrote one.
func (b *Broker) collectResult(ctx context.Context, r Record) bool {
	result, raw, err := b.Tasks.ReadResult(r.ID)
	if errors.Is(err, taskdir.ErrNoResult) {
		// A validated result its child never renamed is published here, and
		// only when the marker still binds the exact bytes on disk. Age is not
		// consent, which is why the marker carries a hash and not a timestamp.
		if ready, _, ok := b.Tasks.ReadReady(r.ID); ok && b.authentic(ctx, r, ready) {
			if err := b.Tasks.AdoptReady(r.ID); err != nil {
				return false
			}
			result, raw, err = b.Tasks.ReadResult(r.ID)
			if err != nil {
				return false
			}
		} else {
			return false
		}
	}
	if err != nil {
		log.Printf("orchestrator: task %s wrote a result nobody can read: %v", r.ID, err)
		return false
	}
	if !b.authentic(ctx, r, result) {
		return false
	}
	result.Secret = ""
	_ = raw
	if _, err := b.Settle(ctx, r.ID, SettleState(result.Status), result.Summary, &result); err != nil {
		if !errors.Is(err, errAlreadyTerminal) {
			log.Printf("orchestrator: task %s could not be settled: %v", r.ID, err)
		}
		return false
	}
	return true
}

// authentic proves a result file was written by the child it claims to be.
//
// Protocol, id and secret, all three. A file that names the right task but
// carries no secret is not a weaker delivery — it is somebody else's file in
// this task's directory, and settling on it would close a task whose child is
// still working.
func (b *Broker) authentic(ctx context.Context, r Record, result taskdir.Result) bool {
	if result.Protocol != Protocol || result.TaskID != r.ID {
		return false
	}
	switch result.Status {
	case "success", "failure":
	default:
		return false
	}
	_, hash, err := b.Record(ctx, r.ID)
	if err != nil {
		return false
	}
	return SecretMatches(hash, result.Secret)
}

// spawnVerdict decides what four minutes of silence from a child means.
//
// It used to mean `spawn_failed` on its own, and that was wrong in the ordinary
// case: the briefing tells a child **not** to send heartbeat notes, so a
// healthy child that is simply working says nothing, and a task that had
// started perfectly well was recorded as one that never started. Silence is not
// evidence of anything; what the tab is doing is.
//
//   - the tab is gone          -> spawn_failed, and the sentence says so
//   - the tab is holding a dialog -> spawn_failed: the briefing could not be
//     typed at it, and a keystroke would have answered the dialog instead
//   - the tab is alive         -> nothing. The task's own timeout is the
//     backstop, and `spawning` is the honest word for a child that has been
//     typed at without anything coming back yet.
func spawnVerdict(alive, choosing bool) (State, string, bool) {
	switch {
	case !alive:
		return StateSpawnFailed, "The child session did not reach a prompt within 4 minutes. " +
			"If several sessions were starting at once, they were competing for this Mac.", true
	case choosing:
		return StateSpawnFailed, "The child session is holding a dialog four minutes after it opened, " +
			"so the briefing was never typed: answering that dialog is a person's decision, not this broker's.", true
	}
	return "", "", false
}

// runClocks is the two deadlines. It answers true when the task became
// terminal.
func (b *Broker) runClocks(ctx context.Context, rd reading, r Record, p *Pulse) bool {
	now := b.now()
	if r.State == StateSpawning && !r.SpawnedAt.IsZero() && now.Sub(r.SpawnedAt) > readyLimit {
		alive := false
		if s, ok := rd.session(r.ChildTerminalID); ok && s.IsAssistant() {
			alive = true
		}
		choosing := false
		if alive && b.Screen != nil {
			if screen, ok := b.Screen(ctx, r.ChildTerminalID); ok {
				choosing = Choosing(screen)
			}
		}
		// A reading with no terminals in it at all cannot say the tab is
		// gone (the Swift app's `3a7adb8e`): that verdict waits for one that
		// sees something. Completeness is deliberately not required here —
		// on a Mac whose iTerm2 cannot be asked, no reading is ever
		// complete, and requiring it would turn a four-minute verdict into a
		// wait for the task's own timeout. The timeout below still runs,
		// because it is a clock on the record, not a reading of the machine.
		if alive || rd.seen() {
			if state, why, decided := spawnVerdict(alive, choosing); decided {
				if _, err := b.Settle(ctx, r.ID, state, why, nil); err == nil {
					p.SpawnFail++
					return true
				}
			}
		}
	}
	if deadline := r.Deadline(); !deadline.IsZero() && now.After(deadline) {
		if _, err := b.Settle(ctx, r.ID, StateTimeout,
			"The task passed its timeout without writing a result.", nil); err == nil {
			p.TimedOut++
			return true
		}
	}
	return false
}

// proveBriefing promotes a child that has plainly taken the turn we typed.
//
// A turn that starts after the briefing was typed is the cheapest positive
// evidence this daemon has that the bytes were read, and it is weaker than the
// Swift app's — which reads the child's own transcript for the task marker. It
// can be fooled by a child that was already working on something else, which is
// why it only ever moves `spawning` to `briefed` and never settles anything.
func (b *Broker) proveBriefing(ctx context.Context, rd reading, r *Record) bool {
	if r.State != StateSpawning || r.ChildTerminalID == "" {
		return false
	}
	s, ok := rd.session(r.ChildTerminalID)
	if !ok || !s.IsAssistant() || s.State != session.StateWorking {
		return false
	}
	now, err := b.promote(ctx, r.ID)
	if err != nil {
		return false
	}
	*r = now
	return true
}
