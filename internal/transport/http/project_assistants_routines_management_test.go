package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type routineHumanProvider struct {
	mu    sync.Mutex
	sends []string
	opens []app.OpenRequest
	caps  func()
}

func (p *routineHumanProvider) Capabilities(_ context.Context, b pa.Bot) (pa.Capabilities, error) {
	if p.caps != nil {
		p.caps()
	}
	c := paServerCaps(b)
	c.Resume = true
	return c, nil
}
func (p *routineHumanProvider) Open(_ context.Context, q app.OpenRequest, _ func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	p.mu.Lock()
	p.opens = append(p.opens, q)
	p.mu.Unlock()
	return &routineHumanSession{p: p, q: q}, nil
}

type routineHumanSession struct {
	p      *routineHumanProvider
	q      app.OpenRequest
	result string
}

func (s *routineHumanSession) SessionID() string {
	if s.q.Binding.SessionSlot != "" {
		return "synthetic-routine-B"
	}
	return "synthetic-main-A"
}
func (s *routineHumanSession) Send(_ context.Context, q app.PromptRequest) (app.PendingPrompt, error) {
	s.p.mu.Lock()
	s.p.sends = append(s.p.sends, s.SessionID()+":"+q.Text)
	s.p.mu.Unlock()
	s.result = "Synthetic main result"
	if s.q.Binding.SessionSlot != "" {
		s.result = "Actual isolated routine result"
	}
	return s, nil
}
func (s *routineHumanSession) Wait(context.Context) (app.PromptResult, error) {
	return app.PromptResult{FinalText: s.result}, nil
}
func (*routineHumanSession) Answer(context.Context, string, string) error { return nil }
func (*routineHumanSession) Cancel(context.Context) error                 { return nil }
func (*routineHumanSession) Close(context.Context) error                  { return nil }

func newRoutineHumanFixture(t *testing.T) (*paServerFixture, *routineHumanProvider, *httptest.Server) {
	return newRoutineHumanFixtureAt(t, nil)
}
func newRoutineHumanFixtureAt(t *testing.T, clock func() time.Time) (*paServerFixture, *routineHumanProvider, *httptest.Server) {
	f := newPAServerFixture(t, true)
	f.s.projectAssistants.clock = clock
	p := &routineHumanProvider{}
	f.s.projectAssistants.provider = func() (app.Provider, error) { f.providerCalls.Add(1); return p, nil }
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	server := httptest.NewServer(f.h)
	t.Cleanup(server.Close)
	return f, p, server
}
func routineHumanTCP(t *testing.T, f *paServerFixture, server *httptest.Server, method, path, token string, body any) (int, []byte) {
	t.Helper()
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	r, err := http.NewRequest(method, server.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		var envelope struct {
			Command contract.ProjectAssistantCommand `json:"command"`
		}
		_ = json.Unmarshal(raw, &envelope)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", envelope.Command.IdempotencyKey)
	}
	res, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, data
}
func routineHumanPage(t *testing.T, f *paServerFixture, server *httptest.Server, id, cursor string, size int) contract.ProjectAssistantRoutineListResponse {
	t.Helper()
	got := f.get(t, f.local)
	suffix := "/routines"
	if id != "" {
		suffix += "/" + id + "/runs"
	}
	path := "/v1/project-assistants/" + f.key.BotID + suffix + "?" + url.Values{"machine_id": {f.key.MachineID}, "project_id": {got.Bot.Project.ProjectID}, "canonical_id": {got.Bot.Project.CanonicalID}, "kind": {string(got.Bot.Project.Kind)}, "conversation_id": {got.Conversation.ID}, "cursor": {cursor}, "page_size": {fmt.Sprint(size)}}.Encode()
	status, raw := routineHumanTCP(t, f, server, "GET", path, f.local, nil)
	if status != 200 {
		t.Fatalf("page %d %s", status, raw)
	}
	var page contract.ProjectAssistantRoutineListResponse
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	return page
}
func routineHumanIdle(t *testing.T, f *paServerFixture) {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		if f.s.projectAssistants.host.Usage().ActiveActors == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("synthetic actor did not finish")
}

