package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/contract"
)

func gateFixture(t *testing.T, repo string) (*GateOrigin, string, string) {
	t.Helper()
	commit := gitIn(t, repo, "rev-parse", "HEAD")
	tree := gitIn(t, repo, "rev-parse", "HEAD^{tree}")
	criteria := "The exact candidate passes its focused checks."
	digest := digestGate([]byte(criteria))
	return &GateOrigin{
		Origin: TaskKindVerificationGate, RoundID: "a7111111-1111-4111-8111-111111111111", Attempt: 0,
		BaseCommit: commit,
		Acceptance: contract.WorkGateAcceptance{Criteria: criteria, Version: 1, Digest: digest},
		Candidate: contract.WorkGateCandidateReceipt{
			Repository: repo, Worktree: repo, Branch: "feature/candidate", Commit: commit, Tree: tree,
			AssignmentID: "a7222222-2222-4222-8222-222222222222", OwnerSessionID: rootConversation,
			Cycle: 1, CriteriaVersion: 1, CriteriaDigest: digest, CreatedAt: time.Now().Unix(),
		},
	}, commit, tree
}

func gateBriefMap(g *GateOrigin) map[string]any {
	return map[string]any{
		"kind": TaskKindVerificationGate, "assistant": "codex", "permission_mode": "full",
		"isolation": IsolationWorktree, "claims": []string{}, "verification_gate": g,
	}
}

func TestGateAdmissionPinsTheCandidateAndReadOnlyLaunch(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "a7333333-3333-4333-8333-333333333333"
	g, commit, _ := gateFixture(t, repo)
	writeBrief(t, b, id, repo, gateBriefMap(g))
	r, err := b.ReadDraft(id)
	if err != nil || r.Gate == nil || r.Gate.Candidate.Commit != commit {
		t.Fatalf("gate draft: %+v, %v", r.Gate, err)
	}
	commitFile(t, repo, "moved.go", "package a\n")
	w, _, err := b.planGateWorktree(ctx, r)
	if err != nil || w.Base != commit || !w.Detached || w.Branch != "" {
		t.Fatalf("planned checker after HEAD moved: %+v, %v", w, err)
	}
	r.Dir = b.Tasks.Path(id)
	launch, _ := projects.Admit(projects.LaunchRequest{ProjectRoot: w.Path, Assistant: "codex"})
	line := shellCommand(launch, r, b.Tasks.Dir, w.Path)
	for _, want := range []string{
		"--strict-config", `default_permissions="clawdline-gate"`,
		`permissions.clawdline-gate.filesystem=`, `":root"="read"`,
		`features.network_proxy=true`, `permissions.clawdline-gate.network.enabled=true`,
		`permissions.clawdline-gate.network.domains=`, `"127.0.0.1"="allow"`,
		"work/build", "work/cache", "work/tmp", "--ask-for-approval never",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("read-only launch is missing %q: %s", want, line)
		}
	}
	for _, forbidden := range []string{"--sandbox", "workspace-write", "--add-dir"} {
		if strings.Contains(line, forbidden) {
			t.Errorf("read-only launch contains %q: %s", forbidden, line)
		}
	}

	badID := "a7444444-4444-4444-8444-444444444444"
	bad := gateBriefMap(g)
	bad["assistant"] = "claude"
	writeBrief(t, b, badID, repo, bad)
	if _, err := b.ReadDraft(badID); refusalCode(err) != "bad_gate_origin" {
		t.Fatalf("non-Codex gate: %v", err)
	}
}

func storedGateTask(t *testing.T, b *Broker, repo, id string) (Record, string) {
	t.Helper()
	g, commit, _ := gateFixture(t, repo)
	path := filepath.Join(t.TempDir(), "checker")
	if err := b.Git.AddDetachedWorktree(ctxForTest(), repo, path, commit); err != nil {
		t.Fatal(err)
	}
	r := Record{
		Protocol: Protocol, ID: id, Kind: TaskKindVerificationGate, Assistant: "codex", PermissionMode: "ask",
		Claims: []string{}, Isolation: IsolationWorktree, LeaseScope: LeaseWorktree,
		ProjectDir: repo, Repository: repo, Title: "check", Instructions: "verify", TimeoutMinutes: 30,
		CreatedAt: time.Now(), State: StateBriefed, Gate: g,
		Worktree: &Worktree{Repository: repo, Path: path, Base: commit, Head: commit, Detached: true},
	}
	if err := b.save(ctxForTest(), r, HashSecret(w1Secret), "task.briefed"); err != nil {
		t.Fatal(err)
	}
	return r, commit
}

func ctxForTest() context.Context { return context.Background() }

