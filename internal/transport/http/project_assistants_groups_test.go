package http

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	_ "modernc.org/sqlite"
)

type groupPersonContext struct{}
type groupHTTPProjects struct{ dir string }

func (p groupHTTPProjects) Resolve(_ context.Context, q pa.ProjectSelection) (pa.ProjectResolution, error) {
	return pa.ProjectResolution{Scope: pa.ProjectScope{MachineID: q.MachineID, ProjectID: q.ProjectID, CanonicalID: "canonical-" + q.ProjectID, Kind: pa.RepositoryProject}, Directory: p.dir, Available: true, Current: true}, nil
}

type groupHTTPFixture struct {
	groupID       string
	server        *httptest.Server
	service       *app.Service
	repo          *store.Store
	rooms         *app.RoomDriver
	engine        *app.Engine
	bots          []pa.Bot
	mu            sync.Mutex
	revision      string
	revoked       bool
	beforePublish func()
}

func newGroupHTTPFixture(t *testing.T, providers ...app.Provider) *groupHTTPFixture {
	t.Helper()
	f := &groupHTTPFixture{revision: "grant-one"}
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "groups.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if err = app.InstallSchemas(t.Context(), db, store.Installer{}, store.GroupInstaller{}); err != nil {
		t.Fatal(err)
	}
	f.repo = store.New(db)
	views, err := store.NewReadViews(db)
	if err != nil {
		t.Fatal(err)
	}
	groups, err := app.NewGroupRooms(store.NewGroupStore(db), app.DefaultGroupLimits(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	var ids atomic.Int64
	id := func() string { return fmt.Sprintf("group-http-%d", ids.Add(1)) }
	projects := groupHTTPProjects{t.TempDir()}
	authorize := func(ctx context.Context, key pa.BotKey, write bool) (app.Authority, error) {
		f.mu.Lock()
		revision, revoked := f.revision, f.revoked
		f.mu.Unlock()
		if revoked {
			return app.Authority{}, paHTTPFailure(pa.Revoked, "device")
		}
		token, _ := ctx.Value(groupPersonContext{}).(string)
		source := pa.SourceStatus{MachineID: key.MachineID, State: pa.SourceOnline, Fresh: true, Readable: true, Writable: token != "reader", Access: pa.Interactive}
		return app.Authority{PrincipalID: "person", ReaderID: "person", AuthorizationRevision: revision, Source: source, Feature: pa.FeatureState{Enabled: true}}, nil
	}
	background := func(ctx context.Context, key pa.BotKey) (pa.FeatureState, pa.SourceStatus, error) {
		a, e := authorize(ctx, key, true)
		return a.Feature, a.Source, e
	}
	f.rooms, err = app.NewRoomDriver(app.RoomDriverOptions{Rooms: groups, Bots: f.repo, Authorize: background, Limits: app.DefaultRoomLimits(), ID: id})
	if err != nil {
		t.Fatal(err)
	}
	var provider app.Provider = &routineHumanProvider{}
	if len(providers) > 0 {
		provider = providers[0]
	}
	f.engine, err = app.NewRuntime(app.RuntimeOptions{Repository: f.repo, Memory: f.repo, WorkBridge: &paHTTPWork{}, Provider: provider, ProjectResolver: projects, Clock: time.Now, ID: id, Authorize: background, Rooms: f.rooms, Limits: app.RuntimeLimits{MaxBots: 8, MaxProcesses: 4, PromptTimeout: 5 * time.Second, CleanupTimeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	f.rooms.Attach(f.engine)
	f.service, err = app.NewService(app.ServiceOptions{Repository: f.repo, Memory: f.repo, Events: f.repo, ReadViews: views, ReadViewByteLimit: cloudops.ReadPlaintextBytesLimit, Runtime: f.engine, Projects: projects, Groups: groups, Rooms: f.rooms, Authorize: authorize, Clock: time.Now, ID: id})
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"Alice", "Bob", "Stranger"} {
		project := "project"
		if i == 2 {
			project = "other"
		}
		key := pa.BotKey{MachineID: "machine", BotID: strings.ToLower(name)}
		b, _, err := f.service.Create(t.Context(), pa.Command{Key: key, Operation: pa.CreateOperation, IdempotencyKey: "seed-" + name}, pa.CreateBot{Target: pa.ProjectSelection{MachineID: "machine", ProjectID: project}, Name: name, Appearance: pa.Appearance{Shape: pa.Blob, Color: pa.EntryColor}, Agent: pa.Codex, PermissionPolicy: pa.Ask})
		if err != nil {
			t.Fatal(err)
		}
		f.bots = append(f.bots, b)
	}
	handler := newProjectAssistantGroupsHTTP(projectAssistantHTTPOptions{Service: f.service, MachineID: "machine", Status: func(ctx context.Context) (pa.SourceStatus, error) {
		a, e := authorize(ctx, pa.BotKey{MachineID: "machine"}, false)
		return a.Source, e
	}, BeforeViewPublish: func() {
		if f.beforePublish != nil {
			f.beforePublish()
		}
	}})
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token != "person" && token != "reader" {
			paWriteHTTPFailure(w, paHTTPFailure(pa.Unpaired, "device"))
			return
		}
		handler.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), groupPersonContext{}, token)))
	}))
	t.Cleanup(func() {
		f.server.Close()
		f.rooms.CancelRooms()
		_ = f.engine.Close(context.Background())
		f.rooms.Idle()
		_ = db.Close()
	})
	return f
}

