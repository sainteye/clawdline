package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
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

// The effect kinds.
const (
	// EffectWorktree makes a task's isolated checkout. Before W2 it was made
	// before the task existed, so a crash between the two left a checkout no
	// record named (G14).
	EffectWorktree = "worktree.add"
	// EffectCloseChild closes the tmux session a spawn_failed child was
	// given (D11). It was run after the verdict with nothing recording that
	// it was owed, so a crash in between left the tab open for good.
	EffectCloseChild = "child.close"
	// EffectMessage types one session's message into another's composer,
	// and answers the request that asked for it (D03).
	EffectMessage = "session.message"
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
}

func (b *Broker) fault(point string, e store.Effect) {
	if b.EffectFault != nil {
		b.EffectFault(point, e)
	}
}

// runEffect starts one of this broker's effects, attempts it and records how
// it ended. The answer is how it ended, or the store's refusal to let it
// start — which means somebody else has it, or it is over.
func (b *Broker) runEffect(ctx context.Context, id int64) (effectResult, error) {
	if err := outside(); err != nil {
		return effectResult{}, err
	}
	e, err := b.Store.StartEffect(ctx, id)
	if err != nil {
		return effectResult{}, err
	}
	h, ok := effectHandlers[e.Kind]
	if !ok {
		res := effectResult{state: store.EffectFailed, outcome: "no handler for effect kind " + e.Kind}
		return res, b.finishEffect(ctx, e, res)
	}
	b.fault("started", e)
	res := h.run(ctx, b, e)
	return res, b.finishEffect(ctx, e, res)
}

func (b *Broker) finishEffect(ctx context.Context, e store.Effect, res effectResult) error {
	if res.state == "" {
		res.state = store.EffectFailed
	}
	answer := res.answer
	if res.release {
		answer = nil
	}
	err := b.Store.FinishEffect(ctx, e.ID, res.state, res.outcome, res.events, answer)
	if err == nil && res.release && e.Receipt != nil {
		err = b.Store.ReleaseReceipt(ctx, *e.Receipt)
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
			res = effectResult{state: store.EffectFailed, outcome: err.Error()}
		}
		out = append(out, res)
	}
	return out
}

// RecoverEffects takes over the effects a broker that has since died left
// unfinished, and settles each one exactly once: an effect never attempted is
// run now; one attempted and unfinished is run again only when its handler can
// tell whether the first attempt happened, and is otherwise recorded `unknown`
// — with the request it answered told so — rather than done twice. It answers
// how many it settled.
func (b *Broker) RecoverEffects(ctx context.Context) int {
	adopted, err := b.Store.AdoptEffects(ctx)
	if err != nil {
		log.Printf("orchestrator: unfinished effects could not be read: %v", err)
		return 0
	}
	n := 0
	for _, e := range adopted {
		h, known := effectHandlers[e.Kind]
		if e.State == store.EffectStarted && (!known || !h.idempotent) {
			res := effectResult{
				state: store.EffectUnknown,
				outcome: "The broker that began this effect stopped before it recorded the outcome, and this kind " +
					"cannot find out whether it happened; it is not repeated.",
			}
			if e.Receipt != nil {
				res.answer = refusalAnswer(refuse(http.StatusConflict, "request_outcome_unknown",
					"The daemon stopped while this request was being carried out, so whether it took effect is "+
						"unknown. It was not repeated; look before asking again under a new key."))
			}
			payload, _ := json.Marshal(map[string]any{"effect": e.ID, "kind": e.Kind, "subject": e.Subject})
			res.events = []store.Event{{Kind: "effect.unknown", Subject: e.Subject, Payload: payload}}
			if err := b.Store.FinishEffect(ctx, e.ID, res.state, res.outcome, res.events, res.answer); err == nil {
				n++
			}
			continue
		}
		if _, err := b.runEffect(ctx, e.ID); err == nil {
			n++
		}
	}
	return n
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
	Pane    string `json:"pane"`
	Session string `json:"session"`
}

// runCloseChild closes the session named for the task only while the pane
// this broker made is still one of its panes (see closeChild). A second
// attempt after a crash finds the pane gone and closes nothing.
func runCloseChild(ctx context.Context, b *Broker, e store.Effect) effectResult {
	var c closeChildEffect
	if err := json.Unmarshal(e.Payload, &c); err != nil {
		return effectResult{state: store.EffectFailed, outcome: "unreadable effect: " + err.Error()}
	}
	if b.Launcher == nil {
		return effectResult{state: store.EffectFailed, outcome: "this daemon has no launcher"}
	}
	closed, err := b.Launcher.CloseTmuxSession(ctx, c.Pane, c.Session)
	body := map[string]any{"task": e.Subject, "pane": c.Pane, "session": c.Session, "closed": closed}
	res := effectResult{state: store.EffectDone, outcome: "closed"}
	if !closed {
		res.outcome = "not closed"
	}
	if err != nil {
		body["error"] = err.Error()
		res.state, res.outcome = store.EffectFailed, err.Error()
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
}

// messageAnswer is a relayed message's answer as its receipt keeps it.
type messageAnswer struct {
	At int64 `json:"at"`
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
	body, _ := json.Marshal(messageAnswer{At: at.Unix()})
	payload, _ := json.Marshal(map[string]any{"from": m.Source, "effect": e.ID})
	return effectResult{
		state: store.EffectDone, outcome: "typed",
		answer: &store.ReceiptAnswer{Status: http.StatusOK, Body: body},
		events: []store.Event{{Kind: "session.message", Subject: target.ID, Payload: payload}},
	}
}
