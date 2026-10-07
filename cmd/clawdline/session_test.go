package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/devices"
	"github.com/sainteye/clawdline/internal/adapters/skillfile"
)

const (
	thinToken        = "fake-orchestrator-token-for-the-thin-command-tests"
	thinConversation = "a7200000-0000-4000-8000-000000000002"
)

// seenRequest is one request a stand-in daemon received.
type seenRequest struct {
	Method, EscapedPath, Query, Token, Key, ContentType string
	Body                                                []byte
}

// standIn is a daemon that answers each path as the test says and records
// what it was asked.
type standIn struct {
	mu   sync.Mutex
	seen []seenRequest
}

func (s *standIn) requests() []seenRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]seenRequest(nil), s.seen...)
}

func newStandIn(t *testing.T, answer func(r *http.Request) (int, string)) (*standIn, *broker) {
	t.Helper()
	s := &standIn{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.seen = append(s.seen, seenRequest{
			Method: r.Method, EscapedPath: r.URL.EscapedPath(), Query: r.URL.RawQuery,
			Token: r.Header.Get("X-Clawdline-Orchestrator"), Key: r.Header.Get("Idempotency-Key"),
			ContentType: r.Header.Get("Content-Type"), Body: body,
		})
		s.mu.Unlock()
		status, text := answer(r)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, text)
	}))
	t.Cleanup(srv.Close)
	return s, &broker{base: srv.URL, token: thinToken, client: &http.Client{Timeout: 5 * time.Second}}
}

func envOf(values map[string]string) func(string) string {
	return func(k string) string { return values[k] }
}

// The report is whoami, then complete on the terminal it answered — escaped
// as one path segment, which a tmux id needs — carrying the token in a header
// and the summary alone in the body.
func TestSessionReportAsksWhoamiThenCompletes(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) {
		if strings.HasSuffix(r.URL.Path, "/whoami") {
			return 200, `{"conversation_id":"` + thinConversation + `","terminal_id":"%47","assistant":"claude"}`
		}
		return 200, `{"ok":true,"created":true,"disposition":{"scope":"session","title":"Shipped it."}}`
	})
	var out, errs bytes.Buffer
	code := reportSession(&out, &errs, b, "Shipped it.", "", "",
		envOf(map[string]string{"CLAUDE_CODE_SESSION_ID": thinConversation}))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	// The third is the root's own list, asked for completions nobody
	// acknowledged (TestSessionReportSaysAChildFinishedUnacknowledged).
	if len(seen) != 3 || seen[2].Method != "GET" ||
		seen[2].EscapedPath != "/v1/work/v2/agent/session-todos/"+thinConversation {
		t.Fatalf("asked %d times: %+v", len(seen), seen)
	}
	if seen[0].Method != "GET" || seen[0].EscapedPath != "/v1/orchestrator/whoami" ||
		seen[0].Query != "conversation_id="+thinConversation {
		t.Fatalf("whoami = %+v", seen[0])
	}
	if seen[1].Method != "POST" || seen[1].EscapedPath != "/v1/orchestrator/sessions/%2547/complete" {
		t.Fatalf("complete = %+v", seen[1])
	}
	var body map[string]any
	if err := json.Unmarshal(seen[1].Body, &body); err != nil || len(body) != 1 || body["summary"] != "Shipped it." {
		t.Fatalf("body = %s", seen[1].Body)
	}
	for _, r := range seen {
		if r.Token != thinToken {
			t.Fatalf("the token was not sent as the orchestrator header: %+v", r)
		}
	}
	if seen[1].ContentType != "application/json" {
		t.Fatalf("content type = %q", seen[1].ContentType)
	}
	if strings.Contains(out.String()+errs.String(), thinToken) {
		t.Fatal("the token was printed")
	}
}

