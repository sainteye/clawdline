package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/persona"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// Opt-in private replay input stays outside this public repository. Only
// aggregate sizes are printed; no catalog content becomes a fixture.
func TestReplayPersonaDisplayCatalog(t *testing.T) {
	directory := os.Getenv("CLAWDLINE_PERSONA_REPLAY_DIR")
	if directory == "" {
		t.Skip("no private replay directory requested")
	}
	raw, err := os.ReadFile(filepath.Join(directory, "personas.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalog contract.PersonaCatalog
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	first := personaDisplay(catalog, "")
	for _, sample := range []struct {
		name  string
		reply contract.PersonaDisplayReply
	}{
		{"persona-display.json", first}, {"persona-unchanged.json", personaDisplay(catalog, first.Version)},
	} {
		body, err := json.Marshal(sample.reply)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, sample.name), body, 0600); err != nil {
			t.Fatal(err)
		}
		t.Logf("sample=%s bytes=%d roles=%d", sample.name, len(body), len(catalog.Personas))
	}
}

// The catalog route lists every persona in the picker's order, with its bot,
// and never the text a session is launched with.
func TestThePersonaCatalogIsListedWithoutItsTexts(t *testing.T) {
	f := newSquadFixture(t)
	status, raw := f.ask(http.MethodGet, "/v1/personas", f.reader, "", "")
	if status != http.StatusOK {
		t.Fatalf("GET: %d %s", status, raw)
	}
	var got contract.PersonaCatalog
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Personas) != len(persona.IDs()) || len(got.Personas) < 16 || got.License != "MIT" {
		t.Fatalf("%d personas, licence %q", len(got.Personas), got.License)
	}
	for i, p := range got.Personas {
		if len(p.Teams) == 0 {
			t.Errorf("%s: no team", p.ID)
		}
		if p.ID != persona.IDs()[i] || p.Name.ZhHant == "" || p.Summary.En == "" || len(p.Icon.Cells) != 7 ||
			(p.Source != persona.Original && !strings.Contains(p.Source, persona.UpstreamCommit)) {
			t.Errorf("%d: %+v", i, p)
		}
	}
	if strings.Contains(raw, "Clawdline persona") || strings.Contains(raw, "never overrides") {
		t.Fatal("the injected text is on the wire")
	}
	var wire map[string]any
	_ = json.Unmarshal([]byte(raw), &wire)
	first := wire["personas"].([]any)[0].(map[string]any)
	if _, ok := first["name"].(map[string]any)["zh-Hant"]; !ok {
		t.Fatalf("the names are not keyed by language: %v", first["name"])
	}

	status, _ = f.ask(http.MethodPost, "/v1/personas", f.reader, "{}", "")
	if status != http.StatusMethodNotAllowed {
		t.Fatalf("POST: %d", status)
	}
}

func TestPersonaDisplayRevalidatesTheWholeProjection(t *testing.T) {
	f := newSquadFixture(t)
	status, raw := f.ask(http.MethodGet, "/v1/personas?view=display-v1", f.reader, "", "")
	if status != http.StatusOK {
		t.Fatalf("display: %d %s", status, raw)
	}
	var first contract.PersonaDisplayReply
	if err := json.Unmarshal([]byte(raw), &first); err != nil {
		t.Fatal(err)
	}
	if first.View != "display-v1" || !personaCatalogVersion(first.Version) || first.Unchanged {
		t.Fatalf("bad receipt: %+v", first)
	}
	if first.Catalog == nil {
		t.Fatal("missing display catalog")
	}
	display := *first.Catalog
	full := personaCatalog()
	if len(display.Personas) != len(full.Personas) || display.License != "" {
		t.Fatal("display lost roles or retained provenance")
	}
	for i, p := range display.Personas {
		if p.ID != full.Personas[i].ID || p.Name != full.Personas[i].Name || p.Summary != full.Personas[i].Summary || len(p.Icon.Cells) != 7 || len(p.Teams) == 0 || p.Source != "" {
			t.Fatalf("lost display metadata at %d", i)
		}
	}
	status, raw = f.ask(http.MethodGet, "/v1/personas?view=display-v1&version="+first.Version, f.reader, "", "")
	var receipt contract.PersonaDisplayReply
	_ = json.Unmarshal([]byte(raw), &receipt)
	if status != 200 || !receipt.Unchanged || receipt.Version != first.Version || receipt.Catalog != nil {
		t.Fatalf("unchanged: %d %s", status, raw)
	}
	for _, path := range []string{"/v1/personas?view=unknown", "/v1/personas?version=" + first.Version, "/v1/personas?view=display-v1&version=wrong"} {
		status, _ = f.ask(http.MethodGet, path, f.reader, "", "")
		if status != http.StatusBadRequest {
			t.Fatalf("%s: %d", path, status)
		}
	}
	// A private role addition, rename, icon update or removal changes the
	// projection version even when built-in roles did not change.
	full.Personas = append(full.Personas, contract.Persona{ID: "private-role", Name: contract.PersonaNames{En: "Private role"}})
	added := personaDisplay(full, first.Version)
	if added.Unchanged || added.Version == first.Version {
		t.Fatal("addition reused the old version")
	}
	full.Personas[len(full.Personas)-1].Name.En = "Renamed role"
	if personaDisplay(full, added.Version).Unchanged {
		t.Fatal("rename reused the old version")
	}
	full.Personas = full.Personas[:len(full.Personas)-1]
	if personaDisplay(full, added.Version).Unchanged {
		t.Fatal("removal reused the old version")
	}
}

