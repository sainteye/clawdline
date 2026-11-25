package http

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline/internal/adapters/memory"
	acp "github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	store "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// This local ACP subprocess reports its actually selected values in its reply.
// It makes no vendor/model/network request and has no account credentials.
func TestMemorySettingsACPFixture(t *testing.T) {
	if os.Getenv("CLAWDLINE_MEMORY_SETTINGS_PEER") != "1" {
		return
	}
	model, effort, fast := "synthetic-small", "low", false
	var waitingSteer json.RawMessage
	config := func() any {
		return map[string]any{"configOptions": []any{
			map[string]any{"id": "model", "category": "model", "type": "select", "name": "Model", "currentValue": model, "options": []any{map[string]string{"value": "synthetic-small", "name": "Synthetic small"}, map[string]string{"value": "synthetic-large", "name": "Synthetic large"}}},
			map[string]any{"id": "effort", "category": "thought_level", "type": "select", "name": "Reasoning effort", "currentValue": effort, "options": []any{map[string]string{"value": "low", "name": "Low"}, map[string]string{"value": "high", "name": "High"}}},
			map[string]any{"id": "fast", "type": "boolean", "name": "Fast mode", "currentValue": fast},
		}}
	}
	log, _ := os.OpenFile(os.Getenv("CLAWDLINE_MEMORY_SETTINGS_RECORD"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	defer log.Close()
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024), 1<<20)
	for scanner.Scan() {
		var f struct {
			ID     json.RawMessage            `json:"id"`
			Method string                     `json:"method"`
			Params map[string]json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &f) != nil {
			os.Exit(2)
		}
		_, _ = log.Write(append(append([]byte{}, scanner.Bytes()...), '\n'))
		_ = log.Sync()
		var result any = map[string]any{}
		switch f.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "agentInfo": map[string]string{"name": "@agentclientprotocol/codex-acp", "version": "fixture"},
				"agentCapabilities": map[string]any{"sessionCapabilities": map[string]any{"systemPrompt": map[string]any{}}, "promptCapabilities": map[string]bool{"image": true}},
				"_meta":             map[string]any{"steering": map[string]bool{"supported": true}}}
		case "session/new":
			v := config().(map[string]any)
			v["sessionId"] = "synthetic-config-session"
			result = v
		case "session/set_config_option":
			var id string
			_ = json.Unmarshal(f.Params["configId"], &id)
			switch id {
			case "model":
				_ = json.Unmarshal(f.Params["value"], &model)
			case "effort":
				_ = json.Unmarshal(f.Params["value"], &effort)
			case "fast":
				if os.Getenv("CLAWDLINE_MEMORY_SETTINGS_REFUSE_FAST") == "1" {
					fast = false
					break
				}
				if json.Unmarshal(f.Params["value"], &fast) != nil {
					os.Exit(3)
				}
			}
			result = config()
		case "session/prompt":
			if strings.Contains(string(f.Params["prompt"]), "STEER-FIXTURE-BEGIN") {
				waitingSteer = f.ID
				continue
			}
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "synthetic-config-session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": fmt.Sprintf("Synthetic runtime selected %s / %s / %t", model, effort, fast)}}}})
			result = map[string]string{"stopReason": "end_turn"}
		case "_session/steering":
			var blocks []struct {
				Type, Text, MimeType, Data string
			}
			if len(waitingSteer) == 0 || json.Unmarshal(f.Params["prompt"], &blocks) != nil || len(blocks) != 2 ||
				blocks[0].Type != "image" || blocks[0].MimeType != "image/png" || blocks[0].Data == "" ||
				blocks[1].Type != "text" || !strings.Contains(blocks[1].Text, "STEER-FIXTURE-FOLLOW-UP") {
				os.Exit(4)
			}
			encoder := json.NewEncoder(os.Stdout)
			_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": f.ID, "result": map[string]string{"outcome": "injected"}})
			_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": waitingSteer, "result": map[string]string{"stopReason": "end_turn", "text": "Synthetic picture steer accepted"}})
			waitingSteer = nil
			continue
		case "session/cancel":
			if len(waitingSteer) != 0 {
				f.ID, waitingSteer = waitingSteer, nil
				result = map[string]string{"stopReason": "cancelled"}
				break
			}
			continue
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": f.ID, "result": result})
	}
	os.Exit(0)
}

