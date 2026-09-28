package contract

import (
	"errors"
	"strings"
	"testing"
)

func TestWorkGateResultRejectsMalformedEnumsAndContradictions(t *testing.T) {
	valid := WorkGateResult{Verdict: WorkGateVerdictPASS, Claims: []WorkGateClaim{{
		Criterion: "The service answers.", State: WorkGateClaimStatePassed, Evidence: []string{"GET /health returned 200"},
	}}}
	for name, tc := range map[string]struct {
		mutate func(*WorkGateResult)
		code   string
	}{
		"verdict":     {func(r *WorkGateResult) { r.Verdict = "MAYBE" }, "invalid_gate_verdict"},
		"claim state": {func(r *WorkGateResult) { r.Claims[0].State = "unknown" }, "invalid_gate_claim"},
		"PASS with failed claim": {func(r *WorkGateResult) {
			r.Claims[0].State, r.Claims[0].Reason = WorkGateClaimStateFailed, "returned 500"
		}, "gate_verdict_contradiction"},
	} {
		t.Run(name, func(t *testing.T) {
			got := valid
			got.Claims = append([]WorkGateClaim(nil), valid.Claims...)
			tc.mutate(&got)
			assertGateCode(t, ValidateWorkGateResult(got), tc.code)
		})
	}
}

func TestWorkGateResultRejectsOverLimitValues(t *testing.T) {
	claim := WorkGateClaim{Criterion: "criterion", State: WorkGateClaimStatePassed, Evidence: []string{"evidence"}}
	result := WorkGateResult{Verdict: WorkGateVerdictPASS, Claims: []WorkGateClaim{claim}}

	tooManyClaims := result
	tooManyClaims.Claims = make([]WorkGateClaim, WorkGateClaimsPerRoundLimit+1)
	assertGateCode(t, ValidateWorkGateResult(tooManyClaims), "gate_claims_full")

	tooManyStrings := result
	tooManyStrings.Claims = []WorkGateClaim{claim}
	tooManyStrings.Claims[0].Evidence = make([]string, WorkGateEvidenceStringsPerClaimLimit+1)
	assertGateCode(t, ValidateWorkGateResult(tooManyStrings), "gate_evidence_strings_full")

	tooLong := result
	tooLong.Claims = []WorkGateClaim{claim}
	tooLong.Claims[0].Evidence = []string{strings.Repeat("é", WorkGateEvidenceStringBytesLimit/2+1)}
	assertGateCode(t, ValidateWorkGateResult(tooLong), "gate_evidence_string_too_large")

	tooManyArtifacts := result
	tooManyArtifacts.Claims = []WorkGateClaim{claim}
	tooManyArtifacts.Claims[0].EvidenceArtifacts = make([]string, WorkGateEvidenceArtifactsPerTaskLimit+1)
	for i := range tooManyArtifacts.Claims[0].EvidenceArtifacts {
		tooManyArtifacts.Claims[0].EvidenceArtifacts[i] = "artifact"
	}
	assertGateCode(t, ValidateWorkGateResult(tooManyArtifacts), "gate_evidence_artifacts_full")

	raw := []byte(`{"verdict":"PASS","claims":[]}` + strings.Repeat(" ", WorkGateResultBytesLimit))
	assertGateCode(t, func() error { _, err := DecodeWorkGateResult(raw); return err }(), "gate_result_too_large")
}

func TestWorkGateEvidenceAdmissionRejectsEveryBound(t *testing.T) {
	meta := WorkGateEvidenceSubmission{ByteCount: 1}
	assertGateCode(t, ValidateWorkGateEvidenceAdmission(WorkGateEvidenceArtifactsPerTaskLimit, 0, meta), "gate_evidence_artifacts_full")
	meta.ByteCount = WorkGateEvidenceArtifactBytesLimit + 1
	assertGateCode(t, ValidateWorkGateEvidenceAdmission(0, 0, meta), "gate_evidence_artifact_too_large")
	meta.ByteCount = 1
	assertGateCode(t, ValidateWorkGateEvidenceAdmission(0, WorkGateEvidenceTotalBytesPerTaskLimit, meta), "gate_evidence_total_full")
}

func assertGateCode(t *testing.T, err error, want string) {
	t.Helper()
	var contractErr *WorkGateContractError
	if !errors.As(err, &contractErr) || contractErr.Code != want {
		t.Fatalf("error = %v, want %s", err, want)
	}
}
