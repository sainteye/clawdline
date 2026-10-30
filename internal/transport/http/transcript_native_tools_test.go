//go:build darwin || linux

package http

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/antigravitysession"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/contract"
)

func TestOwnedNativeToolDetailCrossesHTTPBoundary(t *testing.T) {
	t.Setenv("CLW_HTTP_PEER", "yes")
	t.Setenv("CLW_HTTP_TOOLS", "yes")
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "antigravity-acp"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "antigravity-acp/settings.json"), []byte(`{"useG1Credits":false,"auth":{"type":"oauth-personal"}}`), 0600); err != nil {
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
	s := capacityServer(t)
	s.antigravitySessions.Store(h)
	s.terminals = []ports.TerminalHost{ownedAntigravityTerminal{s}}
	s.inventory = app.Inventory{Terminals: s.terminals}
	row, err := h.Open(context.Background(), ports.OpenRequest{Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.SendProtocol(context.Background(), row, "synthetic input", nil); err != nil {
		t.Fatal(err)
	}
	var tool contract.TranscriptEntry
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		page := s.transcriptPageBeforeProjection(row.ID, row, 200, 0, true, true)
		for _, entry := range page.Entries {
			if entry.Activity != nil && entry.Detail != nil {
				tool = entry
			}
		}
		if tool.Detail != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if tool.Detail == nil {
		t.Fatal("native result never became readable")
	}
	w := httptest.NewRecorder()
	ref := tool.Detail.Reference
	s.transcriptEntryDetail(w, httptest.NewRequest("GET", "/v1/transcript", nil), row.ID, row, "entry-v1:"+ref)
	var answer struct {
		Reference string                   `json:"reference"`
		Entry     contract.TranscriptEntry `json:"entry"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || answer.Reference != ref || answer.Entry.Activity == nil || answer.Entry.Activity.Result != strings.Repeat("complete native output 😀\n", 2000) || answer.Entry.Detail != nil {
		t.Fatalf("native detail did not cross HTTP: %d", w.Code)
	}
	wrong := row
	wrong.ConversationID = "different-native"
	w = httptest.NewRecorder()
	s.transcriptEntryDetail(w, httptest.NewRequest("GET", "/v1/transcript", nil), wrong.ID, wrong, "entry-v1:"+ref)
	if w.Code != 409 {
		t.Fatalf("different native identity accepted: %d", w.Code)
	}
}
