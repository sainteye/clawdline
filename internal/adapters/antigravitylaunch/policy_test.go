package antigravitylaunch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func syntheticNativePolicy() NativePolicy {
	return NativePolicy{
		Version: nativePolicyVersion, Ceiling: "ask", NativeMode: "plan",
		ExecutableDigest: hash([]byte("synthetic executable")), CompatibilityID: "synthetic-behavior-v1",
		HookConfigDigest: hash([]byte("synthetic hook plan")), HelperDigest: hash([]byte("synthetic helper")),
		ConsentRevision: hash([]byte("synthetic explicit project opt-in")),
	}
}
func preparePolicy(t *testing.T, f fixture) Prepared {
	t.Helper()
	p, err := f.store.PrepareLaunchWithPolicy(context.Background(), uuid.NewString(), f.request, syntheticNativePolicy())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNativePolicyIsImmutableAndBoundToTheLaunch(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	original := syntheticNativePolicy()
	id := uuid.NewString()
	p, err := f.store.PrepareLaunchWithPolicy(ctx, id, f.request, original)
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.store.PrepareLaunchWithPolicy(ctx, id, f.request, original)
	if err != nil || !sameRecord(p.Record, again.Record) || p.Record.RequestDigest == "" {
		t.Fatal("retry changed prepared intent", err)
	}
	// Returned private values must not alias the transaction's policy.
	p.Record.Policy.Ceiling = "full"
	got, err := f.store.PolicyForLaunch(ctx, id)
	if err != nil || got != original {
		t.Fatal("caller changed durable policy", err)
	}
	record, err := f.store.PreparedRecord(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	record.Policy.NativeMode = "auto"
	current, err := f.store.PreparedRecord(ctx, id)
	if err != nil || *current.Policy != original {
		t.Fatal("prepared record leaked policy pointer", err)
	}
	for _, change := range []func(*NativePolicy){
		func(p *NativePolicy) { p.Ceiling = "edits" },
		func(p *NativePolicy) { p.ExecutableDigest = hash([]byte("another executable")) },
		func(p *NativePolicy) { p.CompatibilityID = "synthetic-behavior-v2" },
		func(p *NativePolicy) { p.HookConfigDigest = hash([]byte("another hook plan")) },
		func(p *NativePolicy) { p.HelperDigest = hash([]byte("another helper")) },
		func(p *NativePolicy) { p.ConsentRevision = hash([]byte("another explicit opt-in")) },
	} {
		next := original
		change(&next)
		_, err := f.store.PrepareLaunchWithPolicy(ctx, id, f.request, next)
		code(t, err, "launch_conflict")
	}
	next := original
	next.NativeMode = "auto"
	req := f.request
	req.PermissionMode = "auto"
	_, err = f.store.PrepareLaunchWithPolicy(ctx, id, req, next)
	code(t, err, "launch_conflict")
	_, err = f.store.PrepareLaunch(ctx, id, f.request)
	code(t, err, "launch_conflict")
	native := pin(t, f.store, again)
	r, err := f.store.LookupProcess(ctx, native)
	if err != nil {
		t.Fatal(err)
	}
	// Comparison is by immutable values, including a copied policy.
	if _, _, err := f.store.Commitment(ctx, r); err != nil {
		t.Fatal("independent record copy failed commitment", err)
	}
	r.Policy.Ceiling = "full"
	if _, _, err := f.store.Commitment(ctx, r); err == nil {
		t.Fatal("changed policy issued commitment")
	}
	legacy := prep(t, f)
	_, err = f.store.PolicyForLaunch(ctx, legacy.Record.ID)
	code(t, err, "policy_unavailable")
	_, err = f.store.PrepareLaunchWithPolicy(ctx, legacy.Record.ID, f.request, original)
	code(t, err, "launch_conflict")
}

func TestNativePolicyRevokePreservesHistoryButNeverReactivates(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	p := preparePolicy(t, f)
	native := pin(t, f.store, p)
	key := uuid.NewString()
	slot, attempt, err := f.store.ReserveFirstInput(ctx, p.Record.ID, key, hash([]byte("synthetic first input")), native)
	if err != nil || attempt == nil {
		t.Fatal(err)
	}
	b := binding(native, uuid.NewString(), 1)
	if err := f.store.RecordBinding(ctx, p.Record.ID, b, KeepConversation); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.PreInvocationResponse(ctx, b); err != nil {
		t.Fatal(err)
	}
	before, ok, err := f.store.Receipt(ctx, p.Record.ID)
	if err != nil || !ok {
		t.Fatal(err)
	}
	r, err := f.store.LookupProcess(ctx, native)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := os.ReadFile(p.Record.CapabilityPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.RevokeLaunch(ctx, p.Record.ID); err != nil {
		t.Fatal(err)
	}
	checkRefusal := func(s *Store) {
		t.Helper()
		_, err := s.PolicyForLaunch(ctx, p.Record.ID)
		code(t, err, "launch_revoked")
		_, err = s.PreparedRecord(ctx, p.Record.ID)
		code(t, err, "launch_revoked")
		_, err = s.VerifyLaunchCapability(ctx, p.Record.ID, string(capability))
		code(t, err, "launch_revoked")
		code(t, s.PinProcess(ctx, p.Record.ID, native), "launch_revoked")
		code(t, s.AttachTerminal(ctx, p.Record.ID, "tmux", native.TerminalID), "launch_revoked")
		if _, err := s.LookupProcess(ctx, native); err == nil {
			t.Fatal("revoked process stayed active")
		}
		if _, _, err := s.Commitment(ctx, r); err == nil {
			t.Fatal("revoked record issued commitment")
		}
		_, err = s.PrepareLaunchWithPolicy(ctx, p.Record.ID, f.request, syntheticNativePolicy())
		code(t, err, "launch_revoked")
		if _, err := s.PreInvocationResponse(ctx, b); err == nil {
			t.Fatal("revoked launch injected context")
		}
		if err := s.RecordBinding(ctx, p.Record.ID, binding(native, b.ConversationID, 2), KeepConversation); err == nil {
			t.Fatal("revoked process rebound")
		}
		after, ok, err := s.Receipt(ctx, p.Record.ID)
		if err != nil || !ok || after != before {
			t.Fatal("revocation erased response evidence", err)
		}
		saved, err := s.FirstInputStatus(ctx, p.Record.ID, key)
		if err != nil || saved != slot {
			t.Fatal("revocation reset consumed slot", err)
		}
		restored, err := s.Restore(ctx, f.request.ProjectRoot, b.ConversationID)
		if err != nil || restored != *p.Request.AntigravityContext {
			t.Fatal("revocation erased original history", err)
		}
	}
	checkRefusal(f.store)
	if err := attempt.Check(ctx); err == nil {
		t.Fatal("outstanding grant survived revocation")
	}
	if err := attempt.BeforePaste(ctx); err == nil {
		t.Fatal("outstanding grant advanced after revocation")
	}
	if err := f.store.RevokeLaunch(ctx, p.Record.ID); err != nil {
		t.Fatal("revoke was not idempotent", err)
	}
	f.store.Close()
	reopened, err := Open(f.config)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	checkRefusal(reopened)
	// A fresh resume has current policy and the original immutable context,
	// rather than resurrecting the revoked launch or its consumed slot.
	req := f.request
	req.Resume = b.ConversationID
	req.Memory = "new mutable memory"
	resumed, err := reopened.PrepareLaunchWithPolicy(ctx, uuid.NewString(), req, syntheticNativePolicy())
	if err != nil || *resumed.Request.AntigravityContext != *p.Request.AntigravityContext {
		t.Fatal("fresh resume lost original context", err)
	}
}

func TestNativePolicyPublicationDoesNotExposePartialAuthority(t *testing.T) {
	for _, phase := range []string{"index", "context"} {
		t.Run(phase, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			id := uuid.NewString()
			f.store.beforeWrite = func(at string) error {
				if at == phase {
					return errors.New("synthetic write failure")
				}
				return nil
			}
			_, err := f.store.PrepareLaunchWithPolicy(ctx, id, f.request, syntheticNativePolicy())
			code(t, err, "write_failed")
			if _, err := f.store.PolicyForLaunch(ctx, id); err == nil {
				t.Fatal("partial policy authorized")
			}
			f.store.Close()
			s, err := Open(f.config)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			changed := syntheticNativePolicy()
			changed.Ceiling = "full"
			if phase == "context" {
				_, err := s.PrepareLaunchWithPolicy(ctx, id, f.request, changed)
				code(t, err, "launch_conflict")
			}
			p, err := s.PrepareLaunchWithPolicy(ctx, id, f.request, syntheticNativePolicy())
			if err != nil || !p.Record.Prepared {
				t.Fatal("identical recovery failed", err)
			}
			entries, err := os.ReadDir(filepath.Dir(p.Record.ContextPath))
			if err != nil || len(entries) != 2 {
				t.Fatal("recovery left unowned files", err)
			}
		})
	}
}

func TestNativePolicyFailedRevokeInvalidatesVolatileGrants(t *testing.T) {
	for _, kind := range []string{"before-publication", "lost-publication"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			f := setup(t)
			p := preparePolicy(t, f)
			native := pin(t, f.store, p)
			_, attempt, err := f.store.ReserveFirstInput(ctx, p.Record.ID, uuid.NewString(), hash([]byte("synthetic input")), native)
			if err != nil || attempt == nil {
				t.Fatal(err)
			}
			callCtx := ctx
			if kind == "before-publication" {
				f.store.beforeWrite = func(string) error { return errors.New("synthetic disk full") }
			} else {
				var cancel context.CancelFunc
				callCtx, cancel = context.WithCancel(ctx)
				defer cancel()
				f.store.afterPublish = cancel
			}
			if err := f.store.RevokeLaunch(callCtx, p.Record.ID); err == nil {
				t.Fatal("failed/uncertain revoke reported success")
			}
			if err := attempt.Check(ctx); err == nil {
				t.Fatal("uncertain revoke retained volatile grant")
			}
			if err := f.store.PinProcess(ctx, p.Record.ID, native); err == nil {
				t.Fatal("failed revoke reactivated process in same store")
			}
			f.store.Close()
			s, err := Open(f.config)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if kind == "lost-publication" {
				_, err := s.PolicyForLaunch(ctx, p.Record.ID)
				code(t, err, "launch_revoked")
			}
			// A pre-publication failure was not a durable revoke. Retry the
			// exact operation after reopening; do not infer success or reopen a terminal.
			if err := s.RevokeLaunch(ctx, p.Record.ID); err != nil {
				t.Fatal(err)
			}
			code(t, s.PinProcess(ctx, p.Record.ID, native), "launch_revoked")
		})
	}
}

