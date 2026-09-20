package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/projectlinks"
	"github.com/sainteye/clawdline-go/internal/app"
	"github.com/sainteye/clawdline-go/internal/contract"
	"github.com/sainteye/clawdline-go/internal/domain/capacity"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// The Links sheet is read per session, by the id on its row, and a session
// this daemon cannot place is refused rather than answered with an empty list
// — "we do not know where this session is" and "this project has no addresses"
// are the two sentences this route must never merge.
func TestTheLinksSheetIsReadPerSession(t *testing.T) {
	t.Setenv("CLAWDLINE_NEXT_OWN_SESSIONS", "1")
	home := t.TempDir()
	t.Setenv("HOME", home)
	status := filepath.Join(home, ".claude", "statusline-cache")
	if err := os.MkdirAll(status, 0o755); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	row := map[string]any{"state": "running", "label": "test",
		"started_at": 1.0, "typical_seconds": 60.0, "updated_at": float64(time.Now().Unix())}
	data, _ := json.Marshal(row)
	if err := os.WriteFile(filepath.Join(status,
		"run-"+projectlinks.DirectoryKey(work)+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	reading := session.Inventory{Provenance: "ps", Complete: true, Sessions: []session.Session{
		{ID: "%81", TTY: "ttys081", Assistant: session.AssistantClaude, CWD: work},
		{ID: "%82", TTY: "ttys082", Assistant: session.AssistantClaude},
	}}
	s := &Server{inventory: app.Inventory{Process: todoProcesses{reading}}, links: projectlinks.NewCache()}
	get := func(target, method string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, nil)
		rec := httptest.NewRecorder()
		id, ok := linksPath(req)
		if !ok {
			rec.Code = -1
			return rec
		}
		s.sessionLinksRoute(rec, req, id)
		return rec
	}

	rec := get("/v1/sessions/%2581/links", http.MethodGet)
	var reply contract.ProjectLinksReply
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("a session in a directory: %d %s", rec.Code, rec.Body)
	}
	if reply.ObservedAt == 0 {
		t.Fatal("a reading that is served however old says when it was taken")
	}
	// The temporary directory is not a repository, and that is a different
	// word from having no remote.
	if reply.Repository != contract.ProjectRepositoryNotARepository {
		t.Fatalf("repository = %q", reply.Repository)
	}
	if len(reply.Links) != 1 || reply.Links[0].Kind != "run" || !reply.Links[0].Local {
		t.Fatalf("the local run should be the one row: %s", rec.Body)
	}
	if r := s.linksReading(); !r.Known || r.Used != 1 {
		t.Errorf("the cache row after one read: %+v", r)
	}

	if rec := get("/v1/sessions/%2582/links", http.MethodGet); rec.Code != http.StatusNotFound ||
		!strings.Contains(rec.Body.String(), `"not_found"`) {
		t.Fatalf("a session with no working directory: %d %s", rec.Code, rec.Body)
	}
	if rec := get("/v1/sessions/%2599/links", http.MethodGet); rec.Code != http.StatusNotFound {
		t.Fatalf("a session a complete reading does not hold: %d %s", rec.Code, rec.Body)
	}
	if rec := get("/v1/sessions/%2581/links", http.MethodPost); rec.Code != -1 {
		t.Fatalf("a POST was taken as the read: %d", rec.Code)
	}

	// A server with no projection still answers, and its row is a known zero.
	bare := &Server{inventory: s.inventory}
	if r := bare.linksReading(); !r.Known || r.Used != 0 {
		t.Errorf("no projection: %+v", r)
	}
	if capacity.Default(capacity.CacheSessionLinks) <= 0 {
		t.Error("the links projection has no registered limit")
	}
}

// The `deploy` field is the `links` rows the status line draws a chip from,
// unchanged: same rows, same states, no second computation that could differ.
func TestDeployIsTheSameRowsAsLinks(t *testing.T) {
	links := []contract.ProjectLink{
		{Kind: "site", State: "ok", Label: "site"},
		{Kind: "deploy", State: "running", Label: "deploy"},
		{Kind: "run", State: "running", Label: "test"},
		{Kind: "ci", State: "fail", Label: "ci"},
		{Kind: "server", State: "down", Label: "web"},
	}
	got := deployRows(links)
	if len(got) != 3 {
		t.Fatalf("deploy, ci and run, and nothing else: %+v", got)
	}
	for i, want := range []string{"deploy", "test", "ci"} {
		if got[i].Label != want {
			t.Fatalf("row %d is %q, want %q", i, got[i].Label, want)
		}
	}
	if got[0] != links[1] {
		t.Fatal("the row is carried across whole, not rebuilt")
	}
	if len(deployRows(nil)) != 0 {
		t.Fatal("no links is no chip")
	}
}