func TestProjectAssistantRoutineCronPreviewUsesSourceScheduleWithoutMutation(t *testing.T) {
	now := time.Date(2026, 11, 20, 2, 0, 0, 0, time.UTC)
	f, _, server := newRoutineHumanFixtureAt(t, func() time.Time { return now })
	original := routineHumanPage(t, f, server, "", "", 100)
	project := original.Fence.Project
	base := "/v1/project-assistants/" + f.key.BotID + "/routines/preview?"
	q := url.Values{"machine_id": {f.key.MachineID}, "project_id": {project.ProjectID}, "canonical_id": {project.CanonicalID},
		"kind": {string(project.Kind)}, "conversation_id": {original.Fence.ConversationID}, "expression": {"17 9 * * 1-5"}, "time_zone": {"Asia/Taipei"}}
	status, raw := routineHumanTCP(t, f, server, "GET", base+q.Encode(), f.local, nil)
	var reply contract.ProjectAssistantRoutinePreviewResponse
	if status != 200 || json.Unmarshal(raw, &reply) != nil || reply.NextDue != "2026-11-23T01:17:00Z" {
		t.Fatalf("preview %d %s", status, raw)
	}
	for _, tc := range []struct{ expression, zone, field string }{
		{"0 0 31 2 *", "UTC", "routine_cron_future"},
		{"99 9 * * *", "Asia/Taipei", "routine_cron"},
		{"17 9 * * 1-5", "Invalid/Zone", "routine_time_zone"},
		{strings.Repeat(" ", pa.MaxRoutinePreviewFieldBytes+1), "UTC", "routine_preview_input"},
	} {
		q.Set("expression", tc.expression)
		q.Set("time_zone", tc.zone)
		status, raw = routineHumanTCP(t, f, server, "GET", base+q.Encode(), f.local, nil)
		if status != 400 || !bytes.Contains(raw, []byte(`"field":"`+tc.field+`"`)) {
			t.Fatalf("preview refusal %d %s, want %s", status, raw, tc.field)
		}
	}
	q.Set("conversation_id", "other-conversation")
	q.Set("expression", "17 9 * * 1-5")
	status, _ = routineHumanTCP(t, f, server, "GET", base+q.Encode(), f.local, nil)
	if status == 200 {
		t.Fatal("preview crossed the captured conversation")
	}
	after := routineHumanPage(t, f, server, "", "", 100)
	if f.providerCalls.Load() != 0 || original.Fence != after.Fence || !reflect.DeepEqual(original.Definitions, after.Definitions) || !reflect.DeepEqual(original.Runs, after.Runs) {
		t.Fatalf("preview touched routine definitions or started a provider: %+v", after)
	}
}

