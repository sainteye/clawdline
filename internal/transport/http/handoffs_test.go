package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// asJSON is a value as the generic JSON a client decodes.
func asJSON(t *testing.T, v any) map[string]any {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]any{}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// The broker's hand-over types reach the wire through their contract types,
// and nothing is lost on the way: every field set on the broker's side comes
// out under the same name (DG-5: one spelling, the contract's).
func TestTheHandOverTypesAreTheContracts(t *testing.T) {
	window := int64(120000)
	opened := &orchestratorOpened{TerminalID: "%1", Backend: "tmux", OpenedAt: 3, AutoCompactWindow: &window}
	h := orchestrator.Handoff{ID: "h", State: "delivered", ProjectDir: "/p", Dir: "/d", Title: "t", FromSession: "s",
		FromTerm: "%0", Plain: true, Assistant: "claude", Model: "haiku", CreatedAt: 1, TypeAttemptedAt: 2,
		DeliveredAt: 2, Failure: "f", Receipt: "r"}
	raw, _ := json.Marshal(h)
	var withOpened map[string]any
	_ = json.Unmarshal(raw, &withOpened)
	withOpened["opened"] = asJSON(t, opened)
	body, _ := json.Marshal(withOpened)
	_ = json.Unmarshal(body, &h)
	if got, want := asJSON(t, wireHandoff(h)), asJSON(t, h); !reflect.DeepEqual(got, want) {
		t.Fatalf("handoff:\n got %v\nwant %v", got, want)
	}
	a := orchestrator.RootAssignment{ID: "i", RequestID: "q", Assistant: "claude", Model: "m", ProjectDir: "/p",
		Label: "l", State: "briefed", Assignment: orchestrator.Assignment{Objective: "o", Scope: "s", Constraints: "c",
			RelevantReferences: "r", Acceptance: "a"}, CreatedAt: 1, Ownership: "independent_root", BriefPath: "/b",
		BriefAttemptedAt: 2, BriefedAt: 3, Failure: "f", Persona: "architect",
		Closure: &orchestrator.RootAssignmentClosure{TerminalID: "%2", Method: "close", Safe: true, ClosedAt: 4},
		Cleanup: &orchestrator.RootCleanupEligibility{Completion: true, Owner: "unknown", Scratch: "unregistered",
			Preservation: "unverified", Reason: "owner_unknown", NextOwner: "broker"}}
	if got, want := asJSON(t, wireAssignment(a)), asJSON(t, a); !reflect.DeepEqual(got, want) {
		t.Fatalf("root assignment:\n got %v\nwant %v", got, want)
	}
	g := orchestrator.GraphView{ID: "g", Destination: "d", Frontier: []string{"a"}, Conflicts: []string{"x"},
		Nodes: []orchestrator.GraphNodeView{{GraphNode: orchestrator.GraphNode{ID: "a", Title: "t", Kind: "review",
			DependsOn: []string{"b"}, Acceptance: []string{"x"}}, State: "ready", Task: "k"}}}
	var wire contract.BrokerGraph
	recast(g, &wire)
	if got, want := asJSON(t, wire), asJSON(t, g); !reflect.DeepEqual(got, want) {
		t.Fatalf("graph:\n got %v\nwant %v", got, want)
	}
	// The control: a contract type missing a field the broker sends is
	// caught by the same comparison.
	var short struct {
		ID string `json:"handoff_id"`
	}
	recast(h, &short)
	if reflect.DeepEqual(asJSON(t, short), asJSON(t, h)) {
		t.Fatal("the comparison cannot tell a lost field")
	}
}

type orchestratorOpened struct {
	TerminalID string `json:"terminal_id"`
	Backend    string `json:"backend"`
	OpenedAt   int64  `json:"opened_at"`
	// The window a session was opened with (orchestrator compact.go).
	AutoCompactWindow *int64 `json:"auto_compact_window"`
}

