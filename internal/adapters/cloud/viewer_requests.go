package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	domain "github.com/sainteye/clawdline/internal/domain/cloud"
)

type ViewerRefusal struct {
	Code   string `json:"code"`
	Status int    `json:"status,omitempty"`
}

func (r ViewerRefusal) Error() string { return r.Code }

type viewerReply struct {
	body json.RawMessage
	err  error
}

type viewerPending struct {
	key         string
	channel     string
	seq         uint64
	action      bool
	pinnedRead  bool
	destination ViewerDestination
	answer      chan viewerReply
}

func (c *ViewerClient) removePending(p *viewerPending) {
	c.pendingMu.Lock()
	if c.pending[p.key] == p {
		delete(c.pending, p.key)
		delete(c.pendingSeq, p.seq)
	}
	c.pendingMu.Unlock()
}

func viewerReplyKey(machine, session, name string) string {
	return machine + "\x00" + session + "\x00" + name
}

func (c *ViewerClient) settlePending(p *viewerPending, result viewerReply) {
	c.pendingMu.Lock()
	if c.pending[p.key] == p {
		delete(c.pending, p.key)
		delete(c.pendingSeq, p.seq)
		p.answer <- result
	}
	c.pendingMu.Unlock()
}

func (c *ViewerClient) onAck(frame AckFrame) {
	c.pendingMu.Lock()
	p := c.pendingSeq[frame.Seq]
	c.pendingMu.Unlock()
	if p != nil && p.channel == frame.Ch && (frame.Status == AckMachineOffline || frame.Status == AckViewerOffline) {
		c.settlePending(p, viewerReply{err: ErrViewerOffline})
	}
}

func (c *ViewerClient) onPublishRefusal(frame PublishErrorFrame) {
	c.pendingMu.Lock()
	p := c.pendingSeq[frame.Seq]
	c.pendingMu.Unlock()
	if p != nil && p.channel == frame.Ch {
		c.settlePending(p, viewerReply{err: ViewerRefusal{Code: frame.Code}})
	}
}

func (c *ViewerClient) onDisconnect() {
	c.pendingMu.Lock()
	for _, p := range c.pending {
		err := error(ErrViewerOffline)
		if p.action {
			err = ViewerRefusal{Code: "receipt_outcome_unknown"}
		}
		p.answer <- viewerReply{err: err}
	}
	clear(c.pending)
	clear(c.pendingSeq)
	c.pendingMu.Unlock()
	c.richMu.Lock()
	clear(c.rich)
	c.richMu.Unlock()
	select {
	case c.update <- struct{}{}:
	default:
	}
}