func TestProjectAssistantRoutineHumanTCPManagement(t *testing.T) {
	f, p, server := newRoutineHumanFixture(t)
	base := "/v1/project-assistants/" + f.key.BotID + "/routines"
	page := routineHumanPage(t, f, server, "", "", 100)
	if f.providerCalls.Load() != 0 {
		t.Fatal("list initialized a provider")
	}
	q := contract.ProjectAssistantRoutineSaveRequest{Command: f.command(pa.EditOperation, "human-save"), Fence: page.Fence, Name: "Synthetic interval", Instruction: "Synthetic routine task", Triggers: []contract.ProjectAssistantRoutineTrigger{{Type: "interval", Seconds: 3600}}, TimeoutSeconds: 3600, Enabled: true}
	status, raw := routineHumanTCP(t, f, server, "POST", base, f.local, q)
	if status != 200 {
		t.Fatalf("save %d %s", status, raw)
	}
	status, replay := routineHumanTCP(t, f, server, "POST", base, f.local, q)
	if status != 200 || !bytes.Equal(raw, replay) {
		t.Fatal("save replay changed", status, string(replay))
	}
	changed := q
	changed.Name = "Different intent"
	status, raw = routineHumanTCP(t, f, server, "POST", base, f.local, changed)
	if status != 409 || !bytes.Contains(raw, []byte(pa.IdempotencyConflict)) {
		t.Fatal("changed command accepted", status, string(raw))
	}
	p.mu.Lock()
	sends := len(p.sends)
	p.mu.Unlock()
	if sends != 0 {
		t.Fatal("save sent a native prompt")
	}
	page = routineHumanPage(t, f, server, "", "", 100)
	if len(page.Definitions) != 1 {
		t.Fatal(page)
	}
	d := page.Definitions[0]
	q.ID = d.ID
	q.ExpectedRevision = d.Version
	q.Command = f.command(pa.EditOperation, "human-edit")
	q.Name = "Edited interval"
	status, raw = routineHumanTCP(t, f, server, "POST", base, f.local, q)
	if status != 200 {
		t.Fatalf("edit %d %s", status, raw)
	}
	q.Command = f.command(pa.EditOperation, "human-stale")
	status, raw = routineHumanTCP(t, f, server, "POST", base, f.local, q)
	if status != 409 || !bytes.Contains(raw, []byte(pa.StaleVersion)) {
		t.Fatal("stale edit accepted", status, string(raw))
	}
	paServerOK(t, f.send(t, f.local, "human-main-before"))
	routineHumanIdle(t, f)
	page = routineHumanPage(t, f, server, "", "", 100)
	d = page.Definitions[0]
	test := contract.ProjectAssistantRoutineActionRequest{Command: f.command(pa.EditOperation, "human-test"), Fence: page.Fence, ID: d.ID, ExpectedRevision: d.Version}
	status, raw = routineHumanTCP(t, f, server, "POST", base+"/"+d.ID+"/runs", f.local, test)
	if status != 200 {
		t.Fatalf("test %d %s", status, raw)
	}
	status, replay = routineHumanTCP(t, f, server, "POST", base+"/"+d.ID+"/runs", f.local, test)
	if status != 200 || !bytes.Equal(raw, replay) {
		t.Fatal("test replay changed", status, string(replay))
	}
	until := time.Now().Add(5 * time.Second)
	for {
		page = routineHumanPage(t, f, server, d.ID, "", 100)
		if len(page.Runs) == 1 && page.Runs[0].State == "succeeded" {
			break
		}
		if time.Now().After(until) {
			t.Fatal("test not completed", page)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if page.Runs[0].Result != "Actual isolated routine result" {
		t.Fatal("history is not the stored result", page.Runs)
	}
	paServerOK(t, f.send(t, f.local, "human-main-after"))
	routineHumanIdle(t, f)
	page = routineHumanPage(t, f, server, "", "", 100)
	d = page.Definitions[0]
	for _, enabled := range []bool{false, true} {
		q := contract.ProjectAssistantRoutineEnabledRequest{Command: f.command(pa.EditOperation, fmt.Sprint("human-enabled-", enabled)), Fence: page.Fence, ID: d.ID, ExpectedRevision: d.Version, Enabled: enabled}
		status, raw = routineHumanTCP(t, f, server, "POST", base+"/"+d.ID+"/enabled", f.local, q)
		if status != 200 {
			t.Fatalf("enabled %d %s", status, raw)
		}
		page = routineHumanPage(t, f, server, "", "", 100)
		d = page.Definitions[0]
		if d.Enabled != enabled {
			t.Fatal(d)
		}
	}
	del := contract.ProjectAssistantRoutineActionRequest{Command: f.command(pa.EditOperation, "human-delete"), Fence: page.Fence, ID: d.ID, ExpectedRevision: d.Version}
	status, raw = routineHumanTCP(t, f, server, "DELETE", base+"/"+d.ID, f.local, del)
	if status != 200 {
		t.Fatalf("delete %d %s", status, raw)
	}
	if page = routineHumanPage(t, f, server, "", "", 100); len(page.Definitions) != 0 {
		t.Fatal("deleted definition listed")
	}
	if page = routineHumanPage(t, f, server, d.ID, "", 100); len(page.Runs) != 1 {
		t.Fatal("deletion lost history")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.sends) != 3 || !strings.HasPrefix(p.sends[0], "synthetic-main-A:") || !strings.HasPrefix(p.sends[1], "synthetic-routine-B:") || !strings.HasPrefix(p.sends[2], "synthetic-main-A:") || !p.opens[len(p.opens)-1].Resume {
		t.Fatal("main/run/main isolation changed", p.sends, p.opens)
	}
	t.Log("real TCP + production Server/Host/SQLite: create/edit/test/result/pause/enable/delete/history; save/list prompts=0; scripted sends=3; native/model requests=0")
}

func TestProjectAssistantRoutineHumanBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_ROUTINE_HUMAN_BROWSER_DIR")
	if dir == "" {
		t.Skip("explicit synthetic browser acceptance only")
	}
	f, p, server := newRoutineHumanFixture(t)
	paServerOK(t, f.send(t, f.local, "browser-main-before"))
	routineHumanIdle(t, f)
	cmd := exec.Command("node", "../../../web/console/tools/project-assistants-routines.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+server.URL, "BOTS_TOKEN="+f.local, "BOTS_MACHINE="+f.key.MachineID, "BOTS_BOT="+f.key.BotID, "BOTS_ARTIFACTS="+dir)
	log, err := cmd.CombinedOutput()
	_ = os.WriteFile(filepath.Join(dir, "browser.log"), log, 0600)
	if err != nil {
		t.Fatalf("browser %v\n%s", err, log)
	}
	paServerOK(t, f.send(t, f.local, "browser-main-after"))
	routineHumanIdle(t, f)
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.sends) != 4 || !strings.HasPrefix(p.sends[0], "synthetic-main-A:") || !strings.HasPrefix(p.sends[3], "synthetic-main-A:") {
		t.Fatal("browser changed chat admission", p.sends)
	}
	evidence := map[string]any{"fixture": "production Chat and SourceClient against an isolated HTTP source", "scripted_sends": p.sends, "external_model_requests": 0, "native_requests": 0, "tcp": true, "production_manager": true}
	raw, _ := json.MarshalIndent(evidence, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "http-browser.json"), append(raw, '\n'), 0600)
}

