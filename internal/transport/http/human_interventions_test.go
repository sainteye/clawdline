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

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestChildCanManageItsNotesThroughTheCredentialGate(t *testing.T) {
	for _, ownTarget := range []bool{true, false} {
		t.Run(fmt.Sprintf("own-target=%t", ownTarget), func(t *testing.T) {
			s, root, target := humanInterventionTestServer(t)
			child, terminal, label := root, "pane-source", "Manager"
			if ownTarget {
				child, terminal, label = target, "pane-target", "Delivery"
			}
			task := orchestrator.Record{ID: "7a5c0000-0000-4000-8000-000000000001", Kind: "custom", Title: "child",
				Assistant: "codex", State: orchestrator.StateBriefed, CreatedAt: time.Now(), ChildTerminalID: terminal}
			raw, _ := json.Marshal(task)
			if _, err := s.store.CreateBrokerTask(context.Background(), store.BrokerRow{ID: task.ID, Project: "/p",
				Assistant: task.Assistant, State: string(task.State), CreatedAt: task.CreatedAt, SecretHash: "h", Record: raw}, nil); err != nil {
				t.Fatal(err)
			}
			s.cfg = config.Config{Dir: t.TempDir(), Port: 7757}
			t.Cleanup(func() { gates.Delete(s.cfg.Dir) })
			machine, err := s.gate().files.MachineToken()
			if err != nil {
				t.Fatal(err)
			}
			handler := s.Handler()
			request := func(method, path, body, key string) *httptest.ResponseRecorder {
				return call{method: method, path: path, body: body, headers: map[string]string{
					"X-Clawdline-Orchestrator": machine, "Content-Type": "application/json", "Idempotency-Key": key,
				}}.do(handler)
			}
			base := "/v1/work/v2/agent/human-interventions"
			body := humanInterventionBody(child)
			created := request(http.MethodPost, base, body, "child-note")
			var answer struct {
				Note humanInterventionWire `json:"note"`
			}
			if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &answer) != nil {
				t.Fatalf("child create: %d %s", created.Code, created.Body)
			}
			if answer.Note.SourceConversation != child || answer.Note.SourceLabel != label || answer.Note.TargetConversation != target {
				t.Fatalf("child provenance: %+v", answer.Note)
			}
			replay := request(http.MethodPost, base, body, "child-note")
			if replay.Code != created.Code || replay.Body.String() != created.Body.String() {
				t.Fatalf("child retry: %d %s", replay.Code, replay.Body)
			}
			listed := request(http.MethodGet, base+"?source_conversation="+child, "", "")
			var page struct {
				Rows []humanInterventionWire `json:"rows"`
			}
			if listed.Code != http.StatusOK || json.Unmarshal(listed.Body.Bytes(), &page) != nil || len(page.Rows) != 1 || page.Rows[0].ID != answer.Note.ID {
				t.Fatalf("child list: %d %s", listed.Code, listed.Body)
			}
			withdrawPath := base + "/" + answer.Note.ID + "/withdraw"
			withdraw := func(from string) string {
				b, _ := json.Marshal(map[string]any{"source_conversation": from, "expected_version": answer.Note.Version, "reason": "The prerequisite is satisfied."})
				return string(b)
			}
			other := root
			if other == child {
				other = target
			}
			foreign := request(http.MethodPost, withdrawPath, withdraw(other), "foreign-child-note")
			if foreign.Code != http.StatusNotFound || codeOf(t, foreign) != "intervention_not_found" {
				t.Fatalf("foreign withdrawal: %d %s", foreign.Code, foreign.Body)
			}
			withdrawn := request(http.MethodPost, withdrawPath, withdraw(child), "withdraw-child-note")
			if withdrawn.Code != http.StatusOK || json.Unmarshal(withdrawn.Body.Bytes(), &answer) != nil || !answer.Note.Withdrawn || answer.Note.ReadAt != nil {
				t.Fatalf("child withdrawal: %d %s", withdrawn.Code, withdrawn.Body)
			}
			denied := request(http.MethodPost, "/v1/work/v2/agent/session-todos/"+child, todoBatch("child work"), "child-todos")
			if denied.Code != http.StatusConflict || codeOf(t, denied) != "child_session" {
				t.Fatalf("child gained Root privileges: %d %s", denied.Code, denied.Body)
			}
			for _, headers := range []map[string]string{nil, {"X-Clawdline-Orchestrator": "invalid"}} {
				denied := call{method: http.MethodPost, path: base, body: body, headers: headers}.do(handler)
				if denied.Code != http.StatusUnauthorized {
					t.Fatalf("credential gate: %d %s", denied.Code, denied.Body)
				}
			}
			denied = request(http.MethodGet, "/v1/work/v2/human-interventions/conversation:"+target, "", "")
			if denied.Code != http.StatusForbidden {
				t.Fatalf("machine read person route: %d %s", denied.Code, denied.Body)
			}
		})
	}
}

