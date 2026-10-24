package http

import (
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
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app"
)

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
