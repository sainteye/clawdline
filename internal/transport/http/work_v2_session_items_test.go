package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/coordinator"
	"github.com/sainteye/clawdline/internal/domain/icon"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// sessionItemServer is a daemon whose live registry holds one assistant
// Session working in a Project of its own, and the Project's catalog id.
func sessionItemServer(t *testing.T) (*Server, *pane, string) {
	t.Helper()
	home := placesHome(t)
	dir := filepath.Join(home, "code", "notes")
	recordPlace(t, home, dir)
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	canonical, ok := projects.CanonicalProjectKey(dir)
	if !ok {
		t.Fatalf("no canonical Project for %s", dir)
	}
	p := &pane{s: session.Session{ID: "pane-4", Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		CWD: canonical, ConversationID: "10000000-0000-4000-8000-000000000004", State: session.StateIdle}}
	s := paneServer(t, p)
	s.icons = icon.NewRegistry()
	s.broker.Live = func(context.Context) []session.Session { return []session.Session{p.s} }
	for _, place := range s.projectReaders().places.List(s.liveDirectories(context.Background()), 40) {
		if got, _ := projects.CanonicalProjectKey(place.Path); got == canonical {
			return s, p, place.ID
		}
	}
	t.Fatalf("the Session's Project is not in the catalog: %s", canonical)
	return nil, nil, ""
}

func sessionItemBody(t *testing.T, session, run, project, kind string, steps ...string) string {
	t.Helper()
	body := map[string]any{"session_id": session, "via": map[string]string{"run": run}, "project_id": project,
		"kind": kind, "title": "Tidy the release notes", "description": "The person asked for this.\n- draft\n- review",
		"acceptance_criteria": "The release notes are ready to publish."}
	if len(steps) > 0 {
		body["steps"] = steps
	}
	b, _ := json.Marshal(body)
	return string(b)
}

func issueTestRun(t *testing.T, s *Server, p *pane, text string) string {
	t.Helper()
	run, err := s.runs().Issue(context.Background(), p.s, "local", text)
	if err != nil {
		t.Fatal(err)
	}
	return run.ID
}

