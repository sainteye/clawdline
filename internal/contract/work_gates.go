package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// These are the contract's hard bounds. The capacity register names every
// one, and later storage/transport implementations must take their defaults
// from the matching registered row rather than restating a number.
const (
	WorkGateRoundDetailsPerItemLimit       = 64
	WorkGateRoundDetailsPerStoreLimit      = 10_000
	WorkGateTasksPerRoundLimit             = 2
	WorkGateClaimsPerRoundLimit            = 32
	WorkGateEvidenceStringsPerClaimLimit   = 8
	WorkGateEvidenceStringBytesLimit       = 500
	WorkGateResultBytesLimit               = 64 << 10
	WorkGateEvidenceArtifactsPerTaskLimit  = 8
	WorkGateEvidenceArtifactBytesLimit     = 2 << 20
	WorkGateEvidenceTotalBytesPerTaskLimit = 8 << 20
	WorkGateRecentRoundsPerItemReadLimit   = 10
	WorkGateDueRowsPerPassLimit            = 20
	WorkGateRetryBackoffSecondsLimit       = 300
	WorkGateOwnerOfflineGraceSecondsLimit  = 900
)

// WorkGateContractError is stable enough for a transport to turn into a typed
// refusal or technical failure without parsing prose.
type WorkGateContractError struct {
	Code string
}

func (e *WorkGateContractError) Error() string { return e.Code }

func workGateError(code string) error { return &WorkGateContractError{Code: code} }

// DecodeWorkGateResult validates the closed JSON shape and its semantic
// verdict rules. It performs no task, candidate or criteria lookup; those are
// the future broker collector's authority boundary.
func DecodeWorkGateResult(raw []byte) (WorkGateResult, error) {
	if len(raw) > WorkGateResultBytesLimit {
		return WorkGateResult{}, workGateError("gate_result_too_large")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var result WorkGateResult
	if err := dec.Decode(&result); err != nil {
		return WorkGateResult{}, fmt.Errorf("invalid_gate_result: %w", err)
	}
	if err := expectJSONEOF(dec); err != nil {
		return WorkGateResult{}, err
	}
	if err := ValidateWorkGateResult(result); err != nil {
		return WorkGateResult{}, err
	}
	return result, nil
}

func expectJSONEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("invalid_gate_result: %w", err)
	}
	return workGateError("invalid_gate_result")
}

// ValidateWorkGateResult applies the result-shape bounds and prevents a
// verdict from contradicting its per-claim states.
func ValidateWorkGateResult(result WorkGateResult) error {
	if !knownWorkGateVerdict(result.Verdict) {
		return workGateError("invalid_gate_verdict")
	}
	if len(result.Claims) == 0 {
		return workGateError("gate_claims_required")
	}
	if len(result.Claims) > WorkGateClaimsPerRoundLimit {
		return workGateError("gate_claims_full")
	}
	failed, unverified := 0, 0
	artifacts := map[string]struct{}{}
	for _, claim := range result.Claims {
		if strings.TrimSpace(claim.Criterion) == "" || !knownWorkGateClaimState(claim.State) {
			return workGateError("invalid_gate_claim")
		}
		if len(claim.Evidence) > WorkGateEvidenceStringsPerClaimLimit {
			return workGateError("gate_evidence_strings_full")
		}
		for _, evidence := range claim.Evidence {
			if !utf8.ValidString(evidence) {
				return workGateError("gate_evidence_string_invalid_utf8")
			}
			if len(evidence) > WorkGateEvidenceStringBytesLimit {
				return workGateError("gate_evidence_string_too_large")
			}
		}
		for _, artifact := range claim.EvidenceArtifacts {
			if strings.TrimSpace(artifact) == "" {
				return workGateError("invalid_gate_evidence_artifact")
			}
			artifacts[artifact] = struct{}{}
		}
		if len(claim.EvidenceArtifacts) > WorkGateEvidenceArtifactsPerTaskLimit {
			return workGateError("gate_evidence_artifacts_full")
		}
		if len(artifacts) > WorkGateEvidenceArtifactsPerTaskLimit {
			return workGateError("gate_evidence_artifacts_full")
		}
		hasEvidence := len(claim.Evidence) > 0 || len(claim.EvidenceArtifacts) > 0
		switch claim.State {
		case WorkGateClaimStatePassed:
			if !hasEvidence {
				return workGateError("gate_passed_claim_without_evidence")
			}
		case WorkGateClaimStateFailed:
			failed++
			if !hasEvidence || strings.TrimSpace(claim.Reason) == "" {
				return workGateError("gate_failed_claim_incomplete")
			}
		case WorkGateClaimStateUnverified:
			unverified++
			if strings.TrimSpace(claim.Reason) == "" {
				return workGateError("gate_unverified_claim_without_reason")
			}
		}
	}
	switch result.Verdict {
	case WorkGateVerdictPASS:
		if failed != 0 || unverified != 0 {
			return workGateError("gate_verdict_contradiction")
		}
	case WorkGateVerdictFAIL:
		if failed == 0 {
			return workGateError("gate_verdict_contradiction")
		}
	case WorkGateVerdictNEEDSWORK:
		if unverified == 0 {
			return workGateError("gate_verdict_contradiction")
		}
	}
	return nil
}

func knownWorkGateVerdict(verdict WorkGateVerdict) bool {
	for _, known := range WorkGateVerdictValues {
		if verdict == known {
			return true
		}
	}
	return false
}

func knownWorkGateClaimState(state WorkGateClaimState) bool {
	for _, known := range WorkGateClaimStateValues {
		if state == known {
			return true
		}
	}
	return false
}

// ValidateWorkGateEvidenceAdmission applies the artifact count and byte
// ceilings before a future streaming route persists any bytes.
func ValidateWorkGateEvidenceAdmission(existingArtifacts int, existingBytes int64, incoming WorkGateEvidenceSubmission) error {
	if existingArtifacts < 0 || existingBytes < 0 || incoming.ByteCount < 0 {
		return workGateError("invalid_gate_evidence_size")
	}
	if existingArtifacts >= WorkGateEvidenceArtifactsPerTaskLimit {
		return workGateError("gate_evidence_artifacts_full")
	}
	if incoming.ByteCount > WorkGateEvidenceArtifactBytesLimit {
		return workGateError("gate_evidence_artifact_too_large")
	}
	if existingBytes+incoming.ByteCount > WorkGateEvidenceTotalBytesPerTaskLimit {
		return workGateError("gate_evidence_total_full")
	}
	return nil
}
