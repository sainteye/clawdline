package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/icon"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/transport/cloud"
)

// The two words a hosted page could not ask this Mac for, end to end and in
// one process: the plaintext the console seals, decoded by the bridge, carried
// by the in-process router with this daemon's own credentials stamped on it —
// and therefore through the gate, the capability checks and the routes a
// paired browser on this machine's own network reaches.
//
// Nothing here is a fake but the transport: the store is real, the gate is
// real, the schedule files are written and read back through the parser, and
// the places are a recorded `~/.claude/projects` folder under a made-up home.

// standIn is that whole stack, plus the place the hosted page would name.
type standIn struct {
	bridge  cloudops.Bridge
	handler http.Handler
	server  *Server
	machine string
	place   string
}

func cloudStandIn(t *testing.T) *standIn {
	t.Helper()
	home := placesHome(t)
	project := filepath.Join(home, "code", "clawdline-go")
	recordPlace(t, home, project)
	recordConversation(t, home, project, "c6000003-0000-4000-8000-000000000003", "look at the overnight runs")
	state := filepath.Join(home, ".config", "clawdline-next")
	t.Setenv("CLAWDLINE_SWIFT_DIR", filepath.Join(home, "swift"))
	// Task dispatch off, so `schedule-run` reaches this machine's own route
	// and is answered by it without opening anybody a terminal.
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, nextconfig.FileName),
		[]byte(`{"orchestrator_enabled": false}`), 0o600); err != nil {
		t.Fatal(err)
	}

	st, err := store.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := &Server{cfg: config.Config{Dir: state, Port: 7757}, store: st, icons: &icon.Registry{},
		broker: &orchestrator.Broker{Store: st, Tasks: taskdir.New(state), Dir: state}}
	t.Cleanup(func() { scheduleBooks.Delete(state) })

	place := ""
	for _, p := range s.projectReaders().places.List(nil, 40) {
		if p.Path == project {
			place = p.ID
		}
	}
	if place == "" {
		t.Fatalf("the recorded project is not one of this machine's places")
	}
	handler := s.Handler()
	local, machineToken, err := s.CloudCredentials()
	if err != nil {
		t.Fatal(err)
	}
	return &standIn{
		bridge: cloudops.Bridge{MachineID: "mac-01",
			Router:        cloud.Router{Handler: handler, Authorize: cloud.LocalAuthorizer(local, machineToken)},
			AllowCommands: func() bool { return true }},
		handler: handler,
		server:  s,
		machine: machineToken,
		place:   place,
	}
}