func (c *ViewerClient) acceptReply(envelope domain.Envelope, plaintext []byte) {
	parts := strings.Split(envelope.Ch, "/")
	if len(parts) != 3 || parts[0] != "t" {
		return
	}
	machineID, machineErr := url.PathUnescape(parts[1])
	sessionID, sessionErr := url.PathUnescape(parts[2])
	if machineErr != nil || sessionErr != nil || machineID != envelope.Sender || sessionID == "" {
		return
	}
	var answer struct {
		Read               string          `json:"read"`
		MachineID          string          `json:"machine_id"`
		SessionID          string          `json:"session_id"`
		ExpectedGeneration string          `json:"expected_generation"`
		Sequence           json.RawMessage `json:"seq"`
		Status             int             `json:"status"`
		Body               json.RawMessage `json:"body"`
		Error              struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(plaintext, &answer) != nil {
		return
	}
	key := viewerReplyKey(machineID, sessionID, answer.Read)
	var replySequence uint64
	sequenceOK := len(answer.Sequence) > 0 && json.Unmarshal(answer.Sequence, &replySequence) == nil
	c.pendingMu.Lock()
	p := c.pending[key]
	if p == nil && sequenceOK {
		candidate := c.pendingSeq[replySequence]
		if candidate != nil && candidate.pinnedRead && candidate.destination.MachineID == machineID &&
			candidate.destination.SessionID == sessionID {
			p = candidate
		}
	}
	c.pendingMu.Unlock()
	if p == nil {
		return
	}
	if p.pinnedRead && (key != p.key || answer.MachineID != p.destination.MachineID ||
		answer.SessionID != p.destination.SessionID ||
		answer.ExpectedGeneration != p.destination.ExecutionGeneration ||
		!sequenceOK || replySequence != p.seq) {
		c.settlePending(p, viewerReply{err: ViewerRefusal{Code: "read_reply_mismatch"}})
		return
	}
	if answer.Error.Code != "" || answer.Status < 200 || answer.Status >= 300 {
		code := answer.Error.Code
		if code == "" {
			code = "cloud_read_failed"
		}
		c.settlePending(p, viewerReply{err: ViewerRefusal{Code: code, Status: answer.Status}})
		return
	}
	if len(answer.Body) == 0 || !json.Valid(answer.Body) {
		c.settlePending(p, viewerReply{err: ViewerRefusal{Code: "malformed_reply"}})
		return
	}
	c.settlePending(p, viewerReply{body: answer.Body})
}

func (c *ViewerClient) nextSequence() (uint64, error) {
	c.seqMu.Lock()
	defer c.seqMu.Unlock()
	seq := c.sequence
	if err := c.fence.Reserve(seq); err != nil {
		return 0, err
	}
	c.sequence++
	return seq, nil
}

func (c *ViewerClient) request(ctx context.Context, destination ViewerDestination, channel, readName string,
	command map[string]any, action bool) (json.RawMessage, error) {
	if !destination.Valid() {
		return nil, ViewerRefusal{Code: "execution_target_required"}
	}
	if c.transport.State() != StateConnected {
		return nil, ErrViewerOffline
	}
	c.pinsMu.RLock()
	pin, ok := c.pins[destination.MachineID]
	c.pinsMu.RUnlock()
	if !ok {
		return nil, c.pairingRefusal(destination.MachineID)
	}
	plaintext, err := json.Marshal(command)
	if err != nil {
		return nil, err
	}
	seq, err := c.nextSequence()
	if err != nil {
		return nil, err
	}
	key := viewerReplyKey(destination.MachineID, destination.SessionID, readName)
	fullChannel := channel + "/" + viewerChannelSegment(destination.MachineID)
	p := &viewerPending{key: key, channel: fullChannel, seq: seq, action: action,
		pinnedRead: channel == "r", destination: destination, answer: make(chan viewerReply, 1)}
	c.pendingMu.Lock()
	if c.pending[key] != nil {
		c.pendingMu.Unlock()
		return nil, ViewerRefusal{Code: "cloud_read_busy"}
	}
	c.pending[key] = p
	c.pendingSeq[seq] = p
	c.pendingMu.Unlock()
	defer c.removePending(p)
	envelope, err := domain.Seal(plaintext, domain.SealParams{
		Ch: fullChannel, Seq: seq,
		Ts: uint64(time.Now().UnixMilli()), Class: domain.ClassCtl, KeyID: pin.pin.KeyID,
		Sender: c.identity.DeviceID, Key: pin.content, Signer: c.signer,
	})
	if err != nil {
		return nil, err
	}
	raw, err := envelope.CanonicalJSON()
	if err != nil {
		return nil, err
	}
	if err := c.transport.Publish(raw); err != nil {
		if errors.Is(err, ErrNotConnected) {
			return nil, ErrViewerOffline
		}
		if action {
			return nil, ViewerRefusal{Code: "receipt_outcome_unknown"}
		}
		return nil, err
	}
	select {
	case result := <-p.answer:
		return result.body, result.err
	case <-ctx.Done():
		if action {
			return nil, ViewerRefusal{Code: "receipt_outcome_unknown"}
		}
		return nil, fmt.Errorf("%w: %v", ErrViewerStatusUnavailable, ctx.Err())
	}
}

type ViewerDetail struct {
	Destination ViewerDestination `json:"destination"`
	Info        json.RawMessage   `json:"info"`
	Transcript  json.RawMessage   `json:"transcript"`
	NextBefore  *int64            `json:"next_before,omitempty"`
	Question    *ViewerQuestion   `json:"question,omitempty"`
}

type ViewerTranscriptPage struct {
	Destination ViewerDestination `json:"destination"`
	Transcript  json.RawMessage   `json:"transcript"`
	NextBefore  *int64            `json:"next_before,omitempty"`
}

func viewerTranscriptCursor(body json.RawMessage, destination ViewerDestination) (*int64, error) {
	var page struct {
		ID         string          `json:"id"`
		Entries    json.RawMessage `json:"entries"`
		NextBefore *int64          `json:"nextBefore"`
	}
	if json.Unmarshal(body, &page) != nil || page.ID != destination.SessionID ||
		len(page.Entries) == 0 || page.Entries[0] != '[' ||
		page.NextBefore != nil && *page.NextBefore <= 0 {
		return nil, ViewerRefusal{Code: "malformed_reply"}
	}
	return page.NextBefore, nil
}

func viewerTranscriptCommand(destination ViewerDestination, before int64) (string, map[string]any) {
	name := "transcript"
	command := map[string]any{"type": "transcript", "session": destination.SessionID, "limit": 200,
		"priority": "foreground", "machine_id": destination.MachineID,
		"expected_generation": destination.ExecutionGeneration}
	if before > 0 {
		name = fmt.Sprintf("transcript.before.%d", before)
		command["before"] = before
	}
	return name, command
}

func (c *ViewerClient) readTranscript(ctx context.Context, destination ViewerDestination, before int64) (json.RawMessage, *int64, error) {
	name, command := viewerTranscriptCommand(destination, before)
	body, err := c.request(ctx, destination, "r", name, command, false)
	if err != nil {
		return nil, nil, err
	}
	next, err := viewerTranscriptCursor(body, destination)
	return body, next, err
}

// ViewerMutation always carries a caller-owned request ID. The same ID can
// later be queried without guessing whether a lost answer meant execution.
type ViewerMutation struct {
	Destination ViewerDestination `json:"destination"`
	Action      string            `json:"action"`
	Request     string            `json:"request"`
	Text        string            `json:"text,omitempty"`
	Answer      string            `json:"answer,omitempty"`
	Expect      string            `json:"expect,omitempty"`
}

type ViewerMutationResult struct {
	Request string          `json:"request"`
	Body    json.RawMessage `json:"body,omitempty"`
}

func validViewerRequestID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return false
		}
	}
	return true
}