// After the receipt, the to-dos the person sent that are still open are said
// on stderr with the command that completes each; the exit status is still 0.
func TestSessionReportRemindsOfSentToDosStillOpen(t *testing.T) {
	_, b := newStandIn(t, func(r *http.Request) (int, string) {
		return 200, `{"ok":true,"created":true,"disposition":{"scope":"session","title":"Done."},` +
			`"open_todos":[{"id":"td-1","text":"Fix the login page","sent_at":1,"read_at":null},` +
			`{"id":"td-2","text":"Rename the button","sent_at":2,"read_at":3}],` +
			`"open_todos_truncated":false,"open_todos_unknown":false}`
	})
	var out, errs bytes.Buffer
	if code := reportSession(&out, &errs, b, "Done.", "", "%12", envOf(nil)); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	want := "2 to-dos were sent to this Session and are not checked off:\n" +
		"  td-1  Fix the login page\n" +
		"  td-2  Rename the button\n" +
		"Complete each finished one with: clawdline todo done <id>\n"
	if errs.String() != want {
		t.Fatalf("stderr:\n%s\nwant:\n%s", errs.String(), want)
	}
	if !strings.Contains(out.String(), `"open_todos"`) {
		t.Fatalf("the daemon's answer was not printed: %s", out.String())
	}
}

// None open says nothing more; unreadable says unknown, never "none".
func TestSessionReportSaysNothingOrUnknownAboutToDos(t *testing.T) {
	for _, c := range []struct{ name, body, want string }{
		{"none", `{"ok":true,"open_todos":[],"open_todos_truncated":false,"open_todos_unknown":false}`, ""},
		{"unknown", `{"ok":true,"open_todos":[],"open_todos_truncated":false,"open_todos_unknown":true}`,
			"The to-dos sent to this Session could not be read, so whether any are still open is unknown.\n"},
		{"one, truncated", `{"ok":true,"open_todos":[{"id":"td-1","text":"a"}],"open_todos_truncated":true}`,
			"At least 1 to-dos were sent to this Session and are not checked off:\n  td-1  a\n" +
				"Complete each finished one with: clawdline todo done <id>\n"},
		{"one", `{"ok":true,"open_todos":[{"id":"td-1","text":"a"}]}`,
			"1 to-do was sent to this Session and is not checked off:\n  td-1  a\n" +
				"Complete each finished one with: clawdline todo done <id>\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, c.body })
			var out, errs bytes.Buffer
			if code := reportSession(&out, &errs, b, "Done.", "", "%12", envOf(nil)); code != 0 {
				t.Fatalf("exit %d", code)
			}
			if errs.String() != c.want {
				t.Fatalf("stderr %q, want %q", errs.String(), c.want)
			}
		})
	}
}

// A child that finished while this root was busy — its line never typed, or
// typed and given up on — is said after the receipt, with the read that ends
// it; unreadable is said as unknown, and the exit is still 0.
func TestSessionReportSaysAChildFinishedUnacknowledged(t *testing.T) {
	for _, c := range []struct{ name, todos, want string }{
		{"one", `{"ok":true,"direct_todos":[],"unacknowledged_completions":[{"task_id":"c6f3-1",` +
			`"title":"the relay reconnects","state":"success","result_path":"/t/c6f3-1/result.json",` +
			`"notice_id":"n-1","notice_state":"dead_letter","ack_path":"/v1/orchestrator/tasks/c6f3-1/completion/ack"}]}`,
			"1 child task of this Session finished and is not acknowledged:\n" +
				"  c6f3-1  the relay reconnects (success; notice dead_letter)\n" +
				"    then: clawdline task show c6f3-1 (reading it closes the notice)\n"},
		{"none", `{"ok":true,"unacknowledged_completions":[]}`, ""},
		{"unknown", `{"ok":true,"unacknowledged_completions":[],"unacknowledged_completions_unknown":true}`,
			"Whether a child of this Session finished without being acknowledged could not be read.\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, b := newStandIn(t, func(r *http.Request) (int, string) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/whoami"):
					return 200, `{"terminal_id":"%47"}`
				case strings.Contains(r.URL.Path, "/session-todos/"):
					return 200, c.todos
				}
				return 200, `{"ok":true,"open_todos":[]}`
			})
			var out, errs bytes.Buffer
			code := reportSession(&out, &errs, b, "Done.", "", "",
				envOf(map[string]string{"CLAUDE_CODE_SESSION_ID": thinConversation}))
			if code != 0 || errs.String() != c.want {
				t.Fatalf("exit %d, stderr:\n%s\nwant:\n%s", code, errs.String(), c.want)
			}
		})
	}
}

// A refusal is said with its code, and exits 1.
func TestSessionReportSaysTheRefusal(t *testing.T) {
	_, b := newStandIn(t, func(r *http.Request) (int, string) {
		return 409, `{"error":{"code":"child_session","message":"A live Clawdline child reports through result.json."}}`
	})
	var out, errs bytes.Buffer
	code := reportSession(&out, &errs, b, "Done.", "", "%12", envOf(nil))
	if code != 1 || !strings.Contains(errs.String(), "409 child_session") {
		t.Fatalf("exit %d, stderr %q", code, errs.String())
	}
}

