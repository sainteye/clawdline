package orchestrator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/contract"
)

var gateHexDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)
var gateGitObject = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

func admitGateOrigin(raw json.RawMessage, kind, assistant, isolation string, claims []string) (*GateOrigin, error) {
	bad := func(message string) (*GateOrigin, error) {
		return nil, refuse(http.StatusUnprocessableEntity, "bad_gate_origin", message)
	}
	if kind != TaskKindVerificationGate {
		if len(raw) != 0 && string(raw) != "null" {
			return bad("verification_gate metadata is valid only when kind is verification_gate.")
		}
		return nil, nil
	}
	if len(raw) == 0 || string(raw) == "null" {
		return bad("A verification_gate task needs its immutable verification_gate origin.")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var gate GateOrigin
	if err := dec.Decode(&gate); err != nil {
		return bad("verification_gate origin is not the closed checker origin: " + err.Error())
	}
	if err := expectGateEOF(dec); err != nil {
		return bad("verification_gate origin must contain one JSON object.")
	}
	if assistant != "codex" {
		return bad("A verification_gate task requires Codex's read-only sandbox capability.")
	}
	if isolation != IsolationWorktree {
		return bad("A verification_gate task requires an isolated detached worktree.")
	}
	if len(claims) != 0 {
		return bad("A verification_gate task is read-only and must declare claims: [].")
	}
	if gate.Origin != TaskKindVerificationGate || !IsTaskID(gate.RoundID) || gate.Attempt < 0 || gate.Attempt >= contract.WorkGateTasksPerRoundLimit {
		return bad("verification_gate must name origin verification_gate, a lowercase-UUID round_id, and attempt 0 or 1.")
	}
	if !gateGitObject.MatchString(gate.BaseCommit) || !gateGitObject.MatchString(gate.Candidate.Commit) ||
		!gateGitObject.MatchString(gate.Candidate.Tree) {
		return bad("verification_gate base, candidate commit, and candidate tree must be full lowercase Git object ids.")
	}
	if gate.Acceptance.Version < 1 || strings.TrimSpace(gate.Acceptance.Criteria) == "" ||
		!gateHexDigest.MatchString(gate.Acceptance.Digest) || gate.Acceptance.Digest != digestGate([]byte(gate.Acceptance.Criteria)) {
		return bad("verification_gate acceptance digest must be lowercase SHA-256 of the exact non-empty criteria bytes.")
	}
	c := gate.Candidate
	if !filepath.IsAbs(c.Repository) || !filepath.IsAbs(c.Worktree) || strings.TrimSpace(c.Branch) == "" ||
		!IsTaskID(c.AssignmentID) || !IsTaskID(c.OwnerSessionID) || c.Cycle < 1 || c.CriteriaVersion < 1 ||
		c.CriteriaVersion != gate.Acceptance.Version || c.CriteriaDigest != gate.Acceptance.Digest ||
		c.CreatedAt <= 0 || c.UntrackedFiles < 0 {
		return bad("verification_gate candidate metadata is incomplete or disagrees with the frozen acceptance.")
	}
	return &gate, nil
}

func expectGateEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); err == io.EOF {
		return nil
	} else {
		return err
	}
}