// Mutate refuses an unproven or stale execution before sealing an action.
// The machine repeats the generation check at effect admission.
func (c *ViewerClient) Mutate(ctx context.Context, input ViewerMutation) (ViewerMutationResult, error) {
	if !validViewerRequestID(input.Request) {
		return ViewerMutationResult{}, ViewerRefusal{Code: "idempotency_key_required"}
	}
	command := map[string]any{"type": input.Action, "session": input.Destination.SessionID,
		"request": input.Request, "execution_generation": input.Destination.ExecutionGeneration}
	switch input.Action {
	case "send":
		if strings.TrimSpace(input.Text) == "" {
			return ViewerMutationResult{}, ViewerRefusal{Code: "empty"}
		}
		command["text"], command["images"] = strings.TrimSpace(input.Text), []any{}
	case "answer":
		if input.Answer == "" || input.Expect == "" {
			return ViewerMutationResult{}, ViewerRefusal{Code: "menu_unverified"}
		}
		command["answer"], command["expect"] = input.Answer, input.Expect
	case "interrupt":
	case "end":
		command["accept_loss"], command["expected_closeability_version"] = false, ""
	default:
		return ViewerMutationResult{}, ViewerRefusal{Code: "unsupported_action"}
	}
	if err := c.requireCurrentDestination(ctx, input.Destination); err != nil {
		return ViewerMutationResult{}, err
	}
	if input.Action == "answer" {
		detail, err := c.ReadDetail(ctx, input.Destination)
		if err != nil {
			return ViewerMutationResult{}, err
		}
		if detail.Question == nil || detail.Question.Fingerprint != input.Expect {
			return ViewerMutationResult{}, ViewerRefusal{Code: "menu_unverified"}
		}
		validOption := false
		for _, option := range detail.Question.Options {
			if option.Key == input.Answer {
				validOption = true
			}
		}
		if !validOption {
			return ViewerMutationResult{}, ViewerRefusal{Code: "menu_unverified"}
		}
	}
	path := "t/" + viewerChannelSegment(input.Destination.MachineID) + "/" + viewerChannelSegment(input.Destination.SessionID)
	if err := c.transport.Subscribe(path); err != nil {
		return ViewerMutationResult{}, err
	}
	defer func() { _ = c.transport.Unsubscribe(path) }()
	body, err := c.request(ctx, input.Destination, "ctl", "action:"+input.Request, command, true)
	return ViewerMutationResult{Request: input.Request, Body: body}, err
}

