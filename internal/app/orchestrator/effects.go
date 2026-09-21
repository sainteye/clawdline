package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/terminal"
	"github.com/sainteye/clawdline-go/internal/app/lane"
)

// Effects: what the broker does to the world, and the rule that it is never
// done while holding the write right (D08).
//
// Every effect below is recorded as an outbox row in the transaction that
// owes it and run after that transaction commits — by the request that
// recorded it, at once, or, when that process died first, by the next broker
// on the same store (RecoverEffects). The row says whether it was ever
// attempted, and that is what lets a recovery run it exactly once: an effect
// never begun is run; one begun and unfinished is run again only when it can
// find out for itself whether it already happened, and is otherwise recorded
// as unknown rather than done twice.
//
// **An effect outlives the request that asked for it.** It used to run on the
// caller's context, and a caller is a browser tab somebody closes. The line
// was typed into the far session — that part of it reaches the world through
// a terminal, not through the HTTP connection — and then the cancelled context
// failed the store write that would have recorded it, so the bytes were in
// somebody's composer and the account of them said `pending`. A resend under
// the same Idempotency-Key found an outbox row that was open and a receipt
// that was held, and was told `request_in_progress`; and because the row's
// owner was this very process, still running, no recovery would ever take it.
// `request_in_progress` was the last thing that key ever answered.
//
// The five words the fix keeps apart, because the old shape had them as one:
//
//   - **accepted** — the intent is durable. The request may go home now; this
//     is what its answer is about, and it is true whether or not anybody is
//     still listening.
//   - **executed** — the effect ran here. Bounded by effectLifetime and by
//     nothing the caller does.
//   - **delivered** — the far side took it: the composer accepted the line.
//   - **observed** and **acknowledged** — the receiver read it, and acted. No
//     route here can claim either; the evidence is the receiver's own next
//     turn, and this daemon does not watch for it. They are named so that
//     nothing below quietly reports `delivered` as though it were one of them.

// The effect kinds.
const (
	// EffectWorktree makes a task's isolated checkout. Before W2 it was made
	// before the task existed, so a crash between the two left a checkout no
	// record named (G14).
	EffectWorktree = "worktree.add"
	// EffectCloseChild closes the tab a child was given: a spawn_failed
	// child's at once (D11), a finished child's when its linger is over
	// (linger.go). It was run after the verdict with nothing recording that
	// it was owed, so a crash in between left the tab open for good.
	EffectCloseChild = "child.close"
	// EffectMessage types one session's message into another's composer,
	// and answers the request that asked for it (D03).
	EffectMessage = "session.message"
	// EffectDeadLetterPush tells the person that a completion notice went
	// unacknowledged through its whole ladder (D24). The Swift app told
	// nobody: a dead letter there was a closeability reason on a row, found
	// only by somebody who already suspected it.
	EffectDeadLetterPush = "notice.dead_letter.push"
	// EffectWaitDelivery types a file wait's request into its owner, or its
	// release into a waiter (waits.go).
	EffectWaitDelivery = "wait.delivery"
)

// ErrEffectInsideWrite is an effect asked for from inside a write
// transaction's callback — a terminal typed into, a tab opened or closed, a
// push sent, a checkout made while the store's write right is held. It is
// refused, not merely discouraged: the rule that nothing outside the process
// is touched under that lock is enforced by this check and not by anybody
// remembering it.
var ErrEffectInsideWrite = errors.New("an effect was asked for from inside a write transaction")

// outside is the effect guard. Every method below that reaches a terminal,
// the push services or git's write side asks it first.
func outside() error {
	if store.InsideWrite() {
		return ErrEffectInsideWrite
	}
	return nil
}

// Stage is how far one recorded intent has got. The rungs are ordered, and
// each is a different fact about a different party; see the note at the top of
// this file for why they are not one word.
type Stage string

