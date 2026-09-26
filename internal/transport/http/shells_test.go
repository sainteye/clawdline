package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestShellPathSplitsBeforeItDecodes(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/v1/sessions/%25fixture/shells/b0aau3e6s?bytes=4096", nil)
	sessionID, shellID, ok := shellPath(request)
	if !ok || sessionID != "%fixture" || shellID != "b0aau3e6s" {
		t.Fatalf("got session=%q shell=%q ok=%v", sessionID, shellID, ok)
	}
	for _, path := range []string{
		"/v1/sessions/root/shells/../other",
		"/v1/sessions/root/shells/",
		"/v1/sessions/root/shells",
		"/v1/sessions/root/shells/a/b",
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		if readablePath(request.URL.EscapedPath()) {
			if _, _, ok := shellPath(request); ok {
				t.Fatalf("unsafe path %q was accepted", path)
			}
		}
	}
}

// The Shell panel's read, end to end from the route: a Claude session whose
// transcript announced a background command, and the output folder Claude Code
// keeps for it under /tmp/claude-<uid>/<project>/<session>/tasks.
func TestTheShellRouteServesAnnouncedCommandsOnly(t *testing.T) {
	t.Setenv("CLAWDLINE_NEXT_OWN_SESSIONS", "1")
	home := t.TempDir()
	work := t.TempDir()
	const conv = "5a1d0c7e-0000-4000-8000-0000000051e1"
	record := transcript.ClaudePath(home, work, conv)
	if err := os.MkdirAll(filepath.Dir(record), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"call1","name":"Bash","input":{"command":"npm run build","description":"Build the console","run_in_background":true}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"call1","content":"Command running in background with ID: b0aau3e6s. Output is being written to: /tmp/x"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"call2","name":"Bash","input":{"command":"make","run_in_background":true}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"call2","content":"Command running in background with ID: b1oas8ao7"}]}}`,
	}
	if err := os.WriteFile(record, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	host := transcript.NewHost()
	host.Home = home
	host.ShellFiles().Root = t.TempDir()
	tasks := host.ShellFiles().Folder(record)
	if err := os.MkdirAll(tasks, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(tasks, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("b0aau3e6s.output", "vite v7 building\n")
	write("b1oas8ao7.output", "built\n[exited with code 0]\n")
	write("b2unknown.output", "a foreground leftover\n")
	write("big00000.output", "")

	reading := session.Inventory{Provenance: "ps", Complete: true, Sessions: []session.Session{
		{ID: "%71", TTY: "ttys071", Assistant: session.AssistantClaude, CWD: work, ConversationID: conv},
		{ID: "%72", TTY: "ttys072", Assistant: session.AssistantCodex, ConversationID: conv},
	}}
	s := &Server{inventory: app.Inventory{Process: todoProcesses{reading}, Identity: host}}
	get := func(target, method string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, nil)
		rec := httptest.NewRecorder()
		sessionID, shellID, ok := shellPath(req)
		if !ok {
			rec.Code = -1
			return rec
		}
		s.sessionShellRoute(rec, req, sessionID, shellID)
		return rec
	}
	decode := func(rec *httptest.ResponseRecorder) contract.ShellOutputReply {
		t.Helper()
		var out contract.ShellOutputReply
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		return out
	}

	running := decode(get("/v1/sessions/%2571/shells/b0aau3e6s", http.MethodGet))
	if running.Ended || running.Text != "vite v7 building\n" || running.Shell.Command != "npm run build" ||
		running.Shell.What != "Build the console" || running.Shell.Doing != "vite v7 building" || running.Signature == "" {
		t.Fatalf("running: %+v", running)
	}
	ended := decode(get("/v1/sessions/%2571/shells/b1oas8ao7", http.MethodGet))
	if !ended.Ended || ended.Shell.Command != "make" || ended.Shell.Doing != "" {
		t.Fatalf("ended: %+v", ended)
	}
	// The window is the reader's to choose inside the bounds, and a bad one is
	// the default rather than a refusal.
	write("b0aau3e6s.output", strings.Repeat(strings.Repeat("y", 99)+"\n", 30000))
	for target, most := range map[string]int{
		"/v1/sessions/%2571/shells/b0aau3e6s":                 transcript.ShellOutputDefault,
		"/v1/sessions/%2571/shells/b0aau3e6s?bytes=10":        transcript.ShellOutputFloor,
		"/v1/sessions/%2571/shells/b0aau3e6s?bytes=999999999": transcript.MaxShellOutput,
		"/v1/sessions/%2571/shells/b0aau3e6s?bytes=junk":      transcript.ShellOutputDefault,
	} {
		out := decode(get(target, http.MethodGet))
		if len(out.Text) > most || len(out.Text) < most-100 || !out.Truncated {
			t.Errorf("%s: %d bytes (truncated=%v), want about %d", target, len(out.Text), out.Truncated, most)
		}
	}

	for _, target := range []string{
		"/v1/sessions/%2571/shells/b2unknown",        // never announced
		"/v1/sessions/%2571/shells/big00000",         // same
		"/v1/sessions/%2571/shells/b0aau3e6s.output", // not an id
		"/v1/sessions/%2571/shells/..%2Foutside",     // an escape, encoded
		"/v1/sessions/%2572/shells/b0aau3e6s",        // a Codex session keeps no such files
	} {
		rec := get(target, http.MethodGet)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `"not_found"`) {
			t.Errorf("%s: %d %s", target, rec.Code, rec.Body)
		}
	}
	if rec := get("/v1/sessions/%2599/shells/b0aau3e6s", http.MethodGet); rec.Code != http.StatusNotFound {
		t.Errorf("a session a complete reading does not hold: %d %s", rec.Code, rec.Body)
	}
	if rec := get("/v1/sessions/%2571/shells/b0aau3e6s", http.MethodPost); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("a POST: %d %s", rec.Code, rec.Body)
	}
}
