package transcript

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestRemoteControlRegistryEnrichmentRequiresALiveLocalConversation(t *testing.T) {
	started := time.Date(2026, 10, 1, 12, 0, 0, 0, time.Local)
	r := ClaudeRegistry{PID: 42, SessionID: "11111111-1111-4111-8111-111111111111", CWD: "/work/project",
		BridgeSessionID: "session_fixture", Entrypoint: "sdk-cli", PIDDomain: runtime.GOOS,
		ProcStart: started.Format("Mon Jan _2 15:04:05 2006"), Status: "waiting", Tmux: "launcher:@1.%2"}
	got, ok := claudeRemoteSession(42, r, true, started)
	if !ok || got.Backend != session.BackendClaudeRemote || got.TTY != "" || got.ConversationID != r.SessionID ||
		got.State != session.StateWaiting || got.Evidence != session.EvidenceRegistry {
		t.Fatalf("live remote = %+v %v", got, ok)
	}
	for _, change := range []func(*ClaudeRegistry){
		func(x *ClaudeRegistry) { x.PID++ },
		func(x *ClaudeRegistry) { x.BridgeSessionID = "" },
		func(x *ClaudeRegistry) { x.Entrypoint = "cli" },
		func(x *ClaudeRegistry) { x.ProcStart = started.Add(-time.Hour).Format("Mon Jan _2 15:04:05 2006") },
		func(x *ClaudeRegistry) { x.PIDDomain = "other" },
		func(x *ClaudeRegistry) { x.SessionID = "../outside" },
		func(x *ClaudeRegistry) { x.CWD = "relative" },
	} {
		bad := r
		change(&bad)
		if row, ok := claudeRemoteSession(42, bad, true, started); ok {
			t.Fatalf("invalid registry became a live conversation: %+v", row)
		}
	}
	if _, ok := claudeRemoteSession(42, r, true, time.Time{}); ok {
		t.Fatal("a dead/unreadable process became a live conversation")
	}
}

func TestRemoteControlReadsItsLocalUUIDInTheConfiguredStore(t *testing.T) {
	h, home := claudeHost(t)
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	id := "11111111-1111-4111-8111-111111111111"
	path := ClaudePath(home, "/work/project", id)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"type":"custom-title","customTitle":"Remote work"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sessions", "42.json"), []byte(`{"pid":42,"sessionId":"`+id+`","bridgeSessionId":"session_other_id","cwd":"/work/project","status":"idle","tmux":"launcher:@1.%2"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h.Refresh()
	row := session.Session{PID: 42, Backend: session.BackendClaudeRemote, Assistant: session.AssistantClaude, ConversationID: id, CWD: "/work/project"}
	identity, ok := h.ForSession(context.Background(), row)
	if !ok || identity.ConversationID != id || identity.Label != "Remote work" || h.RecordPath(row) != path {
		t.Fatalf("wrong conversation file or identity: %+v %v %q", identity, ok, h.RecordPath(row))
	}
}
