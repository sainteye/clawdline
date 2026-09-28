package taskdir

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
)

// GateStorageError is a durable-submission refusal callers branch on without
// parsing filesystem prose.
type GateStorageError struct{ Code string }

func (e *GateStorageError) Error() string { return e.Code }

func gateStorageError(code string) error { return &GateStorageError{Code: code} }

var gateArtifactID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func validGateArtifactID(id string) bool {
	return len(id) <= contract.WorkGateEvidenceStringBytesLimit && gateArtifactID.MatchString(id)
}

type storedGateEvidence struct {
	contract.WorkGateEvidenceReceipt
	IdempotencyKey string `json:"idempotency_key"`
}

type storedGateResult struct {
	contract.WorkGateResultReceipt
	IdempotencyKey string `json:"idempotency_key"`
}

func (r Root) gatePath(id string) string {
	return filepath.Join(filepath.Dir(r.Dir), "gate-submissions", id)
}

// PurgeGateSubmissions removes only one validated task's exported gate
// evidence/result directory. Missing is success, making durable cleanup rows
// safe to replay after a crash.
func (r Root) PurgeGateSubmissions(id string) error {
	if !isTaskID(id) {
		return gateStorageError("invalid_gate_task")
	}
	return os.RemoveAll(r.gatePath(id))
}

// StoreGateEvidence streams one artifact into a private staging directory,
// fsyncs its bytes and receipt, and atomically publishes the pair. The
// artifact id is its immutable name; an exact keyed replay returns the first
// receipt and any disagreement is a conflict.
func (r Root) StoreGateEvidence(id, key string, meta contract.WorkGateEvidenceSubmission, src io.Reader, at time.Time) (contract.WorkGateEvidenceReceipt, error) {
	if !validGateArtifactID(meta.ArtifactID) {
		return contract.WorkGateEvidenceReceipt{}, gateStorageError("invalid_gate_evidence_artifact")
	}
	if meta.ByteCount < 0 {
		return contract.WorkGateEvidenceReceipt{}, gateStorageError("invalid_gate_evidence_size")
	}
	if meta.ByteCount > contract.WorkGateEvidenceArtifactBytesLimit {
		return contract.WorkGateEvidenceReceipt{}, gateStorageError("gate_evidence_artifact_too_large")
	}
	parent := filepath.Join(r.gatePath(id), "evidence")
	final := filepath.Join(parent, meta.ArtifactID)
	finalExists := false
	if _, err := os.Stat(final); err == nil {
		finalExists = true
	} else if !os.IsNotExist(err) {
		return contract.WorkGateEvidenceReceipt{}, err
	}
	if err := r.ensureGateDir(parent); err != nil {
		return contract.WorkGateEvidenceReceipt{}, err
	}
	tmp, err := os.MkdirTemp(parent, ".writing-")
	if err != nil {
		return contract.WorkGateEvidenceReceipt{}, err
	}
	defer os.RemoveAll(tmp)

	dataPath := filepath.Join(tmp, "content")
	gotHash, gotBytes, err := writeGateStream(dataPath, src, meta.ByteCount)
	if err != nil {
		return contract.WorkGateEvidenceReceipt{}, err
	}
	if finalExists && (gotBytes != meta.ByteCount || gotHash != meta.Sha256) {
		return contract.WorkGateEvidenceReceipt{}, gateStorageError("gate_evidence_conflict")
	}
	if gotBytes != meta.ByteCount {
		return contract.WorkGateEvidenceReceipt{}, gateStorageError("gate_evidence_size_mismatch")
	}
	if gotHash != meta.Sha256 {
		return contract.WorkGateEvidenceReceipt{}, gateStorageError("gate_evidence_hash_mismatch")
	}
	// Replays still consume and validate this request's exact bytes. Trusting
	// only its claimed digest would let different bytes with copied headers be
	// mistaken for the original durable submission.
	if _, err := os.Stat(final); err == nil {
		return replayGateEvidence(final, id, key, meta)
	} else if !os.IsNotExist(err) {
		return contract.WorkGateEvidenceReceipt{}, err
	}
	stored := storedGateEvidence{
		WorkGateEvidenceReceipt: contract.WorkGateEvidenceReceipt{
			AcceptedAt: at.Unix(), ArtifactID: meta.ArtifactID, ByteCount: meta.ByteCount,
			MediaType: meta.MediaType, Sha256: meta.Sha256, TaskID: id,
		},
		IdempotencyKey: key,
	}
	if err := writeGateJSON(filepath.Join(tmp, "receipt.json"), stored); err != nil {
		return contract.WorkGateEvidenceReceipt{}, err
	}
	if err := syncDir(tmp); err != nil {
		return contract.WorkGateEvidenceReceipt{}, err
	}
	if err := os.Rename(tmp, final); err != nil {
		if _, statErr := os.Stat(final); statErr == nil {
			return replayGateEvidence(final, id, key, meta)
		}
		return contract.WorkGateEvidenceReceipt{}, err
	}
	if err := syncDir(parent); err != nil {
		return contract.WorkGateEvidenceReceipt{}, err
	}
	return stored.WorkGateEvidenceReceipt, nil
}

