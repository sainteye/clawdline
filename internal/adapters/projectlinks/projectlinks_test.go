package projectlinks

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/sainteye/clawdline/internal/adapters/git"
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

// TestAStateNobodyKnowsStillSaysSomething is the other half of the rule above,
// and the one that was missing.
//
// The dot stays off — that decision is right and is not what this changes.
// What it changes is that the file's own words come out with it. The measured
// case: `{"state":"none","why":"stale-fail"}` sat in a real cache directory
// with a reason its producer wrote down on purpose, and this package read
// `state`, found it unknown, returned nil, and the screen showed a blank cell
// for days.
func TestAStateNobodyKnowsStillSaysSomething(t *testing.T) {
	r, dir := reader(t, remote{url: fixtureRemote}, 1000)
	write(t, dir, "ghrun-"+fixtureRepo+".json",
		map[string]any{"state": "none", "why": "stale-fail", "updated_at": 940.0})
	got := r.Read(context.Background(), "/projects/widgets")
	if len(got.Links) != 0 {
		t.Fatalf("a state nobody knows still draws no dot: %+v", got.Links)
	}
	quiet := got.DeployQuiet
	if quiet == nil {
		t.Fatal("no dot is not no sentence: the file said why and nothing carried it")
	}
	if quiet.Kind != DeployQuietStateNotDrawn {
		t.Fatalf("kind = %q, want %q", quiet.Kind, DeployQuietStateNotDrawn)
	}
	if quiet.State != "none" || quiet.Why != "stale-fail" {
		t.Fatalf("the producer's own words, verbatim: %+v", quiet)
	}
	if quiet.UpdatedAt != 940 {
		t.Fatalf("a reason written three days ago is a poller that stopped: %+v", quiet)
	}
}

// TestAWhyThisReaderDoesNotKnowIsCarriedAnyway. The vocabulary belongs to the
// producer and is not closed: `gh-run-status.py` writes six words today and
// this package maps none of them, on purpose. A reader that kept only the ones
// it recognised would fall silent again on the first new one — which is the
// shape being fixed, not a smaller version of it.
func TestAWhyThisReaderDoesNotKnowIsCarriedAnyway(t *testing.T) {
	for _, why := range []string{"no-gh", "no-branch", "gh-failed", "no-runs",
		"workflow-disabled", "stale-fail", "a word nobody has written yet"} {
		r, dir := reader(t, remote{url: fixtureRemote}, 1000)
		write(t, dir, "ghrun-"+fixtureRepo+".json",
			map[string]any{"state": "none", "why": why})
		quiet := r.Read(context.Background(), "/projects/widgets").DeployQuiet
		if quiet == nil || quiet.Why != why {
			t.Fatalf("why %q was not carried: %+v", why, quiet)
		}
	}
}

// TestFourKindsOfNoDeployRow: the reason `nil` was not enough. Every one of
// these ends in the same empty cell and needs a different thing done about it.
func TestFourKindsOfNoDeployRow(t *testing.T) {
	name := "ghrun-" + fixtureRepo + ".json"

	t.Run("nobody has written one", func(t *testing.T) {
		r, _ := reader(t, remote{url: fixtureRemote}, 1000)
		quiet := r.Read(context.Background(), "/projects/widgets").DeployQuiet
		if quiet == nil || quiet.Kind != DeployQuietNoFile {
			t.Fatalf("an absent file is nobody looking, not no run: %+v", quiet)
		}
		if quiet.State != "" || quiet.Why != "" {
			t.Fatalf("nothing was read, so nothing is quoted: %+v", quiet)
		}
	})

	t.Run("there and not one small object", func(t *testing.T) {
		r, dir := reader(t, remote{url: fixtureRemote}, 1000)
		if err := os.WriteFile(filepath.Join(dir, name), []byte("not json at all"), 0o600); err != nil {
			t.Fatal(err)
		}
		quiet := r.Read(context.Background(), "/projects/widgets").DeployQuiet
		if quiet == nil || quiet.Kind != DeployQuietUnreadable {
			t.Fatalf("written and unreadable is not the same as never written: %+v", quiet)
		}
	})

	t.Run("a state this reader does not draw", func(t *testing.T) {
		r, dir := reader(t, remote{url: fixtureRemote}, 1000)
		write(t, dir, name, map[string]any{"state": "cancel", "url": "https://example.invalid/runs/1"})
		quiet := r.Read(context.Background(), "/projects/widgets").DeployQuiet
		if quiet == nil || quiet.Kind != DeployQuietStateNotDrawn || quiet.State != "cancel" {
			t.Fatalf("the producer's own state word survives being undrawable: %+v", quiet)
		}
		if quiet.Why != "" {
			t.Fatalf("it said no why, and an invented one would be worse: %+v", quiet)
		}
	})

	t.Run("a run with nowhere to go", func(t *testing.T) {
		r, dir := reader(t, remote{url: fixtureRemote}, 1000)
		write(t, dir, name, map[string]any{"state": "ok"})
		got := r.Read(context.Background(), "/projects/widgets")
		if len(got.Links) != 0 {
			t.Fatalf("a deploy with no url is not a row: %+v", got.Links)
		}
		if got.DeployQuiet == nil || got.DeployQuiet.Kind != DeployQuietNoAddress {
			t.Fatalf("and it is not nothing either: %+v", got.DeployQuiet)
		}
		if got.DeployQuiet.State != "ok" {
			t.Fatalf("it reported a verdict and no page: %+v", got.DeployQuiet)
		}
	})

	t.Run("a drawn row is not quiet", func(t *testing.T) {
		r, dir := reader(t, remote{url: fixtureRemote}, 1000)
		write(t, dir, name, map[string]any{"state": "ok", "url": "https://example.invalid/runs/1"})
		got := r.Read(context.Background(), "/projects/widgets")
		if find(got.Links, "deploy") == nil {
			t.Fatalf("a row was expected: %+v", got.Links)
		}
		if got.DeployQuiet != nil {
			t.Fatalf("the row and the reason there is none are never both present: %+v", got.DeployQuiet)
		}
	})
}

// TestNothingLooksForAWorkflowFileWithoutARemoteToNameIt: the quiet answers
// what was found under the repository's name, so where there is no name there
// is nothing to be quiet about — `Repo` already says which kind of nothing
// that was, and a second sentence saying it again would be noise.
func TestNothingLooksForAWorkflowFileWithoutARemoteToNameIt(t *testing.T) {
	for _, g := range []remote{
		{err: git.ErrNoRemote}, {err: git.ErrNotRepository},
		{url: "git@example.invalid:team/widgets.git"}, {err: git.ErrUnavailable},
	} {
		r, _ := reader(t, g, 1000)
		if quiet := r.Read(context.Background(), "/projects/widgets").DeployQuiet; quiet != nil {
			t.Fatalf("nothing named a workflow file, so nothing was looked for: %+v", quiet)
		}
	}
}

// TestADeployRowCarriesItsOwnWhy. The file's `why` is not only for the beat
// that draws nothing: a producer that explains a `fail` gets that sentence
// under the row, in the field the health rows already use.
func TestADeployRowCarriesItsOwnWhy(t *testing.T) {
	r, dir := reader(t, remote{url: fixtureRemote}, 1000)
	write(t, dir, "ghrun-"+fixtureRepo+".json", map[string]any{
		"state": "fail", "url": "https://example.invalid/runs/1", "why": "the build step died",
	})
	row := find(r.Read(context.Background(), "/projects/widgets").Links, "deploy")
	if row == nil || row.Why != "the build step died" {
		t.Fatalf("the producer's sentence belongs under its row: %+v", row)
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
