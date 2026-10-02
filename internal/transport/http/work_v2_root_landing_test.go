package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gitadapter "github.com/sainteye/clawdline/internal/adapters/git"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/icon"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// A Board item's landing is the broker's record, read the same way by the item
// and by the landings route, and `item finish` right after a merge finds it
// without waiting for the beat. Every repository here is real: the claims are
// about what git answers.

func boardGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t",
		"GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v %s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func boardCommit(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	boardGit(t, dir, "add", name)
	boardGit(t, dir, "commit", "-q", "-m", name)
	return boardGit(t, dir, "rev-parse", "HEAD")
}

// publishedRepo is a repository at dir on main, with one commit, pushed to a
// bare origin so origin/main resolves.
func publishedRepo(t *testing.T, dir, remote string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	boardGit(t, dir, "init", "-q", "-b", "main")
	boardCommit(t, dir, "first.txt")
	boardGit(t, filepath.Dir(remote), "init", "-q", "--bare", "-b", "main", remote)
	boardGit(t, dir, "remote", "add", "origin", remote)
	boardGit(t, dir, "push", "-q", "-u", "origin", "main")
}

type landingBoard struct {
	s       *Server
	p       *pane
	project string
	nested  string
	// nestedID is the catalog Project the nested repository is registered as.
	nestedID string
	item     app.WorkV2View
}