func replayGateEvidence(dir, id, key string, meta contract.WorkGateEvidenceSubmission) (contract.WorkGateEvidenceReceipt, error) {
	stored, body, err := readGateEvidenceDir(dir)
	if err != nil {
		return contract.WorkGateEvidenceReceipt{}, err
	}
	if validateStoredGateEvidence(id, meta.ArtifactID, stored, body) != nil || stored.IdempotencyKey != key ||
		stored.MediaType != meta.MediaType || stored.ByteCount != meta.ByteCount || stored.Sha256 != meta.Sha256 {
		return contract.WorkGateEvidenceReceipt{}, gateStorageError("gate_evidence_conflict")
	}
	out := stored.WorkGateEvidenceReceipt
	out.Replayed = true
	return out, nil
}

// ReadGateEvidence independently proves the stored receipt against the bytes
// it describes. Corruption is never returned as an empty artifact.
func (r Root) ReadGateEvidence(id, artifactID string) (contract.WorkGateEvidenceReceipt, []byte, error) {
	if !validGateArtifactID(artifactID) {
		return contract.WorkGateEvidenceReceipt{}, nil, gateStorageError("invalid_gate_evidence_artifact")
	}
	stored, body, err := readGateEvidenceDir(filepath.Join(r.gatePath(id), "evidence", artifactID))
	if err != nil {
		if os.IsNotExist(err) {
			return contract.WorkGateEvidenceReceipt{}, nil, gateStorageError("gate_evidence_not_found")
		}
		if errors.Is(err, ErrTooLarge) {
			return contract.WorkGateEvidenceReceipt{}, nil, gateStorageError("gate_evidence_corrupt")
		}
		return contract.WorkGateEvidenceReceipt{}, nil, err
	}
	if err := validateStoredGateEvidence(id, artifactID, stored, body); err != nil {
		return contract.WorkGateEvidenceReceipt{}, nil, gateStorageError("gate_evidence_corrupt")
	}
	return stored.WorkGateEvidenceReceipt, body, nil
}

func validateStoredGateEvidence(id, artifactID string, stored storedGateEvidence, body []byte) error {
	if stored.TaskID != id || stored.ArtifactID != artifactID || stored.AcceptedAt <= 0 ||
		stored.ByteCount != int64(len(body)) || stored.ByteCount > contract.WorkGateEvidenceArtifactBytesLimit ||
		stored.Sha256 != gateDigest(body) || strings.TrimSpace(stored.IdempotencyKey) == "" ||
		len(stored.IdempotencyKey) > contract.WorkGateEvidenceStringBytesLimit ||
		strings.ContainsAny(stored.IdempotencyKey, "\r\n") || strings.TrimSpace(stored.MediaType) == "" ||
		len(stored.MediaType) > contract.WorkGateEvidenceStringBytesLimit {
		return gateStorageError("gate_evidence_corrupt")
	}
	if _, _, err := mime.ParseMediaType(stored.MediaType); err != nil {
		return gateStorageError("gate_evidence_corrupt")
	}
	return nil
}

