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

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/app/lane"
)

// Telling the root its child finished, and keeping at it until somebody says
// they saw it.
//
// The Swift app's measured lesson is in three clauses:
//
//   - **A transport success is not an observation.** Bytes reached a composer;
//     nothing says a turn read them. So a successful send reschedules rather
//     than stops, and only `/completion/ack` ends the sequence.
//   - **The sequence ends anyway.** Eight attempts on a 5→300-second ladder,
//     then dead letter. A notice that retries for ever is one nobody has to
//     answer.
//   - **Never type into a session showing a menu.** The keystroke would answer
//     the menu instead, which is how "Tea" once became "Water".

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
	Outstand int        `json:"outstanding"`
	Released bool       `json:"claims_released"`
	MayWrite bool       `json:"child_may_still_write"`
	Body     string     `json:"body"`
	NoticeID string     `json:"notice_id"`
	AckPath  string     `json:"ack_path"`
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
	if r.Landing != nil && r.Landing.State == LandingPending {
		line += " — claimed work may still be in the shared tree; mark landing pending so other roots can see it"
	}
	if noticeID != "" {
		line += fmt.Sprintf(" — after observing, ACK notice %s at /v1/orchestrator/tasks/%s/completion/ack",
			noticeID, r.ID)
	}
	return line
}

// NoticeWire is the whole line typed into the root, wrapper and all.
func (b *Broker) NoticeWire(r Record) (string, error) {
	if r.Notice == nil {
		return "", fmt.Errorf("this task has no completion envelope")
	}
	body := noticeBody{
		Protocol: NoticeProtocol,
		Version:  NoticeVersion,
		Kind:     "task_finished",
		Audience: "root",
		Task:     noticeTask{ID: r.ID, Title: r.Title},
		State:    string(r.State),
		Result:   filepath.Join(b.Tasks.Path(r.ID), "result.json"),
		Released: r.State == StateTimeout && len(r.Lease()) > 0,
		MayWrite: r.State == StateTimeout && len(r.Lease()) > 0,
		Body:     b.FinishedLine(r, r.Notice.ID),
		NoticeID: r.Notice.ID,
		AckPath:  "/v1/orchestrator/tasks/" + r.ID + "/completion/ack",
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
	next := seen
	if seen.LastError != nil {
		e := *seen.LastError
		next.LastError = &e
	}
	change(&next)
	payload, _ := json.Marshal(map[string]any{
		"task": taskID, "notice": seen.ID, "attempt": next.Attempts, "state": next.State,
		"error": errorCode(next.LastError),
	})
	applied, err := b.Store.UpdateBrokerNotice(ctx,
		store.NoticeExpect{State: string(seen.State), Attempts: seen.Attempts},
		noticeRow(taskID, next),
		[]store.Event{{Kind: kind, Subject: taskID, Payload: payload}})
	if err != nil {
		log.Printf("orchestrator: notice %s for task %s could not be recorded: %v", seen.ID, taskID, err)
		return false
	}
	return applied
}

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

	fail := func(code, message string) bool {
		b.moveNotice(ctx, r.ID, seen, "task.completion.attempt", func(n *Notice) {
			n.Attempts++
			n.LastAttemptAt = at
			n.LastError = &NoticeError{Code: code, Message: message, At: at}
			// The Swift app's rule: the attempt that uses the last of the
			// budget ends the sequence then, rather than one ladder rung
			// later.
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

	if r.Root == nil {
		return fail("root_missing", "This task has no root to tell.")
	}
	target, err := b.terminalFor(ctx, r.Root.SessionID, r.Root.Assistant)
	if err != nil {
		if ref, ok := err.(Refusal); ok && ref.Code == "conversation_ambiguous" {
			return fail("conversation_ambiguous", ref.Message)
		}
		return fail("root_missing", "The root session is not on this machine right now.")
	}
	// Backpressure, not an attempt: a root showing a chooser would have the
	// notice typed into its menu. Waiting costs a pass; typing costs an answer
	// nobody gave. The wait is remembered in memory only.
	if b.Choosing != nil && b.Choosing(ctx, target.ID) {
		b.observed.deferNotice(seen.ID, at.Add(10*time.Second))
		return false
	}
	wire, err := b.NoticeWire(r)
	if err != nil {
		return fail("transport_failed", err.Error())
	}
	if b.Type == nil {
		return fail("transport_failed", "this daemon cannot type into a terminal")
	}
	if err := b.Type(ctx, target.ID, wire); err != nil {
		// Backpressure, not an attempt, for the same reason as a chooser: the
		// root's terminal was being written to by somebody else (its lane,
		// D22), and nothing was typed. Counting it would walk a notice toward
		// dead letter for being polite.
		var busy lane.Busy
		if errors.As(err, &busy) {
			b.observed.deferNotice(seen.ID, at.Add(10*time.Second))
			return false
		}
		return fail("transport_failed", err.Error())
	}

	b.moveNotice(ctx, r.ID, seen, "task.completion.delivered", func(n *Notice) {
		n.Attempts++
		n.LastAttemptAt = at
		n.State = NoticeDelivered
		n.Recipient = target.ID
		n.LastError = nil
		if n.DeliveredAt.IsZero() {
			n.DeliveredAt = at
		}
		// Armed again on purpose. Delivered is not observed.
		n.NextRetryAt = at.Add(RetryDelay(n.Attempts))
	})
	return true
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