type ViewerReceipt struct {
	Request             string `json:"request"`
	Action              string `json:"action"`
	ExecutionGeneration string `json:"execution_generation"`
	MachineExecution    string `json:"machine_execution"`
	Status              int    `json:"status"`
	Code                string `json:"code"`
	RelayAccepted       string `json:"relay_accepted"`
	RelayDelivered      string `json:"relay_delivered"`
	ViewerObserved      string `json:"viewer_observed"`
	ViewerAcknowledged  string `json:"viewer_acknowledged"`
}

// ReadReceipt is effect-free. It may inspect an earlier execution after the
// status row has moved to a new generation, but the request keeps its triple.
func (c *ViewerClient) ReadReceipt(ctx context.Context, destination ViewerDestination, action, targetRequest, queryRequest string) (ViewerReceipt, error) {
	if !destination.Valid() || !validViewerRequestID(targetRequest) || !validViewerRequestID(queryRequest) {
		return ViewerReceipt{}, ViewerRefusal{Code: "execution_target_required"}
	}
	switch action {
	case "send", "answer", "interrupt", "end":
	default:
		return ViewerReceipt{}, ViewerRefusal{Code: "unsupported_action"}
	}
	if err := c.RefreshMachinePins(ctx); err != nil {
		return ViewerReceipt{}, err
	}
	if !c.Paired(destination.MachineID) {
		return ViewerReceipt{}, c.pairingRefusal(destination.MachineID)
	}
	if err := c.WaitReady(ctx); err != nil {
		return ViewerReceipt{}, err
	}
	path := "t/" + viewerChannelSegment(destination.MachineID) + "/" + viewerChannelSegment(destination.SessionID)
	if err := c.transport.Subscribe(path); err != nil {
		return ViewerReceipt{}, err
	}
	defer func() { _ = c.transport.Unsubscribe(path) }()
	body, err := c.request(ctx, destination, "ctl", "read:"+queryRequest, map[string]any{
		"type": "session-receipt", "session": destination.SessionID, "request": queryRequest,
		"target_request": targetRequest, "execution_generation": destination.ExecutionGeneration,
		"action": action,
	}, false)
	if err != nil {
		return ViewerReceipt{}, err
	}
	var receipt ViewerReceipt
	if json.Unmarshal(body, &receipt) != nil || receipt.Request != targetRequest || receipt.Action != action ||
		receipt.ExecutionGeneration != destination.ExecutionGeneration {
		return ViewerReceipt{}, ViewerRefusal{Code: "bad_receipt"}
	}
	switch receipt.MachineExecution {
	case "missing", "pending", "completed", "rejected", "unknown":
		return receipt, nil
	default:
		return ViewerReceipt{}, ViewerRefusal{Code: "bad_receipt"}
	}
}

