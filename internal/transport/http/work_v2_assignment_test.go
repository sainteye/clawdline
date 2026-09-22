package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/icon"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

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
	if _, err := s.assignWorkV2(context.Background(), v.Item.ID, "local", v.Item.Version,
		"existing_session", p.s.ID, "", "", nil); err != nil {
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
	if len(body.Assigned) != 1 || body.Assigned[0].ID != v.Item.ID {
		t.Fatalf("assigned items = %+v; body = %s", body.Assigned, rec.Body)
	}
}
