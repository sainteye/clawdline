package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func reportEvidenceFixture(n int, summary string) string {
	commits := make([]string, n)
	for i := range commits {
		commits[i] = fmt.Sprintf("%040x", i+2)
	}
	v := map[string]any{"schema_version": 1, "summary": summary, "baseline_sha256": strings.Repeat("a", 64), "manifest_sha256": strings.Repeat("b", 64), "git_receipt": map[string]any{"schemaVersion": 1, "ok": true, "root": "/repo", "baselineHead": fmt.Sprintf("%040x", 1), "head": commits[n-1], "commits": commits, "ownedFiles": 6353, "issues": []any{}}}
	raw, _ := json.Marshal(v)
	return string(raw)
}
func evidenceReport(t *testing.T, s *Server, terminal, summary, evidence, key string) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]string{"summary": summary}
	if evidence != "" {
		body["delivery_evidence"] = evidence
	}
	raw, _ := json.Marshal(body)
	req := w8Request(http.MethodPost, "/v1/orchestrator/sessions/"+terminal+"/complete", string(raw), true, nil)
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	s.brokerSessionRoute(rec, req)
	return rec
}
func TestSessionDeliveryStoresAllEvidenceAndReplaysOnce(t *testing.T) {
	s, sess := reportServer(t)
	raw := reportEvidenceFixture(219, "Verified.")
	first := evidenceReport(t, s, sess.ID, "Verified.", raw, "batch")
	if first.Code != 200 {
		t.Fatalf("%d %s", first.Code, first.Body)
	}
	second := evidenceReport(t, s, sess.ID, "Verified.", raw, "batch")
	if second.Code != 200 || first.Body.String() != second.Body.String() || second.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay %d %s", second.Code, second.Body)
	}
	n, err := s.store.EventCount(context.Background(), "session.delivered")
	if err != nil || n != 1 {
		t.Fatalf("events %d %v", n, err)
	}
	events, err := s.store.LatestEvents(context.Background(), "session.delivered", []string{sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Evidence string `json:"delivery_evidence"`
		Digest   string `json:"delivery_evidence_sha256"`
	}
	if json.Unmarshal(events[sess.ID].Payload, &payload) != nil || payload.Evidence != raw {
		t.Fatal("evidence bytes lost")
	}
	sum := sha256.Sum256([]byte(raw))
	if payload.Digest != hex.EncodeToString(sum[:]) || !strings.Contains(first.Body.String(), `"delivery_evidence_commits":219`) {
		t.Fatalf("binding %s", first.Body)
	}
	mismatch := evidenceReport(t, s, sess.ID, "Changed.", reportEvidenceFixture(219, "Changed."), "batch")
	if mismatch.Code != 409 || !strings.Contains(mismatch.Body.String(), "idempotency_key_reused") {
		t.Fatalf("%d %s", mismatch.Code, mismatch.Body)
	}
	effects, err := s.store.Effects(context.Background(), "session.delivery.push", sess.ID)
	if err != nil || len(effects) != 1 {
		t.Fatalf("pushes %d %v", len(effects), err)
	}
}
func TestSessionDeliveryEvidenceRejectsWithoutRecording(t *testing.T) {
	s, sess := reportServer(t)
	for _, tc := range []struct{ summary, evidence, key, code string }{
		{"Changed.", reportEvidenceFixture(1, "Verified."), "mismatch", "delivery_evidence_mismatch"},
		{"Verified.", strings.Replace(reportEvidenceFixture(1, "Verified."), `"ok":true`, `"ok":false`, 1), "failed", "delivery_evidence_unverified"},
		{"Verified.", "{", "broken", "delivery_evidence_invalid"},
		{"Verified.", strings.Repeat("x", (256<<10)+1), "large", "delivery_evidence_too_large"},
		{"Verified.", reportEvidenceFixture(1, "Verified."), "", "idempotency_key_required"},
	} {
		rec := evidenceReport(t, s, sess.ID, tc.summary, tc.evidence, tc.key)
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), tc.code) {
			t.Fatalf("%s: %d %s", tc.code, rec.Code, rec.Body)
		}
	}
	if n, _ := s.store.EventCount(context.Background(), "session.delivered"); n != 0 {
		t.Fatalf("invalid evidence recorded %d", n)
	}
}
func TestSessionDeliveryKeeps500CharacterBoundaryAndLegacyBehavior(t *testing.T) {
	s, sess := reportServer(t)
	for _, withEvidence := range []bool{false, true} {
		for _, n := range []int{500, 501} {
			summary := strings.Repeat("字", n)
			evidence := ""
			if withEvidence {
				evidence = reportEvidenceFixture(1, summary)
			}
			rec := evidenceReport(t, s, sess.ID, summary, evidence, fmt.Sprintf("boundary-%t-%d", withEvidence, n))
			if (rec.Code == 200) != (n == 500) {
				t.Fatalf("%t %d: %d %s", withEvidence, n, rec.Code, rec.Body)
			}
		}
	}
	// Legacy callers without a key still issue a new receipt on every call.
	for i := 0; i < 2; i++ {
		if r := evidenceReport(t, s, sess.ID, "Done.", "", ""); r.Code != 200 {
			t.Fatal(r.Body)
		}
	}
	if n, _ := s.store.EventCount(context.Background(), "session.delivered"); n != 4 {
		t.Fatalf("legacy receipts %d", n)
	}
}

func TestSessionDeliveryCrashAfterEventIsUnknownAndNeverDuplicated(t *testing.T) {
	s, sess := reportServer(t)
	raw := reportEvidenceFixture(1, "Verified.")
	key := store.ReceiptKey{Scope: "session-delivery", Actor: sess.ID, Key: "crashed"}
	body, _ := json.Marshal(struct {
		Summary  string  `json:"summary"`
		Evidence *string `json:"delivery_evidence,omitempty"`
	}{"Verified.", &raw})
	if _, err := s.store.ClaimReceipt(context.Background(), key, requestDigest(body), store.ReceiptPolicy{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.broker.ReportSessionDeliveryWithEvidence(context.Background(), sess.ID, "Verified.", raw); err != nil {
		t.Fatal(err)
	}
	dir := s.broker.Dir
	if err := s.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	s.store = reopened
	s.broker.Store = reopened
	for i := 0; i < 2; i++ {
		rec := evidenceReport(t, s, sess.ID, "Verified.", raw, "crashed")
		if rec.Code != 409 || !strings.Contains(rec.Body.String(), "request_outcome_unknown") {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	}
	if n, _ := s.store.EventCount(context.Background(), "session.delivered"); n != 1 {
		t.Fatalf("duplicate events %d", n)
	}
}

func TestNullEvidenceCannotFallBackToLegacyCompletion(t *testing.T) {
	s, sess := reportServer(t)
	req := w8Request(http.MethodPost, "/v1/orchestrator/sessions/"+sess.ID+"/complete", `{"summary":"Verified.","delivery_evidence":null}`, true, nil)
	rec := httptest.NewRecorder()
	s.brokerSessionRoute(rec, req)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "delivery_evidence_invalid") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if n, _ := s.store.EventCount(context.Background(), "session.delivered"); n != 0 {
		t.Fatal("null evidence created receipt")
	}
}
