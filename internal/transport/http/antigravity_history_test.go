//go:build darwin || linux

package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/ownrecords"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/icon"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestGeminiHistoryAndResumeUseExistingProjectRoutesAfterRestart(t *testing.T) {
	s := capacityServer(t)
	s.icons = &icon.Registry{}
	host := ownedAntigravityTerminal{s}
	s.terminals = []ports.TerminalHost{host}
	s.inventory = app.Inventory{Terminals: s.terminals}
	s.broker = newBroker(s)
	s.executionMachineID = "mac_a"
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
	t.Setenv("CLW_HTTP_PEER", "yes")
	cfg, _ := json.Marshal(map[string]any{"executable": exe, "accountDir": profile, "args": []string{"-test.run=^TestOwnedHTTPPeer$"}})
	if err := os.WriteFile(filepath.Join(s.cfg.Dir, "antigravity-acp-pilot.json"), cfg, 0600); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "places.json")
	paths, _ := json.Marshal([]string{profile})
	if err := os.WriteFile(fixture, paths, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(projects.FixtureEnv, fixture)
	places := s.projectReaders().places.List(nil, 40)
	if len(places) != 1 {
		t.Fatal("isolated project missing")
	}
	stop, err := s.StartAntigravitySessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if stop != nil {
			stop()
		}
	})
	rec := httptest.NewRecorder()
	s.startPlace(rec, httptest.NewRequest(http.MethodPost, "/v1/places/fixture/start/antigravity", nil), places[0].ID, "antigravity", "", "")
	var started contract.PlaceStarted
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &started) != nil || !started.OK {
		t.Fatalf("start=%d %s", rec.Code, rec.Body.String())
	}
	initial := s.inventory.Read(context.Background())
	items := initial.Assistants()
	lives := make([]ownrecords.Live, len(items))
	for i, item := range items {
		lives[i] = liveOf(item)
	}
	generation := s.observeExecutions(context.Background(), initial, items, lives)[started.ID]
	if generation == "" {
		t.Fatal("new owned conversation has no paired-device target")
	}
	ctx := app.WithOperationCheck(context.Background(), func() error { return s.AdmitExecutionTarget(context.Background(), "mac_a", started.ID, generation) })
	if _, err := s.actions().Send(ctx, started.ID, "persist this synthetic turn"); err != nil {
		t.Fatal(err)
	}
	var original session.Session
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		inv := s.inventory.Read(context.Background())
		if len(inv.Sessions) == 1 && inv.Sessions[0].State == session.StateIdle {
			original = inv.Sessions[0]
			if page := s.transcriptPage(started.ID, original, 20); len(page.Entries) == 2 {
				break
			}
		}
		time.Sleep(time.Millisecond)
	}
	if original.ID == "" {
		t.Fatal("turn did not finish")
	}
	stop()
	stop, err = s.StartAntigravitySessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	inv := s.inventory.Read(context.Background())
	if len(inv.Sessions) != 1 || inv.Sessions[0].ID != started.ID || inv.Sessions[0].ConversationID != original.ConversationID {
		t.Fatalf("restored inventory=%+v", inv)
	}
	pairedRead := httptest.NewRecorder()
	s.transcriptRoute(pairedRead, pinned(http.MethodGet, "/v1/transcript?session="+started.ID, generation))
	if pairedRead.Code != 200 {
		t.Fatalf("paired-device history before native resume=%d %s", pairedRead.Code, pairedRead.Body.String())
	}
	page := s.transcriptPage(started.ID, inv.Sessions[0], 1)
	if len(page.Entries) != 1 || page.Entries[0].Role != "assistant" || page.NextBefore <= 0 {
		t.Fatalf("newest=%+v", page)
	}
	older := s.transcriptPageBefore(started.ID, inv.Sessions[0], 1, page.NextBefore, page.Signature)
	if len(older.Entries) != 1 || older.Entries[0].Text != "persist this synthetic turn" || older.Entries[0].ID == "" || older.Entries[0].ID == page.Entries[0].ID {
		t.Fatalf("older=%+v", older)
	}
	stale := s.transcriptPageBefore(started.ID, inv.Sessions[0], 1, page.NextBefore, "acp2:other-generation:1")
	if stale.Note != recordNote(transcript.ErrCursorStale) {
		t.Fatalf("stale cursor not exposed=%+v", stale)
	}
	past := httptest.NewRecorder()
	s.pastRoute(past, httptest.NewRequest(http.MethodGet, "/v1/places/fixture/sessions/antigravity", nil), places[0].ID, "antigravity")
	var history contract.PastSessionList
	if past.Code != 200 || json.Unmarshal(past.Body.Bytes(), &history) != nil || len(history.Sessions) != 1 || history.Sessions[0].ID != original.ConversationID {
		t.Fatalf("history=%d %s", past.Code, past.Body.String())
	}
	if history.NextCursor != "native:" {
		t.Fatal("older native history has no entry point")
	}
	nativePast := httptest.NewRecorder()
	s.pastRoute(nativePast, httptest.NewRequest(http.MethodGet, "/v1/places/fixture/sessions/antigravity?cursor=native:", nil), places[0].ID, "antigravity")
	var nativeHistory contract.PastSessionList
	if nativePast.Code != 200 || json.Unmarshal(nativePast.Body.Bytes(), &nativeHistory) != nil || len(nativeHistory.Sessions) != 2 || nativeHistory.NextCursor != "native:older-native" {
		t.Fatalf("native history=%d %s", nativePast.Code, nativePast.Body.String())
	}
	res := httptest.NewRecorder()
	s.resumePlace(res, httptest.NewRequest(http.MethodPost, "/v1/places/fixture/resume/antigravity/native", nil), places[0].ID, "antigravity", original.ConversationID, "")
	var resumed contract.PlaceResumed
	if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &resumed) != nil || !resumed.OK || resumed.ID != started.ID || resumed.Session != original.ConversationID {
		t.Fatalf("resume=%d %s", res.Code, res.Body.String())
	}
	if _, err := s.actions().Send(ctx, started.ID, "continue this same conversation"); err != nil {
		t.Fatal(err)
	}
	stop()
	stop, err = s.StartAntigravitySessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	importedResponse := httptest.NewRecorder()
	s.resumePlace(importedResponse, httptest.NewRequest(http.MethodPost, "/v1/places/fixture/resume/antigravity/previous", nil), places[0].ID, "antigravity", "synthetic-old-native", "")
	var imported contract.PlaceResumed
	if importedResponse.Code != 200 || json.Unmarshal(importedResponse.Body.Bytes(), &imported) != nil || !imported.OK || imported.ID == started.ID {
		t.Fatalf("native import=%d %s", importedResponse.Code, importedResponse.Body.String())
	}
	for _, row := range s.inventory.Read(context.Background()).Sessions {
		if row.ID == imported.ID {
			page := s.transcriptPage(row.ID, row, 20)
			if len(page.Entries) != 2 || page.Entries[0].Text != "previous synthetic input" || page.Entries[1].Text != "previous synthetic reply" {
				t.Fatalf("imported history=%+v", page)
			}
			return
		}
	}
	t.Fatal("imported native conversation did not enter the normal inventory")
}