func TestNoteOperationsRequireOneLiveAssistantSource(t *testing.T) {
	for _, scenario := range []string{"malformed", "missing", "ambiguous", "unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			s, source, _ := humanInterventionTestServer(t)
			status, code := http.StatusNotFound, "session_not_found"
			switch scenario {
			case "malformed":
				source, status, code = "not-a-conversation", http.StatusBadRequest, "conversation_id_malformed"
			case "missing":
				source = "10000000-0000-4000-8000-000000000099"
			case "ambiguous":
				live := s.broker.Live(context.Background())
				live = append(live, live[0])
				s.broker.Live = func(context.Context) []session.Session { return live }
				status, code = http.StatusConflict, "conversation_ambiguous"
			case "unavailable":
				s.broker.Live = nil
				status, code = http.StatusConflict, "registry_stale"
			}
			base := "/v1/work/v2/agent/human-interventions"
			withdraw, _ := json.Marshal(map[string]any{"source_conversation": source, "expected_version": 1, "reason": "Obsolete"})
			for _, operation := range []struct{ method, path, body string }{
				{http.MethodPost, base, humanInterventionBody(source)},
				{http.MethodGet, base + "?source_conversation=" + source, ""},
				{http.MethodPost, base + "/10000000-0000-4000-8000-000000000009/withdraw", string(withdraw)},
			} {
				rec := httptest.NewRecorder()
				s.workV2Route(rec, agentWorkV2Request(operation.method, operation.path, operation.body, "invalid-source"))
				if rec.Code != status || codeOf(t, rec) != code {
					t.Fatalf("%s %s: %d %s", operation.method, operation.path, rec.Code, rec.Body)
				}
			}
			rows, _, err := s.humanInterventions().List(context.Background(), "10000000-0000-4000-8000-000000000002")
			if err != nil || len(rows) != 0 {
				t.Fatalf("invalid source wrote a note: %v %v", rows, err)
			}
		})
	}
}

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

