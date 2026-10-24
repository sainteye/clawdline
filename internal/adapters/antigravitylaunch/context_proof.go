package antigravitylaunch

import (
	"context"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
)

func validContextFence(f transcript.AntigravityContextFence) bool {
	return f.Binding.Verified && f.Binding.Event == "PreInvocation" && f.Binding.Sequence > 0 && validID(f.Binding.ConversationID) && completeProcess(f.Binding.Process) && absolute(f.Binding.TranscriptPath) && f.Generation != "" && f.Generation != "unknown" && validHash(f.PrefixDigest) && f.Bytes >= 0 && f.Bytes <= transcript.ReadBudget && f.LastStep >= -1
}
func validContextReceipt(r ResponseReceipt) bool {
	if !r.Fenced {
		return !r.Loaded && r.Fence == (transcript.AntigravityContextFence{}) && r.Proof == (transcript.AntigravityContextProof{})
	}
	f := r.Fence
	if !validContextFence(f) || f.Binding.Process != r.Process || f.Binding.ConversationID != r.ConversationID || f.Binding.Sequence != r.Sequence || bindingDigest(f.Binding) != r.BindingDigest {
		return false
	}
	if !r.Loaded {
		return r.Proof == (transcript.AntigravityContextProof{})
	}
	p := r.Proof
	return p.Fence == f && p.RowStart >= f.Bytes && p.RowEnd > p.RowStart && p.RowEnd <= transcript.ReadBudget && p.StepIndex > f.LastStep && validHash(p.PrefixDigest) && p.InjectionDigest == r.InjectionDigest
}

// proofSnapshot owns no native IO. Native composition must supply a fresh
// verifier to the reader; no hook callback runs while the Store mutex is held.
func (s *Store) proofSnapshot(ctx context.Context, id string) (ResponseReceipt, ExpectedInjection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return ResponseReceipt{}, ExpectedInjection{}, err
	}
	r, err := s.record(id)
	if err != nil {
		return ResponseReceipt{}, ExpectedInjection{}, err
	}
	receipt, ok := s.state.Receipts[id]
	if !ok || !receipt.Fenced || !validContextReceipt(receipt) {
		return ResponseReceipt{}, ExpectedInjection{}, fail("proof_unavailable")
	}
	live, err := s.lookup(receipt.Process)
	if err != nil || live.ID != id || r.NativeID != receipt.ConversationID {
		return ResponseReceipt{}, ExpectedInjection{}, fail("process_unverified")
	}
	c, err := s.load(r)
	if err != nil {
		return ResponseReceipt{}, ExpectedInjection{}, err
	}
	if _, err := s.capability(r); err != nil {
		return ResponseReceipt{}, ExpectedInjection{}, err
	}
	injection, err := expected(c)
	if err != nil || injection.Digest != receipt.InjectionDigest {
		return ResponseReceipt{}, ExpectedInjection{}, fail("proof_stale")
	}
	return receipt, injection, nil
}

// VerifyContextLoaded reads original private bytes and native evidence, then
// atomically records proof only for the SAME latest prepared receipt. Reading
// a historical proof after restart cannot reactivate the saved process.
func (s *Store) VerifyContextLoaded(ctx context.Context, id, home string, check transcript.AntigravityContextCheck) error {
	receipt, injection, err := s.proofSnapshot(ctx, id)
	if err != nil {
		return err
	}
	proof, err := transcript.VerifyAntigravityContextLoaded(ctx, home, receipt.Fence, injection.Text, check)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return err
	}
	current, ok := s.state.Receipts[id]
	if !ok || current != receipt {
		return fail("proof_stale")
	}
	live, err := s.lookup(receipt.Process)
	if err != nil || live.ID != id || live.NativeID != receipt.ConversationID {
		return fail("process_unverified")
	}
	if _, err := s.load(live); err != nil {
		return err
	}
	if _, err := s.capability(live); err != nil {
		return err
	}
	if receipt.Loaded {
		if receipt.Proof != proof {
			return fail("proof_stale")
		}
		return cancelled(ctx)
	}
	receipt.Loaded = true
	receipt.Proof = proof
	n := clone(s.state)
	n.Receipts[id] = receipt
	return s.write(ctx, n)
}

// CheckContextLoaded is current badge authority only when composed with a
// fresh native verifier. The persisted Loaded bit is historical evidence.
func (s *Store) CheckContextLoaded(ctx context.Context, id, home string, check transcript.AntigravityContextCheck) error {
	receipt, injection, err := s.proofSnapshot(ctx, id)
	if err != nil {
		return err
	}
	if !receipt.Loaded {
		return fail("proof_pending")
	}
	if err := transcript.RecheckAntigravityContextLoaded(ctx, home, receipt.Proof, injection.Text, check); err != nil {
		return err
	}
	current, _, err := s.proofSnapshot(ctx, id)
	if err != nil {
		return err
	}
	if current != receipt {
		return fail("proof_stale")
	}
	return nil
}
