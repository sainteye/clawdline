package projectassistantacp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/botcapabilities"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// This peer is the test executable, invoked through the trusted source connector
// definition. It never starts a provider or contacts a network endpoint.
func TestOwnedConnectorStdioPeer(t *testing.T) {
	args := os.Args
	at := -1
	for n, arg := range args {
		if arg == "owned-connector-peer" {
			at = n
			break
		}
	}
	if at < 0 {
		return
	}
	if len(args) != at+3 {
		os.Exit(11)
	}
	state, mode := args[at+1], args[at+2]
	appendRecord := func(name, text string) {
		f, e := os.OpenFile(filepath.Join(state, name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			os.Exit(12)
		}
		_, _ = io.WriteString(f, text+"\n")
		_ = f.Close()
	}
	appendRecord("starts", "started")
	if mode == "credential" && os.Getenv("MCP_CONNECTOR_TOKEN") != "OWNED_CONNECTOR_CANARY" {
		os.Exit(13)
	}
	ready, initialized := false, false
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024), 1<<20)
	for scanner.Scan() {
		var f frame
		if json.Unmarshal(scanner.Bytes(), &f) != nil {
			os.Exit(14)
		}
		appendRecord("methods", f.Method)
		var result any
		switch f.Method {
		case "initialize":
			initialized = true
			result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "owned-fixture", "version": "1"}}
		case "notifications/initialized":
			if !initialized {
				os.Exit(15)
			}
			ready = true
			continue
		case "tools/list":
			if !ready {
				os.Exit(16)
			}
			result = map[string]any{"tools": []any{map[string]any{"name": "fixture_echo", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			if !ready {
				os.Exit(17)
			}
			appendRecord("effects", "effect")
			if mode == "eof" {
				os.Exit(0)
			}
			if mode == "hold" {
				_ = os.WriteFile(filepath.Join(state, "effect-ready"), []byte("ready"), 0600)
				for {
					if _, e := os.Stat(filepath.Join(state, "release-effect")); e == nil {
						break
					}
					time.Sleep(time.Millisecond)
				}
			}
			result = map[string]any{"content": []any{map[string]string{"type": "text", "text": "owned stdio result"}}}
		default:
			os.Exit(18)
		}
		if e := json.NewEncoder(os.Stdout).Encode(frame{JSONRPC: "2.0", ID: f.ID, Result: raw(result)}); e != nil {
			os.Exit(19)
		}
	}
	os.Exit(0)
}

func ownedConnectorFixture(t *testing.T, mode string) (OwnedMCP, string, *atomic.Bool) {
	t.Helper()
	state := t.TempDir()
	if e := os.Mkdir(filepath.Join(state, "project-assistants"), 0700); e != nil {
		t.Fatal(e)
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	fence := testFence()
	bot := pa.Bot{Key: fence.Key, Project: fence.Project, Agent: pa.Codex}
	definition := botcapabilities.Connector{ID: "owned", Name: "owned-fixture", Executable: exe, Arguments: []string{"-test.run=^TestOwnedConnectorStdioPeer$", "--", "owned-connector-peer", state, mode}, Project: bot.Project, Agent: bot.Agent, Enabled: true}
	if mode == "credential" {
		definition.CredentialEnv = "MCP_CONNECTOR_TOKEN"
	}
	data, _ := json.Marshal([]botcapabilities.Connector{definition})
	if e = os.WriteFile(filepath.Join(state, "project-assistants", "connectors.json"), data, 0600); e != nil {
		t.Fatal(e)
	}
	var revoked atomic.Bool
	inventory := botcapabilities.Inventory{State: state, Credential: func(context.Context, pa.Bot, pa.ExtensionRef) ([]byte, error) {
		return []byte("OWNED_CONNECTOR_CANARY"), nil
	}}
	catalog, e := inventory.Catalog(context.Background(), bot, "")
	if e != nil || len(catalog.Items) != 1 {
		t.Fatal(catalog, e)
	}
	ref := pa.ExtensionRef{ID: catalog.Items[0].ID, Fingerprint: catalog.Items[0].Fingerprint}
	bot.Extensions.Connectors = []pa.ExtensionRef{ref}
	extensions, e := inventory.Resolve(context.Background(), bot, "")
	if e != nil {
		t.Fatal(e)
	}
	tools := extensionTestTools{check: func() error {
		if revoked.Load() {
			return refusal(pa.Revoked, "owned_fixture")
		}
		_, e := inventory.Resolve(context.Background(), bot, "")
		return e
	}}
	limits := testLimits()
	limits.CleanupTimeout = 3 * time.Second
	client := &Client{l: limits, rpcSlots: make(chan struct{}, limits.RPCPending)}
	lease, e := client.openMCP(context.Background(), app.OpenRequest{Directory: state, Binding: pa.ExecutionBinding{Fence: fence}, Tools: tools, Extensions: extensions})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if e := lease.Revoke(ctx); e != nil {
			t.Error(e)
		}
	})
	return lease, state, &revoked
}