// With no conversation to name, nothing is asked at all.
func TestSessionReportWithoutAConversationAsksNothing(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, `{}` })
	var out, errs bytes.Buffer
	if code := reportSession(&out, &errs, b, "Done.", "", "", envOf(nil)); code != 2 {
		t.Fatalf("exit %d", code)
	}
	if code := reportSession(&out, &errs, b, "", "", "%1", envOf(nil)); code != 2 {
		t.Fatalf("an empty summary: exit %d", code)
	}
	if n := len(s.requests()); n != 0 {
		t.Fatalf("asked %d times", n)
	}
}

// A message goes with an Idempotency-Key, said before it is sent; and an
// answer that somehow carried the token is printed with it masked.
func TestSendCarriesAKeyAndNeverPrintsTheToken(t *testing.T) {
	s, b := newStandIn(t, func(r *http.Request) (int, string) {
		return 200, `{"ok":true,"echo":"` + thinToken + `"}`
	})
	var out, errs bytes.Buffer
	code := relayMessage(&out, &errs, b, "%3", "", "hello", "",
		envOf(map[string]string{"CODEX_THREAD_ID": thinConversation}))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	seen := s.requests()
	if len(seen) != 1 || seen[0].Key == "" || !strings.Contains(errs.String(), seen[0].Key) {
		t.Fatalf("key not sent or not said: %+v %q", seen, errs.String())
	}
	var body map[string]string
	_ = json.Unmarshal(seen[0].Body, &body)
	if body["from_session"] != thinConversation || body["to_session"] != "%3" || body["text"] != "hello" {
		t.Fatalf("body = %s", seen[0].Body)
	}
	if strings.Contains(out.String()+errs.String(), thinToken) || !strings.Contains(out.String(), "<orchestrator-token>") {
		t.Fatalf("the token was not masked: %s", out.String())
	}
	// A retry with the printed key sends the same key.
	if code := relayMessage(&out, &errs, b, "%3", "x", "hello", "send-fixed", envOf(nil)); code != 0 {
		t.Fatal(code)
	}
	if got := s.requests()[1].Key; got != "send-fixed" {
		t.Fatalf("retry key = %q", got)
	}
}