const (
	// StageAccepted: the intent is durable and is owed. Nothing has reached
	// the world yet.
	StageAccepted Stage = "accepted"
	// StageExecuted: the effect was attempted here and did not fail.
	StageExecuted Stage = "executed"
	// StageDelivered: the far side took it — for a message, the composer
	// accepted the line.
	StageDelivered Stage = "delivered"
	// StageObserved: the receiver read it. Nothing on this daemon establishes
	// this, and no answer here claims it.
	StageObserved Stage = "observed"
	// StageAcknowledged: the receiver acted on it. The same: named, never
	// claimed.
	StageAcknowledged Stage = "acknowledged"
	// StageUnknown: the attempt began and its outcome cannot be established.
	// It is not a rung — it is the admission that the ladder was lost.
	StageUnknown Stage = "unknown"
)

const (
	// effectLifetime is how long one attempt of an effect may take once it is
	// no longer anybody's request. It is generous because the slowest of them
	// — a checkout, a push through a provider's retries — is slow, and a
	// caller that asked for longer keeps its own deadline (effectContext).
	effectLifetime = 2 * time.Minute
	// effectRecordGrace is the window an outcome has to be written after the
	// attempt is over, on a context of its own. An attempt that ran out of
	// time must still be able to say so; recording it under the deadline it
	// just missed is how an effect ends up neither done nor recorded.
	effectRecordGrace = 15 * time.Second
	// unattendedPending and unattendedStarted are how long one of this
	// handle's own open effects may sit with nothing in this process holding
	// it before the beat settles it (RecoverEffects).
	//
	// They are different because the two states are different evidence. A
	// `pending` row is recorded and then held microseconds later, by the very
	// next statement, so half a minute unheld is not a race — it is a request
	// that left. A `started` row may be an attempt in progress, and an attempt
	// may take effectLifetime, so it is given that and a minute more.
	unattendedPending = 30 * time.Second
	unattendedStarted = effectLifetime + time.Minute
)

// effectContext is the context an effect runs on: its caller's values, none of
// its cancellation, and a deadline of its own — at least effectLifetime, and
// longer when the caller asked for longer (the dead-letter push does).
func effectContext(ctx context.Context) (context.Context, context.CancelFunc) {
	d := effectLifetime
	if until, ok := ctx.Deadline(); ok {
		if left := time.Until(until); left > d {
			d = left
		}
	}
	return context.WithTimeout(context.WithoutCancel(ctx), d)
}

// recordContext is the context an outcome is written on. Never the attempt's:
// see effectRecordGrace.
func recordContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), effectRecordGrace)
}

// typeLine is b.Type behind the effect guard.
func (b *Broker) typeLine(ctx context.Context, terminalID, text string) error {
	if err := outside(); err != nil {
		return err
	}
	if b.Type == nil {
		return errors.New("this daemon cannot type into a terminal")
	}
	return b.Type(ctx, terminalID, text)
}

// effectResult is how one attempt of an effect ended.
type effectResult struct {
	state   string // store.EffectDone, EffectFailed or EffectUnknown
	outcome string
	events  []store.Event
	// answer completes the request receipt the effect answers; release gives
	// the reservation back instead, for an answer about this moment only.
	answer  *store.ReceiptAnswer
	release bool
	// stage is the rung this attempt reached. Empty means the effect never
	// got as far as being attempted.
	stage Stage
	// err is why there is no answer, carried as an error so that a caller
	// decides on `errors.Is` and not on the shape of `outcome` — which is
	// prose for a person and has been rewritten more than once.
	err error
}

// effectHandler runs one kind of effect. idempotent says a second attempt
// finds out whether the first happened, and so may be made after a crash.
type effectHandler struct {
	idempotent bool
	run        func(ctx context.Context, b *Broker, e store.Effect) effectResult
}