type paHTTPWork struct{ app.WorkBridge }

func (f *groupHTTPFixture) command(op pa.Operation, id string) contract.ProjectAssistantCommand {
	key := "room"
	if op != pa.CreateOperation && f.groupID != "" {
		key = f.groupID
	}
	return contract.ProjectAssistantCommand{Key: contract.ProjectAssistantBotKey{MachineID: "machine", BotID: key}, Operation: contract.ProjectAssistantOperation(op), IdempotencyKey: id, ContentHash: "not-trusted"}
}
func (f *groupHTTPFixture) request(t *testing.T, method, path, token string, body any) (int, []byte) {
	t.Helper()
	if f.groupID != "" {
		path = strings.Replace(path, "/rooms/groups/room", "/rooms/groups/"+f.groupID, 1)
	}
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req, err := http.NewRequest(method, f.server.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		var envelope struct {
			Command contract.ProjectAssistantCommand `json:"command"`
		}
		_ = json.Unmarshal(raw, &envelope)
		req.Header.Set("Idempotency-Key", envelope.Command.IdempotencyKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, data
}
func (f *groupHTTPFixture) create(t *testing.T) contract.ProjectAssistantGroup {
	t.Helper()
	request := contract.ProjectAssistantGroupCreateRequest{Command: f.command(pa.CreateOperation, "create-room"), Settings: contract.ProjectAssistantGroupCreateSettings{Target: contract.ProjectAssistantProjectSelection{MachineID: "machine", ProjectID: "project"}, Name: "Review room", Members: []contract.ProjectAssistantBotKey{paWireBotKey(f.bots[0].Key), paWireBotKey(f.bots[1].Key)}}}
	status, body := f.request(t, "POST", "/v1/project-assistants/rooms/groups", "person", request)
	if status != 200 {
		t.Fatalf("create %d %s", status, body)
	}
	var result contract.ProjectAssistantGroupResponse
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	f.groupID = result.Group.Key.BotID
	return result.Group
}
func TestGroupHTTPUsesSQLiteAndPublishesScriptedMemberAuthors(t *testing.T) {
	f := newGroupHTTPFixture(t)
	g := f.create(t)
	if g.Key.BotID == "room" || !strings.HasPrefix(g.Key.BotID, "group-") {
		t.Fatal("client chose the group key", g.Key)
	}
	if replay := f.create(t); replay.Key != g.Key || replay.ConversationID != g.ConversationID {
		t.Fatal("a retried create changed group identity")
	}
	status, body := f.request(t, "GET", "/v1/project-assistants/rooms/groups?machine_id=machine", "reader", nil)
	if status != 200 || !bytes.Contains(body, []byte("Review room")) {
		t.Fatal(status, string(body))
	}
	send := contract.ProjectAssistantGroupSendRequest{Command: f.command(pa.SendOperation, "say"), Text: "Please review"}
	status, body = f.request(t, "POST", "/v1/project-assistants/rooms/groups/room/messages", "person", send)
	if status != 200 {
		t.Fatal(status, string(body))
	}
	deadline := time.Now().Add(3 * time.Second)
	var page contract.ProjectAssistantGroupHistoryResponse
	for time.Now().Before(deadline) {
		status, body = f.request(t, "GET", "/v1/project-assistants/rooms/groups/room?machine_id=machine&before_seq=0&page_size=100", "reader", nil)
		if status != 200 {
			t.Fatal(status, string(body))
		}
		if err := json.Unmarshal(body, &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Lines) >= 3 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(page.Lines) != 3 || page.Lines[0].Kind != "person" || page.Lines[0].Author != nil {
		t.Fatalf("room lines: %s", body)
	}
	seen := map[string]bool{}
	for _, line := range page.Lines[1:] {
		if line.Kind != "member" || line.Author == nil || line.Text != "Synthetic main result" {
			t.Fatalf("member provenance: %+v", line)
		}
		seen[line.Author.BotID] = true
	}
	if !seen["alice"] || !seen["bob"] {
		t.Fatal(seen)
	}
	status, body = f.request(t, "POST", "/v1/project-assistants/rooms/groups/room/messages", "person", send)
	if status != 200 {
		t.Fatal(status, string(body))
	}
	f.rooms.Idle()
	page2, err := f.service.GroupHistory(t.Context(), app.GroupHistoryQuery{Key: paDomainBotKey(g.Key), PageSize: 100})
	if err != nil || len(page2.Lines) != 3 {
		t.Fatal(len(page2.Lines), err)
	}
	edit := contract.ProjectAssistantGroupEditRequest{Command: f.command(pa.EditOperation, "edit"), Settings: contract.ProjectAssistantGroupEditSettings{ExpectedVersion: g.Version, Name: "Renamed room", Members: []contract.ProjectAssistantBotKey{paWireBotKey(f.bots[0].Key)}}}
	status, body = f.request(t, "PATCH", "/v1/project-assistants/rooms/groups/room", "person", edit)
	if status != 200 {
		t.Fatal(status, string(body))
	}
	status, body = f.request(t, "POST", "/v1/project-assistants/rooms/groups/room/stop", "person", contract.ProjectAssistantGroupStopRequest{Command: f.command(pa.StopOperation, "stop")})
	if status != 200 {
		t.Fatal(status, string(body))
	}
	status, body = f.request(t, "DELETE", "/v1/project-assistants/rooms/groups/room", "person", contract.ProjectAssistantGroupDeleteRequest{Command: f.command(pa.DeleteOperation, "delete"), ExpectedVersion: g.Version + 1})
	if status != 200 {
		t.Fatal(status, string(body))
	}
	for _, bot := range f.bots[:2] {
		if _, err = f.repo.Bot(t.Context(), bot.Key); err != nil {
			t.Fatal("deleting a room deleted a Bot", err)
		}
	}
}
func TestGroupHTTPRefusesOtherProjectsReadOnlyAndRevokedGrants(t *testing.T) {
	f := newGroupHTTPFixture(t)
	g := f.create(t)
	request := contract.ProjectAssistantGroupEditRequest{Command: f.command(pa.EditOperation, "foreign"), Settings: contract.ProjectAssistantGroupEditSettings{ExpectedVersion: g.Version, Name: "Mixed", Members: []contract.ProjectAssistantBotKey{paWireBotKey(f.bots[0].Key), paWireBotKey(f.bots[2].Key)}}}
	status, body := f.request(t, "PATCH", "/v1/project-assistants/rooms/groups/room", "person", request)
	if status < 400 || !bytes.Contains(body, []byte(string(pa.ScopeDenied))) {
		t.Fatal(status, string(body))
	}
	for _, method := range []string{"POST", "PATCH", "DELETE"} {
		path := "/v1/project-assistants/rooms/groups/room"
		var req any = request
		if method == "POST" {
			path += "/messages"
			req = contract.ProjectAssistantGroupSendRequest{Command: f.command(pa.SendOperation, "read"), Text: "No"}
		}
		status, body = f.request(t, method, path, "reader", req)
		if status < 400 || !bytes.Contains(body, []byte(string(pa.ReadOnly))) {
			t.Fatal(method, status, string(body))
		}
	}
	f.mu.Lock()
	f.revoked = true
	f.mu.Unlock()
	status, body = f.request(t, "GET", "/v1/project-assistants/rooms/groups/room?machine_id=machine&before_seq=0&page_size=100", "person", nil)
	if status < 400 || !bytes.Contains(body, []byte(string(pa.Revoked))) {
		t.Fatal(status, string(body))
	}
}
func TestGroupHTTPRechecksTheGrantBeforePublishingARead(t *testing.T) {
	f := newGroupHTTPFixture(t)
	f.create(t)
	f.beforePublish = func() { f.mu.Lock(); f.revision = "grant-two"; f.mu.Unlock() }
	status, body := f.request(t, "GET", "/v1/project-assistants/rooms/groups?machine_id=machine", "person", nil)
	if status < 400 || bytes.Contains(body, []byte("Review room")) || !bytes.Contains(body, []byte(string(pa.StaleVersion))) {
		t.Fatal(status, string(body))
	}
}

func TestGroupHTTPNamespacePreservesExistingBotIDs(t *testing.T) {
	f := newGroupHTTPFixture(t)
	for _, id := range []string{"rooms", "groups"} {
		key := pa.BotKey{MachineID: "machine", BotID: id}
		_, _, err := f.service.Create(t.Context(), pa.Command{Key: key, Operation: pa.CreateOperation, IdempotencyKey: "legacy-" + id}, pa.CreateBot{Target: pa.ProjectSelection{MachineID: "machine", ProjectID: "project"}, Name: "Existing " + id, Appearance: pa.Appearance{Shape: pa.Blob, Color: pa.EntryColor}, Agent: pa.Codex, PermissionPolicy: pa.Ask})
		if err != nil {
			t.Fatal(err)
		}
		status, body := f.request(t, "GET", "/v1/project-assistants/"+id+"?machine_id=machine", "person", nil)
		if status != 200 || !bytes.Contains(body, []byte("Existing "+id)) {
			t.Fatal(id, status, string(body))
		}
	}
}

func TestGroupHistoryPacksWholeRecentLinesWithAnExactContinuation(t *testing.T) {
	seed := "<& synthetic room line "
	text := strings.Repeat(seed, cloudops.ReadPlaintextBytesLimit/20/len(seed)+1)
	page := contract.ProjectAssistantGroupHistoryResponse{ContractVersion: pa.ContractVersion, Lines: []contract.ProjectAssistantRoomEntry{}}
	for seq := int64(1); seq <= 20; seq++ {
		page.Lines = append(page.Lines, contract.ProjectAssistantRoomEntry{Seq: seq, Kind: "member", Text: text})
	}
	packed, err := paPackGroupHistory(page, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	encoded := packed.(paEncodedOutput)
	var got contract.ProjectAssistantGroupHistoryResponse
	if err = json.Unmarshal(encoded.body, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Lines) == 0 || len(got.Lines) >= len(page.Lines) || !got.HasMore || got.NextBeforeSeq != got.Lines[0].Seq || got.Lines[len(got.Lines)-1].Seq != 20 {
		t.Fatalf("bad page continuation: lines=%d has_more=%v cursor=%d", len(got.Lines), got.HasMore, got.NextBeforeSeq)
	}
	for _, line := range got.Lines {
		if line.Text != text {
			t.Fatal("room text was truncated")
		}
	}
	page.Lines = []contract.ProjectAssistantRoomEntry{{Seq: 1, Kind: "member", Text: strings.Repeat(text, 20)}}
	if _, err = paPackGroupHistory(page, func(context.Context) error { return nil }); pa.Code(err) != pa.CapacityFull {
		t.Fatal("oversized single line was not refused", err)
	}
}
