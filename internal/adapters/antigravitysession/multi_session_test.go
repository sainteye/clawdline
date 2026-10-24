//go:build darwin || linux

package antigravitysession

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// Two real subprocesses deliberately reuse the same tool ID. Only the exact
// native conversation and its owning pipe may determine where an update lands.
func TestParallelSessionPeer(t *testing.T) {
	if os.Getenv("CLW_PARALLEL_PEER") != "yes" {
		return
	}
	enc := json.NewEncoder(os.Stdout)
	native := "parallel-" + strconv.Itoa(os.Getpid())
	dir := os.Getenv("GEMINI_HOME")
	notify := func(update any) {
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": native, "update": update}})
	}
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				SessionID string `json:"sessionId"`
				Prompt    []struct {
					Text string `json:"text"`
				} `json:"prompt"`
			} `json:"params"`
		}
		if json.Unmarshal(scan.Bytes(), &req) != nil {
			os.Exit(30)
		}
		result := map[string]any{}
		switch req.Method {
		case "initialize":
			result["protocolVersion"] = 1
			result["agentCapabilities"] = map[string]any{"sessionCapabilities": map[string]any{"resume": map[string]any{}}}
		case "session/new":
			result["sessionId"] = native
			result["_meta"] = map[string]any{"antigravity": map[string]any{"tier": map[string]string{"id": "free-tier", "name": "Antigravity Starter Quota"}}}
		case "session/resume":
			native = req.Params.SessionID
			result["sessionId"] = native
			result["_meta"] = map[string]any{"antigravity": map[string]any{"tier": map[string]string{"id": "free-tier", "name": "Antigravity Starter Quota"}}}
		case "session/prompt":
			if req.Params.SessionID != native || len(req.Params.Prompt) != 1 {
				os.Exit(31)
			}
			text := req.Params.Prompt[0].Text
			notify(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "same-tool", "title": text, "kind": "read", "status": "in_progress"})
			for {
				if _, err := os.Stat(filepath.Join(dir, "release-"+native)); err == nil {
					break
				}
				time.Sleep(time.Millisecond)
			}
			notify(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "reply " + text}})
			notify(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "same-tool", "status": "completed"})
			result["stopReason"] = "end_turn"
		}
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}
	os.Exit(0)
}

func TestTwoActiveSessionsKeepMessagesToolsAndRestartIdentitySeparate(t *testing.T) {
	t.Setenv("CLW_PARALLEL_PEER", "yes")
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "antigravity-acp"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "antigravity-acp", "settings.json"), []byte(`{"useG1Credits":false,"auth":{"type":"oauth-personal"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Executable: exe, AccountDir: dir, StateDir: filepath.Join(dir, "state"), Args: []string{"-test.run=^TestParallelSessionPeer$"}}
	h := reopenHost(t, opts)
	a, err := h.Open(context.Background(), ports.OpenRequest{Cwd: dir, Name: "First"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.Open(context.Background(), ports.OpenRequest{Cwd: dir, Name: "Second"})
	if err != nil {
		t.Fatal("second same-project session refused:", err)
	}
	if a.ID == b.ID || a.ConversationID == b.ConversationID || a.PID == b.PID {
		t.Fatal("identities collided", a, b)
	}
	if _, err := h.Open(context.Background(), ports.OpenRequest{Cwd: dir}); err == nil {
		t.Fatal("third process exceeded capacity")
	}
	if n, _ := h.Stats(); n != 2 {
		t.Fatal("active process count", n)
	}
	for _, tc := range []struct {
		row  session.Session
		text string
	}{{a, "first-only"}, {b, "second-only"}} {
		if err := h.SendProtocol(context.Background(), tc.row, tc.text, nil); err != nil {
			t.Fatal(err)
		}
	}
	waitParallelTool(t, h, a)
	waitParallelTool(t, h, b)
	if err := os.WriteFile(filepath.Join(dir, "release-"+b.ConversationID), nil, 0600); err != nil {
		t.Fatal(err)
	}
	idleHost(t, h, b)
	assertParallelTranscript(t, h, b, "second-only", "first-only", true)
	assertParallelTranscript(t, h, a, "first-only", "second-only", false)
	if err := os.WriteFile(filepath.Join(dir, "release-"+a.ConversationID), nil, 0600); err != nil {
		t.Fatal(err)
	}
	idleHost(t, h, a)
	assertParallelTranscript(t, h, a, "first-only", "second-only", true)
	stopHost(t, h)
	h = reopenHost(t, opts)
	if n, _ := h.Stats(); n != 0 {
		t.Fatal("history started processes", n)
	}
	for _, tc := range []struct {
		row        session.Session
		own, other string
	}{{a, "first-only", "second-only"}, {b, "second-only", "first-only"}} {
		assertParallelTranscript(t, h, tc.row, tc.own, tc.other, true)
		row, err := h.Resume(context.Background(), dir, tc.row.ConversationID)
		if err != nil || row.ID != tc.row.ID || row.ConversationID != tc.row.ConversationID {
			t.Fatal("resume changed identity", row, err)
		}
		if err := h.SendProtocol(context.Background(), row, tc.own+"-after-restart", nil); err != nil {
			t.Fatal(err)
		}
		idleHost(t, h, row)
		assertParallelTranscript(t, h, row, tc.own, tc.other, true)
	}
	if n, _ := h.Stats(); n != 2 {
		t.Fatal("two resumed processes not retained", n)
	}
	if err := h.Close(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if !h.OwnsProtocol(b) {
		t.Fatal("closing first hid second")
	}
	if err := h.SendProtocol(context.Background(), b, "second-only-after-close", nil); err != nil {
		t.Fatal(err)
	}
	idleHost(t, h, b)
	assertParallelTranscript(t, h, b, "second-only", "first-only", true)
}

func waitParallelTool(t *testing.T, h *Host, row session.Session) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		p, err := h.Transcript(row, 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range p.Entries {
			if e.Activity != nil {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("native tool update not observed", row.ID)
}

func assertParallelTranscript(t *testing.T, h *Host, row session.Session, own, other string, complete bool) {
	t.Helper()
	p, err := h.Transcript(row, 100)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(p.Entries)
	if !strings.Contains(string(raw), own) || strings.Contains(string(raw), other) {
		t.Fatal("conversation content crossed", string(raw))
	}
	if complete && !strings.Contains(string(raw), "reply "+own) {
		t.Fatal("native reply missing", string(raw))
	}
	tools := 0
	for _, e := range p.Entries {
		if e.Activity != nil && strings.Contains(e.Activity.Title, own) {
			tools++
			if (e.Activity.Status == "completed") != complete {
				t.Fatal("tool state crossed", string(raw))
			}
		}
	}
	if tools != 1 {
		t.Fatal("exact native tool row missing", string(raw))
	}
}