func memorySettingsFixture(t *testing.T, refuseFast ...bool) (*paServerFixture, string) {
	t.Helper()
	f := newPAServerFixture(t, true)
	record := filepath.Join(t.TempDir(), "protocol.jsonl")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	extra := ""
	if len(refuseFast) > 0 && refuseFast[0] {
		extra = "CLAWDLINE_MEMORY_SETTINGS_REFUSE_FAST=1"
	}
	p, err := acp.NewProvider(acp.ProviderOptions{Configurations: map[pa.Agent]acp.Configuration{pa.Codex: {Executable: exe, Arguments: []string{"-test.run=^TestMemorySettingsACPFixture$"}, Environment: []string{"CLAWDLINE_MEMORY_SETTINGS_PEER=1", "CLAWDLINE_MEMORY_SETTINGS_RECORD=" + record, extra}}}, Limits: acp.Limits{RPCPending: 16, Notifications: 256, FrameBytes: 1 << 20, CaptureBytes: 4 << 20, StderrBytes: 64 << 10, OwnedDescendants: 32, WriteTimeout: time.Second, CleanupTimeout: 5 * time.Second, RPCTimeout: 10 * time.Second, DescendantPoll: 10 * time.Millisecond}, Verify: func(_ context.Context, b pa.Bot, _ acp.Configuration) (pa.Capabilities, error) {
		return paServerCaps(b), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	f.s.projectAssistants.provider = func() (app.Provider, error) { return p, nil }
	f.enable(t)
	create := f.create()
	create.Settings.Agent = contract.ProjectAssistantAgentCodex
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, create))
	key, ok := memory.KeyFor(f.project)
	if !ok {
		t.Fatal("fixture memory key")
	}
	_, err = f.s.memoryStore().Add(key, memory.Entry{Name: "shared-note", Description: "Synthetic shared long note", Type: memory.TypeProject, Body: strings.Repeat("Synthetic authorized Project note.\n", 1000)})
	if err != nil {
		t.Fatal(err)
	}
	g := f.get(t, f.local)
	save := contract.ProjectAssistantMemoryWriteRequest{Command: f.command(pa.MemorySaveOperation, "remember"), ExpectedRevision: 1, Entry: contract.ProjectAssistantMemoryEntry{ID: "local-note", Key: g.Bot.Key, Project: g.Bot.Project, Text: "Synthetic local fact", Revision: 1, Sources: []contract.ProjectAssistantMemorySource{}}}
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants/"+f.key.BotID+"/memory", f.local, save))
	return f, record
}
func memoryViewURL(f *paServerFixture, conversation string, extra url.Values) string {
	if extra == nil {
		extra = url.Values{"source_epoch": {""}, "layer": {""}, "entry_id": {""}, "revision": {""}}
	}
	extra.Set("machine_id", f.key.MachineID)
	extra.Set("conversation_id", conversation)
	return "/v1/project-assistants/" + f.key.BotID + "/memory-view?" + extra.Encode()
}
func TestMemorySettingsStoredHTTP(t *testing.T) {
	f, record := memorySettingsFixture(t)
	g := f.get(t, f.local)
	read := f.request(t, "GET", memoryViewURL(f, g.Conversation.ID, nil), f.readonly, nil)
	paAssertHTTPContract(t, read, 200, "ProjectAssistantMemoryViewResponse")
	v := paHTTPDecode[contract.ProjectAssistantMemoryViewResponse](t, read)
	if len(v.View.Layers) != 2 || v.View.Layers[0].Used != 1 || v.View.Layers[1].Used != 1 || v.View.Layers[0].Entries[0].Author != "" {
		t.Fatalf("layers %+v", v.View.Layers)
	}
	detail := url.Values{"layer": {"project"}, "entry_id": {"shared-note"}, "revision": {v.View.Layers[0].Revision}, "source_epoch": {v.SourceEpoch}}
	r := f.request(t, "GET", memoryViewURL(f, g.Conversation.ID, detail), f.readonly, nil)
	paAssertHTTPContract(t, r, 200, "ProjectAssistantMemoryViewResponse")
	if len(paHTTPDecode[contract.ProjectAssistantMemoryViewResponse](t, r).Detail.Text) < 32000 {
		t.Fatal("long authorized note missing")
	}
	paAssertHTTPFailure(t, f.request(t, "GET", memoryViewURL(f, "other-conversation", nil), f.local, nil), 403, pa.ScopeDenied)
	other := f.create()
	other.Command = f.command(pa.CreateOperation, "second-bot")
	other.Command.Key.BotID = "second-bot"
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, other))
	otherGet := f.request(t, "GET", "/v1/project-assistants/second-bot?machine_id="+f.key.MachineID, f.local, nil)
	paServerOK(t, otherGet)
	b := paHTTPDecode[contract.ProjectAssistantGetResponse](t, otherGet)
	otherPath := strings.Replace(memoryViewURL(f, b.Conversation.ID, nil), f.key.BotID, "second-bot", 1)
	otherRead := f.request(t, "GET", otherPath, f.local, nil)
	paServerOK(t, otherRead)
	shared := paHTTPDecode[contract.ProjectAssistantMemoryViewResponse](t, otherRead)
	if shared.View.Layers[0].Revision != v.View.Layers[0].Revision {
		t.Fatal("same Project did not share its authority")
	}
	paAssertHTTPFailure(t, f.request(t, "GET", strings.Replace(memoryViewURL(f, b.Conversation.ID, detail), f.key.BotID, "second-bot", 1), f.local, nil), 403, pa.Revoked)
	settings := contract.ProjectAssistantEditSettings{ExpectedVersion: g.Bot.Version, Name: g.Bot.Name, Appearance: g.Bot.Appearance, Instructions: g.Bot.Instructions.Text, Agent: contract.ProjectAssistantAgentCodex, Notify: "off", RuntimeSettings: &contract.ProjectAssistantRuntimeSettings{Model: "synthetic-large", Options: map[string]string{"effort": "high", "fast": "true"}}}
	edit := contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, "settings"), Settings: settings}
	if got := f.request(t, "PATCH", "/v1/project-assistants/"+f.key.BotID, f.readonly, edit); got.Code != 403 {
		t.Fatalf("read-only write=%d %s", got.Code, got.Body)
	}
	paServerOK(t, f.request(t, "PATCH", "/v1/project-assistants/"+f.key.BotID, f.local, edit))
	paServerOK(t, f.send(t, f.local, "configured-input"))
	x := fallbackHTTPWait(t, f)
	found := false
	for _, m := range x.Messages {
		found = found || strings.Contains(m.Text, "Synthetic runtime selected synthetic-large / high / true")
	}
	if !found {
		t.Fatalf("actual scripted ACP configuration did not reach prompt: %+v", x.Turns)
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"value":true`) {
		t.Fatalf("fast mode was not a native boolean: %s", data)
	}
	// Publication loses its authority when the source's real Project note is forgotten.
	key, _ := memory.KeyFor(f.project)
	f.s.projectAssistants.beforeViewPublish = func() { _, _ = f.s.memoryStore().Forget(key, "shared-note") }
	paAssertHTTPFailure(t, f.request(t, "GET", memoryViewURL(f, g.Conversation.ID, detail), f.readonly, nil), 404, pa.NotFound)
}

func TestProjectSharedMemoryHTTPPages(t *testing.T) {
	f, _ := memorySettingsFixture(t)
	conversation := f.get(t, f.local).Conversation.ID
	key, ok := memory.KeyFor(f.project)
	if !ok {
		t.Fatal("fixture memory key missing")
	}
	populateProjectMemoryPages(t, f, key, 1023)
	pages := url.Values{"page": {"0"}, "page_size": {"25"}, "source_epoch": {""}, "layer": {""}, "entry_id": {""}, "revision": {""}}
	req := func() *httptest.ResponseRecorder {
		return f.request(t, "GET", memoryViewURL(f, conversation, pages), f.readonly, nil)
	}
	load := func() contract.ProjectAssistantMemoryViewResponse {
		t.Helper()
		response := req()
		paAssertHTTPContract(t, response, 200, "ProjectAssistantMemoryViewResponse")
		return paHTTPDecode[contract.ProjectAssistantMemoryViewResponse](t, response)
	}
	initial := load()
	if len(initial.View.Layers) != 1 || initial.View.Layers[0].Layer != "bot" {
		t.Fatal("opening Bot memory returned Project rows")
	}
	pages.Set("source_epoch", initial.SourceEpoch)
	pages.Set("layer", "project")
	pages.Set("page", "1")
	first := load().View.Layers[0]
	if first.Used != 1024 || len(first.Entries) != 25 || first.Entries[0].ID != "sample-0000" || first.Entries[24].ID != "sample-0024" {
		t.Fatal("first page did not contain exactly the first 25 of 1024 rows")
	}
	pages.Set("page", "2")
	second := load().View.Layers[0]
	if len(second.Entries) != 25 || second.Entries[0].ID != "sample-0025" {
		t.Fatal("second page duplicated or skipped rows")
	}
	pages.Set("page", "41")
	last := load().View.Layers[0]
	if len(last.Entries) != 24 || last.Entries[23].ID != "shared-note" {
		t.Fatal("last page was incorrect")
	}
	pages.Set("entry_id", "shared-note")
	pages.Set("revision", last.Revision)
	detail := load().Detail
	if detail == nil || !strings.HasPrefix(detail.Text, "Synthetic authorized Project note.") {
		t.Fatal("paged detail omitted its body")
	}
	pages.Set("page", "1")
	paAssertHTTPFailure(t, req(), 409, pa.ContextStale)
	pages.Set("page_size", "101")
	paAssertHTTPFailure(t, req(), 400, pa.Invalid)
}

func populateProjectMemoryPages(t *testing.T, f *paServerFixture, key string, count int) {
	t.Helper()
	dir, err := f.s.memoryStore().Dir(key)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < count; i++ {
		entry := memory.Entry{Name: fmt.Sprintf("sample-%04d", i), Description: "Synthetic preview", Type: memory.TypeProject, Body: "Synthetic body"}
		if err = os.WriteFile(filepath.Join(dir, entry.Name+".md"), []byte(memory.Format(entry)), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMemorySettingsBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_MEMORY_SETTINGS_BROWSER_DIR")
	if dir == "" {
		t.Skip("set CLAWDLINE_MEMORY_SETTINGS_BROWSER_DIR for real synthetic browser")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	f, record := memorySettingsFixture(t)
	key, ok := memory.KeyFor(f.project)
	if !ok {
		t.Fatal("fixture memory key missing")
	}
	populateProjectMemoryPages(t, f, key, 26)
	server := httptest.NewServer(f.h)
	defer server.Close()
	cmd := exec.Command("node", "../../../web/console/tools/project-assistants-memory-settings.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+server.URL, "BOTS_TOKEN="+f.local, "BOTS_READ_TOKEN="+f.readonly, "BOTS_MACHINE="+f.key.MachineID, "BOTS_BOT="+f.key.BotID, "BOTS_ARTIFACTS="+dir)
	log, err := cmd.CombinedOutput()
	_ = os.WriteFile(filepath.Join(dir, "browser.log"), log, 0600)
	if err != nil {
		t.Fatalf("browser: %v\n%s", err, log)
	}
	x := fallbackHTTPWait(t, f)
	found := false
	for _, m := range x.Messages {
		found = found || strings.Contains(m.Text, "Synthetic runtime selected synthetic-large / high / true")
	}
	if !found {
		t.Fatalf("browser did not configure actual ACP: %+v", x.Turns)
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "protocol.jsonl"), data, 0600)
	_, err = store.New(f.s.store.ProjectAssistantDatabase()).Export(context.Background(), f.key)
	if err != nil {
		t.Fatal(err)
	}
}

// TestMemoryWritesBrowser drives writable personal memory through real Chrome
// input and production MemoryPanel/SourceClient, then reads SQLite: the edited
// topic note keeps its id and is forgotten, and the typed short fact is stored.
func TestMemoryWritesBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_MEMORY_WRITE_BROWSER_DIR")
	if dir == "" {
		t.Skip("set CLAWDLINE_MEMORY_WRITE_BROWSER_DIR for real synthetic browser")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	f, _ := memorySettingsFixture(t)
	server := httptest.NewServer(f.h)
	defer server.Close()
	cmd := exec.Command("node", "../../../web/console/tools/project-assistants-memory-settings.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+server.URL, "BOTS_TOKEN="+f.local, "BOTS_READ_TOKEN="+f.readonly, "BOTS_MACHINE="+f.key.MachineID, "BOTS_BOT="+f.key.BotID, "BOTS_ARTIFACTS="+dir, "BOTS_MEMORY_WRITE=1")
	log, err := cmd.CombinedOutput()
	_ = os.WriteFile(filepath.Join(dir, "browser.log"), log, 0600)
	if err != nil {
		t.Fatalf("browser: %v\n%s", err, log)
	}
	x, err := store.New(f.s.store.ProjectAssistantDatabase()).Export(context.Background(), f.key)
	if err != nil {
		t.Fatal(err)
	}
	// Forgetting erases the note's title, summary and text and keeps a tombstone;
	// the edit read-back after reload is asserted in the browser before that.
	var notes []pa.MemoryEntry
	var fact *pa.MemoryEntry
	for i, e := range x.MemoryEntries {
		if e.Kind == "note" {
			notes = append(notes, e)
		}
		if e.Text == "Synthetic short fact typed on a phone." {
			fact = &x.MemoryEntries[i]
		}
	}
	if len(notes) != 1 || !notes[0].Forgotten || notes[0].Title != "" || notes[0].Summary != "" || notes[0].Text != "" ||
		!slices.Contains(x.Memory.ForgottenSourceIDs, notes[0].ID) {
		t.Fatalf("SQLite does not hold exactly one erased, forgotten topic note: %+v\n%+v", notes, x.Memory)
	}
	if fact == nil || fact.Forgotten || len(fact.Sources) != 1 || fact.Sources[0].Kind != "human" {
		t.Fatalf("SQLite does not hold the typed short fact with its human source: %+v", fact)
	}
	if x.Memory.Revision < 4 {
		t.Fatalf("four memory writes should advance the revision: %+v", x.Memory)
	}
}

func TestMemoryViewSeparatesProjectsMachinesAndAccounts(t *testing.T) {
	f, _ := memorySettingsFixture(t)
	g := f.get(t, f.local)
	otherDir := t.TempDir()
	_, err := projects.OpenPlaceRegistry(f.s.cfg.Dir).Add([]string{otherDir}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	create := f.create()
	create.Command = f.command(pa.CreateOperation, "different-project")
	create.Command.Key.BotID = "different-project"
	canonicalOther, err := filepath.EvalSymlinks(otherDir)
	if err != nil {
		t.Fatal(err)
	}
	create.Settings.Target.ProjectID = projects.PlaceID(canonicalOther)
	paServerOK(t, f.request(t, "POST", "/v1/project-assistants", f.local, create))
	got := f.request(t, "GET", "/v1/project-assistants/different-project?machine_id="+f.key.MachineID, f.local, nil)
	paServerOK(t, got)
	other := paHTTPDecode[contract.ProjectAssistantGetResponse](t, got)
	path := strings.Replace(memoryViewURL(f, other.Conversation.ID, nil), f.key.BotID, "different-project", 1)
	read := f.request(t, "GET", path, f.local, nil)
	paServerOK(t, read)
	v := paHTTPDecode[contract.ProjectAssistantMemoryViewResponse](t, read)
	if v.View.Layers[0].Used != 0 || !v.View.Layers[0].Available {
		t.Fatal("another Project saw shared entries", v.View.Layers)
	}
	wrong := strings.Replace(memoryViewURL(f, g.Conversation.ID, nil), "machine_id="+f.key.MachineID, "machine_id=another-machine", 1)
	paAssertHTTPFailure(t, f.request(t, "GET", wrong, f.local, nil), 403, pa.ScopeDenied)
	identity := f.identity
	identity.AccountID = "another-synthetic-account"
	if err = adaptercloud.NewIdentityStore(filepath.Join(f.s.cfg.Dir, cloudkeys.DirName)).Save(identity); err != nil {
		t.Fatal(err)
	}
	paAssertHTTPFailure(t, f.request(t, "GET", memoryViewURL(f, g.Conversation.ID, nil), f.local, nil), 403, pa.ScopeDenied)
}

func TestMemorySettingsRejectInventedConfigurationWithoutMutation(t *testing.T) {
	f, record := memorySettingsFixture(t)
	g := f.get(t, f.local)
	for i, settings := range []contract.ProjectAssistantRuntimeSettings{{Model: "not-offered", Options: map[string]string{}}, {Model: "synthetic-large", Options: map[string]string{"mode": "dangerous"}}} {
		q := contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, fmt.Sprint("invalid-config-", i)), Settings: contract.ProjectAssistantEditSettings{ExpectedVersion: g.Bot.Version, Name: g.Bot.Name, Appearance: g.Bot.Appearance, Instructions: g.Bot.Instructions.Text, RuntimeSettings: &settings}}
		paAssertHTTPFailure(t, f.request(t, "PATCH", "/v1/project-assistants/"+f.key.BotID, f.local, q), 501, pa.Unsupported)
		if now := f.get(t, f.local); now.Bot.Version != g.Bot.Version {
			t.Fatal("refused config changed version")
		}
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"method":"session/prompt"`) {
		t.Fatal("configuration validation sent a prompt")
	}
	q := map[string]any{"command": f.command(pa.EditOperation, "unknown-field"), "settings": map[string]any{"expected_version": g.Bot.Version, "name": g.Bot.Name, "appearance": g.Bot.Appearance, "instructions": g.Bot.Instructions.Text, "new_unrecognized_setting": true}}
	paAssertHTTPFailure(t, f.request(t, "PATCH", "/v1/project-assistants/"+f.key.BotID, f.local, q), 400, pa.Invalid)
}

