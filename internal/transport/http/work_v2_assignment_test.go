package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/icon"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

func TestCompletingAnAssignedItemRecordsAndSendsItsNotification(t *testing.T) {
	s, p, v := workV2AssignmentServer(t, session.StateWorking)
	owned, err := s.assignWorkV2(context.Background(), v.Item.ID, "local", v.Item.Version,
		"existing_session", p.s.ID, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	advance := func(next work.Phase, verification string, landing *app.VerifiedLandingV2, deployment string) {
		t.Helper()
		owned, err = s.workV2().Advance(context.Background(), v.Item.ID, app.AdvanceWorkV2{ExpectedVersion: owned.Item.Version,
			SessionID: p.s.ConversationID, Next: next, Verification: verification, Landing: landing,
			Deployment: deployment, Actor: p.s.ConversationID}, nil)
		if err != nil {
			t.Fatalf("advance %s: %v", next, err)
		}
	}
	advance(work.PhaseImplementing, "", nil, "")
	advance(work.PhaseVerifying, "", nil, "")
	advance(work.PhaseMerging, "verified", nil, "")
	advance(work.PhaseDeploying, "", &app.VerifiedLandingV2{Commit: strings.Repeat("a", 40), Target: "main",
		TargetCommit: strings.Repeat("b", 40), Remote: "origin", RemoteCommit: strings.Repeat("c", 40)}, "")

	var pushed orchestrator.WorkItemCompletedPush
	s.broker.Push = func(_ context.Context, title, body, terminal, tag string) (int, int, error) {
		pushed = orchestrator.WorkItemCompletedPush{WorkID: v.Item.ID, Terminal: terminal, Title: title, Body: body, Tag: tag}
		return 1, 0, nil
	}
	body, _ := json.Marshal(map[string]any{"expected_version": owned.Item.Version, "session_id": p.s.ConversationID,
		"next": "done", "deployment": "production receipt"})
	req := httptest.NewRequest(http.MethodPost, "/v1/work/v2/agent/items/"+v.Item.ID+"/phase", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "complete-and-notify")
	req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{machine: true, verdict: auth.Verdict{Allowed: true}}))
	rec := httptest.NewRecorder()
	s.workV2Route(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", rec.Code, rec.Body)
	}
	var answer struct {
		Item workV2ItemWire `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Item.ClosedAt == nil || pushed.Terminal != p.s.ID || pushed.Title != "看板項目已完成" ||
		pushed.Body != "「Queued assignment」已完成" || pushed.Tag != "work-item-"+v.Item.ID {
		t.Fatalf("answer/push: %+v %+v", answer.Item, pushed)
	}
	effects, err := s.store.Effects(context.Background(), orchestrator.EffectWorkItemCompletedPush, v.Item.ID)
	if err != nil || len(effects) != 1 || effects[0].State != "done" || effects[0].Outcome != "pushed" {
		t.Fatalf("effect: %+v %v", effects, err)
	}
}

func TestTheOwningAgentNamesTheActionItNeedsFromThePerson(t *testing.T) {
	s, p, v := workV2AssignmentServer(t, session.StateWorking)
	assigned, err := s.assignWorkV2(context.Background(), v.Item.ID, "local", v.Item.Version,
		"existing_session", p.s.ID, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	action := "Confirm the release window."
	body, err := json.Marshal(map[string]any{
		"expected_version": assigned.Item.Version,
		"session_id":       p.s.ConversationID,
		"condition":        "waiting_user",
		"user_action":      action,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/v1/work/v2/agent/items/"+v.Item.ID+"/edit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "agent-needs-release-confirmation")
	req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{
		machine: true, verdict: auth.Verdict{Allowed: true},
	}))
	rec := httptest.NewRecorder()
	s.workV2Route(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("edit: %d %s", rec.Code, rec.Body)
	}
	var answer struct {
		Item workV2ItemWire `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Item.Condition == nil || *answer.Item.Condition != "waiting_user" || answer.Item.UserAction != action {
		t.Fatalf("edited item = %+v", answer.Item)
	}
}

