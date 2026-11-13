package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/planner"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/copilot"
	"github.com/sainteye/clawdline/internal/domain/voicework"
)

func workServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &Server{cfg: config.Config{Dir: dir}, store: st}
}
func workCall(t *testing.T, s *Server, method, body, key string, send bool) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "/v1/voice/copilot-work", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", key)
	caps := auth.NewCaps(auth.Read)
	if send {
		caps = auth.NewCaps(auth.Read, auth.Send)
	}
	r = r.WithContext(context.WithValue(r.Context(), accessKey{}, access{verdict: auth.Verdict{Allowed: true, Caps: caps}}))
	w := httptest.NewRecorder()
	s.copilotWorkRoute(w, r)
	return w
}

func TestVoiceWorkReceivesBeforePlanningAndGETNeverCallsAModel(t *testing.T) {
	s := workServer(t)
	on := true
	_, err := s.changeCopilot(copilot.Change{WorkEnabled: &on})
	if err != nil {
		t.Fatal(err)
	}
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	was := voiceWorkPlan
	voiceWorkPlan = func(ctx context.Context, _, _ string) (planner.VoiceWorkPlan, error) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
		}
		close(finished)
		return planner.VoiceWorkPlan{Reply: "Recorded."}, nil
	}
	t.Cleanup(func() { voiceWorkPlan = was })
	if w := workCall(t, s, http.MethodGet, "", "", false); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	select {
	case <-started:
		t.Fatal("GET started a model")
	default:
	}
	w := workCall(t, s, http.MethodPost, `{"action":"input","id":"utterance-one","text":"Inspect the work"}`, "receipt-one", true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var answer struct {
		State voicework.State `json:"state"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &answer)
	if len(answer.State.Inputs) != 1 || answer.State.Inputs[0].Planned {
		t.Fatal("not received first")
	}
	workCall(t, s, http.MethodPost, `{"action":"plan","sessions":[],"input_ids":["utterance-one"]}`, "plan-one", true)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("planner not started")
	}
	close(release)
	<-finished
	deadline := time.Now().Add(time.Second)
	for {
		s.voiceWorkMu.Lock()
		running := s.voiceWorkRunning
		s.voiceWorkMu.Unlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("planner still running")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestVoiceWorkRequiresSendAndRejectsChangedIdempotencyBodies(t *testing.T) {
	s := workServer(t)
	body := `{"action":"input","id":"utterance","text":"Inspect the work"}`
	if w := workCall(t, s, http.MethodPost, body, "same", false); w.Code != 403 {
		t.Fatal(w.Code)
	}
	for i := 0; i < 2; i++ {
		if w := workCall(t, s, http.MethodPost, body, "same", true); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := workCall(t, s, http.MethodPost, strings.Replace(body, "Inspect", "Change", 1), "same", true); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	state, err := s.store.VoiceWork(context.Background())
	if err != nil || len(state.Inputs) != 1 {
		t.Fatalf("%+v %v", state, err)
	}
}

func waitVoicePlanner(t *testing.T, s *Server) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		s.voiceWorkMu.Lock()
		running := s.voiceWorkRunning
		s.voiceWorkMu.Unlock()
		if !running {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("planner did not stop")
		}
		time.Sleep(time.Millisecond)
	}
}
func TestVoiceWorkFailureIsIsolatedAndGETNeverRetries(t *testing.T) {
	s := workServer(t)
	on := true
	_, _ = s.changeCopilot(copilot.Change{WorkEnabled: &on})
	was := voiceWorkPlan
	calls := 0
	voiceWorkPlan = func(context.Context, string, string) (planner.VoiceWorkPlan, error) {
		calls++
		return planner.VoiceWorkPlan{}, planner.ErrOutOfQuota
	}
	defer func() { voiceWorkPlan = was }()
	w := workCall(t, s, http.MethodPost, `{"action":"input","id":"one","text":"Read progress","sessions":[]}`, "input", true)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	waitVoicePlanner(t, s)
	for i := 0; i < 3; i++ {
		workCall(t, s, http.MethodGet, "", "", false)
	}
	workCall(t, s, http.MethodPost, `{"action":"input","id":"two","text":"Also read another Session","sessions":[]}`, "second-input", true)
	waitVoicePlanner(t, s)
	state, _ := s.store.VoiceWork(context.Background())
	if calls != 2 || len(state.Inputs) != 2 || len(state.Pending()) != 2 || state.Inputs[0].PlanningError != "voice_work_quota" || state.Inputs[1].PlanningError != "voice_work_quota" {
		t.Fatalf("calls %d state %+v", calls, state)
	}
}
func TestVoiceWorkDisabledDuringPlanningCannotPublishAPlan(t *testing.T) {
	s := workServer(t)
	on := true
	_, _ = s.changeCopilot(copilot.Change{WorkEnabled: &on})
	was := voiceWorkPlan
	release, started := make(chan struct{}), make(chan struct{})
	voiceWorkPlan = func(context.Context, string, string) (planner.VoiceWorkPlan, error) {
		close(started)
		<-release
		return planner.VoiceWorkPlan{Changes: []voicework.Change{{Op: "add", ID: "one", Text: "List Sessions", Tool: "list_sessions", Args: map[string]any{}}}}, nil
	}
	defer func() { voiceWorkPlan = was }()
	workCall(t, s, http.MethodPost, `{"action":"input","id":"one","text":"List Sessions","sessions":[]}`, "input", true)
	<-started
	off := false
	_, _ = s.changeCopilot(copilot.Change{WorkEnabled: &off})
	close(release)
	waitVoicePlanner(t, s)
	state, _ := s.store.VoiceWork(context.Background())
	if len(state.Tasks) != 0 || len(state.Inputs) != 1 || state.Inputs[0].Planned {
		t.Fatal(state)
	}
}

func TestVoiceWorkDiscoveryFinishesIntoASecondBoundedPlanningTurn(t *testing.T) {
	s := workServer(t)
	on := true
	_, _ = s.changeCopilot(copilot.Change{WorkEnabled: &on})
	was := voiceWorkPlan
	calls := 0
	voiceWorkPlan = func(_ context.Context, raw, _ string) (planner.VoiceWorkPlan, error) {
		calls++
		if calls == 1 {
			return planner.VoiceWorkPlan{Changes: []voicework.Change{{Op: "add", ID: "discover", Text: "Find the requested project", Tool: "list_projects", Args: map[string]any{}}}, WaitFor: []string{"discover"}}, nil
		}
		var contextData struct {
			OriginalInputs []voicework.Input `json:"original_inputs"`
			Tasks          []voicework.Task  `json:"tasks"`
			Contracts      json.RawMessage   `json:"tool_contracts"`
		}
		if err := json.Unmarshal([]byte(raw), &contextData); err != nil {
			t.Error(err)
		}
		if len(contextData.OriginalInputs) != 1 || contextData.OriginalInputs[0].Text != "Start the sample project" || len(contextData.Tasks) != 1 || !strings.Contains(contextData.Tasks[0].Result, "project-exact") || len(contextData.Contracts) == 0 {
			t.Errorf("continuation lost the goal, discovery or contracts: %s", raw)
		}
		return planner.VoiceWorkPlan{Changes: []voicework.Change{{Op: "add", ID: "start", Text: "Start the sample project", Tool: "start_session", Args: map[string]any{"project_id": "project-exact", "project_label": "Sample", "assistant": "codex"}}}}, nil
	}
	t.Cleanup(func() { voiceWorkPlan = was })
	w := workCall(t, s, "POST", `{"action":"input","id":"goal","text":"Start the sample project","sessions":[],"tool_contracts":[{"name":"start_session","parameters":{"required":["project_id","project_label","assistant"]}}]}`, "receive-goal", true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	waitVoicePlanner(t, s)
	claim := workCall(t, s, "POST", `{"action":"claim","runner":"browser","executor_version":2,"tools":["list_projects"]}`, "claim-discovery", true)
	var answer struct {
		Claimed *voicework.Task `json:"claimed"`
	}
	_ = json.Unmarshal(claim.Body.Bytes(), &answer)
	if claim.Code != 200 || answer.Claimed == nil {
		t.Fatal(claim.Code, claim.Body.String())
	}
	body, _ := json.Marshal(map[string]any{"action": "finish", "runner": "browser", "id": answer.Claimed.ID, "key": answer.Claimed.ActionKey, "result": `{"projects":[{"id":"project-exact","label":"Sample"}]}`})
	finished := workCall(t, s, "POST", string(body), "finish-discovery", true)
	if finished.Code != 200 {
		t.Fatal(finished.Code, finished.Body.String())
	}
	waitVoicePlanner(t, s)
	state, _ := s.store.VoiceWork(context.Background())
	if calls != 2 || len(state.Tasks) != 2 || state.Tasks[1].Status != "waiting_confirmation" || state.Tasks[1].Args["project_id"] != "project-exact" || len(state.Pending()) != 0 {
		t.Fatalf("not safely continued: calls=%d state=%+v", calls, state)
	}
	_ = workCall(t, s, "GET", "", "", false)
	if calls != 2 {
		t.Fatal("GET retried a model")
	}
}

func TestVoiceWorkQuestionApprovalUsesTheOriginalDirectoryQuestion(t *testing.T) {
	s := workServer(t)
	on := true
	_, _ = s.changeCopilot(copilot.Change{WorkEnabled: &on})
	was := voiceWorkPlan
	voiceWorkPlan = func(context.Context, string, string) (planner.VoiceWorkPlan, error) {
		return planner.VoiceWorkPlan{Changes: []voicework.Change{{Op: "add", ID: "answer", Text: "Choose Yes", Tool: "answer_session_question", Args: map[string]any{"session_id": "one", "session_label": "Sample", "choice": "1", "expected_question": "model supplied", "_voice_question": "model supplied"}}}}, nil
	}
	t.Cleanup(func() { voiceWorkPlan = was })
	fingerprint := strings.Repeat("a", 64)
	body, _ := json.Marshal(map[string]any{"action": "input", "id": "a", "text": "Choose the first option", "sessions": []copilotDecisionSession{{ID: "one", Label: "Sample", Conversation: "conversation", Question: "Run the requested check?", MenuFingerprint: fingerprint, Options: []copilotDecisionOption{{N: 1, Label: "Yes"}, {N: 2, Label: "No"}}}}})
	w := workCall(t, s, "POST", string(body), "original-question", true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	waitVoicePlanner(t, s)
	state, _ := s.store.VoiceWork(context.Background())
	if len(state.Tasks) != 1 || state.Tasks[0].Args["expected_question"] != fingerprint || state.Tasks[0].Args["choice"] != "1" || state.Tasks[0].Status != "waiting_confirmation" {
		t.Fatal(state)
	}
	q := state.Tasks[0].Args["_voice_question"].(map[string]any)
	if q["question"] != "Run the requested check?" || q["option"] != "Yes" {
		t.Fatal(q)
	}
	old := workCall(t, s, "POST", `{"action":"claim","runner":"old-browser"}`, "old-executor", true)
	if old.Code != 400 {
		t.Fatal("old executor can claim sealed tasks", old.Code)
	}
}

func TestVoiceWorkContinuationStopsBeforeAFifthModelTurn(t *testing.T) {
	s := workServer(t)
	on := true
	_, _ = s.changeCopilot(copilot.Change{WorkEnabled: &on})
	_, _ = s.store.UpdateVoiceWork(context.Background(), func(state *voicework.State) error {
		_, err := state.AddInput(voicework.Input{ID: "goal", Text: "Find the project", Ready: true})
		state.Inputs[0].PlanRounds = voicework.MaxPlanRounds
		return err
	})
	was := voiceWorkPlan
	called := false
	voiceWorkPlan = func(context.Context, string, string) (planner.VoiceWorkPlan, error) {
		called = true
		return planner.VoiceWorkPlan{}, nil
	}
	t.Cleanup(func() { voiceWorkPlan = was })
	s.beginVoiceWork([]copilotDecisionSession{})
	waitVoicePlanner(t, s)
	state, _ := s.store.VoiceWork(context.Background())
	if called || state.PlannerError != "voice_work_continuation_limit" || len(state.Inputs) != 1 || state.Inputs[0].Planned || state.Inputs[0].PlanningError != "voice_work_continuation_limit" {
		t.Fatal(called, state)
	}
}

func TestVoiceConversationFocusIsSavedWithoutAPlanningTurn(t *testing.T) {
	s := workServer(t)
	on := true
	_, _ = s.changeCopilot(copilot.Change{WorkEnabled: &on})
	original := voiceWorkPlan
	defer func() { voiceWorkPlan = original }()
	calls := 0
	voiceWorkPlan = func(_ context.Context, raw, _ string) (planner.VoiceWorkPlan, error) {
		calls++
		var input struct {
			Inputs []voicework.Input `json:"inputs"`
		}
		if err := json.Unmarshal([]byte(raw), &input); err != nil {
			t.Fatal(err)
		}
		if len(input.Inputs) != 1 || input.Inputs[0].Focus == nil || input.Inputs[0].Focus.ID != "session-a" {
			t.Error(raw)
		}
		return planner.VoiceWorkPlan{Reply: "Recorded."}, nil
	}
	body := `{"action":"input","id":"conversation","call":"call-a","text":"Do this later","deferred":true,"focus":{"id":"session-a","label":"Review","machine":"machine-a","conversation":"conversation-a"}}`
	if w := workCall(t, s, http.MethodPost, body, "save", true); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if calls != 0 {
		t.Fatal("saving conversation called a model")
	}
	get := workCall(t, s, http.MethodGet, "", "", false)
	if !strings.Contains(get.Body.String(), `"protocol":3`) {
		t.Fatal(get.Body.String())
	}
	if w := workCall(t, s, http.MethodPost, `{"action":"answer","id":"conversation","call":"wrong"}`, "wrong", true); w.Code != 409 {
		t.Fatal(w.Code)
	}
	plan := `{"action":"plan","input_ids":["conversation"],"sessions":[{"id":"session-a","label":"Review","machine":"machine-a","conversation":"conversation-a"}]}`
	if w := workCall(t, s, http.MethodPost, plan, "queue", true); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	waitVoicePlanner(t, s)
	if calls != 1 {
		t.Fatal(calls)
	}
}

func TestVoiceWorkRejectsChangedFocusBeforeCallingAProvider(t *testing.T) {
	s := workServer(t)
	on := true
	_, _ = s.changeCopilot(copilot.Change{WorkEnabled: &on})
	original := voiceWorkPlan
	defer func() { voiceWorkPlan = original }()
	voiceWorkPlan = func(context.Context, string, string) (planner.VoiceWorkPlan, error) {
		t.Error("stale focus called provider")
		return planner.VoiceWorkPlan{}, nil
	}
	workCall(t, s, http.MethodPost, `{"action":"input","id":"request","text":"Read this","focus":{"id":"same-pane","conversation":"old"}}`, "save", true)
	workCall(t, s, http.MethodPost, `{"action":"plan","input_ids":["request"],"sessions":[{"id":"same-pane","conversation":"replacement"}]}`, "plan", true)
	waitVoicePlanner(t, s)
	state, _ := s.store.VoiceWork(context.Background())
	if state.Inputs[0].PlanningError != "voice_work_focus_changed" {
		t.Fatal(state)
	}
}

func TestVoiceWorkCannotRetryAProviderWhenFailurePersistenceIsBroken(t *testing.T) {
	s := workServer(t)
	on := true
	_, _ = s.changeCopilot(copilot.Change{WorkEnabled: &on})
	original := voiceWorkPlan
	defer func() { voiceWorkPlan = original }()
	started, release := make(chan struct{}), make(chan struct{})
	calls := 0
	voiceWorkPlan = func(context.Context, string, string) (planner.VoiceWorkPlan, error) {
		calls++
		if calls == 1 {
			close(started)
			<-release
		}
		return planner.VoiceWorkPlan{}, errors.New("synthetic provider failure")
	}
	workCall(t, s, http.MethodPost, `{"action":"input","id":"one","text":"Read a Session","sessions":[]}`, "input", true)
	<-started
	db, err := sql.Open("sqlite", filepath.Join(s.cfg.Dir, store.DBFile))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TRIGGER refuse_voice_update BEFORE UPDATE ON voice_assistant_workspace BEGIN SELECT RAISE(ABORT,'synthetic write failure'); END`); err != nil {
		t.Fatal(err)
	}
	close(release)
	waitVoicePlanner(t, s)
	if calls != 1 {
		t.Fatalf("provider was retried %d times", calls)
	}
}