func gateResultBody(t *testing.T, r Record, artifact string) []byte {
	t.Helper()
	claim := contract.WorkGateClaim{Criterion: r.Gate.Acceptance.Criteria, State: contract.WorkGateClaimStatePassed,
		Evidence: []string{"focused check passed"}, EvidenceArtifacts: []string{}, Reason: ""}
	if artifact != "" {
		claim.EvidenceArtifacts = []string{artifact}
	}
	result := contract.WorkGateResult{
		RoundID: r.Gate.RoundID, TaskID: r.ID, CandidateCommit: r.Gate.Candidate.Commit,
		CandidateTree: r.Gate.Candidate.Tree, CriteriaVersion: r.Gate.Acceptance.Version,
		CriteriaDigest: r.Gate.Acceptance.Digest, Verdict: contract.WorkGateVerdictPASS,
		Claims: []contract.WorkGateClaim{claim}, Summary: "candidate verified",
	}
	body, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestGateResultRecoversAfterPersistenceAndIgnoresOrdinaryResult(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "a7555555-5555-4555-8555-555555555555"
	r, _ := storedGateTask(t, b, repo, id)
	r.Dir = b.Tasks.Path(id)
	evidence := []byte("test output")
	meta := contract.WorkGateEvidenceSubmission{ArtifactID: "focused-log", MediaType: "text/plain",
		ByteCount: int64(len(evidence)), Sha256: digestGate(evidence)}
	b.GateFault = func(point string) error {
		if point == "evidence_persisted" {
			return errors.New("injected evidence response loss")
		}
		return nil
	}
	if _, err := b.SubmitGateEvidence(ctx, id, w1Secret, "evidence-key", meta, bytes.NewReader(evidence)); refusalCode(err) != "gate_response_lost" {
		t.Fatalf("injected evidence boundary: %v", err)
	}
	b.GateFault = nil
	if receipt, err := b.SubmitGateEvidence(ctx, id, w1Secret, "evidence-key", meta, bytes.NewReader(evidence)); err != nil || !receipt.Replayed {
		t.Fatalf("evidence replay after response loss: %+v, %v", receipt, err)
	}

	// A direct malformed ordinary result has no gate authority and is ignored
	// by the collector rather than being interpreted as a gate verdict.
	if err := os.MkdirAll(b.Tasks.Path(id), 0o700); err != nil {
		t.Fatal(err)
	}
	direct := `{malformed ordinary result`
	if err := os.WriteFile(filepath.Join(b.Tasks.Path(id), "result.json"), []byte(direct), 0o600); err != nil {
		t.Fatal(err)
	}
	if settled, err := b.collect(ctx, r); settled || !errors.Is(err, taskdir.ErrNoResult) {
		t.Fatalf("ordinary result settled gate: settled=%v err=%v", settled, err)
	}

	body := gateResultBody(t, r, "focused-log")
	b.GateFault = func(point string) error {
		if point == "result_persisted" {
			return errors.New("injected response loss")
		}
		return nil
	}
	if _, err := b.SubmitGateResult(ctx, id, w1Secret, "result-key", body); refusalCode(err) != "gate_response_lost" {
		t.Fatalf("injected persistence boundary: %v", err)
	}
	before, _, _ := b.Record(ctx, id)
	if before.State != StateBriefed {
		t.Fatalf("settled despite injected boundary: %s", before.State)
	}
	restarted := &Broker{Store: b.Store, Tasks: b.Tasks, Git: b.Git, Dir: b.Dir, Live: b.Live}
	receipt, err := restarted.SubmitGateResult(ctx, id, w1Secret, "result-key", body)
	if err != nil || !receipt.Replayed {
		t.Fatalf("replay after response loss: %+v, %v", receipt, err)
	}
	after, _, _ := b.Record(ctx, id)
	if after.State != StateSuccess || after.Result == nil || after.Result.GateVerdict == nil || after.Landing != nil {
		t.Fatalf("settled gate record: %+v", after)
	}
}

func TestGateCheckoutAndTaskRootAreReadOnlyExceptScratch(t *testing.T) {
	b, _ := newTestBroker(t)
	repo := gitRepo(t)
	id := "a7888888-8888-4888-8888-888888888888"
	r, _ := storedGateTask(t, b, repo, id)
	r.Dir = b.Tasks.Path(id)
	t.Cleanup(func() {
		_ = makeTreeOwnerWritable(r.Worktree.Path)
		_ = makeTreeOwnerWritable(b.Dir)
	})
	if _, err := b.Tasks.Write(r.Brief(), "checker brief"); err != nil {
		t.Fatal(err)
	}
	if err := b.prepareGateReadonly(r); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(r.Worktree.Path, "a.go"), filepath.Join(r.Dir, "CHILD.md"), r.Worktree.Path, r.Dir} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o222 != 0 {
			t.Errorf("%s remained writable: %o", path, info.Mode().Perm())
		}
	}
	scratch := b.gateScratch(id)
	for _, path := range []string{scratch.Build, scratch.Cache, scratch.Temp} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Errorf("scratch %s mode = %o, want 700", path, info.Mode().Perm())
		}
	}
}