// The token is read from this app's directory as a plain file, and never
// from the Swift app's, however that is spelled.
func TestTheTokenIsReadOnlyFromThisAppsDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	swift := filepath.Join(home, ".config", "clawdline")
	if err := os.MkdirAll(swift, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(swift, devices.MachineTokenFile), []byte(thinToken), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := machineToken(swift); !errors.Is(err, skillfile.ErrForeignDir) {
		t.Fatalf("the Swift app's directory = %v", err)
	}
	// Through a link to it, too.
	link := filepath.Join(home, "elsewhere")
	if err := os.Symlink(swift, link); err == nil {
		if _, err := machineToken(link); !errors.Is(err, skillfile.ErrForeignDir) {
			t.Fatalf("a link to the Swift app's directory = %v", err)
		}
	}

	own := filepath.Join(home, ".config", "clawdline-next")
	if _, err := machineToken(own); err == nil || strings.Contains(err.Error(), thinToken) {
		t.Fatalf("a missing token = %v", err)
	}
	if err := os.MkdirAll(own, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(own, devices.MachineTokenFile), []byte(thinToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := machineToken(own)
	if err != nil || got != thinToken {
		t.Fatalf("token = %q, %v", got, err)
	}
}

// The guide prints without a daemon, in either language, and lists itself.
func TestGuidePrints(t *testing.T) {
	var out, errs bytes.Buffer
	if code := printGuide(&out, &errs, nil); code != 0 || !strings.Contains(out.String(), "\n# Clawdline guide") ||
		!strings.Contains(out.String(), "`clawdline guide dispatch`") {
		t.Fatalf("exit %d: %.40q", code, out.String())
	}
	core := out.Len()
	out.Reset()
	if code := printGuide(&out, &errs, []string{"all"}); code != 0 || out.Len() <= core*4 {
		t.Fatalf("all: exit %d, %d bytes against a core of %d", code, out.Len(), core)
	}
	out.Reset()
	if code := printGuide(&out, &errs, []string{"zh-TW", "board"}); code != 0 || !strings.Contains(out.String(), "\n## 10.") {
		t.Fatalf("zh-TW board: exit %d: %.20q", code, out.String())
	}
	out.Reset()
	if code := printGuide(&out, &errs, []string{"zh-TW", "project"}); code != 0 ||
		!strings.Contains(out.String(), "Project 在 Clawdline") || !strings.Contains(out.String(), ".devstack.json") {
		t.Fatalf("zh-TW project: exit %d: %.80q", code, out.String())
	}
	out.Reset()
	if code := printGuide(&out, &errs, []string{"-sections"}); code != 0 || !strings.Contains(out.String(), "dispatch\n") {
		t.Fatalf("sections = %q", out.String())
	}
	out.Reset()
	if code := printGuide(&out, &errs, []string{"-list"}); code != 0 || out.String() != "en\nzh-TW\n" {
		t.Fatalf("list = %q", out.String())
	}
	out.Reset()
	if code := printGuide(&out, &errs, []string{"zh-TW"}); code != 0 || out.Len() == 0 {
		t.Fatalf("zh-TW: exit %d", code)
	}
	if code := printGuide(&out, &errs, []string{"fr"}); code != 1 {
		t.Fatalf("unknown topic: exit %d", code)
	}
	out.Reset()
	if code := printGuide(&out, &errs, []string{"child"}); code != 0 ||
		!strings.HasPrefix(out.String(), "# How a Clawdline child works") || !strings.Contains(out.String(), "result.json") {
		t.Fatalf("child: exit %d: %.60q", code, out.String())
	}
}

// Every guide text opens with its hash, the core as well as a part, and
// --since that hash answers one line; a refusal code prints the part that
// owns it, and an unknown one prints nothing on stdout.
func TestGuideVersionAndRefusalLookup(t *testing.T) {
	var out, errs bytes.Buffer
	if code := printGuide(&out, &errs, []string{"refused", "steps_incomplete"}); code != 0 || !strings.Contains(out.String(), "\n## 2a.") {
		t.Fatalf("refusal lookup: %d %q %q", code, out.String(), errs.String())
	}
	for _, args := range [][]string{nil, {"zh-TW"}, {"zh-TW", "board"}, {"all"}, {"refused", "steps_incomplete"}} {
		out.Reset()
		if code := printGuide(&out, &errs, args); code != 0 {
			t.Fatal(args, code, errs.String())
		}
		first, rest, _ := strings.Cut(out.String(), "\n")
		hash, ok := strings.CutPrefix(first, "guide-version: ")
		if !ok || len(hash) != 64 || strings.HasPrefix(rest, "guide-version:") {
			t.Fatalf("%v: first line %q", args, first)
		}
		out.Reset()
		if code := printGuide(&out, &errs, append(append([]string{}, args...), "--since", hash)); code != 0 || out.String() != "unchanged "+hash+"\n" {
			t.Fatalf("%v --since: %d %q %q", args, code, out.String(), errs.String())
		}
		out.Reset()
		other := strings.Repeat("0", 64)
		if code := printGuide(&out, &errs, append(append([]string{}, args...), "--since", other)); code != 0 || !strings.HasPrefix(out.String(), first+"\n") || out.String()[len(first)+1:] != rest {
			t.Fatalf("%v --since another hash: %d %.80q", args, code, out.String())
		}
	}
	for _, bad := range []string{"abc", strings.Repeat("G", 64)} {
		out.Reset()
		errs.Reset()
		if code := printGuide(&out, &errs, []string{"board", "--since", bad}); code != 2 || out.Len() != 0 || !strings.HasPrefix(errs.String(), "usage:") {
			t.Fatalf("malformed --since %q: %d %q %q", bad, code, out.String(), errs.String())
		}
	}
	out.Reset()
	errs.Reset()
	if code := printGuide(&out, &errs, []string{"refused", "not_a_real_refusal"}); code != 1 || out.Len() != 0 ||
		errs.String() != "clawdline guide: no guide part explains refusal code \"not_a_real_refusal\"\n" {
		t.Fatalf("unknown refusal: %d %q %q", code, out.String(), errs.String())
	}
}

// Install, install again, uninstall: each says what it did.
func TestSkillInstallSaysEachStep(t *testing.T) {
	root := t.TempDir()
	paths := skillfile.Paths{Home: filepath.Join(root, "home"), StateDir: filepath.Join(root, "state")}
	var out, errs bytes.Buffer
	now := time.Date(2026, 9, 19, 6, 0, 0, 0, time.UTC)
	if code := installSkill(&out, &errs, paths, now); code != 0 ||
		!strings.Contains(out.String(), "Installed this build's stub at "+skillfile.StubPath(paths.Home)) ||
		!strings.Contains(out.String(), "Installed this build's stub at "+skillfile.AgentsStubPath(paths.Home)) {
		t.Fatalf("install: exit %d %q %q", code, out.String(), errs.String())
	}
	out.Reset()
	if code := installSkill(&out, &errs, paths, now); code != 0 || !strings.Contains(out.String(), "Already installed") {
		t.Fatalf("again: exit %d %q", code, out.String())
	}
	out.Reset()
	if code := uninstallSkill(&out, &errs, paths); code != 0 || !strings.Contains(out.String(), "Put back") {
		t.Fatalf("uninstall: exit %d %q", code, out.String())
	}
	for _, stub := range []string{skillfile.StubPath(paths.Home), skillfile.AgentsStubPath(paths.Home)} {
		if _, err := os.Lstat(stub); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("the stub is still there: %s %v", stub, err)
		}
	}
}

// bothAssistants is a Codex process whose environment also carries a Claude
// Code conversation it inherited — a contaminated tmux global environment —
// so the two variables name different conversations.
var bothAssistants = map[string]string{
	"CLAUDE_CODE_SESSION_ID": "c1a0de00-0000-4000-8000-000000000001",
	"CODEX_THREAD_ID":        thinConversation,
}

// With both assistants' variables set, no command takes the first one it
// asks: each refuses before asking the daemon anything, names both variables
// and says which flag settles it.
func TestEveryCommandRefusesWhenTwoAssistantsNameTheConversation(t *testing.T) {
	env := envOf(bothAssistants)
	cases := []struct {
		name, flag string
		run        func(stdout, stderr io.Writer, b *broker) int
	}{
		{"session report", "--conversation", func(o, e io.Writer, b *broker) int {
			return reportSession(o, e, b, "Shipped it.", "", "", env)
		}},
		{"item name", "--conversation", func(o, e io.Writer, b *broker) int {
			return sessionItem(o, e, b, "name", itemFlags{}, []string{"item-1", "A name"}, "", "", env)
		}},
		{"todo list", "--conversation", func(o, e io.Writer, b *broker) int {
			return sessionTodo(o, e, b, "list", nil, "", "", env)
		}},
		{"send", "--from", func(o, e io.Writer, b *broker) int {
			return relayMessage(o, e, b, "%3", "", "hello", "", env)
		}},
		{"usage", "--session", func(o, e io.Writer, b *broker) int {
			return showUsage(o, e, b, usageAsk{}, env)
		}},
		{"note create", "--from", func(o, e io.Writer, b *broker) int {
			return createNote(o, e, b, "target-terminal", "", []byte(`{"kind":"answer","title":"Choose"}`), "k", env)
		}},
		{"coordinator bind", "--conversation", func(o, e io.Writer, b *broker) int {
			return bindCoordinatorWithWait(o, e, b, "", env, func() {})
		}},
		{"dispatch", "--conversation", func(o, e io.Writer, b *broker) int {
			return dispatchTask(o, e, b, testDispatchOptions(), testDispatchEnv(bothAssistants))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, `{"ok":true}` })
			var out, errs bytes.Buffer
			if code := tc.run(&out, &errs, b); code != 2 {
				t.Fatalf("exit %d, want 2: %s", code, errs.String())
			}
			if seen := s.requests(); len(seen) != 0 {
				t.Fatalf("asked the daemon %d times: %+v", len(seen), seen)
			}
			msg := errs.String()
			for _, want := range []string{"CLAUDE_CODE_SESSION_ID", "CODEX_THREAD_ID", tc.flag} {
				if !strings.Contains(msg, want) {
					t.Fatalf("the refusal does not say %q: %s", want, msg)
				}
			}
		})
	}
}