func readGateEvidenceDir(dir string) (storedGateEvidence, []byte, error) {
	var stored storedGateEvidence
	if err := readGateJSON(filepath.Join(dir, "receipt.json"), &stored); err != nil {
		return storedGateEvidence{}, nil, err
	}
	body, err := readBounded(filepath.Join(dir, "content"), contract.WorkGateEvidenceArtifactBytesLimit)
	return stored, body, err
}

// GateEvidenceUsage returns every durable artifact id, its count and total
// bytes. Every receipt is checked against its content on the way through.
func (r Root) GateEvidenceUsage(id string) ([]string, int64, error) {
	dir := filepath.Join(r.gatePath(id), "evidence")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []string{}, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	ids := make([]string, 0, len(entries))
	var total int64
	for _, entry := range entries {
		if !entry.IsDir() || !validGateArtifactID(entry.Name()) {
			continue
		}
		receipt, _, err := r.ReadGateEvidence(id, entry.Name())
		if err != nil {
			return nil, 0, err
		}
		ids = append(ids, entry.Name())
		total += receipt.ByteCount
	}
	sort.Strings(ids)
	return ids, total, nil
}

// StoreGateResult publishes the exact JSON bytes and immutable receipt as one
// atomic directory. Only the original key and bytes are a replay.
func (r Root) StoreGateResult(id, key string, body []byte, verdict contract.WorkGateVerdict, at time.Time) (contract.WorkGateResultReceipt, error) {
	if len(body) > contract.WorkGateResultBytesLimit {
		return contract.WorkGateResultReceipt{}, gateStorageError("gate_result_too_large")
	}
	parent := r.gatePath(id)
	final := filepath.Join(parent, "result")
	if _, err := os.Stat(final); err == nil {
		return replayGateResult(final, id, key, body, verdict)
	} else if !os.IsNotExist(err) {
		return contract.WorkGateResultReceipt{}, err
	}
	if err := r.ensureGateDir(parent); err != nil {
		return contract.WorkGateResultReceipt{}, err
	}
	tmp, err := os.MkdirTemp(parent, ".result-writing-")
	if err != nil {
		return contract.WorkGateResultReceipt{}, err
	}
	defer os.RemoveAll(tmp)
	if err := writeGateBytes(filepath.Join(tmp, "result.json"), body); err != nil {
		return contract.WorkGateResultReceipt{}, err
	}
	stored := storedGateResult{
		WorkGateResultReceipt: contract.WorkGateResultReceipt{
			AcceptedAt: at.Unix(), ByteCount: int64(len(body)), Sha256: gateDigest(body),
			TaskID: id, Verdict: verdict,
		},
		IdempotencyKey: key,
	}
	if err := writeGateJSON(filepath.Join(tmp, "receipt.json"), stored); err != nil {
		return contract.WorkGateResultReceipt{}, err
	}
	if err := syncDir(tmp); err != nil {
		return contract.WorkGateResultReceipt{}, err
	}
	if err := os.Rename(tmp, final); err != nil {
		if _, statErr := os.Stat(final); statErr == nil {
			return replayGateResult(final, id, key, body, verdict)
		}
		return contract.WorkGateResultReceipt{}, err
	}
	if err := syncDir(parent); err != nil {
		return contract.WorkGateResultReceipt{}, err
	}
	return stored.WorkGateResultReceipt, nil
}

func replayGateResult(dir, id, key string, body []byte, verdict contract.WorkGateVerdict) (contract.WorkGateResultReceipt, error) {
	stored, durable, err := readGateResultDir(dir)
	if err != nil {
		return contract.WorkGateResultReceipt{}, err
	}
	digest := gateDigest(body)
	if validateStoredGateResult(id, stored, durable) != nil || stored.IdempotencyKey != key ||
		stored.Sha256 != digest || stored.Verdict != verdict || !bytes.Equal(durable, body) {
		return contract.WorkGateResultReceipt{}, gateStorageError("gate_result_conflict")
	}
	out := stored.WorkGateResultReceipt
	out.Replayed = true
	return out, nil
}