func TestProjectAssistantRoutineHumanDenialsAndPublication(t *testing.T) {
	f, p, server := newRoutineHumanFixture(t)
	base := "/v1/project-assistants/" + f.key.BotID + "/routines"
	page := routineHumanPage(t, f, server, "", "", 100)
	q := contract.ProjectAssistantRoutineSaveRequest{Command: f.command(pa.EditOperation, "denied-save"), Fence: page.Fence, Name: "Denied fixture", Instruction: "Never send", Triggers: []contract.ProjectAssistantRoutineTrigger{{Type: "interval", Seconds: 3600}}, TimeoutSeconds: 3600, Enabled: true}
	status, raw := routineHumanTCP(t, f, server, "POST", base, f.readonly, q)
	if status != 403 {
		t.Fatal("read-only saved", status, string(raw))
	}
	encoded, _ := json.Marshal(q)
	for _, malformed := range []string{
		strings.Replace(string(encoded), `"name":`, `"Name":`, 1),
		strings.Replace(string(encoded), `"timeout_seconds":3600`, `"timeout_seconds":0`, 1),
		strings.Replace(string(encoded), `"name":"Denied fixture"`, `"name":null`, 1),
		strings.Replace(string(encoded), `"name":"Denied fixture"`, `"name":"Denied fixture","name":"Duplicate"`, 1),
		strings.Replace(string(encoded), `"seconds":3600`, `"seconds":3600,"credential":"forged"`, 1),
	} {
		paAssertHTTPFailure(t, f.request(t, "POST", base, f.local, malformed), 400, pa.Invalid)
	}
	// Revocation during supported-engine inspection must precede the SQL effect.
	p.caps = func() {
		p.caps = nil
		id := f.s.gate().auth.Verify(f.lan).Device
		if err := f.s.gate().auth.Revoke(id); err != nil {
			t.Fatal(err)
		}
	}
	status, raw = routineHumanTCP(t, f, server, "POST", base, f.lan, q)
	if status != 403 {
		t.Fatal("revoked preparation saved", status, string(raw))
	}
	if page = routineHumanPage(t, f, server, "", "", 100); len(page.Definitions) != 0 {
		t.Fatal("revoked write persisted")
	}
	q.Command = f.command(pa.EditOperation, "visible-save")
	status, raw = routineHumanTCP(t, f, server, "POST", base, f.local, q)
	if status != 200 {
		t.Fatal(status, string(raw))
	}
	got := f.get(t, f.local)
	path := base + "?" + url.Values{"machine_id": {f.key.MachineID}, "project_id": {got.Bot.Project.ProjectID}, "canonical_id": {got.Bot.Project.CanonicalID}, "kind": {string(got.Bot.Project.Kind)}, "conversation_id": {got.Conversation.ID}, "cursor": {""}, "page_size": {"100"}}.Encode()
	f.s.projectAssistants.beforeViewPublish = func() {
		id := f.s.gate().auth.Verify(f.readonly).Device
		if id != "" {
			_ = f.s.gate().auth.Revoke(id)
		}
	}
	status, raw = routineHumanTCP(t, f, server, "GET", path, f.readonly, nil)
	if status != 403 || bytes.Contains(raw, []byte("Denied fixture")) {
		t.Fatal("late revoked read exposed bytes", status, string(raw))
	}
	f.s.projectAssistants.beforeViewPublish = nil
	status, raw = routineHumanTCP(t, f, server, "GET", strings.Replace(path, "canonical_id=", "canonical_id=wrong-", 1), f.local, nil)
	if status != 403 {
		t.Fatal("wrong Project exposed rows", status, string(raw))
	}
	// Installed disabled history remains readable, while every write is denied.
	paServerOK(t, f.request(t, "POST", "/v1/settings/project-assistants", f.local, `{"enabled":false}`))
	status, raw = routineHumanTCP(t, f, server, "GET", path, f.local, nil)
	if status != 200 || !bytes.Contains(raw, []byte(`"access":"history_only"`)) {
		t.Fatal("disabled history unavailable", status, string(raw))
	}
	status, raw = routineHumanTCP(t, f, server, "POST", base, f.local, q)
	if status != 403 {
		t.Fatal("disabled source accepted replay", status, string(raw))
	}
}