// An explicit conversation settles it, and one assistant's variable alone is
// still taken as before — with the assistant it belongs to.
func TestConversationFromEnvTakesOneAssistantOrTheFlag(t *testing.T) {
	if id, name, err := conversationFromEnv(envOf(map[string]string{"CODEX_THREAD_ID": "x"})); err != nil || id != "x" || name != "CODEX_THREAD_ID" {
		t.Fatalf("codex alone = %q %q %v", id, name, err)
	}
	if id, name, err := conversationFromEnv(envOf(map[string]string{"CLAUDE_CODE_SESSION_ID": "y"})); err != nil || id != "y" || name != "CLAUDE_CODE_SESSION_ID" {
		t.Fatalf("claude alone = %q %q %v", id, name, err)
	}
	if id, _, err := conversationFromEnv(envOf(map[string]string{"CODEX_THREAD_ID": "x", "CODEX_SESSION_ID": "z"})); err != nil || id != "x" {
		t.Fatalf("two codex variables = %q %v", id, err)
	}
	if id, _, err := conversationFromEnv(envOf(map[string]string{"CLAUDE_CODE_SESSION_ID": "x", "CODEX_THREAD_ID": "x"})); err != nil || id != "x" {
		t.Fatalf("both naming the same conversation = %q %v", id, err)
	}
	if _, _, err := conversationFromEnv(envOf(bothAssistants)); err == nil {
		t.Fatal("two assistants were not refused")
	}
	if got := conversationOf(envOf(bothAssistants)); got != "" {
		t.Fatalf("a note was signed with %q", got)
	}
	s, b := newStandIn(t, func(r *http.Request) (int, string) { return 200, `{"ok":true,"todos":[]}` })
	var out, errs bytes.Buffer
	if code := sessionTodo(&out, &errs, b, "list", nil, thinConversation, "", envOf(bothAssistants)); code != 0 {
		t.Fatalf("--conversation did not settle it: exit %d %s", code, errs.String())
	}
	if seen := s.requests(); len(seen) != 1 || !strings.Contains(seen[0].EscapedPath, thinConversation) {
		t.Fatalf("requests = %+v", seen)
	}
}

