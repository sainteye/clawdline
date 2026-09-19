package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/taskdir"
	"github.com/sainteye/clawdline-go/internal/app"
	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
	"github.com/sainteye/clawdline-go/internal/app/ports"
	"github.com/sainteye/clawdline-go/internal/domain/auth"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// A session's writes — typing a message, answering its menu, closing it — are
// the ones a caller is least sure about: across Clawdline Cloud the answer can
// be lost after the Mac acted, and "try again" is then the second message. So
// a write that names an Idempotency-Key is answered once per key, as starting
// a session already was (start.go): the same key gets the first answer back
// and nothing is typed twice.

// pane is one tmux pane this daemon can type at, with a picker on it.
type pane struct {
	mu      sync.Mutex
	s       session.Session
	screen  string
	acts    []string
	failKey bool
	onKey   func(p *pane, b []byte)
}

func (p *pane) Name() string { return "tmux" }
func (p *pane) Inventory(ctx context.Context) (session.Inventory, error) {
	return session.Inventory{Complete: true, Sessions: []session.Session{p.s}}, nil
}
func (p *pane) Open(ctx context.Context, req ports.OpenRequest) (session.Session, error) {
	return session.Session{}, errors.New("no")
}
func (p *pane) Send(ctx context.Context, s session.Session, text string) error {
	p.act("send:" + text)
	return nil
}
func (p *pane) Interrupt(ctx context.Context, s session.Session) error { return nil }
func (p *pane) Close(ctx context.Context, s session.Session) error {
	p.act("close")
	return nil
}
func (p *pane) Type(ctx context.Context, s session.Session, text string) error { return nil }
func (p *pane) Reveal(ctx context.Context, s session.Session, activate bool) error {
	return nil
}
func (p *pane) Screen(ctx context.Context, s session.Session, lines int) (string, bool) {
	return p.Capture(ctx, s)
}
func (p *pane) Capture(ctx context.Context, s session.Session) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.screen, p.screen != ""
}
func (p *pane) Keystroke(ctx context.Context, s session.Session, b []byte) error {
	if p.failKey {
		return errors.New("the pane went away mid-answer")
	}
	p.act("key:" + string(b))
	p.mu.Lock()
	hook := p.onKey
	p.mu.Unlock()
	if hook != nil {
		hook(p, b)
	}
	return nil
}
func (p *pane) act(a string) {
	p.mu.Lock()
	p.acts = append(p.acts, a)
	p.mu.Unlock()
}
func (p *pane) show(screen string) {
	p.mu.Lock()
	p.screen = screen
	p.mu.Unlock()
}
func (p *pane) done() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.acts...)
}

// prompt is a Claude Code permission dialog for one command.
func prompt(command string, caret int) string {
	row := func(n int, label string) string {
		mark := "  "
		if n == caret {
			mark = "❯ "
		}
		return fmt.Sprintf("│ %s%d. %-40s│\n", mark, n, label)
	}
	return "✻ Working…\n\n" +
		"╭──────────────────────────────────────────────╮\n" +
		"│ Bash command                                 │\n" +
		"│                                              │\n" +
		"│   " + fmt.Sprintf("%-43s", command) + "│\n" +
		"│                                              │\n" +
		"│ Do you want to proceed?                      │\n" +
		row(1, "Yes") +
		row(2, "Yes, and don't ask again for rm") +
		row(3, "No, and tell Claude what to do (esc)") +
		"╰──────────────────────────────────────────────╯\n"
}

func fingerprintOf(t *testing.T, screen string) string {
	t.Helper()
	m, ok := session.ReadMenu(screen, session.AssistantClaude, true)
	if !ok {
		t.Fatalf("not a menu:\n%s", screen)
	}
	return session.MenuFingerprint(m)
}

// paneServer is a daemon with one pane on it and a real store for receipts.
func paneServer(t *testing.T, p *pane) *Server {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "clawdline-next")
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hosts := []ports.TerminalHost{p}
	return &Server{store: st, terminals: hosts,
		inventory: app.Inventory{Terminals: hosts, Screen: p},
		broker:    &orchestrator.Broker{Store: st, Tasks: taskdir.New(dir), Dir: dir}}
}