func workV2AssignmentServer(t *testing.T, state session.State) (*Server, *pane, app.WorkV2View) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	project, ok := projects.CanonicalProjectKey(cwd)
	if !ok {
		t.Fatalf("test checkout has no canonical Project: %s", cwd)
	}
	p := &pane{s: session.Session{ID: "%4", Backend: session.BackendTmux, Assistant: session.AssistantCodex,
		CWD: cwd, ConversationID: "10000000-0000-4000-8000-000000000004", State: state}}
	s := paneServer(t, p)
	s.icons = icon.NewRegistry()
	v, err := s.workV2().Create(context.Background(), app.NewWorkV2{ProjectID: "project-1", ProjectPath: project,
		Kind: work.KindFeature, Title: "Queued assignment", Description: "Wait for the current turn to finish.", Actor: "local"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s, p, v
}

func TestOnlyAnIdleSessionReceivesAnAssignmentBrief(t *testing.T) {
	for _, state := range []session.State{session.StateWorking, session.StateWaiting, session.StateUnknown} {
		t.Run(string(state), func(t *testing.T) {
			s, p, v := workV2AssignmentServer(t, state)
			assigned, err := s.assignWorkV2(context.Background(), v.Item.ID, "local", v.Item.Version,
				"existing_session", p.s.ID, "", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			if assigned.Item.OwnerSession != p.s.ConversationID || assigned.Item.Phase != work.PhaseAssigned {
				t.Fatalf("assignment = %+v", assigned.Item)
			}
			if got := p.done(); len(got) != 0 {
				t.Fatalf("a %s Session was interrupted: %v", state, got)
			}
		})
	}

	s, p, v := workV2AssignmentServer(t, session.StateIdle)
	if _, err := s.assignWorkV2(context.Background(), v.Item.ID, "local", v.Item.Version,
		"existing_session", p.s.ID, "", "", nil); err != nil {
		t.Fatal(err)
	}
	if got := p.done(); len(got) != 1 || !strings.HasPrefix(got[0], "send:") {
		t.Fatalf("idle Session brief = %v", got)
	}
}

func TestAnAssignedItemIsInTheAgentsTodoRead(t *testing.T) {
	s, p, v := workV2AssignmentServer(t, session.StateWorking)
	assigned, err := s.assignWorkV2(context.Background(), v.Item.ID, "local", v.Item.Version,
		"existing_session", p.s.ID, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	waiting := work.ConditionWaitingUser
	action := "Confirm the release window."
	if _, err := s.workV2().Edit(context.Background(), v.Item.ID, app.EditWorkV2{ExpectedVersion: assigned.Item.Version,
		Condition: &waiting, UserAction: &action, Actor: p.s.ConversationID, OwnerSession: p.s.ConversationID}, nil); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet,
		"/v1/work/v2/agent/session-todos/"+p.s.ConversationID, nil)
	req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{
		machine: true, verdict: auth.Verdict{Allowed: true},
	}))
	rec := httptest.NewRecorder()
	s.workV2Route(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("read: %d %s", rec.Code, rec.Body)
	}
	var body struct {
		Assigned []workV2ItemWire `json:"assigned_items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Assigned) != 1 || body.Assigned[0].ID != v.Item.ID || body.Assigned[0].UserAction != action {
		t.Fatalf("assigned items = %+v; body = %s", body.Assigned, rec.Body)
	}
}

func TestAnAgentSeesAndCanRetractItsRecentCompletion(t *testing.T) {
	s, p, v := workV2AssignmentServer(t, session.StateWorking)
	owned, err := s.workV2().Assign(context.Background(), v.Item.ID, app.AssignWorkV2{ExpectedVersion: v.Item.Version,
		Mode: "existing_session", SessionID: p.s.ConversationID, TerminalID: p.s.ID, Assistant: "codex",
		Model: "default", Actor: "local"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	advance := func(next work.Phase, verification string, landing *app.VerifiedLandingV2, deployment string) {
		t.Helper()
		owned, err = s.workV2().Advance(context.Background(), v.Item.ID, app.AdvanceWorkV2{ExpectedVersion: owned.Item.Version,
			SessionID: p.s.ConversationID, Next: next, Verification: verification, Landing: landing,
			Deployment: deployment, Actor: p.s.ConversationID}, nil)
		if err != nil {
			t.Fatalf("advance to %s: %v", next, err)
		}
	}
	advance(work.PhaseImplementing, "", nil, "")
	advance(work.PhaseVerifying, "", nil, "")
	advance(work.PhaseMerging, "tests passed", nil, "")
	advance(work.PhaseDeploying, "", &app.VerifiedLandingV2{Commit: strings.Repeat("a", 40), Target: "main",
		TargetCommit: strings.Repeat("b", 40), Remote: "origin", RemoteCommit: strings.Repeat("c", 40)}, "")
	advance(work.PhaseDone, "", nil, "deployed")

	read := httptest.NewRequest(http.MethodGet, "/v1/work/v2/agent/session-todos/"+p.s.ConversationID, nil)
	read = read.WithContext(context.WithValue(read.Context(), accessKey{}, access{machine: true, verdict: auth.Verdict{Allowed: true}}))
	readAnswer := httptest.NewRecorder()
	s.workV2Route(readAnswer, read)
	if readAnswer.Code != http.StatusOK {
		t.Fatalf("read: %d %s", readAnswer.Code, readAnswer.Body)
	}
	var page struct {
		Recent []workV2ItemWire `json:"recent_items"`
	}
	if err := json.Unmarshal(readAnswer.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Recent) != 1 || page.Recent[0].ID != v.Item.ID {
		t.Fatalf("recent items = %+v; body = %s", page.Recent, readAnswer.Body)
	}

	body, err := json.Marshal(map[string]any{"expected_version": owned.Item.Version,
		"session_id": p.s.ConversationID, "reason": "The user's reported behavior is still broken."})
	if err != nil {
		t.Fatal(err)
	}
	reopen := httptest.NewRequest(http.MethodPost, "/v1/work/v2/agent/items/"+v.Item.ID+"/reopen", bytes.NewReader(body))
	reopen.Header.Set("Content-Type", "application/json")
	reopen.Header.Set("Idempotency-Key", "agent-retracts-mistaken-completion")
	reopen = reopen.WithContext(context.WithValue(reopen.Context(), accessKey{}, access{machine: true, verdict: auth.Verdict{Allowed: true}}))
	reopenAnswer := httptest.NewRecorder()
	s.workV2Route(reopenAnswer, reopen)
	if reopenAnswer.Code != http.StatusOK {
		t.Fatalf("reopen: %d %s", reopenAnswer.Code, reopenAnswer.Body)
	}
	var answer struct {
		Item workV2ItemWire `json:"item"`
	}
	if err := json.Unmarshal(reopenAnswer.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Item.Phase != "implementing" || answer.Item.OwnerSession == nil ||
		*answer.Item.OwnerSession != p.s.ConversationID || answer.Item.Cycle != 2 {
		t.Fatalf("reopened item = %+v", answer.Item)
	}
}
