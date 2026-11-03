package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/icon"
	"github.com/sainteye/clawdline/internal/domain/opsdocs"
	"github.com/sainteye/clawdline/internal/domain/session"
)

type opsFixture struct {
	s     *Server
	p     *pane
	place string
	root  string
}

func opsGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// newOpsFixture is a server with one Project, a git repository, and one
// Session working in it.
func newOpsFixture(t *testing.T) opsFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	state := filepath.Join(home, "state")
	root := filepath.Join(home, "project")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	opsGit(t, root, "init", "-q", "-b", "main")
	st, err := store.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	icons, err := icon.NewRegistryWithOverrides(state)
	if err != nil {
		t.Fatal(err)
	}
	p := &pane{s: session.Session{ID: "pane-4", Backend: session.BackendTmux, Assistant: session.AssistantCodex,
		ConversationID: "10000000-0000-4000-8000-000000000004", State: session.StateIdle, CWD: root}}
	hosts := []ports.TerminalHost{p}
	s := &Server{cfg: config.Config{Dir: state, Port: 7757}, store: st, icons: icons, terminals: hosts,
		inventory: app.Inventory{Terminals: hosts, Screen: p},
		broker:    &orchestrator.Broker{Store: st, Tasks: taskdir.New(state), Dir: state}}
	s.projectReaders().places.Fixture = []string{root}
	places := s.projectReaders().places.List(nil, 40)
	if len(places) != 1 {
		t.Fatalf("fixture places: %v", places)
	}
	return opsFixture{s: s, p: p, place: places[0].ID, root: root}
}

type opsActor int

const (
	opsPerson opsActor = iota
	opsReader
	opsMachine
)

func (f opsFixture) do(t *testing.T, who opsActor, method, path, body, key string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	a := access{verdict: auth.Verdict{Allowed: true, Device: "phone", Caps: auth.NewCaps(auth.Read, auth.Send)}}
	switch who {
	case opsReader:
		a.verdict = auth.Verdict{Allowed: true, Device: "reader", Caps: auth.NewCaps(auth.Read)}
	case opsMachine:
		a = access{machine: true, verdict: auth.Verdict{Allowed: true, Local: true, Device: "this-machine"}}
	}
	req = req.WithContext(context.WithValue(req.Context(), accessKey{}, a))
	rec := httptest.NewRecorder()
	if strings.HasPrefix(path, "/v1/ops") {
		f.s.opsAgentRoute(rec, req)
	} else {
		f.s.projectsRoute(rec, req)
	}
	return rec
}

func (f opsFixture) base() string { return "/v1/projects/" + f.place + "/ops" }

func decodeAs[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("answer %d %s: %v", rec.Code, rec.Body, err)
	}
	return v
}

func TestAnOpsLinkIsAddedOnceUnderItsKey(t *testing.T) {
	f := newOpsFixture(t)
	body := `{"kind":"link","url":"https://docs.google.com/document/d/FAKE-id/edit"}`
	first := f.do(t, opsPerson, http.MethodPost, f.base(), body, "add-1")
	if first.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", first.Code, first.Body)
	}
	created := decodeAs[contract.OpsEntryAnswer](t, first)
	if created.Entry.Type != contract.OpsEntryTypeGoogleDoc || !opsdocs.ValidEntryID(created.Entry.ID) {
		t.Fatalf("entry: %+v", created.Entry)
	}
	again := f.do(t, opsPerson, http.MethodPost, f.base(), body, "add-1")
	if again.Code != http.StatusCreated || again.Header().Get("Idempotent-Replayed") != "true" || again.Body.String() != first.Body.String() {
		t.Fatalf("replay: %d %v %s", again.Code, again.Header(), again.Body)
	}
	other := f.do(t, opsPerson, http.MethodPost, f.base(), `{"kind":"link","url":"https://example.com/x"}`, "add-1")
	if other.Code != http.StatusConflict || codeOf(t, other) != "idempotency_key_reused" {
		t.Fatalf("reused key: %d %s", other.Code, other.Body)
	}
	if bad := f.do(t, opsPerson, http.MethodPost, f.base(), `{"kind":"link","url":"javascript:alert(1)"}`, "add-2"); codeOf(t, bad) != "ops_bad_url" {
		t.Fatalf("bad url: %d %s", bad.Code, bad.Body)
	}
	if missing := f.do(t, opsPerson, http.MethodPost, f.base(), body, ""); missing.Code != http.StatusBadRequest {
		t.Fatalf("no key: %d %s", missing.Code, missing.Body)
	}
	list := decodeAs[contract.OpsList](t, f.do(t, opsReader, http.MethodGet, f.base(), "", ""))
	if len(list.Entries) != 1 || list.Entries[0].ID != created.Entry.ID {
		t.Fatalf("list after a replay and a refusal: %+v", list.Entries)
	}
	if unknown := f.do(t, opsPerson, http.MethodGet, "/v1/projects/no-such-place/ops", "", ""); unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown place: %d %s", unknown.Code, unknown.Body)
	}
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, f.base(), body},
		{http.MethodPatch, f.base() + "/" + created.Entry.ID, `{"title":"x"}`},
		{http.MethodDelete, f.base() + "/" + created.Entry.ID, ""},
		{http.MethodPatch, f.base() + "/setup", `{"git":"exclude"}`},
		{http.MethodPost, f.base() + "/" + created.Entry.ID + "/resolve-request", `{"terminal":"pane-4"}`},
	} {
		if rec := f.do(t, opsReader, tc.method, tc.path, tc.body, "reader-"+tc.method); rec.Code != http.StatusForbidden {
			t.Errorf("read-only device %s %s: %d %s", tc.method, tc.path, rec.Code, rec.Body)
		}
	}
}

