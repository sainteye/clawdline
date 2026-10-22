package antigravitylaunch

import (
	"context"
	"encoding/json"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app/ports"
)

// PolicyVersion binds model and effort as well as the native permission plan.
const PolicyVersion = 2

const nativePolicyVersion = PolicyVersion

// NativePolicy is private launch evidence. It never grants permission by
// itself: production must recheck the compiled native behavior, current opt-in,
// hook plan, exact image and live process before each effect. Store owns no
// account predicate or compatibility table.
type NativePolicy struct {
	Model            string `json:"model,omitempty"`
	Effort           string `json:"effort,omitempty"`
	Version          int    `json:"version"`
	Ceiling          string `json:"ceiling"`
	ExecutableDigest string `json:"executable_digest"`
	CompatibilityID  string `json:"compatibility_id"`
	NativeMode       string `json:"native_mode"`
	HookConfigDigest string `json:"hook_config_digest"`
	HelperDigest     string `json:"helper_digest"`
	ConsentRevision  string `json:"consent_revision"`
}

// Structural validation permits a future policy version to retain readable
// history. Authority below accepts only the version this reader understands.
func (p NativePolicy) wellFormed() bool {
	return p.Version > 0 && (p.Ceiling == "ask" || p.Ceiling == "edits" || p.Ceiling == "full") &&
		validHash(p.ExecutableDigest) && safeText(p.CompatibilityID) && safeText(p.NativeMode) &&
		validHash(p.HookConfigDigest) && validHash(p.HelperDigest) && validHash(p.ConsentRevision)
}
func copyPolicy(p *NativePolicy) *NativePolicy {
	if p == nil {
		return nil
	}
	copy := *p
	return &copy
}
func samePolicy(a, b *NativePolicy) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
func cloneRecord(r Record) Record { r.Policy = copyPolicy(r.Policy); return r }
func sameRecord(a, b Record) bool {
	equal := samePolicy(a.Policy, b.Policy)
	a.Policy, b.Policy = nil, nil
	return equal && a == b
}
func recordAuthority(r Record) error {
	if r.Revoked {
		return fail("launch_revoked")
	}
	if r.Policy != nil && r.Policy.Version != nativePolicyVersion {
		return fail("policy_version_unsupported")
	}
	return nil
}
func launchRequestDigest(req projects.LaunchRequest, policy *NativePolicy) (string, error) {
	// Preserve legacy request identity. Policy-bearing launches use an explicit
	// envelope in the SAME index transaction as their reserved launch slot.
	var value any = req
	if policy != nil {
		value = struct {
			Request projects.LaunchRequest `json:"request"`
			Policy  NativePolicy           `json:"policy"`
		}{req, *policy}
	}
	b, err := json.Marshal(value)
	if err != nil {
		return "", fail("request_invalid")
	}
	return hash(b), nil
}

// PrepareLaunchWithPolicy binds a server-selected policy to one durable ID.
// A persisted policy is preparation, never evidence of current native behavior.
func (s *Store) PrepareLaunchWithPolicy(ctx context.Context, id string, req projects.LaunchRequest, policy NativePolicy) (Prepared, error) {
	if !policy.wellFormed() {
		return Prepared{}, fail("policy_invalid")
	}
	if policy.Version != nativePolicyVersion {
		return Prepared{}, fail("policy_version_unsupported")
	}
	if policy.NativeMode != req.PermissionMode {
		return Prepared{}, fail("policy_mode_conflict")
	}
	if policy.Model != req.Model || policy.Effort != req.ReasoningEffort {
		return Prepared{}, fail("policy_request_conflict")
	}
	return s.prepareLaunch(ctx, id, req, &policy)
}

// PolicyForLaunch refuses policy-free legacy preparation. It is the required
// production entry point, not a replacement for fresh live policy checks.
func (s *Store) PolicyForLaunch(ctx context.Context, id string) (NativePolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return NativePolicy{}, err
	}
	r, err := s.record(id)
	if err != nil {
		return NativePolicy{}, err
	}
	if r.Policy == nil {
		return NativePolicy{}, fail("policy_unavailable")
	}
	if _, err := s.load(r); err != nil {
		return NativePolicy{}, err
	}
	if _, err := s.capability(r); err != nil {
		return NativePolicy{}, err
	}
	if err := cancelled(ctx); err != nil {
		return NativePolicy{}, err
	}
	return *r.Policy, nil
}

// RevokeLaunch durably removes managed authority without erasing original
// context, native history, loaded proof or consumed first-input slots. It does
// not stop an independently running provider. Repeated revocation is idempotent.
func (s *Store) RevokeLaunch(ctx context.Context, id string) error {
	return s.revokeLaunch(ctx, id, false)
}

// RevokeUnopenedLaunch additionally refuses a recorded terminal or process.
// The caller must still positively prove that no opener effect occurred; an
// absent terminal receipt alone cannot establish that an open did not happen.
func (s *Store) RevokeUnopenedLaunch(ctx context.Context, id string) error {
	return s.revokeLaunch(ctx, id, true)
}
func (s *Store) revokeLaunch(ctx context.Context, id string, unopened bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return err
	}
	r, ok := s.state.Records[id]
	if !ok {
		return fail("launch_unknown")
	}
	if r.Revoked {
		delete(s.active, id)
		return nil
	}
	if unopened && (r.TerminalID != "" || r.Process.PID != 0) {
		return fail("opening_outcome_not_unopened")
	}
	// Invalidate volatile authority even if durable publication fails. A
	// failed/uncertain result is not permission to reopen or reuse this launch.
	delete(s.active, id)
	r.Revoked = true
	n := clone(s.state)
	n.Records[id] = r
	if err := s.write(ctx, n); err != nil {
		s.uncertain = true
		return err
	}
	return nil
}

// ManagedRecordForProcess is provenance, not live authority. A revoked,
// inactive or future-policy launch MUST remain distinguishable from an
// unmanaged native process. False is returned only after a complete bounded
// private-state read, never on unavailable or ambiguous ownership.
func (s *Store) ManagedRecordForProcess(ctx context.Context, p ports.AntigravityProcess) (Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return Record{}, false, err
	}
	if !completeProcess(p) {
		return Record{}, false, fail("process_invalid")
	}
	var found Record
	for _, record := range s.state.Records {
		if err := cancelled(ctx); err != nil {
			return Record{}, false, err
		}
		if record.Process == p {
			if found.ID != "" {
				return Record{}, false, fail("process_ambiguous")
			}
			found = cloneRecord(record)
		}
	}
	return found, found.ID != "", nil
}
