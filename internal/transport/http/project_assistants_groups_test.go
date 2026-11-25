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
	"os"
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
	if pa.IsPersonalWorkspaceID(q.ProjectID) {
		return pa.ProjectResolution{Scope: pa.PersonalScope(q), Directory: filepath.Join(p.dir, q.ProjectID), Available: true, Current: true}, nil
	}
	return pa.ProjectResolution{Scope: pa.ProjectScope{MachineID: q.MachineID, ProjectID: q.ProjectID, CanonicalID: "canonical-" + q.ProjectID, Kind: pa.RepositoryProject}, Directory: p.dir, Available: true, Current: true}, nil
}

func (p groupHTTPProjects) ProvisionPersonalWorkspace(ctx context.Context, q pa.ProjectSelection) (pa.ProjectResolution, error) {
	resolved, err := p.Resolve(ctx, q)
	if err != nil {
		return pa.ProjectResolution{}, err
	}
	return resolved, os.MkdirAll(resolved.Directory, 0700)
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
		actor := "person"
		if token == "other-person" {
			actor = token
		}
		return app.Authority{PrincipalID: actor, ReaderID: actor, AuthorizationRevision: revision, Source: source, Feature: pa.FeatureState{Enabled: true}}, nil
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
	f.engine, err = app.NewRuntime(app.RuntimeOptions{Repository: f.repo, Memory: f.repo, WorkBridge: &paHTTPWork{}, Provider: provider, ProjectResolver: projects, Clock: time.Now, ID: id, Authorize: background, Rooms: f.rooms, Limits: app.RuntimeLimits{MaxBots: 8, PromptTimeout: 5 * time.Second, CleanupTimeout: time.Second}})
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
	handler := newProjectAssistantGroupsHTTP(projectAssistantHTTPOptions{Service: f.service, MachineID: "machine", Attachments: store.Attachments{Dir: t.TempDir()}, Status: func(ctx context.Context) (pa.SourceStatus, error) {
		a, e := authorize(ctx, pa.BotKey{MachineID: "machine"}, false)
		return a.Source, e
	}, BeforeViewPublish: func() {
		if f.beforePublish != nil {
			f.beforePublish()
		}
	}})
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token != "person" && token != "reader" && token != "other-person" {
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

func (f *groupHTTPFixture) createBot(t *testing.T, id, name, project string) pa.Bot {
	t.Helper()
	key := pa.BotKey{MachineID: "machine", BotID: id}
	bot, _, err := f.service.Create(t.Context(), pa.Command{Key: key, Operation: pa.CreateOperation, IdempotencyKey: "seed-" + id}, pa.CreateBot{
		Target: pa.ProjectSelection{MachineID: "machine", ProjectID: project}, Name: name, Appearance: pa.Appearance{Shape: pa.Blob, Color: pa.EntryColor}, Agent: pa.Codex, PermissionPolicy: pa.Ask,
	})
	if err != nil {
		t.Fatal(err)
	}
	return bot
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

type roomFallbackProvider struct {
	*routineHumanProvider
	held *groupUIHeldProvider
}

func (p *roomFallbackProvider) Open(ctx context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	if (q.Bot.Key.BotID != "alice" || q.Bot.Agent != pa.Codex) && p.held != nil {
		return p.held.Open(ctx, q, sink)
	}
	session, err := p.routineHumanProvider.Open(ctx, q, sink)
	if err != nil || q.Bot.Key.BotID != "alice" || q.Bot.Agent != pa.Codex {
		return session, err
	}
	return session, &app.PreSendRefusal{Failure: &app.ProviderFailure{
		Reason: app.ProviderUsageExhausted,
		Cause:  &pa.Failure{Code: pa.Unsupported, Field: "fixture diagnostic must stay private"},
	}}
}

func enableRoomFixtureFallback(t *testing.T, f *groupHTTPFixture) {
	t.Helper()
	bot := f.bots[0]
	chain := []pa.Agent{pa.ClaudeCode}
	_, _, err := f.service.Edit(t.Context(),
		pa.Command{Key: bot.Key, Operation: pa.EditOperation, IdempotencyKey: "fixture-fallback"},
		pa.EditBot{ExpectedVersion: bot.Version, Name: bot.Name, Appearance: bot.Appearance, FallbackAgents: &chain})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGroupHistoryPublishesOnlyVerifiedFallbackAndRoomResults(t *testing.T) {
	native := &routineHumanProvider{}
	held := &groupUIHeldProvider{Provider: native}
	provider := &roomFallbackProvider{routineHumanProvider: native, held: held}
	f := newGroupHTTPFixture(t, provider)
	enableRoomFixtureFallback(t, f)
	f.create(t)
	const path = "/v1/project-assistants/rooms/groups/room?machine_id=machine&before_seq=0&page_size=100"
	held.block()
	defer held.release()
	for i := range 2 {
		status, body := f.request(t, http.MethodPost, "/v1/project-assistants/rooms/groups/room/messages", "person",
			contract.ProjectAssistantGroupSendRequest{Command: f.command(pa.SendOperation, fmt.Sprintf("fallback-%d", i)), Text: "@Alice review"})
		if status != http.StatusOK {
			t.Fatal(status, string(body))
		}
		if i == 0 {
			var live contract.ProjectAssistantGroupHistoryResponse
			seen := false
			for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
				status, body = f.request(t, http.MethodGet, path, "reader", nil)
				if err := json.Unmarshal(body, &live); err != nil || status != http.StatusOK {
					t.Fatal(status, string(body), err)
				}
				if len(live.MemberOutcomes) == 1 && len(live.MemberOutcomes[0].Attempts) == 2 {
					fallback := live.MemberOutcomes[0].Attempts[1]
					if fallback.Stage == "sent" {
						seen = fallback.Fallback && live.MemberOutcomes[0].Name == "Alice" &&
							live.MemberOutcomes[0].Disposition == "" && len(live.Activity) == 1 && len(live.Lines) == 1
						break
					}
				}
				time.Sleep(time.Millisecond)
			}
			if !seen {
				t.Fatalf("in-flight fallback or unpublished room boundary was lost: %s", body)
			}
			held.release()
		}
		f.rooms.Idle()
		status, body = f.request(t, http.MethodGet, path, "reader", nil)
		var page contract.ProjectAssistantGroupHistoryResponse
		if err := json.Unmarshal(body, &page); err != nil || status != http.StatusOK {
			t.Fatal(status, string(body), err)
		}
		if len(page.MemberOutcomes) != 1 || len(page.Activity) != 0 || len(page.Lines) != 2*(i+1) {
			t.Fatalf("unexpected room snapshot: %s", body)
		}
		got := page.MemberOutcomes[0]
		if got.Name != "Alice" || got.Member.BotID != "alice" || got.State != "succeeded" || got.Disposition != "published" ||
			got.ReplySeq != page.Lines[len(page.Lines)-1].Seq || len(got.Attempts) != 2-i {
			t.Fatalf("unexpected publication evidence: %s", body)
		}
		alt := got.Attempts[len(got.Attempts)-1]
		if alt.Agent != "claude" || !alt.Fallback || alt.Stage != "closed" {
			t.Fatalf("alternate execution was not attributed: %+v", alt)
		}
		if i == 0 {
			primary := got.Attempts[0]
			if primary.Agent != "codex" || primary.Fallback || primary.Reason != "usage_limit" || primary.Stage != "closed" || alt.PrimaryRestReason != "" {
				t.Fatalf("first pass did not preserve the refusal: %+v", got.Attempts)
			}
		} else if alt.PrimaryRestReason != "usage_limit" || alt.Ordinal != 1 {
			t.Fatalf("resting primary was mistaken for a sent attempt: %+v", got.Attempts)
		}
		for _, private := range []string{"fixture diagnostic", "native_session_id", "login_profile", "carry_after_seq", "binding_id", "reset_at", "Your full toolkit"} {
			if bytes.Contains(body, []byte(private)) {
				t.Fatalf("room read disclosed %q: %s", private, body)
			}
		}
	}
	provider.mu.Lock()
	if len(provider.sends) != 2 || len(provider.opens) != 3 {
		t.Errorf("expected one actual send per room turn, got %d opens and %d sends", len(provider.opens), len(provider.sends))
	}
	provider.mu.Unlock()
	f.beforePublish = func() { f.mu.Lock(); f.revision = "grant-two"; f.mu.Unlock() }
	status, body := f.request(t, http.MethodGet, path, "reader", nil)
	if status < 400 || bytes.Contains(body, []byte("member_outcomes")) || !bytes.Contains(body, []byte(string(pa.StaleVersion))) {
		t.Fatal(status, string(body))
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

func TestPersonalGroupHTTPKeepsMembersInTheirOwnWorkspaces(t *testing.T) {
	f := newGroupHTTPFixture(t)
	a, b := f.createBot(t, "personal-one", "Personal One", ""), f.createBot(t, "personal-two", "Personal Two", "")
	if a.Project == b.Project || a.Project.Kind != pa.PersonalWorkspace || b.Project.Kind != pa.PersonalWorkspace {
		t.Fatal("fixture must keep separate personal workspaces", a.Project, b.Project)
	}
	path := "/v1/project-assistants/rooms/groups"
	request := contract.ProjectAssistantGroupCreateRequest{Command: f.command(pa.CreateOperation, "create-personal-room"), Settings: contract.ProjectAssistantGroupCreateSettings{
		Target: contract.ProjectAssistantProjectSelection{MachineID: "machine"}, Name: "Personal room", Members: []contract.ProjectAssistantBotKey{paWireBotKey(a.Key), paWireBotKey(b.Key)},
	}}
	status, body := f.request(t, "POST", path, "person", request)
	var created contract.ProjectAssistantGroupResponse
	if status != 200 || json.Unmarshal(body, &created) != nil || len(created.Group.Members) != 2 || created.Group.Project.Kind != "personal" {
		t.Fatal(status, string(body))
	}
	f.groupID = created.Group.Key.BotID
	request.Command = f.command(pa.CreateOperation, "reversed-personal-room")
	request.Settings.Members[0], request.Settings.Members[1] = request.Settings.Members[1], request.Settings.Members[0]
	status, body = f.request(t, "POST", path, "person", request)
	var duplicate contract.ProjectAssistantGroupResponse
	if status != 200 || json.Unmarshal(body, &duplicate) != nil || duplicate.Group.Key != created.Group.Key {
		t.Fatal("reordered members should open their existing room", status, string(body))
	}
	edit := contract.ProjectAssistantGroupEditRequest{Command: f.command(pa.EditOperation, "foreign-personal-room"), Settings: contract.ProjectAssistantGroupEditSettings{
		ExpectedVersion: created.Group.Version, Name: created.Group.Name, Members: []contract.ProjectAssistantBotKey{paWireBotKey(a.Key), paWireBotKey(f.bots[0].Key)},
	}}
	if status, body = f.request(t, "PATCH", path+"/room", "person", edit); status != 403 || !bytes.Contains(body, []byte("group_member_project")) {
		t.Fatal(status, string(body))
	}
	send := contract.ProjectAssistantGroupSendRequest{Command: f.command(pa.SendOperation, "say-personal"), Text: "Please review"}
	if status, body = f.request(t, "POST", path+"/room/messages", "person", send); status != 200 {
		t.Fatal(status, string(body))
	}
	deadline := time.Now().Add(3 * time.Second)
	var page contract.ProjectAssistantGroupHistoryResponse
	for {
		status, body = f.request(t, "GET", path+"/room?machine_id=machine&before_seq=0&page_size=100", "reader", nil)
		if status != 200 || json.Unmarshal(body, &page) != nil || time.Now().After(deadline) {
			t.Fatal(status, string(body))
		}
		if len(page.Lines) == 3 && len(page.MemberOutcomes) == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	for i, bot := range []pa.Bot{a, b} {
		if line := page.Lines[i+1]; line.Author == nil || *line.Author != paWireBotKey(bot.Key) || line.Kind != "member" {
			t.Fatal("wrong room author", line)
		}
		x, err := f.repo.Export(t.Context(), bot.Key)
		if err != nil || len(x.Turns) != 1 || x.Turns[0].Fence.Project != bot.Project || x.Turns[0].State != pa.TurnSucceeded {
			t.Fatal("member did not answer under its own workspace fence", x.Turns, err)
		}
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

func TestGroupReactionsKeepReaderIdentityAndRecoverLostPublication(t *testing.T) {
	f := newGroupHTTPFixture(t)
	group := f.create(t)
	const room = "/v1/project-assistants/rooms/groups/room"
	send := contract.ProjectAssistantGroupSendRequest{Command: f.command(pa.SendOperation, "reaction-topic"), Text: "A group message"}
	if status, raw := f.request(t, http.MethodPost, room+"/messages", "person", send); status != http.StatusOK {
		t.Fatalf("send: %d %s", status, raw)
	}
	f.rooms.Idle()

	read := func(token string) contract.ProjectAssistantGroupHistoryResponse {
		t.Helper()
		status, raw := f.request(t, http.MethodGet, room+"?machine_id=machine&before_seq=2&page_size=1", token, nil)
		var page contract.ProjectAssistantGroupHistoryResponse
		if err := json.Unmarshal(raw, &page); status != http.StatusOK || err != nil || len(page.Lines) != 1 {
			t.Fatalf("history: %d %s (%v)", status, raw, err)
		}
		return page
	}
	before := read("person")
	q := contract.ProjectAssistantGroupReactRequest{
		Command: f.command(pa.ReactOperation, "first-reaction"), ConversationID: group.ConversationID,
		GroupVersion: group.Version, MessageSeq: 1, Emoji: "👍", Present: true,
	}
	react := func(token string, request contract.ProjectAssistantGroupReactRequest, want string) []byte {
		t.Helper()
		status, raw := f.request(t, http.MethodPost, room+"/reactions", token, request)
		if want == "" {
			if status != http.StatusOK {
				t.Fatalf("reaction: %d %s", status, raw)
			}
		} else if status < 400 || !bytes.Contains(raw, []byte(`"code":"`+want+`"`)) {
			t.Fatalf("wanted %s: %d %s", want, status, raw)
		}
		return raw
	}
	react("reader", q, "pa_read_only")
	wrong := q
	wrong.ConversationID = "other-room"
	react("person", wrong, "pa_scope_denied")
	wrong = q
	wrong.GroupVersion++
	react("person", wrong, "pa_stale_version")
	wrong = q
	wrong.MessageSeq = 1000
	react("person", wrong, "pa_not_found")

	f.beforePublish = func() {
		f.mu.Lock()
		f.revoked = true
		f.mu.Unlock()
	}
	react("person", q, "pa_outcome_unknown")
	react("person", q, "pa_revoked")
	f.beforePublish = nil
	f.mu.Lock()
	f.revoked = false
	f.revision = "grant-after-repair"
	f.mu.Unlock()
	receipt := react("person", q, "")
	if !bytes.Equal(receipt, react("person", q, "")) {
		t.Fatal("recovered operation changed receipt")
	}
	other := q
	other.Command = f.command(pa.ReactOperation, "second-actor")
	other.Emoji = "🎉"
	react("other-person", other, "")
	after := read("person")
	reactions := after.Lines[0].Reactions
	if len(reactions) != 2 || after.Room != before.Room || after.Group.Version != before.Group.Version || after.Lines[0].Text != before.Lines[0].Text {
		t.Fatalf("reaction changed content or room state: %+v", after)
	}
	for _, v := range reactions {
		if v.Revision != 1 || len(v.Emojis) != 1 || v.Own != (v.Emojis[0] == "👍") {
			t.Fatalf("reader projection: %+v", reactions)
		}
	}
	for _, v := range read("other-person").Lines[0].Reactions {
		if v.Own != (v.Emojis[0] == "🎉") {
			t.Fatalf("other reader projection: %+v", v)
		}
	}
	q.Present = false
	react("person", q, "pa_idempotency_conflict")
	q.Command = f.command(pa.ReactOperation, "remove-reaction")
	react("person", q, "pa_stale_version")
	q.ExpectedRevision = 1
	react("person", q, "")
	for _, v := range read("person").Lines[0].Reactions {
		if v.Own && (v.Revision != 2 || len(v.Emojis) != 0) {
			t.Fatalf("removal reset the actor revision: %+v", v)
		}
	}
}
