package projectlinks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/devstack"
	git "github.com/sainteye/clawdline/internal/adapters/git"
)

// MaxLinks bounds the rows one answer carries (page). Past it the rest are not
// in the list and the answer says it was cut: a sheet nobody scrolls that far
// down is not made more useful by being unbounded.
const MaxLinks = 64

// Link is one address a project has.
//
// The fields are the Swift app's row, key for key (`ProjectArtifact.swift:9`),
// because the console's Links sheet and status-line chip read them directly.
type Link struct {
	Label string
	URL   string
	// Kind is `deploy`, `run`, `server` or — for a health row — whatever kind
	// the receipt named itself, `site` when it named none. `ci` reaches the
	// status-line chip this way, which is why the chip's filter names three
	// kinds and this type does not enumerate them.
	Kind   string
	State  string
	Status string
	Why    string
	// Local says the address is on this machine's own network, so a paired
	// phone cannot follow it.
	Local bool
	// StartedAt and TypicalSeconds are only on a `running` row, and only
	// where the producer wrote them: they are what a progress bar is drawn
	// from, and a bar drawn from a missing number is a bar that lies.
	StartedAt      float64
	TypicalSeconds float64
	Phase          string
	// UnknownReason says why State is empty on a server row: `status_not_run`
	// when only the project's own status command could tell and this daemon
	// does not run one, `nothing_declared` when the file declares no port to
	// ask. Empty state with no reason would be a row whose dot means nothing.
	UnknownReason string
}

// Repo is what the one git read found, and it has four answers rather than one
// because **"could not read" and "there is none" are different words.**
//
// A deploy row is a file named after the repository's GitHub remote. So a
// directory that is not a repository, a repository with no `origin`, an
// `origin` that is not on GitHub and a git that would not answer all produce no
// deploy row — and only the last one leaves it unknown whether there was one to
// produce. Collapsing them into "no links" tells somebody their project has no
// deploy when what happened is that git is wedged.
type Repo string

const (
	// RepoGitHub is an `origin` on GitHub: a workflow file could be named.
	RepoGitHub Repo = "github"
	// RepoRemoteNotGitHub is an `origin` somewhere else. Nothing names its
	// workflow runs.
	RepoRemoteNotGitHub Repo = "remote_not_github"
	// RepoNoRemote is a repository with nobody to push to.
	RepoNoRemote Repo = "no_remote"
	// RepoNotRepository is a directory that is not a repository at all.
	RepoNotRepository Repo = "not_a_repository"
	// RepoUnreadable is git not answering. Unreadable says which way.
	RepoUnreadable Repo = "unreadable"
)

// Unreadable is which way a git read failed. Four words rather than one
// because the thing to do about each is different: install git, wait, look at
// the repository.
type Unreadable string

const (
	UnreadableMissing  Unreadable = "git_missing"
	UnreadableTimedOut Unreadable = "git_timeout"
	UnreadableFailed   Unreadable = "git_failed"
	UnreadableTooLarge Unreadable = "git_answer_too_large"
)

// Reading is one walk of one working directory.
type Reading struct {
	Links []Link
	// Repo is the repository question, answered even when it produced no row.
	Repo Repo
	// Unreadable is set only when Repo is RepoUnreadable.
	Unreadable Unreadable
	// DeployQuiet is the *other* half of the repository question, and the
	// half that was missing. Repo says whether a workflow run could be named
	// at all; this says what was found under that name when no deploy row
	// came out of it — including the producer's own reason, which until now
	// this package read past. Set only with RepoGitHub, and only when there
	// is no deploy row.
	DeployQuiet *DeployQuiet
	// Truncated says rows were left out of Links.
	Truncated bool
}

