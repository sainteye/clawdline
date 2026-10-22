package antigravitylaunch

import (
	"context"

	"github.com/sainteye/clawdline/internal/app/ports"
)

type FirstInputPhase string

const (
	FirstInputReserved       FirstInputPhase = "reserved_before_bytes"
	FirstInputPastePossible  FirstInputPhase = "possibly_pasted"
	FirstInputEnterAttempted FirstInputPhase = "enter_attempted"
)

// FirstInputReceipt is private metadata, never message content or delivery
// proof. A retained slot is consumed even if no terminal bytes were written.
type FirstInputReceipt struct {
	LaunchID    string                   `json:"launch_id"`
	RequestID   string                   `json:"request_id"`
	TextDigest  string                   `json:"text_digest"`
	ContextHash string                   `json:"context_hash"`
	Backend     string                   `json:"backend"`
	Process     ports.AntigravityProcess `json:"process"`
	Phase       FirstInputPhase          `json:"phase"`
}

func firstInputPhase(phase FirstInputPhase) bool {
	return phase == FirstInputReserved || phase == FirstInputPastePossible || phase == FirstInputEnterAttempted
}

// FirstInputAttempt is a volatile execution grant issued only to the durable
// creator. Its unexported owner cannot be reconstructed from stored metadata.
// Reopening a Store never revives a grant, even after fresh process pinning.
// Root must separately fence kernel/foreground/request policy at every effect.
type FirstInputAttempt struct {
	owner   *Store
	receipt FirstInputReceipt
}

// ReserveFirstInput consumes one slot before terminal IO. Identical retries
// return status and a nil attempt, never another grant. Text is only a digest.
func (s *Store) ReserveFirstInput(ctx context.Context, id, key, textDigest string, p ports.AntigravityProcess) (FirstInputReceipt, *FirstInputAttempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return FirstInputReceipt{}, nil, err
	}
	if !validID(key) || !validHash(textDigest) || !completeProcess(p) {
		return FirstInputReceipt{}, nil, fail("first_input_invalid")
	}
	r, err := s.record(id)
	if err != nil {
		return FirstInputReceipt{}, nil, err
	}
	if old, ok := s.state.FirstInputs[id]; ok {
		if old.RequestID != key || old.TextDigest != textDigest || old.Process != p {
			return FirstInputReceipt{}, nil, fail("first_input_conflict")
		}
		return old, nil, nil
	}
	if r.RequestedResume != "" || r.NativeID != "" {
		return FirstInputReceipt{}, nil, fail("first_input_not_new")
	}
	current, err := s.lookup(p)
	if err != nil {
		return FirstInputReceipt{}, nil, err
	}
	if current.ID != id {
		return FirstInputReceipt{}, nil, fail("first_input_conflict")
	}
	if _, err = s.load(r); err != nil {
		return FirstInputReceipt{}, nil, err
	}
	if _, err = s.capability(r); err != nil {
		return FirstInputReceipt{}, nil, err
	}
	if len(s.state.FirstInputs) >= MaxFirstInputs {
		return FirstInputReceipt{}, nil, fail("first_inputs_full")
	}
	receipt := FirstInputReceipt{LaunchID: id, RequestID: key, TextDigest: textDigest, ContextHash: r.ContextHash, Backend: r.Backend, Process: p, Phase: FirstInputReserved}
	n := clone(s.state)
	n.FirstInputs[id] = receipt
	if err = s.write(ctx, n); err != nil {
		return FirstInputReceipt{}, nil, err
	}
	// A cancellation after publication consumes the slot without granting IO.
	if err = cancelled(ctx); err != nil {
		return FirstInputReceipt{}, nil, err
	}
	return receipt, &FirstInputAttempt{owner: s, receipt: receipt}, nil
}

func (s *Store) FirstInputStatus(ctx context.Context, id, key string) (FirstInputReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return FirstInputReceipt{}, err
	}
	r, ok := s.state.FirstInputs[id]
	if !ok {
		return FirstInputReceipt{}, fail("first_input_unavailable")
	}
	if r.RequestID != key {
		return FirstInputReceipt{}, fail("first_input_conflict")
	}
	return r, nil
}

// Check verifies the same owned context/process but is not a kernel read.
func (a *FirstInputAttempt) Check(ctx context.Context) error {
	if a == nil || a.owner == nil {
		return fail("first_input_grant_unavailable")
	}
	s := a.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	return a.checkLocked(ctx)
}
func (a *FirstInputAttempt) checkLocked(ctx context.Context) error {
	s := a.owner
	if err := s.ready(ctx); err != nil {
		return err
	}
	receipt, ok := s.state.FirstInputs[a.receipt.LaunchID]
	if !ok || receipt != a.receipt {
		return fail("first_input_grant_stale")
	}
	r, err := s.lookup(receipt.Process)
	if err != nil {
		return err
	}
	if r.ID != receipt.LaunchID || r.ContextHash != receipt.ContextHash || r.Backend != receipt.Backend {
		return fail("first_input_grant_stale")
	}
	if _, err = s.load(r); err != nil {
		return err
	}
	_, err = s.capability(r)
	return err
}
func (a *FirstInputAttempt) advance(ctx context.Context, from, to FirstInputPhase) error {
	if a == nil || a.owner == nil {
		return fail("first_input_grant_unavailable")
	}
	s := a.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := a.checkLocked(ctx); err != nil {
		return err
	}
	if a.receipt.Phase != from {
		return fail("first_input_phase_conflict")
	}
	next := a.receipt
	next.Phase = to
	n := clone(s.state)
	n.FirstInputs[next.LaunchID] = next
	if err := s.write(ctx, n); err != nil {
		return err
	}
	a.receipt = next
	return cancelled(ctx)
}

// BeforePaste and BeforeEnter persist uncertainty BEFORE the corresponding
// terminal effect. Repeated calls refuse; no method releases or resets a slot.
func (a *FirstInputAttempt) BeforePaste(ctx context.Context) error {
	return a.advance(ctx, FirstInputReserved, FirstInputPastePossible)
}
func (a *FirstInputAttempt) BeforeEnter(ctx context.Context) error {
	return a.advance(ctx, FirstInputPastePossible, FirstInputEnterAttempted)
}