// ReadGateResult re-hashes the exact durable JSON before returning it to the
// collector.
func (r Root) ReadGateResult(id string) (contract.WorkGateResultReceipt, []byte, error) {
	stored, body, err := readGateResultDir(filepath.Join(r.gatePath(id), "result"))
	if err != nil {
		if os.IsNotExist(err) {
			return contract.WorkGateResultReceipt{}, nil, ErrNoResult
		}
		if errors.Is(err, ErrTooLarge) {
			return contract.WorkGateResultReceipt{}, nil, gateStorageError("gate_result_corrupt")
		}
		return contract.WorkGateResultReceipt{}, nil, err
	}
	if err := validateStoredGateResult(id, stored, body); err != nil {
		return contract.WorkGateResultReceipt{}, nil, gateStorageError("gate_result_corrupt")
	}
	return stored.WorkGateResultReceipt, body, nil
}

func validateStoredGateResult(id string, stored storedGateResult, body []byte) error {
	if stored.TaskID != id || stored.AcceptedAt <= 0 || stored.ByteCount != int64(len(body)) ||
		stored.ByteCount > contract.WorkGateResultBytesLimit || stored.Sha256 != gateDigest(body) ||
		strings.TrimSpace(stored.IdempotencyKey) == "" ||
		len(stored.IdempotencyKey) > contract.WorkGateEvidenceStringBytesLimit ||
		strings.ContainsAny(stored.IdempotencyKey, "\r\n") {
		return gateStorageError("gate_result_corrupt")
	}
	return nil
}

func readGateResultDir(dir string) (storedGateResult, []byte, error) {
	var stored storedGateResult
	if err := readGateJSON(filepath.Join(dir, "receipt.json"), &stored); err != nil {
		return storedGateResult{}, nil, err
	}
	body, err := readBounded(filepath.Join(dir, "result.json"), contract.WorkGateResultBytesLimit)
	return stored, body, err
}

// ensureGateDir makes every directory in the private submission path durable
// before a receipt is published inside it. Syncing only the leaf would not
// make a newly-created gate-submissions entry survive a power loss.
func (r Root) ensureGateDir(path string) error {
	state := filepath.Dir(r.Dir)
	rel, err := filepath.Rel(state, path)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator) {
		return gateStorageError("invalid_gate_storage_path")
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	current := state
	if err := syncDir(current); err != nil {
		return err
	}
	for _, part := range splitGatePath(rel) {
		current = filepath.Join(current, part)
		if err := os.Chmod(current, 0o700); err != nil {
			return err
		}
		if err := syncDir(current); err != nil {
			return err
		}
	}
	return nil
}

func splitGatePath(rel string) []string {
	var parts []string
	for rel != "." && rel != "" {
		dir, base := filepath.Split(rel)
		parts = append([]string{base}, parts...)
		rel = filepath.Clean(dir)
	}
	return parts
}

func writeGateStream(path string, src io.Reader, declared int64) (string, int64, error) {
	if declared < 0 {
		return "", 0, gateStorageError("invalid_gate_evidence_size")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", 0, err
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(src, declared+1))
	syncErr := f.Sync()
	closeErr := f.Close()
	if copyErr != nil {
		return "", n, copyErr
	}
	if syncErr != nil {
		return "", n, syncErr
	}
	if closeErr != nil {
		return "", n, closeErr
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func writeGateBytes(path string, body []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	n, err := f.Write(body)
	if err != nil {
		_ = f.Close()
		return err
	}
	if n != len(body) {
		_ = f.Close()
		return io.ErrShortWrite
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func writeGateJSON(path string, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writeGateBytes(path, append(body, '\n'))
}

func readGateJSON(path string, value any) error {
	body, err := readBounded(path, noteLimit)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(value); err != nil {
		return fmt.Errorf("unreadable gate receipt: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return fmt.Errorf("unreadable gate receipt: %w", err)
	}
	return nil
}

func gateDigest(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
