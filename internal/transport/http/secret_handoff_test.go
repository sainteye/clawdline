package http

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/work"
)

func secretHandoffReceiptNote(t *testing.T, s *Server, source, target, key string) (string, string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"source_conversation": source, "target_session": "pane-target", "kind": "secret",
		"title": "Fixture credential", "summary": "A local test tool needs a credential.",
		"action": "Hand off the fixture value.", "reason": "The test tool needs it.",
	})
	if err != nil {
		t.Fatal(err)
	}
	created := httptest.NewRecorder()
	s.workV2Route(created, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/human-interventions", string(body), key))
	var result struct {
		Note humanInterventionWire `json:"note"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil || created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s %v", created.Code, created.Body, err)
	}
	return result.Note.ID, "/v1/work/v2/human-interventions/conversation:" + target + "/" + result.Note.ID + "/handoff"
}

func TestSecretHandoffReceiptCannotVerifyASecretGuess(t *testing.T) {
	s, source, target := humanInterventionTestServer(t)
	s.secretHandoffs = app.NewSecretHandoffs()
	_, path := secretHandoffReceiptNote(t, s, source, target, "create")
	// A short, synthetic PIN makes the offline dictionary risk explicit. No
	// production receipt or real credential is read by this regression.
	const value = "0472"
	const body = `{"value":"0472"}`
	const key = "submit"
	submitted := httptest.NewRecorder()
	s.workV2Route(submitted, personWorkV2Request(http.MethodPost, path, body, key))
	if submitted.Code != http.StatusOK {
		t.Fatalf("submit: %d %s", submitted.Code, submitted.Body)
	}
	k := store.ReceiptKey{Scope: workScope, Actor: "device:phone", Key: key}
	if claim, err := s.store.LookupReceipt(t.Context(), k); err != nil || claim.Outcome != store.ReceiptReplay {
		t.Fatalf("submission receipt missing: %s %v", claim.Outcome, err)
	}
	for name, digest := range map[string]string{
		"request body with public route": requestDigest([]byte(http.MethodPost), []byte(path), []byte(body)),
		"framed body":                    requestDigest([]byte(body)),
		"framed value":                   requestDigest([]byte(value)),
		"body hash":                      fmt.Sprintf("%x", sha256.Sum256([]byte(body))),
		"value hash":                     fmt.Sprintf("%x", sha256.Sum256([]byte(value))),
	} {
		t.Run(name, func(t *testing.T) {
			claim, err := s.store.ClaimReceipt(t.Context(), k, digest, store.ReceiptPolicy{}, time.Now())
			if err != nil || claim.Outcome != store.ReceiptMismatch {
				t.Fatalf("durable receipt verifies a secret guess: outcome=%s error=%v", claim.Outcome, err)
			}
		})
	}
}

func TestSecretHandoffRetryAcknowledgesFirstValueWithoutComparingIt(t *testing.T) {
	s, source, target := humanInterventionTestServer(t)
	s.secretHandoffs = app.NewSecretHandoffs()
	id, path := secretHandoffReceiptNote(t, s, source, target, "create")
	first := httptest.NewRecorder()
	s.workV2Route(first, personWorkV2Request(http.MethodPost, path, `{"value":"first fixture"}`, "submit"))
	if first.Code != http.StatusOK {
		t.Fatalf("submit: %d %s", first.Code, first.Body)
	}
	// A hash, HMAC, length or other fingerprint of the value would make this
	// retry differ. Neither retries nor JSON formatting may compare secrets.
	retry := httptest.NewRecorder()
	s.workV2Route(retry, personWorkV2Request(http.MethodPost, path, "{\n \"value\":\"a longer replacement fixture\"\n}", "submit"))
	if retry.Code != first.Code || retry.Body.String() != first.Body.String() || retry.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("retry: %d %s", retry.Code, retry.Body)
	}
	value, err := s.secretHandoffs.Take(id, target)
	if err != nil || string(value) != "first fixture" {
		t.Fatalf("retry replaced the first value: %v", err)
	}
	if _, err := s.secretHandoffs.Take(id, target); err == nil {
		t.Fatal("value was claimable twice")
	}
	// The operation still binds the exact Note, and ordinary Note writes
	// continue to reject changed bodies under the same key.
	otherID, otherPath := secretHandoffReceiptNote(t, s, source, target, "create-other")
	other := httptest.NewRecorder()
	s.workV2Route(other, personWorkV2Request(http.MethodPost, otherPath, `{"value":"other fixture"}`, "submit"))
	if other.Code != http.StatusConflict || !strings.Contains(other.Body.String(), "idempotency_key_reused") || s.secretHandoffs.Pending(otherID, target) {
		t.Fatalf("key reused for another Note: %d %s", other.Code, other.Body)
	}
	changedNote := strings.Replace(humanInterventionBody(source), "Choose a date", "A changed title", 1)
	changed := httptest.NewRecorder()
	s.workV2Route(changed, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/human-interventions", changedNote, "create"))
	if changed.Code != http.StatusConflict || !strings.Contains(changed.Body.String(), "idempotency_key_reused") {
		t.Fatalf("ordinary Note write stopped comparing bodies: %d %s", changed.Code, changed.Body)
	}
	// Read the durable receipt through a new Store handle and discard the
	// memory-only handoffs, as a daemon restart does. The fixture's original
	// handle stays open for asynchronous Note notification effects.
	reopened, err := store.Open(s.broker.Dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	s.store = reopened
	s.secretHandoffs = app.NewSecretHandoffs()
	afterRestart := httptest.NewRecorder()
	s.workV2Route(afterRestart, personWorkV2Request(http.MethodPost, path, `{"value":"restart fixture"}`, "submit"))
	if afterRestart.Code != first.Code || afterRestart.Body.String() != first.Body.String() || s.secretHandoffs.Pending(id, target) {
		t.Fatalf("restart replay recreated a value: %d %s", afterRestart.Code, afterRestart.Body)
	}
	newKey := httptest.NewRecorder()
	s.workV2Route(newKey, personWorkV2Request(http.MethodPost, path, `{"value":"restart fixture"}`, "new-key"))
	if newKey.Code != http.StatusNotFound || s.secretHandoffs.Pending(id, target) {
		t.Fatalf("restart reopened a completed Note: %d %s", newKey.Code, newKey.Body)
	}
}

func TestSecretHandoffLegacyReceiptFailsClosedWithoutRewritingIt(t *testing.T) {
	for _, state := range []string{"pending", "complete", "expired"} {
		t.Run(state, func(t *testing.T) {
			s, source, target := humanInterventionTestServer(t)
			s.secretHandoffs = app.NewSecretHandoffs()
			id, path := secretHandoffReceiptNote(t, s, source, target, "create")
			const body = `{"value":"legacy fixture"}`
			k := store.ReceiptKey{Scope: workScope, Actor: "device:phone", Key: "legacy"}
			digest := requestDigest([]byte(http.MethodPost), []byte(path), []byte(body))
			now := time.Now()
			claim, err := s.store.ClaimReceipt(t.Context(), k, digest, store.ReceiptPolicy{}, now)
			if err != nil || claim.Outcome != store.ReceiptNew {
				t.Fatalf("seed legacy receipt: %s %v", claim.Outcome, err)
			}
			wantOutcome := store.ReceiptPending
			if state != "pending" {
				note, err := s.store.HumanIntervention(t.Context(), id)
				if err != nil {
					t.Fatal(err)
				}
				_, _, err = s.humanInterventions().Update(t.Context(), id, target, "resolve", "", note.Version, k,
					func(work.HumanIntervention) []byte { return []byte(`{"ok":true,"received":true}`) })
				if err != nil {
					t.Fatal(err)
				}
				wantOutcome = store.ReceiptReplay
				if state == "expired" {
					wantOutcome = store.ReceiptExpired
					claim, err = s.store.ClaimReceipt(t.Context(), k, digest, store.ReceiptPolicy{}, now.Add(25*time.Hour))
					if err != nil || claim.Outcome != wantOutcome {
						t.Fatalf("expire fixture: %s %v", claim.Outcome, err)
					}
				}
			}
			retry := httptest.NewRecorder()
			s.workV2Route(retry, personWorkV2Request(http.MethodPost, path, body, k.Key))
			if retry.Code != http.StatusConflict || !strings.Contains(retry.Body.String(), "idempotency_key_reused") || s.secretHandoffs.Pending(id, target) {
				t.Fatalf("legacy retry was not refused: %d %s", retry.Code, retry.Body)
			}
			// Compatibility is fail-closed, not an automatic data migration.
			claim, err = s.store.ClaimReceipt(t.Context(), k, digest, store.ReceiptPolicy{}, now)
			if err != nil || claim.Outcome != wantOutcome {
				t.Fatalf("legacy receipt was rewritten: %s %v", claim.Outcome, err)
			}
		})
	}
}

func TestSecretHandoffConcurrentSubmissionsKeepOneValue(t *testing.T) {
	for _, sameKey := range []bool{true, false} {
		t.Run(fmt.Sprintf("same-key=%t", sameKey), func(t *testing.T) {
			s, source, target := humanInterventionTestServer(t)
			s.secretHandoffs = app.NewSecretHandoffs()
			id, path := secretHandoffReceiptNote(t, s, source, target, "create")
			before, err := s.store.HumanIntervention(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			var wg sync.WaitGroup
			var answers [2]*httptest.ResponseRecorder
			values := [2]string{"first fixture", "second fixture"}
			for i := range answers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					key := "submit"
					if !sameKey {
						key += fmt.Sprint(i)
					}
					answers[i] = httptest.NewRecorder()
					s.workV2Route(answers[i], personWorkV2Request(http.MethodPost, path, fmt.Sprintf(`{"value":%q}`, values[i]), key))
				}()
			}
			close(start)
			wg.Wait()
			accepted := 0
			for _, answer := range answers {
				if answer.Code == http.StatusOK {
					accepted++
				} else if answer.Code != http.StatusNotFound && answer.Code != http.StatusServiceUnavailable {
					t.Fatalf("unexpected concurrent refusal: %d %s", answer.Code, answer.Body)
				}
			}
			wantAccepted := 1
			if sameKey {
				wantAccepted = 2 // The second acknowledgement is a replay.
			}
			if accepted != wantAccepted {
				t.Fatalf("accepted %d requests, want %d", accepted, wantAccepted)
			}
			note, err := s.store.HumanIntervention(t.Context(), id)
			if err != nil || note.ResolvedAt == nil || note.Version != before.Version+1 {
				t.Fatalf("Note was not completed exactly once: %v", err)
			}
			value, err := s.secretHandoffs.Take(id, target)
			if err != nil {
				t.Fatalf("no first value: %v", err)
			}
			matched := false
			for i, candidate := range values {
				matched = matched || (string(value) == candidate && answers[i].Code == http.StatusOK)
			}
			if !matched {
				t.Fatal("the pending value came from a refused submission")
			}
			if _, err := s.secretHandoffs.Take(id, target); err == nil {
				t.Fatal("concurrent submissions allowed a second claim")
			}
		})
	}
}

func TestSecretHandoffOnlyTargetMayClaimOnce(t *testing.T) {
	s, source, target := humanInterventionTestServer(t)
	s.secretHandoffs = app.NewSecretHandoffs()
	createBody, _ := json.Marshal(map[string]any{
		"source_conversation": source, "target_session": "pane-target", "kind": "secret",
		"title": "Database credential", "summary": "A local tool needs a credential.",
		"action": "Paste it into Secret Handoff.", "reason": "The tool cannot continue without it.",
	})
	created := httptest.NewRecorder()
	s.workV2Route(created, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/human-interventions", string(createBody), "secret-note"))
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body)
	}
	var result struct {
		Note humanInterventionWire `json:"note"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	id := result.Note.ID
	base := "/v1/work/v2/human-interventions/conversation:" + target + "/" + id + "/handoff"
	secret := "SAFE_FIXTURE_DONT_STORE_123"
	wrong := httptest.NewRecorder()
	s.workV2Route(wrong, agentWorkV2Request(http.MethodPost, base, `{"value":"`+secret+`"}`, "wrong-actor"))
	if wrong.Code != http.StatusForbidden {
		t.Fatalf("agent submitted: %d %s", wrong.Code, wrong.Body)
	}
	submitted := httptest.NewRecorder()
	s.workV2Route(submitted, personWorkV2Request(http.MethodPost, base, `{"value":"`+secret+`"}`, "secret-submit"))
	if submitted.Code != http.StatusOK || strings.Contains(submitted.Body.String(), secret) {
		t.Fatalf("submit: %d %s", submitted.Code, submitted.Body)
	}
	listPath := "/v1/work/v2/human-interventions/conversation:" + target
	listed := httptest.NewRecorder()
	s.workV2Route(listed, personWorkV2Request(http.MethodGet, listPath, "", "secret-read"))
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), secret) {
		t.Fatalf("receipt read: %d %s", listed.Code, listed.Body)
	}
	var waiting struct {
		Rows []humanInterventionWire `json:"rows"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &waiting); err != nil || len(waiting.Rows) != 1 || waiting.Rows[0].ResolvedAt == nil {
		t.Fatalf("completed state missing after reload: %+v %v", waiting, err)
	}
	note, err := s.store.HumanIntervention(t.Context(), id)
	if err != nil || strings.Contains(note.Title+note.Summary+note.Action+note.Reason+note.Detail+note.Resolution, secret) {
		t.Fatalf("Note stored value: %v", err)
	}
	if note.ResolvedAt == nil || note.ReadAt == nil {
		t.Fatalf("handoff did not complete the Note before claim: %+v", note)
	}
	if !s.secretHandoffs.Pending(id, target) {
		t.Fatal("completing the Note discarded the unclaimed value")
	}
	replayed := httptest.NewRecorder()
	s.workV2Route(replayed, personWorkV2Request(http.MethodPost, base, `{"value":"`+secret+`"}`, "secret-submit"))
	if replayed.Code != http.StatusOK || replayed.Body.String() != submitted.Body.String() {
		t.Fatalf("completed handoff replay: %d %s", replayed.Code, replayed.Body)
	}
	duplicate := httptest.NewRecorder()
	s.workV2Route(duplicate, personWorkV2Request(http.MethodPost, base, `{"value":"replacement"}`, "secret-resubmit"))
	if duplicate.Code != http.StatusNotFound {
		t.Fatalf("completed Note accepted a new submission: %d %s", duplicate.Code, duplicate.Body)
	}
	claimPath := "/v1/work/v2/agent/human-interventions/" + id + "/claim"
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	nonce := []byte("0123456789abcdef0123456789abcdef")
	callback := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			callback <- "accept failed"
			return
		}
		defer conn.Close()
		presented := make([]byte, 32)
		if _, err := io.ReadFull(conn, presented); err != nil || string(presented) != string(nonce) {
			callback <- "bad nonce"
			return
		}
		var size [4]byte
		if _, err := io.ReadFull(conn, size[:]); err != nil {
			callback <- "bad size"
			return
		}
		value := make([]byte, binary.BigEndian.Uint32(size[:]))
		if _, err := io.ReadFull(conn, value); err != nil {
			callback <- "bad value"
			return
		}
		_, _ = conn.Write([]byte{1})
		callback <- string(value)
	}()
	claimBody := func(conversation string) string {
		return fmt.Sprintf(`{"conversation_id":%q,"callback_port":%d,"nonce":%q}`, conversation,
			listener.Addr().(*net.TCPAddr).Port, base64.RawURLEncoding.EncodeToString(nonce))
	}
	other := httptest.NewRecorder()
	s.workV2Route(other, agentWorkV2Request(http.MethodPost, claimPath, claimBody(source), "wrong-target"))
	if other.Code != http.StatusNotFound {
		t.Fatalf("wrong target: %d %s", other.Code, other.Body)
	}
	claim := httptest.NewRecorder()
	s.workV2Route(claim, agentWorkV2Request(http.MethodPost, claimPath, claimBody(target), "claim"))
	gotCallback := ""
	select {
	case gotCallback = <-callback:
	case <-time.After(2 * time.Second):
		t.Fatal("callback did not arrive")
	}
	if claim.Code != http.StatusOK || strings.Contains(claim.Body.String(), secret) || claim.Header().Get("Cache-Control") != "no-store" || gotCallback != secret {
		t.Fatalf("claim: %d %q", claim.Code, claim.Body.String())
	}
	again := httptest.NewRecorder()
	s.workV2Route(again, agentWorkV2Request(http.MethodPost, claimPath, claimBody(target), "again"))
	if again.Code != http.StatusConflict || strings.Contains(again.Body.String(), secret) {
		t.Fatalf("reclaimed: %d %s", again.Code, again.Body)
	}
	note, err = s.store.HumanIntervention(t.Context(), id)
	if err != nil || note.ResolvedAt == nil || note.Resolution != "Secret Handoff used" {
		t.Fatalf("not handled: %+v %v", note, err)
	}
	claimedList := httptest.NewRecorder()
	s.workV2Route(claimedList, personWorkV2Request(http.MethodGet, listPath, "", "secret-read-after-claim"))
	var handled struct {
		Rows []humanInterventionWire `json:"rows"`
	}
	if err := json.Unmarshal(claimedList.Body.Bytes(), &handled); err != nil || claimedList.Code != http.StatusOK || len(handled.Rows) != 1 || handled.Rows[0].SecretReceived {
		t.Fatalf("receipt remained after claim: %+v %v", handled, err)
	}
	if err := filepath.WalkDir(s.broker.Dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(content), secret) {
			t.Errorf("secret reached a durable file: %s", entry.Name())
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSecretHandoffRejectedSubmissionKeepsNotePending(t *testing.T) {
	for _, tc := range []struct {
		name   string
		value  string
		full   bool
		status int
	}{
		{"empty", "", false, http.StatusUnprocessableEntity},
		{"too large", strings.Repeat("x", app.MaxSecretHandoffBytes+1), false, http.StatusRequestEntityTooLarge},
		{"full", "SAFE_FIXTURE", true, http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, source, target := humanInterventionTestServer(t)
			s.secretHandoffs = app.NewSecretHandoffs()
			created := httptest.NewRecorder()
			body, _ := json.Marshal(map[string]any{
				"source_conversation": source, "target_session": "pane-target", "kind": "secret",
				"title": "Credential", "summary": "Tool credential", "action": "Hand off the value.", "reason": "The tool needs it.",
			})
			s.workV2Route(created, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/human-interventions", string(body), "create"))
			var result struct {
				Note humanInterventionWire `json:"note"`
			}
			if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil || created.Code != http.StatusCreated {
				t.Fatalf("create: %d %s %v", created.Code, created.Body, err)
			}
			if tc.full {
				for i := 0; i < app.MaxSecretHandoffPending; i++ {
					if err := s.secretHandoffs.Put(fmt.Sprint(i), target, []byte("SAFE_FIXTURE")); err != nil {
						t.Fatal(err)
					}
				}
			}
			payload, _ := json.Marshal(map[string]string{"value": tc.value})
			submitted := httptest.NewRecorder()
			s.workV2Route(submitted, personWorkV2Request(http.MethodPost, "/v1/work/v2/human-interventions/conversation:"+target+"/"+result.Note.ID+"/handoff", string(payload), "submit"))
			if submitted.Code != tc.status {
				t.Fatalf("submit: %d %s", submitted.Code, submitted.Body)
			}
			note, err := s.store.HumanIntervention(t.Context(), result.Note.ID)
			if err != nil || note.ResolvedAt != nil || note.ReadAt != nil || note.Version != result.Note.Version {
				t.Fatalf("rejected submission changed the Note: %+v %v", note, err)
			}
			if s.secretHandoffs.Pending(note.ID, target) {
				t.Fatal("rejected value was accepted")
			}
		})
	}
}
