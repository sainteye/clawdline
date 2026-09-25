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
	if len(seen) != 2 {
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
	if code := printGuide(&out, &errs, nil); code != 0 || !strings.HasPrefix(out.String(), "# Clawdline guide") {
		t.Fatalf("exit %d: %.40q", code, out.String())
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
}

// Install, install again, uninstall: each says what it did.
func TestSkillInstallSaysEachStep(t *testing.T) {
	root := t.TempDir()
	paths := skillfile.Paths{Home: filepath.Join(root, "home"), StateDir: filepath.Join(root, "state")}
	var out, errs bytes.Buffer
	now := time.Date(2026, 9, 19, 6, 0, 0, 0, time.UTC)
	if code := installSkill(&out, &errs, paths, now); code != 0 || !strings.Contains(out.String(), "Installed") {
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
	if _, err := os.Lstat(skillfile.StubPath(paths.Home)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the stub is still there: %v", err)
	}
}