// act posts one session action as a device that may send.
func act(t *testing.T, s *Server, verb, id, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/sessions/"+strings.ReplaceAll(id, "%", "%25")+"/"+verb,
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{
		verdict: auth.Verdict{Allowed: true, Device: "phone", Caps: auth.NewCaps(auth.Read, auth.Send)},
	}))
	rec := httptest.NewRecorder()
	s.sessionAction(rec, req)
	return rec
}

func codeOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error any `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	switch e := body.Error.(type) {
	case string:
		return e
	case map[string]any:
		code, _ := e["code"].(string)
		return code
	}
	return ""
}

func waitingPane(id, screen string) *pane {
	return &pane{s: session.Session{ID: id, Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		State: session.StateWaiting}, screen: screen}
}

// F2: a message whose answer was lost is sent again under the same key. The
// second request is answered with the first one's answer, and the words are
// typed once.
func TestASendRetriedUnderItsKeyIsTypedOnce(t *testing.T) {
	p := waitingPane("%4", "")
	s := paneServer(t, p)
	first := act(t, s, "send", "%4", "card-1", `{"text":"delete the build directory"}`)
	again := act(t, s, "send", "%4", "card-1", `{"text":"delete the build directory"}`)
	if first.Code != 200 || again.Code != 200 {
		t.Fatalf("answers %d %s / %d %s", first.Code, first.Body, again.Code, again.Body)
	}
	if got := p.done(); len(got) != 1 {
		t.Fatalf("typed %q; the retry typed the words a second time", got)
	}
	if again.Header().Get("Idempotent-Replayed") != "true" || again.Body.String() != first.Body.String() {
		t.Fatalf("the retry was not answered with the first answer: %q vs %q", again.Body, first.Body)
	}
	// Another key is another message.
	if other := act(t, s, "send", "%4", "card-2", `{"text":"delete the build directory"}`); other.Code != 200 {
		t.Fatalf("a new key: %d %s", other.Code, other.Body)
	}
	if got := p.done(); len(got) != 2 {
		t.Fatalf("typed %q after a second card", got)
	}
}

// The key names one request. The same key with other words is not a retry,
// and is refused rather than answered with the first message's answer.
func TestAKeyReusedForOtherWordsIsRefused(t *testing.T) {
	p := waitingPane("%4", "")
	s := paneServer(t, p)
	act(t, s, "send", "%4", "card-1", `{"text":"yes"}`)
	rec := act(t, s, "send", "%4", "card-1", `{"text":"no"}`)
	if rec.Code != http.StatusConflict || codeOf(t, rec) != "idempotency_key_reused" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if got := p.done(); len(got) != 1 {
		t.Fatalf("typed %q", got)
	}
}

// A refusal decided before anything was typed is not filed: it is about that
// moment, and the retry under the same key is carried out. Filing it would
// make one busy terminal refuse the card for a day.
func TestARefusalBeforeTypingIsNotFiled(t *testing.T) {
	p := waitingPane("%4", "")
	s := paneServer(t, p)
	if rec := act(t, s, "send", "%9", "card-1", `{"text":"hello"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("a missing session: %d %s", rec.Code, rec.Body)
	}
	// The same card, the session found this time (the id was the only thing
	// that differed, so the digest does too: a new request under that key).
	p2 := waitingPane("%9", "")
	s2 := paneServer(t, p2)
	s2.store = s.store
	if rec := act(t, s2, "send", "%9", "card-1", `{"text":"hello"}`); rec.Code != 200 {
		t.Fatalf("the retry after a refusal was not carried out: %d %s", rec.Code, rec.Body)
	}
	if got := p2.done(); len(got) != 1 {
		t.Fatalf("typed %q", got)
	}
}

