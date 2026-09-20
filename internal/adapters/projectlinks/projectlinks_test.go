package projectlinks

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/sainteye/clawdline-go/internal/adapters/git"
)

// The fixtures here are invented, and deliberately: this repository is public,
// and a real project's name, remote or path is the person's.

const (
	fixtureRepo   = "example-widgets"
	fixtureRemote = "git@github.com:example/widgets.git"
)

// remote is a git that answers whatever the test says, so no repository and no
// `git` on PATH is needed.
type remote struct {
	url string
	err error
}

func (r remote) Remote(context.Context, string) (string, error) { return r.url, r.err }

// reader is a Reader over a temporary status directory.
func reader(t *testing.T, g Runner, now float64) (*Reader, string) {
	t.Helper()
	dir := t.TempDir()
	return &Reader{
		StatusDir: dir,
		Home:      t.TempDir(),
		Git:       g,
		Now:       func() time.Time { return time.Unix(int64(now), 0) },
	}, dir
}

func write(t *testing.T, dir, name string, row map[string]any) {
	t.Helper()
	data, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func find(rows []Link, kind string) *Link {
	for i := range rows {
		if rows[i].Kind == kind {
			return &rows[i]
		}
	}
	return nil
}

// TestUnreadableAndAbsentAreDifferentWords is the whole point of the
// repository answer: four ways of having no deploy row, and a screen that can
// tell them apart. A reader that answered "no links" to all of them would tell
// somebody their project has no deploy when what happened is that git is
// wedged.
func TestUnreadableAndAbsentAreDifferentWords(t *testing.T) {
	cases := []struct {
		name       string
		git        remote
		want       Repo
		unreadable Unreadable
	}{
		{"a GitHub remote", remote{url: fixtureRemote}, RepoGitHub, ""},
		{"a remote somewhere else", remote{url: "git@example.invalid:team/widgets.git"}, RepoRemoteNotGitHub, ""},
		{"a repository with no origin", remote{err: git.ErrNoRemote}, RepoNoRemote, ""},
		{"not a repository at all", remote{err: git.ErrNotRepository}, RepoNotRepository, ""},
		{"no git on this machine", remote{err: git.ErrUnavailable}, RepoUnreadable, UnreadableMissing},
		{"git did not answer in time", remote{err: git.ErrTimedOut}, RepoUnreadable, UnreadableTimedOut},
		{"git refused", remote{err: git.ErrFailed}, RepoUnreadable, UnreadableFailed},
		{"git said more than this reads", remote{err: git.ErrTooLarge}, RepoUnreadable, UnreadableTooLarge},
		{"an error nobody named", remote{err: errors.New("something else")}, RepoUnreadable, UnreadableFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, _ := reader(t, c.git, 1000)
			got := r.Read(context.Background(), "/projects/widgets")
			if got.Repo != c.want {
				t.Fatalf("repository = %q, want %q", got.Repo, c.want)
			}
			if got.Unreadable != c.unreadable {
				t.Fatalf("unreadable = %q, want %q", got.Unreadable, c.unreadable)
			}
			if len(got.Links) != 0 {
				t.Fatalf("no status file was written, so there is nothing to link to: %+v", got.Links)
			}
		})
	}
}

// TestOnlyAGitHubRemoteNamesAWorkflowFile: the deploy row's file is named
// after the remote, so the same file is invisible to a project whose remote is
// elsewhere. That is "there is none", not "could not read".
func TestOnlyAGitHubRemoteNamesAWorkflowFile(t *testing.T) {
	r, dir := reader(t, remote{url: fixtureRemote}, 1000)
	write(t, dir, "ghrun-"+fixtureRepo+".json", map[string]any{
		"state": "running", "url": "https://example.invalid/runs/1",
		"started_at": 900.0, "typical_seconds": 120.0, "label": "deploy",
	})
	got := r.Read(context.Background(), "/projects/widgets")
	row := find(got.Links, "deploy")
	if row == nil {
		t.Fatalf("a GitHub remote should find its workflow file: %+v", got)
	}
	if row.StartedAt != 900 || row.TypicalSeconds != 120 {
		t.Fatalf("a running row carries what the bar is drawn from: %+v", row)
	}

	elsewhere, _ := reader(t, remote{url: "git@example.invalid:team/widgets.git"}, 1000)
	elsewhere.StatusDir = dir
	if rows := elsewhere.Read(context.Background(), "/projects/widgets").Links; len(rows) != 0 {
		t.Fatalf("nothing names a workflow run for a remote that is not GitHub: %+v", rows)
	}
}

