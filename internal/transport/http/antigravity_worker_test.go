//go:build darwin || linux

package http

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/antigravitysession"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestOwnedWorkerCallbacksReusePublishedRoleAndNativeSettings(t *testing.T) {
	t.Setenv("CLW_HTTP_PEER", "yes")
	s := capacityServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	profile := t.TempDir()
	if err := os.MkdirAll(filepath.Join(profile, "antigravity-acp"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "antigravity-acp", "settings.json"), []byte(`{"auth":{"type":"oauth-personal"},"useG1Credits":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	h, err := antigravitysession.New(ctx, antigravitysession.Options{Executable: exe, Args: []string{"-test.run=^TestOwnedHTTPPeer$"}, AccountDir: profile, StateDir: filepath.Join(t.TempDir(), "history")})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Shutdown()
	s.antigravitySessions.Store(h)
	s.terminals = []ports.TerminalHost{ownedAntigravityTerminal{s}}
	s.inventory = app.Inventory{Terminals: s.terminals}
	s.broker = newBroker(s)
	resolved := 0
	starter := app.Starter{Squad: s.store, SquadDir: s.cfg.Dir, ResolveSquadSnapshot: func(context.Context, string, string) (json.RawMessage, error) {
		resolved++
		return json.RawMessage(`{"definition_id":"clawdline.persona.backend","scope_id":"global","definition":{"version":"1","body":"Synthetic worker role"}}`), nil
	}}
	place := projects.Place{Path: profile, Label: "Synthetic worker"}
	published, err := starter.PrepareOwnedContext(ctx, place, "backend")
	if err != nil {
		t.Fatal(err)
	}
	r := orchestrator.Record{Title: place.Label, Persona: "backend", Model: "synthetic-pro", ReasoningEffort: "high", PermissionMode: "edits"}
	child, err := s.broker.OpenOwnedChild(ctx, r, profile, published.LaunchID, published.Files)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != 1 || child.Model != "synthetic-pro" || child.ReasoningEffort != "high" {
		t.Fatalf("snapshot/facts=%d %+v", resolved, child)
	}
	if _, err := os.Stat(filepath.Join(profile, "last-prompt.json")); !os.IsNotExist(err) {
		t.Fatal("worker configuration sent a prompt")
	}
	writes, err := os.ReadFile(filepath.Join(profile, "config-writes"))
	if err != nil || string(writes) != "mode=auto_edit\nmodel=synthetic-pro\nthinking_level=high\n" {
		t.Fatalf("native ACK order=%q %v", writes, err)
	}
	if err := h.Send(ctx, child.Row, "Synthetic first task input"); err != nil {
		t.Fatal(err)
	}
	for {
		rd, _ := h.Inventory(ctx)
		if len(rd.Sessions) == 1 && rd.Sessions[0].State == session.StateIdle {
			break
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		time.Sleep(time.Millisecond)
	}
	prompt, err := os.ReadFile(filepath.Join(profile, "last-prompt.json"))
	if err != nil || !strings.Contains(string(prompt), "Synthetic worker role") {
		t.Fatal("published role did not reach native prompt")
	}
	capPath, err := os.ReadFile(filepath.Join(profile, "last-capability-path"))
	if err != nil || string(capPath) != published.Files.CapabilityPath {
		t.Fatal("worker received another launch capability")
	}
	if err := s.broker.CloseOwnedChild(ctx, child.Row.ID, "another-native", profile); err == nil {
		t.Fatal("wrong native binding stopped worker")
	}
	if err := s.broker.CloseOwnedChild(ctx, child.Row.ID, child.Row.ConversationID, profile); err != nil {
		t.Fatal(err)
	}
	if count, _ := h.Stats(); count != 0 {
		t.Fatal("native worker still active")
	}
	if _, err := h.Transcript(child.Row, 100); err != nil {
		t.Fatal("stopping worker discarded history", err)
	}
}