func TestASessionCreatesAnItemOnThePersonsMessageAndThePersonSeesTheirWords(t *testing.T) {
	s, p, project := sessionItemServer(t)
	run := issueTestRun(t, s, p, "Make a Board item: draft, check and publish the notes")
	body := sessionItemBody(t, p.s.ConversationID, run, project, "feature", "draft", "check", "publish")

	rec := httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items", body, "session-item-1"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var created struct {
		OK   bool           `json:"ok"`
		Item workV2ItemWire `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	it := created.Item
	if it.Phase != "assigned" || it.OwnerSession == nil || *it.OwnerSession != p.s.ConversationID ||
		it.CreatedBy != work.ActorViaSession+run || len(it.Assignments) != 1 ||
		it.Assignments[0].State != "active" || it.Assignments[0].TerminalID != p.s.ID ||
		it.AcceptanceCriteria != "The release notes are ready to publish." || it.AcceptanceVersion != 1 ||
		it.AcceptanceDigest != work.AcceptanceDigest("The release notes are ready to publish.") ||
		it.GateSnapshotCycle != 1 || !it.PlanningGate || it.VerifyGate {
		t.Fatalf("item: %s", rec.Body)
	}
	var titles []string
	for _, st := range it.Steps {
		titles = append(titles, st.Title)
	}
	if strings.Join(titles, "|") != "draft|check|publish" {
		t.Fatalf("steps: %v", titles)
	}
	if it.CreatedVia == nil || it.CreatedVia.Run != run || it.CreatedVia.At == 0 ||
		it.CreatedVia.Excerpt != "Make a Board item: draft, check and publish the notes" {
		t.Fatalf("created_via: %+v", it.CreatedVia)
	}
	// The Session asked for it: nothing is typed into its terminal, and none of
	// the steps became a Session to-do.
	if effects := p.done(); len(effects) != 0 {
		t.Fatalf("the route typed into the Session: %v", effects)
	}
	if rows := ownTodoRows(t, s, p.s.ConversationID); len(rows) != 0 {
		t.Fatalf("steps became %d Session to-dos", len(rows))
	}

	// The same key and body answer what was stored; another body is refused.
	replay := httptest.NewRecorder()
	s.workV2Route(replay, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items", body, "session-item-1"))
	if replay.Code != http.StatusCreated || replay.Body.String() != rec.Body.String() ||
		replay.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay: %d %s", replay.Code, replay.Body)
	}
	reused := httptest.NewRecorder()
	s.workV2Route(reused, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items",
		sessionItemBody(t, p.s.ConversationID, run, project, "issue"), "session-item-1"))
	if reused.Code == http.StatusCreated {
		t.Fatalf("a reused key wrote: %d %s", reused.Code, reused.Body)
	}

	// A person's read of the Board carries the provenance; a person's own item
	// carries none.
	person := httptest.NewRecorder()
	s.workV2Route(person, personWorkV2Request(http.MethodPost, "/v1/work/v2/items",
		`{"project_id":"`+project+`","kind":"issue","title":"Mine","description":"Mine"}`, "person-item"))
	if person.Code != http.StatusCreated || strings.Contains(person.Body.String(), "created_via") {
		t.Fatalf("person item: %d %s", person.Code, person.Body)
	}
	list := httptest.NewRecorder()
	s.workV2Route(list, httptest.NewRequest(http.MethodGet, "/v1/work/v2/items?status=open", nil))
	if list.Code != http.StatusOK || strings.Count(list.Body.String(), `"created_via":{"run":"`+run+`"`) != 1 {
		t.Fatalf("list: %d %s", list.Code, list.Body)
	}
}

func TestUnregisteredMachineSessionCannotCreateProjectItem(t *testing.T) {
	s, p, project := sessionItemServer(t)
	s.cfg.Dir = s.broker.Dir
	p.s.CWD = projects.MachineWorkspace(s.cfg.Dir)
	run := issueTestRun(t, s, p, "Create a Project Board item before delegation")
	rec := httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items",
		sessionItemBody(t, p.s.ConversationID, run, project, "feature"), "machine-unregistered"))
	if rec.Code != http.StatusForbidden || codeOf(t, rec) != "coordinator_required" {
		t.Fatalf("unregistered machine Session created work: %d %s", rec.Code, rec.Body.String())
	}
}

func registeredMachineItemServer(t *testing.T) (*Server, *pane, *pane, string) {
	t.Helper()
	s, machine, project := sessionItemServer(t)
	projectPane := &pane{s: machine.s}
	projectPane.s.ID = "project-pane"
	projectPane.s.ConversationID = "20000000-0000-4000-8000-000000000004"
	s.cfg.Dir = s.broker.Dir
	machine.s.ID = "machine-pane"
	machine.s.CWD = projects.MachineWorkspace(s.cfg.Dir)
	machine.s.PID = os.Getpid()
	hosts := []ports.TerminalHost{machine, projectPane}
	s.terminals = hosts
	s.inventory = app.Inventory{Terminals: hosts, Screen: machine}
	s.broker.Live = func(context.Context) []session.Session { return []session.Session{machine.s, projectPane.s} }
	c := s.coordinator()
	started := c.ProcessStart(machine.s.PID)
	if started.IsZero() {
		t.Skip("this platform did not expose the test process start")
	}
	role := coordinator.Record{ID: "30000000-0000-4000-8000-000000000004", Identity: coordinator.Identity{
		ConversationID: machine.s.ConversationID, TerminalID: machine.s.ID,
		Assistant: string(machine.s.Assistant), PID: machine.s.PID, ProcessStart: started,
	}, CWD: machine.s.CWD, Generation: 1, RegisteredAt: time.Now()}
	if err := s.store.CommitCoordinator(context.Background(), nil, role, nil); err != nil {
		t.Fatal(err)
	}
	return s, machine, projectPane, project
}

func TestRegisteredClawdfatherCreatesItemThenDelegatesToProjectSession(t *testing.T) {
	s, machine, projectPane, project := registeredMachineItemServer(t)
	run := issueTestRun(t, s, machine, "Create a Board item and delegate the engineering work")
	var body map[string]any
	if err := json.Unmarshal([]byte(sessionItemBody(t, machine.s.ConversationID, run, project, "feature", "implement")), &body); err != nil {
		t.Fatal(err)
	}
	body["assign"] = map[string]string{"mode": "existing_session", "terminal_id": projectPane.s.ID}
	encoded, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items", string(encoded), "machine-delegate"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create and delegate: %d %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Assigned        bool           `json:"assigned"`
		AssignmentState string         `json:"assignment_state"`
		Item            workV2ItemWire `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || !got.Assigned || got.AssignmentState != "assigned" || got.Item.OwnerSession == nil ||
		*got.Item.OwnerSession != projectPane.s.ConversationID || got.Item.Phase != "assigned" {
		t.Fatalf("Board item did not reach the Project owner: %v %s", err, rec.Body.String())
	}
	retry := httptest.NewRecorder()
	s.workV2Route(retry, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items", string(encoded), "machine-delegate"))
	if retry.Code != http.StatusCreated || retry.Body.String() != rec.Body.String() {
		t.Fatalf("same delegation key did not replay one item: %d %s", retry.Code, retry.Body.String())
	}
}

func TestRegisteredClawdfatherDistinguishesUnassignedAndFailedDelegation(t *testing.T) {
	s, machine, _, project := registeredMachineItemServer(t)
	run := issueTestRun(t, s, machine, "Create a Board item for engineering work")
	body := sessionItemBody(t, machine.s.ConversationID, run, project, "issue")
	created := httptest.NewRecorder()
	s.workV2Route(created, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items", body, "machine-no-assign"))
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"assignment_state":"not_requested"`) ||
		strings.Contains(created.Body.String(), `"assigned":true`) {
		t.Fatalf("unassigned: %d %s", created.Code, created.Body)
	}
	var initial struct {
		Item workV2ItemWire `json:"item"`
	}
	_ = json.Unmarshal(created.Body.Bytes(), &initial)
	if initial.Item.OwnerSession != nil || initial.Item.Phase != "created" {
		t.Fatalf("unassigned item: %s", created.Body)
	}
	var request map[string]any
	_ = json.Unmarshal([]byte(body), &request)
	request["assign"] = map[string]string{"mode": "existing_session", "terminal_id": "missing-pane"}
	encoded, _ := json.Marshal(request)
	failed := httptest.NewRecorder()
	s.workV2Route(failed, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items", string(encoded), "machine-failed-assign"))
	if failed.Code != http.StatusCreated || !strings.Contains(failed.Body.String(), `"assignment_state":"failed"`) ||
		!strings.Contains(failed.Body.String(), `"assignment_error"`) {
		t.Fatalf("failed assignment: %d %s", failed.Code, failed.Body)
	}
}

func TestMachineItemReceiptFailureKeepsTheCreatedItemForReplay(t *testing.T) {
	s, machine, projectPane, project := registeredMachineItemServer(t)
	run := issueTestRun(t, s, machine, "Create and delegate this item")
	var request map[string]any
	_ = json.Unmarshal([]byte(sessionItemBody(t, machine.s.ConversationID, run, project, "feature")), &request)
	request["assign"] = map[string]string{"mode": "existing_session", "terminal_id": projectPane.s.ID}
	encoded, _ := json.Marshal(request)
	s.replaceMachineItemReceipt = func(context.Context, store.ReceiptKey, store.ReceiptAnswer) error {
		return errors.New("injected receipt update failure")
	}
	failed := httptest.NewRecorder()
	s.workV2Route(failed, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items", string(encoded), "machine-receipt-fault"))
	if failed.Code != http.StatusInternalServerError || codeOf(t, failed) != "receipt_update_failed" {
		t.Fatalf("receipt fault: %d %s", failed.Code, failed.Body)
	}
	replay := httptest.NewRecorder()
	s.workV2Route(replay, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items", string(encoded), "machine-receipt-fault"))
	if replay.Code != http.StatusCreated || replay.Header().Get("Idempotent-Replayed") != "true" ||
		!strings.Contains(replay.Body.String(), `"assignment_state":"pending"`) {
		t.Fatalf("replay: %d %s", replay.Code, replay.Body)
	}
	var got struct {
		Item workV2ItemWire `json:"item"`
	}
	_ = json.Unmarshal(replay.Body.Bytes(), &got)
	items, _, err := s.store.WorkV2Items(context.Background(), "", "", "all", "", 100)
	if err != nil || len(items) != 1 || items[0].ID != got.Item.ID || !strings.Contains(failed.Body.String(), got.Item.ID) {
		t.Fatalf("duplicate or lost item: %+v, %v, %s", items, err, failed.Body)
	}
}

func TestMachineItemNewSessionDialogIsAwaitingUserNotAssigned(t *testing.T) {
	s, machine, projectPane, project := registeredMachineItemServer(t)
	dir := s.broker.Dir
	s.broker = &orchestrator.Broker{Store: s.store, Tasks: taskdir.New(dir), Dir: dir,
		Launcher: oneTmuxPane{pane: projectPane.s.ID},
		Live:     func(context.Context) []session.Session { return []session.Session{machine.s, projectPane.s} },
		Screen: func(_ context.Context, id string) (string, bool) {
			return orchestratorScreen(t, "codex-update"), id == projectPane.s.ID
		},
		Type: func(context.Context, string, string) error { return nil },
	}
	run := issueTestRun(t, s, machine, "Create a Board item and delegate it to a new Session")
	var request map[string]any
	_ = json.Unmarshal([]byte(sessionItemBody(t, machine.s.ConversationID, run, project, "feature")), &request)
	request["assign"] = map[string]string{"mode": "new_session", "assistant": "codex"}
	encoded, _ := json.Marshal(request)
	rec := httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items", string(encoded), "machine-dialog"))
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"assignment_state":"awaiting_user"`) ||
		!strings.Contains(rec.Body.String(), `"assigned":false`) {
		t.Fatalf("waiting assignment: %d %s", rec.Code, rec.Body)
	}
	var got struct {
		Item workV2ItemWire `json:"item"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Item.Phase != "assigning" || got.Item.OwnerSession != nil || got.Item.Condition == nil ||
		*got.Item.Condition != string(work.ConditionWaitingUser) {
		t.Fatalf("waiting Board item: %s", rec.Body)
	}
}

// Completing the idempotency receipt is part of the item transaction. The
// response projector therefore must not consult the terminal inventory: an
// unavailable terminal would otherwise hold the store's only connection and
// stop unrelated conversation and Board reads.
func TestCreatingAnItemDoesNotReadTerminalInventoryInsideTheStoreTransaction(t *testing.T) {
	s, _, project := sessionItemServer(t)
	base := s.inventory.Read(context.Background())
	entered, release := make(chan struct{}), make(chan struct{})
	calls := 0
	s.readings = app.NewInventoryReading(func(ctx context.Context) session.Inventory {
		calls++
		if calls > 1 {
			select {
			case <-entered:
			default:
				close(entered)
			}
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		return base
	}, time.Nanosecond)

	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		s.workV2Route(rec, personWorkV2Request(http.MethodPost, "/v1/work/v2/items",
			`{"project_id":"`+project+`","kind":"issue","title":"Do not block","description":"Keep reads moving."}`,
			"inventory-outside-transaction"))
		close(done)
	}()

	select {
	case <-done:
		if rec.Code != http.StatusCreated {
			t.Fatalf("create: %d %s", rec.Code, rec.Body)
		}
		return
	case <-entered:
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, _, readErr := s.store.WorkV2Items(ctx, "", "", "open", "", 1)
	close(release)
	<-done
	if readErr != nil {
		t.Fatalf("an inventory read inside the item transaction blocked an unrelated store read: %v", readErr)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
}

func TestABoardPageReadsTheProjectCatalogOnce(t *testing.T) {
	s, _, project := sessionItemServer(t)
	personItem(t, s, project, "issue", "catalog-once-1")
	personItem(t, s, project, "feature", "catalog-once-2")
	base := s.inventory.Read(context.Background())
	var scans atomic.Int32
	s.readings = app.NewInventoryReading(func(context.Context) session.Inventory {
		scans.Add(1)
		return base
	}, time.Nanosecond)

	rec := httptest.NewRecorder()
	s.workV2Route(rec, httptest.NewRequest(http.MethodGet, "/v1/work/v2/items?status=open&q=Tidy", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	if got := scans.Load(); got != 1 {
		t.Fatalf("one Board page scanned the terminal-backed Project catalog %d times, want 1", got)
	}
}

func TestASessionCreatedItemIsRefusedByNameAndWritesNothing(t *testing.T) {
	s, p, project := sessionItemServer(t)
	mine := issueTestRun(t, s, p, "make an item")
	other, err := s.runs().Issue(context.Background(), session.Session{ID: "pane-9",
		ConversationID: "10000000-0000-4000-8000-000000000009"}, "local", "not to you")
	if err != nil {
		t.Fatal(err)
	}
	tooMany := make([]string, store.WorkV2StepLimit+1)
	for i := range tooMany {
		tooMany[i] = "step"
	}
	for _, c := range []struct {
		name, body string
		status     int
		code       string
	}{
		{"no run", sessionItemBody(t, p.s.ConversationID, "", project, "feature"), http.StatusForbidden, "run_unknown"},
		{"unknown run", sessionItemBody(t, p.s.ConversationID, "0f0f0f0f-0000-4000-8000-000000000001", project, "feature"),
			http.StatusForbidden, "run_unknown"},
		{"another Session's run", sessionItemBody(t, p.s.ConversationID, other.ID, project, "feature"),
			http.StatusForbidden, "run_other_session"},
		{"no live Session", sessionItemBody(t, "10000000-0000-4000-8000-000000000999", mine, project, "feature"),
			http.StatusNotFound, "session_not_found"},
		{"unknown Project", sessionItemBody(t, p.s.ConversationID, mine, "nope", "feature"),
			http.StatusUnprocessableEntity, "project_not_found"},
		{"bad kind", sessionItemBody(t, p.s.ConversationID, mine, project, "chore"), http.StatusConflict, "invalid_kind"},
		{"too many steps", sessionItemBody(t, p.s.ConversationID, mine, project, "feature", tooMany...),
			http.StatusRequestEntityTooLarge, "too_many_steps"},
	} {
		rec := httptest.NewRecorder()
		s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items", c.body, "refused-"+c.name))
		if rec.Code != c.status || codeOf(t, rec) != c.code {
			t.Fatalf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
	}
	// A person's device is not a Session.
	rec := httptest.NewRecorder()
	s.workV2Route(rec, personWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items",
		sessionItemBody(t, p.s.ConversationID, mine, project, "feature"), "person-on-agent"))
	if rec.Code != http.StatusUnauthorized || codeOf(t, rec) != "machine_required" {
		t.Fatalf("person: %d %s", rec.Code, rec.Body)
	}
	// Without a key nothing is written.
	rec = httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items",
		sessionItemBody(t, p.s.ConversationID, mine, project, "feature"), ""))
	if rec.Code == http.StatusCreated {
		t.Fatalf("no key: %d %s", rec.Code, rec.Body)
	}
	items, _, err := s.workV2().List(context.Background(), "", "", "all", "")
	if err != nil || len(items) != 0 {
		t.Fatalf("refusals wrote %d items (%v)", len(items), err)
	}

	// Five items on one message, and the sixth is refused.
	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items",
			sessionItemBody(t, p.s.ConversationID, mine, project, "issue"), "five-"+string(rune('a'+i))))
		if rec.Code != http.StatusCreated {
			t.Fatalf("item %d: %d %s", i+1, rec.Code, rec.Body)
		}
	}
	rec = httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items",
		sessionItemBody(t, p.s.ConversationID, mine, project, "issue"), "sixth"))
	if rec.Code != http.StatusTooManyRequests || codeOf(t, rec) != "run_items_exhausted" {
		t.Fatalf("sixth: %d %s", rec.Code, rec.Body)
	}
}

func TestAnExpiredRunOrAChildCreatesNoItem(t *testing.T) {
	s, p, project := sessionItemServer(t)
	at := time.Now().Add(-work.RelayWindow - time.Minute)
	old := s.runs()
	old.Now = func() time.Time { return at }
	expired := issueTestRun(t, s, p, "yesterday")
	old.Now = nil
	rec := httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items",
		sessionItemBody(t, p.s.ConversationID, expired, project, "feature"), "expired"))
	if rec.Code != http.StatusForbidden || codeOf(t, rec) != "run_expired" {
		t.Fatalf("expired: %d %s", rec.Code, rec.Body)
	}

	r := orchestrator.Record{ID: "7a5c0000-0000-4000-8000-000000000002", Kind: "custom", Title: "child",
		Assistant: "claude", State: orchestrator.StateBriefed, CreatedAt: time.Now(),
		ChildTerminalID: p.s.ID, ChildBackend: "tmux", SpawnedAt: time.Now()}
	body, _ := json.Marshal(r)
	if _, err := s.store.CreateBrokerTask(context.Background(), store.BrokerRow{ID: r.ID, Project: "/p",
		Assistant: "claude", State: string(r.State), CreatedAt: r.CreatedAt, SecretHash: "h", Record: body}, nil); err != nil {
		t.Fatal(err)
	}
	fresh := issueTestRun(t, s, p, "today")
	rec = httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items",
		sessionItemBody(t, p.s.ConversationID, fresh, project, "feature"), "child"))
	if rec.Code != http.StatusConflict || codeOf(t, rec) != "child_session" {
		t.Fatalf("child: %d %s", rec.Code, rec.Body)
	}
	items, _, _ := s.workV2().List(context.Background(), "", "", "all", "")
	if len(items) != 0 {
		t.Fatalf("refusals wrote %d items", len(items))
	}
}