// TestAStateNobodyKnowsDrawsNothing is the always-wrong red mark: of the
// fifteen workflow files on the Swift app's machine, twelve were exactly
// `{"state":"none"}`.
func TestAStateNobodyKnowsDrawsNothing(t *testing.T) {
	for _, state := range []string{"none", "unknown", "", "queued"} {
		r, dir := reader(t, remote{url: fixtureRemote}, 1000)
		write(t, dir, "ghrun-"+fixtureRepo+".json",
			map[string]any{"state": state, "url": "https://example.invalid/runs/1"})
		if rows := r.Read(context.Background(), "/projects/widgets").Links; len(rows) != 0 {
			t.Fatalf("state %q should draw nothing, drew %+v", state, rows)
		}
	}
}

// TestADeployWithNowhereToGoIsNotARow: a workflow run without its page is a
// chip that does nothing.
func TestADeployWithNowhereToGoIsNotARow(t *testing.T) {
	r, dir := reader(t, remote{url: fixtureRemote}, 1000)
	write(t, dir, "ghrun-"+fixtureRepo+".json", map[string]any{"state": "ok"})
	if rows := r.Read(context.Background(), "/projects/widgets").Links; len(rows) != 0 {
		t.Fatalf("a deploy with no url is not a row: %+v", rows)
	}
}

// TestALocalRunIsARowWithNoAddress, and the liveness ceiling that keeps a
// killed one out of the bar for ever.
func TestALocalRunIsARowWithNoAddress(t *testing.T) {
	cwd := "/projects/widgets"
	key := DirectoryKey(cwd)

	fresh, dir := reader(t, remote{err: git.ErrNoRemote}, 1000)
	write(t, dir, "run-"+key+".json", map[string]any{
		"state": "running", "label": "test", "phase": "compiling",
		"started_at": 940.0, "typical_seconds": 60.0, "updated_at": 990.0,
	})
	row := find(fresh.Read(context.Background(), cwd).Links, "run")
	if row == nil || row.URL != "" || !row.Local || row.Phase != "compiling" {
		t.Fatalf("a local run is a row with nowhere to go: %+v", row)
	}

	// The same file, read well past its default ceiling.
	stale, _ := reader(t, remote{err: git.ErrNoRemote}, 1000+defaultStaleAfter+1)
	stale.StatusDir = dir
	if rows := stale.Read(context.Background(), cwd).Links; len(rows) != 0 {
		t.Fatalf("a running row nobody has touched for stale_after is gone: %+v", rows)
	}

	// A finished row is never stale: `ok` is a verdict, not a claim about
	// something still moving.
	write(t, dir, "run-"+key+".json", map[string]any{"state": "ok", "label": "test"})
	if find(stale.Read(context.Background(), cwd).Links, "run") == nil {
		t.Fatal("a finished run is an answer whatever the clock says")
	}
}

// TestARunningRowThatWillNotSayWhenIsMalformed.
func TestARunningRowThatWillNotSayWhenIsMalformed(t *testing.T) {
	cwd := "/projects/widgets"
	r, dir := reader(t, remote{err: git.ErrNoRemote}, 1000)
	write(t, dir, "run-"+DirectoryKey(cwd)+".json",
		map[string]any{"state": "running", "started_at": 900.0})
	if rows := r.Read(context.Background(), cwd).Links; len(rows) != 0 {
		t.Fatalf("running with no updated_at is drawn as nothing: %+v", rows)
	}
}