// A Session whose only blocker is its own turn: the dry run says a real run
// would schedule, and the real run posts the close and says it waits for the
// turn to end. Another blocker beside the turn still closes nothing.
func TestSessionCloseDuringItsOwnTurnIsScheduled(t *testing.T) {
	audit := func(extra string) string {
		return `{"terminal_id":"%84","state":"blocked","version":"v1","authority":"self","reasons":[` +
			`{"code":"terminal_working","kind":"obligation","subject_kind":"session","subject_id":"%84","mover":{"kind":"session","self":true}}` +
			extra + `]}`
	}
	answer := func(a string) func(r *http.Request) (int, string) {
		return func(r *http.Request) (int, string) {
			if r.Method == http.MethodPost {
				return http.StatusAccepted, `{"ok":true,"id":"%84","action":"close_scheduled"}`
			}
			return http.StatusOK, a
		}
	}
	env := envOf(map[string]string{"CLAUDE_CODE_SESSION_ID": thinConversation})

	s, b := newStandIn(t, answer(audit("")))
	var out, errs bytes.Buffer
	if code := closeSession(&out, &errs, b, "", "%84", true, env); code != 0 ||
		!strings.Contains(out.String(), "would schedule") {
		t.Fatalf("dry run: %d %q %q", code, out.String(), errs.String())
	}
	for _, r := range s.requests() {
		if r.Method == http.MethodPost {
			t.Fatal("a dry run posted the close")
		}
	}

	_, b = newStandIn(t, answer(audit("")))
	out.Reset()
	errs.Reset()
	if code := closeSession(&out, &errs, b, "", "%84", false, env); code != 0 ||
		!strings.Contains(out.String(), "close scheduled") || !strings.Contains(out.String(), "restart") {
		t.Fatalf("real run: %d %q %q", code, out.String(), errs.String())
	}

	s, b = newStandIn(t, answer(audit(`,{"code":"board_item_open","kind":"obligation","subject_id":"w1","mover":{"kind":"session","self":true}}`)))
	out.Reset()
	errs.Reset()
	if code := closeSession(&out, &errs, b, "", "%84", false, env); code != 1 {
		t.Fatalf("another blocker: %d %q %q", code, out.String(), errs.String())
	}
	for _, r := range s.requests() {
		if r.Method == http.MethodPost {
			t.Fatal("another blocker posted the close")
		}
	}
}
