package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/lane"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// Telling the root its child finished, and keeping at it until somebody says
// they saw it.
//
// The Swift app's measured lesson is in three clauses:
//
//   - **A transport success is not an observation.** Bytes reached a composer;
//     nothing says a turn read them. So a successful send reschedules rather
//     than stops, and only an acknowledgement ends the sequence.
//   - **The sequence ends anyway.** Eight attempts, then dead letter. A notice
//     that retries for ever is one nobody has to answer.
//   - **Never type into a session showing a menu.** The keystroke would answer
//     the menu instead, which is how "Tea" once became "Water".
//
// Three things this broker learned after that, all of them from one report — a
// person who watched the same "task finished" arrive four and five times and
// asked whether the work had been dispatched twice:
//
//   - **The delivered wait is not the failure wait.** Both used the 5-second
//     ladder, so a root busy integrating its child was told again five seconds
//     later, and again ten seconds after that. The delivered case has its own
//     ladder now (AckWaitDelay, record.go), whose first rung is longer than a
//     turn.
//   - **A hold is not an attempt, and is not silence either.** A menu, a busy
//     lane and an occupied composer are all reasons nothing was typed and
//     nobody is at fault; spending the budget on them would walk a notice to
//     dead letter for being polite. They are recorded as the notice's last
//     error all the same — once per hold, not once per pass — so a person
//     reading `GET /v1/orchestrator/completions` can see what this machine is
//     sitting on and why.
//   - **Evidence beats a receipt.** A root that landed the task's work knew it
//     had finished; asking it to also curl the ACK route, and typing the line
//     again until it does, is noise about something already done (NoticeSeen).

var zeroTime time.Time

// noticeBody is the `clawdline.notice` object, whose key set is closed and
// whose encoding must be one physical line: a newline inside it would split the
// message into two things the far side cannot parse.
type noticeBody struct {
	Protocol string     `json:"protocol"`
	Version  int        `json:"version"`
	Kind     string     `json:"kind"`
	Audience string     `json:"audience"`
	Task     noticeTask `json:"task"`
	State    string     `json:"state"`
	Result   string     `json:"result_path"`
	// Outstand is how many other tasks of the root being told are still
	// running. It is the Swift app's field (`Orchestrator.swift:8661`:
	// `liveTasks(under: [parentTaskId]).count`), whose parent is a task
	// where this daemon's is a root session, and until now nothing here
	// wrote it — so every completion notice this broker had ever sent said
	// `"outstanding": 0`, which reads as "nothing else of yours is running"
	// and meant "nobody counted" (work-system-review §2.2). It counts the
	// live rows this broker can decode; a row it cannot decode is named as
	// unreadable wherever tasks are listed and is not counted here.
	Outstand int `json:"outstanding"`
	// Leftovers is how many things this delivery says it did not do. The list
	// itself is in result.json and on the task, which the root is being told
	// to read; what belongs in one line typed at a session is the number and
	// the fact that nothing happens to them until somebody acts.
	Leftovers int    `json:"leftovers,omitempty"`
	Released  bool   `json:"claims_released"`
	MayWrite  bool   `json:"child_may_still_write"`
	Body      string `json:"body"`
	NoticeID  string `json:"notice_id"`
	AckPath   string `json:"ack_path"`
}

type noticeTask struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// FinishedLine is the sentence a person reads in the root's tab.
func (b *Broker) FinishedLine(r Record, noticeID string) string {
	short := r.ID
	if len(short) > 8 {
		short = short[:8]
	}
	resultPath := filepath.Join(b.Tasks.Path(r.ID), "result.json")
	line := fmt.Sprintf("[clawdline] task %s (%s) finished: %s — read %s",
		short, r.Title, r.State, resultPath)
	if r.State == StateTimeout && len(r.Lease()) > 0 {
		line += " — claims released; child tab may still be writing"
	}
	if r.Stalled() {
		line += fmt.Sprintf(" — it stalled: briefed, never signed, idle through one nudge; "+
			"POST /v1/orchestrator/tasks/%s/respawn opens a copy", r.ID)
	}
	if r.Landing != nil && r.Landing.State == LandingPending {
		line += " — " + landingLine(r)
	}
	// The moment this whole path exists for. A root integrating a child has
	// just read what it did not do, and until now the only place that went
	// was the root's memory: every Backlog row on this machine was written in
	// a moment somebody complained, never in this one. The line says what is
	// there and what raising one costs; nothing is created by saying nothing.
	if n := len(leftoversOf(r)); n > 0 {
		line += fmt.Sprintf(" — it says it did not do %d thing(s) (result.leftovers); to put one to the person, "+
			`POST /v1/orchestrator/proposals {"session_id":"<yours>","task_id":"%s","leftover":"<its title>"}`+
			" — they answer track, later (Backlog) or no, and nothing reaches their board until they do",
			n, r.ID)
	}
	if noticeID != "" {
		line += fmt.Sprintf(" — after observing, ACK notice %s at /v1/orchestrator/tasks/%s/completion/ack",
			noticeID, r.ID)
	}
	return line
}