// TestHealthKeepsBothVocabularies: the dot's colour and the receipt's own
// word are different facts, and a component list replaces the overall chip.
func TestHealthKeepsBothVocabularies(t *testing.T) {
	cwd := "/projects/widgets"
	r, dir := reader(t, remote{err: git.ErrNoRemote}, 1000)
	write(t, dir, "health-"+DirectoryKey(cwd)+".json", map[string]any{
		"state": "ok", "url": "https://example.invalid", "label": "overall",
		"components": []any{
			map[string]any{"label": "site", "state": "not_deployed", "url": "https://example.invalid",
				"reason": "no_build_found"},
			map[string]any{"label": "build", "state": "unhealthy", "url": "https://example.invalid/ci",
				"kind": "ci"},
			map[string]any{"label": "nothing", "state": "unknown", "url": "https://example.invalid/x"},
		},
	})
	rows := r.Read(context.Background(), cwd).Links
	if len(rows) != 2 {
		t.Fatalf("a component list replaces the overall chip, and `unknown` draws nothing: %+v", rows)
	}
	if rows[0].State != "down" || rows[0].Status != "not deployed" || rows[0].Why != "no build found" {
		t.Fatalf("the dot is a colour and the receipt keeps its word: %+v", rows[0])
	}
	if rows[1].State != "fail" || rows[1].Kind != "ci" {
		t.Fatalf("a receipt that names its kind keeps it: %+v", rows[1])
	}
}

// TestHealthTakesItsEndpointFromTheRegistry: the result is a fact about this
// minute, the endpoint a fact about the project, and they are in two files.
func TestHealthTakesItsEndpointFromTheRegistry(t *testing.T) {
	cwd := "/projects/widgets"
	r, dir := reader(t, remote{err: git.ErrNoRemote}, 1000)
	r.Registry = func(string) map[string]any {
		return map[string]any{"health": map[string]any{
			"url": "https://example.invalid", "label": "site"}}
	}
	write(t, dir, "health-"+DirectoryKey(cwd)+".json", map[string]any{"state": "ok"})
	rows := r.Read(context.Background(), cwd).Links
	if len(rows) != 1 || rows[0].URL != "https://example.invalid" || rows[0].Label != "site" {
		t.Fatalf("the registry supplies what this minute's reading does not: %+v", rows)
	}
}

