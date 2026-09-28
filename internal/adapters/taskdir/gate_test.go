package taskdir

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
)

func TestGateEvidenceIsDurableAndIdempotent(t *testing.T) {
	r := Root{Dir: t.TempDir()}
	id := "a7000000-0000-4000-8000-000000000001"
	body := []byte("exact evidence bytes")
	meta := contract.WorkGateEvidenceSubmission{
		ArtifactID: "test-log", MediaType: "text/plain", ByteCount: int64(len(body)),
		Sha256: gateDigest(body),
	}
	at := time.Unix(42, 0)

	first, err := r.StoreGateEvidence(id, "evidence-key", meta, bytes.NewReader(body), at)
	if err != nil || first.Replayed || first.ArtifactID != meta.ArtifactID {
		t.Fatalf("first evidence: %+v, %v", first, err)
	}
	replayed, err := r.StoreGateEvidence(id, "evidence-key", meta, bytes.NewReader(body), at.Add(time.Hour))
	if err != nil || !replayed.Replayed || replayed.AcceptedAt != first.AcceptedAt {
		t.Fatalf("replayed evidence: %+v, %v", replayed, err)
	}
	if _, err := r.StoreGateEvidence(id, "evidence-key", meta, bytes.NewReader([]byte("changed evidence byt")), at); gateErrorCode(err) != "gate_evidence_conflict" {
		t.Fatalf("changed replay bytes with copied headers: %v", err)
	}
	changed := append([]byte{}, body...)
	changed[0] = 'E'
	meta.Sha256 = gateDigest(changed)
	if _, err := r.StoreGateEvidence(id, "evidence-key", meta, bytes.NewReader(changed), at); gateErrorCode(err) != "gate_evidence_conflict" {
		t.Fatalf("conflicting replay: %v", err)
	}
	stored, got, err := r.ReadGateEvidence(id, "test-log")
	if err != nil || string(got) != string(body) || stored.Sha256 != gateDigest(body) {
		t.Fatalf("durable evidence: %+v %q, %v", stored, got, err)
	}
	receiptPath := filepath.Join(r.gatePath(id), "evidence", "test-log", "receipt.json")
	receiptBody, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	badMedia := bytes.Replace(receiptBody, []byte(`"media_type":"text/plain"`), []byte(`"media_type":"not a media type"`), 1)
	if err := os.WriteFile(receiptPath, badMedia, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.ReadGateEvidence(id, "test-log"); gateErrorCode(err) != "gate_evidence_corrupt" {
		t.Fatalf("corrupt evidence media receipt: %v", err)
	}
	if err := os.WriteFile(receiptPath, receiptBody, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.gatePath(id), "evidence", "test-log", "content"), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.ReadGateEvidence(id, "test-log"); gateErrorCode(err) != "gate_evidence_corrupt" {
		t.Fatalf("corrupt evidence: %v", err)
	}
}

func TestGateResultIsDurableAndIdempotent(t *testing.T) {
	r := Root{Dir: t.TempDir()}
	id := "a7000000-0000-4000-8000-000000000002"
	body := []byte(`{"round_id":"r","task_id":"a7000000-0000-4000-8000-000000000002","candidate_commit":"c","candidate_tree":"t","criteria_version":1,"criteria_digest":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","verdict":"NEEDS_WORK","claims":[{"criterion":"x","state":"unverified","evidence":[],"evidence_artifacts":[],"reason":"could not run"}],"summary":"unverified"}`)
	at := time.Unix(84, 0)

	first, err := r.StoreGateResult(id, "result-key", body, contract.WorkGateVerdictNEEDSWORK, at)
	if err != nil || first.Replayed || first.Sha256 != gateDigest(body) {
		t.Fatalf("first result: %+v, %v", first, err)
	}
	replayed, err := r.StoreGateResult(id, "result-key", body, contract.WorkGateVerdictNEEDSWORK, at.Add(time.Hour))
	if err != nil || !replayed.Replayed || replayed.AcceptedAt != first.AcceptedAt {
		t.Fatalf("replayed result: %+v, %v", replayed, err)
	}
	if _, err := r.StoreGateResult(id, "another-key", body, contract.WorkGateVerdictNEEDSWORK, at); gateErrorCode(err) != "gate_result_conflict" {
		t.Fatalf("conflicting result key: %v", err)
	}
	stored, got, err := r.ReadGateResult(id)
	if err != nil || !bytes.Equal(got, body) || stored.Sha256 != first.Sha256 {
		t.Fatalf("durable result: %+v %q, %v", stored, got, err)
	}
	receiptPath := filepath.Join(r.gatePath(id), "result", "receipt.json")
	receiptBody, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	badTask := bytes.Replace(receiptBody, []byte(id), []byte("a7000000-0000-4000-8000-000000000009"), 1)
	if err := os.WriteFile(receiptPath, badTask, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.ReadGateResult(id); gateErrorCode(err) != "gate_result_corrupt" {
		t.Fatalf("corrupt result identity receipt: %v", err)
	}
	if err := os.WriteFile(receiptPath, receiptBody, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.gatePath(id), "result", "result.json"), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.ReadGateResult(id); gateErrorCode(err) != "gate_result_corrupt" {
		t.Fatalf("corrupt result: %v", err)
	}
}

func TestGateEvidenceRejectsDeclaredSizeAndHashDisagreements(t *testing.T) {
	r := Root{Dir: t.TempDir()}
	id := "a7000000-0000-4000-8000-000000000003"
	body := []byte("exact")
	meta := contract.WorkGateEvidenceSubmission{ArtifactID: "log", MediaType: "text/plain", ByteCount: int64(len(body)), Sha256: gateDigest(body)}
	meta.ByteCount--
	if _, err := r.StoreGateEvidence(id, "size", meta, bytes.NewReader(body), time.Now()); gateErrorCode(err) != "gate_evidence_size_mismatch" {
		t.Fatalf("size mismatch: %v", err)
	}
	meta.ByteCount = int64(len(body))
	meta.Sha256 = gateDigest([]byte("other"))
	if _, err := r.StoreGateEvidence(id, "hash", meta, bytes.NewReader(body), time.Now()); gateErrorCode(err) != "gate_evidence_hash_mismatch" {
		t.Fatalf("hash mismatch: %v", err)
	}
}

func TestExportedGateSubmissionsPurgeIdempotently(t *testing.T) {
	r := Root{Dir: filepath.Join(t.TempDir(), "tasks")}
	id := "a7999999-9999-4999-8999-999999999999"
	body := []byte("evidence")
	meta := contract.WorkGateEvidenceSubmission{ArtifactID: "focused-log", MediaType: "text/plain",
		ByteCount: int64(len(body)), Sha256: gateDigest(body)}
	if _, err := r.StoreGateEvidence(id, "key", meta, bytes.NewReader(body), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := r.PurgeGateSubmissions(id); err != nil {
		t.Fatal(err)
	}
	if err := r.PurgeGateSubmissions(id); err != nil {
		t.Fatalf("idempotent purge: %v", err)
	}
	if _, err := os.Stat(r.gatePath(id)); !os.IsNotExist(err) {
		t.Fatalf("gate submission still exists: %v", err)
	}
	if err := r.PurgeGateSubmissions("../outside"); err == nil {
		t.Fatal("unsafe gate purge id was accepted")
	}
}

func gateErrorCode(err error) string {
	var gateErr *GateStorageError
	if errors.As(err, &gateErr) {
		return gateErr.Code
	}
	return ""
}
