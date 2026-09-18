package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
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
	if r.State == StateTimeout && len(r.Claims) > 0 {
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
		Released: r.State == StateTimeout && len(r.Claims) > 0,
		MayWrite: r.State == StateTimeout && len(r.Claims) > 0,
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

// PumpNotices makes one pass over the notices that are due.
//
// At most eight per pass, oldest deadline first, so a machine that was asleep
// for an hour does not type sixty lines into one composer the moment it wakes.
func (b *Broker) PumpNotices(ctx context.Context) int {
	records, err := b.records(ctx)
	if err != nil {
		return 0
	}
	now := b.now()
	due := []Record{}
	for _, r := range records {
		n := r.Notice
		if n == nil || n.State == NoticeAcknowledged || n.State == NoticeDeadLetter {
			continue
		}
		if n.NextRetryAt.IsZero() || !n.NextRetryAt.After(now) {
			due = append(due, r)
		}
	}
	sort.Slice(due, func(i, j int) bool { return due[i].Notice.NextRetryAt.Before(due[j].Notice.NextRetryAt) })
	if len(due) > 8 {
		due = due[:8]
	}
	sent := 0
	for _, r := range due {
		if b.attemptNotice(ctx, r) {
			sent++
		}
	}
	return sent
}

// attemptNotice delivers one notice, or records why it did not.
//
// The typing happens outside mutate, because it takes seconds and holds a
// terminal. What it learned is then applied only if the notice is still the one
// that was attempted and nobody has acknowledged it meanwhile: an ACK that
// lands while this is typing wins, and the attempt is discarded rather than
// written back over it.
func (b *Broker) attemptNotice(ctx context.Context, r Record) bool {
	noticeID := r.Notice.ID
	stillOpen := func(now *Record) bool {
		return now.Notice != nil && now.Notice.ID == noticeID &&
			now.Notice.State != NoticeAcknowledged && now.Notice.State != NoticeDeadLetter
	}
	record := func(kind string, change func(n *Notice)) {
		_, _ = b.mutate(ctx, r.ID, kind, func(now *Record) error {
			if !stillOpen(now) {
				return errUnchanged
			}
			change(now.Notice)
			return nil
		})
	}
	at := b.now()

	if r.Notice.Attempts >= AttemptLimit {
		record("task.completion.dead_letter", func(n *Notice) {
			n.State = NoticeDeadLetter
			n.DeadLetterAt = at
			n.NextRetryAt = zeroTime
			n.LastError = &NoticeError{
				Code:    "acknowledgement_timeout",
				Message: "No root acknowledgement arrived within the bounded retry budget.",
				At:      at,
			}
		})
		return false
	}

	fail := func(code, message string) bool {
		record("task.completion.attempt", func(n *Notice) {
			n.Attempts++
			n.LastAttemptAt = at
			n.LastError = &NoticeError{Code: code, Message: message, At: at}
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
	// nobody gave.
	if b.Choosing != nil && b.Choosing(ctx, target.ID) {
		record("task.completion.deferred", func(n *Notice) {
			n.NextRetryAt = at.Add(10 * time.Second)
		})
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
		return fail("transport_failed", err.Error())
	}

	record("task.completion.delivered", func(n *Notice) {
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
