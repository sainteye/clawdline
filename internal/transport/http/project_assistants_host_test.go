package http

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	featurestore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

type paServerFixture struct {
	s                             *Server
	h                             http.Handler
	local, machine, readonly, lan string
	identity                      adaptercloud.Identity
	project                       string
	key                           pa.BotKey
	providerCalls                 atomic.Int32
}

func newPAServerFixture(t *testing.T, paired bool) *paServerFixture {
	t.Helper()
	dir := t.TempDir()
	i := adaptercloud.Identity{AccountID: "synthetic-account", MachineID: "synthetic-machine", MachineCredential: "synthetic-credential", APIBase: "https://invalid.example"}
	if paired {
		if err := adaptercloud.NewIdentityStore(filepath.Join(dir, cloudkeys.DirName)).Save(i); err != nil {
			t.Fatal(err)
		}
	}
	s, err := New(config.Config{Dir: dir, Host: "127.0.0.1", Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.StopProjectAssistants(context.Background()); err != nil {
			t.Logf("fixture cleanup: %v", err)
		}
		if err := s.store.Close(); err != nil {
			t.Error(err)
		}
	})
	local, machine, err := s.CloudCredentials()
	if err != nil {
		t.Fatal(err)
	}
	_, readonly, err := s.gate().auth.AddDeviceWithPermission("synthetic reader", true)
	if err != nil {
		t.Fatal(err)
	}
	_, lan, err := s.gate().auth.AddDeviceWithPermission("synthetic sender", false)
	if err != nil {
		t.Fatal(err)
	}
	f := &paServerFixture{s: s, local: local, machine: machine, readonly: readonly, lan: lan, identity: i,
		project: filepath.Join(t.TempDir(), "selected"), key: pa.BotKey{MachineID: i.MachineID, BotID: "synthetic-bot"}}
	if err := os.Mkdir(f.project, 0o700); err != nil {
		t.Fatal(err)
	}
	registered, err := projects.OpenPlaceRegistry(dir).Add([]string{f.project}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	f.project = registered[0].Path
	s.projectAssistants.provider = func() (app.Provider, error) {
		f.providerCalls.Add(1)
		return nil, paHTTPFailure(pa.AdapterNotFound, "fixture")
	}
	f.h = s.Handler()
	return f
}

func (f *paServerFixture) request(t *testing.T, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw string
	if text, ok := body.(string); ok {
		raw = text
	} else if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		raw = string(b)
	}
	r := httptest.NewRequest(method, path, strings.NewReader(raw))
	r.Host = "127.0.0.1"
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if raw != "" {
		r.Header.Set("Content-Type", "application/json")
		var envelope struct {
			Command contract.ProjectAssistantCommand `json:"command"`
		}
		_ = json.Unmarshal([]byte(raw), &envelope)
		r.Header.Set("Idempotency-Key", envelope.Command.IdempotencyKey)
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}

func paServerOK(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body)
	}
}

