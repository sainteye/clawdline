package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// On the Agent routes expected_version is optional: omitted, the write acts
// on the version the item holds then; named, it is still compared and a
// stale one is still a version_conflict. A replay under the same
// Idempotency-Key answers what the first attempt answered. A person's write
// still needs its version.
func TestAgentItemWritesTakeAnOptionalExpectedVersion(t *testing.T) {
	s, p, v := workV2AssignmentServer(t, session.StateWorking)
	owned, err := s.assignWorkV2(context.Background(), v.Item.ID, "local", v.Item.Version,
		"existing_session", p.s.ID, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	conversation := p.s.ConversationID
	base := "/v1/work/v2/agent/items/" + v.Item.ID
	post := func(method, path, body, key string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		s.workV2Route(rec, agentWorkV2Request(method, path, body, key))
		return rec
	}
	if rec := post(http.MethodPost, base+"/steps", `{"session_id":"`+conversation+`","title":"Wire it","position":0}`,
		"opt-step-add"); rec.Code != http.StatusCreated {
		t.Fatalf("step add without a version: %d %s", rec.Code, rec.Body)
	}
	read, err := s.workV2().Item(context.Background(), v.Item.ID)
	if err != nil || len(read.Steps) != 1 {
		t.Fatalf("steps = %+v %v", read.Steps, err)
	}
	if rec := post(http.MethodPost, base+"/steps/"+read.Steps[0].ID+"/complete", `{"session_id":"`+conversation+`"}`,
		"opt-step-done"); rec.Code != http.StatusOK {
		t.Fatalf("step done without a version: %d %s", rec.Code, rec.Body)
	}
	phase := `{"session_id":"` + conversation + `","next":"implementing"}`
	first := post(http.MethodPost, base+"/phase", phase, "opt-phase")
	if first.Code != http.StatusOK {
		t.Fatalf("phase without a version: %d %s", first.Code, first.Body)
	}
	if again := post(http.MethodPost, base+"/phase", phase, "opt-phase"); again.Code != first.Code ||
		again.Body.String() != first.Body.String() {
		t.Fatalf("replay = %d %s, first %s", again.Code, again.Body, first.Body)
	}
	stale := `{"expected_version":` + strconv.FormatInt(owned.Item.Version, 10) + `,"session_id":"` + conversation +
		`","role":"spec","title":"Spec","body":"What it does."}`
	if rec := post(http.MethodPost, base+"/documents", stale, "opt-doc-stale"); rec.Code != http.StatusConflict {
		t.Fatalf("stale version: %d %s", rec.Code, rec.Body)
	}
	doc := `{"session_id":"` + conversation + `","role":"spec","title":"Spec","body":"What it does."}`
	rec := post(http.MethodPost, base+"/documents", doc, "opt-doc")
	if rec.Code != http.StatusCreated {
		t.Fatalf("doc without a version: %d %s", rec.Code, rec.Body)
	}
	var answer struct {
		Item workV2ItemWire `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil || answer.Item.Phase != "implementing" {
		t.Fatalf("doc answer = %+v %v", answer.Item, err)
	}
	// A person's edit names its version; none is no version at all.
	renamed := "Renamed"
	_, err = s.workV2().Edit(context.Background(), v.Item.ID, app.EditWorkV2{Title: &renamed, Actor: "local", Person: true}, nil)
	var we *app.WorkError
	if !errors.As(err, &we) || we.Code != "version_conflict" {
		t.Fatalf("person edit without a version = %v", err)
	}
}
