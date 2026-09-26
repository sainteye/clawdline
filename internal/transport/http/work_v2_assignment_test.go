package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	reportBody, err := json.Marshal(map[string]any{"expected_version": owned.Item.Version,
		"session_id": p.s.ConversationID, "role": "completion_report", "title": "Completion report",
		"body": "The unstable observation timestamp made an unchanged transcript look new."})
	if err != nil {
		t.Fatal(err)
	}
	report := httptest.NewRequest(http.MethodPost, "/v1/work/v2/agent/items/"+v.Item.ID+"/documents", bytes.NewReader(reportBody))
	report.Header.Set("Content-Type", "application/json")
	report.Header.Set("Idempotency-Key", "agent-completion-report")
	report = report.WithContext(context.WithValue(report.Context(), accessKey{}, access{machine: true,
		verdict: auth.Verdict{Allowed: true}}))
	reportAnswer := httptest.NewRecorder()
	s.workV2Route(reportAnswer, report)
	if reportAnswer.Code != http.StatusCreated {
		t.Fatalf("report: %d %s", reportAnswer.Code, reportAnswer.Body)
	}
	var reported struct {
		Item workV2ItemWire `json:"item"`
	}
	if err := json.Unmarshal(reportAnswer.Body.Bytes(), &reported); err != nil {
		t.Fatal(err)
	}
	if len(reported.Item.Documents) != 1 || reported.Item.Documents[0].Role != "completion_report" ||
		reported.Item.Documents[0].CreatedAt == 0 {
		t.Fatalf("report response = %+v", reported.Item.Documents)
	}
	owned.Item.Version = reported.Item.Version
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

func TestWorkV2ListCarriesStatusAndSearchToTheStore(t *testing.T) {
	s, _, v := workV2AssignmentServer(t, session.StateIdle)

	matching := httptest.NewRequest(http.MethodGet, "/v1/work/v2/items?status=open&q=QUEUED", nil)
	matched := httptest.NewRecorder()
	s.workV2Route(matched, matching)
	if matched.Code != http.StatusOK || !strings.Contains(matched.Body.String(), `"id":"`+v.Item.ID+`"`) {
		t.Fatalf("matching search: %d %s", matched.Code, matched.Body)
	}

	missing := httptest.NewRequest(http.MethodGet, "/v1/work/v2/items?status=open&q=absent", nil)
	notFound := httptest.NewRecorder()
	s.workV2Route(notFound, missing)
	if notFound.Code != http.StatusOK || !strings.Contains(notFound.Body.String(), `"rows":[]`) {
		t.Fatalf("empty search: %d %s", notFound.Code, notFound.Body)
	}

	invalid := httptest.NewRequest(http.MethodGet, "/v1/work/v2/items?status=closed", nil)
	refused := httptest.NewRecorder()
	s.workV2Route(refused, invalid)
	if refused.Code != http.StatusBadRequest || !strings.Contains(refused.Body.String(), `"error":"invalid_status"`) {
		t.Fatalf("invalid status: %d %s", refused.Code, refused.Body)
	}
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
	if got := p.done(); len(got) != 1 || !strings.HasPrefix(got[0], "send:") ||
		!strings.Contains(got[0], "completion_report") || !strings.Contains(got[0], "straightforward fix") ||
		!strings.Contains(got[0], "clawdline item phase "+v.Item.ID+" implementing") {
		t.Fatalf("idle Session brief = %v", got)
	}
}

