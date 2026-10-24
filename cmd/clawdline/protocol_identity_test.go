package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func protocolIdentityFile(t *testing.T, raw string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "identity.json")
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestProtocolIdentityResolver(t *testing.T) {
	path := protocolIdentityFile(t, `{"assistant":"antigravity","conversation_id":"native-exact"}`)
	env := map[string]string{"CLAWDLINE_PROTOCOL_SESSION_FILE": path}
	id, name, err := conversationFromEnv(envOf(env))
	if err != nil || id != "native-exact" || conversationAssistant[name] != "antigravity" {
		t.Fatalf("%q %q %v", id, name, err)
	}
	env["CODEX_THREAD_ID"] = "parent-codex"
	if _, _, err := conversationFromEnv(envOf(env)); err == nil {
		t.Fatal("different provider identity accepted")
	}
	delete(env, "CODEX_THREAD_ID")
	env["CLAUDE_CODE_SESSION_ID"] = "parent-claude"
	if _, _, err := conversationFromEnv(envOf(env)); err == nil {
		t.Fatal("different Claude identity accepted")
	}
}

func TestProtocolIdentityInvalidNeverBorrowsParent(t *testing.T) {
	for _, raw := range []string{"", `{`, `null`, `{"assistant":"codex","conversation_id":"native"}`, `{"assistant":"unknown","conversation_id":"native"}`, `{"assistant":"antigravity","conversation_id":" "}`, `{"assistant":"antigravity"}`, strings.Repeat("x", brokerTokenLimit+1), `{"assistant":"antigravity","conversation_id":"native"} {}`} {
		path := protocolIdentityFile(t, raw)
		if _, _, err := conversationFromEnv(envOf(map[string]string{"CLAWDLINE_PROTOCOL_SESSION_FILE": path, "CODEX_THREAD_ID": "parent"})); err == nil || !strings.Contains(err.Error(), "CLAWDLINE_PROTOCOL_SESSION_FILE") {
			t.Fatalf("accepted %q: %v", raw, err)
		}
	}
	dir := t.TempDir()
	regular := protocolIdentityFile(t, `{"assistant":"antigravity","conversation_id":"native"}`)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(regular, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(dir, "missing"), dir, link, "relative.json"} {
		if _, _, err := conversationFromEnv(envOf(map[string]string{"CLAWDLINE_PROTOCOL_SESSION_FILE": path, "CODEX_THREAD_ID": "parent"})); err == nil {
			t.Fatalf("accepted %s", path)
		}
	}
}

func TestProtocolIdentitySharedCommandsUseExactNativeID(t *testing.T) {
	path := protocolIdentityFile(t, `{"assistant":"antigravity","conversation_id":"native-exact"}`)
	env := envOf(map[string]string{"CLAWDLINE_PROTOCOL_SESSION_FILE": path})
	s, b := newStandIn(t, func(r *http.Request) (int, string) {
		if strings.HasSuffix(r.URL.Path, "/whoami") {
			if r.URL.Query().Get("conversation_id") != "native-exact" {
				t.Error("wrong whoami identity")
			}
			return 200, `{"terminal_id":"owned:acp:synthetic"}`
		}
		return 200, `{"ok":true,"open_todos":[]}`
	})
	var out, errs bytes.Buffer
	if code := reportSession(&out, &errs, b, "Synthetic delivery", "", "", env); code != 0 {
		t.Fatalf("report %d %s", code, errs.String())
	}
	if code := sessionTodo(&out, &errs, b, "list", nil, "", "", env); code != 0 {
		t.Fatalf("todo %d %s", code, errs.String())
	}
	found := false
	for _, r := range s.requests() {
		if strings.Contains(r.EscapedPath, "/session-todos/native-exact") {
			found = true
		}
	}
	if !found {
		t.Fatal("shared todo resolver lost native identity")
	}
	// Explicit IDs retain their existing override, including a broken pointer.
	bad := envOf(map[string]string{"CLAWDLINE_PROTOCOL_SESSION_FILE": "/missing"})
	if code := sessionTodo(&out, &errs, b, "list", nil, "explicit-native", "", bad); code != 0 {
		t.Fatalf("explicit conversation %d %s", code, errs.String())
	}
}

func TestProtocolIdentityDispatchUsesExistingRootContract(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "environment", true: "explicit"}[explicit], func(t *testing.T) {
			path := protocolIdentityFile(t, `{"assistant":"antigravity","conversation_id":"native-exact"}`)
			w := &dispatchWorld{root: t.TempDir(), generations: []string{"aaaaaaaaaaaaaaaa"}}
			s, b := w.daemon(t, func(int) (int, string) { return 200, dispatchedAnswer("spawning") })
			o := testDispatchOptions()
			o.Assistant = "codex"
			if explicit {
				o.Conversation = "native-exact"
			}
			var out, errs bytes.Buffer
			if code := dispatchTask(&out, &errs, b, o, testDispatchEnv(map[string]string{"CLAWDLINE_PROTOCOL_SESSION_FILE": path})); code != 0 {
				t.Fatalf("dispatch %d %s", code, errs.String())
			}
			if len(postBodies(s)) != 1 {
				t.Fatal("stand-in received no dispatch")
			}
			raw, err := os.ReadFile(filepath.Join(w.root, dispatchID, "task.json"))
			if err != nil {
				t.Fatal(err)
			}
			var task struct {
				Root struct {
					Conversation string `json:"session_id"`
					Assistant    string `json:"assistant"`
				} `json:"root"`
			}
			if json.Unmarshal(raw, &task) != nil || task.Root.Conversation != "native-exact" || task.Root.Assistant != "antigravity" {
				t.Fatalf("root contract %s", raw)
			}
		})
	}
}

func TestProtocolIdentityDispatchRefusesParentAndHonorsExplicitRoot(t *testing.T) {
	path := protocolIdentityFile(t, `{"assistant":"antigravity","conversation_id":"native-exact"}`)
	w := &dispatchWorld{root: t.TempDir(), generations: []string{"aaaaaaaaaaaaaaaa"}}
	_, b := w.daemon(t, func(int) (int, string) { return 200, dispatchedAnswer("spawning") })
	o := testDispatchOptions()
	o.Assistant = "codex"
	var out, errs bytes.Buffer
	if code := dispatchTask(&out, &errs, b, o, testDispatchEnv(map[string]string{"CLAWDLINE_PROTOCOL_SESSION_FILE": path, "CODEX_THREAD_ID": "parent-codex"})); code != 2 || w.posts != 0 {
		t.Fatalf("parent dispatched: %d %s", code, errs.String())
	}
	o.Conversation = "explicit-native"
	o.RootAssistant = "antigravity"
	if code := dispatchTask(&out, &errs, b, o, testDispatchEnv(map[string]string{"CLAWDLINE_PROTOCOL_SESSION_FILE": "/missing"})); code != 0 {
		t.Fatalf("explicit root refused: %d %s", code, errs.String())
	}
}