func TestVoiceMessagePreparationAndSpokenConfirmationNeedNoPlanningProvider(t *testing.T) {
	s := workServer(t)
	on := true
	if _, err := s.changeCopilot(copilot.Change{WorkEnabled: &on}); err != nil {
		t.Fatal(err)
	}
	was := voiceWorkPlan
	voiceWorkPlan = func(context.Context, string, string) (planner.VoiceWorkPlan, error) {
		t.Error("message preparation started a model")
		return planner.VoiceWorkPlan{}, errors.New("unexpected provider")
	}
	t.Cleanup(func() { voiceWorkPlan = was })
	if w := workCall(t, s, http.MethodPost, `{"action":"input","id":"request","call":"call","text":"Tell the Session the requirement","deferred":true}`, "save-request", true); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	proposal := `{"action":"prepare_message","id":"recipient","call":"call","text":"Please inspect the delay.","input_ids":["request"],"sessions":[{"id":"recipient","label":"Recipient","conversation":"conversation","machine":"machine"}]}`
	if w := workCall(t, s, http.MethodPost, proposal, "forbidden", false); w.Code != 403 {
		t.Fatal(w.Code)
	}
	w := workCall(t, s, http.MethodPost, proposal, "prepare-once", true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var answer struct {
		Prepared            *voicework.Task `json:"prepared"`
		MessageConfirmation bool            `json:"message_confirmation"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &answer); err != nil || answer.Prepared == nil || !answer.MessageConfirmation {
		t.Fatal(err, w.Body.String())
	}
	if answer.Prepared.Status != "waiting_confirmation" {
		t.Fatal(answer.Prepared)
	}
	again := workCall(t, s, http.MethodPost, proposal, "prepare-once", true)
	if again.Body.String() != w.Body.String() {
		t.Fatal("idempotent preparation changed")
	}
	if w := workCall(t, s, http.MethodPost, `{"action":"input","id":"consent","call":"call","text":"可以，送出","deferred":true}`, "save-consent", true); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	body, _ := json.Marshal(map[string]any{"action": "confirm_message", "id": answer.Prepared.ID, "version": answer.Prepared.Version, "call": "call", "input_ids": []string{"consent"}})
	if w := workCall(t, s, http.MethodPost, string(body), "confirm-once", true); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	state, err := s.store.VoiceWork(context.Background())
	if err != nil || len(state.Tasks) != 1 || state.Tasks[0].ApprovedVersion != state.Tasks[0].Version || state.Tasks[0].ConfirmedInput != "consent" {
		t.Fatal(err, state)
	}
}