func TestANewOpsMarkdownFileAsksAboutGitFirst(t *testing.T) {
	f := newOpsFixture(t)
	body := `{"kind":"markdown","name":"Runbook","content":"# Runbook\n"}`
	if rec := f.do(t, opsPerson, http.MethodPost, f.base(), body, "md-1"); rec.Code != http.StatusConflict || codeOf(t, rec) != "ops_git_choice_required" {
		t.Fatalf("no choice: %d %s", rec.Code, rec.Body)
	}
	if _, err := os.Lstat(filepath.Join(f.root, "clawdline-ops")); !os.IsNotExist(err) {
		t.Fatalf("folder made before the choice: %v", err)
	}
	made := f.do(t, opsPerson, http.MethodPost, f.base(), `{"kind":"markdown","name":"Runbook","content":"# Runbook\n","git":"exclude"}`, "md-2")
	if made.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", made.Code, made.Body)
	}
	entry := decodeAs[contract.OpsEntryAnswer](t, made).Entry
	if entry.Path != "clawdline-ops/Runbook.md" || entry.Type != contract.OpsEntryTypeMarkdown || entry.Title != "Runbook" {
		t.Fatalf("entry: %+v", entry)
	}
	setup := decodeAs[contract.OpsSetup](t, f.do(t, opsPerson, http.MethodGet, f.base()+"/setup", "", ""))
	if !setup.Excluded || !setup.FolderExists || setup.Git != contract.OpsGitStateRepo {
		t.Fatalf("setup: %+v", setup)
	}
	if rec := f.do(t, opsPerson, http.MethodPost, f.base(), body, "md-3"); rec.Code != http.StatusConflict || codeOf(t, rec) != "ops_name_taken" {
		t.Fatalf("same name: %d %s", rec.Code, rec.Body)
	}
	read := decodeAs[contract.OpsRead](t, f.do(t, opsMachine, http.MethodGet, "/v1/ops/"+entry.ID, "", ""))
	if read.Content != "# Runbook\n" || read.Version == "" {
		t.Fatalf("read: %+v", read)
	}
	saved := f.do(t, opsPerson, http.MethodPatch, f.base()+"/"+entry.ID, `{"content":"# Runbook v2\n","expected_version":"`+read.Version+`"}`, "save-1")
	if saved.Code != http.StatusOK || decodeAs[contract.OpsEntryAnswer](t, saved).Version == read.Version {
		t.Fatalf("save: %d %s", saved.Code, saved.Body)
	}
	if stale := f.do(t, opsPerson, http.MethodPatch, f.base()+"/"+entry.ID, `{"content":"x","expected_version":"`+read.Version+`"}`, "save-2"); codeOf(t, stale) != "file_changed" {
		t.Fatalf("stale save: %d %s", stale.Code, stale.Body)
	}
	renamed := f.do(t, opsPerson, http.MethodPatch, f.base()+"/"+entry.ID, `{"rename":"Playbook.md"}`, "rename-1")
	if renamed.Code != http.StatusOK || decodeAs[contract.OpsEntryAnswer](t, renamed).Entry.Path != "clawdline-ops/Playbook.md" {
		t.Fatalf("rename: %d %s", renamed.Code, renamed.Body)
	}
	if deleted := f.do(t, opsPerson, http.MethodDelete, f.base()+"/"+entry.ID, "", "del-1"); deleted.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", deleted.Code, deleted.Body)
	}
	if _, err := os.Stat(filepath.Join(f.root, "clawdline-ops", "Playbook.md")); err != nil {
		t.Fatalf("delete removed the file: %v", err)
	}
}