// landingLine is the one sentence the completion notice spends on the landing
// this delivery has just opened.
//
// **It used to ask for an action that had already happened.** "claimed work
// may still be in the shared tree; mark landing pending so other roots can see
// it" was typed at a root whose child's landing the broker had itself just
// opened as pending — so the only thing it asked for was already done, and the
// one thing worth saying was left unsaid. Measured on 2026-09-20: sixteen
// deliveries were refused `nothing_delivered` four hours after this line went
// out, every one of them for a branch that was empty at the moment it was
// typed and that nobody had been told was empty (work-system-review §2.3,
// W1-a; G3: knowing and not saying is the most expensive silence here).
//
// So it says what this broker knew at that moment and nothing else, and for an
// empty branch it says it while the checkout is still on disk — which is the
// whole point of saying it now. It never refuses the delivery and it never
// commits anything itself: rejecting finished work does not bring the work
// back, and a daemon committing somebody's changes is an irreversible act it
// has no standing to take (§2.3, the two paths deliberately not taken).
func landingLine(r Record) string {
	branch, path := "", ""
	if r.Worktree != nil {
		branch, path = r.Worktree.Branch, r.Worktree.Path
	}
	switch r.Landing.Settlement {
	case SettlementEmpty:
		return "nothing is committed on its delivery branch " + branch + ", and a landing is proved from that " +
			"branch — so as it stands there is nothing this task could ever be recorded as landing. Its checkout " +
			path + " is still on disk: commit there, on that branch, now. Once the sweep takes the checkout the " +
			"only records left for it are abandoned and nothing_to_land"
	case SettlementCarried:
		return "its delivery is committed on branch " + branch + "; merge that branch into its target and record " +
			"the landing with the commit that carries it"
	case SettlementUnreadable:
		return "its delivery branch " + branch + " could not be read when it ended, so whether anything was " +
			"committed is not known; look at the branch before recording this landing"
	}
	return "it wrote the shared checkout under its claims; record the landing with the commit that carries that " +
		"work onto its target, or abandoned"
}

