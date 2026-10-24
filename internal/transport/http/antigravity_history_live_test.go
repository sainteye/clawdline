//go:build darwin || linux

package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/icon"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// This opt-in probe uses an existing personal profile without copying its
// credentials. It sends exactly two tiny synthetic prompts, without retries,
// through an isolated server and workspace; the running daemon is untouched.
func geminiLiveConfiguration(t *testing.T) []byte {
	t.Helper()
	path := os.Getenv("CLAWDLINE_GEMINI_FREE_RESUME_TEST_CONFIG")
	if path == "" {
		t.Skip("explicit free personal CLI probe configuration required")
	}
	if !filepath.IsAbs(path) {
		t.Fatal("probe configuration must be an explicit absolute path")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("probe configuration unavailable")
	}
	// The HTTP suite keeps its server in an empty HOME. Only the explicitly
	// selected native CLI receives its original login environment; constructors
	// and repository fixtures continue to use the hermetic test home.
	nativeHome := os.Getenv("CLAWDLINE_GEMINI_NATIVE_HOME")
	if !filepath.IsAbs(nativeHome) {
		t.Fatal("explicit native login HOME required")
	}
	var cfg struct {
		Executable, AccountDir string
		Args                   []string
	}
	if json.Unmarshal(raw, &cfg) != nil || !filepath.IsAbs(cfg.Executable) {
		t.Fatal("invalid native probe configuration")
	}
	cfg.Args = append([]string{"HOME=" + nativeHome, cfg.Executable}, cfg.Args...)
	cfg.Executable = "/usr/bin/env"
	raw, err = json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestGeminiFreeNativeHistorySurvivesRestart(t *testing.T) {
	raw := geminiLiveConfiguration(t)
	s := capacityServer(t)
	s.icons = &icon.Registry{}
	host := ownedAntigravityTerminal{s}
	s.terminals = []ports.TerminalHost{host}
	s.inventory = app.Inventory{Terminals: s.terminals}
	s.broker = newBroker(s)
	if err := os.WriteFile(filepath.Join(s.cfg.Dir, "antigravity-acp-pilot.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	fixture := filepath.Join(t.TempDir(), "places.json")
	paths, _ := json.Marshal([]string{workspace})
	if err := os.WriteFile(fixture, paths, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(projects.FixtureEnv, fixture)
	places := s.projectReaders().places.List(nil, 40)
	if len(places) != 1 {
		t.Fatal("isolated workspace unavailable")
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
	s.startPlace(rec, httptest.NewRequest(http.MethodPost, "/v1/places/probe/start/antigravity", nil), places[0].ID, "antigravity", "", "")
	var started contract.PlaceStarted
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &started) != nil || !started.OK {
		var refusal contract.PlaceRefusal
		_ = json.Unmarshal(rec.Body.Bytes(), &refusal)
		t.Fatalf("native start status=%d code=%s detail=%s", rec.Code, refusal.Error.Code, refusal.Error.Message)
	}
	ctx := app.WithOperationCheck(context.Background(), func() error { return nil })
	prompts := 0
	send := func(text string) {
		t.Helper()
		if prompts >= 2 {
			t.Fatal("probe prompt cap reached")
		}
		prompts++
		bounded, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		if _, err := s.actions().Send(bounded, started.ID, text); err != nil {
			t.Fatalf("native send %d failed: %v", prompts, err)
		}
	}
	await := func(want string) session.Session {
		t.Helper()
		deadline := time.Now().Add(45 * time.Second)
		for time.Now().Before(deadline) {
			inv := s.inventory.Read(context.Background())
			for _, row := range inv.Sessions {
				if row.ID != started.ID {
					continue
				}
				page := s.transcriptPage(row.ID, row, 100)
				for _, entry := range page.Entries {
					if entry.Role == "assistant" && strings.TrimSpace(entry.Text) == want && row.State == session.StateIdle {
						return row
					}
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("native reply deadline; prompt was not retried")
		return session.Session{}
	}
	send("Remember this synthetic marker: ORCHARD-67148. Reply only SAVED. Do not use tools.")
	original := await("SAVED")
	stop()
	stop, err = s.StartAntigravitySessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	inv := s.inventory.Read(context.Background())
	if len(inv.Sessions) != 1 || inv.Sessions[0].ID != started.ID || inv.Sessions[0].ConversationID != original.ConversationID {
		t.Fatal("restart lost exact conversation identity")
	}
	page := s.transcriptPage(started.ID, inv.Sessions[0], 100)
	if len(page.Entries) != 2 || page.Entries[1].Text != "SAVED" {
		t.Fatal("saved history unavailable before native resume")
	}
	send("What synthetic marker did I ask you to remember? Reply only with that marker. Do not use tools.")
	resumed := await("ORCHARD-67148")
	if resumed.ConversationID != original.ConversationID {
		t.Fatal("resumed a different native conversation")
	}
	stop()
	stop, err = s.StartAntigravitySessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	inv = s.inventory.Read(context.Background())
	if len(inv.Sessions) != 1 || inv.Sessions[0].ID != started.ID {
		t.Fatal("second restart lost stable owned ID")
	}
	page = s.transcriptPage(started.ID, inv.Sessions[0], 100)
	if len(page.Entries) != 4 {
		t.Fatalf("history after two restarts has %d entries, want 4", len(page.Entries))
	}
	t.Logf("personal free CLI prompts=%d; two isolated service restarts; exact native and owned identity retained; four history entries readable before reconnect", prompts)
}

// The target comes from a private native-list receipt, never a guessed latest
// conversation. Import and read-only reopen make no model request.
func TestGeminiPersonalNativeImportSurvivesRestart(t *testing.T) {
	targetFile := os.Getenv("CLAWDLINE_GEMINI_IMPORT_TARGET")
	if targetFile == "" {
		t.Skip("explicit native history target required")
	}
	raw := geminiLiveConfiguration(t)
	targetRaw, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal("native target receipt unavailable")
	}
	var target struct {
		CWD      string `json:"cwd"`
		NativeID string `json:"nativeId"`
	}
	if json.Unmarshal(targetRaw, &target) != nil || !filepath.IsAbs(target.CWD) || target.NativeID == "" {
		t.Fatal("invalid native target receipt")
	}
	s := capacityServer(t)
	s.icons = &icon.Registry{}
	host := ownedAntigravityTerminal{s}
	s.terminals = []ports.TerminalHost{host}
	s.inventory = app.Inventory{Terminals: s.terminals}
	s.broker = newBroker(s)
	if err := os.WriteFile(filepath.Join(s.cfg.Dir, "antigravity-acp-pilot.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "places.json")
	paths, _ := json.Marshal([]string{target.CWD})
	if err := os.WriteFile(fixture, paths, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(projects.FixtureEnv, fixture)
	places := s.projectReaders().places.List(nil, 40)
	if len(places) != 1 {
		t.Fatal("exact import workspace unavailable")
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
	// The first upgrade must expose the old native conversation through the
	// ordinary project history list, before any local projection exists.
	cursor := ""
	found := false
	visited := map[string]bool{}
	deadline, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	for !found {
		past := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v1/places/probe/sessions/antigravity?cursor="+url.QueryEscape(cursor), nil).WithContext(deadline)
		s.pastRoute(past, req, places[0].ID, "antigravity")
		var history contract.PastSessionList
		if past.Code != 200 || json.Unmarshal(past.Body.Bytes(), &history) != nil {
			t.Fatalf("native history listing status=%d", past.Code)
		}
		for _, row := range history.Sessions {
			if row.ID == target.NativeID {
				found = true
			}
		}
		if found {
			break
		}
		if history.NextCursor == "" || visited[history.NextCursor] {
			t.Fatal("exact native conversation absent from project history")
		}
		visited[history.NextCursor] = true
		cursor = history.NextCursor
	}
	response := httptest.NewRecorder()
	s.resumePlace(response, httptest.NewRequest(http.MethodPost, "/v1/places/probe/resume/antigravity/import", nil), places[0].ID, "antigravity", target.NativeID, "")
	var resumed contract.PlaceResumed
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &resumed) != nil || !resumed.OK {
		t.Fatalf("native import status=%d", response.Code)
	}
	inv := s.inventory.Read(context.Background())
	if len(inv.Sessions) != 1 || inv.Sessions[0].ConversationID != target.NativeID {
		t.Fatal("native import identity mismatch")
	}
	page := s.transcriptPage(resumed.ID, inv.Sessions[0], 1000)
	if len(page.Entries) < 2 || page.Truncation != nil {
		t.Fatal("native replay was not completely imported")
	}
	stop()
	stop, err = s.StartAntigravitySessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	inv = s.inventory.Read(context.Background())
	if len(inv.Sessions) != 1 || inv.Sessions[0].ID != resumed.ID || inv.Sessions[0].ConversationID != target.NativeID {
		t.Fatal("imported identity lost after restart")
	}
	var entries []contract.TranscriptEntry
	before := int64(0)
	held := ""
	seen := map[string]bool{}
	for {
		part := s.transcriptPageBefore(resumed.ID, inv.Sessions[0], 1, before, held)
		if part.Note != "" || len(part.Entries) != 1 || seen[part.Entries[0].ID] {
			t.Fatal("imported older page was unreadable or repeated")
		}
		seen[part.Entries[0].ID] = true
		entries = append(part.Entries, entries...)
		if part.NextBefore == 0 {
			break
		}
		if before > 0 && part.NextBefore >= before {
			t.Fatal("native older cursor did not advance")
		}
		before, held = part.NextBefore, part.Signature
	}
	if len(entries) != len(page.Entries) {
		t.Fatal("native import lost history on reopen")
	}
	for i, entry := range entries {
		if entry.ID != page.Entries[i].ID || entry.Text != page.Entries[i].Text || entry.Role != page.Entries[i].Role {
			t.Fatal("native import changed saved content")
		}
	}
	t.Logf("model prompts=0; existing native history imported and read after isolated restart across %d pages", len(entries))
}
