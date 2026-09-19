package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/taskdir"
)

// The beat: what the broker does when nobody asked it anything.
//
// Four things, in this order, and the order is the design:
//
//  1. collect `accepted.json` and `progress.json`, because a signed receipt is
//     the only thing that can prove a child read its briefing (D10), and the
//     spawn clock below would otherwise judge a child that is working but
//     cannot reach loopback;
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
	// Unreadable is how many stored rows this pass could not decode (D05 ②).
	// They are not watched — nothing about them can be — and they are not
	// silent either: the count is here, and each one is listed by the task
	// list and the inventory.
	Unreadable int
	// NotesRefused is how many progress.json bodies this pass refused for
	// good: too long, empty, or not signed by the task (G34). Each is also one
	// `task.progress.refused` event, once per distinct body.
	NotesRefused int
	// Closed is how many child sessions this pass closed after judging them
	// spawn_failed on positive evidence (D11), or after their linger (#26).
	Closed int
	// Recovered is how many effects a dead broker left unfinished that this
	// pass settled — run once, or recorded as unknown (effects.go).
	Recovered int
	// Todos is how many session to-dos this pass moved: made or followed on
	// the first pass, handed off, returned or dropped on a reading (todos.go).
	Todos int
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
	// Answered receipts past their window become tombstones (D03), once every
	// sixty passes: a late resend is told "expired" either way, and this only
	// lets go of the answer it no longer owes anybody.
	if number%60 == 1 {
		_, _ = b.Store.ExpireReceipts(ctx, "", store.ReceiptPolicy{}, b.now())
	}
	// Effects a broker that has since died left unfinished are settled first
	// (effects.go): a message it recorded and never typed is typed now, once.
	p.Recovered = b.RecoverEffects(ctx)
	// Only the live rows. The beat's cost is the number of tasks still
	// running, not the length of the history (G33).
	live, bad, err := b.liveLedger(ctx)
	if err != nil {
		p.StoreErr = err.Error()
		return p
	}
	p.Unreadable = len(bad)
	rd := b.read(ctx)
	b.keepReading(rd)
	for _, r := range live {
		p.Watched++
		switch b.collectNote(ctx, &r) {
		case noteTaken:
			p.Notes++
		case noteRefused:
			p.NotesRefused++
		}
		b.collectAccepted(ctx, &r)
		if settled := b.collectResult(ctx, r); settled {
			p.Settled++
			continue
		}
		if b.settleOrphan(ctx, r) {
			p.SpawnFail++
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
	// After the reading is recorded, so an owner's presence is this pass's.
	p.Todos = b.tendTodos(ctx)
	p.Notices = b.PumpNotices(ctx)
	// Finished children's tabs whose linger is over (linger.go), and — off
	// the beat, when one is due — the reclamation sweep (reclaim.go).
	p.Closed += b.closeLingers(ctx, rd)
	b.reclaimDue(ctx)
	return p
}

// What collectNote did with a progress.json.
type noteOutcome int

const (
	noteNone noteOutcome = iota
	noteTaken
	noteRefused
)

// collectNote reads the file a child writes when it cannot reach loopback.
//
// The secret is checked here and not by the reader: a file under a directory
// the child owns proves only that somebody who can write there wrote it, and
// this daemon's own task root is writable by this user's other processes too.
//
// A body refused for good — too long, empty, not signed by this task — is
// said, once: one `task.progress.refused` event and a count in the pulse. The
// first version dropped it without a word, so a child whose sentence ran to
// 301 characters looked exactly like a child that had said nothing (G34).
func (b *Broker) collectNote(ctx context.Context, r *Record) noteOutcome {
	note, _, ok := b.Tasks.ReadProgress(r.ID)
	if !ok {
		return noteNone
	}
	trimmed := strings.TrimSpace(note.Note)
	// The same file read again is an observation, not a note: it was offered
	// to the store the first time it was seen, and offering it every five
	// seconds after that would open a write transaction per child per pass
	// to be told "already have it". The key includes the secret, so a file
	// rewritten with the right secret after a wrong one is looked at afresh.
	key := note.Secret + "\x00" + trimmed
	if b.observed.progressKnown(r.ID, key) {
		return noteNone
	}
	_, hash, err := b.Record(ctx, r.ID)
	if err != nil {
		return noteNone
	}
	reason := ""
	switch {
	case !SecretMatches(hash, note.Secret):
		reason = "bad_secret"
	case trimmed == "":
		reason = "empty"
	case utf8.RuneCountInString(trimmed) > progressLimit:
		reason = "too_long"
	}
	if reason != "" {
		payload, _ := json.Marshal(map[string]any{
			"task": r.ID, "reason": reason, "runes": utf8.RuneCountInString(trimmed), "limit": progressLimit,
		})
		if err := b.Store.Append(ctx, store.Event{Kind: "task.progress.refused", Subject: r.ID, Payload: payload}); err != nil {
			// Not settled: the refusal is said on a later pass, when the
			// store can take it, rather than lost to this one.
			return noteNone
		}
		b.observed.settleProgress(r.ID, key)
		return noteRefused
	}
	had, err := b.Store.HasBrokerNote(ctx, r.ID, trimmed)
	if err != nil {
		return noteNone
	}
	if had {
		b.observed.settleProgress(r.ID, key)
		return noteNone
	}
	stored, added, err := b.Store.AppendBrokerNote(ctx, r.ID, trimmed)
	if err != nil {
		return noteNone
	}
	b.observed.settleProgress(r.ID, key)
	if !added {
		return noteNone
	}
	b.progress.publish(stored)
	if now, err := b.promote(ctx, r.ID); err == nil {
		*r = now
	}
	return noteTaken
}

// collectAccepted reads the receipt a child writes when it cannot reach
// loopback: accepted.json, `{"task_secret": …}`, the shape of progress.json
// (D10). A task that already holds a receipt is not read again, and a body
// refused once is not re-checked every pass.
func (b *Broker) collectAccepted(ctx context.Context, r *Record) {
	if !r.AcceptedAt.IsZero() {
		return
	}
	receipt, ok := b.Tasks.ReadAccepted(r.ID)
	if !ok || b.observed.acceptedKnown(r.ID, receipt.Secret) {
		return
	}
	now, err := b.Accept(ctx, r.ID, receipt.Secret)
	if err != nil {
		if ref, ok := err.(Refusal); ok && ref.Code != "orchestrator_store_unavailable" {
			b.observed.settleAccepted(r.ID, receipt.Secret)
		}
		return
	}
	b.observed.settleAccepted(r.ID, receipt.Secret)
	*r = now
}

// promote moves a task that has proved it read its briefing — with a signed
// note, the only proof besides `accepted` — from `spawning` to `briefed`, and
// leaves every other state alone. Under mutate, so a promotion never lands on
// a task that finished in the meantime.
func (b *Broker) promote(ctx context.Context, id string) (Record, error) {
	return b.mutate(ctx, id, "task.briefed", func(r *Record) error {
		if r.State != StateSpawning && r.State != StateQueued {
			return errUnchanged
		}
		r.State = StateBriefed
		return nil
	})
}

// Why collect did not settle a task. taskdir.ErrNoResult is the ordinary one.
var (
	errResultNotAuthentic = errors.New("result.json is not this task's")
	errResultUnreadable   = errors.New("result.json is not readable JSON")
)

// collectResult is the beat's collection: collect, with anything unusual
// logged rather than returned, because nobody is waiting on the answer.
func (b *Broker) collectResult(ctx context.Context, r Record) bool {
	settled, err := b.collect(ctx, r)
	switch {
	case settled, err == nil, errors.Is(err, taskdir.ErrNoResult), errors.Is(err, errAlreadyTerminal),
		errors.Is(err, errResultNotAuthentic):
	default:
		log.Printf("orchestrator: task %s: %v", r.ID, err)
	}
	return settled
}

// collect settles a task on the result.json its child wrote, adopting a
// validated one the child never renamed. It is the one place a result enters
// the record — the beat and `/complete` both come here (D15).
func (b *Broker) collect(ctx context.Context, r Record) (bool, error) {
	result, _, err := b.Tasks.ReadResult(r.ID)
	if errors.Is(err, taskdir.ErrNoResult) {
		// A validated result its child never renamed is published here, and
		// only when the marker still binds the exact bytes on disk. Age is not
		// consent, which is why the marker carries a hash and not a timestamp.
		ready, body, ok := b.Tasks.ReadReady(r.ID)
		if !ok || !b.authentic(ctx, r, ready) {
			return false, taskdir.ErrNoResult
		}
		// Create-if-absent (D16): a result.json the child renamed into place
		// meanwhile is the child's, and adoption stands aside for it.
		if err := b.Tasks.AdoptReady(r.ID, body); err != nil && !errors.Is(err, taskdir.ErrResultExists) {
			return false, err
		}
		result, _, err = b.Tasks.ReadResult(r.ID)
	}
	if err != nil {
		if errors.Is(err, taskdir.ErrNoResult) {
			return false, err
		}
		return false, fmt.Errorf("%w: %v", errResultUnreadable, err)
	}
	if !b.authentic(ctx, r, result) {
		return false, errResultNotAuthentic
	}
	result.Secret = ""
	if _, err := b.Settle(ctx, r.ID, SettleState(result.Status), "", &result); err != nil {
		return false, err
	}
	return true, nil
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

// spawnVerdict decides what four minutes without a signed receipt means
// (docs/design-decisions.md D11).
//
// It used to mean `spawn_failed` on its own, and that was wrong in the ordinary
// case: the briefing tells a child **not** to send heartbeat notes, so a
// healthy child that is simply working said nothing, and a task that had
// started perfectly well was recorded as one that never started. Then it meant
// "the tab is not in a reading that saw some terminal", which was wrong the
// other way: on this Mac the iTerm2 half of every reading fails, so a tmux
// listing that failed too still left a reading with iTerm rows in it, and a
// live child in tmux could be called gone on no evidence about tmux at all.
//
// So it is decided only on positive evidence, and each piece has an owner:
//
//   - the tab is absent, **and the source that owns it** — tmux for a tmux
//     child — answered completely: spawn_failed;
//   - the tab is there and holding a dialog: spawn_failed. The briefing could
//     not be typed at it, and a keystroke would have answered the dialog;
//   - the broker never typed the briefing (Record.Unbriefed): spawn_failed,
//     whatever the reading says. That is not a reading at all — the secret
//     never left the broker and is not kept, so no child can ever sign. The
//     dispatch settles such a task itself; this is the case where that
//     settlement was not written;
//   - anything else — a source that failed, a tab that is there and quiet, a
//     tab showing a shell — decides nothing. The task's own timeout is the
//     backstop. Wrongly calling a live child dead costs somebody's work; a
//     missed verdict costs a wait.
//
// Measured on a Mac whose iTerm2 listing never completes: an iTerm2 child the
// broker could not brief fell through every case above but the last, and
// ended twelve minutes later as `timeout`, saying nothing of why.
func spawnVerdict(present, sourceComplete, choosing bool, r Record) (State, string, bool) {
	switch {
	case !present && sourceComplete:
		return StateSpawnFailed, "The child session did not reach a prompt within 4 minutes, and its terminal " +
			"answered completely that the tab is gone. If several sessions were starting at once, they were " +
			"competing for this Mac.", true
	case present && choosing:
		return StateSpawnFailed, "The child session is holding a dialog four minutes after it opened, " +
			"so the briefing was never typed: answering that dialog is a person's decision, not this broker's.", true
	case r.Unbriefed:
		return StateSpawnFailed, unbriefedVerdict(r), true
	}
	return "", "", false
}

// runClocks is the two deadlines. It answers true when the task became
// terminal.
func (b *Broker) runClocks(ctx context.Context, rd reading, r Record, p *Pulse) bool {
	now := b.now()
	if r.State == StateSpawning && !r.SpawnedAt.IsZero() && now.Sub(r.SpawnedAt) > readyLimit {
		_, present := rd.session(r.ChildTerminalID)
		if r.ChildTerminalID == "" {
			present = false
		}
		choosing := false
		if present && b.Screen != nil {
			if screen, ok := b.Screen(ctx, r.ChildTerminalID); ok {
				choosing = Choosing(screen)
			}
		}
		// Completeness is asked of the tab's own source, not of the whole
		// reading (D05 ③). The whole reading is never complete on a Mac whose
		// iTerm2 cannot be asked, and "the reading saw something" says nothing
		// about the one source that could have seen this tab.
		complete := r.ChildTerminalID != "" && rd.sourceComplete(r.ChildBackend)
		if state, why, decided := spawnVerdict(present, complete, choosing, r); decided {
			// The session it opened is closed after the verdict is durable,
			// and the verdict records that it is owed (closeChild).
			if _, ids, err := b.settle(ctx, r.ID, state, why, nil, b.closeChild(r, present)...); err == nil {
				p.SpawnFail++
				for _, res := range b.runRecorded(ctx, ids) {
					if res.state == store.EffectDone && res.outcome == "closed" {
						p.Closed++
					}
				}
				return true
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

// closeChild is the effect that closes what a spawn_failed dispatch opened
// (D11), and only what it can prove it opened — nil when there is nothing
// provably ours to close.
//
// The proof is the pane: the record holds the id tmux gave back when this
// broker made it, and the adapter closes the session named for this task only
// when that pane is still one of its panes. A tab that has already gone left
// nothing provably ours behind, and a session that merely has our name is not
// proof — a second task whose id shares the first eight characters is refused
// that name by tmux, and closing by name would close the first task's tab. The
// Swift app's `3e37e8ec` is why this is done at all: a tab left open was the
// next spawn's failure.
//
// It is an outbox effect (G14): recorded in the transaction that settles the
// task, run after it, and — if this process dies in between — run by the next
// broker, once, rather than never.
func (b *Broker) closeChild(r Record, present bool) []store.Effect {
	if !present || r.ChildBackend != "tmux" || r.ChildTerminalID == "" || b.Launcher == nil {
		return nil
	}
	payload, _ := json.Marshal(closeChildEffect{Pane: r.ChildTerminalID, Session: ChildSessionName(r.ID)})
	return []store.Effect{{Kind: EffectCloseChild, Subject: r.ID, Payload: payload}}
}

// settleOrphan settles a task still `queued` whose dispatcher has provably
// gone. Only that process ever held the plaintext secret, so nothing can brief
// the task now; leaving it to its timeout would hold its claims for hours
// against a tab that will never open. "Provably" is the store's answer that
// the process is not running — an owner this Mac cannot account for is left
// alone (DG-7).
func (b *Broker) settleOrphan(ctx context.Context, r Record) bool {
	if r.State != StateQueued || r.Dispatcher == "" || !b.Store.OwnerGone(r.Dispatcher) {
		return false
	}
	_, err := b.Settle(ctx, r.ID, StateSpawnFailed,
		"The daemon that admitted this task stopped before it opened the tab. Its secret is never kept at rest, "+
			"so no broker can brief it now; dispatch it again.", nil)
	return err == nil
}