func TestProjectAssistantRoutineHumanPreservesOnceORAndMetadata(t *testing.T) {
	var clock atomic.Int64
	clock.Store(time.Now().UnixMilli())
	f, p, server := newRoutineHumanFixtureAt(t, func() time.Time { return time.UnixMilli(clock.Load()) })
	base := "/v1/project-assistants/" + f.key.BotID + "/routines"
	page := routineHumanPage(t, f, server, "", "", 100)
	q := contract.ProjectAssistantRoutineSaveRequest{Command: f.command(pa.EditOperation, "or-save"), Fence: page.Fence, Name: "OR fixture", Instruction: "No prompt on edits", Triggers: []contract.ProjectAssistantRoutineTrigger{{Type: "once", At: time.Now().Add(time.Hour).UnixMilli() + 123}, {Type: "cron", Expression: "0 9 * * 1-5", TimeZone: "Asia/Taipei"}, {Type: "interval", Seconds: 3600}}, TimeoutSeconds: 3600, Enabled: false}
	status, raw := routineHumanTCP(t, f, server, "POST", base, f.local, q)
	if status != 200 {
		t.Fatal(status, string(raw))
	}
	page = routineHumanPage(t, f, server, "", "", 100)
	d := page.Definitions[0]
	st := store.New(f.s.store.ProjectAssistantDatabase())
	original, err := st.RoutineDefinition(context.Background(), f.key, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	// An unchanged past once timestamp is legal on an unrelated edit. Advance
	// only the controlled Host clock; this paused fixture cannot wake a prompt.
	clock.Add(int64(2 * time.Hour / time.Millisecond))
	q.ID = d.ID
	q.ExpectedRevision = d.Version
	q.Command = f.command(pa.EditOperation, "or-name-edit")
	q.Name = "Name changed"
	status, raw = routineHumanTCP(t, f, server, "POST", base, f.local, q)
	if status != 200 {
		t.Fatal(status, string(raw))
	}
	after, err := st.RoutineDefinition(context.Background(), f.key, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original.Triggers, after.Triggers) || !reflect.DeepEqual(original.NextRuns, after.NextRuns) || !original.NextDue.Equal(after.NextDue) {
		t.Fatal("name edit altered exact deadlines", original, after)
	}
	page = routineHumanPage(t, f, server, "", "", 100)
	d = page.Definitions[0]
	calls := f.providerCalls.Load()
	// Metadata admission does not inspect the provider or acquire a process.
	p.caps = func() { t.Error("metadata inspected native capabilities") }
	paused := contract.ProjectAssistantRoutineEnabledRequest{Command: f.command(pa.EditOperation, "metadata-pause"), Fence: page.Fence, ID: d.ID, ExpectedRevision: d.Version, Enabled: false}
	status, raw = routineHumanTCP(t, f, server, "POST", base+"/"+d.ID+"/enabled", f.local, paused)
	if status != 200 {
		t.Fatal(status, string(raw))
	}
	page = routineHumanPage(t, f, server, "", "", 100)
	d = page.Definitions[0]
	del := contract.ProjectAssistantRoutineActionRequest{Command: f.command(pa.EditOperation, "metadata-delete"), Fence: page.Fence, ID: d.ID, ExpectedRevision: d.Version}
	status, raw = routineHumanTCP(t, f, server, "DELETE", base+"/"+d.ID, f.local, del)
	if status != 200 || calls != f.providerCalls.Load() {
		t.Fatal("metadata started provider", status, string(raw))
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.sends) != 0 {
		t.Fatal("metadata or save sent prompt")
	}
}

func TestProjectAssistantRoutineHumanReplyPacksWholeResults(t *testing.T) {
	out := contract.ProjectAssistantRoutineListResponse{ContractVersion: pa.ContractVersion, Definitions: []contract.ProjectAssistantRoutineDefinition{}, Runs: []contract.ProjectAssistantRoutineRun{}}
	cursors := []string{}
	text := strings.Repeat("\x01", pa.MaxMessageBytes)
	for i := 0; i < pa.MaxPageRows; i++ {
		out.Runs = append(out.Runs, contract.ProjectAssistantRoutineRun{ID: fmt.Sprint(i), Result: text})
		cursors = append(cursors, fmt.Sprint("source-signed-position-", i))
	}
	raw, err := paPackRoutinePage(out, cursors)
	if err != nil {
		t.Fatal(err)
	}
	size, err := cloudops.ProjectAssistantReadReplySize("read:"+strings.Repeat("<", 128), 200, raw)
	if err != nil || size > cloudops.ReadPlaintextBytesLimit {
		t.Fatal("reply exceeded its actual envelope", size, err)
	}
	var got contract.ProjectAssistantRoutineListResponse
	if err = json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Runs) == 0 || len(got.Runs) >= len(out.Runs) || got.NextCursor != cursors[len(got.Runs)-1] {
		t.Fatal("reply lost its honest prefix", len(got.Runs), got.NextCursor)
	}
	for _, run := range got.Runs {
		if run.Result != text {
			t.Fatal("result truncated")
		}
	}
}
