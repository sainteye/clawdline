package projectassistantacp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// initializePersisted opens a scripted peer and runs initialize with q. The
// peer answers session/new and session/load with the IDs given here.
func initializePersisted(t *testing.T, q app.OpenRequest, newID, loadID string, env ...string) (error, map[string]any) {
	t.Helper()
	err, sent, _ := initializeWith(t, q, newID, loadID, env...)
	return err, sent
}

func initializeWith(t *testing.T, q app.OpenRequest, newID, loadID string, env ...string) (error, map[string]any, *Client) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows native supervisor is explicitly unavailable")
	}
	if inventoryUnavailable() {
		t.Skip("process inventory is unavailable; safe startup admission must refuse")
	}
	params := filepath.Join(t.TempDir(), "params.json")
	config := peerConfiguration(t, "configure-claude")
	config.Environment = append(config.Environment, "CLAWDLINE_ACP_NEW_ID="+newID, "CLAWDLINE_ACP_LOAD_ID="+loadID, "CLAWDLINE_ACP_PARAMS_FILE="+params)
	config.Environment = append(config.Environment, env...)
	l := testLimits()
	p, err := StartProcess(context.Background(), ProcessOptions{Configuration: config, Directory: t.TempDir(), Limits: l})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewClient(p, l, testFence(), func(context.Context, app.ProtocolEvent) error { return nil }, func([]byte) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = c.Close(ctx)
	})
	q.Directory = t.TempDir()
	err = c.initialize(context.Background(), q)
	var sent map[string]any
	if data, e := os.ReadFile(params); e == nil {
		var f struct {
			Params map[string]any `json:"params"`
		}
		_ = json.Unmarshal(data, &f)
		sent = f.Params
	}
	return err, sent, c
}

func persisted(f pa.Fence, resume bool, id string) app.OpenRequest {
	return app.OpenRequest{Bot: pa.Bot{Key: f.Key, Project: f.Project, Agent: pa.ClaudeCode}, Binding: pa.ExecutionBinding{Fence: f, Agent: pa.ClaudeCode, NativeSessionID: id}, Resume: resume, PersistNativeSession: true}
}

func persistFlag(sent map[string]any) any {
	meta, _ := sent["_meta"].(map[string]any)
	claude, _ := meta["claudeCode"].(map[string]any)
	options, _ := claude["options"].(map[string]any)
	return options["persistSession"]
}

func TestACPPersistedSessionRequiresAnOwnedNativeID(t *testing.T) {
	f := testFence()
	const owned = "a1000001-1111-4111-8111-111111111111"
	const other = "b2000002-2222-4222-8222-222222222222"

	e, sent := initializePersisted(t, persisted(f, false, ""), owned, "")
	if e != nil || persistFlag(sent) != true {
		t.Fatalf("owned new session was not persisted: %v %v", e, persistFlag(sent))
	}
	if e, _ := initializePersisted(t, persisted(f, false, ""), "scripted-session", ""); pa.Code(e) != pa.Invalid {
		t.Fatalf("a non-UUID native ID was accepted for persistence: %v", e)
	}
	if e, sent := initializePersisted(t, persisted(f, true, "../"+owned), "", owned); pa.Code(e) != pa.ScopeDenied || sent != nil {
		t.Fatalf("a malformed resume ID reached the peer: %v %v", e, sent)
	}
	if e, _ := initializePersisted(t, persisted(f, true, owned), "", other); pa.Code(e) != pa.ScopeDenied {
		t.Fatalf("load answered with another session and was accepted: %v", e)
	}
	if e, sent := initializePersisted(t, persisted(f, true, owned), "", owned); e != nil || sent["sessionId"] != owned {
		t.Fatalf("owned load failed: %v %v", e, sent)
	}
	// Without the host-only flag the request stays non-persistent.
	plain := persisted(f, false, "")
	plain.PersistNativeSession = false
	if e, sent := initializePersisted(t, plain, "scripted-session", ""); e != nil || persistFlag(sent) != false {
		t.Fatalf("a plain session became persistent: %v %v", e, persistFlag(sent))
	}
}
