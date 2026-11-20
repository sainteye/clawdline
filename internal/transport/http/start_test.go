//go:build darwin || linux

package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestStartRegisteredProjectDoesNotWaitForSessionDiscovery(t *testing.T) {
	t.Setenv(projects.FixtureEnv, "")
	s, _ := automaticGeminiFixture(t)
	home := placesHome(t)
	registered := filepath.Join(home, "saved-project")
	discovered := filepath.Join(home, "live-project")
	for _, path := range []string{registered, discovered} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	registry := s.projectReaders().registry
	if _, err := registry.Add([]string{registered}, time.Now()); err != nil {
		t.Fatal(err)
	}
	var scans atomic.Int32
	s.readings = app.NewInventoryReading(func(context.Context) session.Inventory {
		scans.Add(1)
		// A separate observation of the running daemon took roughly this long.
		time.Sleep(250 * time.Millisecond)
		return session.Inventory{Complete: true, Sessions: []session.Session{
			{ID: "%1", CWD: discovered, Assistant: session.AssistantCodex},
			{ID: "%2", CWD: registered, Assistant: session.AssistantCodex},
		}}
	}, 0)

	start := func(path, key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/places/"+projects.PlaceID(path)+"/start/antigravity", nil)
		req.Header.Set("Idempotency-Key", key)
		ctx := context.WithValue(req.Context(), accessKey{}, access{
			verdict: auth.Verdict{Allowed: true, Caps: auth.NewCaps(auth.Read, auth.Send), Device: "fixture-device"},
		})
		rec := httptest.NewRecorder()
		s.placeRoute(rec, req.WithContext(ctx))
		return rec
	}
	checkStarted := func(rec *httptest.ResponseRecorder, path string) string {
		t.Helper()
		var result contract.PlaceStarted
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &result) != nil ||
			!result.OK || result.ID == "" || result.Place != projects.PlaceID(path) {
			t.Fatalf("start = %d, %s", rec.Code, rec.Body.String())
		}
		return result.ID
	}

	requestedAt := time.Now()
	id := checkStarted(start(registered, "saved"), registered)
	t.Logf("registered start acknowledgement in %s; discovery reads: %d", time.Since(requestedAt), scans.Load())
	if scans.Load() != 0 {
		t.Fatal("a registered Project waited for discovery of unrelated Sessions")
	}
	if replay := checkStarted(start(registered, "saved"), registered); replay != id {
		t.Fatal("retry started a second Session")
	}
	checkStarted(start(discovered, "live"), discovered)
	if scans.Load() != 1 {
		t.Fatal("a Project known through a live Session needs fresh discovery")
	}
	if _, err := registry.Remove([]string{registered}); err != nil {
		t.Fatal(err)
	}
	if refused := start(registered, "removed"); refused.Code != http.StatusNotFound {
		t.Fatalf("removed Project stayed launchable: %d %s", refused.Code, refused.Body.String())
	}
}