func digestGate(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func validGateKey(key string) bool {
	key = strings.TrimSpace(key)
	return key != "" && len(key) <= contract.WorkGateEvidenceStringBytesLimit && !strings.ContainsAny(key, "\r\n")
}

func validateEvidenceMeta(meta contract.WorkGateEvidenceSubmission) error {
	if !gateHexDigest.MatchString(meta.Sha256) || meta.ByteCount < 0 {
		return refuse(http.StatusUnprocessableEntity, "invalid_gate_evidence", "Evidence needs a lowercase SHA-256 digest and a non-negative byte_count.")
	}
	if strings.TrimSpace(meta.MediaType) == "" || len(meta.MediaType) > contract.WorkGateEvidenceStringBytesLimit {
		return refuse(http.StatusUnprocessableEntity, "invalid_gate_evidence_media", "Evidence needs a bounded media_type.")
	}
	if _, _, err := mime.ParseMediaType(meta.MediaType); err != nil {
		return refuse(http.StatusUnprocessableEntity, "invalid_gate_evidence_media", "Evidence media_type is not a valid media type.")
	}
	return nil
}

// SubmitGateEvidence authenticates the checker, enforces the registered
// count/byte bounds, and returns only after taskdir has fsynced and atomically
// published the exact stream and receipt.
func (b *Broker) SubmitGateEvidence(ctx context.Context, id, secret, key string, meta contract.WorkGateEvidenceSubmission, src io.Reader) (contract.WorkGateEvidenceReceipt, error) {
	if !validGateKey(key) {
		return contract.WorkGateEvidenceReceipt{}, refuse(http.StatusBadRequest, "idempotency_key_required",
			fmt.Sprintf("Gate evidence needs an Idempotency-Key header of at most %d bytes.", contract.WorkGateEvidenceStringBytesLimit))
	}
	if err := validateEvidenceMeta(meta); err != nil {
		return contract.WorkGateEvidenceReceipt{}, err
	}
	r, _, err := b.Authenticate(ctx, id, secret)
	if err != nil {
		return contract.WorkGateEvidenceReceipt{}, err
	}
	if r.Gate == nil || r.Kind != TaskKindVerificationGate {
		return contract.WorkGateEvidenceReceipt{}, refuse(http.StatusConflict, "not_verification_gate", "Only a verification_gate task may submit gate evidence.")
	}
	b.gateMu.Lock()
	defer b.gateMu.Unlock()
	ids, used, err := b.Tasks.GateEvidenceUsage(id)
	if err != nil {
		return contract.WorkGateEvidenceReceipt{}, gateStoreFailure(err)
	}
	existing := false
	for _, artifactID := range ids {
		if artifactID == meta.ArtifactID {
			existing = true
			break
		}
	}
	if !existing {
		if err := contract.ValidateWorkGateEvidenceAdmission(len(ids), used, meta); err != nil {
			return contract.WorkGateEvidenceReceipt{}, gateContractFailure(err)
		}
	}
	receipt, err := b.Tasks.StoreGateEvidence(id, key, meta, src, b.now())
	if err != nil {
		return contract.WorkGateEvidenceReceipt{}, gateStoreFailure(err)
	}
	if b.GateFault != nil {
		if err := b.GateFault("evidence_persisted"); err != nil {
			return contract.WorkGateEvidenceReceipt{}, refuse(http.StatusServiceUnavailable, "gate_response_lost", "Evidence is durable but its response was interrupted; retry with the same Idempotency-Key.")
		}
	}
	return receipt, nil
}

// SubmitGateResult validates the closed contract and frozen identity before
// publishing its exact bytes. Collection then independently re-reads those
// bytes, every artifact and the checkout tree before settling the task.
func (b *Broker) SubmitGateResult(ctx context.Context, id, secret, key string, body []byte) (contract.WorkGateResultReceipt, error) {
	if !validGateKey(key) {
		return contract.WorkGateResultReceipt{}, refuse(http.StatusBadRequest, "idempotency_key_required",
			fmt.Sprintf("A gate result needs an Idempotency-Key header of at most %d bytes.", contract.WorkGateEvidenceStringBytesLimit))
	}
	r, _, err := b.Authenticate(ctx, id, secret)
	if err != nil {
		return contract.WorkGateResultReceipt{}, err
	}
	if r.Gate == nil || r.Kind != TaskKindVerificationGate {
		return contract.WorkGateResultReceipt{}, refuse(http.StatusConflict, "not_verification_gate", "Only a verification_gate task may submit a gate result.")
	}
	result, err := contract.DecodeWorkGateResult(body)
	if err != nil {
		return contract.WorkGateResultReceipt{}, gateContractFailure(err)
	}
	if err := validateGateResultIdentity(r, result); err != nil {
		return contract.WorkGateResultReceipt{}, err
	}
	if err := b.validateGateArtifacts(r.ID, result); err != nil {
		return contract.WorkGateResultReceipt{}, err
	}
	b.gateMu.Lock()
	receipt, err := b.Tasks.StoreGateResult(id, key, body, result.Verdict, b.now())
	b.gateMu.Unlock()
	if err != nil {
		return contract.WorkGateResultReceipt{}, gateStoreFailure(err)
	}
	if b.GateFault != nil {
		if err := b.GateFault("result_persisted"); err != nil {
			return contract.WorkGateResultReceipt{}, refuse(http.StatusServiceUnavailable, "gate_response_lost", "The verdict is durable but its response was interrupted; retry with the same Idempotency-Key.")
		}
	}
	settled, err := b.collectGate(ctx, r)
	if errors.Is(err, errAlreadyTerminal) {
		return receipt, nil
	}
	if err != nil {
		return contract.WorkGateResultReceipt{}, err
	}
	if !settled {
		return contract.WorkGateResultReceipt{}, refuse(http.StatusConflict, "gate_result_not_settled", "The durable gate result was not settled; retry with the same Idempotency-Key.")
	}
	return receipt, nil
}

func validateGateResultIdentity(r Record, result contract.WorkGateResult) error {
	g := r.Gate
	if g == nil || result.TaskID != r.ID || result.RoundID != g.RoundID ||
		result.CandidateCommit != g.Candidate.Commit || result.CandidateTree != g.Candidate.Tree ||
		result.CriteriaVersion != g.Acceptance.Version || result.CriteriaDigest != g.Acceptance.Digest {
		return refuse(http.StatusConflict, "gate_identity_mismatch", "The result does not name this task's frozen round, candidate, tree, criteria version and digest.")
	}
	return nil
}

func (b *Broker) validateGateArtifacts(taskID string, result contract.WorkGateResult) error {
	seen := map[string]bool{}
	for _, claim := range result.Claims {
		for _, artifactID := range claim.EvidenceArtifacts {
			if seen[artifactID] {
				continue
			}
			seen[artifactID] = true
			if _, _, err := b.Tasks.ReadGateEvidence(taskID, artifactID); err != nil {
				var storage *taskdir.GateStorageError
				if errors.As(err, &storage) && storage.Code == "gate_evidence_not_found" {
					return refuse(http.StatusUnprocessableEntity, "gate_evidence_not_found", "The result names evidence that was not durably uploaded: "+artifactID+".")
				}
				return gateStoreFailure(err)
			}
		}
	}
	return nil
}

// collectGate is the only path that turns durable gate bytes into a task
// result. It deliberately never reads ordinary result.json.
func (b *Broker) collectGate(ctx context.Context, r Record) (bool, error) {
	receipt, body, err := b.Tasks.ReadGateResult(r.ID)
	if err != nil {
		return false, err
	}
	result, err := contract.DecodeWorkGateResult(body)
	if err != nil {
		return false, gateContractFailure(err)
	}
	if receipt.TaskID != r.ID || receipt.Verdict != result.Verdict || receipt.Sha256 != digestGate(body) {
		return false, refuse(http.StatusConflict, "gate_result_corrupt", "The durable gate result receipt does not match its exact bytes.")
	}
	if err := validateGateResultIdentity(r, result); err != nil {
		return false, err
	}
	if err := b.validateGateArtifacts(r.ID, result); err != nil {
		return false, err
	}
	if r.Worktree == nil || !r.Worktree.Detached || r.Gate == nil {
		return false, refuse(http.StatusConflict, "gate_checkout_invalid", "The verification gate has no detached candidate checkout.")
	}
	snapshot, err := b.Git.SnapshotCheckout(ctx, r.Worktree.Path)
	if err != nil {
		return false, refuse(http.StatusConflict, "gate_checkout_unreadable", "The checker checkout could not be independently read: "+err.Error())
	}
	if snapshot.Head != r.Gate.Candidate.Commit || snapshot.HeadTree != r.Gate.Candidate.Tree || snapshot.Tree != r.Gate.Candidate.Tree {
		return false, refuseWith(http.StatusConflict, "gate_candidate_changed",
			"The checker checkout no longer has the frozen candidate commit and clean tree; the durable verdict cannot settle.",
			map[string]any{"candidate_commit": r.Gate.Candidate.Commit, "candidate_tree": r.Gate.Candidate.Tree})
	}
	ordinary := taskdir.Result{
		Protocol: Protocol, TaskID: r.ID, Status: "success", Summary: result.Summary,
		GateVerdict: &result, FinishedAt: b.now().UTC().Format(time.RFC3339),
	}
	if _, err := b.Settle(ctx, r.ID, StateSuccess, "", &ordinary); err != nil {
		return false, err
	}
	return true, nil
}

func gateContractFailure(err error) error {
	var contractErr *contract.WorkGateContractError
	code := "invalid_gate_result"
	if errors.As(err, &contractErr) {
		code = contractErr.Code
	}
	return refuse(http.StatusUnprocessableEntity, code, err.Error())
}

func gateStoreFailure(err error) error {
	var storage *taskdir.GateStorageError
	if errors.As(err, &storage) {
		status := http.StatusConflict
		if strings.HasPrefix(storage.Code, "invalid_") || strings.Contains(storage.Code, "mismatch") || strings.HasSuffix(storage.Code, "too_large") {
			status = http.StatusUnprocessableEntity
		}
		return refuse(status, storage.Code, storage.Code)
	}
	return fmt.Errorf("gate durable store: %w", err)
}
