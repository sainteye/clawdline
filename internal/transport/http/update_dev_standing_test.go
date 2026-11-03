package http

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/contract"
)

// A dev build is told apart from the release it would go back to by ancestry:
// past it, short of it, beside it, or the same commit. Release versions cannot
// say which, since every dev build sorts before its release.
func TestADevBuildIsPlacedAgainstTheReleaseByAncestry(t *testing.T) {
	repo := t.TempDir()
	git := gitAt(t, repo)
	commit := func(msg string) string {
		git("commit", "-q", "--allow-empty", "-m", msg)
		return git("rev-parse", "HEAD")
	}
	git("init", "-q", "-b", "main")
	older := commit("older")
	release := commit("release")
	ahead1 := commit("dev 1")
	ahead2 := commit("dev 2")
	git("checkout", "-q", "-b", "side", older)
	side := commit("side")
	devStandingCases(t, repo, older, release, ahead1, ahead2, side)
}

func gitAt(t *testing.T, repo string) func(args ...string) string {
	return func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.invalid",
			"-c", "commit.gpgsign=false"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
}

func devStandingCases(t *testing.T, repo, older, release, ahead1, ahead2, side string) {
	ctx := context.Background()
	for _, c := range []struct {
		name, dev     string
		relation      contract.UpdateDevRelation
		ahead, behind int64
	}{
		{"two commits past the release", ahead2, contract.UpdateDevRelationAhead, 2, 0},
		{"one commit past it", ahead1, contract.UpdateDevRelationAhead, 1, 0},
		{"one commit short of it", older, contract.UpdateDevRelationBehind, 0, 1},
		{"beside it", side, contract.UpdateDevRelationDiverged, 1, 1},
		{"the release's own commit", release, contract.UpdateDevRelationSame, 0, 0},
	} {
		got := devStandingIn(ctx, []string{t.TempDir(), repo}, c.dev, release)
		if got.Relation != c.relation || got.Ahead != c.ahead || got.Behind != c.behind {
			t.Errorf("%s: %+v, want %s +%d -%d", c.name, got, c.relation, c.ahead, c.behind)
		}
	}
	missing := strings.Repeat("0", 40)
	if got := devStandingIn(ctx, []string{repo}, missing, release); got.Relation != contract.UpdateDevRelationUnknown || got.Reason == "" {
		t.Errorf("a commit no Project holds: %+v, want unknown with a reason", got)
	}
}

// Right after a start the daemon has not listed its Projects yet. The first
// reading then finds no repository holding the commits; that is not kept, and
// a later read, with the checkout listed, places the dev build.
func TestAnUnknownStandingIsAskedAgain(t *testing.T) {
	repo, dev, release := devAndRelease(t)
	var mu sync.Mutex
	var listed []projects.Place
	s := &Server{squadPlaces: func(context.Context) []projects.Place {
		mu.Lock()
		defer mu.Unlock()
		return listed
	}}
	st := standingStatus(dev, release)
	if got := settledStanding(t, s, st); got.Relation != contract.UpdateDevRelationUnknown {
		t.Fatalf("with no Project listed: %+v, want unknown", got)
	}
	mu.Lock()
	listed = []projects.Place{{Path: repo}}
	mu.Unlock()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if got := s.withDevStanding(st).Dev; got != nil && got.Relation == contract.UpdateDevRelationAhead && got.Ahead == 1 {
			return
		} else if time.Now().After(deadline) {
			t.Fatalf("with the checkout listed a moment later: %+v, want ahead by 1", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Listing Projects reads every live Session and can take minutes. GET
// /v1/update does not wait for it: a read while the listing is stuck answers
// at once, without `dev`.
func TestReadingTheStandingNeverHoldsTheRoute(t *testing.T) {
	_, dev, release := devAndRelease(t)
	stuck := make(chan struct{})
	defer close(stuck)
	s := &Server{squadPlaces: func(ctx context.Context) []projects.Place {
		select {
		case <-stuck:
		case <-ctx.Done():
		}
		return nil
	}}
	st := standingStatus(dev, release)
	for i := 0; i < 3; i++ {
		done := make(chan contract.UpdateStatus, 1)
		go func() { done <- s.withDevStanding(st) }()
		select {
		case got := <-done:
			if got.Dev != nil {
				t.Fatalf("read %d answered %+v before anything was read", i, got.Dev)
			}
		case <-time.After(time.Second):
			t.Fatalf("read %d waited on the Project listing", i)
		}
	}
}

func devAndRelease(t *testing.T) (repo, dev, release string) {
	repo = t.TempDir()
	git := gitAt(t, repo)
	git("init", "-q", "-b", "main")
	git("commit", "-q", "--allow-empty", "-m", "release")
	release = git("rev-parse", "HEAD")
	git("commit", "-q", "--allow-empty", "-m", "dev")
	return repo, git("rev-parse", "HEAD"), release
}

func standingStatus(dev, release string) contract.UpdateStatus {
	return contract.UpdateStatus{
		Running: contract.BuildStamp{Stamp: dev, Version: "v1.0.7-dev." + dev[:7]},
		Latest:  contract.BuildStamp{Stamp: release, Version: "v1.0.7"},
	}
}

// settledStanding reads until the background reading has answered.
func settledStanding(t *testing.T, s *Server, st contract.UpdateStatus) contract.UpdateDevStanding {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got := s.withDevStanding(st).Dev; got != nil {
			return *got
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the standing was never read")
	return contract.UpdateDevStanding{}
}

// ~/Library is not looked in: cloud-storage folders live there, and a git
// process started there can make a privacy prompt name Clawdline.
func TestPlacesUnderLibraryAreNotLookedIn(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	inside := filepath.Join(home, "Library", "CloudStorage", "Dropbox", "work")
	beside := filepath.Join(home, "LibraryNotes")
	s := &Server{squadPlaces: func(context.Context) []projects.Place {
		return []projects.Place{{Path: inside}, {Path: beside}, {Path: filepath.Join(home, "code", "app")}}
	}}
	got := s.placeDirectories(context.Background())
	want := []string{beside, filepath.Join(home, "code", "app")}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("placeDirectories = %v, want %v", got, want)
	}
}

func TestDevStandingDoesNotProbePersonalProjectDirectories(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	code := filepath.Join(home, "code", "project")
	if err := os.MkdirAll(code, 0700); err != nil {
		t.Fatal(err)
	}
	rows := []projects.Place{{Path: code}}
	for _, name := range []string{"Desktop", "Documents", "Downloads", "Pictures", "Music", "Movies", "Library"} {
		rows = append(rows, projects.Place{Path: filepath.Join(home, name, "project")})
	}
	s := &Server{squadPlaces: func(context.Context) []projects.Place { return rows }}
	if got := s.placeDirectories(context.Background()); len(got) != 1 || got[0] != code {
		t.Fatalf("dev ancestry probes: %v", got)
	}
}
