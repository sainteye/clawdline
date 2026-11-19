package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
	sb "github.com/sainteye/clawdline/internal/domain/sessionboard"
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

type boardCloseHost struct {
	*pane
	lead session.Session
}

func (h *boardCloseHost) Inventory(context.Context) (session.Inventory, error) {
	return session.Inventory{Complete: true, Sessions: []session.Session{h.s, h.lead}}, nil
}

func TestMessageBoardLeadClosesOnlyAnAuditedCurrentMember(t *testing.T) {
	s, target, _ := sessionItemServer(t)
	target.s.PID = os.Getpid()
	lead := target.s
	lead.ID = "pane-5"
	lead.ConversationID = "20000000-0000-4000-8000-000000000009"
	host := &boardCloseHost{pane: target, lead: lead}
	hosts := []ports.TerminalHost{host}
	s.terminals, s.inventory.Terminals, s.inventory.Screen = hosts, hosts, host
	project := projects.ProjectID(target.s.CWD)
	leadActor := sb.Actor{ID: string(lead.Assistant) + ":" + lead.ConversationID, Label: "Lead"}
	targetActor := sb.Actor{ID: string(target.s.Assistant) + ":" + target.s.ConversationID, Label: "Member"}
	change := func(actor sb.Actor, cmd sb.Command, key string) sb.Result {
		t.Helper()
		out, err := s.store.ChangeSessionBoard(t.Context(), project, actor, targetActor, cmd, key, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	create := func(name string) string {
		t.Helper()
		return change(leadActor, sb.Command{Action: "create", Name: name, Text: "Shared work"}, name).ID
	}
	board := create("First board")

	const base = "/v1/work/v2/agent/sessions/pane-4"
	audit := func(want int) agentCloseAudit {
		t.Helper()
		status, code, payload := agentCloseCode(t, s, http.MethodGet, base+"/closeability?session_id="+lead.ConversationID, "")
		if status != want {
			t.Fatalf("audit status = %d %s, want %d: %s", status, code, want, payload)
		}
		var got agentCloseAudit
		if err := json.Unmarshal(payload, &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	close := func(version string, wantStatus int, wantCode string) {
		t.Helper()
		request, err := json.Marshal(agentCloseRequest{SessionID: lead.ConversationID, ExpectedCloseabilityVersion: version})
		if err != nil {
			t.Fatal(err)
		}
		status, code, payload := agentCloseCode(t, s, http.MethodPost, base+"/close", string(request))
		if status != wantStatus || code != wantCode {
			t.Fatalf("close returned %d %s, want %d %s: %s", status, code, wantStatus, wantCode, payload)
		}
	}

	audit(http.StatusForbidden)
	change(targetActor, sb.Command{Action: "join", Board: board}, "")
	current := audit(http.StatusOK)
	if current.Authority != "board_lead" || current.State != "safe" || current.Version == "" {
		t.Fatalf("board Lead's audit: %+v", current)
	}
	close("", http.StatusConflict, "close_not_proven")
	close("stale-audit", http.StatusConflict, "close_not_proven")
	target.s.State, target.screen = session.StateWorking, "✻ Working…"
	close(current.Version, http.StatusConflict, "close_blocked")
	target.s.State, target.screen = session.StateIdle, ""

	change(leadActor, sb.Command{Action: "archive", Board: board}, "")
	close(current.Version, http.StatusForbidden, "close_not_yours")
	// A fresh active board restores authority until the role transfers.
	board = create("Second board")
	change(targetActor, sb.Command{Action: "join", Board: board}, "")
	current = audit(http.StatusOK)
	change(leadActor, sb.Command{Action: "transfer", Board: board, Target: targetActor.ID}, "")
	close(current.Version, http.StatusForbidden, "close_not_yours")
	if target.closeCalls != 0 || s.closes.count() != 0 {
		t.Fatal("a delegated refusal closed or scheduled a close")
	}

	newAuditPath := "/v1/work/v2/agent/sessions/pane-5"
	status, code, payload := agentCloseCode(t, s, http.MethodGet, newAuditPath+"/closeability?session_id="+target.s.ConversationID, "")
	var inherited agentCloseAudit
	if status != http.StatusOK || json.Unmarshal(payload, &inherited) != nil || inherited.Authority != "board_lead" || inherited.State != "safe" {
		t.Fatalf("transferred Lead: %d %s %s", status, code, payload)
	}
	request, err := json.Marshal(agentCloseRequest{SessionID: target.s.ConversationID, ExpectedCloseabilityVersion: inherited.Version})
	if err != nil {
		t.Fatal(err)
	}
	status, code, payload = agentCloseCode(t, s, http.MethodPost, newAuditPath+"/close", string(request))
	if status != http.StatusOK || target.closeCalls != 1 || s.closes.count() != 0 {
		t.Fatalf("safe delegated close: %d %s %s; close calls = %d", status, code, payload, target.closeCalls)
	}
}