var effectHandlers = map[string]effectHandler{
	EffectWorktree:   {idempotent: true, run: runWorktree},
	EffectCloseChild: {idempotent: true, run: runCloseChild},
	EffectMessage:    {idempotent: false, run: runMessage},
	// A push cannot be asked afterwards whether it arrived, so a recovery
	// never sends one a second time.
	EffectDeadLetterPush: {idempotent: false, run: runDeadLetterPush},
	EffectWaitDelivery:   {idempotent: false, run: runWaitDelivery},
	EffectCapacityPush:   {idempotent: false, run: runCapacityPush},
	EffectWaitingPush:    {idempotent: false, run: runWaitingPush},
}

func (b *Broker) fault(point string, e store.Effect) {
	if b.EffectFault != nil {
		b.EffectFault(point, e)
	}
}

// runEffect starts one of this broker's effects, attempts it and records how
// it ended. The answer is how it ended, or the store's refusal to let it
// start — which means somebody else has it, or it is over.
//
// The attempt runs on a context of its own (effectContext): the intent is
// already durable, so what the caller's cancellation still decides is when to
// stop waiting for the answer, never whether the effect happens. While it runs
// the row is held, so the beat can tell it from one nobody is attending.
func (b *Broker) runEffect(ctx context.Context, id int64) (res effectResult, err error) {
	if err := outside(); err != nil {
		return effectResult{err: err}, err
	}
	b.hold(id)
	defer b.release(id)
	run, cancel := effectContext(ctx)
	defer cancel()
	e, err := b.Store.StartEffect(run, id)
	if err != nil {
		return effectResult{err: err}, err
	}
	h, ok := effectHandlers[e.Kind]
	if !ok {
		res := effectResult{state: store.EffectFailed, outcome: "no handler for effect kind " + e.Kind}
		return res, b.finishEffect(run, e, res)
	}
	b.fault("started", e)
	res = h.run(run, b, e)
	if res.stage == "" && res.state == store.EffectDone {
		res.stage = StageExecuted
	}
	return res, b.finishEffect(run, e, res)
}

func (b *Broker) finishEffect(ctx context.Context, e store.Effect, res effectResult) error {
	if res.state == "" {
		res.state = store.EffectFailed
	}
	answer := res.answer
	if res.release {
		answer = nil
	}
	rec, cancel := recordContext(ctx)
	defer cancel()
	err := b.Store.FinishEffect(rec, e.ID, res.state, res.outcome, res.events, answer)
	if err == nil && res.release && e.Receipt != nil {
		err = b.Store.ReleaseReceipt(rec, *e.Receipt)
	}
	if err != nil {
		log.Printf("orchestrator: effect %d (%s %s) ran and could not be recorded: %v", e.ID, e.Kind, e.Subject, err)
	}
	return err
}

// runRecorded runs the effects a commit just recorded, in order, after the
// commit — never inside it.
func (b *Broker) runRecorded(ctx context.Context, ids []int64) []effectResult {
	out := make([]effectResult, 0, len(ids))
	for _, id := range ids {
		if len(ids) > 0 {
			b.fault("committed", store.Effect{ID: id})
		}
		res, err := b.runEffect(ctx, id)
		if err != nil {
			res = effectResult{state: store.EffectFailed, outcome: err.Error(), err: err}
		}
		out = append(out, res)
	}
	return out
}

