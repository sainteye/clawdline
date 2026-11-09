//go:build darwin || linux

package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/antigravitysession"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/icon"
)

func TestOwnedBusyCloseUsesConfirmedHTTPRouteAndKeepsHistory(t *testing.T) {
	t.Setenv("CLW_HTTP_PEER", "yes")
	t.Setenv("CLW_HTTP_INTERRUPT", "yes")
	s := capacityServer(t)
	s.icons = &icon.Registry{}
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
	h, err := antigravitysession.New(context.Background(), antigravitysession.Options{Executable: exe, AccountDir: dir, StateDir: filepath.Join(dir, "history"), Args: []string{"-test.run=^TestOwnedHTTPPeer$"}})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Shutdown()
	s.antigravitySessions.Store(h)
	s.terminals = []ports.TerminalHost{ownedAntigravityTerminal{s}}
	s.inventory = app.Inventory{Terminals: s.terminals}
	s.broker = newBroker(s)
	row, err := h.Open(context.Background(), ports.OpenRequest{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.actions().Send(context.Background(), row.ID, "synthetic unfinished close request"); err != nil {
		t.Fatal(err)
	}
	close := func(force bool) *httptest.ResponseRecorder {
		snapshot := s.sessionsPayloadFrom(context.Background(), s.inventory.Read(context.Background()))
		if len(snapshot.Sessions) != 1 {
			t.Fatal(snapshot)
		}
		body, _ := json.Marshal(contract.CloseRequest{Force: force, ExpectedCloseabilityVersion: snapshot.Sessions[0].Closeability.Version})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/sessions/"+row.ID+"/close", bytes.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{verdict: auth.Verdict{Allowed: true, Caps: auth.NewCaps(auth.Read, auth.Send)}}))
		s.sessionAction(rec, req)
		return rec
	}
	if rec := close(false); rec.Code != http.StatusConflict {
		t.Fatal("working close skipped confirmation", rec.Code, rec.Body.String())
	}
	if rec := close(true); rec.Code != http.StatusOK {
		t.Fatal("confirmed close failed", rec.Code, rec.Body.String())
	}
	if inv, err := h.Inventory(context.Background()); err != nil || len(inv.Sessions) != 0 {
		t.Fatal("closed conversation remained visible", inv, err)
	}
	if processes, _ := h.Stats(); processes != 0 {
		t.Fatal("native process survived confirmed close", processes)
	}
	if page, err := h.Transcript(row, 10); err != nil || len(page.Entries) == 0 || page.Entries[0].Text != "synthetic unfinished close request" {
		t.Fatal("confirmed close discarded history", page, err)
	}
}

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
