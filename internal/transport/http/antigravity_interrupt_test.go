//go:build darwin || linux

package http

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/antigravitysession"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/ports"
)

func TestOwnedInterruptUsesNativeChannelWithoutTerminalFacts(t *testing.T) {
	t.Setenv("CLW_HTTP_PEER", "yes")
	t.Setenv("CLW_HTTP_INTERRUPT", "yes")
	s := capacityServer(t)
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
	h, err := antigravitysession.New(context.Background(), antigravitysession.Options{Executable: exe, AccountDir: dir, Args: []string{"-test.run=^TestOwnedHTTPPeer$"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := h.Shutdown(); err != nil {
			t.Error(err)
		}
	}()
	s.antigravitySessions.Store(h)
	s.terminals = []ports.TerminalHost{ownedAntigravityTerminal{s}}
	s.inventory = app.Inventory{Terminals: s.terminals}
	row, err := h.Open(context.Background(), ports.OpenRequest{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.actions().Send(context.Background(), row.ID, "synthetic active turn"); err != nil {
		t.Fatal(err)
	}
	checks := 0
	ctx := app.WithOperationCheck(context.Background(), func() error { checks++; return nil })
	if _, err = s.actions().Interrupt(ctx, row.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		raw, _ := os.ReadFile(filepath.Join(dir, "native-cancel"))
		if string(raw) == row.ConversationID {
			if checks < 2 {
				t.Fatal("write authorization was not rechecked")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("interrupt did not reach the native process")
}
