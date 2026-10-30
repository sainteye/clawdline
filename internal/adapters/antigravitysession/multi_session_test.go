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
			if os.Getenv("CLW_PARALLEL_PERMISSION") == "yes" {
				_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": "shared-permission", "method": "session/request_permission", "params": map[string]any{
					"sessionId": native,
					"toolCall":  map[string]string{"toolCallId": "same-tool", "title": "Read synthetic file?"},
					"options": []any{
						map[string]string{"optionId": "allow-exact", "name": "Allow once", "kind": "allow_once"},
						map[string]string{"optionId": "reject-exact", "name": "Reject once", "kind": "reject_once"},
					},
				}})
				if !scan.Scan() {
					os.Exit(32)
				}
				if err := os.WriteFile(filepath.Join(dir, "permission-"+native), scan.Bytes(), 0600); err != nil {
					os.Exit(33)
				}
			}
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

func TestTwoActivePermissionOffersCannotAnswerEachOther(t *testing.T) {
	t.Setenv("CLW_PARALLEL_PEER", "yes")
	t.Setenv("CLW_PARALLEL_PERMISSION", "yes")
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
	h := reopenHost(t, Options{Executable: exe, AccountDir: dir, StateDir: filepath.Join(dir, "state"), Args: []string{"-test.run=^TestParallelSessionPeer$"}})
	a, err := h.Open(context.Background(), ports.OpenRequest{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.Open(context.Background(), ports.OpenRequest{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []session.Session{a, b} {
		if err := os.WriteFile(filepath.Join(dir, "release-"+row.ConversationID), nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := h.Send(context.Background(), row, "identical prompt"); err != nil {
			t.Fatal(err)
		}
	}
	ma := parallelPermission(t, h, a)
	mb := parallelPermission(t, h, b)
	fa, fb := session.MenuFingerprint(*ma.Menu), session.MenuFingerprint(*mb.Menu)
	if ma.Menu.Question != mb.Menu.Question || fa == fb {
		t.Fatal("independent identical offers have no exact identity")
	}
	if err := h.SelectProtocol(context.Background(), b, 1, fa, nil); err == nil {
		t.Fatal("first offer answered the second conversation")
	}
	if _, err := os.Stat(filepath.Join(dir, "permission-"+b.ConversationID)); !os.IsNotExist(err) {
		t.Fatal("crossed answer reached native pipe")
	}
	if err := h.SelectProtocol(context.Background(), a, 1, fa, nil); err != nil {
		t.Fatal(err)
	}
	idleHost(t, h, a)
	if err := h.SelectProtocol(context.Background(), a, 1, fa, nil); err == nil {
		t.Fatal("duplicate answer accepted")
	}
	still := parallelPermission(t, h, b)
	if session.MenuFingerprint(*still.Menu) != fb {
		t.Fatal("first answer changed second offer")
	}
	if err := h.SelectProtocol(context.Background(), b, 2, fb, nil); err != nil {
		t.Fatal(err)
	}
	idleHost(t, h, b)
	for _, tc := range []struct {
		row    session.Session
		option string
	}{{a, "allow-exact"}, {b, "reject-exact"}} {
		raw, err := os.ReadFile(filepath.Join(dir, "permission-"+tc.row.ConversationID))
		if err != nil {
			t.Fatal(err)
		}
		var reply struct {
			ID     string `json:"id"`
			Result struct {
				Outcome struct {
					Outcome  string `json:"outcome"`
					OptionID string `json:"optionId"`
				} `json:"outcome"`
			} `json:"result"`
		}
		if json.Unmarshal(raw, &reply) != nil || reply.ID != "shared-permission" || reply.Result.Outcome.Outcome != "selected" || reply.Result.Outcome.OptionID != tc.option {
			t.Fatal("wrong native reply", string(raw))
		}
	}
}

func parallelPermission(t *testing.T, h *Host, row session.Session) session.Session {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		inv, err := h.Inventory(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, got := range inv.Sessions {
			if got.ID == row.ID && got.Menu != nil {
				return got
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("native offer was not shown for exact session", row.ID)
	return session.Session{}
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

// Explicitly opened conversations are not limited to the pilot's two workers.
func TestThreeActiveSessionsCanOpenAndResumeWithoutReplacingEachOther(t *testing.T) {
	h, first, opts := persistentFixture(t)
	rows := []session.Session{first}
	for len(rows) < 3 {
		row, err := h.Open(context.Background(), ports.OpenRequest{Cwd: opts.AccountDir})
		if err != nil {
			t.Fatalf("conversation %d refused: %v", len(rows)+1, err)
		}
		for _, previous := range rows {
			if row.ID == previous.ID || row.ConversationID == previous.ConversationID || row.PID == previous.PID {
				t.Fatal("identity reused")
			}
		}
		rows = append(rows, row)
	}
	if n, _ := h.Stats(); n != 3 {
		t.Fatal("active conversations", n)
	}
	stopHost(t, h)
	h = reopenHost(t, opts)
	for _, previous := range rows {
		row, err := h.Resume(context.Background(), previous.CWD, previous.ConversationID)
		if err != nil || row.ID != previous.ID || row.ConversationID != previous.ConversationID {
			t.Fatal("resume failed", err)
		}
	}
	if n, _ := h.Stats(); n != 3 {
		t.Fatal("resumed conversations", n)
	}
}