func TestRefusedNativeConfigurationNeverSendsPrompt(t *testing.T) {
	f, record := memorySettingsFixture(t, true)
	g := f.get(t, f.local)
	q := contract.ProjectAssistantEditRequest{Command: f.command(pa.EditOperation, "native-refuses-fast"), Settings: contract.ProjectAssistantEditSettings{ExpectedVersion: g.Bot.Version, Name: g.Bot.Name, Appearance: g.Bot.Appearance, Instructions: g.Bot.Instructions.Text, RuntimeSettings: &contract.ProjectAssistantRuntimeSettings{Model: "synthetic-large", Options: map[string]string{"fast": "true"}}}}
	paAssertHTTPFailure(t, f.request(t, "PATCH", "/v1/project-assistants/"+f.key.BotID, f.local, q), 501, pa.Unsupported)
	if now := f.get(t, f.local); now.Bot.Version != g.Bot.Version {
		t.Fatal("refused native option changed saved settings")
	}
	x, err := store.New(f.s.store.ProjectAssistantDatabase()).Export(context.Background(), f.key)
	if err != nil || len(x.Turns) != 0 {
		t.Fatal("refused native setting created a turn", err)
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"method":"session/prompt"`) {
		t.Fatal("native configuration refusal still sent a prompt")
	}
}
