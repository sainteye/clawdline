package http

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/icon"
)

// placesHome is a made-up home directory for the start places to read: a
// `~/.claude/projects` folder recording each of dirs, and each of dirs made.
//
// The temporary directory is a scratch root the places never offer, so the
// test's own tree is moved out from under it; otherwise every directory here
// would be refused for being temporary and none of the cases would be tested.
func placesHome(t *testing.T) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if base == "/tmp" || strings.HasPrefix(base, "/tmp/") || strings.HasPrefix(base, "/private/tmp/") {
		t.Skip("the temporary directory is under /tmp, which the places never offer")
	}
	t.Setenv("TMPDIR", filepath.Join(base, "scratch"))
	home := filepath.Join(base, "home")
	t.Setenv("HOME", home)
	return home
}

// recordPlace makes dir and a Claude Code transcript folder that names it, the
// way `claude` leaves one behind after running there.
func recordPlace(t *testing.T, home, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(home, ".claude", "projects", slugOf(dir))
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","cwd":"` + dir + `"}` + "\n"
	if err := os.WriteFile(filepath.Join(folder, "c6000001-0000-4000-8000-000000000001.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
}

func slugOf(path string) string {
	var b strings.Builder
	for _, r := range path {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// The start sheet ("open a session"), the Projects catalog and the schedules
// all list the places this server's readers build. A checkout a Clawdline
// broker made for a child is not a project somebody keeps: the one this
// daemon made under its own state directory, and one the Swift app left under
// its own, are both kept off that list, whether they are known from a
// transcript folder or from a session running in them right now.
//
// The judgment is where a directory is, not what it is called: a project the
// person keeps in a directory named `worktrees`, even one shaped exactly like
// a broker's `<slug>/<task id>`, is still offered.
func TestBrokerCheckoutsAreNotOfferedAsPlaces(t *testing.T) {
	home := placesHome(t)
	state := filepath.Join(home, ".config", "clawdline-next")
	const slug, task = "repo-11111111", "c6000002-0000-4000-8000-000000000002"
	fresh := filepath.Join(state, "worktrees", slug, task)
	legacy := filepath.Join(home, "Library", "Application Support", "Clawdline", "worktrees", slug, task)
	kept := filepath.Join(home, "code", "worktrees", "foodblogs")
	lookalike := filepath.Join(home, "src", "worktrees", slug, task)
	for _, dir := range []string{fresh, legacy, kept, lookalike} {
		recordPlace(t, home, dir)
	}

	s := &Server{cfg: config.Config{Dir: state}, broker: &orchestrator.Broker{Dir: state}, icons: &icon.Registry{}}
	for _, live := range [][]string{nil, {fresh, legacy, kept, lookalike}} {
		listed := map[string]bool{}
		for _, p := range s.projectReaders().places.List(live, 40) {
			listed[p.Path] = true
		}
		for name, dir := range map[string]string{"this daemon's checkout": fresh, "the Swift app's checkout": legacy} {
			if listed[dir] {
				t.Errorf("%s is offered as a place (live %d): %s", name, len(live), dir)
			}
		}
		for name, dir := range map[string]string{"a project in a directory named worktrees": kept,
			"a project shaped like a checkout, elsewhere": lookalike} {
			if !listed[dir] {
				t.Errorf("%s is not offered (live %d): %s", name, len(live), dir)
			}
		}
	}
}