// newLandingBoard is a Project directory holding a nested git repository with
// its own remote (`cloud/`, registered as a Project of its own), and an item
// of the outer Project owned by an existing Session and moved to merging.
func newLandingBoard(t *testing.T) *landingBoard {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(root, "project")
	publishedRepo(t, project, filepath.Join(root, "project.git"))
	nested := filepath.Join(project, "cloud")
	publishedRepo(t, nested, filepath.Join(root, "cloud.git"))
	project, _ = projects.CanonicalProjectKey(project)
	nested, _ = projects.CanonicalProjectKey(nested)

	p := &pane{s: session.Session{ID: "%4", Backend: session.BackendTmux, Assistant: session.AssistantCodex,
		CWD: project, ConversationID: "10000000-0000-4000-8000-000000000004", State: session.StateIdle}}
	s := paneServer(t, p)
	s.icons = icon.NewRegistry()
	s.broker.Git = gitadapter.New()
	s.workV2().GateSettings = func(context.Context) (app.WorkV2GateSettings, error) {
		return app.WorkV2GateSettings{}, nil
	}
	// The Project readers are kept per state directory; paneServer leaves it
	// empty, the key every other pane test shares, so this fixture gets its own.
	s.cfg.Dir = s.broker.Dir
	s.projectReaders().places.Fixture = []string{project, nested}
	b := &landingBoard{s: s, p: p, project: project, nested: nested}
	projectID := ""
	for id, place := range s.workV2Projects(context.Background()) {
		switch place.Path {
		case project:
			projectID = id
		case nested:
			b.nestedID = id
		}
	}
	if projectID == "" || b.nestedID == "" {
		t.Fatalf("catalog does not hold both repositories: %+v", s.workV2Projects(context.Background()))
	}
	v, err := s.workV2().Create(context.Background(), app.NewWorkV2{ProjectID: projectID, ProjectPath: project,
		Kind: work.KindIssue, Title: "Land it", Description: "A change to land.", Actor: "local"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := s.assignWorkV2(context.Background(), v.Item.ID, "local", v.Item.Version,
		"existing_session", p.s.ID, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []app.AdvanceWorkV2{{Next: work.PhaseImplementing}, {Next: work.PhaseVerifying},
		{Next: work.PhaseMerging, Verification: "go test ./... passed"}} {
		step.ExpectedVersion, step.SessionID, step.Actor = owned.Item.Version, p.s.ConversationID, p.s.ConversationID
		if owned, err = s.workV2().Advance(context.Background(), v.Item.ID, step, nil); err != nil {
			t.Fatalf("advance to %s: %v", step.Next, err)
		}
	}
	b.item = owned
	return b
}

func (b *landingBoard) version(t *testing.T) int64 {
	t.Helper()
	v, err := b.s.workV2().Item(context.Background(), b.item.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	return v.Item.Version
}

// post sends one agent write for the owning Session.
func (b *landingBoard) post(t *testing.T, verb, key string, fields map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	fields["expected_version"], fields["session_id"] = b.version(t), b.p.s.ConversationID
	body, _ := json.Marshal(fields)
	req := httptest.NewRequest(http.MethodPost, "/v1/work/v2/agent/items/"+b.item.Item.ID+"/"+verb, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{
		machine: true, verdict: auth.Verdict{Allowed: true},
	}))
	rec := httptest.NewRecorder()
	b.s.workV2Route(rec, req)
	return rec
}

// itemLandings is the Board item read's `landings`.
func (b *landingBoard) itemRead(t *testing.T) workV2ItemWire {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/work/v2/items/"+b.item.Item.ID, nil)
	rec := httptest.NewRecorder()
	b.s.workV2Route(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("item read: %d %s", rec.Code, rec.Body)
	}
	var out struct {
		Item workV2ItemWire `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("item read: %v %s", err, rec.Body)
	}
	return out.Item
}

// routeLandings is GET /v1/orchestrator/landings?work_id=.
func (b *landingBoard) routeLandings(t *testing.T, id string) (int, contract.ItemLandingList) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/orchestrator/landings?work_id="+id, nil)
	rec := httptest.NewRecorder()
	b.s.brokerLandings(rec, req)
	var out contract.ItemLandingList
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("landings: %v %s", err, rec.Body)
		}
	}
	return rec.Code, out
}

// childTask stores a finished child bound to the item whose isolated branch
// in repo carries one commit, its landing pending — what settlement leaves.
func (b *landingBoard) childTask(t *testing.T, repo, id string) string {
	t.Helper()
	base := boardGit(t, repo, "rev-parse", "HEAD")
	branch := orchestrator.BranchName(id)
	boardGit(t, repo, "branch", branch, base)
	checkout := filepath.Join(t.TempDir(), "child")
	boardGit(t, repo, "worktree", "add", "-q", checkout, branch)
	head := boardCommit(t, checkout, "child-"+id[len(id)-2:]+".txt")
	at := time.Now()
	record, _ := json.Marshal(orchestrator.Record{Protocol: orchestrator.Protocol, ID: id, Kind: "custom",
		Assistant: "claude", Title: "the child", WorkID: b.item.Item.ID, WorkFrom: work.WorkNamed,
		State: orchestrator.StateSuccess, CreatedAt: at, FinishedAt: at, Claims: []string{"child.txt"},
		LeaseScope: orchestrator.LeaseWorktree, Isolation: orchestrator.IsolationWorktree, ProjectDir: repo,
		Repository: repo, TimeoutMinutes: 240,
		Root:     &orchestrator.RootRef{SessionID: b.p.s.ConversationID, Assistant: "codex"},
		Worktree: &orchestrator.Worktree{Repository: repo, Path: checkout, Branch: branch, Base: base, Head: head},
		Landing:  &orchestrator.Landing{State: orchestrator.LandingPending}})
	if _, err := b.s.store.CreateBrokerTask(context.Background(), store.BrokerRow{ID: id, Project: repo,
		Repository: repo, Assistant: "claude", State: "success", CreatedAt: at, SecretHash: "h", Record: record}, nil); err != nil {
		t.Fatal(err)
	}
	return branch
}

func rootLandingRows(t *testing.T, s *Server, workID string) []store.RootLanding {
	t.Helper()
	rows, err := s.store.RootLandings(context.Background(), workID)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// R6: a child's branch merged a moment ago is found by `item finish` itself;
// the beat has not run, and the answer is done, not landing_required.
func TestFinishRightAfterAMergeFindsTheLandingAtOnce(t *testing.T) {
	b := newLandingBoard(t)
	id := "7a5c0000-0000-4000-8000-0000000000a1"
	branch := b.childTask(t, b.project, id)
	boardGit(t, b.project, "merge", "-q", "--no-ff", "-m", "merge the child", branch)

	rec := b.post(t, "finish", "finish-after-merge", map[string]any{"no_deployment_reason": "a library change"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"phase":"done"`) {
		t.Fatalf("finish after merge: %d %s", rec.Code, rec.Body)
	}
	read := b.itemRead(t)
	if len(read.Landings) != 1 || read.Landings[0].ID != id || read.Landings[0].Source != contract.RecordedLandingSourceTask ||
		read.Landings[0].State != "landed" || read.Landings[0].Target != "main" {
		t.Fatalf("item landings = %+v", read.Landings)
	}
	if rows := rootLandingRows(t, b.s, b.item.Item.ID); len(rows) != 0 {
		t.Fatalf("a task landing also wrote root landings: %+v", rows)
	}
}

// A detection git cannot answer leaves the refusal standing, says why, and
// writes nothing; with no task at all the refusal is the plain one.
func TestFinishWithAnUnreadableDetectionKeepsTheRefusalAndSaysWhy(t *testing.T) {
	b := newLandingBoard(t)
	before := b.version(t)
	rec := b.post(t, "finish", "finish-nothing", map[string]any{"no_deployment_reason": "none"})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"landing_required"`) ||
		strings.Contains(rec.Body.String(), "looked just now") {
		t.Fatalf("no landing anywhere: %d %s", rec.Code, rec.Body)
	}

	id := "7a5c0000-0000-4000-8000-0000000000a2"
	b.childTask(t, b.project, id)
	// The repository the task names is gone: git cannot read its branch.
	gone := filepath.Join(t.TempDir(), "gone")
	row, err := b.s.store.BrokerTask(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := orchestrator.Decode(row.Record)
	r.Worktree.Repository = gone
	row.Record, _ = json.Marshal(r)
	if err := b.s.store.SaveBrokerTask(context.Background(), row, nil); err != nil {
		t.Fatal(err)
	}
	rec = b.post(t, "finish", "finish-unreadable", map[string]any{"no_deployment_reason": "none"})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"landing_required"`) ||
		!strings.Contains(rec.Body.String(), "task "+id+": its delivery branch") {
		t.Fatalf("unreadable detection: %d %s", rec.Code, rec.Body)
	}
	if after := b.version(t); after != before {
		t.Fatalf("a refused finish wrote: version %d -> %d", before, after)
	}
}

// R3 and the nested repository: the landing proved in `cloud/` is written as
// one broker record, the event names its id, and the item read and the
// landings route answer the same row. Sent again under a new key after a
// reread, it is refused and no second row appears.
func TestARootLandingInANestedRepositoryReadsTheSameEverywhere(t *testing.T) {
	b := newLandingBoard(t)
	commit := boardCommit(t, b.nested, "cloud-change.txt")
	boardGit(t, b.nested, "push", "-q", "origin", "main")
	landing := map[string]any{"commit": commit, "target": "main", "remote": "origin", "project": b.nestedID}

	// The nested repository is not the item's: named without the Project,
	// the commit does not resolve.
	rec := b.post(t, "phase", "deploy-unnamed", map[string]any{"next": "deploying",
		"landing": map[string]any{"commit": commit, "target": "main", "remote": "origin"}})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "landing_commit_unresolved") {
		t.Fatalf("unnamed nested commit: %d %s", rec.Code, rec.Body)
	}
	rec = b.post(t, "phase", "deploy-once", map[string]any{"next": "deploying", "landing": landing})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"phase":"deploying"`) {
		t.Fatalf("deploy: %d %s", rec.Code, rec.Body)
	}
	rec = b.post(t, "phase", "deploy-twice", map[string]any{"next": "deploying", "landing": landing})
	if rec.Code != http.StatusConflict {
		t.Fatalf("second deploy: %d %s", rec.Code, rec.Body)
	}
	rows := rootLandingRows(t, b.s, b.item.Item.ID)
	if len(rows) != 1 {
		t.Fatalf("root landings = %+v, want one", rows)
	}
	want := rows[0]
	if want.Repository != b.nested || want.Commit != commit || want.Target != "main" || want.Remote != "origin" ||
		want.RemoteCommit != commit {
		t.Fatalf("root landing = %+v", want)
	}

	read := b.itemRead(t)
	code, listed := b.routeLandings(t, b.item.Item.ID)
	if code != http.StatusOK {
		t.Fatalf("landings route: %d", code)
	}
	if len(read.Landings) != 1 || len(listed.Landings) != 1 || read.Landings[0] != listed.Landings[0] {
		t.Fatalf("item %+v and route %+v differ", read.Landings, listed.Landings)
	}
	got := listed.Landings[0]
	if got.ID != want.ID || got.Source != contract.RecordedLandingSourceRoot || got.Commit != commit ||
		got.Target != "main" || got.Remote != "origin" || got.Repository != b.nested {
		t.Fatalf("listed landing = %+v, want row %+v", got, want)
	}
	// The event names the record and keeps no copy of it.
	var deploying string
	for _, e := range read.Events {
		if e.Kind == "item.phase_changed" && strings.Contains(string(e.Payload), `"to":"deploying"`) {
			deploying = string(e.Payload)
		}
	}
	if !strings.Contains(deploying, `"landing_id":"`+want.ID+`"`) || strings.Contains(deploying, `"landing":`) {
		t.Fatalf("deploying event = %s", deploying)
	}

	if code, _ := b.routeLandings(t, "10000000-0000-4000-8000-00000000dead"); code != http.StatusNotFound {
		t.Fatalf("landings of no item: %d, want 404", code)
	}
}

// --no-landing-reason: refused beside a commit before git is asked, refused
// while a bound task still owes its landing, and accepted with neither.
func TestNoLandingReasonIsForWorkWithNoCode(t *testing.T) {
	b := newLandingBoard(t)
	before := b.version(t)
	rec := b.post(t, "phase", "both", map[string]any{"next": "deploying", "no_landing_reason": "docs only",
		"landing": map[string]any{"commit": "HEAD", "target": "main", "remote": "origin"}})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"invalid_landing_evidence"`) {
		t.Fatalf("reason with commit: %d %s", rec.Code, rec.Body)
	}
	rec = b.post(t, "finish", "both-finish", map[string]any{"no_landing_reason": "docs only",
		"no_deployment_reason": "none", "landing": map[string]any{"commit": "HEAD"}})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"invalid_landing_evidence"`) {
		t.Fatalf("finish reason with commit: %d %s", rec.Code, rec.Body)
	}

	id := "7a5c0000-0000-4000-8000-0000000000a3"
	b.childTask(t, b.project, id) // not merged: its landing stays pending
	rec = b.post(t, "phase", "owed", map[string]any{"next": "deploying", "no_landing_reason": "docs only"})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"landing_owed"`) ||
		!strings.Contains(rec.Body.String(), id) {
		t.Fatalf("reason with an owed landing: %d %s", rec.Code, rec.Body)
	}
	if after := b.version(t); after != before {
		t.Fatalf("a refused reason wrote: version %d -> %d", before, after)
	}
}

func TestNoLandingReasonTakesAnItemWithNoCodeToDone(t *testing.T) {
	b := newLandingBoard(t)
	rec := b.post(t, "finish", "no-code", map[string]any{"no_landing_reason": "a decision recorded, no code",
		"no_deployment_reason": "nothing to deploy"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"phase":"done"`) {
		t.Fatalf("finish with no landing reason: %d %s", rec.Code, rec.Body)
	}
	read := b.itemRead(t)
	if read.NoLandingReason != "a decision recorded, no code" || len(read.Landings) != 0 {
		t.Fatalf("item = no_landing_reason %q landings %+v", read.NoLandingReason, read.Landings)
	}
}