func TestPersonCanRemindTheCurrentOwnerWithoutReassigningTheItem(t *testing.T) {
	s, p, v := workV2AssignmentServer(t, session.StateWorking)
	assigned, err := s.assignWorkV2(context.Background(), v.Item.ID, "local", v.Item.Version,
		"existing_session", p.s.ID, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.done(); len(got) != 0 {
		t.Fatalf("working Session received the assignment courtesy brief: %v", got)
	}

	path := "/v1/work/v2/items/" + v.Item.ID + "/remind"
	body := fmt.Sprintf(`{"expected_version":%d}`, assigned.Item.Version)
	request := func(key string) *http.Request {
		return personWorkV2Request(http.MethodPost, path, body, key)
	}
	rec := httptest.NewRecorder()
	s.workV2Route(rec, request("remind-current-owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("remind: %d %s", rec.Code, rec.Body)
	}
	if got := p.done(); len(got) != 1 || !strings.Contains(got[0], "Clawdline reminder for Board item "+v.Item.ID) ||
		!strings.Contains(got[0], v.Item.Title) {
		t.Fatalf("reminder delivery = %v", got)
	}

	replay := httptest.NewRecorder()
	s.workV2Route(replay, request("remind-current-owner"))
	if replay.Code != http.StatusOK || replay.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay: %d %s, headers=%v", replay.Code, replay.Body, replay.Header())
	}
	if got := p.done(); len(got) != 1 {
		t.Fatalf("idempotent replay typed a second reminder: %v", got)
	}

	current, err := s.workV2().Item(context.Background(), v.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	active := 0
	for _, assignment := range current.Assignments {
		if assignment.State == "active" {
			active++
		}
	}
	if current.Item.Version != assigned.Item.Version || current.Item.OwnerSession != p.s.ConversationID || active != 1 {
		t.Fatalf("reminder changed assignment state: item=%+v assignments=%+v", current.Item, current.Assignments)
	}
}

func TestReminderRefusesAnUnassignedItemAndAReusedTerminal(t *testing.T) {
	s, p, v := workV2AssignmentServer(t, session.StateWorking)
	path := "/v1/work/v2/items/" + v.Item.ID + "/remind"
	rec := httptest.NewRecorder()
	s.workV2Route(rec, personWorkV2Request(http.MethodPost, path,
		fmt.Sprintf(`{"expected_version":%d}`, v.Item.Version), "remind-unassigned"))
	if rec.Code != http.StatusConflict || codeOf(t, rec) != "item_unassigned" {
		t.Fatalf("unassigned reminder: %d %s", rec.Code, rec.Body)
	}

	assigned, err := s.assignWorkV2(context.Background(), v.Item.ID, "local", v.Item.Version,
		"existing_session", p.s.ID, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	p.s.ConversationID = "20000000-0000-4000-8000-000000000004"
	reused := httptest.NewRecorder()
	s.workV2Route(reused, personWorkV2Request(http.MethodPost, path,
		fmt.Sprintf(`{"expected_version":%d}`, assigned.Item.Version), "remind-reused-terminal"))
	if reused.Code != http.StatusConflict || codeOf(t, reused) != "assignment_session_changed" {
		t.Fatalf("reused terminal reminder: %d %s", reused.Code, reused.Body)
	}
	if got := p.done(); len(got) != 0 {
		t.Fatalf("reused terminal received another Session's reminder: %v", got)
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
	owned, err = s.workV2().AddDocument(context.Background(), v.Item.ID, app.AddDocumentV2{
		ExpectedVersion: owned.Item.Version, SessionID: p.s.ConversationID, Role: "completion_report",
		Title: "Completion report", Body: "The unstable observation timestamp made an unchanged transcript look new.",
	}, nil)
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
	if len(page.Recent) != 1 || page.Recent[0].ID != v.Item.ID || len(page.Recent[0].Documents) != 1 ||
		page.Recent[0].Documents[0].Body != "The unstable observation timestamp made an unchanged transcript look new." {
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

// The Board chooses which assistant a new Session runs. The choice reaches the
// assignment record, a blank one from an older console still means Codex, and
// an assistant this machine cannot open is refused before anything is written.
func TestANewSessionAssignmentRecordsTheChosenAssistant(t *testing.T) {
	for _, tc := range []struct{ sent, recorded string }{{"claude", "claude"}, {"codex", "codex"}, {"", "codex"}} {
		t.Run("sent="+tc.sent, func(t *testing.T) {
			s, _, v := workV2AssignmentServer(t, session.StateIdle)
			// This broker has no terminal to open a Session in, so the
			// assignment fails after the pending record names its assistant.
			_, _ = s.assignWorkV2(context.Background(), v.Item.ID, "local", v.Item.Version,
				"new_session", "", tc.sent, "", nil)
			after, err := s.workV2().Item(context.Background(), v.Item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(after.Assignments) != 1 || after.Assignments[0].Assistant != tc.recorded {
				t.Fatalf("assignments = %+v, want one naming %q", after.Assignments, tc.recorded)
			}
		})
	}

	s, _, v := workV2AssignmentServer(t, session.StateIdle)
	_, err := s.assignWorkV2(context.Background(), v.Item.ID, "local", v.Item.Version,
		"new_session", "", "gemini", "", nil)
	var refusal *app.WorkError
	if !errors.As(err, &refusal) || refusal.Code != "invalid_assistant" {
		t.Fatalf("unknown assistant: %v", err)
	}
	after, err := s.workV2().Item(context.Background(), v.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Assignments) != 0 || after.Item.Version != v.Item.Version {
		t.Fatalf("a refused assistant wrote %+v", after)
	}
}

// A Session that reaches for the edit route to move its item's phase is told
// by name where the phase moves, and the item is not touched; any other field
// the route does not take is named in the refusal.
func TestTheEditRouteNamesThePhaseRouteAndAnyFieldItRefuses(t *testing.T) {
	s, p, v := workV2AssignmentServer(t, session.StateWorking)
	assigned, err := s.assignWorkV2(context.Background(), v.Item.ID, "local", v.Item.Version,
		"existing_session", p.s.ID, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	edit := func(key string, fields map[string]any) *httptest.ResponseRecorder {
		fields["expected_version"], fields["session_id"] = assigned.Item.Version, p.s.ConversationID
		body, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPatch, "/v1/work/v2/agent/items/"+v.Item.ID+"/edit", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{
			machine: true, verdict: auth.Verdict{Allowed: true},
		}))
		rec := httptest.NewRecorder()
		s.workV2Route(rec, req)
		return rec
	}
	rec := edit("agent-edits-phase", map[string]any{"phase": "done"})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "phase_not_editable") ||
		!strings.Contains(rec.Body.String(), "/v1/work/v2/agent/items/"+v.Item.ID+"/phase") ||
		!strings.Contains(rec.Body.String(), "clawdline item phase") {
		t.Fatalf("phase edit: %d %s", rec.Code, rec.Body)
	}
	after, err := s.workV2().Item(context.Background(), v.Item.ID)
	if err != nil || after.Item.Phase != work.PhaseAssigned || after.Item.Version != assigned.Item.Version {
		t.Fatalf("item after a refused phase edit: %+v %v", after.Item, err)
	}
	rec = edit("agent-edits-bogus", map[string]any{"bogus": 1})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_request") ||
		!strings.Contains(rec.Body.String(), `\"bogus\"`) {
		t.Fatalf("unknown field edit: %d %s", rec.Code, rec.Body)
	}
}