// RecoverEffects settles the effects nobody is attending to, and settles each
// one exactly once: an effect never attempted is run now; one attempted and
// unfinished is run again only when its handler can tell whether the first
// attempt happened, and is otherwise recorded `unknown` — with the request it
// answered told so — rather than done twice. It answers how many it settled.
//
// Two sets, for two different ways of being unattended.
//
//   - A broker that has since died left rows under its own name. They are
//     taken over by AdoptEffects, which proves the owner gone before it moves
//     one.
//   - This broker's own rows, recorded by a request that then went away —
//     `unattended` below. AdoptEffects will not touch these and must not: an
//     owner that is running is exactly what it refuses to take from. Only this
//     process knows which of its rows have somebody on them, so only this
//     process can settle the rest, and the proof it uses is the in-memory hold
//     plus an age past which a row that is still unheld is not a race.
func (b *Broker) RecoverEffects(ctx context.Context) int {
	adopted, err := b.Store.AdoptEffects(ctx)
	if err != nil {
		log.Printf("orchestrator: unfinished effects could not be read: %v", err)
		return 0
	}
	n := 0
	for _, e := range adopted {
		n += b.settleEffect(ctx, e, "The broker that began this effect stopped before it recorded the outcome, "+
			"and this kind cannot find out whether it happened; it is not repeated.")
	}
	for _, e := range b.unattended(ctx) {
		n += b.settleEffect(ctx, e, "The request that recorded this effect went away while it was being carried "+
			"out, and this kind cannot find out whether it happened; it is not repeated.")
	}
	return n
}

// unattended is this handle's own open effects that nothing in this process is
// holding and that are past the age at which "unheld" stops being a race.
func (b *Broker) unattended(ctx context.Context) []store.Effect {
	own, err := b.Store.OwnOpenEffects(ctx)
	if err != nil {
		log.Printf("orchestrator: this broker's own unfinished effects could not be read: %v", err)
		return nil
	}
	now := b.now()
	out := make([]store.Effect, 0, len(own))
	for _, e := range own {
		if b.holding(e.ID) {
			continue
		}
		since, limit := e.CreatedAt, unattendedPending
		if e.State == store.EffectStarted {
			since, limit = e.StartedAt, unattendedStarted
		}
		if since.IsZero() || now.Sub(since) < limit {
			continue
		}
		out = append(out, e)
	}
	return out
}

// settleEffect runs or closes one unattended effect and answers 1 when it
// settled it. why is what the `unknown` outcome says about how it was left.
func (b *Broker) settleEffect(ctx context.Context, e store.Effect, why string) int {
	h, known := effectHandlers[e.Kind]
	if e.State == store.EffectStarted && (!known || !h.idempotent) {
		res := effectResult{state: store.EffectUnknown, outcome: why, stage: StageUnknown}
		if e.Receipt != nil {
			res.answer = refusalAnswer(refuse(http.StatusConflict, "request_outcome_unknown",
				"The request was being carried out when whoever was carrying it out stopped, so whether it took "+
					"effect is unknown. It was not repeated; look before asking again under a new key."))
		}
		payload, _ := json.Marshal(map[string]any{"effect": e.ID, "kind": e.Kind, "subject": e.Subject,
			"stage": string(StageUnknown)})
		res.events = []store.Event{{Kind: "effect.unknown", Subject: e.Subject, Payload: payload}}
		rec, cancel := recordContext(ctx)
		defer cancel()
		if err := b.Store.FinishEffect(rec, e.ID, res.state, res.outcome, res.events, res.answer); err == nil {
			return 1
		}
		return 0
	}
	if _, err := b.runEffect(ctx, e.ID); err == nil {
		return 1
	}
	return 0
}

// refusalAnswer is a refusal as a receipt keeps it.
func refusalAnswer(ref Refusal) *store.ReceiptAnswer {
	body, _ := json.Marshal(storedRefusal{Code: ref.Code, Message: ref.Message, Extra: ref.Extra})
	return &store.ReceiptAnswer{Status: ref.Status, Body: body}
}

// storedRefusal is a refusal's shape inside a receipt: enough to say it again
// word for word.
type storedRefusal struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Extra   map[string]any `json:"extra,omitempty"`
}

// --- worktree.add ---------------------------------------------------------

type worktreeEffect struct {
	Repository string `json:"repository"`
	Path       string `json:"path"`
	Branch     string `json:"branch"`
	Base       string `json:"base"`
}

