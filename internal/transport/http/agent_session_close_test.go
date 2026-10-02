package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
)

func agentCloseCode(t *testing.T, s *Server, method, path, body string) (int, string, []byte) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(method, path, body, ""))
	var refusal struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &refusal)
	return rec.Code, refusal.Error, rec.Body.Bytes()
}

func TestAnAgentCannotForceAClose(t *testing.T) {
	s, _ := ownTodoServer(t)
	status, code, _ := agentCloseCode(t, s, http.MethodPost, "/v1/work/v2/agent/sessions/pane-4/close",
		`{"session_id":"10000000-0000-4000-8000-000000000004","force":true}`)
	if status != http.StatusUnprocessableEntity || code != "force_refused" {
		t.Fatalf("force: %d %s", status, code)
	}
}

func TestAnAgentClosesOnlyItself(t *testing.T) {
	s, _ := ownTodoServer(t)
	status, code, _ := agentCloseCode(t, s, http.MethodGet,
		"/v1/work/v2/agent/sessions/pane-4/closeability?session_id=20000000-0000-4000-8000-000000000009", "")
	if status != http.StatusForbidden || code != "close_not_yours" {
		t.Fatalf("another Session's close: %d %s", status, code)
	}
	status, code, _ = agentCloseCode(t, s, http.MethodPost, "/v1/work/v2/agent/sessions/pane-4/close",
		`{"session_id":"20000000-0000-4000-8000-000000000009"}`)
	if status != http.StatusForbidden || code != "close_not_yours" {
		t.Fatalf("another Session's close: %d %s", status, code)
	}
}

func TestAnAgentCloseOfAGoneSessionSaysSo(t *testing.T) {
	s, _ := ownTodoServer(t)
	status, code, _ := agentCloseCode(t, s, http.MethodPost, "/v1/work/v2/agent/sessions/pane-99/close",
		`{"session_id":"10000000-0000-4000-8000-000000000004"}`)
	if status != http.StatusNotFound || code != "session_not_found" {
		t.Fatalf("gone: %d %s", status, code)
	}
}

func TestAnAgentReadsItsOwnCloseability(t *testing.T) {
	s, _ := ownTodoServer(t)
	status, _, body := agentCloseCode(t, s, http.MethodGet,
		"/v1/work/v2/agent/sessions/pane-4/closeability?session_id=10000000-0000-4000-8000-000000000004", "")
	if status != http.StatusOK {
		t.Fatalf("own closeability: %d %s", status, body)
	}
	var audit agentCloseAudit
	if err := json.Unmarshal(body, &audit); err != nil || audit.Authority != "self" || audit.State == "" {
		t.Fatalf("audit: %s", body)
	}
}

func TestAnAgentsCloseStateNeverReadsSafeOverWhatItAdded(t *testing.T) {
	safe := contract.CloseabilityStateSafe
	if got := agentCloseState(safe, nil); got != safe {
		t.Fatalf("nothing owed: %s", got)
	}
	owed := []contract.CloseReason{{Code: "completion_unacknowledged", Kind: "obligation"}}
	if got := agentCloseState(safe, owed); got != contract.CloseabilityStateBlocked {
		t.Fatalf("unacknowledged completion: %s", got)
	}
	unread := []contract.CloseReason{{Code: "completion_notices_unreadable", Kind: "evidence"}}
	if got := agentCloseState(safe, unread); got != contract.CloseabilityStateUnknown {
		t.Fatalf("unreadable ledger: %s", got)
	}
}

func ownCloseAudit(t *testing.T, s *Server) agentCloseAudit {
	t.Helper()
	status, _, body := agentCloseCode(t, s, http.MethodGet,
		"/v1/work/v2/agent/sessions/pane-4/closeability?session_id=10000000-0000-4000-8000-000000000004", "")
	if status != http.StatusOK {
		t.Fatalf("closeability: %d %s", status, body)
	}
	var audit agentCloseAudit
	if err := json.Unmarshal(body, &audit); err != nil {
		t.Fatal(err)
	}
	return audit
}

func hasCloseReason(audit agentCloseAudit, code, subject string) bool {
	for _, r := range audit.Reasons {
		if r.Code == code && r.SubjectID == subject {
			return true
		}
	}
	return false
}