func rewritePolicyIndex(t *testing.T, f fixture, change func(map[string]any)) []byte {
	t.Helper()
	f.store.Close()
	path := filepath.Join(f.config.StateDir, "context-index.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var index map[string]any
	if err := json.Unmarshal(data, &index); err != nil {
		t.Fatal(err)
	}
	change(index)
	data, err = json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	put(t, path, data)
	return data
}

func TestNativePolicyVersionThreeMigrationRetainsOriginalProof(t *testing.T) {
	ctx := context.Background()
	f, p, b, home, path, check, fence, data := proofFixture(t)
	if _, err := f.store.PrepareFencedResponse(ctx, b, fence); err != nil {
		t.Fatal(err)
	}
	put(t, path, []byte(data))
	if err := f.store.VerifyContextLoaded(ctx, p.Record.ID, home, check); err != nil {
		t.Fatal(err)
	}
	receipt, _, err := f.store.Receipt(ctx, p.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	old := rewritePolicyIndex(t, f, func(index map[string]any) { index["version"] = 3 })
	s, err := Open(f.config)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	actual, err := os.ReadFile(filepath.Join(f.config.StateDir, "context-index.json"))
	if err != nil || string(actual) != string(old) {
		t.Fatal("Open rewrote legacy index", err)
	}
	_, err = s.PolicyForLaunch(ctx, p.Record.ID)
	code(t, err, "policy_unavailable")
	saved, _, err := s.Receipt(ctx, p.Record.ID)
	if err != nil || saved != receipt {
		t.Fatal("migration erased native proof", err)
	}
	if err := s.CheckContextLoaded(ctx, p.Record.ID, home, check); err == nil {
		t.Fatal("migration reactivated loaded badge")
	}
	restored, err := s.Restore(ctx, f.request.ProjectRoot, b.ConversationID)
	if err != nil || restored != *p.Request.AntigravityContext {
		t.Fatal("migration lost history", err)
	}
	if err := s.Deactivate(ctx, p.Record.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeLaunch(ctx, p.Record.ID); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(f.config.StateDir, "context-index.json"))
	var current map[string]any
	if err != nil || json.Unmarshal(raw, &current) != nil || current["version"] != float64(4) {
		t.Fatal("write did not fence old readers", err)
	}
}

func TestNativePolicyUnknownVersionIsHistoricalOnly(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	p := preparePolicy(t, f)
	native := pin(t, f.store, p)
	b := binding(native, uuid.NewString(), 1)
	if err := f.store.RecordBinding(ctx, p.Record.ID, b, KeepConversation); err != nil {
		t.Fatal(err)
	}
	rewritePolicyIndex(t, f, func(index map[string]any) {
		records := index["records"].(map[string]any)
		policy := records[p.Record.ID].(map[string]any)["policy"].(map[string]any)
		policy["version"] = nativePolicyVersion + 1
	})
	s, err := Open(f.config)
	if err != nil {
		t.Fatal("future policy made history unreadable", err)
	}
	defer s.Close()
	_, err = s.PolicyForLaunch(ctx, p.Record.ID)
	code(t, err, "policy_version_unsupported")
	_, err = s.PreparedRecord(ctx, p.Record.ID)
	code(t, err, "policy_version_unsupported")
	code(t, s.PinProcess(ctx, p.Record.ID, native), "policy_version_unsupported")
	restored, err := s.Restore(ctx, f.request.ProjectRoot, b.ConversationID)
	if err != nil || restored != *p.Request.AntigravityContext {
		t.Fatal("future policy lost original context", err)
	}
	if err := s.RevokeLaunch(ctx, p.Record.ID); err != nil {
		t.Fatal("future authority could not be revoked", err)
	}
}

func TestNativePolicyInvalidAndOversizedPreparationHasNoAuthority(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	policy := syntheticNativePolicy()
	cases := []struct {
		policy NativePolicy
		want   string
	}{}
	for _, mutate := range []func(*NativePolicy){
		func(p *NativePolicy) { p.Version = 0 }, func(p *NativePolicy) { p.Ceiling = "auto" },
		func(p *NativePolicy) { p.ExecutableDigest = "" }, func(p *NativePolicy) { p.CompatibilityID = "\ninvalid" },
		func(p *NativePolicy) { p.NativeMode = "" }, func(p *NativePolicy) { p.HookConfigDigest = "invalid" },
		func(p *NativePolicy) { p.HelperDigest = "invalid" }, func(p *NativePolicy) { p.ConsentRevision = "" },
	} {
		next := policy
		mutate(&next)
		cases = append(cases, struct {
			policy NativePolicy
			want   string
		}{next, "policy_invalid"})
	}
	next := policy
	next.Version++
	cases = append(cases, struct {
		policy NativePolicy
		want   string
	}{next, "policy_version_unsupported"})
	next = policy
	next.NativeMode = "auto"
	cases = append(cases, struct {
		policy NativePolicy
		want   string
	}{next, "policy_mode_conflict"})
	next = policy
	next.CompatibilityID = strings.Repeat("x", MaxIndexBytes)
	cases = append(cases, struct {
		policy NativePolicy
		want   string
	}{next, "index_full"})
	for _, test := range cases {
		id := uuid.NewString()
		_, err := f.store.PrepareLaunchWithPolicy(ctx, id, f.request, test.policy)
		code(t, err, test.want)
		if _, err := f.store.PreparedRecord(ctx, id); err == nil {
			t.Fatal("refused preparation became visible")
		}
	}
	stats, err := f.store.Stats(ctx)
	if err != nil || stats.Launches != 0 || stats.Active != 0 {
		t.Fatal("refused policy consumed authority", stats, err)
	}
	entries, err := os.ReadDir(f.store.launches.path)
	if err != nil || len(entries) != 0 {
		t.Fatal("refused policy left launch files", err)
	}
}

func TestNativePolicyLegacyVersionCannotClaimNewAuthorityFields(t *testing.T) {
	for _, version := range []int{1, 2, 3} {
		for _, kind := range []string{"policy", "revoked"} {
			t.Run(fmt.Sprintf("v%d/%s", version, kind), func(t *testing.T) {
				f := setup(t)
				var p Prepared
				if kind == "policy" {
					p = preparePolicy(t, f)
				} else {
					p = prep(t, f)
					if err := f.store.RevokeLaunch(context.Background(), p.Record.ID); err != nil {
						t.Fatal(err)
					}
				}
				rewritePolicyIndex(t, f, func(index map[string]any) { index["version"] = version })
				s, err := Open(f.config)
				if s != nil {
					s.Close()
				}
				code(t, err, "index_invalid")
			})
		}
	}
}

func TestManagedProvenanceSurvivesDeactivateRevokeAndReopen(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	p := preparePolicy(t, f)
	native := pin(t, f.store, p)
	verify := func(s *Store, revoked bool) {
		t.Helper()
		got, found, err := s.ManagedRecordForProcess(ctx, native)
		if err != nil || !found || got.ID != p.Record.ID || got.Revoked != revoked {
			t.Fatal("managed provenance became absent", found, err)
		}
		got.Policy.Ceiling = "full"
		again, found, err := s.ManagedRecordForProcess(ctx, native)
		if err != nil || !found || again.Policy.Ceiling != "ask" {
			t.Fatal("metadata leaked mutable policy", err)
		}
	}
	verify(f.store, false)
	if err := f.store.Deactivate(ctx, p.Record.ID); err != nil {
		t.Fatal(err)
	}
	verify(f.store, false)
	if err := f.store.RevokeLaunch(ctx, p.Record.ID); err != nil {
		t.Fatal(err)
	}
	verify(f.store, true)
	f.store.Close()
	s, err := Open(f.config)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	verify(s, true)
	other := native
	other.KernelGeneration += "-new"
	if _, found, err := s.ManagedRecordForProcess(ctx, other); err != nil || found {
		t.Fatal("unowned generation became managed", err)
	}
	s.Close()
	if _, _, err := s.ManagedRecordForProcess(ctx, other); err == nil {
		t.Fatal("unreadable provenance became unowned")
	}
}

func TestManagedProvenanceRefusesAmbiguousOwners(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	first := preparePolicy(t, f)
	native := pin(t, f.store, first)
	if err := f.store.Deactivate(ctx, first.Record.ID); err != nil {
		t.Fatal(err)
	}
	second := preparePolicy(t, f)
	if got := pin(t, f.store, second); got != native {
		t.Fatal("synthetic process differs")
	}
	_, _, err := f.store.ManagedRecordForProcess(ctx, native)
	code(t, err, "process_ambiguous")
}

func TestPolicyVersionOneKeepsHistoryButCannotReactivate(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	p := preparePolicy(t, f)
	native := pin(t, f.store, p)
	b := binding(native, uuid.NewString(), 1)
	if err := f.store.RecordBinding(ctx, p.Record.ID, b, KeepConversation); err != nil {
		t.Fatal(err)
	}
	rewritePolicyIndex(t, f, func(index map[string]any) {
		policy := index["records"].(map[string]any)[p.Record.ID].(map[string]any)["policy"].(map[string]any)
		policy["version"] = 1
		delete(policy, "model")
		delete(policy, "effort")
	})
	s, err := Open(f.config)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.PolicyForLaunch(ctx, p.Record.ID)
	code(t, err, "policy_version_unsupported")
	if _, err := s.Restore(ctx, f.request.ProjectRoot, b.ConversationID); err != nil {
		t.Fatal("old policy lost history", err)
	}
	got, found, err := s.ManagedRecordForProcess(ctx, native)
	if err != nil || !found || got.Policy.Version != 1 {
		t.Fatal("old policy became unmanaged", err)
	}
}

func TestNativePolicyBindsModelAndEffort(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	p := preparePolicy(t, f)
	for _, kind := range []string{"model", "effort"} {
		req := f.request
		policy := syntheticNativePolicy()
		if kind == "model" {
			req.Model = "synthetic-model"
		} else {
			req.ReasoningEffort = "high"
		}
		_, err := f.store.PrepareLaunchWithPolicy(ctx, uuid.NewString(), req, policy)
		code(t, err, "policy_request_conflict")
		policy.Model, policy.Effort = req.Model, req.ReasoningEffort
		_, err = f.store.PrepareLaunchWithPolicy(ctx, p.Record.ID, req, policy)
		code(t, err, "launch_conflict")
	}
}