// runWorktree makes the checkout, or finds it already made — the branch at
// that path is the answer to "did the first attempt happen".
func runWorktree(ctx context.Context, b *Broker, e store.Effect) effectResult {
	var w worktreeEffect
	if err := json.Unmarshal(e.Payload, &w); err != nil {
		return effectResult{state: store.EffectFailed, outcome: "unreadable effect: " + err.Error()}
	}
	payload := func(extra map[string]any) json.RawMessage {
		body := map[string]any{"task": e.Subject, "path": w.Path, "branch": w.Branch}
		for k, v := range extra {
			body[k] = v
		}
		out, _ := json.Marshal(body)
		return out
	}
	if dirExists(w.Path) {
		if exists, known := b.Git.BranchExists(ctx, w.Repository, w.Branch); known && exists {
			return effectResult{state: store.EffectDone, outcome: "already there",
				events: []store.Event{{Kind: "task.worktree.added", Subject: e.Subject, Payload: payload(map[string]any{"already": true})}}}
		}
		return effectResult{state: store.EffectFailed, outcome: "the checkout path is taken by something else",
			events: []store.Event{{Kind: "task.worktree.failed", Subject: e.Subject, Payload: payload(nil)}}}
	}
	if err := b.Git.AddWorktree(ctx, w.Repository, w.Path, w.Branch, w.Base); err != nil {
		return effectResult{state: store.EffectFailed, outcome: err.Error(),
			events: []store.Event{{Kind: "task.worktree.failed", Subject: e.Subject, Payload: payload(map[string]any{"error": err.Error()})}}}
	}
	return effectResult{state: store.EffectDone, outcome: "added",
		events: []store.Event{{Kind: "task.worktree.added", Subject: e.Subject, Payload: payload(nil)}}}
}

// --- child.close ----------------------------------------------------------

type closeChildEffect struct {
	// Backend is empty for tmux — every effect written before iTerm2 tabs
	// were closed is one — and "iterm" for an iTerm2 session, whose id is
	// then Pane.
	Backend string `json:"backend,omitempty"`
	Pane    string `json:"pane"`
	Session string `json:"session"`
	// Before is when the task ended, in Unix seconds, for an iTerm2 child:
	// a job still running in its tab is ended as the child's only when it
	// began before then (terminal.Launcher.CloseITermChild). Zero ends none.
	Before int64 `json:"before,omitempty"`
}

// runCloseChild closes the pane this broker made only while it is still one of
// the panes of the session named for the task, or the iTerm2 session by the id
// iTerm2 gave back (see closeChild). A second attempt after a crash finds the
// pane or session gone and closes nothing.
func runCloseChild(ctx context.Context, b *Broker, e store.Effect) effectResult {
	var c closeChildEffect
	if err := json.Unmarshal(e.Payload, &c); err != nil {
		return effectResult{state: store.EffectFailed, outcome: "unreadable effect: " + err.Error()}
	}
	if b.Launcher == nil {
		return effectResult{state: store.EffectFailed, outcome: "this daemon has no launcher"}
	}
	var (
		closed bool
		err    error
	)
	switch c.Backend {
	case "":
		closed, err = b.Launcher.CloseTmuxSession(ctx, c.Pane, c.Session)
	case "iterm":
		closer, ok := b.Launcher.(itermCloser)
		if !ok {
			return effectResult{state: store.EffectFailed, outcome: "this daemon cannot close an iTerm2 session"}
		}
		var before time.Time
		if c.Before > 0 {
			before = time.Unix(c.Before, 0)
		}
		closed, err = closer.CloseITermChild(ctx, c.Pane, before)
	default:
		return effectResult{state: store.EffectFailed, outcome: "no close for a " + c.Backend + " child"}
	}
	body := map[string]any{"task": e.Subject, "pane": c.Pane, "session": c.Session, "closed": closed}
	if c.Backend != "" {
		body["backend"] = c.Backend
	}
	res := effectResult{state: store.EffectDone, outcome: "closed"}
	if !closed {
		res.outcome = "not closed"
	}
	if err != nil {
		body["error"] = err.Error()
		res.state, res.outcome = store.EffectFailed, err.Error()
		// A close the terminal did not answer, and a look afterwards could
		// not settle: neither done nor failed, and not tried again.
		var unconfirmed terminal.Unconfirmed
		if errors.As(err, &unconfirmed) {
			body["unconfirmed"] = true
			res.state = store.EffectUnknown
		}
	}
	payload, _ := json.Marshal(body)
	res.events = []store.Event{{Kind: "task.child.closed", Subject: e.Subject, Payload: payload}}
	return res
}