// Deploying is the row the status-line chip would draw, by the Swift app's own
// rule (`input/status-line.js` `runningDeploy`): a running `run`, `deploy` or
// `ci`, and **a local run wins over a deploy** — both are "something is
// happening", but only one of them is happening on the machine in front of the
// person and holding up the next thing they were going to do.
//
// It is here and not only in the console so that a test can pin the rule
// without a browser, and so that a second reader of the same answer cannot
// choose differently.
func (r Reading) Deploying() *Link {
	var first *Link
	for i := range r.Links {
		row := &r.Links[i]
		if row.State != "running" {
			continue
		}
		switch row.Kind {
		case "run":
			return row
		case "deploy", "ci":
			if first == nil {
				first = row
			}
		}
	}
	return first
}

// Runner is how this walk asks git for a remote, so a test needs no
// repository on disk and no `git` on PATH.
type Runner interface {
	Remote(ctx context.Context, cwd string) (string, error)
}

// Reader is one walk's inputs. Everything it touches is a local file, a local
// directory or a port on this machine.
type Reader struct {
	// StatusDir is where the status line keeps its cache. Empty is
	// ~/.claude/statusline-cache.
	StatusDir string
	Home      string
	Git       Runner
	// Probe asks whether anything is listening on a declared port. Nil is the
	// real one.
	Probe devstack.Prober
	// Registry answers the health endpoint a project registered, by working
	// directory. Nil is none, and then a health file must carry its own URL.
	Registry func(cwd string) map[string]any
	// Now is the clock the run row's liveness ceiling is measured against.
	Now func() time.Time
}

// NewReader is the production reader.
func NewReader(home string, statusDir func() string) *Reader {
	dir := ""
	if statusDir != nil {
		dir = statusDir()
	}
	return &Reader{StatusDir: dir, Home: home, Git: git.New(), Now: time.Now}
}

func (r *Reader) now() float64 {
	if r.Now == nil {
		return float64(time.Now().UnixNano()) / 1e9
	}
	return float64(r.Now().UnixNano()) / 1e9
}

func (r *Reader) statusDir() string {
	if r.StatusDir != "" {
		return r.StatusDir
	}
	return filepath.Join(r.Home, ".claude", "statusline-cache")
}

// Read is one walk: the addresses this working directory has, in the order
// somebody would want them — what is up, what is deploying, what is running
// here, what is listening here.
//
// The one subprocess is the remote read, and it is the reason this answer is
// held rather than taken on every request (cache.go).
func (r *Reader) Read(ctx context.Context, cwd string) Reading {
	var out Reading
	if cwd == "" {
		out.Repo = RepoNotRepository
		return out
	}
	repo, state, why := r.remote(ctx, cwd)
	out.Repo, out.Unreadable = state, why

	now := r.now()
	var registry map[string]any
	if r.Registry != nil {
		registry = r.Registry(cwd)
	}
	status := ReadStatus(r.statusDir(), cwd, repo, now)
	if registry != nil {
		// The registry's own `health` row is where a project writes the
		// endpoint; the cache file is where this minute's result lands.
		if h, ok := registry["health"].(map[string]any); ok {
			registry = h
		}
		if status.Health != nil && status.Health.URL == "" {
			status.Health.URL = address(str(registry, "url"))
		}
		if status.Health != nil && status.Health.Label == "health" {
			if named := label(str(registry, "label"), "health"); named != "health" {
				status.Health.Label = named
			}
		}
	}
	out.Truncated = status.Truncated

	// Health first: what is up is the question somebody opened this to answer.
	health := status.Components
	if len(health) == 0 && status.Health != nil {
		health = []Health{*status.Health}
	}
	for _, h := range health {
		// A check with nowhere to point is not a row: the sheet's whole
		// vocabulary is "here is where this is".
		if h.URL == "" {
			continue
		}
		kind := h.Kind
		if kind == "" {
			kind = "site"
		}
		row := Link{Label: h.Label, URL: h.URL, Kind: kind,
			State: h.VisualState(), Status: oneLine(spaced(h.State))}
		if h.State != "online" && h.Reason != "" {
			row.Why = h.Reason
		}
		out.Links = append(out.Links, row)
	}
	// The deploy, behind its URL: a workflow run without its Actions page is a
	// chip that does nothing, which parseDeploy answers as a quiet rather than
	// as a row nobody can follow.
	out.DeployQuiet = status.DeployQuiet
	if d := status.Deploy; d != nil {
		row := Link{Label: d.Label, URL: d.URL, Kind: "deploy", State: d.State, Why: d.Why}
		if d.State == "running" {
			row.StartedAt, row.TypicalSeconds = d.StartedAt, d.TypicalSeconds
		}
		out.Links = append(out.Links, row)
	}
	// The local run, and the one row here with nowhere to go. Its log is a
	// filesystem path rather than an http address, and requiring a URL would
	// emit nothing at all — which is the whole reason it is a row of its own.
	if run := status.Run; run != nil {
		row := Link{Label: run.Label, Kind: "run", State: run.State, Local: true}
		if run.State == "running" {
			row.Phase = run.Phase
			row.StartedAt, row.TypicalSeconds = run.StartedAt, run.TypicalSeconds
		}
		out.Links = append(out.Links, row)
	}
	out.Links = append(out.Links, r.servers(ctx, cwd)...)

	if len(out.Links) > MaxLinks {
		out.Links, out.Truncated = out.Links[:MaxLinks], true
	}
	return out
}