// recordConversation is one transcript somebody had in dir: a user turn with
// words in it, which is what makes a `.jsonl` a conversation the resume list
// offers rather than a file it steps over.
func recordConversation(t *testing.T, home, dir, id, said string) {
	t.Helper()
	folder := filepath.Join(home, ".claude", "projects", slugOf(dir))
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	line, err := json.Marshal(map[string]any{"type": "user", "cwd": dir,
		"message": map[string]any{"role": "user", "content": said}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, id+".jsonl"), append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ask seals nothing and decrypts nothing: it hands the bridge the plaintext a
// viewer's envelope would have carried.
func (s *standIn) ask(t *testing.T, seq uint64, body map[string]any) (cloudops.Answer, map[string]any) {
	t.Helper()
	plaintext, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	answer := s.bridge.Handle(context.Background(), cloudops.Command{
		Channel: "ctl/mac-01", Class: cloudops.ClassCtl, Sender: "web_viewer-01",
		Sequence: seq, Plaintext: plaintext})
	var payload map[string]any
	if len(answer.Payload) > 0 {
		if err := json.Unmarshal(answer.Payload, &payload); err != nil {
			t.Fatalf("payload %s: %v", answer.Payload, err)
		}
	}
	return answer, payload
}

func bodyOf(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	out, ok := payload["body"].(map[string]any)
	if !ok {
		t.Fatalf("that answer carries no body: %v", payload)
	}
	return out
}

// dailySchedule is the form's own fields for one that runs every day, which is
// the case this whole exercise is about.
func dailySchedule(place, title, at string) map[string]any {
	return map[string]any{"title": title, "at": at, "days": []any{"mon", "tue", "wed", "thu", "fri"},
		"place_id": place, "assistant": "claude", "instructions": "read the overnight runs"}
}

// TestACloudViewerCanListTheEarlierSessionsOfAPlace: the resume list the
// hosted console draws, answered by this Mac instead of refused as a word it
// does not know.
func TestACloudViewerCanListTheEarlierSessionsOfAPlace(t *testing.T) {
	s := cloudStandIn(t)

	answer, payload := s.ask(t, 1, map[string]any{"type": "past-sessions",
		"session": cloudops.MachineReplySession, "request": "req-past",
		"place": s.place, "assistant": ""})
	if !answer.OK() {
		t.Fatalf("the resume list was refused: %d/%q %s", answer.Status, answer.Code, answer.Payload)
	}
	body := bodyOf(t, payload)
	if body["place"] != s.place {
		t.Fatalf("the answer is about %v, not the place that was asked for", body["place"])
	}
	rows, ok := body["sessions"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("the recorded conversation is not in the list: %v", body)
	}

	// And a place this machine does not have is the route's own refusal,
	// carried with its own code — not silence, and not a shrug.
	answer, payload = s.ask(t, 2, map[string]any{"type": "past-sessions",
		"session": cloudops.MachineReplySession, "request": "req-nowhere",
		"place": "place-nobody-has", "assistant": ""})
	if answer.Status != http.StatusNotFound || answer.Code != "not_found" {
		t.Fatalf("answered %d/%q, wanted 404/not_found", answer.Status, answer.Code)
	}
	failure, _ := payload["error"].(map[string]any)
	if failure["layer"] != "mac_route" {
		t.Fatalf("the refusal was not the route's: %v", failure)
	}
}

// TestACloudViewerArrangesARepeatingSchedule is the judgement this task turns
// on, made where it can be seen fail.
//
// A schedule that runs every day is a standing arrangement of this machine's
// work, so changing it is a person's act. `MachineRefusal` keeps this Mac's
// own orchestrator token to schedules that run **once** for exactly that
// reason — an automation that could rewrite a daily arrangement could quietly
// rewrite what it itself does every day. A paired Cloud viewer is not that
// automation: it is the person, on the account's roster, with the Mac's
// remote-write switch on, and over this machine's own network the same person
// already arranges repeating schedules. So the same body is:
//
//   - made, saved, run and removed when it comes from the viewer;
//   - refused, in the sentence that names the door they should use, when it
//     comes from this machine's orchestrator token.
func TestACloudViewerArrangesARepeatingSchedule(t *testing.T) {
	s := cloudStandIn(t)

	answer, payload := s.ask(t, 1, map[string]any{"type": "schedule-create",
		"session": cloudops.MachineReplySession, "request": "req-make",
		"schedule": dailySchedule(s.place, "morning sweep", "09:00")})
	if !answer.OK() {
		t.Fatalf("the viewer could not make a repeating schedule: %d/%q %s",
			answer.Status, answer.Code, answer.Payload)
	}
	made, _ := bodyOf(t, payload)["schedule"].(map[string]any)
	id, _ := made["id"].(string)
	if id == "" {
		t.Fatalf("the answer names no schedule: %v", payload)
	}

	// The same key again is the same answer, not a second schedule.
	again, payloadAgain := s.ask(t, 2, map[string]any{"type": "schedule-create",
		"session": cloudops.MachineReplySession, "request": "req-make",
		"schedule": dailySchedule(s.place, "morning sweep", "09:00")})
	if !again.OK() {
		t.Fatalf("the retry was refused: %d/%q", again.Status, again.Code)
	}
	repeated, _ := bodyOf(t, payloadAgain)["schedule"].(map[string]any)
	if repeated["id"] != id {
		t.Fatalf("the retry made a second schedule: %v then %v", id, repeated["id"])
	}

	// Saving it moves when it runs, and it still repeats.
	answer, _ = s.ask(t, 3, map[string]any{"type": "schedule-update",
		"session": cloudops.MachineReplySession, "request": "req-save", "id": id,
		"schedule": dailySchedule(s.place, "morning sweep", "10:30")})
	if !answer.OK() {
		t.Fatalf("the viewer could not save it: %d/%q %s", answer.Status, answer.Code, answer.Payload)
	}

	// Running it now reaches the route rather than the vocabulary: task
	// dispatch is off in this state directory, so the route says so itself.
	answer, payload = s.ask(t, 4, map[string]any{"type": "schedule-run",
		"session": cloudops.MachineReplySession, "request": "req-now", "id": id})
	if answer.Code != "orchestrator_disabled" || answer.Status != http.StatusForbidden {
		t.Fatalf("answered %d/%q, wanted 403/orchestrator_disabled", answer.Status, answer.Code)
	}
	if failure, _ := payload["error"].(map[string]any); failure["layer"] != "mac_route" {
		t.Fatalf("the refusal was not the route's: %v", failure)
	}

	// And taking it away.
	answer, payload = s.ask(t, 5, map[string]any{"type": "schedule-delete",
		"session": cloudops.MachineReplySession, "request": "req-gone", "id": id})
	if !answer.OK() {
		t.Fatalf("the viewer could not remove it: %d/%q %s", answer.Status, answer.Code, answer.Payload)
	}
	if bodyOf(t, payload)["deleted"] != id {
		t.Fatalf("something else was removed: %v", payload)
	}

	// The other door, unchanged: this machine's own token still may not make
	// a schedule that repeats.
	rec := s.orchestratorCreate(t, dailySchedule(s.place, "morning sweep", "09:00"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("the orchestrator token made a repeating schedule: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "A repeating schedule is arranged by a person") {
		t.Fatalf("the refusal lost its reason: %s", rec.Body)
	}
}

// orchestratorCreate is the same request from this machine's own automation:
// the orchestrator token, and no word from the request about being anybody's
// device.
func (s *standIn) orchestratorCreate(t *testing.T, schedule map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return s.orchestratorWrite(t, http.MethodPost, "/v1/orchestrator/schedules", "cron-1", schedule)
}

func (s *standIn) orchestratorWrite(t *testing.T, method, path, key string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, "http://127.0.0.1:7757"+path,
		strings.NewReader(string(raw)))
	req.Host = "127.0.0.1:7757"
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	req.Header.Set(machineHeader, s.machine)
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec
}

// A repeating schedule is a standing instruction, so the machine-wide
// orchestrator token alone still cannot make one. A session can carry the
// person's explicit instruction under the run issued when that person sent
// the session a message through Clawdline. The run must belong to the same
// conversation, and neither proof field becomes part of the stored schedule.
func TestASessionCanArrangeARepeatingScheduleOnAPersonsRun(t *testing.T) {
	s := cloudStandIn(t)
	const conversation = "c6000003-0000-4000-8000-000000000003"
	run, err := s.server.runs().Issue(context.Background(), session.Session{
		ID: "terminal-3", ConversationID: conversation,
	}, "local", "")
	if err != nil {
		t.Fatal(err)
	}
	body := dailySchedule(s.place, "morning sweep", "09:00")
	body["session_id"] = conversation
	body["via"] = map[string]any{"run": run.ID}

	rec := s.orchestratorWrite(t, http.MethodPost, "/v1/orchestrator/schedules", "session-cron-1", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("the person's instruction was refused: %d %s", rec.Code, rec.Body)
	}
	var made struct {
		Schedule struct {
			ID string `json:"id"`
		} `json:"schedule"`
	}
	if json.Unmarshal(rec.Body.Bytes(), &made) != nil || made.Schedule.ID == "" {
		t.Fatalf("the answer names no schedule: %s", rec.Body)
	}
	detail := call{method: http.MethodGet, path: "/v1/orchestrator/schedules/" + made.Schedule.ID,
		headers: map[string]string{machineHeader: s.machine}}.do(s.handler)
	if detail.Code != http.StatusOK || strings.Contains(detail.Body.String(), "session_id") ||
		strings.Contains(detail.Body.String(), `"via"`) {
		t.Fatalf("authorization leaked into the schedule: %d %s", detail.Code, detail.Body)
	}

	wrong := dailySchedule(s.place, "wrong conversation", "10:00")
	wrong["session_id"] = "c6000004-0000-4000-8000-000000000004"
	wrong["via"] = map[string]any{"run": run.ID}
	refused := s.orchestratorWrite(t, http.MethodPost, "/v1/orchestrator/schedules", "session-cron-2", wrong)
	if refused.Code != http.StatusForbidden || !strings.Contains(refused.Body.String(), "run_other_session") {
		t.Fatalf("another conversation used the run: %d %s", refused.Code, refused.Body)
	}

	malformed := dailySchedule(s.place, "missing proof", "10:00")
	malformed["session_id"] = conversation
	refused = s.orchestratorWrite(t, http.MethodPost, "/v1/orchestrator/schedules", "session-cron-3", malformed)
	if refused.Code != http.StatusBadRequest || !strings.Contains(refused.Body.String(), "invalid_user_authorization") {
		t.Fatalf("partial proof was accepted or misreported: %d %s", refused.Code, refused.Body)
	}

	changed := dailySchedule(s.place, "later sweep", "10:30")
	changed["session_id"] = conversation
	changed["via"] = map[string]any{"run": run.ID}
	updated := s.orchestratorWrite(t, http.MethodPatch,
		"/v1/orchestrator/schedules/"+made.Schedule.ID, "session-cron-4", changed)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"title":"later sweep"`) {
		t.Fatalf("the person's change was refused: %d %s", updated.Code, updated.Body)
	}

	proof := map[string]any{"session_id": conversation, "via": map[string]any{"run": run.ID}}
	deleted := s.orchestratorWrite(t, http.MethodDelete,
		"/v1/orchestrator/schedules/"+made.Schedule.ID, "session-cron-5", proof)
	if deleted.Code != http.StatusOK || !strings.Contains(deleted.Body.String(), made.Schedule.ID) {
		t.Fatalf("the person's removal was refused: %d %s", deleted.Code, deleted.Body)
	}
}

// TestTheActorHeaderOnlyEverTakesAuthorityAway is why the gate may honour a
// header any caller can set. It closes the machine door and opens nothing: a
// caller holding only the orchestrator token loses every route behind it, and
// a caller holding a device that may only read gains nothing on the way.
func TestTheActorHeaderOnlyEverTakesAuthorityAway(t *testing.T) {
	f, h := newGateFixture(t)
	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    int
	}{{
		name: "the orchestrator token alone reaches an orchestrator route",
		headers: map[string]string{"X-Clawdline-Orchestrator": f.machine,
			"Content-Type": "application/json"},
		want: http.StatusOK,
	}, {
		name: "and reaches nothing once it says it is a device",
		headers: map[string]string{"X-Clawdline-Orchestrator": f.machine,
			actorHeader: actorDevice, "Content-Type": "application/json"},
		want: http.StatusUnauthorized,
	}, {
		name: "a device that may only read is still refused with it",
		headers: map[string]string{"Authorization": "Bearer " + f.read,
			"X-Clawdline-Orchestrator": f.machine, actorHeader: actorDevice,
			"Content-Type": "application/json"},
		want: http.StatusForbidden,
	}, {
		name: "a device that may send is let through, as it is without it",
		headers: map[string]string{"Authorization": "Bearer " + f.send,
			"X-Clawdline-Orchestrator": f.machine, actorHeader: actorDevice,
			"Content-Type": "application/json"},
		want: http.StatusOK,
	}} {
		rec := call{path: "/v1/orchestrator/schedules", body: `{}`, headers: tc.headers}.do(h)
		if rec.Code != tc.want {
			t.Errorf("%s: %d %s, want %d", tc.name, rec.Code, rec.Body, tc.want)
		}
	}
}

// TestACloudMoveCarriesTheTemplateToTheStoredFile: a schedule moved through
// Clawdline Cloud keeps its hidden settings end to end. Every layer between
// the browser and the file — the Cloud word, the local route, the idempotent
// receipt, `build` — would drop `deliverables` without a word if it reshaped
// the body, so the test reads the stored schedule back rather than the answer.
func TestACloudMoveCarriesTheTemplateToTheStoredFile(t *testing.T) {
	s := cloudStandIn(t)

	body := dailySchedule(s.place, "morning sweep", "09:00")
	body["template"] = map[string]any{"deliverables": []any{"docs/report.md"}, "claims": []any{"web"}}
	answer, payload := s.ask(t, 1, map[string]any{"type": "schedule-create",
		"session": cloudops.MachineReplySession, "request": "req-move", "schedule": body})
	if !answer.OK() {
		t.Fatalf("the moved copy was refused: %d/%q %s", answer.Status, answer.Code, answer.Payload)
	}
	made, _ := bodyOf(t, payload)["schedule"].(map[string]any)
	id, _ := made["id"].(string)

	answer, payload = s.ask(t, 2, map[string]any{"type": "schedule",
		"session": cloudops.MachineReplySession, "request": "req-read", "id": id})
	if !answer.OK() {
		t.Fatalf("the copy could not be read back: %d/%q", answer.Status, answer.Code)
	}
	record, _ := bodyOf(t, payload)["schedule"].(map[string]any)
	task, _ := record["task"].(map[string]any)
	if fmt.Sprint(task["deliverables"]) != "[docs/report.md]" || fmt.Sprint(task["claims"]) != "[web]" {
		t.Fatalf("the stored task lost its template: %v", task)
	}

	// And the permission setting stays a named refusal on this door too.
	body["template"] = map[string]any{"permission_mode": "full"}
	answer, _ = s.ask(t, 3, map[string]any{"type": "schedule-create",
		"session": cloudops.MachineReplySession, "request": "req-wider", "schedule": body})
	if answer.Code != "template_permission_mode" {
		t.Fatalf("a template naming permission_mode answered %d/%q", answer.Status, answer.Code)
	}
}

// TestACloudMoveCarriesThePermissionAsTheFormField: a Cloud write is a
// person's device, so the move's copy may carry the source's permission, as
// the form field, and the stored file says it.
func TestACloudMoveCarriesThePermissionAsTheFormField(t *testing.T) {
	s := cloudStandIn(t)

	body := dailySchedule(s.place, "full sweep", "09:00")
	body["permission_mode"] = "full"
	body["template"] = map[string]any{"claims": []any{"web"}}
	answer, payload := s.ask(t, 1, map[string]any{"type": "schedule-create",
		"session": cloudops.MachineReplySession, "request": "req-move-full", "schedule": body})
	if !answer.OK() {
		t.Fatalf("the moved copy was refused: %d/%q %s", answer.Status, answer.Code, answer.Payload)
	}
	made, _ := bodyOf(t, payload)["schedule"].(map[string]any)
	id, _ := made["id"].(string)
	answer, payload = s.ask(t, 2, map[string]any{"type": "schedule",
		"session": cloudops.MachineReplySession, "request": "req-read-full", "id": id})
	if !answer.OK() {
		t.Fatalf("the copy could not be read back: %d/%q", answer.Status, answer.Code)
	}
	record, _ := bodyOf(t, payload)["schedule"].(map[string]any)
	task, _ := record["task"].(map[string]any)
	if task["permission_mode"] != "full" || fmt.Sprint(task["claims"]) != "[web]" {
		t.Fatalf("the stored task: %v, want permission_mode full and the claims", task)
	}
}
