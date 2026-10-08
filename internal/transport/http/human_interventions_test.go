package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func humanInterventionTestServer(t *testing.T) (*Server, string, string) {
	t.Helper()
	source := session.Session{ID: "pane-source", Backend: session.BackendTmux, Assistant: session.AssistantCodex,
		ConversationID: "10000000-0000-4000-8000-000000000001", State: session.StateWorking, Label: "Manager"}
	target := session.Session{ID: "pane-target", Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		ConversationID: "10000000-0000-4000-8000-000000000002", State: session.StateWorking, Label: "Delivery"}
	p := &pane{s: target}
	s := paneServer(t, p)
	s.broker.Live = func(context.Context) []session.Session { return []session.Session{source, target} }
	return s, source.ConversationID, target.ConversationID
}

func humanInterventionBody(source string) string {
	b, _ := json.Marshal(map[string]any{"source_conversation": source, "target_session": "pane-target", "kind": "answer",
		"title": "Choose a date", "summary": "The schedule needs a date.", "action": "Choose one date before the release continues.",
		"reason": "Only the person knows the preferred date.", "options": []map[string]string{{"label": "Tuesday", "draft": "Tuesday works."}, {"label": "Wednesday", "draft": "Wednesday works."}}})
	return string(b)
}

func TestManagerRootCanPostToAnotherSessionWithoutSendingAReply(t *testing.T) {
	s, source, target := humanInterventionTestServer(t)
	path := "/v1/work/v2/agent/human-interventions"
	body := humanInterventionBody(source)
	create := httptest.NewRecorder()
	s.workV2Route(create, agentWorkV2Request(http.MethodPost, path, body, "human-create"))
	if create.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", create.Code, create.Body)
	}
	var created struct {
		Note humanInterventionWire `json:"note"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Note.SourceConversation != source || created.Note.TargetConversation != target || created.Note.TargetSession != "pane-target" || created.Note.SourceLabel != "Manager" {
		t.Fatalf("provenance: %+v", created.Note)
	}
	replay := httptest.NewRecorder()
	s.workV2Route(replay, agentWorkV2Request(http.MethodPost, path, body, "human-create"))
	if replay.Code != http.StatusCreated || replay.Body.String() != create.Body.String() {
		t.Fatalf("replay: %d %s", replay.Code, replay.Body)
	}
	readPath := "/v1/work/v2/human-interventions/conversation:" + target
	read := httptest.NewRecorder()
	s.workV2Route(read, personWorkV2Request(http.MethodGet, readPath, "", ""))
	var page struct {
		Rows []humanInterventionWire `json:"rows"`
	}
	if read.Code != http.StatusOK || json.Unmarshal(read.Body.Bytes(), &page) != nil || len(page.Rows) != 1 {
		t.Fatalf("read target: %d %s", read.Code, read.Body)
	}
	other := httptest.NewRecorder()
	s.workV2Route(other, personWorkV2Request(http.MethodGet, "/v1/work/v2/human-interventions/conversation:"+source, "", ""))
	if other.Code != http.StatusOK || json.Unmarshal(other.Body.Bytes(), &page) != nil || len(page.Rows) != 0 {
		t.Fatalf("read source: %d %s", other.Code, other.Body)
	}
	actionPath := readPath + "/" + created.Note.ID
	mark := httptest.NewRecorder()
	s.workV2Route(mark, personWorkV2Request(http.MethodPost, actionPath+"/read", fmt.Sprintf(`{"expected_version":%d}`, created.Note.Version), "human-read"))
	if mark.Code != http.StatusOK {
		t.Fatalf("read action: %d %s", mark.Code, mark.Body)
	}
	var marked struct {
		Note humanInterventionWire `json:"note"`
	}
	_ = json.Unmarshal(mark.Body.Bytes(), &marked)
	if marked.Note.ReadAt == nil || marked.Note.ResolvedAt != nil {
		t.Fatalf("read must not resolve: %+v", marked.Note)
	}
	stale := httptest.NewRecorder()
	s.workV2Route(stale, personWorkV2Request(http.MethodPost, actionPath+"/resolve", fmt.Sprintf(`{"expected_version":%d}`, created.Note.Version), "human-stale"))
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale: %d %s", stale.Code, stale.Body)
	}
	resolve := httptest.NewRecorder()
	s.workV2Route(resolve, personWorkV2Request(http.MethodPost, actionPath+"/resolve", fmt.Sprintf(`{"expected_version":%d}`, marked.Note.Version), "human-resolve"))
	if resolve.Code != http.StatusOK {
		t.Fatalf("resolve: %d %s", resolve.Code, resolve.Body)
	}
	_ = json.Unmarshal(resolve.Body.Bytes(), &marked)
	if marked.Note.ResolvedAt == nil {
		t.Fatalf("not resolved: %s", resolve.Body)
	}
}

func TestHumanInterventionRejectsMissingHumanActionAndMachineRead(t *testing.T) {
	s, source, target := humanInterventionTestServer(t)
	bad := httptest.NewRecorder()
	s.workV2Route(bad, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/human-interventions", `{"source_conversation":"`+source+`","target_session":"pane-target","kind":"read","title":"FYI","summary":"Just progress"}`, "human-bad"))
	if bad.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing action: %d %s", bad.Code, bad.Body)
	}
	machine := httptest.NewRecorder()
	s.workV2Route(machine, agentWorkV2Request(http.MethodGet, "/v1/work/v2/human-interventions/conversation:"+target, "", ""))
	if machine.Code != http.StatusForbidden {
		t.Fatalf("machine person read: %d %s", machine.Code, machine.Body)
	}
}

func TestHumanInterventionRejectsUnresolvedSourceAndTarget(t *testing.T) {
	s, source, _ := humanInterventionTestServer(t)
	path := "/v1/work/v2/agent/human-interventions"
	missingSource := httptest.NewRecorder()
	s.workV2Route(missingSource, agentWorkV2Request(http.MethodPost, path,
		humanInterventionBody("10000000-0000-4000-8000-000000000099"), "missing-source"))
	if missingSource.Code != http.StatusNotFound {
		t.Fatalf("missing source: %d %s", missingSource.Code, missingSource.Body)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(humanInterventionBody(source)), &body); err != nil {
		t.Fatal(err)
	}
	body["target_session"] = "missing-pane"
	bytes, _ := json.Marshal(body)
	missingTarget := httptest.NewRecorder()
	s.workV2Route(missingTarget, agentWorkV2Request(http.MethodPost, path, string(bytes), "missing-target"))
	if missingTarget.Code != http.StatusConflict {
		t.Fatalf("missing target: %d %s", missingTarget.Code, missingTarget.Body)
	}
}

func TestAgentNotePushesOnceAndRequestedNoteStaysQuiet(t *testing.T) {
	s, source, target := humanInterventionTestServer(t)
	type sentPush struct{ title, body, terminal, tag string }
	pushes := make(chan sentPush, 2)
	s.broker.Push = func(_ context.Context, title, body, terminal, tag string) (int, int, error) {
		pushes <- sentPush{title, body, terminal, tag}
		return 1, 0, nil
	}
	path := "/v1/work/v2/agent/human-interventions"
	input := humanInterventionBody(source)
	created := httptest.NewRecorder()
	s.workV2Route(created, agentWorkV2Request(http.MethodPost, path, input, "agent-note"))
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body)
	}
	var answer struct {
		Note humanInterventionWire `json:"note"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	select {
	case push := <-pushes:
		if push.terminal != "pane-target" || push.title == "" || push.body == "" || push.tag != "human-intervention-"+answer.Note.ID {
			t.Fatalf("wrong push: %+v", push)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("agent note did not push")
	}
	replay := httptest.NewRecorder()
	s.workV2Route(replay, agentWorkV2Request(http.MethodPost, path, input, "agent-note"))
	if replay.Code != http.StatusCreated || replay.Body.String() != created.Body.String() {
		t.Fatalf("replay: %d %s", replay.Code, replay.Body)
	}
	effects, err := s.store.Effects(context.Background(), orchestrator.EffectHumanInterventionPush, answer.Note.ID)
	if err != nil || len(effects) != 1 {
		t.Fatalf("effects after replay: %d %v", len(effects), err)
	}
	var requested map[string]any
	if err := json.Unmarshal([]byte(input), &requested); err != nil {
		t.Fatal(err)
	}
	requested["requested_by_person"] = true
	raw, _ := json.Marshal(requested)
	quiet := httptest.NewRecorder()
	s.workV2Route(quiet, agentWorkV2Request(http.MethodPost, path, string(raw), "requested-note"))
	if quiet.Code != http.StatusCreated {
		t.Fatalf("requested note: %d %s", quiet.Code, quiet.Body)
	}
	if err := json.Unmarshal(quiet.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	effects, err = s.store.Effects(context.Background(), orchestrator.EffectHumanInterventionPush, answer.Note.ID)
	if err != nil || len(effects) != 0 || len(pushes) != 0 {
		t.Fatalf("requested note notified: effects=%d pushes=%d err=%v", len(effects), len(pushes), err)
	}
	read := httptest.NewRecorder()
	s.workV2Route(read, personWorkV2Request(http.MethodGet, "/v1/work/v2/human-interventions/conversation:"+target, "", ""))
	var listed struct {
		Rows []humanInterventionWire `json:"rows"`
	}
	if read.Code != http.StatusOK || json.Unmarshal(read.Body.Bytes(), &listed) != nil || len(listed.Rows) != 2 {
		t.Fatalf("both notes remain visible: %d %s", read.Code, read.Body)
	}
}

func TestAgentNoteRemainsReadableWhenPushFails(t *testing.T) {
	s, source, target := humanInterventionTestServer(t)
	attempted := make(chan struct{}, 1)
	s.broker.Push = func(context.Context, string, string, string, string) (int, int, error) {
		attempted <- struct{}{}
		return 0, 1, errors.New("push service unavailable")
	}
	created := httptest.NewRecorder()
	s.workV2Route(created, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/human-interventions",
		humanInterventionBody(source), "push-fails"))
	if created.Code != http.StatusCreated {
		t.Fatalf("note creation depended on push: %d %s", created.Code, created.Body)
	}
	select {
	case <-attempted:
	case <-time.After(2 * time.Second):
		t.Fatal("no push attempt")
	}
	read := httptest.NewRecorder()
	s.workV2Route(read, personWorkV2Request(http.MethodGet,
		"/v1/work/v2/human-interventions/conversation:"+target, "", ""))
	var listed struct {
		Rows []humanInterventionWire `json:"rows"`
	}
	if read.Code != http.StatusOK || json.Unmarshal(read.Body.Bytes(), &listed) != nil || len(listed.Rows) != 1 {
		t.Fatalf("note disappeared after push failure: %d %s", read.Code, read.Body)
	}
}
