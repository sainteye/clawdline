package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
func TestVoiceWorkQuotaRetainsInputAndGETDoesNotRetry(t *testing.T) {
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
	if calls != 1 || len(state.Pending()) != 2 || state.PlannerError != "voice_work_quota" {
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
	if len(state.Tasks) != 0 || len(state.Pending()) != 1 {
		t.Fatal(state)
	}
}
