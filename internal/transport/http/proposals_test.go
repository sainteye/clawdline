package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/taskdir"
	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
	"github.com/sainteye/clawdline-go/internal/domain/auth"
	"github.com/sainteye/clawdline-go/internal/domain/session"
	"github.com/sainteye/clawdline-go/internal/domain/work"
)

// The participation routes over HTTP: the session's door and the person's,
// who may knock on which, one answer per key, and /v1/diagnostics counting
// what the server decided against what the sessions reported — then the
// whole chain from a proposal to the moves on the item it made.
func TestTheProposalRoutesAndTheDiagnosticsCounts(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Server{store: st}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/work/", s.workRoute)
	mux.HandleFunc("/v1/orchestrator/sessions/", s.brokerSessionRoute)
	mux.HandleFunc("/v1/orchestrator/runs/", s.runRoute)
	s.participationRoutes(mux)
	person := access{verdict: auth.Verdict{Allowed: true, Local: true}}
	machine := access{machine: true}
	do := func(a access, method, target, key, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		req = req.WithContext(context.WithValue(req.Context(), accessKey{}, a))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	code := func(rec *httptest.ResponseRecorder) string {
		var body struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return body.Error
	}
	type answer struct {
		Proposal struct {
			ID       string `json:"id"`
			WorkID   string `json:"work_id"`
			Ask      bool   `json:"ask"`
			Reason   string `json:"reason"`
			Question string `json:"question"`
		} `json:"proposal"`
		Item *struct {
			ID    string `json:"id"`
			Place string `json:"place"`
		} `json:"item"`
		Instructions string `json:"instructions"`
	}
	read := func(rec *httptest.ResponseRecorder) answer {
		var a answer
		if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil {
			t.Fatalf("%s: %v", rec.Body, err)
		}
		return a
	}
	diagnostics := func() map[string]any {
		req := httptest.NewRequest(http.MethodGet, "/v1/diagnostics", nil)
		req = req.WithContext(context.WithValue(req.Context(), accessKey{}, person))
		rec := httptest.NewRecorder()
		s.diagnostics(rec, req)
		var d struct {
			Proposals map[string]any `json:"proposals"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil || d.Proposals == nil {
			t.Fatalf("diagnostics %d %s", rec.Code, rec.Body)
		}
		return d.Proposals
	}

	// Two lines of work, each a child its root sent.
	lines := []string{"0b0e0000-0000-4000-8000-000000000001", "0b0e0000-0000-4000-8000-000000000002"}
	for i, l := range lines {
		r := orchestrator.Record{ID: "7a5c0000-0000-4000-8000-00000000000" + string(rune('1'+i)), Kind: "custom",
			Title: "child", WorkID: l, State: orchestrator.StateBriefed, CreatedAt: time.Now(),
			Root: &orchestrator.RootRef{SessionID: "root-conv", Assistant: "claude"}}
		body, _ := json.Marshal(r)
		if _, err := st.CreateBrokerTask(context.Background(), store.BrokerRow{ID: r.ID, Project: "/p", Assistant: "claude",
			State: string(r.State), CreatedAt: r.CreatedAt, SecretHash: "h", Record: body}, nil); err != nil {
			t.Fatal(err)
		}
	}
	propose := func(line string) string {
		return `{"session_id":"root-conv","work_id":"` + line + `","title":"ship it","project":"/p"}`
	}

	// Who may knock: a person does not propose, nobody unknown does.
	if rec := do(person, http.MethodPost, "/v1/orchestrator/proposals", "p0", propose(lines[0])); rec.Code != 403 ||
		code(rec) != "proposal_is_for_sessions" {
		t.Fatalf("a person proposing: %d %s", rec.Code, rec.Body)
	}
	if rec := do(access{}, http.MethodPost, "/v1/orchestrator/proposals", "p0", propose(lines[0])); rec.Code != 401 {
		t.Fatalf("nobody: %d %s", rec.Code, rec.Body)
	}
	if rec := do(machine, http.MethodPost, "/v1/orchestrator/proposals", "", propose(lines[0])); code(rec) != "idempotency_key_required" {
		t.Fatalf("no key: %d %s", rec.Code, rec.Body)
	}
	if rec := do(machine, http.MethodPost, "/v1/orchestrator/proposals", "p0", `{"session_id":"root-conv","work_id":"`+
		lines[0]+`","title":"x","project":"/p","ask":true}`); code(rec) != "invalid_command" {
		t.Fatalf("an agent deciding ask itself: %d %s", rec.Code, rec.Body)
	}

	// A person wrote to the root a moment ago: the first is asked.
	s.participation().Heard.Mark(time.Now(), "root-conv")
	rec := do(machine, http.MethodPost, "/v1/orchestrator/proposals", "p1", propose(lines[0]))
	if rec.Code != 201 {
		t.Fatalf("propose: %d %s", rec.Code, rec.Body)
	}
	asked := read(rec)
	if !asked.Proposal.Ask || asked.Proposal.Question == "" || !strings.Contains(asked.Instructions, asked.Proposal.ID+"/asked") {
		t.Fatalf("asked: %s", rec.Body)
	}
	if again := do(machine, http.MethodPost, "/v1/orchestrator/proposals", "p1", propose(lines[0])); again.Body.String() != rec.Body.String() ||
		again.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("a retry: %d %s", again.Code, again.Body)
	}
	if dup := do(machine, http.MethodPost, "/v1/orchestrator/proposals", "p2", propose(lines[0])); dup.Code != 409 ||
		code(dup) != "proposal_duplicate" {
		t.Fatalf("a duplicate: %d %s", dup.Code, dup.Body)
	}
	// The second, in the same turn, goes to the "to confirm" area.
	held := read(do(machine, http.MethodPost, "/v1/orchestrator/proposals", "p3", propose(lines[1])))
	if held.Proposal.Ask || held.Proposal.Reason != "proposal_budget_exhausted" || strings.Contains(held.Instructions, "/asked") {
		t.Fatalf("held: %+v", held)
	}
	d := diagnostics()
	if d["ask_true"] != 1.0 || d["asked_inline"] != 0.0 || d["matched"] != false || d["pending"] != 2.0 {
		t.Fatalf("before the report: %v", d)
	}
	if rec := do(machine, http.MethodPost, "/v1/orchestrator/proposals/"+asked.Proposal.ID+"/asked", "",
		`{"session_id":"root-conv"}`); rec.Code != 200 {
		t.Fatalf("asked: %d %s", rec.Code, rec.Body)
	}
	if d := diagnostics(); d["ask_true"] != 1.0 || d["asked_inline"] != 1.0 || d["matched"] != true {
		t.Fatalf("after the report: %v", d)
	}
	// Asked anyway on the held one: the two numbers no longer agree.
	do(machine, http.MethodPost, "/v1/orchestrator/proposals/"+held.Proposal.ID+"/asked", "", `{"session_id":"root-conv"}`)
	if d := diagnostics(); d["asked_inline"] != 2.0 || d["asked_inline_unprompted"] != 1.0 || d["matched"] != false {
		t.Fatalf("after an unprompted ask: %v", d)
	}

	// The "to confirm" area is the person's to read.
	list := do(person, http.MethodGet, "/v1/work/proposals", "", "")
	if list.Code != 200 || !strings.Contains(list.Body.String(), `"pending":2`) {
		t.Fatalf("to confirm: %d %s", list.Code, list.Body)
	}
	// A session answers only by relaying a person, under their run.
	path := "/v1/work/proposals/" + asked.Proposal.ID
	if rec := do(machine, http.MethodPost, path, "a1", `{"answer":"track"}`); code(rec) != "session_cannot_decide" {
		t.Fatalf("a session answering: %d %s", rec.Code, rec.Body)
	}
	// The run is one this daemon issued when the person's message was sent,
	// and one said to this proposal's root: an invented run, and a message
	// to another session, answer nothing and leave the proposal pending.
	if rec := do(machine, http.MethodPost, path, "a1", `{"answer":"track","via":{"run":"run-9"}}`); rec.Code != 403 ||
		code(rec) != "run_unknown" {
		t.Fatalf("an invented run: %d %s", rec.Code, rec.Body)
	}
	if rec := do(machine, http.MethodGet, "/v1/orchestrator/sessions/root-conv/run", "", ""); rec.Code != 404 ||
		code(rec) != "no_run" {
		t.Fatalf("a session nobody wrote to: %d %s", rec.Code, rec.Body)
	}
	other, err := s.runs().Issue(context.Background(), session.Session{ID: "%8", ConversationID: "other-conv"}, "local")
	if err != nil {
		t.Fatal(err)
	}
	if rec := do(machine, http.MethodPost, path, "a1", `{"answer":"track","via":{"run":"`+other.ID+`"}}`); rec.Code != 403 ||
		code(rec) != "run_other_session" {
		t.Fatalf("another session's run: %d %s", rec.Code, rec.Body)
	}
	mine, err := s.runs().Issue(context.Background(), session.Session{ID: "%4", ConversationID: "root-conv"}, "device:phone")
	if err != nil {
		t.Fatal(err)
	}
	// The session finds its run by its conversation id; anyone who may
	// read finds what a run was by its id.
	if rec := do(machine, http.MethodGet, "/v1/orchestrator/sessions/root-conv/run", "", ""); rec.Code != 200 ||
		!strings.Contains(rec.Body.String(), `"id":"`+mine.ID+`"`) {
		t.Fatalf("the session's run: %d %s", rec.Code, rec.Body)
	}
	if rec := do(person, http.MethodGet, "/v1/orchestrator/sessions/root-conv/run", "", ""); rec.Code != 403 {
		t.Fatalf("a person reading a session's run route: %d %s", rec.Code, rec.Body)
	}
	if rec := do(person, http.MethodGet, "/v1/orchestrator/runs/"+mine.ID, "", ""); rec.Code != 200 ||
		!strings.Contains(rec.Body.String(), `"session_id":"root-conv"`) || !strings.Contains(rec.Body.String(), `"principal":"device:phone"`) {
		t.Fatalf("what the run was: %d %s", rec.Code, rec.Body)
	}
	if rec := do(person, http.MethodGet, "/v1/orchestrator/runs/run-9", "", ""); rec.Code != 404 || code(rec) != "run_unknown" {
		t.Fatalf("a run nobody issued: %d %s", rec.Code, rec.Body)
	}
	rec = do(machine, http.MethodPost, path, "a1", `{"answer":"track","via":{"run":"`+mine.ID+`"}}`)
	made := read(rec)
	if rec.Code != 200 || made.Item == nil || made.Item.Place != "board" || made.Item.ID != lines[0] {
		t.Fatalf("track: %d %s", rec.Code, rec.Body)
	}
	moves := do(person, http.MethodGet, "/v1/work/items/"+lines[0]+"/moves", "", "")
	if !strings.Contains(moves.Body.String(), `"from":"proposal"`) ||
		!strings.Contains(moves.Body.String(), `"actor":"user_via_session:`+mine.ID+`"`) ||
		!strings.Contains(moves.Body.String(), `"session":"root-conv"`) {
		t.Fatalf("moves: %s", moves.Body)
	}
	// A decision on it, and the person's answer as the item's newest move.
	rec = do(machine, http.MethodPost, "/v1/orchestrator/decisions", "d1", `{"session_id":"root-conv","work_id":"`+
		lines[0]+`","question":"Which way?","options":[{"id":"l","label":"left"},{"id":"r","label":"right"}],"default":"l"}`)
	if rec.Code != 201 {
		t.Fatalf("decision: %d %s", rec.Code, rec.Body)
	}
	var opened struct {
		Decision struct {
			ID   string `json:"id"`
			Push string `json:"push"`
		} `json:"decision"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &opened)
	if opened.Decision.Push != "none" {
		t.Fatalf("a quiet decision is not pushed: %s", rec.Body)
	}
	if rec := do(person, http.MethodPost, "/v1/orchestrator/decisions", "d2", `{}`); code(rec) != "decision_is_for_sessions" {
		t.Fatalf("a person asking: %d %s", rec.Code, rec.Body)
	}
	if rec := do(person, http.MethodPost, "/v1/work/decisions/"+opened.Decision.ID, "d3", `{"answer":"r"}`); rec.Code != 200 {
		t.Fatalf("answer: %d %s", rec.Code, rec.Body)
	}
	moves = do(person, http.MethodGet, "/v1/work/items/"+lines[0]+"/moves", "", "")
	if !strings.Contains(moves.Body.String(), `"trigger":"decision_answered"`) {
		t.Fatalf("moves after the decision: %s", moves.Body)
	}
	if rec := do(machine, http.MethodGet, "/v1/orchestrator/decisions/"+opened.Decision.ID, "", ""); !strings.Contains(rec.Body.String(), `"answer":"r"`) {
		t.Fatalf("the session reads the answer: %d %s", rec.Code, rec.Body)
	}
	if rec := do(person, http.MethodGet, "/v1/work/digests?kind=monthly", "", ""); rec.Code != 400 {
		t.Fatalf("a kind that is not one: %d", rec.Code)
	}
}

// A root proposing one of its child's leftovers has read the line this broker
// typed at it: that line is what says the route exists and what the leftover is
// called. So the proposal ends the resend, the same way a landing does — and
// only from the root's own session, because the same route takes a child's
// proposal for its root and a child cannot observe on its behalf.
func TestALeftoverProposalFromTheRootEndsTheResend(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dir := t.TempDir()
	b := &orchestrator.Broker{Store: st, Tasks: taskdir.New(dir), Dir: dir}
	s := &Server{store: st, broker: b}
	mux := http.NewServeMux()
	s.participationRoutes(mux)
	machine := access{machine: true}
	ctx := context.Background()

	settled := func(id string) orchestrator.Record {
		r := orchestrator.Record{Protocol: orchestrator.Protocol, ID: id, Kind: "custom", Assistant: "claude",
			Title: "child", ProjectDir: "/p", State: orchestrator.StateBriefed, CreatedAt: time.Now(),
			Root: &orchestrator.RootRef{SessionID: "root-conv", Assistant: "claude"}}
		body, _ := json.Marshal(r)
		if _, err := st.CreateBrokerTask(ctx, store.BrokerRow{ID: id, Project: "/p", Assistant: "claude",
			State: string(r.State), CreatedAt: r.CreatedAt, SecretHash: orchestrator.HashSecret("s"),
			Record: body}, nil); err != nil {
			t.Fatal(err)
		}
		out, err := b.Settle(ctx, id, orchestrator.StateSuccess, "done", &taskdir.Result{
			Status: "success", Summary: "done",
			Leftovers: []work.Leftover{{Title: "the retry ladder has no ceiling", Why: "out of budget"}}})
		if err != nil {
			t.Fatal(err)
		}
		if out.Notice == nil {
			t.Fatal("a finished task with no completion envelope")
		}
		return out
	}
	propose := func(key, session, id string, as access) *httptest.ResponseRecorder {
		body := `{"session_id":"` + session + `","task_id":"` + id + `","leftover":"the retry ladder has no ceiling"}`
		req := httptest.NewRequest(http.MethodPost, "/v1/orchestrator/proposals", strings.NewReader(body))
		req.Header.Set("Idempotency-Key", key)
		if !as.machine {
			req.Header.Set("X-Clawdline-Task-Secret", "s")
		}
		req = req.WithContext(context.WithValue(req.Context(), accessKey{}, as))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	state := func(id string) orchestrator.NoticeState {
		after, _, err := b.Record(ctx, id)
		if err != nil || after.Notice == nil {
			t.Fatalf("reading the notice back: %v", err)
		}
		return after.Notice.State
	}

	mine := settled("7a5c0000-0000-4000-8000-000000000011")
	if rec := propose("q1", "root-conv", mine.ID, machine); rec.Code != 201 {
		t.Fatalf("the root's own proposal: %d %s", rec.Code, rec.Body)
	}
	if got := state(mine.ID); got != orchestrator.NoticeAcknowledged {
		t.Fatalf("the root proposed its child's leftover and the notice is %q", got)
	}

	// The child's own proposal about its own leftover arrives on the same route,
	// with the task secret, and is recorded as its root's line of work — but the
	// root has still been told nothing, so the notice stands.
	childs := settled("7a5c0000-0000-4000-8000-000000000012")
	if rec := propose("q2", "", childs.ID, access{}); rec.Code != 201 {
		t.Fatalf("the child's own proposal: %d %s", rec.Code, rec.Body)
	}
	if got := state(childs.ID); got == orchestrator.NoticeAcknowledged {
		t.Fatal("a child's proposal closed the notice its root had not read")
	}
}
