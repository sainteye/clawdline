//go:build darwin || linux

package http

import (
	"context"
	"os"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/antigravitysession"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// The Workflow setting grok_full_permission freezes Grok's always-approve
// mode into each new Grok Session. Off, a new Session stays supervised and
// cannot be widened after launch; a running Session keeps what it started with.
func TestGrokFullPermissionSettingStartsNewSessionsWithoutAsking(t *testing.T) {
	t.Setenv("CLW_HTTP_PEER", "yes")
	s := capacityServer(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	h, err := antigravitysession.New(context.Background(), antigravitysession.Options{Assistant: session.AssistantGrok, Executable: exe, AccountDir: dir, Args: []string{"-test.run=^TestOwnedHTTPPeer$"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := h.Shutdown(); err != nil {
			t.Error(err)
		}
	}()
	s.grokSessions.Store(h)
	open := func() session.Session {
		t.Helper()
		row, err := s.openOwnedAssistant(t.Context(), app.Starter{}, projects.Place{Path: dir}, "", "", nil, projects.AssistantGrok)
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	supervised := open()
	if err := h.ConfigureDispatch(t.Context(), supervised, "", "", "full"); err == nil {
		t.Fatal("a Session opened with the setting off runs with full permission")
	}
	if _, err := s.settingsFile().Set(map[string]any{"grok_full_permission": true}); err != nil {
		t.Fatal(err)
	}
	full := open()
	if err := h.ConfigureDispatch(t.Context(), full, "", "", "full"); err != nil {
		t.Fatalf("a Session opened with the setting on is not full: %v", err)
	}
	if err := h.ConfigureDispatch(t.Context(), supervised, "", "", "full"); err == nil {
		t.Fatal("turning the setting on widened a Session that was already running")
	}
	if _, err := s.settingsFile().Set(map[string]any{"grok_full_permission": false}); err != nil {
		t.Fatal(err)
	}
	if err := h.ConfigureDispatch(t.Context(), open(), "", "", "full"); err == nil {
		t.Fatal("turning the setting off left new Sessions at full permission")
	}
}