func TestGateChangedTreeAndWrongIdentityNeverSettle(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "a7666666-6666-4666-8666-666666666666"
	r, _ := storedGateTask(t, b, repo, id)
	body := gateResultBody(t, r, "")
	wrong := bytes.Replace(body, []byte(r.Gate.Acceptance.Digest), []byte(strings.Repeat("e", 64)), 1)
	if _, err := b.SubmitGateResult(ctx, id, w1Secret, "wrong", wrong); refusalCode(err) != "gate_identity_mismatch" {
		t.Fatalf("wrong digest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.Worktree.Path, "untracked.txt"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.SubmitGateResult(ctx, id, w1Secret, "changed", body); refusalCode(err) != "gate_candidate_changed" {
		t.Fatalf("changed tree: %v", err)
	}
	after, _, _ := b.Record(ctx, id)
	if after.State != StateBriefed {
		t.Fatalf("changed tree settled as %s", after.State)
	}
}

func TestGateEvidenceAdmissionRefusesOversizeAndCountAbuse(t *testing.T) {
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	id := "a7655555-5555-4655-8655-555555555555"
	_, _ = storedGateTask(t, b, repo, id)

	tooLarge := contract.WorkGateEvidenceSubmission{
		ArtifactID: "oversize", MediaType: "application/octet-stream",
		ByteCount: contract.WorkGateEvidenceArtifactBytesLimit + 1, Sha256: digestGate(nil),
	}
	if _, err := b.SubmitGateEvidence(ctx, id, w1Secret, "oversize-key", tooLarge, bytes.NewReader(nil)); refusalCode(err) != "gate_evidence_artifact_too_large" {
		t.Fatalf("oversize evidence: %v", err)
	}

	empty := contract.WorkGateEvidenceSubmission{MediaType: "text/plain", ByteCount: 0, Sha256: digestGate(nil)}
	for i := range contract.WorkGateEvidenceArtifactsPerTaskLimit {
		empty.ArtifactID = fmt.Sprintf("artifact-%d", i)
		if _, err := b.SubmitGateEvidence(ctx, id, w1Secret, fmt.Sprintf("key-%d", i), empty, bytes.NewReader(nil)); err != nil {
			t.Fatalf("artifact %d: %v", i, err)
		}
	}
	empty.ArtifactID = "one-too-many"
	if _, err := b.SubmitGateEvidence(ctx, id, w1Secret, "overflow-key", empty, bytes.NewReader(nil)); refusalCode(err) != "gate_evidence_artifacts_full" {
		t.Fatalf("artifact count overflow: %v", err)
	}
}

func TestGateRespawnHasOnePersistentDescendantAndNeverMovesCandidate(t *testing.T) {
	b, ctx := newTestBroker(t)
	t.Cleanup(func() { _ = makeTreeOwnerWritable(b.Dir) })
	repo := gitRepo(t)
	original := "a7777777-7777-4777-8777-777777777777"
	g, commit, _ := gateFixture(t, repo)
	writeBrief(t, b, original, repo, gateBriefMap(g))
	r, err := b.ReadDraft(original)
	if err != nil {
		t.Fatal(err)
	}
	r.State, r.CreatedAt, r.Dir, r.Repository = StateSpawnFailed, time.Now(), b.Tasks.Path(original), repo
	if err := b.save(ctx, r, HashSecret(w1Secret), "task.spawn_failed"); err != nil {
		t.Fatal(err)
	}
	commitFile(t, repo, "head-moved.go", "package a\n")

	var wg sync.WaitGroup
	results := make(chan Respawned, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := b.Respawn(ctx, original, "")
			if err != nil {
				errs <- err
				return
			}
			results <- out
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	if len(results) != 1 || len(errs) != 1 {
		t.Fatalf("concurrent gate respawns: successes=%d errors=%d", len(results), len(errs))
	}
	out := <-results
	if out.Record.Worktree == nil || out.Record.Worktree.Base != commit || !out.Record.Worktree.Detached ||
		out.Record.Gate == nil || out.Record.Gate.Candidate.Commit != commit || out.Record.Gate.FamilyID != original ||
		out.Record.Gate.Attempt != 1 {
		t.Fatalf("respawn moved candidate: %+v", out.Record)
	}
	if err := <-errs; refusalCode(err) != "gate_respawn_exhausted" {
		t.Fatalf("second concurrent respawn: %v", err)
	}
}
