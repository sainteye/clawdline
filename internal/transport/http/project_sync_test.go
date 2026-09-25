package http

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/domain/icon"
	domain "github.com/sainteye/clawdline/internal/domain/projectsync"
)

// syncStandIn is iconCloudStandIn whose one project is a git checkout with
// the given origin and an untracked project skill.
func syncStandIn(t *testing.T, origin, skill string) (*standIn, string) {
	t.Helper()
	s := iconCloudStandIn(t)
	project, ok := s.server.workV2Project(t.Context(), s.place)
	if !ok {
		t.Fatal("missing fixture place")
	}
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", origin}} {
		if out, err := exec.Command("git", append([]string{"-C", project.Path}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if skill != "" {
		dir := filepath.Join(project.Path, ".claude", "skills", "deploy")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skill), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mirror, err := domain.OpenMirror(s.server.cfg.Dir)
	if err != nil {
		t.Fatal(err)
	}
	s.server.icons.SetMirror(mirrorLookup(mirror))
	s.server.projectSync = s.server.newProjectSync(mirror)
	return s, project.Path
}

func TestCloudCarriesProjectSettingsFromASourceToAMirror(t *testing.T) {
	src, _ := syncStandIn(t, "git@github.com:acme/shop.git", "deploy steps")
	dst, there := syncStandIn(t, "https://github.com/acme/shop", "")
	machine := cloudops.MachineReplySession

	a, _ := src.ask(t, 1, map[string]any{"type": "project-manifest", "session": machine, "request": "m"})
	var manifest domain.Manifest
	if !a.OK() || unwrap(a.Payload, &manifest) != nil || len(manifest.Projects) != 1 {
		t.Fatalf("manifest: %d %s", a.Status, a.Payload)
	}
	a, _ = src.ask(t, 2, map[string]any{"type": "project-entry", "session": machine, "request": "e", "repo": manifest.Projects[0].Repo})
	var entry struct {
		Project map[string]any `json:"project"`
	}
	if !a.OK() || unwrap(a.Payload, &entry) != nil || entry.Project["repo"] != "github.com/acme/shop" {
		t.Fatalf("entry: %d %s", a.Status, a.Payload)
	}
	entry.Project["label"] = "Acme Shop"

	apply := func(seq uint64) cloudops.Answer {
		a, _ := dst.ask(t, seq, map[string]any{"type": "project-mirror-apply", "session": machine, "request": "apply",
			"item": map[string]any{"source": map[string]any{"machine": "mac-01", "name": "Studio"}, "project": entry.Project, "clone": false}})
		return a
	}
	dst.bridge.AllowCommands = func() bool { return false }
	if a := apply(3); a.OK() {
		t.Fatal("an apply passed the remote-write gate while it was off")
	}
	dst.bridge.AllowCommands = func() bool { return true }
	if a := apply(4); !a.OK() {
		t.Fatalf("apply: %d %s", a.Status, a.Payload)
	}
	data, err := os.ReadFile(filepath.Join(there, ".claude", "skills", "deploy", "SKILL.md"))
	if err != nil || string(data) != "deploy steps" {
		t.Fatalf("the skill did not arrive: %q %v", data, err)
	}
	if got := dst.server.icons.Label(there); got != "Acme Shop" {
		t.Fatalf("the mirror's label is %q", got)
	}

	// The mirror's icon is the source's, and cannot be changed here.
	current := dst.server.icons.For(there)
	c := "#FEDCBA"
	other := icon.Grid{Accent: c, Cells: [][]*string{{&c}}}
	a, _ = dst.ask(t, 5, map[string]any{"type": "project-icon-copy", "session": machine, "request": "icon",
		"id": dst.place, "item": map[string]any{"icon": other, "expected": current}})
	if a.Status != 409 {
		t.Fatalf("an icon write on a mirrored project answered %d %s", a.Status, a.Payload)
	}
	a, _ = dst.ask(t, 6, map[string]any{"type": "project-mirror", "session": machine, "request": "state"})
	var state struct {
		Projects []domain.Record `json:"projects"`
	}
	if !a.OK() || unwrap(a.Payload, &state) != nil || len(state.Projects) != 1 || state.Projects[0].Source.Name != "Studio" {
		t.Fatalf("mirror state: %d %s", a.Status, a.Payload)
	}
	a, _ = dst.ask(t, 7, map[string]any{"type": "project-mirror-detach", "session": machine, "request": "detach", "repo": "github.com/acme/shop"})
	if !a.OK() {
		t.Fatalf("detach: %d %s", a.Status, a.Payload)
	}
	if own := dst.server.icons.For(there); own.Accent == current.Accent {
		t.Fatal("a detached project still draws the mirrored mark")
	}
	a, _ = dst.ask(t, 8, map[string]any{"type": "project-icon-copy", "session": machine, "request": "icon2",
		"id": dst.place, "item": map[string]any{"icon": other, "expected": dst.server.icons.For(there)}})
	if !a.OK() {
		t.Fatalf("a detached project's icon is its own again: %d %s", a.Status, a.Payload)
	}
}

// unwrap reads the route's answer out of a read envelope's payload.
func unwrap(payload []byte, into any) error {
	var outer struct {
		Body json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(payload, &outer); err != nil {
		return err
	}
	return json.Unmarshal(outer.Body, into)
}

func TestALargeProjectFitsItsOwnBound(t *testing.T) {
	src, _ := syncStandIn(t, "git@github.com:acme/big.git", "")
	dst, _ := syncStandIn(t, "https://github.com/acme/big", "")
	project, _ := src.server.workV2Project(t.Context(), src.place)
	big := make([]byte, 220<<10)
	for i := 0; i < 7; i++ {
		dir := filepath.Join(project.Path, ".claude", "skills", string(rune('a'+i)))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), big[:len(big)-i], 0o644); err != nil {
			t.Fatal(err)
		}
	}
	machine := cloudops.MachineReplySession
	a, _ := src.ask(t, 1, map[string]any{"type": "project-entry", "session": machine, "request": "e", "repo": "github.com/acme/big"})
	var entry struct {
		Project map[string]any `json:"project"`
	}
	if !a.OK() || unwrap(a.Payload, &entry) != nil {
		t.Fatalf("entry: %d", a.Status)
	}
	a, _ = dst.ask(t, 2, map[string]any{"type": "project-mirror-apply", "session": machine, "request": "apply",
		"item": map[string]any{"source": map[string]any{"machine": "mac-01", "name": "Studio"}, "project": entry.Project, "clone": false}})
	if !a.OK() {
		t.Fatalf("a 1.5 MB project was refused: %d %.200s", a.Status, a.Payload)
	}
}
