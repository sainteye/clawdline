package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
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
		BriefAttemptedAt: 2, BriefedAt: 3, Failure: "f"}
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