// TestAServerRowIsOnlyAsSureAsWhatWasAsked. Nothing here runs a project's
// commands, so a process with no declared port has no state at all, and says
// which kind of nothing it is.
func TestAServerRowIsOnlyAsSureAsWhatWasAsked(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "widgets")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	spec := map[string]any{
		"name": "widgets", "status": "./status.sh",
		"processes": []any{
			map[string]any{"name": "web", "port": 4310},
			map[string]any{"name": "api", "port": 4311},
			map[string]any{"name": "docs", "url": "http://127.0.0.1:4312"},
		},
	}
	data, _ := json.Marshal(spec)
	if err := os.WriteFile(filepath.Join(root, ".devstack.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	r, _ := reader(t, remote{err: git.ErrNotRepository}, 1000)
	r.Home = home
	r.Probe = func(_ context.Context, port int) bool { return port == 4310 }
	rows := r.Read(context.Background(), root).Links
	if len(rows) != 3 {
		t.Fatalf("one row per process with somewhere to go: %+v", rows)
	}
	byName := map[string]Link{}
	for _, row := range rows {
		byName[row.Label] = row
	}
	if byName["web"].State != "ok" || byName["web"].URL != "http://127.0.0.1:4310" {
		t.Fatalf("a port that answered is ok: %+v", byName["web"])
	}
	if byName["api"].State != "down" {
		t.Fatalf("a port that did not answer is down: %+v", byName["api"])
	}
	docs := byName["docs"]
	if docs.State != "" || docs.UnknownReason != "status_not_run" {
		t.Fatalf("nothing asked, so no dot and a reason: %+v", docs)
	}
	if !docs.Local {
		t.Fatal("a dev stack is on this machine's own network")
	}
}

// TestALocalRunWinsTheChip is the Swift app's own rule: one chip, and it is
// spent on the thing happening in front of the person.
func TestALocalRunWinsTheChip(t *testing.T) {
	reading := Reading{Links: []Link{
		{Kind: "ci", State: "running", Label: "ci"},
		{Kind: "deploy", State: "running", Label: "deploy"},
		{Kind: "run", State: "running", Label: "test"},
		{Kind: "server", State: "running", Label: "web"},
	}}
	if got := reading.Deploying(); got == nil || got.Label != "test" {
		t.Fatalf("a local run wins: %+v", got)
	}
	reading.Links = reading.Links[:2]
	if got := reading.Deploying(); got == nil || got.Label != "ci" {
		t.Fatalf("otherwise the first of the other two: %+v", got)
	}
	reading.Links = []Link{{Kind: "deploy", State: "ok"}, {Kind: "server", State: "running"}}
	if got := reading.Deploying(); got != nil {
		t.Fatalf("a finished deploy and a server are not the chip: %+v", got)
	}
}

// TestARealGitAnswerEndsInANewline, which is how this was found and not how
// it was reasoned about.
//
// `git remote get-url origin` prints its answer and a newline. The first fake
// git here returned a clean string, so every test passed while the real walk
// looked for `ghrun-example-widgets.git-.json` — the `.git` suffix does not
// strip when there is a newline after it — and found no deploy row on a
// machine that had one.
func TestARealGitAnswerEndsInANewline(t *testing.T) {
	r, dir := reader(t, remote{url: fixtureRemote + "\n"}, 1000)
	write(t, dir, "ghrun-"+fixtureRepo+".json",
		map[string]any{"state": "ok", "url": "https://example.invalid/runs/1"})
	got := r.Read(context.Background(), "/projects/widgets")
	if got.Repo != RepoGitHub {
		t.Fatalf("repository = %q", got.Repo)
	}
	if find(got.Links, "deploy") == nil {
		t.Fatalf("the workflow file is named after the trimmed remote: %+v", got.Links)
	}
}

// TestGitHubRepoNamesTheFile, in both spellings git hands back.
func TestGitHubRepoNamesTheFile(t *testing.T) {
	cases := map[string]string{
		"git@github.com:example/widgets.git\n":     fixtureRepo,
		"  https://github.com/example/widgets  ":   fixtureRepo,
		"git@github.com:example/widgets.git":       fixtureRepo,
		"https://github.com/example/widgets.git":   fixtureRepo,
		"https://github.com/example/widgets":       fixtureRepo,
		"ssh://git@github.com/example/widgets.git": fixtureRepo,
		"git@example.invalid:example/widgets.git":  "",
		"https://github.com/example":               "",
		"":                                         "",
	}
	for url, want := range cases {
		if got := GitHubRepo(url); got != want {
			t.Fatalf("GitHubRepo(%q) = %q, want %q", url, got, want)
		}
	}
}

// TestAFileTooLargeIsNothingRatherThanHalfOfSomething.
func TestAFileTooLargeIsNothingRatherThanHalfOfSomething(t *testing.T) {
	cwd := "/projects/widgets"
	r, dir := reader(t, remote{err: git.ErrNoRemote}, 1000)
	padding := make([]byte, maxStatusBytes+1)
	for i := range padding {
		padding[i] = ' '
	}
	body := append([]byte(`{"state":"ok","url":"https://example.invalid"}`), padding...)
	if err := os.WriteFile(filepath.Join(dir, "health-"+DirectoryKey(cwd)+".json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	if rows := r.Read(context.Background(), cwd).Links; len(rows) != 0 {
		t.Fatalf("a file past the read bound is nothing: %+v", rows)
	}
}
