package http

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/icon"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestProjectAndSessionIconReadLiveOverride(t *testing.T) {
	home := placesHome(t)
	state := filepath.Join(home, ".config", "clawdline-next")
	path := filepath.Join(home, "projects", "wave")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.OpenPlaceRegistry(state).Add([]string{path}, time.Now()); err != nil {
		t.Fatal(err)
	}
	icons, err := icon.NewRegistryWithOverrides(state)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: config.Config{Dir: state}, broker: &orchestrator.Broker{Dir: state}, icons: icons}
	read := func() (string, string, int) {
		t.Helper()
		rec := httptest.NewRecorder()
		s.placesRoute(rec, httptest.NewRequest("GET", "/v1/places", nil))
		if rec.Code != 200 {
			return "", "", rec.Code
		}
		var answer struct {
			Places []struct {
				Path string `json:"path"`
				Icon struct {
					Accent string `json:"accent"`
				} `json:"icon"`
				Setup struct {
					Icon string `json:"icon"`
				} `json:"setup"`
			} `json:"places"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
			t.Fatal(err)
		}
		for _, p := range answer.Places {
			if p.Path == path {
				return p.Icon.Accent, p.Setup.Icon, rec.Code
			}
		}
		t.Fatal("project absent")
		return "", "", 0
	}
	file := filepath.Join(state, "project-icon-overrides.json")
	write := func(color string) {
		t.Helper()
		data, err := json.Marshal(map[string]icon.Grid{path: {Accent: color, Cells: [][]*string{{&color}}}})
		if err != nil {
			t.Fatal(err)
		}
		tmp := file + ".new"
		if err := os.WriteFile(tmp, data, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, file); err != nil {
			t.Fatal(err)
		}
	}
	write("#68C8D8")
	if accent, source, code := read(); accent != "#68C8D8" || source != "override" || code != 200 {
		t.Fatalf("created: %s %s %d", accent, source, code)
	}
	row := s.sessionRow(rowInput{item: session.Session{CWD: path}})
	if row.Icon == nil || row.Icon.Accent != "#68C8D8" {
		t.Fatalf("session icon: %+v", row.Icon)
	}
	write("#123456")
	if accent, source, code := read(); accent != "#123456" || source != "override" || code != 200 {
		t.Fatalf("replaced: %s %s %d", accent, source, code)
	}
	row = s.sessionRow(rowInput{item: session.Session{CWD: path}})
	if row.Icon == nil || row.Icon.Accent != "#123456" {
		t.Fatalf("session replacement: %+v", row.Icon)
	}
	stale := sessionsSnapshotWire{Sessions: []sessionRowWire{{SessionRow: row.SessionRow}}}
	stale.Sessions[0].Icon.Accent = "#000000"
	if err := s.refreshSnapshotIcons(&stale); err != nil {
		t.Fatal(err)
	}
	if stale.Sessions[0].Icon.Accent != "#123456" {
		t.Fatalf("held Session snapshot: %+v", stale.Sessions[0].Icon)
	}
	// An empty machine's list stays a list: it went out as `"sessions": null`
	// and the header read that as still loading.
	empty := sessionsSnapshotWire{Sessions: []sessionRowWire{}}
	if err := s.refreshSnapshotIcons(&empty); err != nil {
		t.Fatal(err)
	}
	if wire, _ := json.Marshal(empty); !strings.Contains(string(wire), `"sessions":[]`) {
		t.Fatalf("empty snapshot: %s", wire)
	}
	if err := os.WriteFile(file, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, code := read(); code != 503 {
		t.Fatalf("corrupt override returned %d", code)
	}
	if err := s.refreshSnapshotIcons(&stale); err == nil {
		t.Fatal("corrupt override refreshed Session snapshot")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if _, source, code := read(); source == "override" || code != 200 {
		t.Fatalf("removed: %s %d", source, code)
	}
	// The compatibility source also refuses a damaged refresh.
	legacy := filepath.Join(home, ".claude", "project-icons.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, code := read(); code != 503 {
		t.Fatalf("corrupt legacy returned %d", code)
	}
}

func TestSVGProjectAndSessionIconWire(t *testing.T) {
	home := placesHome(t)
	state := filepath.Join(home, ".config", "clawdline-next")
	path := filepath.Join(home, "projects", "vector")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.OpenPlaceRegistry(state).Add([]string{path}, time.Now()); err != nil {
		t.Fatal(err)
	}
	icons, err := icon.NewRegistryWithOverrides(state)
	if err != nil {
		t.Fatal(err)
	}
	source := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M0 0h24v24z" fill="#123456"/></svg>`
	g := icon.Grid{Accent: "#123456", SVG: source}
	if err := icons.Save(path, g, icons.For(path)); err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: config.Config{Dir: state}, broker: &orchestrator.Broker{Dir: state}, icons: icons}
	rec := httptest.NewRecorder()
	s.placesRoute(rec, httptest.NewRequest("GET", "/v1/places", nil))
	if rec.Code != 200 {
		t.Fatalf("places: %d %s", rec.Code, rec.Body.String())
	}
	var answer struct {
		Places []struct {
			Icon icon.Grid `json:"icon"`
		} `json:"places"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if len(answer.Places) != 1 || answer.Places[0].Icon.SVG != source {
		t.Fatal("project wire lost SVG")
	}
	row := s.sessionRow(rowInput{item: session.Session{CWD: path}})
	if row.Icon == nil || row.Icon.Svg != source || row.Icon.Cells == nil {
		t.Fatalf("session wire lost SVG or empty cells: %+v", row.Icon)
	}
}

// Saving or mirroring a generated creature must not turn it into a configured
// Project identity. The same Server also exercises setup-cache transitions.
func TestProjectSetupDoesNotCompleteStoredDefaultIcons(t *testing.T) {
	home := placesHome(t)
	state := filepath.Join(home, ".config", "clawdline-next")
	path := filepath.Join(home, "projects", "shop")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.OpenPlaceRegistry(state).Add([]string{path}, time.Now()); err != nil {
		t.Fatal(err)
	}
	registry := filepath.Join(home, ".claude", "project-icons.json")
	if err := os.MkdirAll(filepath.Dir(registry), 0755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"projects": map[string]any{path: map[string]any{"label": "Shop", "hue": 3, "tone": 1, "shape": 8}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registry, data, 0600); err != nil {
		t.Fatal(err)
	}
	icons, err := icon.NewRegistryWithOverrides(state)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: config.Config{Dir: state}, broker: &orchestrator.Broker{Dir: state}, icons: icons}
	read := func(t *testing.T, want string, grid icon.Grid) {
		t.Helper()
		rec := httptest.NewRecorder()
		s.placesRoute(rec, httptest.NewRequest("GET", "/v1/places", nil))
		if rec.Code != 200 {
			t.Fatalf("places returned %d: %s", rec.Code, rec.Body.String())
		}
		var answer struct {
			Places []struct {
				Path  string    `json:"path"`
				Icon  icon.Grid `json:"icon"`
				Setup *struct {
					Icon string `json:"icon"`
				} `json:"setup"`
			} `json:"places"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
			t.Fatal(err)
		}
		for _, p := range answer.Places {
			if p.Path != path {
				continue
			}
			got, _ := json.Marshal(p.Icon)
			expected, _ := json.Marshal(grid)
			if string(got) != string(expected) {
				t.Fatal("the displayed icon changed with its setup classification")
			}
			if p.Setup == nil || p.Setup.Icon != want {
				t.Fatalf("setup = %+v, want icon %q", p.Setup, want)
			}
			return
		}
		t.Fatal("project absent")
	}
	registered := icons.For(path)
	t.Run("registered creature", func(t *testing.T) { read(t, "generated", registered) })
	// This is a creature from a different path: comparing only with this Project's
	// fallback would miss copied and synced defaults.
	copied := icon.Generated("/work/another-project")
	write := func(grid icon.Grid) {
		t.Helper()
		data, err := json.Marshal(map[string]icon.Grid{path: grid})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(state, "project-icon-overrides.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(copied)
	t.Run("copied creature", func(t *testing.T) { read(t, "generated", copied) })
	color := "#123456"
	custom := icon.Grid{Accent: color, Cells: [][]*string{{&color}}}
	write(custom)
	t.Run("custom pixels", func(t *testing.T) { read(t, "override", custom) })
	write(copied)
	t.Run("custom replaced by default", func(t *testing.T) { read(t, "generated", copied) })
	mirrored := copied
	icons.SetMirror(func(cwd string) (icon.Grid, string, bool) { return mirrored, "Shop", cwd == path })
	t.Run("mirrored creature", func(t *testing.T) { read(t, "generated", mirrored) })
	mirrored = icon.Grid{Accent: color, Cells: [][]*string{}, SVG: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 8 8"><circle cx="4" cy="4" r="3" fill="#123456"/></svg>`}
	t.Run("mirrored logo", func(t *testing.T) { read(t, "mirrored", mirrored) })
}