// remote is the one git read, and the four answers it can give.
func (r *Reader) remote(ctx context.Context, cwd string) (string, Repo, Unreadable) {
	if r.Git == nil {
		return "", RepoUnreadable, UnreadableMissing
	}
	url, err := r.Git.Remote(ctx, cwd)
	switch {
	case err == nil:
		if repo := GitHubRepo(url); repo != "" {
			return repo, RepoGitHub, ""
		}
		return "", RepoRemoteNotGitHub, ""
	case errors.Is(err, git.ErrNoRemote):
		return "", RepoNoRemote, ""
	case errors.Is(err, git.ErrNotRepository):
		return "", RepoNotRepository, ""
	case errors.Is(err, git.ErrUnavailable):
		return "", RepoUnreadable, UnreadableMissing
	case errors.Is(err, git.ErrTimedOut):
		return "", RepoUnreadable, UnreadableTimedOut
	case errors.Is(err, git.ErrTooLarge):
		return "", RepoUnreadable, UnreadableTooLarge
	}
	return "", RepoUnreadable, UnreadableFailed
}

// servers is this project's dev stack, if it describes one: the ports its
// `.devstack.json` declares, asked directly.
//
// **No command of that file is run**, not even its `status`, for the reason
// devstack's package comment gives: this daemon's surface is HTTP and every
// paired device reaches it. So a process with a declared port gets an honest
// `ok` or `down` from a probe, and one with only a URL gets **no state at
// all** with the reason why — a red dot nobody measured is the mark that is
// always wrong.
func (r *Reader) servers(ctx context.Context, cwd string) []Link {
	home := r.Home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	found := devstack.Discover(nil, []string{cwd}, home)
	if len(found.Specs) == 0 {
		return nil
	}
	probe := r.Probe
	if probe == nil {
		probe = devstack.Listening
	}
	var out []Link
	for _, stack := range devstack.Read(ctx, found.Specs, probe) {
		for _, p := range stack.Processes {
			url := p.URL
			if url == "" && p.Port > 0 {
				url = fmt.Sprintf("http://127.0.0.1:%d", p.Port)
			}
			url = address(url)
			if url == "" {
				continue
			}
			row := Link{Label: label(p.Name, stack.Name), URL: url,
				Kind: "server", Local: true}
			switch {
			case p.Port > 0 && p.State == "running":
				row.State = "ok"
			case p.Port > 0:
				row.State = "down"
			case stack.Declares("status"):
				row.UnknownReason = "status_not_run"
			default:
				row.UnknownReason = "nothing_declared"
			}
			out = append(out, row)
		}
	}
	return out
}

// spaced is a receipt's own word as a row prints it: `not_deployed` reads as
// two words.
func spaced(s string) string {
	return replaceUnderscores(s)
}

func replaceUnderscores(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '_' {
			out = append(out, ' ')
			continue
		}
		out = append(out, r)
	}
	return string(out)
}