// A running child, then its unacknowledged completion, each keeps its root
// from closing itself; neither is on the Session list's own projection.
func TestAnAgentCloseWaitsForItsChildrenAndTheirNotices(t *testing.T) {
	s, p := ownTodoServer(t)
	ctx := context.Background()
	const id = "c6f30000-0000-4000-8000-0000000000c1"
	r := orchestrator.Record{Protocol: orchestrator.Protocol, ID: id, Kind: "custom", Assistant: "claude",
		Title: "a child", ProjectDir: "/p", State: orchestrator.StateBriefed, CreatedAt: time.Now(),
		Root: &orchestrator.RootRef{SessionID: p.s.ConversationID, Assistant: "claude"}}
	body, _ := json.Marshal(r)
	if _, err := s.store.CreateBrokerTask(ctx, store.BrokerRow{ID: id, Project: "/p", Assistant: "claude",
		State: string(r.State), CreatedAt: r.CreatedAt, SecretHash: orchestrator.HashSecret("s"),
		Record: body}, nil); err != nil {
		t.Fatal(err)
	}
	audit := ownCloseAudit(t, s)
	if !hasCloseReason(audit, "child_task_running", id) || audit.State == string(contract.CloseabilityStateSafe) {
		t.Fatalf("a running child does not block: %+v", audit)
	}
	status, code, _ := agentCloseCode(t, s, http.MethodPost, "/v1/work/v2/agent/sessions/pane-4/close",
		`{"session_id":"10000000-0000-4000-8000-000000000004"}`)
	if status != http.StatusConflict || code == "" {
		t.Fatalf("a close past a running child: %d %s", status, code)
	}

	done, err := s.broker.Settle(ctx, id, orchestrator.StateSuccess, "done", &taskdir.Result{Status: "success", Summary: "done"})
	if err != nil || done.Notice == nil {
		t.Fatalf("settle: %v", err)
	}
	audit = ownCloseAudit(t, s)
	if hasCloseReason(audit, "child_task_running", id) || !hasCloseReason(audit, "completion_unacknowledged", id) ||
		audit.State == string(contract.CloseabilityStateSafe) {
		t.Fatalf("an unacknowledged completion does not block: %+v", audit)
	}
	if _, err := s.broker.Acknowledge(ctx, id, done.Notice.ID); err != nil {
		t.Fatal(err)
	}
	if audit = ownCloseAudit(t, s); hasCloseReason(audit, "completion_unacknowledged", id) {
		t.Fatalf("an acknowledged completion still blocks: %+v", audit)
	}
}

// A ledger row that cannot be decoded may be a child of this Session, so the
// close reads unknown and refuses rather than closing on a list it never saw.
func TestAnAgentCloseWithUnreadableChildrenIsUnknown(t *testing.T) {
	s, _ := ownTodoServer(t)
	const id = "c6f30000-0000-4000-8000-0000000000c2"
	if _, err := s.store.CreateBrokerTask(context.Background(), store.BrokerRow{ID: id, Project: "/p",
		Assistant: "claude", State: "briefed", CreatedAt: time.Now(), SecretHash: orchestrator.HashSecret("s"),
		Record: []byte(`{"not a record"`)}, nil); err != nil {
		t.Fatal(err)
	}
	audit := ownCloseAudit(t, s)
	if audit.State != string(contract.CloseabilityStateUnknown) || !hasCloseReason(audit, "child_tasks_unreadable", "") {
		t.Fatalf("unreadable children: %+v", audit)
	}
	status, code, _ := agentCloseCode(t, s, http.MethodPost, "/v1/work/v2/agent/sessions/pane-4/close",
		`{"session_id":"10000000-0000-4000-8000-000000000004"}`)
	if status != http.StatusConflict || code != "closeability_unknown" {
		t.Fatalf("a close on an unknown list: %d %s", status, code)
	}
}

// A tmux pane is named `%84`, and the command escapes it to `%2584` in the
// path. The route must read the name it was given, not its spelling: on
// 2026-10-02 a Feature Root asking about its own pane `%84` was told its
// Session was not on this machine.
func TestAnAgentClosesAPaneWhoseNameNeedsEscaping(t *testing.T) {
	s, p := ownTodoServer(t)
	p.s.ID = "%84"
	status, _, body := agentCloseCode(t, s, http.MethodGet,
		"/v1/work/v2/agent/sessions/"+url.PathEscape("%84")+"/closeability?session_id=10000000-0000-4000-8000-000000000004", "")
	if status != http.StatusOK {
		t.Fatalf("closeability of an escaped pane: %d %s", status, body)
	}
	var audit agentCloseAudit
	if err := json.Unmarshal(body, &audit); err != nil || audit.TerminalID != "%84" || audit.Authority != "self" {
		t.Fatalf("audit: %s", body)
	}
}
