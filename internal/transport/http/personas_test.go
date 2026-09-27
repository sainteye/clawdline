package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/persona"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// The catalog route lists every persona in the picker's order, with its bot,
// and never the text a session is launched with.
func TestThePersonaCatalogIsListedWithoutItsTexts(t *testing.T) {
	s := &Server{}
	w := httptest.NewRecorder()
	s.personasRoute(w, httptest.NewRequest(http.MethodGet, "/v1/personas", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", w.Code, w.Body)
	}
	var got contract.PersonaCatalog
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Personas) != 8 || got.License != "MIT" {
		t.Fatalf("%d personas, licence %q", len(got.Personas), got.License)
	}
	for i, p := range got.Personas {
		if p.ID != persona.IDs()[i] || p.Name.ZhHant == "" || p.Summary.En == "" || len(p.Icon.Cells) != 7 ||
			!strings.Contains(p.Source, persona.UpstreamCommit) {
			t.Errorf("%d: %+v", i, p)
		}
	}
	if strings.Contains(w.Body.String(), "Clawdline persona") || strings.Contains(w.Body.String(), "never overrides") {
		t.Fatal("the injected text is on the wire")
	}
	var wire map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &wire)
	first := wire["personas"].([]any)[0].(map[string]any)
	if _, ok := first["name"].(map[string]any)["zh-Hant"]; !ok {
		t.Fatalf("the names are not keyed by language: %v", first["name"])
	}

	w = httptest.NewRecorder()
	s.personasRoute(w, httptest.NewRequest(http.MethodPost, "/v1/personas", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: %d", w.Code)
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
		if status, _ := post(path); status != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", path, status)
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