// --- session.message ------------------------------------------------------

type messageEffect struct {
	Target string `json:"target"`
	Source string `json:"source"`
	Wire   string `json:"wire"`
	// Accepted is when the intent became durable, carried through the effect
	// so the answer can report it beside the moment of delivery rather than
	// giving one timestamp two meanings.
	Accepted int64 `json:"accepted,omitempty"`
}

// messageAnswer is a relayed message's answer as its receipt keeps it: when it
// was accepted, when it was delivered, and which of those the answer is.
type messageAnswer struct {
	At       int64  `json:"at"`
	Accepted int64  `json:"accepted,omitempty"`
	Stage    string `json:"stage,omitempty"`
}

// runMessage types one message. It is not idempotent — nothing can tell
// afterwards whether the bytes went in — so a recovery never repeats an
// attempt that had begun.
func runMessage(ctx context.Context, b *Broker, e store.Effect) effectResult {
	var m messageEffect
	if err := json.Unmarshal(e.Payload, &m); err != nil {
		return effectResult{state: store.EffectFailed, outcome: "unreadable effect: " + err.Error(),
			answer: refusalAnswer(refuse(http.StatusInternalServerError, "encoding_failed", "The message could not be read back."))}
	}
	target, ok := b.sessionByTerminal(ctx, m.Target)
	if !ok || !target.IsAssistant() {
		return effectResult{state: store.EffectFailed, outcome: "target gone", release: true,
			answer: refusalAnswer(refuse(http.StatusNotFound, "target_not_found",
				"No current assistant session has that terminal id."))}
	}
	// A target showing a menu would read the message as an answer to it.
	// Nothing was typed, so the reservation is given back: a resend of the
	// same request, once the menu is gone, is still the first delivery.
	if b.Choosing != nil && b.Choosing(ctx, target.ID) {
		return effectResult{state: store.EffectFailed, outcome: "target_busy", release: true,
			answer: refusalAnswer(refuse(http.StatusConflict, "target_busy",
				"The target is showing a menu; typing would answer it instead of delivering the message."))}
	}
	if err := b.typeLine(ctx, target.ID, m.Wire); err != nil {
		var busy lane.Busy
		if errors.As(err, &busy) {
			return effectResult{state: store.EffectFailed, outcome: "terminal_busy", release: true,
				answer: refusalAnswer(refuseWith(http.StatusTooManyRequests, "terminal_busy",
					"The target's terminal was being written to and did not come free; nothing was typed.",
					map[string]any{"retry_after": 5}))}
		}
		return effectResult{state: store.EffectFailed, outcome: err.Error(),
			answer: refusalAnswer(refuse(http.StatusBadGateway, "delivery_failed", err.Error()))}
	}
	at := b.now()
	// Delivered: the composer took the line. Not observed, and not
	// acknowledged — the receiver's own next turn is the evidence for those,
	// and nothing here waits for it.
	body, _ := json.Marshal(messageAnswer{At: at.Unix(), Accepted: m.Accepted, Stage: string(StageDelivered)})
	payload, _ := json.Marshal(map[string]any{"from": m.Source, "effect": e.ID, "stage": string(StageDelivered),
		"accepted": m.Accepted, "delivered": at.Unix()})
	return effectResult{
		state: store.EffectDone, outcome: "typed", stage: StageDelivered,
		answer: &store.ReceiptAnswer{Status: http.StatusOK, Body: body},
		events: []store.Event{{Kind: "session.message", Subject: target.ID, Payload: payload}},
	}
}

