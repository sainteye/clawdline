package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func evidenceFixture(n int) map[string]any {
	commits := make([]string, n)
	for i := range commits {
		commits[i] = fmt.Sprintf("%040x", i+2)
	}
	return map[string]any{"schema_version": 1, "summary": "Verified.", "baseline_sha256": strings.Repeat("a", 64), "manifest_sha256": strings.Repeat("b", 64), "git_receipt": map[string]any{"schemaVersion": 1, "ok": true, "root": "/repo", "baselineHead": fmt.Sprintf("%040x", 1), "head": commits[n-1], "commits": commits, "ownedFiles": 6353, "issues": []any{}}}
}
func evidenceJSON(v any) string { raw, _ := json.Marshal(v); return string(raw) }

func TestDeliveryEvidencePreservesAll219Commits(t *testing.T) {
	raw := evidenceJSON(evidenceFixture(219))
	digest, n, err := ValidateDeliveryEvidence(raw, "Verified.")
	if err != nil || n != 219 || len(digest) != 64 {
		t.Fatalf("%s %d %v", digest, n, err)
	}

	padded := raw + strings.Repeat(" ", DeliveryEvidenceBytesLimit-len(raw))
	if _, _, err := ValidateDeliveryEvidence(padded, "Verified."); err != nil {
		t.Fatalf("byte boundary rejected: %v", err)
	}
	if _, _, err := ValidateDeliveryEvidence(padded+" ", "Verified."); err == nil || err.Error() != "delivery_evidence_too_large" {
		t.Fatalf("byte overflow: %v", err)
	}
	again, _, _ := ValidateDeliveryEvidence(raw, "Verified.")
	if digest != again {
		t.Fatal("unstable digest")
	}
	changed, _, _ := ValidateDeliveryEvidence(raw+"\n", "Verified.")
	if digest == changed {
		t.Fatal("exact bytes were not bound")
	}
}
func TestDeliveryEvidenceRejectsIncompleteAndContradictoryResults(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		change     func(map[string]any)
	}{
		{"summary", "delivery_evidence_mismatch", func(v map[string]any) { v["summary"] = "Different." }},
		{"unknown verification", "delivery_evidence_unverified", func(v map[string]any) { delete(v["git_receipt"].(map[string]any), "ok") }},
		{"failed verification", "delivery_evidence_unverified", func(v map[string]any) { v["git_receipt"].(map[string]any)["ok"] = false }},
		{"issues", "delivery_evidence_unverified", func(v map[string]any) { v["git_receipt"].(map[string]any)["issues"] = []string{"dirty"} }},
		{"missing issues", "delivery_evidence_unverified", func(v map[string]any) { delete(v["git_receipt"].(map[string]any), "issues") }},
		{"duplicate commit", "delivery_evidence_invalid", func(v map[string]any) {
			v["git_receipt"].(map[string]any)["commits"] = []string{strings.Repeat("c", 40), strings.Repeat("c", 40)}
		}},
		{"abbreviation", "delivery_evidence_invalid", func(v map[string]any) { v["git_receipt"].(map[string]any)["commits"] = []string{"abcdef0"} }},
		{"broken input binding", "delivery_evidence_invalid", func(v map[string]any) { v["manifest_sha256"] = "bad" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := evidenceFixture(1)
			tc.change(v)
			_, _, err := ValidateDeliveryEvidence(evidenceJSON(v), "Verified.")
			if err == nil || err.Error() != tc.code {
				t.Fatalf("%v", err)
			}
		})
	}
	raw := evidenceJSON(evidenceFixture(1))
	for _, bad := range []string{raw + " {}", strings.Replace(raw, `"ok":true`, `"ok":false,"ok":true`, 1), "{", strings.Repeat("x", DeliveryEvidenceBytesLimit+1)} {
		if _, _, err := ValidateDeliveryEvidence(bad, "Verified."); err == nil {
			t.Fatal("invalid document accepted")
		}
	}
	for _, n := range []int{DeliveryEvidenceCommitsLimit, DeliveryEvidenceCommitsLimit + 1} {
		_, count, err := ValidateDeliveryEvidence(evidenceJSON(evidenceFixture(n)), "Verified.")
		if n == DeliveryEvidenceCommitsLimit && (err != nil || count != n) {
			t.Fatalf("boundary %v", err)
		}
		if n > DeliveryEvidenceCommitsLimit && (err == nil || err.Error() != "delivery_evidence_too_large") {
			t.Fatalf("oversize %v", err)
		}
	}
}