// A terminal that failed part-way may have taken some of the bytes. That
// answer is filed: a retry is told what happened the first time, and is not
// a second go at a half-typed message.
func TestAFailureThatMayHaveTypedIsFiled(t *testing.T) {
	p := waitingPane("%4", prompt("rm -rf build", 1))
	p.failKey = true
	s := paneServer(t, p)
	first := act(t, s, "key", "%4", "press-1", `{"key":"1"}`)
	if first.Code != http.StatusBadGateway || codeOf(t, first) != "terminal_io_failed" {
		t.Fatalf("%d %s", first.Code, first.Body)
	}
	p.failKey = false
	again := act(t, s, "key", "%4", "press-1", `{"key":"1"}`)
	if again.Header().Get("Idempotent-Replayed") != "true" || codeOf(t, again) != "terminal_io_failed" {
		t.Fatalf("the retry was carried out again: %d %s", again.Code, again.Body)
	}
	if got := p.done(); len(got) != 0 {
		t.Fatalf("typed %q", got)
	}
}

// A menu answer retried under its key is pressed once: the second press would
// be a stray digit in whatever question came next.
func TestAnAnswerRetriedUnderItsKeyIsPressedOnce(t *testing.T) {
	p := waitingPane("%4", prompt("rm -rf build", 1))
	p.onKey = func(p *pane, b []byte) {
		if string(b) == "\r" {
			p.show(prompt("rm -rf /", 1))
		}
	}
	s := paneServer(t, p)
	expect := fingerprintOf(t, prompt("rm -rf build", 1))
	body := `{"key":"1","expect":"` + expect + `"}`
	if rec := act(t, s, "key", "%4", "press-1", body); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	again := act(t, s, "key", "%4", "press-1", body)
	if again.Code != 200 || again.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("%d %s", again.Code, again.Body)
	}
	if got := p.done(); len(got) != 2 || got[0] != "key:1" || got[1] != "key:\r" {
		t.Fatalf("pressed %q", got)
	}
}

// F1 over this route: the answer names the question it was chosen for, and a
// screen now asking about another command gets nothing typed at it.
func TestTheKeyRouteRefusesAnAnswerForAnotherQuestion(t *testing.T) {
	p := waitingPane("%4", prompt("rm -rf /", 1))
	s := paneServer(t, p)
	body := `{"key":"1","expect":"` + fingerprintOf(t, prompt("rm -rf build", 1)) + `"}`
	rec := act(t, s, "key", "%4", "press-1", body)
	if rec.Code != http.StatusConflict || codeOf(t, rec) != "menu_moved" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if got := p.done(); len(got) != 0 {
		t.Fatalf("typed %q at another question", got)
	}
	// Refused before typing, so not filed: a press of the same key once the
	// question is back is carried out.
	p.show(prompt("rm -rf build", 1))
	if rec := act(t, s, "key", "%4", "press-1", body); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if bad := act(t, s, "key", "%4", "", `{"key":"1","expect":"zz"}`); bad.Code != http.StatusBadRequest {
		t.Fatalf("a malformed expectation: %d %s", bad.Code, bad.Body)
	}
}

// A close retried under its key closes once.
func TestACloseRetriedUnderItsKeyClosesOnce(t *testing.T) {
	p := waitingPane("%4", "")
	s := paneServer(t, p)
	first := act(t, s, "close", "%4", "end-1", `{"force":false}`)
	again := act(t, s, "close", "%4", "end-1", `{"force":false}`)
	if first.Code != 200 || again.Code != 200 || again.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("%d %s / %d %s", first.Code, first.Body, again.Code, again.Body)
	}
	if got := p.done(); len(got) != 1 || got[0] != "close" {
		t.Fatalf("acts %q", got)
	}
}

// Without a key, a write is what it always was: carried out every time it is
// asked. Old pages send none.
func TestAWriteWithoutAKeyIsCarriedOutEachTime(t *testing.T) {
	p := waitingPane("%4", "")
	s := paneServer(t, p)
	act(t, s, "send", "%4", "", `{"text":"again"}`)
	act(t, s, "send", "%4", "", `{"text":"again"}`)
	if got := p.done(); len(got) != 2 {
		t.Fatalf("typed %q", got)
	}
}