func TestRootAssignmentReadProjectsIndependentCleanupGates(t *testing.T) {
	s := paneServer(t, &pane{s: session.Session{ID: "%8", Backend: session.BackendTmux,
		Assistant: session.AssistantCodex, State: session.StateIdle}})
	ctx := context.Background()
	a := orchestrator.RootAssignment{ID: "10000000-0000-4000-8000-000000000001", State: orchestrator.AssignmentBriefed,
		Assistant: "codex", ProjectDir: t.TempDir(), BriefPath: s.broker.AssignmentRoot() + "/brief/ASSIGNMENT.md",
		Closure: &orchestrator.RootAssignmentClosure{TerminalID: "%4", Method: "close", Safe: true, ClosedAt: 4}}
	withExecutor := asJSON(t, a)
	withExecutor["executor"] = orchestratorOpened{TerminalID: "%4", Backend: "tmux", OpenedAt: 1}
	executorBody, _ := json.Marshal(withExecutor)
	if err := json.Unmarshal(executorBody, &a); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(a)
	if err := s.store.CreateOpened(ctx, store.TableRootAssignments, store.Opened{ID: a.ID, State: a.State,
		Record: body, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil); err != nil {
		t.Fatal(err)
	}
	s.broker.Reading = func(context.Context) session.Inventory {
		return session.Inventory{Complete: true, Sessions: []session.Session{{ID: "%8"}}, Sources: map[string]bool{"tmux": true}}
	}
	s.broker.ProcessCWDs = func(context.Context) ([]string, error) { return nil, nil }
	req := httptest.NewRequest(http.MethodGet, "/v1/orchestrator/root-assignments/"+a.ID, nil)
	req = req.WithContext(context.WithValue(ctx, accessKey{}, access{machine: true}))
	rec := httptest.NewRecorder()
	s.rootAssignmentsRoute(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("read: %d %s", rec.Code, rec.Body.String())
	}
	var got contract.BrokerRootAssignmentEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.RootAssignment.Closure == nil || got.RootAssignment.Cleanup == nil ||
		!got.RootAssignment.Cleanup.Completion || got.RootAssignment.Cleanup.Owner != "absent" ||
		got.RootAssignment.Cleanup.Eligible || got.RootAssignment.Cleanup.Reason != "scratch_not_registered" {
		t.Fatalf("projection: %+v", got.RootAssignment)
	}
	stored, err := s.broker.RootAssignmentByID(ctx, a.ID)
	if err != nil || stored.Cleanup != nil {
		t.Fatalf("transient cleanup was stored: %+v %v", stored, err)
	}
}

func TestPersonCloseRecordsOnlyTheBoundRootExecutor(t *testing.T) {
	start := swiftstore.ProcessStart(os.Getpid())
	if start.IsZero() {
		t.Skip("this platform has no process-start reader for exact Root binding")
	}
	p := waitingPane("%4", "")
	p.s.State = session.StateIdle
	p.s.PID = os.Getpid()
	p.s.TTY = "ttys004"
	p.s.ConversationID = "20000000-0000-4000-8000-000000000002"
	s := paneServer(t, p)
	s.swift = swiftstore.Open(t.TempDir())
	a := orchestrator.RootAssignment{ID: "10000000-0000-4000-8000-000000000001", State: orchestrator.AssignmentBriefed,
		Assistant: "claude", ProjectDir: t.TempDir(), BriefedAt: start.Unix() + 1,
		BriefPath: s.broker.AssignmentRoot() + "/brief/ASSIGNMENT.md"}
	withExecutor := asJSON(t, a)
	withExecutor["executor"] = orchestratorOpened{TerminalID: "%4", Backend: "tmux", OpenedAt: start.Unix()}
	body, _ := json.Marshal(withExecutor)
	if err := s.store.CreateOpened(context.Background(), store.TableRootAssignments, store.Opened{ID: a.ID,
		State: a.State, Record: body, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil); err != nil {
		t.Fatal(err)
	}
	first := act(t, s, "close", "%4", "root-end-1", `{"force":false}`)
	again := act(t, s, "close", "%4", "root-end-1", `{"force":false}`)
	if first.Code != http.StatusOK || again.Code != http.StatusOK || again.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("close/replay: %d %s / %d %s", first.Code, first.Body, again.Code, again.Body)
	}
	got, err := s.broker.RootAssignmentByID(context.Background(), a.ID)
	if err != nil || got.Closure == nil || !got.Closure.Safe || got.Closure.TerminalID != "%4" ||
		got.Closure.ConversationID != p.s.ConversationID {
		t.Fatalf("Root receipt: %+v %v", got, err)
	}
	if n, err := s.store.EventCount(context.Background(), "root_assignment.closed"); err != nil || n != 1 {
		t.Fatalf("Root close events: %d %v", n, err)
	}
}

func TestAuditedRootCloseRejectsAReusedTerminal(t *testing.T) {
	audit := agentCloseAudit{RootAssignment: "10000000-0000-4000-8000-000000000001",
		Target: session.Session{ID: "%4", Backend: session.BackendTmux, Assistant: session.AssistantCodex,
			PID: 100, ConversationID: "20000000-0000-4000-8000-000000000002"}}
	closed := audit.Target
	closed.PID = 101
	if err := (&Server{}).recordAuditedRootClosure(context.Background(), audit, closed, "scheduled"); err == nil {
		t.Fatal("a reused terminal recorded the previous Root's close")
	}
	closed = audit.Target
	closed.ConversationID = "20000000-0000-4000-8000-000000000003"
	if err := (&Server{}).recordAuditedRootClosure(context.Background(), audit, closed, "close"); err == nil {
		t.Fatal("another conversation recorded the previous Root's close")
	}
}

// Every hand-over route is the orchestrator token's, reads included, and the
// sweep's POST is a dry run unless the body says otherwise.
func TestTheHandOverRoutesAreTheMachines(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Server{broker: &orchestrator.Broker{Store: st, Tasks: taskdir.New(dir), Dir: dir}, store: st}
	call := func(h http.HandlerFunc, method, target, body string, machine bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{machine: machine}))
		rec := httptest.NewRecorder()
		h(rec, req)
		return rec
	}
	for _, c := range []struct {
		h      http.HandlerFunc
		target string
	}{
		{s.handoffsRoute, "/v1/orchestrator/handoffs"},
		{s.rootAssignmentsRoute, "/v1/orchestrator/root-assignments"},
		{s.graphsRoute, "/v1/orchestrator/graphs"},
		{s.reclaimRoute, "/v1/orchestrator/reclaim"},
	} {
		if rec := call(c.h, http.MethodGet, c.target, "", false); rec.Code != http.StatusForbidden {
			t.Errorf("%s without the token: %d", c.target, rec.Code)
		}
		if rec := call(c.h, http.MethodGet, c.target, "", true); rec.Code != http.StatusOK {
			t.Errorf("%s with it: %d %s", c.target, rec.Code, rec.Body)
		}
	}
	rec := call(s.reclaimRoute, http.MethodPost, "/v1/orchestrator/reclaim", "", true)
	var rep contract.BrokerReclaimReport
	if err := json.Unmarshal(rec.Body.Bytes(), &rep); err != nil || !rep.DryRun {
		t.Fatalf("a POST with no body: %d %s", rec.Code, rec.Body)
	}
}
