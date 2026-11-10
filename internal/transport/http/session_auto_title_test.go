//go:build darwin || linux

package http

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/antigravitysession"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/icon"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func automaticGeminiFixture(t *testing.T) (*Server, session.Session) {
	t.Helper()
	t.Setenv("CLW_HTTP_PEER", "yes")
	s := capacityServer(t)
	s.icons = &icon.Registry{}
	s.broker = newBroker(s)
	s.terminals = []ports.TerminalHost{ownedAntigravityTerminal{s}}
	s.inventory = app.Inventory{Terminals: s.terminals}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "antigravity-acp"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "antigravity-acp", "settings.json"), []byte(`{"useG1Credits":false,"auth":{"type":"oauth-personal"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"executable": exe, "accountDir": dir, "args": []string{"-test.run=^TestOwnedHTTPPeer$"}})
	if err := os.WriteFile(filepath.Join(s.cfg.Dir, "antigravity-acp-pilot.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	stop, err := s.StartAntigravitySessions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	row, err := s.antigravitySessions.Load().Open(t.Context(), ports.OpenRequest{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	return s, row
}

func TestGeminiAutomaticNamingRunsAfterItsTurnAndPersists(t *testing.T) {
	s, row := automaticGeminiFixture(t)
	if _, err := nextconfig.Open(s.cfg.Dir).Set(map[string]any{"codex_auto_name": true, "auto_name_assistant": "claude"}); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	s.nameSession = func(ctx context.Context, text, assistant string) (string, error) {
		calls.Add(1)
		// Naming may run when the prompt is accepted, before the reply exists,
		// or after the turn; either sees the first request. Requiring the
		// reply failed every run on one CPU and twice in 100 on two.
		if assistant != "claude" || !strings.Contains(text, "Build a calendar") {
			return "", context.Canceled
		}
		return "Calendar reminders", nil
	}
	h := s.antigravitySessions.Load()
	if err := h.Send(t.Context(), row, "Build a calendar"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		inv, _ := h.Inventory(t.Context())
		if len(inv.Sessions) == 1 && inv.Sessions[0].Rungs.Thread == "Calendar reminders" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	inv, _ := h.Inventory(t.Context())
	if calls.Load() != 1 || len(inv.Sessions) != 1 || inv.Sessions[0].Rungs.Thread != "Calendar reminders" {
		t.Fatalf("automatic naming did not run: calls=%d", calls.Load())
	}
	// The title can land before the turn has settled; a send in that moment
	// is refused antigravity_session_busy (CI run 42336338871).
	sendAutomaticFixture(t, s, row, "Add reminders")
	if err := h.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("named conversation spent another turn")
	}
	s.antigravitySessions.Store(nil)
	stop, err := s.StartAntigravitySessions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	// "Add reminders" was still running at Shutdown, so the restart sends its
	// continuation; it holds the host's opening slot that Past also needs.
	waitGeminiSettled(t, s, row.ID)
	inv, _ = s.antigravitySessions.Load().Inventory(t.Context())
	if len(inv.Sessions) != 1 || inv.Sessions[0].Rungs.Thread != "Calendar reminders" {
		t.Fatal("title did not survive restart")
	}
	past, _, err := s.antigravitySessions.Load().Past(t.Context(), row.CWD, "", 10)
	if err != nil || len(past) != 1 || past[0].Title != "Calendar reminders" {
		t.Fatal("history picker lost automatic title", err)
	}
}

func sendAutomaticFixture(t *testing.T, s *Server, row session.Session, text string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		h := s.antigravitySessions.Load()
		err := h.Send(t.Context(), row, text)
		if err == nil {
			// A message sent during a turn is queued; wait until it is written.
			for rows, _ := h.QueueUse(); rows > 0; rows, _ = h.QueueUse() {
				if time.Now().After(deadline) {
					t.Fatal("queued message was not sent")
				}
				time.Sleep(time.Millisecond)
			}
			return
		}
		if err.Error() != "antigravity_session_busy" || time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestGeminiAutomaticNamingRespectsOffAndManualTitles(t *testing.T) {
	for _, manual := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "manual"}[manual], func(t *testing.T) {
			s, row := automaticGeminiFixture(t)
			var calls atomic.Int32
			s.nameSession = func(context.Context, string, string) (string, error) { calls.Add(1); return "Unexpected", nil }
			if manual {
				if _, err := nextconfig.Open(s.cfg.Dir).Set(map[string]any{"codex_auto_name": true}); err != nil {
					t.Fatal(err)
				}
				if _, err := s.saveSessionTitle(row, "Human name", time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			s.firstSessionRequest = func(session.Session) (string, error) { return "First request", nil }
			// Invoke the callback synchronously with a live context. Shutdown
			// cancellation must not hide an incorrectly admitted naming turn.
			s.automaticOwnedTitle(t.Context(), row)
			if calls.Load() != 0 {
				t.Fatal("off/manual conversation spent a model turn")
			}
		})
	}
}

func TestGeminiFailedAutomaticNamingIsNotRetriedAfterRestart(t *testing.T) {
	s, row := automaticGeminiFixture(t)
	if _, err := nextconfig.Open(s.cfg.Dir).Set(map[string]any{"codex_auto_name": true, "auto_name_assistant": "claude"}); err != nil {
		t.Fatal(err)
	}
	called := make(chan struct{})
	var calls atomic.Int32
	s.nameSession = func(context.Context, string, string) (string, error) {
		if calls.Add(1) == 1 {
			close(called)
		}
		return "", context.Canceled
	}
	sendAutomaticFixture(t, s, row, "First request")
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("naming did not start")
	}
	sendAutomaticFixture(t, s, row, "Second request")
	if err := s.antigravitySessions.Load().Shutdown(); err != nil {
		t.Fatal(err)
	}
	s.antigravitySessions.Store(nil)
	stop, err := s.StartAntigravitySessions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	sendAutomaticFixture(t, s, row, "Third request after restart")
	if err := s.antigravitySessions.Load().Shutdown(); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("failed automatic naming repeated %d times", calls.Load())
	}
}

func TestGeminiNamingDoesNotBlockInputOrOverwriteManualName(t *testing.T) {
	s, row := automaticGeminiFixture(t)
	if _, err := nextconfig.Open(s.cfg.Dir).Set(map[string]any{"codex_auto_name": true, "auto_name_assistant": "claude"}); err != nil {
		t.Fatal(err)
	}
	called, release := make(chan struct{}), make(chan struct{})
	s.nameSession = func(ctx context.Context, _ string, _ string) (string, error) {
		close(called)
		select {
		case <-release:
			return "Model name", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	sendAutomaticFixture(t, s, row, "First request")
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("naming did not start")
	}
	sendAutomaticFixture(t, s, row, "Second request while naming runs")
	if _, err := s.saveSessionTitle(row, "Human name", time.Now()); err != nil {
		t.Fatal(err)
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	var current session.Session
	for time.Now().Before(deadline) {
		inv, _ := s.antigravitySessions.Load().Inventory(t.Context())
		if len(inv.Sessions) == 1 {
			current = inv.Sessions[0]
			if current.Rungs.Thread == "Model name" {
				break
			}
		}
		time.Sleep(time.Millisecond)
	}
	if current.Rungs.Thread != "Model name" || s.sessionDisplayLabel(t.Context(), current) != "Human name" {
		t.Fatal("manual name lost its priority")
	}
}

func TestGeminiAutomaticNamingOffDuringTurnPreventsPublication(t *testing.T) {
	s, row := automaticGeminiFixture(t)
	s.firstSessionRequest = func(session.Session) (string, error) { return "First request", nil }
	if _, err := nextconfig.Open(s.cfg.Dir).Set(map[string]any{"codex_auto_name": true, "auto_name_assistant": "claude"}); err != nil {
		t.Fatal(err)
	}
	called, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s.nameSession = func(ctx context.Context, _ string, _ string) (string, error) {
		calls.Add(1)
		close(called)
		select {
		case <-release:
			return "Model name", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	go func() { defer close(done); s.automaticOwnedTitle(t.Context(), row) }()
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("naming did not start")
	}
	if _, err := nextconfig.Open(s.cfg.Dir).Set(map[string]any{"codex_auto_name": false}); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("naming did not settle")
	}
	inv, _ := s.antigravitySessions.Load().Inventory(t.Context())
	if calls.Load() != 1 || len(inv.Sessions) != 1 || inv.Sessions[0].Rungs.Thread != "" {
		t.Fatal("disabled naming published a title")
	}
}

func TestGeminiAutomaticNamingResolvesSessionModelToAntigravity(t *testing.T) {
	s, row := automaticGeminiFixture(t)
	// auto_name_assistant is set to "session", without explicitly setting codex_auto_name
	if _, err := nextconfig.Open(s.cfg.Dir).Set(map[string]any{"auto_name_assistant": "session"}); err != nil {
		t.Fatal(err)
	}
	var askedAssistant string
	s.nameSession = func(ctx context.Context, text, assistant string) (string, error) {
		askedAssistant = assistant
		return "Antigravity generated title", nil
	}
	h := s.antigravitySessions.Load()
	if err := h.Send(t.Context(), row, "Build an analytics dashboard"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		inv, _ := h.Inventory(t.Context())
		if len(inv.Sessions) == 1 && inv.Sessions[0].Rungs.Thread == "Antigravity generated title" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if askedAssistant != "antigravity" {
		t.Fatalf("expected assistant antigravity, got %q", askedAssistant)
	}
	inv, _ := h.Inventory(t.Context())
	if len(inv.Sessions) != 1 || inv.Sessions[0].Rungs.Thread != "Antigravity generated title" {
		t.Fatalf("session was not automatically named: %+v", inv.Sessions)
	}
}

// A Grok Session is named by the same producer callback as Gemini; before the
// Grok host carried the callbacks, its title stayed empty forever.
func TestGrokAutomaticNamingRunsAfterItsTurn(t *testing.T) {
	t.Setenv("CLW_HTTP_PEER", "yes")
	s := capacityServer(t)
	s.icons = &icon.Registry{}
	s.broker = newBroker(s)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	h, err := antigravitysession.New(t.Context(), s.grokHostOptions(exe, dir, []string{"-test.run=^TestOwnedHTTPPeer$"}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Shutdown() })
	s.grokSessions.Store(h)
	if _, err := nextconfig.Open(s.cfg.Dir).Set(map[string]any{"codex_auto_name": true}); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	s.nameSession = func(ctx context.Context, text, assistant string) (string, error) {
		calls.Add(1)
		if !strings.Contains(text, "Build a calendar") {
			return "", context.Canceled
		}
		return "Calendar reminders", nil
	}
	row, err := h.Open(t.Context(), ports.OpenRequest{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Send(t.Context(), row, "Build a calendar"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		inv, _ := h.Inventory(t.Context())
		if len(inv.Sessions) == 1 && inv.Sessions[0].Rungs.Thread == "Calendar reminders" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	inv, _ := h.Inventory(t.Context())
	if calls.Load() != 1 || len(inv.Sessions) != 1 || inv.Sessions[0].Rungs.Thread != "Calendar reminders" {
		t.Fatalf("Grok automatic naming did not run: calls=%d", calls.Load())
	}
}
