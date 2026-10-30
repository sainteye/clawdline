//go:build darwin || linux

package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/contract"
)

func TestGeminiStartAppliesOfferedModelBeforePublishing(t *testing.T) {
	for _, model := range []string{"synthetic-pro", "unoffered"} {
		t.Run(model, func(t *testing.T) {
			s, _ := automaticGeminiFixture(t)
			dir := t.TempDir()
			fixture := filepath.Join(t.TempDir(), "places.json")
			raw, _ := json.Marshal([]string{dir})
			if err := os.WriteFile(fixture, raw, 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv(projects.FixtureEnv, fixture)
			places := s.projectReaders().places.List(nil, 40)
			if len(places) != 1 {
				t.Fatal("isolated project missing")
			}
			rec := httptest.NewRecorder()
			s.startPlace(rec, httptest.NewRequest(http.MethodPost, "/v1/places/fixture/start/antigravity/"+model, nil), places[0].ID, "antigravity", model, "")
			h := s.antigravitySessions.Load()
			inv, _ := h.Inventory(t.Context())
			if model == "unoffered" {
				if rec.Code != 409 || len(inv.Sessions) != 1 {
					t.Fatalf("failed selection published a duplicate: %d %s", rec.Code, rec.Body)
				}
				return
			}
			var reply contract.PlaceStarted
			if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &reply) != nil || reply.Model != model || len(inv.Sessions) != 2 {
				t.Fatalf("offered startup selection refused: %d %s", rec.Code, rec.Body)
			}
			for _, row := range inv.Sessions {
				if row.ID != reply.ID {
					continue
				}
				facts, _ := h.Facts(row)
				page, _ := h.Transcript(row, 20)
				if facts.Model != model || len(page.Entries) != 0 {
					t.Fatal("model was not applied without a prompt")
				}
			}
		})
	}
}
