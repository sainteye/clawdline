package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// bornPane is a tmux pane whose login shell becomes Claude Code after a
// number of readings of the machine: what a just-launched Session is.
type bornPane struct {
	touchPane
	mu       sync.Mutex
	reads    int
	becomeAt int // 0: never
}

func (p *bornPane) Inventory(ctx context.Context) (session.Inventory, error) {
	p.mu.Lock()
	p.reads++
	born := p.becomeAt > 0 && p.reads >= p.becomeAt
	p.mu.Unlock()
	s := session.Session{ID: "%0", Backend: session.BackendTmux, State: session.StateIdle}
	if born {
		s.Assistant = session.AssistantClaude
		p.show("❯ ")
	}
	return session.Inventory{Complete: true, Sessions: []session.Session{s}}, nil
}

func (p *bornPane) readings() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.reads
}

func newBornPane(becomeAt int) *bornPane {
	return &bornPane{touchPane: touchPane{&pane{s: session.Session{ID: "%0", Backend: session.BackendTmux,
		State: session.StateIdle}, screen: "~ % "}}, becomeAt: becomeAt}
}

func postSend(t *testing.T, handler http.Handler, local string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/sessions/%250/send", strings.NewReader(`{"text":"hello"}`))
	req.Host = "127.0.0.1:7757"
	req.Header.Set("Authorization", "Bearer "+local)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// A message sent to a Session this daemon just started, while its login
// shell is still starting the assistant, waits for the assistant and is typed
// into it once — not refused as an ordinary shell.
func TestAMessageToAJustStartedSessionWaitsForItsAssistant(t *testing.T) {
	p := newBornPane(4)
	s, handler, local, _ := wholeServer(t, p, p)
	s.launchHolds().opened("%0")
	rec := postSend(t, handler, local)
	if rec.Code != http.StatusOK {
		t.Fatalf("a send to a starting Session answered %d: %s", rec.Code, rec.Body)
	}
	if got := p.done(); len(got) != 1 || got[0] != "send:hello" {
		t.Fatalf("typed %q", got)
	}
	if p.readings() < 4 {
		t.Fatalf("the send was typed after %d readings, before the assistant was there", p.readings())
	}
}

// A shell this daemon did not just launch is refused at once, as before.
func TestAShellNobodyJustLaunchedIsStillRefusedAtOnce(t *testing.T) {
	p := newBornPane(4)
	_, handler, local, _ := wholeServer(t, p, p)
	start := time.Now()
	rec := postSend(t, handler, local)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "not_an_agent_session") {
		t.Fatalf("a send to bash answered %d %s", rec.Code, rec.Body)
	}
	if waited := time.Since(start); waited > launchHoldPoll {
		t.Fatalf("a shell nobody launched was waited on for %v", waited)
	}
	if typed := p.done(); len(typed) != 0 {
		t.Fatalf("the shell was typed into: %q", typed)
	}
}

// A launched pane that is still a shell when its window closes is refused
// then, and nothing reaches the shell.
func TestALaunchThatNeverBecomesAnAssistantIsRefusedWhenItsWindowCloses(t *testing.T) {
	p := newBornPane(0)
	s, handler, local, _ := wholeServer(t, p, p)
	holds := s.launchHolds()
	holds.opened("%0")
	holds.mu.Lock()
	holds.until["%0"] = time.Now().Add(400 * time.Millisecond)
	holds.mu.Unlock()
	rec := postSend(t, handler, local)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "not_an_agent_session") {
		t.Fatalf("a send to a pane that stayed a shell answered %d %s", rec.Code, rec.Body)
	}
	if p.readings() < 2 {
		t.Fatalf("the pane was read %d times inside its window", p.readings())
	}
	if typed := p.done(); len(typed) != 0 {
		t.Fatalf("the shell was typed into: %q", typed)
	}
}

// The record forgets the launch remembered longest ago past its bound.
func TestLaunchHoldsForgetTheOldestPastTheirBound(t *testing.T) {
	clock := time.Unix(1_800_000_000, 0)
	h := &launchHolds{until: map[string]time.Time{}, now: func() time.Time { return clock }}
	for i := 0; i <= MaxLaunchHoldRows; i++ {
		h.opened("%" + string(rune('A'+i%26)) + strings.Repeat("x", i/26))
		clock = clock.Add(time.Millisecond)
	}
	if len(h.until) != MaxLaunchHoldRows {
		t.Fatalf("remembered %d launches", len(h.until))
	}
	if h.remaining("%A") != 0 {
		t.Fatal("the oldest launch is still remembered")
	}
	clock = clock.Add(LaunchHoldSecondsLimit * time.Second)
	if h.remaining("%B") != 0 {
		t.Fatal("a launch is remembered after its window")
	}
}