// outstandingFor is how many other tasks of one root are still running: the
// `outstanding` field, which nothing wrote until now.
//
// A store that cannot answer refuses rather than answering 0. The number
// would be indistinguishable from "nothing else of yours is running", which
// is the sentence this field existed to say wrongly; the attempt is spent as
// any other failure to put the line on the root's screen is, and retried.
func (b *Broker) outstandingFor(ctx context.Context, r Record) (int, error) {
	if r.Root == nil || r.Root.SessionID == "" {
		return 0, nil
	}
	live, err := b.liveTasks(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, other := range live {
		if other.ID == r.ID || other.Root == nil {
			continue
		}
		if other.Root.SessionID == r.Root.SessionID {
			n++
		}
	}
	return n, nil
}

// NoticeWire is the whole line typed into the root, wrapper and all.
func (b *Broker) NoticeWire(ctx context.Context, r Record) (string, error) {
	if r.Notice == nil {
		return "", fmt.Errorf("this task has no completion envelope")
	}
	outstanding, err := b.outstandingFor(ctx, r)
	if err != nil {
		return "", fmt.Errorf("this machine could not count the root's other running tasks: %w", err)
	}
	body := noticeBody{
		Protocol:  NoticeProtocol,
		Version:   NoticeVersion,
		Kind:      NoticeKind(r),
		Audience:  "root",
		Task:      noticeTask{ID: r.ID, Title: r.Title},
		State:     string(r.State),
		Result:    filepath.Join(b.Tasks.Path(r.ID), "result.json"),
		Outstand:  outstanding,
		Leftovers: len(leftoversOf(r)),
		Released:  r.State == StateTimeout && len(r.Lease()) > 0,
		MayWrite:  r.State == StateTimeout && len(r.Lease()) > 0,
		Body:      b.FinishedLine(r, r.Notice.ID),
		NoticeID:  r.Notice.ID,
		AckPath:   "/v1/orchestrator/tasks/" + r.ID + "/completion/ack",
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	if strings.ContainsAny(string(encoded), "\n\r") {
		return "", fmt.Errorf("the notice could not be encoded safely")
	}
	return "<clawdline-notice>" + string(encoded) + "</clawdline-notice>", nil
}

// leftoversOf is what a task's delivery said it did not do, or nothing at all
// for a task that has no result — a timeout, a tab that never opened. A
// broker's own verdict never names leftovers: it is not the child speaking.
func leftoversOf(r Record) []work.Leftover {
	if r.Result == nil {
		return nil
	}
	return r.Result.Leftovers
}

// noticeOf is a ledger row as the broker reads it.
func noticeOf(n store.BrokerNotice) *Notice {
	out := &Notice{
		ID:             n.ID,
		State:          NoticeState(n.State),
		Attempts:       n.Attempts,
		CreatedAt:      n.CreatedAt,
		LastAttemptAt:  n.LastAttemptAt,
		NextRetryAt:    n.NextRetryAt,
		DeliveredAt:    n.DeliveredAt,
		ObservedAt:     n.ObservedAt,
		AcknowledgedAt: n.AcknowledgedAt,
		DeadLetterAt:   n.DeadLetterAt,
		Recipient:      n.Recipient,
	}
	if n.ErrorCode != "" {
		out.LastError = &NoticeError{Code: n.ErrorCode, Message: n.ErrorMessage, At: n.ErrorAt}
	}
	return out
}

// noticeRow is a notice as the ledger stores it.
func noticeRow(taskID string, n Notice) store.BrokerNotice {
	row := store.BrokerNotice{
		ID: n.ID, TaskID: taskID, State: string(n.State), Attempts: n.Attempts,
		CreatedAt: n.CreatedAt, LastAttemptAt: n.LastAttemptAt, NextRetryAt: n.NextRetryAt,
		DeliveredAt: n.DeliveredAt, ObservedAt: n.ObservedAt, AcknowledgedAt: n.AcknowledgedAt,
		DeadLetterAt: n.DeadLetterAt, Recipient: n.Recipient,
	}
	if n.LastError != nil {
		row.ErrorCode, row.ErrorMessage, row.ErrorAt = n.LastError.Code, n.LastError.Message, n.LastError.At
	}
	return row
}

// sameNotice compares two readings of one envelope as the ledger would store
// them, which is to the second: a record read back from the ledger has lost
// the nanoseconds the value it was made from had.
func sameNotice(a, b Notice) bool {
	return sameStored(noticeRow("", a), noticeRow("", b))
}

func sameStored(a, b store.BrokerNotice) bool {
	sec := func(t time.Time) int64 {
		if t.IsZero() {
			return 0
		}
		return t.Unix()
	}
	return a.ID == b.ID && a.State == b.State && a.Attempts == b.Attempts &&
		sec(a.CreatedAt) == sec(b.CreatedAt) && sec(a.LastAttemptAt) == sec(b.LastAttemptAt) &&
		sec(a.NextRetryAt) == sec(b.NextRetryAt) && sec(a.DeliveredAt) == sec(b.DeliveredAt) &&
		sec(a.ObservedAt) == sec(b.ObservedAt) && sec(a.AcknowledgedAt) == sec(b.AcknowledgedAt) &&
		sec(a.DeadLetterAt) == sec(b.DeadLetterAt) && a.Recipient == b.Recipient &&
		a.ErrorCode == b.ErrorCode && a.ErrorMessage == b.ErrorMessage && sec(a.ErrorAt) == sec(b.ErrorAt)
}

// moveNotice is the one way a notice changes after it is opened: a
// compare-and-set against the state and attempt count its writer read, with
// the event that explains it. It answers whether the move happened; false
// means somebody moved the notice first and this decision no longer applies.
func (b *Broker) moveNotice(ctx context.Context, taskID string, seen Notice, kind string, change func(n *Notice)) bool {
	return b.moveNoticeWith(ctx, taskID, seen, kind, nil, change)
}

// moveNoticeWith is moveNotice with fields of its own on the event, for a move
// whose reason is not in the notice itself.
func (b *Broker) moveNoticeWith(ctx context.Context, taskID string, seen Notice, kind string, extra map[string]any, change func(n *Notice)) bool {
	next := seen
	if seen.LastError != nil {
		e := *seen.LastError
		next.LastError = &e
	}
	change(&next)
	fields := map[string]any{
		"task": taskID, "notice": seen.ID, "attempt": next.Attempts, "state": next.State,
		"error": errorCode(next.LastError),
	}
	for k, v := range extra {
		fields[k] = v
	}
	payload, _ := json.Marshal(fields)
	// A notice entering dead letter owes the person a push (D24): the one
	// line of this machine that nobody acknowledged is exactly what a person
	// must hear about, and the root it was for has not. It is recorded as
	// intent in the move's own transaction and sent after it commits.
	var effects []store.Effect
	if next.State == NoticeDeadLetter && seen.State != NoticeDeadLetter {
		body, _ := json.Marshal(deadLetterEffect{Notice: seen.ID, Attempts: next.Attempts, Reason: errorCode(next.LastError)})
		effects = append(effects, store.Effect{Kind: EffectDeadLetterPush, Subject: taskID, Payload: body})
	}
	applied, ids, err := b.Store.UpdateBrokerNoticeWith(ctx,
		store.NoticeExpect{State: string(seen.State), Attempts: seen.Attempts},
		noticeRow(taskID, next),
		[]store.Event{{Kind: kind, Subject: taskID, Payload: payload}}, effects)
	if err != nil {
		log.Printf("orchestrator: notice %s for task %s could not be recorded: %v", seen.ID, taskID, err)
		return false
	}
	if applied && len(ids) > 0 {
		// Not on this pass. A push is a request to a push service with its
		// own retries, up to half a minute; the beat that decided the dead
		// letter would be that much late, and three ticks late is `stalled`
		// on /v1/health — an alarm about the beat raised by a phone's
		// provider. The intent is already durable: a process that dies first
		// leaves the effect for RecoverEffects.
		go func() {
			pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deadLetterPushDeadline)
			defer cancel()
			b.runRecorded(pctx, ids)
			if b.pushed != nil {
				b.pushed()
			}
		}()
	}
	return applied
}

// deadLetterPushDeadline bounds the one push a dead letter owes, retries and
// all.
const deadLetterPushDeadline = 2 * time.Minute

func errorCode(e *NoticeError) string {
	if e == nil {
		return "none"
	}
	return e.Code
}

// PumpNotices makes one pass over the notices that are due.
//
// At most eight per pass, oldest deadline first, so a machine that was asleep
// for an hour does not type sixty lines into one composer the moment it wakes.
func (b *Broker) PumpNotices(ctx context.Context) int {
	now := b.now()
	due, err := b.Store.DueBrokerNotices(ctx, now, 32)
	if err != nil {
		return 0
	}
	sent := 0
	tried := 0
	for _, n := range due {
		if tried >= 8 {
			break
		}
		// A root that was showing a menu is asked again after its deferral,
		// which lives in memory: "it was still choosing ten seconds ago" is
		// an observation, and writing it down every pass is the Swift app's
		// six-megabyte clock over again.
		if b.observed.deferredUntil(n.ID, now).After(now) {
			continue
		}
		r, _, err := b.Record(ctx, n.TaskID)
		if err != nil || r.Notice == nil || r.Notice.ID != n.ID {
			continue
		}
		tried++
		if b.attemptNotice(ctx, r) {
			sent++
		}
	}
	b.retypeDeadLetters(ctx, now)
	return sent
}

// attemptNotice delivers one notice, or records why it did not.
//
// The typing happens outside any lock, because it takes seconds and holds a
// terminal. What it learned is then recorded only if the notice is still in
// the state and at the attempt count it was read at: an ACK that lands while
// this is typing wins, and the attempt is discarded rather than written back
// over it — by the ledger's compare-and-set, not by anybody remembering to
// re-read.
func (b *Broker) attemptNotice(ctx context.Context, r Record) bool {
	seen := *r.Notice
	at := b.now()

	if seen.Attempts >= AttemptLimit {
		b.moveNotice(ctx, r.ID, seen, "task.completion.dead_letter", func(n *Notice) {
			deadLetter(n, at)
		})
		return false
	}

	if r.Root == nil {
		return b.spendAttempt(ctx, r.ID, seen, at, "root_missing", "This task has no root to tell.")
	}
	target, err := b.terminalFor(ctx, r.Root.SessionID, r.Root.Assistant)
	if err != nil {
		if ref, ok := err.(Refusal); ok && ref.Code == "conversation_ambiguous" {
			return b.spendAttempt(ctx, r.ID, seen, at, "conversation_ambiguous", ref.Message)
		}
		return b.spendAttempt(ctx, r.ID, seen, at, "root_missing",
			"The root session is not on this machine right now.")
	}
	// A root showing a chooser would have the notice typed into its menu.
	// Waiting costs a pass; typing costs an answer nobody gave.
	if b.Choosing != nil && b.Choosing(ctx, target.ID) {
		return b.holdNotice(ctx, r.ID, seen, at, holdChoosing)
	}
	state, screen := b.readComposer(ctx, target.ID, r.Root.Assistant)
	var prepare Prepare
	composer := ""
	switch state {
	case ComposerDraft:
		// A root whose composer holds a half-written line would have the notice
		// appended to it and submitted with it. Unless it is Claude Code's own
		// suggestion, or a draft its stash can keep (stash.go) — which is only
		// asked of text that stood still for a whole hold, because a person
		// still typing is somebody whose next keystroke lands after the stash.
		text := composerHolds(screen, session.Assistant(r.Root.Assistant))
		if b.observed.sawDraft(seen.ID, text) != text || !b.mayStash(r.Root.Assistant) {
			return b.holdNotice(ctx, r.ID, seen, at, holdComposer)
		}
		prepare = stashDraft(text, &composer)
	case ComposerQueued:
		// Something is already waiting to be read there. Whether that matters
		// depends on whether it is this notice: a copy typed once and not yet
		// reached is exactly what the person saw four times, and a second copy
		// behind it changes nothing except how many they read. A notice never
		// delivered queues behind whatever that is, which is how it gets read.
		if !seen.DeliveredAt.IsZero() {
			return b.holdNotice(ctx, r.ID, seen, at, holdQueued)
		}
	}
	wire, err := b.NoticeWire(ctx, r)
	if err != nil {
		return b.spendAttempt(ctx, r.ID, seen, at, "transport_failed", err.Error())
	}
	if b.Type == nil {
		return b.spendAttempt(ctx, r.ID, seen, at, "transport_failed",
			"this daemon cannot type into a terminal")
	}
	if err := b.typeNotice(ctx, target.ID, wire, prepare); err != nil {
		// A hold for the same reason as a chooser: the root's terminal was
		// being written to by somebody else (its lane, D22), and nothing was
		// typed.
		var busy lane.Busy
		if errors.As(err, &busy) {
			return b.holdNotice(ctx, r.ID, seen, at, holdLane)
		}
		// The look inside the lane did not clear the composer, and nothing
		// was typed: the draft is held for as it was before stash.go.
		if errors.Is(err, errStashWithheld) {
			return b.holdNotice(ctx, r.ID, seen, at, holdComposer)
		}
		return b.spendAttempt(ctx, r.ID, seen, at, "transport_failed", err.Error())
	}

	var how map[string]any
	if composer != "" {
		how = map[string]any{"composer": composer}
	}
	b.moveNoticeWith(ctx, r.ID, seen, "task.completion.delivered", how, func(n *Notice) {
		n.Attempts++
		n.LastAttemptAt = at
		n.State = NoticeDelivered
		n.Recipient = target.ID
		n.LastError = nil
		if n.DeliveredAt.IsZero() {
			n.DeliveredAt = at
		}
		// Armed again on purpose: delivered is not observed. On the other
		// ladder, though — what this one waits for is somebody finishing a
		// turn and answering, and nothing about that is helped by asking
		// again inside a minute.
		n.NextRetryAt = at.Add(AckWaitDelay(n.Attempts))
	})
	b.observed.forgetDraft(seen.ID)
	return true
}

// spendAttempt records one attempt that put nothing on the root's screen, and
// arms the next on the failure ladder.
func (b *Broker) spendAttempt(ctx context.Context, taskID string, seen Notice, at time.Time, code, message string) bool {
	b.moveNotice(ctx, taskID, seen, "task.completion.attempt", func(n *Notice) {
		n.Attempts++
		n.LastAttemptAt = at
		n.LastError = &NoticeError{Code: code, Message: message, At: at}
		// The Swift app's rule: the attempt that uses the last of the budget
		// ends the sequence then, rather than one ladder rung later.
		if n.Attempts >= AttemptLimit {
			n.State = NoticeDeadLetter
			n.DeadLetterAt = at
			n.NextRetryAt = zeroTime
			return
		}
		n.NextRetryAt = at.Add(RetryDelay(n.Attempts))
	})
	return false
}

// noticeHold is a reason nothing was typed that is nobody's fault and that the
// next look may find gone.
type noticeHold struct {
	Code    string
	Message string
}

var (
	holdChoosing = noticeHold{"root_choosing",
		"The root is showing something waiting to be answered; a line typed at it would answer that instead."}
	holdLane = noticeHold{"terminal_busy",
		"Somebody else is writing to the root's terminal; nothing was typed."}
	holdComposer = noticeHold{"composer_occupied",
		"The root's composer already holds something; a line typed at it would be appended to that and submitted with it."}
	holdQueued = noticeHold{"notice_queued",
		"A copy of this notice is already waiting to be read in the root's composer."}
)

// noticeHoldStep is how long one hold lasts before the root is looked at again.
// Short, because what it is waiting for — a menu answered, a lane released, a
// draft sent — is over in seconds and the notice should go the moment it is.
const noticeHoldStep = 20 * time.Second

// maxNoticeHold is how long one notice may be held for one reason before the
// hold stops being free.
//
// A hold has to be bounded, and both ways of bounding it are wrong on their
// own: holding for ever is how a notice quietly never arrives, and typing
// anyway is the draft this hold exists to protect. So at the limit the hold
// spends an attempt instead — the record says the machine tried and could not,
// the budget walks toward dead letter as it does for any other reason nothing
// was typed, and the person is pushed when it gets there. Ten minutes: three
// rungs of the acknowledgement ladder, and somebody who has not touched a
// half-written line in ten minutes is not about to. A menu is the exception,
// with a ceiling of its own (maxChoosingHold).
const maxNoticeHold = 10 * time.Minute

// maxChoosingHold is how long one notice may be held because its root is
// showing something waiting to be answered.
//
// A menu is not like the other holds. A lane is released in seconds and a draft
// is sent or abandoned in minutes, but a question is answered when its person
// comes back, and on 2026-09-25 that was two hours and fourteen minutes: a root
// showed a question from 19:54 to 22:08, its child finished at 20:07, and ten
// minutes of holding at a time spent all eight attempts by 21:27. The notice
// was a dead letter when the person answered, and the root carried on not
// knowing. So a menu holds for free, for hours — nobody is at fault and nothing
// was tried — and at this ceiling the notice goes to dead letter in one step,
// saying so, with the push that owes the person. Twelve hours covers a night
// away from the machine; past it the person is told rather than kept waiting
// on a machine that is. A dead letter is still typed once more when its root
// next reads idle (retypeDeadLetters).
const maxChoosingHold = 12 * time.Hour

// holdNotice defers one notice without spending an attempt, and writes the
// reason the first time it is held for it. It always answers false: nothing was
// sent.
func (b *Broker) holdNotice(ctx context.Context, taskID string, seen Notice, at time.Time, h noticeHold) bool {
	held := seen.LastError != nil && seen.LastError.Code == h.Code
	if held && h == holdChoosing && at.Sub(seen.LastError.At) >= maxChoosingHold {
		since := seen.LastError.At
		b.moveNotice(ctx, taskID, seen, "task.completion.dead_letter", func(n *Notice) {
			n.State = NoticeDeadLetter
			n.DeadLetterAt = at
			n.NextRetryAt = zeroTime
			n.LastError = &NoticeError{Code: h.Code, At: since,
				Message: h.Message + " Held since then without spending an attempt, up to its ceiling of " +
					maxChoosingHold.String() + "; it is typed once more when the root next reads idle."}
		})
		return false
	}
	if held && h != holdChoosing && at.Sub(seen.LastError.At) >= maxNoticeHold {
		return b.spendAttempt(ctx, taskID, seen, at, h.Code,
			h.Message+" Held for "+maxNoticeHold.String()+" for this reason, so this attempt is spent rather than held again.")
	}
	if !held {
		// Once per hold, not once per pass: a clock written down every pass is
		// the Swift app's six-megabyte file over again (observe.go). What the
		// row carries afterwards is the reason and when the hold began, which
		// is what both the limit above and a person reading the ledger need.
		b.moveNotice(ctx, taskID, seen, "task.completion.held", func(n *Notice) {
			n.LastError = &NoticeError{Code: h.Code, Message: h.Message, At: at}
			n.NextRetryAt = at.Add(noticeHoldStep)
		})
	}
	b.observed.deferNotice(seen.ID, at.Add(noticeHoldStep))
	return false
}

// maxRetypeAge is how old a dead letter may be and still be typed once more
// when its root reads idle. A day: the case this exists for is a root that was
// busy when its notice gave up and is back within the same working day. Older
// ones stay listed — on the completions list and the root's own session-todos —
// and are re-armed only by a person (Rearm), so a root that has been open for a
// week is not handed last week's notices the first time it goes quiet.
const maxRetypeAge = 24 * time.Hour

// retypeDeadLetters types each recent dead letter once more, the first time
// its root reads idle.
//
// A dead letter is the end of the ladder, and until now it stayed dead until
// somebody called reconcile — which nobody did, because the root it was for had
// never heard of it. An idle root is the one moment typing costs nothing: no
// menu to answer by accident, no turn to interrupt, an empty composer. So the
// notice is typed there once, the attempt is recorded (attempts past the limit
// is the durable mark that it was spent), and the notice stays a dead letter:
// no second push, still listed, still acknowledged the ordinary way. Anything
// short of idle — not live, working, a menu, a draft, a lane somebody else
// holds — waits for a later look without spending it.
func (b *Broker) retypeDeadLetters(ctx context.Context, now time.Time) {
	rows, err := b.Store.CompletionNotices(ctx, zeroTime, false, completionsPage)
	if err != nil {
		return
	}
	var live []session.Session
	read := false
	for _, row := range rows {
		n := noticeOf(row)
		if n.State != NoticeDeadLetter || n.Attempts > AttemptLimit || now.Sub(n.DeadLetterAt) > maxRetypeAge {
			continue
		}
		if b.observed.deferredUntil(n.ID, now).After(now) {
			continue
		}
		r, _, err := b.Record(ctx, row.TaskID)
		if err != nil || r.Notice == nil || r.Notice.ID != n.ID || r.Root == nil || r.Root.SessionID == "" {
			continue
		}
		if !read {
			if b.Live != nil {
				live = b.Live(ctx)
			}
			read = true
		}
		if !b.retypeIfIdle(ctx, r, live, now) {
			b.observed.deferNotice(n.ID, now.Add(noticeHoldStep))
		}
	}
}

// retypeIfIdle types one dead letter at its root if that root reads idle now,
// answering whether it was typed.
func (b *Broker) retypeIfIdle(ctx context.Context, r Record, live []session.Session, now time.Time) bool {
	var target session.Session
	found := 0
	for _, s := range live {
		if s.IsAssistant() && s.ConversationID == r.Root.SessionID &&
			(r.Root.Assistant == "" || string(s.Assistant) == r.Root.Assistant) {
			target = s
			found++
		}
	}
	if found != 1 || target.State != session.StateIdle || b.Type == nil {
		return false
	}
	if b.Choosing != nil && b.Choosing(ctx, target.ID) {
		return false
	}
	if state, _ := b.readComposer(ctx, target.ID, r.Root.Assistant); state != ComposerEmpty {
		return false
	}
	wire, err := b.NoticeWire(ctx, r)
	if err != nil {
		return false
	}
	seen := *r.Notice
	typeErr := b.typeNotice(ctx, target.ID, wire, nil)
	var busy lane.Busy
	if errors.As(typeErr, &busy) {
		return false
	}
	b.moveNotice(ctx, r.ID, seen, "task.completion.retyped", func(n *Notice) {
		n.Attempts = max(n.Attempts, AttemptLimit) + 1
		n.LastAttemptAt = now
		if typeErr != nil {
			n.LastError = &NoticeError{Code: "transport_failed", Message: typeErr.Error(), At: now}
			return
		}
		n.Recipient = target.ID
		if n.DeliveredAt.IsZero() {
			n.DeliveredAt = now
		}
	})
	return typeErr == nil
}

// RootCompletions is every completion notice nobody has acknowledged whose
// root is this conversation — pending, delivered or dead — newest first. It is
// the pull path: a root reads its own list at every turn boundary, so a line it
// never saw typed still reaches it at its next one.
func (b *Broker) RootCompletions(ctx context.Context, conversation string) ([]Completion, error) {
	all, err := b.Completions(ctx, false)
	if err != nil {
		return nil, err
	}
	out := []Completion{}
	for _, c := range all {
		if c.Notice.State == NoticeAcknowledged || c.Record.Root == nil || c.Record.Root.SessionID != conversation {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

// ResultPath is where a task's result.json is, as the notice names it.
func (b *Broker) ResultPath(taskID string) string {
	return filepath.Join(b.Tasks.Path(taskID), "result.json")
}

// readComposer asks the root's own screen what is in its composer.
//
// A daemon with no screen reader at all answers empty, as the briefing path
// does (composerReady): the machines where that is true are the ones this broker
// can only drive blind, and a notice that is never typed there is a promise
// broken to keep a draft that may not exist.
//
// A reader that is there and did not answer this time also answers empty, and
// the direction is the opposite of the briefing's on purpose. A briefing's
// Return can answer a workspace-trust dialog and kill the task; a notice typed
// onto a draft submits that draft, which is a person's sentence lost and not the
// machine's state corrupted. Weighed against holding every notice on a Mac whose
// root terminal cannot be captured — which would turn every completion into a
// dead letter and a push — the notice goes.
func (b *Broker) readComposer(ctx context.Context, terminalID, assistant string) (ComposerState, string) {
	if b.Screen == nil {
		return ComposerEmpty, ""
	}
	screen, ok := b.Screen(ctx, terminalID)
	if !ok {
		return ComposerEmpty, ""
	}
	return ReadComposer(screen, session.Assistant(assistant)), screen
}

// mayStash is whether a root's composer text may be put to Claude Code's stash
// (stash.go): a Claude Code root, a daemon that can press a key inside the
// send's lane turn, and a ctrl+s that still means what was measured.
func (b *Broker) mayStash(assistant string) bool {
	if session.Assistant(assistant) != session.AssistantClaude || b.TypePrepared == nil {
		return false
	}
	return b.StashRebound == nil || !b.StashRebound()
}

// typeNotice is typeLine, with prepare run first inside the same lane turn when
// there is one.
func (b *Broker) typeNotice(ctx context.Context, terminalID, text string, prepare Prepare) error {
	if prepare == nil {
		return b.typeLine(ctx, terminalID, text)
	}
	if err := outside(); err != nil {
		return err
	}
	return b.TypePrepared(ctx, terminalID, text, prepare)
}

// How the broker learns a root already knows, spelled once so a reader can see
// the whole set in one place.
const (
	SeenByLanding  = "landing"
	SeenByProposal = "leftover_proposal"
)

// NoticeSeen closes a completion notice on evidence instead of on a receipt.
//
// The chain is accepted, executed, delivered, observed, acknowledged, and the
// ACK route exists because a line typed into a terminal proves nothing about
// anybody having read it. But some things a root does are *better* evidence
// than the ACK curl is, and when the broker has one of those, typing the line
// again is noise about work already integrated — which is what the person
// actually complained about.
//
// What counts:
//
//   - **A landing recorded with the orchestrator token.** Saying where a
//     child's work went, or that there was none to land, is something only a
//     root that had read the child's delivery could say. The token is part of
//     the rule: a landing written with the *task* secret is the child's own
//     voice, and a child cannot observe on its root's behalf.
//   - **A leftover proposal naming this task from the root's own session.** The
//     line this broker types is what tells a root that route exists and what
//     its leftovers are called; writing one is reading it.
//
// What does not, and why the reasons are different:
//
//   - **Reading result.json.** Not weak evidence — invisible evidence. The
//     child writes the file and the root opens it, and nothing in between comes
//     through this daemon. A rung the broker cannot observe cannot be one it
//     acts on.
//   - **A GET of the task.** It names nobody. The console's own polling, the
//     Dashboard and any other holder of the machine token read that route, so
//     counting it would let this daemon silence its own notices by looking at
//     them.
//   - **A transport success.** The rule this whole file is built on.
//
// Best effort, and never an error to its caller: the landing is the fact, and a
// notice this could not move is left to the pump exactly as before.
func (b *Broker) NoticeSeen(ctx context.Context, taskID, how string) {
	if b.Store == nil {
		return
	}
	// Twice, for the same reason Acknowledge retries: the pump may move the
	// notice between the read and the compare-and-set, and what it learned
	// does not outrank this.
	for tries := 0; tries < 2; tries++ {
		r, _, err := b.Record(ctx, taskID)
		if err != nil || r.Notice == nil || r.Notice.State == NoticeAcknowledged {
			return
		}
		seen := *r.Notice
		now := b.now()
		if b.moveNoticeWith(ctx, taskID, seen, "task.completion.observed",
			map[string]any{"how": how}, func(n *Notice) {
				if n.ObservedAt.IsZero() {
					n.ObservedAt = now
				}
				n.AcknowledgedAt = now
				n.State = NoticeAcknowledged
				n.NextRetryAt = zeroTime
				n.LastError = nil
			}) {
			b.observed.forgetNotice(seen.ID)
			return
		}
	}
}

func deadLetter(n *Notice, at time.Time) {
	n.State = NoticeDeadLetter
	n.DeadLetterAt = at
	n.NextRetryAt = zeroTime
	n.LastError = &NoticeError{
		Code:    "acknowledgement_timeout",
		Message: "No root acknowledgement arrived within the bounded retry budget.",
		At:      at,
	}
}
