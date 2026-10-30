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
		if assistant != "claude" || !strings.Contains(text, "Build a calendar") || !strings.Contains(text, "synthetic HTTP reply") {
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
	if err := h.Send(t.Context(), row, "Add reminders"); err != nil {
		t.Fatal(err)
	}
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
		err := s.antigravitySessions.Load().Send(t.Context(), row, text)
		if err == nil {
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
			sendAutomaticFixture(t, s, row, "First request")
			// Shutdown joins every callback, including a wrongly started model turn.
			if err := s.antigravitySessions.Load().Shutdown(); err != nil {
				t.Fatal(err)
			}
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