// `…/as/<persona>` is read off a start or a resume before anything opens: a
// name the catalog does not have is refused by code, and `as` where a model
// goes is still only a model that does not exist.
func TestAStartOrResumePathMayEndWithAPersona(t *testing.T) {
	s := &Server{}
	post := func(path string) (int, map[string]any) {
		w := httptest.NewRecorder()
		s.placeRoute(w, httptest.NewRequest(http.MethodPost, path, nil))
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		return w.Code, body
	}
	code := func(body map[string]any) any {
		if e, ok := body["error"].(map[string]any); ok {
			return e["code"]
		}
		return body["code"]
	}
	for _, path := range []string{
		"/v1/places/p1/start/claude/as/wizard",
		"/v1/places/p1/start/codex/gpt-5/as/Architect",
		"/v1/places/p1/resume/claude/0f1e2d3c-0000-4000-8000-000000000001/as/wizard",
	} {
		if status, body := post(path); status != http.StatusBadRequest || code(body) != "unknown_persona" {
			t.Errorf("%s: %d %v", path, status, body)
		}
	}
	for _, path := range []string{
		"/v1/places/p1/start/claude/as",
		// A resume names its assistant before a persona can follow it.
		"/v1/places/p1/resume/0f1e2d3c-0000-4000-8000-000000000001/as/architect",
		"/v1/places/p1/start/claude/opus/extra/as/architect",
	} {
		if status, _ := post(path); status != http.StatusNotFound && status != http.StatusNotImplemented {
			t.Errorf("%s: %d, want 404 or 501", path, status)
		}
	}
	// A known one is taken off the path and the request goes on to the
	// writing gate, which this bare server has nobody past.
	for _, path := range []string{
		"/v1/places/p1/start/claude/as/architect",
		"/v1/places/p1/start/claude/opus/as/security",
		"/v1/places/p1/resume/codex/0f1e2d3c-0000-4000-8000-000000000001/as/backend",
	} {
		if status, body := post(path); status == http.StatusNotFound || code(body) == "unknown_persona" {
			t.Errorf("%s: %d %v", path, status, body)
		}
	}
}

func TestASessionRowSaysWhichPersonaItWasLaunchedAs(t *testing.T) {
	s := &Server{}
	item := session.Session{ID: "%4", Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		Persona: "reality-checker"}
	if got := wire(t, s.sessionRow(rowInput{item: item}).SessionRow)["persona"]; got != "reality-checker" {
		t.Fatalf("persona = %v", got)
	}
	item.Persona = ""
	if _, ok := wire(t, s.sessionRow(rowInput{item: item}).SessionRow)["persona"]; ok {
		t.Fatal("a row launched as nobody names a persona")
	}
}

// A dispatched task's row names the persona its brief asked for.
func TestATaskRowNamesItsPersona(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := &Server{broker: &orchestrator.Broker{Store: st}, store: st}
	row := s.brokerTaskRow(context.Background(), orchestrator.Record{ID: "t", Assistant: "codex", Persona: "security"})
	if got := wire(t, row)["persona"]; got != "security" {
		t.Fatalf("persona = %v", got)
	}
	row = s.brokerTaskRow(context.Background(), orchestrator.Record{ID: "t", Assistant: "codex"})
	if _, ok := wire(t, row)["persona"]; ok {
		t.Fatal("a task with no persona names one")
	}
}

// A restored session comes back as the persona it was recorded with while
// this build has it, and as nobody once a build has dropped it.
func TestARestoredPersonaIsOneThisBuildHas(t *testing.T) {
	for in, want := range map[string]string{"architect": "architect", "": "", "retired-persona": ""} {
		if got := restorablePersona(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

func TestPersonaDisplayVersionMatchesEscapedUnicode(t *testing.T) {
	catalog := contract.PersonaCatalog{Personas: []contract.Persona{{ID: "example", Name: contract.PersonaNames{En: "A<&>\u2028\u2029 \"quoted\"\n", ZhHant: "角色"}}}}
	if got := personaDisplay(catalog, "").Version; got != "9b9953cf5eeb72ef57532b2acd4bd71d080352606c9234f9def774c48b86bf88" {
		t.Fatalf("cross-viewer digest: %s", got)
	}
}