func ownedConnectorRequest(t *testing.T, lease OwnedMCP, body string) (int, string) {
	t.Helper()
	r, e := http.NewRequest("POST", lease.URL+"/extensions/0", strings.NewReader(body))
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("Authorization", "Bearer "+lease.BearerToken)
	r.Header.Set("Content-Type", "application/json")
	response, e := http.DefaultClient.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	data, e := io.ReadAll(response.Body)
	if e != nil {
		t.Fatal(e)
	}
	return response.StatusCode, string(data)
}

func ownedConnectorHandshake(t *testing.T, lease OwnedMCP) {
	t.Helper()
	if code, body := ownedConnectorRequest(t, lease, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"fixture","version":"1"}}}`); code != 200 || !strings.Contains(body, "2025-06-18") {
		t.Fatal(code, body)
	}
	if code, body := ownedConnectorRequest(t, lease, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); code != 202 {
		t.Fatal(code, body)
	}
	if code, body := ownedConnectorRequest(t, lease, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`); code != 200 || !strings.Contains(body, "fixture_echo") {
		t.Fatal(code, body)
	}
}

const ownedConnectorCall = `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"fixture_echo","arguments":{}}}`

func TestSelectedStdioConnectorUsesOwnedProcessAndStopsBeforeRevokedCall(t *testing.T) {
	lease, state, revoked := ownedConnectorFixture(t, "credential")
	ownedConnectorHandshake(t, lease)
	if code, body := ownedConnectorRequest(t, lease, ownedConnectorCall); code != 200 || !strings.Contains(body, "owned stdio result") {
		t.Fatal(code, body)
	}
	revoked.Store(true)
	if code, body := ownedConnectorRequest(t, lease, ownedConnectorCall); code != 403 {
		t.Fatal(code, body)
	}
	effects, e := os.ReadFile(filepath.Join(state, "effects"))
	if e != nil || string(effects) != "effect\n" {
		t.Fatal("revoked call reached connector", string(effects), e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e = lease.Revoke(ctx); e != nil {
		t.Fatal(e)
	}
	entries, e := os.ReadDir(filepath.Join(state, "project-assistants", "extension-executions"))
	if e != nil || len(entries) != 0 {
		t.Fatal("owned process reservation survived proven cleanup", entries, e)
	}
}

func TestStdioDisconnectAfterEffectNeverRestartsOrReplays(t *testing.T) {
	lease, state, _ := ownedConnectorFixture(t, "eof")
	ownedConnectorHandshake(t, lease)
	for n := 0; n < 2; n++ {
		if code, body := ownedConnectorRequest(t, lease, ownedConnectorCall); code != 502 || !strings.Contains(body, string(pa.OutcomeUnknown)) {
			t.Fatal(code, body)
		}
	}
	for _, name := range []string{"starts", "effects"} {
		data, e := os.ReadFile(filepath.Join(state, name))
		if e != nil || strings.Count(string(data), "\n") != 1 {
			t.Fatal("unknown effect replayed or restarted", name, string(data), e)
		}
	}
}

func TestStdioChangedSourceDefinitionBlocksNextCall(t *testing.T) {
	lease, state, _ := ownedConnectorFixture(t, "ordinary")
	ownedConnectorHandshake(t, lease)
	path := filepath.Join(state, "project-assistants", "connectors.json")
	data, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var definitions []botcapabilities.Connector
	if e = json.Unmarshal(data, &definitions); e != nil {
		t.Fatal(e)
	}
	definitions[0].Arguments = append(definitions[0].Arguments, "changed-source-revision")
	data, _ = json.Marshal(definitions)
	if e = os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	if code, body := ownedConnectorRequest(t, lease, ownedConnectorCall); code != 403 {
		t.Fatal("changed source definition retained authority", code, body)
	}
	if _, e = os.Stat(filepath.Join(state, "effects")); !os.IsNotExist(e) {
		t.Fatal("stale source reached connector effect", e)
	}
}

func TestQueuedStdioCallRechecksAuthorityBeforeWriting(t *testing.T) {
	state := t.TempDir()
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	var revoked atomic.Bool
	tools := extensionTestTools{check: func() error {
		if revoked.Load() {
			return refusal(pa.Revoked, "queued_fixture")
		}
		return nil
	}}
	limits := testLimits()
	limits.CleanupTimeout = 3 * time.Second
	connector, e := startConnector(context.Background(), app.OpenRequest{Directory: state, Binding: pa.ExecutionBinding{Fence: testFence()}, Tools: tools, Extensions: app.NativeExtensions{State: state}}, app.NativeConnector{Ref: pa.ExtensionRef{ID: "connector:queued", Fingerprint: strings.Repeat("a", 64)}, Executable: exe, Arguments: []string{"-test.run=^TestOwnedConnectorStdioPeer$", "--", "owned-connector-peer", state, "ordinary"}}, limits)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if e := connector.close(ctx); e != nil {
			t.Error(e)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, body := range []string{`{"jsonrpc":"2.0","id":1,"method":"initialize"}`, `{"jsonrpc":"2.0","method":"notifications/initialized"}`} {
		if _, e = connector.exchange(ctx, []byte(body)); e != nil {
			t.Fatal(e)
		}
	}
	connector.gate <- struct{}{}
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		_, e := connector.exchange(ctx, []byte(ownedConnectorCall))
		done <- e
	}()
	<-started
	revoked.Store(true)
	<-connector.gate
	if e = <-done; pa.Code(e) != pa.Revoked {
		t.Fatal("queued call did not recheck authority before writing", e)
	}
	if _, e = os.Stat(filepath.Join(state, "effects")); !os.IsNotExist(e) {
		t.Fatal("queued revoked call reached effect", e)
	}
}

func TestStdioUnknownCleanupQuarantinesAndKeepsReservation(t *testing.T) {
	state := t.TempDir()
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	var quarantined atomic.Bool
	tools := extensionTestTools{check: func() error { return nil }}
	limits := testLimits()
	limits.CleanupTimeout = 3 * time.Second
	connector, e := startConnector(context.Background(), app.OpenRequest{Directory: state, Binding: pa.ExecutionBinding{Fence: testFence()}, Tools: tools, Extensions: app.NativeExtensions{State: state}}, app.NativeConnector{Ref: pa.ExtensionRef{ID: "connector:cleanup", Fingerprint: strings.Repeat("a", 64)}, Executable: exe, Arguments: []string{"-test.run=^TestOwnedConnectorStdioPeer$", "--", "owned-connector-peer", state, "ordinary"}, Quarantine: func(context.Context) error { quarantined.Store(true); return nil }}, limits)
	if e != nil {
		t.Fatal(e)
	}
	// The subprocess is real; only its cleanup census fails. Its direct reap
	// receipt does not establish that every possible descendant was observed.
	connector.p.mu.Lock()
	connector.p.censusOverride = func(context.Context) ([]processIdentity, error) {
		return nil, errors.New("owned_fixture_inventory_denied")
	}
	connector.p.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e = connector.close(ctx); pa.Code(e) != pa.ExecutionEffectUnknown {
		t.Fatal("ambiguous cleanup claimed success", e)
	}
	if !quarantined.Load() {
		t.Fatal("uncertain cleanup discarded credential quarantine")
	}
	if _, e = os.Stat(filepath.Join(connector.owner, "owner.json")); e != nil {
		t.Fatal("uncertain cleanup removed durable reservation", e)
	}
	if !waitOwnedDone(ctx, connector.p.done) {
		t.Fatal("owned direct subprocess was not reaped")
	}
}

func TestStdioRevocationAfterEffectSuppressesPublication(t *testing.T) {
	lease, state, revoked := ownedConnectorFixture(t, "hold")
	ownedConnectorHandshake(t, lease)
	type result struct {
		code int
		body string
	}
	done := make(chan result, 1)
	go func() { code, body := ownedConnectorRequest(t, lease, ownedConnectorCall); done <- result{code, body} }()
	deadline := time.Now().Add(time.Second)
	for {
		if _, e := os.Stat(filepath.Join(state, "effect-ready")); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("connector effect not admitted")
		}
		time.Sleep(time.Millisecond)
	}
	revoked.Store(true)
	if e := os.WriteFile(filepath.Join(state, "release-effect"), []byte("release"), 0600); e != nil {
		t.Fatal(e)
	}
	select {
	case r := <-done:
		if r.code != 409 || strings.Contains(r.body, "owned stdio result") {
			t.Fatal("revoked effect reply escaped", r)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("connector call did not finish")
	}
	if code, _ := ownedConnectorRequest(t, lease, ownedConnectorCall); code != 403 {
		t.Fatal(code)
	}
	effects, e := os.ReadFile(filepath.Join(state, "effects"))
	if e != nil || string(effects) != "effect\n" {
		t.Fatal("late revocation permitted another effect", string(effects), e)
	}
}