func TestOnlyTheAskedSessionResolvesALinkAndTheAskIsSendable(t *testing.T) {
	f := newOpsFixture(t)
	created := decodeAs[contract.OpsEntryAnswer](t, f.do(t, opsPerson, http.MethodPost, f.base(),
		`{"kind":"link","url":"https://app.notion.com/p/Fake-0123456789abcdef0123456789abcdef"}`, "add")).Entry
	asked := f.do(t, opsPerson, http.MethodPost, f.base()+"/"+created.ID+"/resolve-request", `{"terminal":"pane-4"}`, "ask-1")
	if asked.Code != http.StatusOK {
		t.Fatalf("resolve request: %d %s", asked.Code, asked.Body)
	}
	request := decodeAs[contract.OpsResolveRequested](t, asked)
	if request.Entry.Status != contract.OpsEntryStatusResolving || request.Entry.Resolve == nil ||
		request.Entry.Resolve.Todo != request.Todo.ID || request.Entry.Resolve.TodoState != contract.OpsTodoStateUnsent {
		t.Fatalf("requested: %+v", request)
	}
	// The existing send route finds the to-do under the Session's conversation.
	sent := httptest.NewRecorder()
	f.s.workV2Route(sent, personWorkV2Request(http.MethodPost, "/v1/work/v2/session-todos/pane-4/"+request.Todo.ID+"/send", `{}`, "send-1"))
	if sent.Code != http.StatusOK {
		t.Fatalf("send: %d %s", sent.Code, sent.Body)
	}
	if effects := f.p.done(); len(effects) != 1 || !strings.Contains(effects[0], "clawdline ops resolve "+created.ID+" --todo "+request.Todo.ID) {
		t.Fatalf("typed: %v", effects)
	}
	resolve := f.base()[:0] + "/v1/ops/" + created.ID + "/resolve"
	if device := f.do(t, opsPerson, http.MethodPost, resolve, `{"todo":"`+request.Todo.ID+`","title":"Runbook"}`, "r-0"); device.Code != http.StatusForbidden || codeOf(t, device) != "ops_resolve_agent_only" {
		t.Fatalf("device resolve: %d %s", device.Code, device.Body)
	}
	if stale := f.do(t, opsMachine, http.MethodPost, resolve, `{"todo":"other-todo","title":"Runbook"}`, "r-1"); stale.Code != http.StatusConflict || codeOf(t, stale) != "resolve_stale" {
		t.Fatalf("stale resolve: %d %s", stale.Code, stale.Body)
	}
	done := f.do(t, opsMachine, http.MethodPost, resolve, `{"todo":"`+request.Todo.ID+`","title":"Incident runbook"}`, "r-2")
	got := decodeAs[contract.OpsEntryAnswer](t, done).Entry
	if done.Code != http.StatusOK || got.Title != "Incident runbook" || got.TitleSource != contract.OpsTitleSourceAgent || got.Status != contract.OpsEntryStatusReady {
		t.Fatalf("resolve: %d %+v", done.Code, got)
	}
	// A person's title is never replaced by a later resolution.
	f.do(t, opsPerson, http.MethodPatch, f.base()+"/"+created.ID, `{"title":"Mine"}`, "t-1")
	again := decodeAs[contract.OpsResolveRequested](t, f.do(t, opsPerson, http.MethodPost, f.base()+"/"+created.ID+"/resolve-request", `{"terminal":"pane-4"}`, "ask-2"))
	needs := f.do(t, opsMachine, http.MethodPost, resolve, `{"todo":"`+again.Todo.ID+`","needs_access":true,"reason":"Share it with the team"}`, "r-3")
	got = decodeAs[contract.OpsEntryAnswer](t, needs).Entry
	if got.Title != "Mine" || got.Status != contract.OpsEntryStatusNeedsAccess || got.AccessNote != "Share it with the team" {
		t.Fatalf("needs access: %+v", got)
	}
	// The Session's CLI reads its Project's list by terminal.
	list := decodeAs[contract.OpsList](t, f.do(t, opsMachine, http.MethodGet, "/v1/ops?session=pane-4", "", ""))
	if list.Place != f.place || len(list.Entries) != 1 {
		t.Fatalf("session list: %+v", list)
	}
}

func TestARenameTheDaemonStoppedInIsSettledAtStart(t *testing.T) {
	f := newOpsFixture(t)
	if err := os.WriteFile(filepath.Join(f.root, "notes.md"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := decodeAs[contract.OpsEntryAnswer](t, f.do(t, opsPerson, http.MethodPost, f.base(), `{"kind":"file","path":"notes.md"}`, "add")).Entry
	// The daemon recorded the rename and made the second link, then stopped.
	if err := f.s.store.WriteWorkV2(context.Background(), func(tx *store.WorkV2Tx) error {
		return tx.PutOpsPendingRename(opsdocs.PendingRename{EntryID: entry.ID, ProjectKey: f.root, From: "notes.md", To: "renamed.md"})
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(f.root, "notes.md"), filepath.Join(f.root, "renamed.md")); err != nil {
		t.Skip("hard links unavailable:", err)
	}
	if err := f.s.SettleOpsRenames(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(f.root, "renamed.md")); !os.IsNotExist(err) {
		t.Fatalf("the unrecorded new name was kept: %v", err)
	}
	if pending, _ := f.s.store.OpsPendingRenames(context.Background()); len(pending) != 0 {
		t.Fatalf("pending left: %+v", pending)
	}
	read := decodeAs[contract.OpsRead](t, f.do(t, opsMachine, http.MethodGet, "/v1/ops/"+entry.ID, "", ""))
	if read.Entry.Path != "notes.md" || read.Content != "n" {
		t.Fatalf("after settling: %+v", read)
	}
}