// ReadDetail pins the same execution before and after both effect-free r/
// reads. Rich and transcript channels are held only while this call is open.
func (c *ViewerClient) ReadDetail(ctx context.Context, destination ViewerDestination) (ViewerDetail, error) {
	if err := c.requireCurrentDestination(ctx, destination); err != nil {
		return ViewerDetail{}, err
	}
	suffix := viewerChannelSegment(destination.MachineID) + "/" + viewerChannelSegment(destination.SessionID)
	c.openRich(destination)
	defer c.closeRich(destination)
	if err := c.transport.Subscribe("s/"+suffix, "t/"+suffix); err != nil {
		return ViewerDetail{}, err
	}
	defer func() { _ = c.transport.Unsubscribe("s/"+suffix, "t/"+suffix) }()
	info, err := c.request(ctx, destination, "r", "info.full", map[string]any{
		"type": "info", "session": destination.SessionID, "parts": "full",
		"machine_id": destination.MachineID, "expected_generation": destination.ExecutionGeneration,
	}, false)
	if err != nil {
		return ViewerDetail{}, err
	}
	if err := c.requireCurrentDestination(ctx, destination); err != nil {
		return ViewerDetail{}, err
	}
	transcript, nextBefore, err := c.readTranscript(ctx, destination, 0)
	if err != nil {
		return ViewerDetail{}, err
	}
	if err := c.requireCurrentDestination(ctx, destination); err != nil {
		return ViewerDetail{}, err
	}
	question := c.currentQuestion(destination, time.Now())
	if question == nil {
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		for question == nil {
			select {
			case <-c.richUpdate:
				question = c.currentQuestion(destination, time.Now())
			case <-timer.C:
				return ViewerDetail{Destination: destination, Info: info, Transcript: transcript, NextBefore: nextBefore}, nil
			case <-ctx.Done():
				return ViewerDetail{}, ctx.Err()
			}
		}
	}
	return ViewerDetail{Destination: destination, Info: info, Transcript: transcript, NextBefore: nextBefore, Question: question}, nil
}

// ReadTranscriptPage fetches one older page. The cursor names a byte boundary
// in the same transcript; each request separately rechecks the live execution.
func (c *ViewerClient) ReadTranscriptPage(ctx context.Context, destination ViewerDestination, before int64) (ViewerTranscriptPage, error) {
	if before <= 0 {
		return ViewerTranscriptPage{}, ViewerRefusal{Code: "bad_request"}
	}
	if err := c.requireCurrentDestination(ctx, destination); err != nil {
		return ViewerTranscriptPage{}, err
	}
	path := "t/" + viewerChannelSegment(destination.MachineID) + "/" + viewerChannelSegment(destination.SessionID)
	if err := c.transport.Subscribe(path); err != nil {
		return ViewerTranscriptPage{}, err
	}
	defer func() { _ = c.transport.Unsubscribe(path) }()
	transcript, nextBefore, err := c.readTranscript(ctx, destination, before)
	if err != nil {
		return ViewerTranscriptPage{}, err
	}
	if err := c.requireCurrentDestination(ctx, destination); err != nil {
		return ViewerTranscriptPage{}, err
	}
	return ViewerTranscriptPage{Destination: destination, Transcript: transcript, NextBefore: nextBefore}, nil
}

func (c *ViewerClient) requireCurrentDestination(ctx context.Context, destination ViewerDestination) error {
	if !destination.Valid() {
		return ViewerRefusal{Code: "execution_target_required"}
	}
	projection, err := c.ReadMachine(ctx, destination.MachineID)
	if err != nil {
		return err
	}
	if projection.Kind != "ready" {
		return ViewerRefusal{Code: projection.Reason}
	}
	switch projection.Available(destination) {
	case "current":
		return nil
	case "stale":
		return ViewerRefusal{Code: "stale"}
	case "unknown":
		return ViewerRefusal{Code: "viewer_status_unavailable"}
	default:
		return ViewerRefusal{Code: "execution_generation_changed"}
	}
}