func TestAgentWithdrawsOnlyOwnOpenNoteAndPersonCanReopen(t *testing.T) {
	s, source, target := humanInterventionTestServer(t)
	base := "/v1/work/v2/agent/human-interventions"
	created := httptest.NewRecorder()
	s.workV2Route(created, agentWorkV2Request(http.MethodPost, base, humanInterventionBody(source), "withdraw-create"))
	var result struct {
		Note humanInterventionWire `json:"note"`
	}
	if created.Code != 201 || json.Unmarshal(created.Body.Bytes(), &result) != nil {
		t.Fatalf("create: %d %s", created.Code, created.Body)
	}
	id := result.Note.ID
	post := func(from, reason string, version int64, key string) *httptest.ResponseRecorder {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"source_conversation": from, "reason": reason, "expected_version": version})
		r := httptest.NewRecorder()
		s.workV2Route(r, agentWorkV2Request(http.MethodPost, base+"/"+id+"/withdraw", string(b), key))
		return r
	}
	for _, tc := range []struct {
		from, reason string
		version      int64
		key          string
		status       int
	}{
		{target, "Obsolete", 1, "foreign-withdraw", 404},
		{source, " ", 1, "empty-withdraw", 422},
		{source, "Obsolete", 9, "stale-withdraw", 409},
		{"10000000-0000-4000-8000-000000000099", "Obsolete", 1, "dead-withdraw", 404},
	} {
		if r := post(tc.from, tc.reason, tc.version, tc.key); r.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.key, r.Code, r.Body)
		}
	}
	listed := httptest.NewRecorder()
	s.workV2Route(listed, agentWorkV2Request(http.MethodGet, base+"?source_conversation="+source, "", ""))
	var page struct {
		Rows []humanInterventionWire `json:"rows"`
	}
	if listed.Code != 200 || json.Unmarshal(listed.Body.Bytes(), &page) != nil || len(page.Rows) != 1 || page.Rows[0].ID != id {
		t.Fatalf("own list: %d %s", listed.Code, listed.Body)
	}
	other := httptest.NewRecorder()
	s.workV2Route(other, agentWorkV2Request(http.MethodGet, base+"?source_conversation="+target, "", ""))
	if other.Code != 200 || json.Unmarshal(other.Body.Bytes(), &page) != nil || len(page.Rows) != 0 {
		t.Fatalf("other list: %d %s", other.Code, other.Body)
	}
	withdrawn := post(source, "The prerequisite is satisfied.", 1, "withdraw-once")
	if withdrawn.Code != 200 || json.Unmarshal(withdrawn.Body.Bytes(), &result) != nil {
		t.Fatalf("withdraw: %d %s", withdrawn.Code, withdrawn.Body)
	}
	if !result.Note.Withdrawn || result.Note.ResolvedAt == nil || result.Note.ReadAt != nil || result.Note.Resolution != "The prerequisite is satisfied." {
		t.Fatalf("outcome: %+v", result.Note)
	}
	replay := post(source, "The prerequisite is satisfied.", 1, "withdraw-once")
	if replay.Code != 200 || replay.Body.String() != withdrawn.Body.String() {
		t.Fatalf("replay: %d %s", replay.Code, replay.Body)
	}
	if r := post(source, "Overwrite", result.Note.Version, "withdraw-closed"); r.Code != 422 {
		t.Fatalf("closed: %d %s", r.Code, r.Body)
	}
	stored, err := s.store.HumanIntervention(context.Background(), id)
	if err != nil || !stored.Withdrawn || stored.ResolvedAt == nil {
		t.Fatalf("stored: %+v %v", stored, err)
	}
	attention, err := s.store.OpenHumanInterventionCounts(context.Background())
	if err != nil || attention[target] != 0 {
		t.Fatalf("attention: %v %v", attention, err)
	}
	personPath := "/v1/work/v2/human-interventions/conversation:" + target + "/" + id
	reopened := httptest.NewRecorder()
	s.workV2Route(reopened, personWorkV2Request(http.MethodPost, personPath+"/reopen", fmt.Sprintf(`{"expected_version":%d}`, result.Note.Version), "withdraw-reopen"))
	result.Note = humanInterventionWire{}
	if reopened.Code != 200 || json.Unmarshal(reopened.Body.Bytes(), &result) != nil || result.Note.Withdrawn || result.Note.ResolvedAt != nil || result.Note.Resolution != "" {
		t.Fatalf("reopen: %d %s", reopened.Code, reopened.Body)
	}
	resolved := httptest.NewRecorder()
	s.workV2Route(resolved, personWorkV2Request(http.MethodPost, personPath+"/resolve", fmt.Sprintf(`{"expected_version":%d,"resolution":"Person handled it"}`, result.Note.Version), "person-resolve"))
	if resolved.Code != 200 || json.Unmarshal(resolved.Body.Bytes(), &result) != nil {
		t.Fatalf("resolve: %d %s", resolved.Code, resolved.Body)
	}
	if r := post(source, "Overwrite", result.Note.Version, "withdraw-person-outcome"); r.Code != 422 {
		t.Fatalf("person outcome: %d %s", r.Code, r.Body)
	}
}