func (f *paServerFixture) command(op pa.Operation, id string) contract.ProjectAssistantCommand {
	return contract.ProjectAssistantCommand{Key: paWireBotKey(f.key), Operation: contract.ProjectAssistantOperation(op), IdempotencyKey: id, ContentHash: "not-authority"}
}
func (f *paServerFixture) create() contract.ProjectAssistantCreateRequest {
	return contract.ProjectAssistantCreateRequest{Command: f.command(pa.CreateOperation, "create"), Settings: contract.ProjectAssistantCreateSettings{
		Target: contract.ProjectAssistantProjectSelection{MachineID: f.key.MachineID, ProjectID: projects.PlaceID(f.project)},
		Name:   "Synthetic Bot", Appearance: contract.ProjectAssistantAppearance{Shape: contract.ProjectAssistantShapeBlob, Color: pa.EntryColor},
		Agent: contract.ProjectAssistantAgentCodex, PermissionPolicy: contract.ProjectAssistantPermissionPolicyAsk, Instructions: "Synthetic fixture"}}
}
func (f *paServerFixture) enable(t *testing.T) {
	t.Helper()
	paServerOK(t, f.request(t, "POST", "/v1/settings/project-assistants", f.local, `{"enabled":true}`))
}
func (f *paServerFixture) get(t *testing.T, token string) contract.ProjectAssistantGetResponse {
	t.Helper()
	w := f.request(t, "GET", "/v1/project-assistants/"+f.key.BotID+"?machine_id="+f.key.MachineID, token, nil)
	paServerOK(t, w)
	var got contract.ProjectAssistantGetResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestProjectAssistantServerDefaultOffAndTrueGateLifecycle(t *testing.T) {
	f := newPAServerFixture(t, true)
	if err := f.s.StartProjectAssistants(context.Background()); err != nil {
		t.Fatal(err)
	}
	var tables int
	if err := f.s.store.ProjectAssistantDatabase().QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name LIKE 'project_assistant_%'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 || f.s.broker.AssistantAuthority != nil || f.providerCalls.Load() != 0 {
		t.Fatal("default off installed runtime")
	}
	status := f.request(t, "GET", "/v1/project-assistants/status", f.local, nil)
	paServerOK(t, status)
	if !strings.Contains(status.Body.String(), `"access":"hidden"`) {
		t.Fatal(status.Body)
	}
	paAssertHTTPFailure(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()), http.StatusForbidden, pa.Disabled)
	f.enable(t)
	created := f.request(t, "POST", "/v1/project-assistants", f.local, f.create())
	paServerOK(t, created)
	got := f.get(t, f.local)
	paServerOK(t, f.request(t, "GET", "/v1/project-assistants?machine_id="+f.key.MachineID+"&query=&cursor=&page_size=10", f.local, nil))
	paServerOK(t, f.request(t, "GET", "/v1/project-assistants/"+f.key.BotID+"/messages?machine_id="+f.key.MachineID+"&conversation_id="+got.Conversation.ID+"&before_seq=0&page_size=10", f.local, nil))
	paServerOK(t, f.request(t, "POST", "/v1/settings", f.local, `{"project_assistants_enabled":false,"language":"en"}`))
	got = f.get(t, f.readonly)
	if got.Source.Access != contract.ProjectAssistantAccessHistoryOnly || got.Source.Writable {
		t.Fatalf("disabled source: %+v", got.Source)
	}
	paServerOK(t, f.request(t, "GET", "/v1/project-assistants/"+f.key.BotID+"/export?machine_id="+f.key.MachineID, f.readonly, nil))
	read := contract.ProjectAssistantReadRequest{Command: f.command(pa.ReadOperation, "read"), Cursor: contract.ProjectAssistantReadCursor{Key: paWireBotKey(f.key), ConversationID: got.Conversation.ID, ReaderID: "forged-reader", MessageSeq: 0}}
	w := f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/read", f.readonly, read)
	paServerOK(t, w)
	if strings.Contains(w.Body.String(), "forged-reader") {
		t.Fatal("body chose reader identity")
	}
	var receipt contract.ProjectAssistantBotResponse
	if err := json.Unmarshal(created.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	observe := contract.ProjectAssistantObserveRequest{Command: f.command(pa.ObserveOperation, "observe"), ReceiptID: receipt.Receipt.ID, EventSeq: receipt.Receipt.EventSeq}
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/observed", f.readonly, observe))
	for _, token := range []string{f.local, f.readonly, f.lan} {
		w := f.request(t, "POST", "/v1/project-assistants", token, f.create())
		if w.Code < 400 {
			t.Fatal("disabled create accepted")
		}
	}
	settings := f.request(t, "GET", "/v1/settings", f.local, nil)
	paServerOK(t, settings)
	if !strings.Contains(settings.Body.String(), `"project_assistants_enabled":false`) || !strings.Contains(settings.Body.String(), `"language":"en"`) {
		t.Fatal(settings.Body)
	}
	paServerOK(t, f.request(t, "GET", "/v1/settings/project-assistants", f.local, nil))
	legacy := f.request(t, "GET", "/v1/projects", f.local, nil)
	paServerOK(t, legacy)
	if strings.Contains(legacy.Body.String(), f.project) {
		t.Fatal("retired catalog changed")
	}
	if f.providerCalls.Load() != 0 {
		t.Fatal("metadata initialized provider")
	}
	if f.s.projectAssistants.host == nil || !f.s.projectAssistants.host.Usage().Known {
		t.Fatal("clean disabled host unknown")
	}
}

func TestProjectAssistantServerSettingsRejectBeforeEffects(t *testing.T) {
	f := newPAServerFixture(t, true)
	for _, path := range []string{"/v1/settings/project-assistants", "/v1/settings"} {
		key := "enabled"
		if path == "/v1/settings" {
			key = "project_assistants_enabled"
		}
		for _, raw := range []string{"null", `"true"`, "1", "{}"} {
			w := f.request(t, "POST", path, f.local, fmt.Sprintf(`{"%s":%s}`, key, raw))
			if w.Code < 400 {
				t.Fatalf("accepted %s %s", path, raw)
			}
		}
		for _, body := range []string{fmt.Sprintf(`{"%s":true} {}`, key), fmt.Sprintf(`{"%s":false,"%s":true}`, key, key), fmt.Sprintf(`{"%s":true,"unknown":true}`, key)} {
			if w := f.request(t, "POST", path, f.local, body); w.Code < 400 {
				t.Fatalf("accepted %s", body)
			}
		}
		for _, token := range []string{f.readonly, f.lan} {
			if w := f.request(t, "POST", path, token, fmt.Sprintf(`{"%s":true}`, key)); w.Code != 403 {
				t.Fatalf("nonowner %s: %d %s", path, w.Code, w.Body)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(f.s.cfg.Dir, nextconfig.FileName)); !os.IsNotExist(err) {
		t.Fatal("invalid setting changed disk")
	}
	if f.s.projectAssistants.host != nil || f.providerCalls.Load() != 0 {
		t.Fatal("refusal allocated host/provider")
	}
	r := httptest.NewRequest("POST", "/v1/settings/project-assistants", strings.NewReader(`{"enabled":true}`))
	r.Host = "127.0.0.1"
	r.Header.Set(machineHeader, f.machine)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("orchestrator enabled feature: %d", w.Code)
	}
	r = httptest.NewRequest("POST", "/v1/settings/project-assistants", strings.NewReader(`{"enabled":true}`))
	r.Host = "127.0.0.1"
	r.Header.Set("Authorization", "Bearer "+f.local)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://elsewhere.invalid")
	w = httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("foreign origin enabled feature")
	}
}

func TestProjectAssistantServerUnpairedAndIdentityReplacement(t *testing.T) {
	f := newPAServerFixture(t, false)
	if err := f.s.StartProjectAssistants(context.Background()); err != nil {
		t.Fatal(err)
	}
	paAssertHTTPFailure(t, f.request(t, "POST", "/v1/settings/project-assistants", f.local, `{"enabled":true}`), 403, pa.Unpaired)
	if f.s.projectAssistants.host != nil {
		t.Fatal("unpaired source allocated host")
	}
	if err := adaptercloud.NewIdentityStore(filepath.Join(f.s.cfg.Dir, cloudkeys.DirName)).Save(f.identity); err != nil {
		t.Fatal(err)
	}
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	old := f.s.projectAssistants.host
	i := f.identity
	i.AccountID = "replacement-account"
	if err := adaptercloud.NewIdentityStore(filepath.Join(f.s.cfg.Dir, cloudkeys.DirName)).Save(i); err != nil {
		t.Fatal(err)
	}
	paAssertHTTPFailure(t, f.request(t, "GET", "/v1/project-assistants/"+f.key.BotID+"?machine_id="+f.key.MachineID, f.local, nil), 403, pa.ScopeDenied)
	paAssertHTTPFailure(t, f.request(t, "POST", "/v1/settings/project-assistants", f.local, `{"enabled":true}`), 403, pa.ScopeDenied)
	if f.s.projectAssistants.host != old {
		t.Fatal("changed identity replaced retained owner")
	}
}

func TestProjectAssistantServerOptOutQuiescesRestartedQueueWithoutActors(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	st, turn := persistPAServerQueue(t, f)
	if _, err := f.s.settingsFile().Set(map[string]any{"project_assistants_enabled": false}); err != nil {
		t.Fatal(err)
	}
	// Reopen a real Server over the same durable DB while no Engine exists.
	other, err := New(config.Config{Dir: f.s.cfg.Dir, Host: "127.0.0.1", Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.StopProjectAssistants(context.Background()); _ = other.store.Close() })
	if err := other.StartProjectAssistants(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := st.Turn(context.Background(), f.key, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != pa.TurnCancelled {
		t.Fatalf("queued opt-out remained %s", got.State)
	}
	if f.providerCalls.Load() != 0 || other.projectAssistants.host.Usage().Processes != 0 {
		t.Fatal("opt-out ran provider")
	}
}

func persistPAServerQueue(t *testing.T, f *paServerFixture) (*featurestore.Store, pa.Turn) {
	t.Helper()
	st := featurestore.New(f.s.store.ProjectAssistantDatabase())
	x, err := st.Export(context.Background(), f.key)
	if err != nil {
		t.Fatal(err)
	}
	fence := pa.Fence{Key: f.key, Project: x.Bot.Project, ConversationID: x.Conversation.ID}
	turn := pa.Turn{ID: "persisted-queued", Fence: fence, State: pa.TurnQueued, MessageID: "persisted-input", IdempotencyKey: "persisted-queue"}
	conversation := x.Conversation
	conversation.LastMessageSeq = 1
	msg := pa.Message{ID: turn.MessageID, Fence: fence, TurnID: turn.ID, Seq: 1, Kind: pa.UserMessage, Text: "Synthetic queued input", At: time.Now()}
	c := pa.Command{Key: f.key, Operation: pa.SendOperation, IdempotencyKey: "persisted-queue", ContentHash: strings.Repeat("a", 64)}
	if _, err := st.Commit(context.Background(), app.Mutation{Command: c, ExpectedVersion: x.Bot.Version, Conversation: &conversation, Turn: &turn, Messages: []pa.Message{msg}, Receipt: pa.Receipt{ID: "queued-receipt", Command: c, State: pa.EffectAccepted}}); err != nil {
		t.Fatal(err)
	}
	return st, turn
}

func TestProjectAssistantServerCleanupFailurePersistsFalseAndUnknown(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	st, turn := persistPAServerQueue(t, f)
	db := f.s.store.ProjectAssistantDatabase()
	if _, err := db.Exec(`CREATE TRIGGER deny_pa_cleanup BEFORE UPDATE ON project_assistant_bots BEGIN SELECT RAISE(ABORT,'synthetic cleanup failure'); END`); err != nil {
		t.Fatal(err)
	}
	paAssertHTTPFailure(t, f.request(t, "POST", "/v1/settings/project-assistants", f.local, `{"enabled":false}`), 500, pa.ExecutionEffectUnknown)
	v, err := f.s.settingsFile().Read()
	if err != nil || v.ProjectAssistantsEnabled() {
		t.Fatal("failed cleanup did not preserve durable false", err)
	}
	u := f.s.projectAssistants.host.Usage()
	if u.Known || u.Processes != 0 {
		t.Fatalf("unknown queue cleanup reported %+v", u)
	}
	if measure := f.s.projectAssistantCapacityMeasures()[capacity.BotsActors](); measure.Known {
		t.Fatal("unknown cleanup measured known zero")
	}
	paAssertHTTPFailure(t, f.request(t, "POST", "/v1/settings/project-assistants", f.local, `{"enabled":true}`), 500, pa.ExecutionEffectUnknown)
	paAssertHTTPFailure(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()), 403, pa.ReadOnly)
	if _, err := db.Exec(`DROP TRIGGER deny_pa_cleanup`); err != nil {
		t.Fatal(err)
	}
	f.enable(t)
	got, err := st.Turn(context.Background(), f.key, turn.ID)
	if err != nil || got.State != pa.TurnCancelled {
		t.Fatalf("retry did not quiesce %+v %v", got, err)
	}
	if f.providerCalls.Load() != 0 {
		t.Fatal("cleanup initialized provider")
	}
}

func (f *paServerFixture) pin(t *testing.T, sender, scope string, invited bool) adaptercloud.PinnedDevice {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	pin := adaptercloud.PinnedDevice{DeviceID: sender, PublicKey: base64.StdEncoding.EncodeToString(pub), Fingerprint: domaincloud.Fingerprint(pub), PairedAt: time.Now().Unix(), Scope: scope, Invited: invited}
	if err := adaptercloud.NewPinnedStore(filepath.Join(f.s.cfg.Dir, cloudkeys.DirName)).Pin(f.identity.AccountID, pin); err != nil {
		t.Fatal(err)
	}
	return pin
}
func (f *paServerFixture) cloud(t *testing.T, sender, method, path string, body any) cloudops.LocalResponse {
	t.Helper()
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	router := cloudtransport.Router{Handler: f.h, Authorize: cloudtransport.LocalAuthorizer(f.local, f.machine)}
	var envelope struct {
		Command contract.ProjectAssistantCommand `json:"command"`
	}
	_ = json.Unmarshal(raw, &envelope)
	r, err := router.Do(context.Background(), cloudops.LocalRequest{Viewer: sender, Method: method, Path: path, Header: map[string]string{actorHeader: actorDevice, "Idempotency-Key": envelope.Command.IdempotencyKey}, Body: raw})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestProjectAssistantServerCloudFreshPinAndMetadataThroughGate(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	f.pin(t, "owner", adaptercloud.ScopeOperate, false)
	f.pin(t, "reader", adaptercloud.ScopeReadOnly, false)
	f.pin(t, "guest", adaptercloud.ScopeOperate, true)
	path := "/v1/project-assistants/" + f.key.BotID + "?machine_id=" + f.key.MachineID
	for _, sender := range []string{"owner", "reader", "guest"} {
		if r := f.cloud(t, sender, "GET", path, nil); r.Status != 200 {
			t.Fatalf("%s: %d %s", sender, r.Status, r.Body)
		}
	}
	owner := f.cloud(t, "owner", "POST", "/v1/project-assistants", f.create())
	if owner.Status != 200 {
		t.Fatalf("owner replay %d %s", owner.Status, owner.Body)
	}
	for _, sender := range []string{"reader", "", "missing"} {
		if r := f.cloud(t, sender, "POST", "/v1/project-assistants", f.create()); r.Status < 400 {
			t.Fatalf("%s wrote without operate pin", sender)
		}
	}
	if r := f.cloud(t, "owner", "POST", "/v1/settings/project-assistants", map[string]bool{"enabled": false}); r.Status != 403 {
		t.Fatal("Cloud changed global feature")
	}
	got := f.get(t, f.local)
	read := contract.ProjectAssistantReadRequest{Command: f.command(pa.ReadOperation, "cloud-read"), Cursor: contract.ProjectAssistantReadCursor{Key: paWireBotKey(f.key), ConversationID: got.Conversation.ID, ReaderID: "body-is-not-identity", MessageSeq: 0}}
	r := f.cloud(t, "reader", "POST", "/v1/project-assistants/"+f.key.BotID+"/read", read)
	if r.Status != 200 {
		t.Fatalf("readonly metadata %d %s", r.Status, r.Body)
	}
	var result contract.ProjectAssistantReadResponse
	if err := json.Unmarshal(r.Body, &result); err != nil {
		t.Fatal(err)
	}
	_, ownerReader := adaptercloud.ProjectAssistantOwnerIdentity(f.identity.AccountID)
	if result.Cursor.ReaderID != ownerReader {
		t.Fatal("owner cursor differs across local/cloud")
	}
	read.Command.IdempotencyKey = "guest-read"
	r = f.cloud(t, "guest", "POST", "/v1/project-assistants/"+f.key.BotID+"/read", read)
	if r.Status != 200 {
		t.Fatalf("guest metadata %d %s", r.Status, r.Body)
	}
	if err := json.Unmarshal(r.Body, &result); err != nil {
		t.Fatal(err)
	}
	if result.Cursor.ReaderID == ownerReader {
		t.Fatal("invite shares owner cursor")
	}
	pins := adaptercloud.NewPinnedStore(filepath.Join(f.s.cfg.Dir, cloudkeys.DirName))
	if _, err := pins.Revoke("owner", time.Now()); err != nil {
		t.Fatal(err)
	}
	if r := f.cloud(t, "owner", "GET", path, nil); r.Status != 403 {
		t.Fatalf("fresh revoked pin %d %s", r.Status, r.Body)
	}
	if f.providerCalls.Load() != 0 {
		t.Fatal("Cloud metadata allocated provider")
	}
}

type paServerProvider struct {
	app.Provider
	capability func(pa.Bot) (pa.Capabilities, error)
	open       func(context.Context, app.OpenRequest) (app.ProviderSession, error)
}

func (p paServerProvider) Capabilities(_ context.Context, b pa.Bot) (pa.Capabilities, error) {
	if p.capability != nil {
		return p.capability(b)
	}
	return paServerCaps(b), nil
}
func (p paServerProvider) Open(ctx context.Context, q app.OpenRequest, _ func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	return p.open(ctx, q)
}
func paServerCaps(b pa.Bot) pa.Capabilities {
	return pa.Capabilities{Key: b.Key, Project: b.Project, Agent: b.Agent, CLIVersion: "synthetic", AdapterVersion: "fixture", Evidence: "fixture-only", ReadOnlyEnforced: true, NativeMemory: pa.NativeMemoryDisabled, NativeMemoryVerified: true}
}

func (f *paServerFixture) send(t *testing.T, token, id string) *httptest.ResponseRecorder {
	t.Helper()
	got := f.get(t, token)
	fence := pa.Fence{Key: f.key, Project: paDomainProjectScope(got.Bot.Project), ConversationID: got.Conversation.ID, BindingID: got.Conversation.BindingID, Generation: got.Conversation.Generation}
	return f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/messages", token, contract.ProjectAssistantSendRequest{Command: f.command(pa.SendOperation, id), Fence: paWireFence(fence), Text: "Synthetic first input"})
}

func TestProjectAssistantServerRevalidatesWitnessAfterCapabilityAdmission(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.s.projectAssistants.provider = func() (app.Provider, error) {
		f.providerCalls.Add(1)
		return paServerProvider{capability: func(b pa.Bot) (pa.Capabilities, error) {
			id := f.s.gate().auth.Verify(f.lan).Device
			if err := f.s.gate().auth.Revoke(id); err != nil {
				t.Fatal(err)
			}
			return paServerCaps(b), nil
		}}, nil
	}
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	paAssertHTTPFailure(t, f.send(t, f.lan, "revoke-at-admission"), 403, pa.Revoked)
	x, err := featurestore.New(f.s.store.ProjectAssistantDatabase()).Export(context.Background(), f.key)
	if err != nil {
		t.Fatal(err)
	}
	if len(x.Turns) != 0 || len(x.Messages) != 0 || f.providerCalls.Load() != 1 {
		t.Fatal("revoked admission persisted a turn")
	}
}

type paServerSession struct {
	sent       chan struct{}
	closeFails atomic.Bool
	closes     atomic.Int32
}

func (*paServerSession) SessionID() string { return "synthetic-session" }
func (p *paServerSession) Send(context.Context, app.PromptRequest) (app.PendingPrompt, error) {
	close(p.sent)
	return paServerPending{}, nil
}
func (*paServerSession) Answer(context.Context, string, string) error { return nil }
func (*paServerSession) Cancel(context.Context) error                 { return nil }
func (p *paServerSession) Close(ctx context.Context) error {
	p.closes.Add(1)
	if p.closeFails.Load() {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}
func (*paServerSession) Usage() app.SessionUsage { return app.SessionUsage{PendingRPC: 1} }

type paServerPending struct{}

func (paServerPending) Wait(ctx context.Context) (app.PromptResult, error) {
	<-ctx.Done()
	return app.PromptResult{}, ctx.Err()
}

func TestProjectAssistantServerFirstSendIsLazyAndCleanupDeadlineRetainsOwner(t *testing.T) {
	f := newPAServerFixture(t, true)
	limits := app.RuntimeLimits{MaxBots: capacity.MaximumBotActors, MaxProcesses: capacity.MaximumBotProcesses, PromptTimeout: time.Second, CleanupTimeout: 50 * time.Millisecond}
	f.s.projectAssistants.limits = &limits
	session := &paServerSession{sent: make(chan struct{})}
	session.closeFails.Store(true)
	f.s.projectAssistants.provider = func() (app.Provider, error) {
		f.providerCalls.Add(1)
		return paServerProvider{open: func(ctx context.Context, q app.OpenRequest) (app.ProviderSession, error) {
			if _, ok := ctx.Deadline(); !ok {
				t.Error("Open had no original deadline")
			}
			expected, _ := filepath.EvalSymlinks(f.project)
			if q.Directory != expected {
				t.Errorf("wrong selected directory %s", q.Directory)
			}
			return session, nil
		}}, nil
	}
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	_ = f.get(t, f.local)
	if f.providerCalls.Load() != 0 {
		t.Fatal("metadata initialized fake provider")
	}
	paServerOK(t, f.send(t, f.local, "first-send"))
	select {
	case <-session.sent:
	case <-time.After(2 * time.Second):
		t.Fatal("fake provider never received first input")
	}
	paAssertHTTPFailure(t, f.request(t, "POST", "/v1/settings/project-assistants", f.local, `{"enabled":false}`), 500, pa.ExecutionEffectUnknown)
	if u := f.s.projectAssistants.host.Usage(); u.Known || u.Processes != 1 {
		t.Fatalf("unknown Close dropped owner %+v", u)
	}
	paAssertHTTPFailure(t, f.request(t, "POST", "/v1/settings/project-assistants", f.local, `{"enabled":true}`), 500, pa.ExecutionEffectUnknown)
	session.closeFails.Store(false)
	paServerOK(t, f.request(t, "POST", "/v1/settings/project-assistants", f.local, `{"enabled":false}`))
	if u := f.s.projectAssistants.host.Usage(); !u.Known || u.Processes != 0 {
		t.Fatalf("explicit cleanup did not finish %+v", u)
	}
	if f.providerCalls.Load() != 1 || session.closes.Load() < 2 {
		t.Fatal("lazy provider/retained close ownership not exercised")
	}
}

func TestProjectAssistantServerResolvesOnlyCurrentRegisteredSelection(t *testing.T) {
	f := newPAServerFixture(t, true)
	resolver := projectAssistantProjects{f.s}
	target := pa.ProjectSelection{MachineID: f.identity.MachineID, ProjectID: projects.PlaceID(f.project)}
	resolved, err := resolver.Resolve(context.Background(), target)
	if err != nil || resolved.Scope.Kind != pa.PlaceProject {
		t.Fatalf("registered place %+v %v", resolved, err)
	}
	wrong := target
	wrong.MachineID = "other-machine"
	if _, err := resolver.Resolve(context.Background(), wrong); pa.Code(err) != pa.ScopeDenied {
		t.Fatal("wrong machine resolved", err)
	}
	wrong = target
	wrong.ProjectID = "remembered-but-absent"
	if _, err := resolver.Resolve(context.Background(), wrong); pa.Code(err) != pa.NotFound {
		t.Fatal("absent project resolved", err)
	}
	registry := projects.OpenPlaceRegistry(f.s.cfg.Dir)
	if _, err := registry.Remove([]string{f.project}); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background(), target); pa.Code(err) != pa.NotFound {
		t.Fatal("removed path remained authority", err)
	}
	if _, err := registry.Add([]string{f.project}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(f.project); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background(), target); pa.Code(err) != pa.NotFound {
		t.Fatal("deleted directory resolved", err)
	}
	// Real directory markers exercise the existing canonical resolver without
	// invoking git, native providers or a live Session.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "repository")
	worktree := filepath.Join(root, "selected-worktree")
	metadata := filepath.Join(repo, ".git", "worktrees", "selected")
	if err := os.MkdirAll(metadata, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(worktree, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: "+metadata+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(metadata, "commondir"), []byte("../..\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Add([]string{repo, worktree}, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{repo, worktree} {
		selection := pa.ProjectSelection{MachineID: target.MachineID, ProjectID: projects.PlaceID(path)}
		got, err := resolver.Resolve(context.Background(), selection)
		canonical, _ := filepath.EvalSymlinks(repo)
		directory, _ := filepath.EvalSymlinks(path)
		if err != nil || got.Scope.Kind != pa.RepositoryProject || got.Scope.CanonicalID != canonical || got.Directory != directory {
			t.Fatalf("canonical selection %+v %v", got, err)
		}
	}
	other := filepath.Join(root, "replacement")
	if err := os.MkdirAll(filepath.Join(other, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(metadata, "commondir"), []byte(filepath.Join(other, ".git")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	target.ProjectID = projects.PlaceID(worktree)
	if _, err := resolver.Resolve(context.Background(), target); pa.Code(err) != pa.StaleVersion {
		t.Fatal("changed canonical selection resolved", err)
	}
}

func TestProjectAssistantServerFailedEnableWriteCompensatesHostAndReportsUnknown(t *testing.T) {
	f := newPAServerFixture(t, true)
	configPath := filepath.Join(f.s.cfg.Dir, nextconfig.FileName)
	if err := os.Mkdir(configPath, 0o700); err != nil {
		t.Fatal(err)
	}
	paAssertHTTPFailure(t, f.request(t, "POST", "/v1/settings/project-assistants", f.local, `{"enabled":true}`), 500, pa.OutcomeUnknown)
	state, err := f.s.projectAssistantFeature(context.Background())
	if err != nil || state.Enabled {
		t.Fatal("write failure left runtime enabled", state, err)
	}
	if f.s.projectAssistants.allowed || f.providerCalls.Load() != 0 {
		t.Fatal("failed enable admitted execution")
	}
	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	f.enable(t)
}

func TestProjectAssistantServerMissingProviderRefusesWithoutInventingReadiness(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.s.projectAssistants.provider = nil
	f.enable(t)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, f.create()))
	// A Codex Bot stops at its CLI on macOS; elsewhere its platform is refused first.
	if runtime.GOOS == "darwin" {
		paAssertHTTPFailure(t, f.send(t, f.local, "unconfigured-provider"), 503, pa.CLINotInstalled)
	} else {
		paAssertHTTPFailure(t, f.send(t, f.local, "unconfigured-provider"), 501, pa.Unsupported)
	}
	x, err := featurestore.New(f.s.store.ProjectAssistantDatabase()).Export(context.Background(), f.key)
	if err != nil || len(x.Turns) != 0 || len(x.Messages) != 0 {
		t.Fatal("missing provider accepted work", err)
	}
	status := f.request(t, "GET", "/v1/project-assistants/status", f.local, nil)
	paServerOK(t, status)
	if !strings.Contains(status.Body.String(), `"capabilities":[]`) {
		t.Fatal("missing provider invented native evidence", status.Body)
	}
}

func TestProjectAssistantServerUnreadableInstalledSchemaIsNotKnownZero(t *testing.T) {
	f := newPAServerFixture(t, true)
	if _, err := f.s.store.ProjectAssistantDatabase().Exec(`CREATE TABLE project_assistant_schema (id INTEGER PRIMARY KEY, version INTEGER NOT NULL); INSERT INTO project_assistant_schema VALUES(1,42)`); err != nil {
		t.Fatal(err)
	}
	if err := f.s.StartProjectAssistants(context.Background()); pa.Code(err) != pa.Unsupported {
		t.Fatal("unknown schema accepted", err)
	}
	for _, name := range []string{capacity.BotsActors, capacity.BotsProcesses, capacity.BotsPendingTurns} {
		if got := f.s.projectAssistantCapacityMeasures()[name](); got.Known {
			t.Fatalf("%s reported known zero after failed installed schema inspection: %+v", name, got)
		}
	}
}

func TestProjectAssistantServerExistingHostInspectionFailureRetainsUnknownUntilFreshSuccess(t *testing.T) {
	f := newPAServerFixture(t, true)
	f.enable(t)
	db := f.s.store.ProjectAssistantDatabase()
	if _, err := db.Exec(`UPDATE project_assistant_schema SET version=42 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.projectAssistantFeature(context.Background()); pa.Code(err) != pa.Unsupported {
		t.Fatal("existing Host accepted changed schema", err)
	}
	for _, name := range []string{capacity.BotsActors, capacity.BotsProcesses, capacity.BotsPendingTurns} {
		if got := f.s.projectAssistantCapacityMeasures()[name](); got.Known {
			t.Errorf("%s reported known zero after existing Host inspection failed: %+v", name, got)
		}
	}
	if _, err := db.Exec(`UPDATE project_assistant_schema SET version=1 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	// A measurement must not query SQL per row or silently clear the observed
	// failure just because the underlying file may have been repaired.
	for _, name := range []string{capacity.BotsActors, capacity.BotsProcesses} {
		if got := f.s.projectAssistantCapacityMeasures()[name](); got.Known {
			t.Errorf("%s cleared inspection failure without fresh success", name)
		}
	}
	if _, err := f.s.projectAssistantFeature(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{capacity.BotsActors, capacity.BotsProcesses} {
		if got := f.s.projectAssistantCapacityMeasures()[name](); !got.Known || got.Used != 0 {
			t.Errorf("%s did not recover after fresh successful inspection: %+v", name, got)
		}
	}
}
