package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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
	Protocol  string     `json:"protocol"`
	Version   int        `json:"version"`
	Kind      string     `json:"kind"`
	Audience  string     `json:"audience"`
	Task      noticeTask `json:"task"`
	State     string     `json:"state"`
	Result    string     `json:"result_path"`
	Outstand  int        `json:"outstanding"`
	Released  bool       `json:"claims_released"`
	MayWrite  bool       `json:"child_may_still_write"`
	Body      string     `json:"body"`
	NoticeID  string     `json:"notice_id"`
	AckPath   string     `json:"ack_path"`
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
func (b *Broker) attemptNotice(ctx context.Context, r Record) bool {
	_, hash, err := b.Record(ctx, r.ID)
	if err != nil {
		return false
	}
	now := b.now()
	if r.Notice.Attempts >= AttemptLimit {
		r.Notice.State = NoticeDeadLetter
		r.Notice.DeadLetterAt = now
		r.Notice.NextRetryAt = zeroTime
		r.Notice.LastError = &NoticeError{
			Code:    "acknowledgement_timeout",
			Message: "No root acknowledgement arrived within the bounded retry budget.",
			At:      now,
		}
		_ = b.save(ctx, r, hash, "task.completion.dead_letter")
		return false
	}

	fail := func(code, message string) bool {
		r.Notice.Attempts++
		r.Notice.LastAttemptAt = now
		r.Notice.LastError = &NoticeError{Code: code, Message: message, At: now}
		r.Notice.NextRetryAt = now.Add(RetryDelay(r.Notice.Attempts))
		_ = b.save(ctx, r, hash, "task.completion.attempt")
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
		r.Notice.NextRetryAt = now.Add(10 * time.Second)
		_ = b.save(ctx, r, hash, "task.completion.deferred")
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

	r.Notice.Attempts++
	r.Notice.LastAttemptAt = now
	r.Notice.State = NoticeDelivered
	r.Notice.Recipient = target.ID
	r.Notice.LastError = nil
	if r.Notice.DeliveredAt.IsZero() {
		r.Notice.DeliveredAt = now
	}
	// Armed again on purpose. Delivered is not observed.
	r.Notice.NextRetryAt = now.Add(RetryDelay(r.Notice.Attempts))
	if err := b.save(ctx, r, hash, "task.completion.delivered"); err != nil {
		log.Printf("orchestrator: could not record a delivered notice for %s: %v", r.ID, err)
	}
	return true
}
