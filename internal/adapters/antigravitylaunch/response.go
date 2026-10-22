package antigravitylaunch

import (
	"context"
	"encoding/json"

	"github.com/sainteye/clawdline/internal/adapters/antigravity"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app/ports"
)

// ResponseReceipt is the latest prepared response per launch. Sequence forms a
// high-water mark, so bounded retention does not permit old events to execute.
// This is never a delivered/loaded acknowledgement or a native role badge.
type ResponseReceipt struct {
	LaunchID        string                   `json:"launch_id"`
	Process         ports.AntigravityProcess `json:"process"`
	ConversationID  string                   `json:"conversation_id"`
	Sequence        uint64                   `json:"sequence"`
	BindingDigest   string                   `json:"binding_digest"`
	Status          string                   `json:"status"`
	InjectionDigest string                   `json:"injection_digest"`
	ResponseDigest  string                   `json:"response_digest"`
}
type ExpectedInjection struct {
	Text   string
	Digest string
}

func expected(c projects.AntigravityContext) (ExpectedInjection, error) {
	b, err := projects.AntigravityContextResponse(c, "PreInvocation", json.RawMessage(`{}`))
	if err != nil {
		return ExpectedInjection{}, fail("response_invalid")
	}
	var response struct {
		InjectSteps []struct {
			SystemMessage struct {
				SystemMessage string `json:"systemMessage"`
			} `json:"systemMessage"`
		} `json:"injectSteps"`
	}
	if json.Unmarshal(b, &response) != nil {
		return ExpectedInjection{}, fail("response_invalid")
	}
	text := ""
	if len(response.InjectSteps) > 0 {
		text = response.InjectSteps[0].SystemMessage.SystemMessage
	}
	return ExpectedInjection{Text: text, Digest: hash([]byte(text))}, nil
}

// ExpectedInjection loads immutable bytes for a separate native transcript
// proof reader. A model's answer repeating these bytes is not such a proof.
func (s *Store) ExpectedInjection(ctx context.Context, id string) (ExpectedInjection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return ExpectedInjection{}, err
	}
	r, err := s.record(id)
	if err != nil {
		return ExpectedInjection{}, err
	}
	c, err := s.load(r)
	if err != nil {
		return ExpectedInjection{}, err
	}
	return expected(c)
}

// PreInvocationResponse accepts only the exact binding previously persisted
// by RecordBinding, including its server sequence and complete process. Root
// adapts this directly to antigravity.HookResponse after trusted admission.
func (s *Store) PreInvocationResponse(ctx context.Context, b ports.AntigravityBinding) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	r, err := s.lookup(b.Process)
	if err != nil {
		return nil, err
	}
	if !b.Verified || b.Event != "PreInvocation" || r.NativeID != b.ConversationID || r.BindingSequence != b.Sequence || r.BindingDigest != bindingDigest(b) {
		return nil, fail("binding_unverified")
	}
	c, err := s.load(r)
	if err != nil {
		return nil, err
	}
	observer, err := antigravity.ObserverResponse(b.Event)
	if err != nil {
		return nil, fail("response_invalid")
	}
	response, err := projects.AntigravityContextResponse(c, b.Event, observer)
	if err != nil {
		return nil, fail("response_invalid")
	}
	injection, err := expected(c)
	if err != nil {
		return nil, err
	}
	receipt := ResponseReceipt{LaunchID: r.ID, Process: b.Process, ConversationID: b.ConversationID, Sequence: b.Sequence, BindingDigest: bindingDigest(b), Status: "PREPARED", InjectionDigest: injection.Digest, ResponseDigest: hash(response)}
	if old, ok := s.state.Receipts[r.ID]; ok {
		if old.Sequence >= receipt.Sequence {
			if old == receipt {
				if err := cancelled(ctx); err != nil {
					return nil, err
				}
				return response, nil
			}
			return nil, fail("response_replayed")
		}
	} else if len(s.state.Receipts) >= MaxReceipts {
		return nil, fail("receipts_full")
	}
	n := clone(s.state)
	n.Receipts[r.ID] = receipt
	if err := s.write(ctx, n); err != nil {
		return nil, err
	}
	return response, nil
}
func (s *Store) Receipt(ctx context.Context, id string) (ResponseReceipt, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return ResponseReceipt{}, false, err
	}
	r, ok := s.state.Receipts[id]
	return r, ok, nil
}
