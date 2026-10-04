package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// On the Agent routes expected_version is optional: omitted, the write acts
// on the version the item holds then; named, it is still compared and a
// stale one is still a version_conflict. A replay under the same
// Idempotency-Key answers what the first attempt answered. A person's write
// still needs its version.
func TestAgentItemWritesTakeAnOptionalExpectedVersion(t *testing.T) {
	s, p, v := workV2AssignmentServer(t, session.StateWorking)
	owned, err := s.assignWorkV2(context.Background(), v.Item.ID, "local", v.Item.Version,
		"existing_session", p.s.ID, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	conversation := p.s.ConversationID
	base := "/v1/work/v2/agent/items/" + v.Item.ID
	post := func(method, path, body, key string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		s.workV2Route(rec, agentWorkV2Request(method, path, body, key))
		return rec
	}
	if rec := post(http.MethodPost, base+"/steps", `{"session_id":"`+conversation+`","title":"Wire it","position":0}`,
		"opt-step-add"); rec.Code != http.StatusCreated {
		t.Fatalf("step add without a version: %d %s", rec.Code, rec.Body)
	}
	read, err := s.workV2().Item(context.Background(), v.Item.ID)
	if err != nil || len(read.Steps) != 1 {
		t.Fatalf("steps = %+v %v", read.Steps, err)
	}
	if rec := post(http.MethodPost, base+"/steps/"+read.Steps[0].ID+"/complete", `{"session_id":"`+conversation+`"}`,
		"opt-step-done"); rec.Code != http.StatusOK {
		t.Fatalf("step done without a version: %d %s", rec.Code, rec.Body)
	}
	phase := `{"session_id":"` + conversation + `","next":"implementing"}`
	first := post(http.MethodPost, base+"/phase", phase, "opt-phase")
	if first.Code != http.StatusOK {
		t.Fatalf("phase without a version: %d %s", first.Code, first.Body)
	}
	if again := post(http.MethodPost, base+"/phase", phase, "opt-phase"); again.Code != first.Code ||
		again.Body.String() != first.Body.String() {
		t.Fatalf("replay = %d %s, first %s", again.Code, again.Body, first.Body)
	}
	stale := `{"expected_version":` + strconv.FormatInt(owned.Item.Version, 10) + `,"session_id":"` + conversation +
		`","role":"spec","title":"Spec","body":"What it does."}`
	if rec := post(http.MethodPost, base+"/documents", stale, "opt-doc-stale"); rec.Code != http.StatusConflict {
		t.Fatalf("stale version: %d %s", rec.Code, rec.Body)
	}
	doc := `{"session_id":"` + conversation + `","role":"spec","title":"Spec","body":"What it does."}`
	rec := post(http.MethodPost, base+"/documents", doc, "opt-doc")
	if rec.Code != http.StatusCreated {
		t.Fatalf("doc without a version: %d %s", rec.Code, rec.Body)
	}
	var answer struct {
		Item workV2ItemWire `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil || answer.Item.Phase != "implementing" {
		t.Fatalf("doc answer = %+v %v", answer.Item, err)
	}
	// A person's edit names its version; none is no version at all.
	renamed := "Renamed"
	_, err = s.workV2().Edit(context.Background(), v.Item.ID, app.EditWorkV2{Title: &renamed, Actor: "local", Person: true}, nil)
	var we *app.WorkError
	if !errors.As(err, &we) || we.Code != "version_conflict" {
		t.Fatalf("person edit without a version = %v", err)
	}
}

// A blocked Project registry models a stalled history/catalog dependency. The
// folded summary must finish without consulting it, even with an assigned
// item that would make the full answer project its Project.
func TestSessionTodoSummaryIgnoresStalledProjectHistory(t *testing.T) {
	s, p, item := workV2AssignmentServer(t, session.StateWorking)
	if _, err := s.assignWorkV2(context.Background(), item.Item.ID, "local", item.Item.Version,
		"existing_session", p.s.ID, "", "", nil); err != nil {
		t.Fatal(err)
	}
	for index, actor := range []string{"device:phone", p.s.ConversationID, "device:phone"} {
		todo, err := s.workV2().CreateDirectTodo(context.Background(), app.NewDirectTodoV2{
			SessionID: p.s.ConversationID, Text: "Check the count", Actor: actor,
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if index == 2 {
			if _, err := s.workV2().CompleteDirectTodo(context.Background(), todo.ID, p.s.ConversationID,
				p.s.ConversationID, false, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	blocked := make(chan struct{})
	t.Cleanup(func() { close(blocked) })
	s.projectReaders().places.Registered = func() ([]projects.RegisteredPlace, error) {
		<-blocked
		return nil, nil
	}
	answer := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		s.workV2Route(rec, personWorkV2Request(http.MethodGet,
			"/v1/work/v2/session-todos/conversation:"+p.s.ConversationID+"?summary=1", "", ""))
		answer <- rec
	}()
	select {
	case rec := <-answer:
		if rec.Code != http.StatusOK {
			t.Fatalf("summary: %d %s", rec.Code, rec.Body)
		}
		var counts struct {
			Done, Active, Waiting int
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &counts); err != nil || counts.Waiting != 2 || counts.Done != 1 || counts.Active != 1 {
			t.Fatalf("counts = %+v: %v", counts, err)
		}
	case <-time.After(time.Second):
		t.Fatal("summary waited for Project history")
	}
}

func TestSessionTodoSummaryRejectsUnknownModeByName(t *testing.T) {
	s, p, _ := directTodoServer(t)
	rec := httptest.NewRecorder()
	s.workV2Route(rec, personWorkV2Request(http.MethodGet,
		"/v1/work/v2/session-todos/conversation:"+p.s.ConversationID+"?summary=slow", "", ""))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"error":"invalid_summary"`) {
		t.Fatalf("summary mode: %d %s", rec.Code, rec.Body)
	}
}

func TestSessionTodoSummarySkipsStalledFullDependencies(t *testing.T) {
	s, p, _ := directTodoServer(t)
	for _, dependency := range []string{"project history", "images", "gate feedback"} {
		t.Run(dependency, func(t *testing.T) {
			blocked := make(chan struct{})
			entered := make(chan struct{}, 2)
			full := func() {
				entered <- struct{}{}
				<-blocked
			}
			fullDone := make(chan struct{})
			defer func() { close(blocked); <-fullDone }()
			go func() {
				s.workV2SessionTodosRead(httptest.NewRecorder(), personWorkV2Request(http.MethodGet,
					"/v1/work/v2/session-todos/conversation:"+p.s.ConversationID, "", ""), p.s.ConversationID, full)
				close(fullDone)
			}()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("full read did not reach the injected stall")
			}
			select {
			case <-fullDone:
				t.Fatal("full read did not stay stalled")
			default:
			}
			answer := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				rec := httptest.NewRecorder()
				s.workV2SessionTodosRead(rec, personWorkV2Request(http.MethodGet,
					"/v1/work/v2/session-todos/conversation:"+p.s.ConversationID+"?summary=1", "", ""),
					p.s.ConversationID, full)
				answer <- rec
			}()
			select {
			case rec := <-answer:
				if rec.Code != http.StatusOK {
					t.Fatalf("summary: %d %s", rec.Code, rec.Body)
				}
			case <-time.After(time.Second):
				t.Fatalf("summary entered stalled %s dependency", dependency)
			}
		})
	}
	called := false
	s.workV2SessionTodosRead(httptest.NewRecorder(), personWorkV2Request(http.MethodGet,
		"/v1/work/v2/session-todos/conversation:"+p.s.ConversationID, "", ""), p.s.ConversationID,
		func() { called = true })
	if !called {
		t.Fatal("the expanded operation did not enter full details")
	}
}

// Every version the store issues is positive, and app.AnyVersion is the most
// negative int64: a body that names a negative expected_version is refused
// before it reaches the store, so no route — a person's included — can name
// AnyVersion and skip its comparison.
func TestANegativeExpectedVersionIsRefused(t *testing.T) {
	s, p, v := workV2AssignmentServer(t, session.StateWorking)
	if _, err := s.assignWorkV2(context.Background(), v.Item.ID, "local", v.Item.Version,
		"existing_session", p.s.ID, "", "", nil); err != nil {
		t.Fatal(err)
	}
	any := strconv.FormatInt(app.AnyVersion, 10)
	rec := httptest.NewRecorder()
	s.workV2Route(rec, agentWorkV2Request(http.MethodPost, "/v1/work/v2/agent/items/"+v.Item.ID+"/phase",
		`{"expected_version":`+any+`,"session_id":"`+p.s.ConversationID+`","next":"implementing"}`, "neg-phase"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("negative expected_version = %d %s", rec.Code, rec.Body)
	}
	read, err := s.workV2().Item(context.Background(), v.Item.ID)
	if err != nil || read.Item.Phase == "implementing" {
		t.Fatalf("the refused write moved the item: %v %v", read.Item.Phase, err)
	}
}
