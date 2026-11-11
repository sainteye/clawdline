//go:build darwin || linux

package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/antigravitysession"
	"github.com/sainteye/clawdline/internal/adapters/artifacts"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/icon"
)

// `clawdline image send` from a Grok Session answered 409
// session_has_no_record (2026-11-11): only Gemini's host was asked for the
// history address. A Grok Session's pictures now bind to its next reply.
func TestGrokImageSendBindsNextReply(t *testing.T) {
	t.Setenv("CLW_HTTP_PEER", "yes")
	s := capacityServer(t)
	s.icons = &icon.Registry{}
	s.broker = newBroker(s)
	s.terminals = []ports.TerminalHost{ownedAntigravityTerminal{s}}
	s.inventory = app.Inventory{Terminals: s.terminals}
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
	row, err := h.Open(t.Context(), ports.OpenRequest{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	s.pictures.store = artifacts.NewStore(t.TempDir())
	body, _ := json.Marshal(map[string]any{"terminal": row.ID, "images": []map[string]string{{"path": geminiImageFile(t)}}})
	req := httptest.NewRequest(http.MethodPost, "/v1/artifacts/images", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{machine: true}))
	rec := httptest.NewRecorder()
	s.imagesRoute(rec, req)
	var stored contract.ImageStoreResult
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &stored) != nil || len(stored.Artifacts) != 1 {
		t.Fatalf("Grok image send refused: %d %s", rec.Code, rec.Body)
	}
	page := geminiImageReply(t, s, row, "Here are the two product pictures")
	for _, e := range page.Entries {
		if e.Role == "assistant" && len(e.Artifacts) == 1 && e.Artifacts[0].ID == stored.Artifacts[0].ID {
			return
		}
	}
	t.Fatalf("picture not under the Grok reply: %+v", page.Entries)
}