// --- notice.dead_letter.push ----------------------------------------------

type deadLetterEffect struct {
	Notice   string `json:"notice"`
	Attempts int    `json:"attempts"`
	// Reason is the notice's last error code as the move that gave up read it.
	// Empty on an effect recorded before this field existed, which reads as the
	// only reason there was then: nobody acknowledged it.
	Reason string `json:"reason,omitempty"`
}

// deadLetterTitle and the two bodies are server strings, in the one language
// this daemon's own pushes speak today (push.go's pushTestBody).
//
// There are two because there are now two ways to give up, and telling a person
// "the notice was sent eight times" about a notice that was never typed once
// sends them looking for something that is not there. A hold code means the
// root's terminal could not be written to at all — it was holding a dialog, or
// its composer had something in it that typing would have been submitted with
// (notice.go).
const (
	deadLetterTitle = "完成通知沒有送達"
	deadLetterBody  = "「%s」已經結束（%s），但通知送了 %d 次都沒有被收下。打開那個 session 讀 result.json；" +
		"修好原因後可以用 POST /v1/orchestrator/completions/reconcile 重送。"
	deadLetterHeldBody = "「%s」已經結束（%s），但它的 root 終端機一直不能打字（%s），通知沒有送進去。" +
		"讀 result.json；把那個終端機空出來之後，用 POST /v1/orchestrator/completions/reconcile 重送。"
)

// heldReason is a hold code's Chinese sentence for the push, or empty for a
// code that is not a hold.
func heldReason(code string) string {
	switch code {
	case holdComposer.Code:
		return "輸入框裡有還沒送出的內容"
	case holdChoosing.Code:
		return "畫面停在一個等人回答的選單"
	case holdLane.Code:
		return "那個終端機一直被別人佔用"
	case holdQueued.Code:
		return "先前那一份還排在輸入框裡沒被讀到"
	}
	return ""
}

// runDeadLetterPush sends the one push a dead letter owes. Tapping it opens
// the root the notice was for, when this machine is watching it.
func runDeadLetterPush(ctx context.Context, b *Broker, e store.Effect) effectResult {
	var d deadLetterEffect
	_ = json.Unmarshal(e.Payload, &d)
	r, _, err := b.Record(ctx, e.Subject)
	if err != nil {
		return effectResult{state: store.EffectFailed, outcome: "task unreadable: " + err.Error()}
	}
	if b.Push == nil {
		return effectResult{state: store.EffectFailed, outcome: "this daemon cannot push"}
	}
	title := r.Title
	if title == "" {
		title = r.ID
	}
	said := fmt.Sprintf(deadLetterBody, title, r.State, d.Attempts)
	if why := heldReason(d.Reason); why != "" {
		said = fmt.Sprintf(deadLetterHeldBody, title, r.State, why)
	}
	sent, failed, err := b.Push(ctx, deadLetterTitle, said, r.RootTerminalID, "dead-letter-"+r.ID)
	body, _ := json.Marshal(map[string]any{"task": r.ID, "notice": d.Notice, "sent": sent, "failed": failed})
	events := []store.Event{{Kind: "task.completion.dead_letter.pushed", Subject: r.ID, Payload: body}}
	switch {
	case err != nil:
		return effectResult{state: store.EffectFailed, outcome: err.Error(), events: events}
	case sent == 0 && failed == 0:
		// Nobody asked to be notified. There is nothing to retry: the dead
		// letter is still counted in /v1/diagnostics and listed by
		// /v1/orchestrator/completions.
		return effectResult{state: store.EffectDone, outcome: "not_subscribed", events: events}
	case sent == 0:
		return effectResult{state: store.EffectFailed, outcome: "no push service accepted it", events: events}
	}
	return effectResult{state: store.EffectDone, outcome: "pushed", events: events}
}
