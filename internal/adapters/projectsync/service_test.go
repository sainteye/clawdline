package projectsync

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/icon"
	domain "github.com/sainteye/clawdline/internal/domain/projectsync"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "<missing>"
	}
	return string(data)
}

// checkout makes a repository with one tracked skill and the given origin.
func checkout(t *testing.T, dir, origin string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "init", "-q")
	write(t, dir, ".claude/skills/shared/SKILL.md", "tracked")
	run(t, dir, "add", ".")
	run(t, dir, "commit", "-qm", "init")
	if origin != "" {
		run(t, dir, "remote", "add", "origin", origin)
	}
	return dir
}

func colour(c string) icon.Grid { return icon.Grid{Accent: c, Cells: [][]*string{{&c}}} }

func service(t *testing.T, dirs ...string) *Service {
	t.Helper()
	m, err := domain.OpenMirror(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &Service{
		Mirror: m,
		Checkouts: func(context.Context) []Checkout {
			var out []Checkout
			for _, d := range dirs {
				out = append(out, Checkout{Path: d, Label: filepath.Base(d)})
			}
			return out
		},
		Icon: func(string) icon.Grid { return colour("#123456") },
	}
}

func TestASourceOffersOnlyWhatGitDoesNotCarry(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	shop := checkout(t, filepath.Join(base, "shop"), "git@github.com:acme/shop.git")
	write(t, shop, ".claude/skills/local/SKILL.md", "only here")
	write(t, shop, "CLAUDE.local.md", "notes")
	write(t, shop, ".claude/settings.local.json", `{"permissions":{}}`)
	loose := checkout(t, filepath.Join(base, "loose"), "")
	src := service(t, shop, loose)

	m := src.Manifest(ctx)
	if len(m.Projects) != 1 || m.Projects[0].Repo != "github.com/acme/shop" {
		t.Fatalf("projects %+v", m.Projects)
	}
	var paths []string
	for _, f := range m.Projects[0].Files {
		paths = append(paths, f.Path)
		if f.Content != nil {
			t.Errorf("the manifest carried the content of %s", f.Path)
		}
	}
	if len(paths) != 2 || paths[0] != ".claude/skills/local/SKILL.md" || paths[1] != "CLAUDE.local.md" {
		t.Fatalf("files %v: want the untracked skill and CLAUDE.local.md, never settings.local.json or a tracked skill", paths)
	}
	if len(m.Skipped) != 1 || m.Skipped[0].Reason != domain.SkipNoRemote {
		t.Fatalf("a project without an origin should be named as skipped: %+v", m.Skipped)
	}
	e, err := src.Entry(ctx, "github.com/acme/shop")
	if err != nil || len(e.Files) != 2 || string(e.Files[1].Content) != "notes" {
		t.Fatalf("entry %+v %v", e, err)
	}
	if e.Revision != m.Projects[0].Revision {
		t.Fatal("the entry and the manifest disagree about the revision")
	}
}

func TestAMirrorAppliesKeepsEditsAndNeverRepublishes(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	shop := checkout(t, filepath.Join(base, "a", "shop"), "git@github.com:acme/shop.git")
	write(t, shop, ".claude/skills/local/SKILL.md", "v1")
	write(t, shop, "CLAUDE.local.md", "notes v1")
	src := service(t, shop)
	// The receiver's checkout lives at another path and spells the remote over https.
	there := checkout(t, filepath.Join(base, "b", "elsewhere"), "https://github.com/acme/shop")
	dst := service(t, there)
	source := domain.Source{Machine: "mac-1", Name: "Studio"}

	e, err := src.Entry(ctx, "github.com/acme/shop")
	if err != nil {
		t.Fatal(err)
	}
	e.Label, e.Icon = "Shop", colour("#ABCDEF")
	res, err := dst.Apply(ctx, ApplyRequest{Source: source, Project: e})
	if err != nil || res.State != StateApplied || res.Path != there || len(res.Written) != 2 {
		t.Fatalf("first apply %+v %v", res, err)
	}
	if read(t, there, ".claude/skills/local/SKILL.md") != "v1" {
		t.Fatal("the skill did not arrive")
	}
	if rec, ok := dst.Mirror.Lookup(filepath.Join(there, "sub")); !ok || rec.Label != "Shop" || rec.Icon.Accent != "#ABCDEF" {
		t.Fatalf("the mirror does not name the project: %+v", rec)
	}
	// Applying the same thing again is a no-op.
	if res, err = dst.Apply(ctx, ApplyRequest{Source: source, Project: e}); err != nil || res.State != StateUnchanged {
		t.Fatalf("repeat %+v %v", res, err)
	}
	// A mirror never offers what it mirrors.
	if m := dst.Manifest(ctx); len(m.Projects) != 0 || len(m.Skipped) != 1 || m.Skipped[0].Reason != domain.SkipMirrored {
		t.Fatalf("the mirror republished: %+v", m)
	}

	// Somebody edits CLAUDE.local.md on the mirror; the source changes both
	// files and drops nothing.
	write(t, there, "CLAUDE.local.md", "edited on the mirror")
	write(t, shop, ".claude/skills/local/SKILL.md", "v2")
	write(t, shop, "CLAUDE.local.md", "notes v2")
	e2, _ := src.Entry(ctx, "github.com/acme/shop")
	res, err = dst.Apply(ctx, ApplyRequest{Source: source, Project: e2})
	if err != nil || len(res.Written) != 1 || len(res.Kept) != 1 || res.Kept[0].Reason != domain.KeepLocalEdit {
		t.Fatalf("second apply %+v %v", res, err)
	}
	if read(t, there, ".claude/skills/local/SKILL.md") != "v2" || read(t, there, "CLAUDE.local.md") != "edited on the mirror" {
		t.Fatal("the mirror overwrote an edit, or missed an update")
	}

	// The source deletes the skill: the mirror's untouched copy goes too.
	if err := os.RemoveAll(filepath.Join(shop, ".claude/skills/local")); err != nil {
		t.Fatal(err)
	}
	e3, _ := src.Entry(ctx, "github.com/acme/shop")
	res, err = dst.Apply(ctx, ApplyRequest{Source: source, Project: e3})
	if err != nil || len(res.Deleted) != 1 || read(t, there, ".claude/skills/local/SKILL.md") != "<missing>" {
		t.Fatalf("deletion %+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(there, ".claude/skills/local")); !os.IsNotExist(err) {
		t.Fatal("the emptied skill directory was left behind")
	}
	if read(t, there, ".claude/skills/shared/SKILL.md") != "tracked" {
		t.Fatal("a tracked skill was touched")
	}

	// Another machine may not take the project over silently.
	if _, err := dst.Apply(ctx, ApplyRequest{Source: domain.Source{Machine: "mac-2"}, Project: e3}); err == nil {
		t.Fatal("a second source was accepted")
	}
	if res, err := dst.Apply(ctx, ApplyRequest{Source: domain.Source{Machine: "mac-2"}, Project: e3, ReplaceSource: true}); err != nil || res.State != StateApplied {
		t.Fatalf("an explicit replacement was refused: %+v %v", res, err)
	}
	if removed, err := dst.Detach("github.com/acme/shop"); err != nil || !removed {
		t.Fatal("detach")
	}
	if m := dst.Manifest(ctx); len(m.Projects) != 1 {
		t.Fatal("a detached project should be this machine's own again")
	}
}

func TestAMirrorRefusesToWriteThroughALink(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	there := checkout(t, filepath.Join(base, "there"), "git@github.com:acme/shop.git")
	if err := os.Symlink(outside, filepath.Join(there, ".claude", "commands")); err != nil {
		t.Fatal(err)
	}
	dst := service(t, there)
	content := []byte("x")
	e := domain.Entry{Repo: "github.com/acme/shop", Icon: colour("#000000"),
		Files: []domain.File{{Path: ".claude/commands/evil.md", Content: content, Size: 1, SHA256: domain.Sum(content)}}}
	res, err := dst.Apply(ctx, ApplyRequest{Project: e})
	if err != nil || len(res.Written) != 0 || len(res.Kept) != 1 || res.Kept[0].Reason != domain.KeepUnsafe {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(outside, "evil.md")); !os.IsNotExist(err) {
		t.Fatal("a write went through the link")
	}
}

func TestAMissingRepositoryIsClonedWhenAsked(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	shop := checkout(t, filepath.Join(base, "src", "shop"), "https://git.example.test/acme/shop.git")
	write(t, shop, "CLAUDE.local.md", "notes")
	// The portable URL resolves to the local repository for this test only.
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url."+shop+".insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", "https://git.example.test/acme/shop.git")
	src := service(t, shop)
	e, err := src.Entry(ctx, "git.example.test/acme/shop")
	if err != nil {
		t.Fatal(err)
	}
	dst := service(t)
	cloneRoot := filepath.Join(base, "projects")
	dst.CloneRoot = func() string { return cloneRoot }
	var registered []string
	dst.Register = func(p string) error { registered = append(registered, p); return nil }

	if res, err := dst.Apply(ctx, ApplyRequest{Project: e}); err != nil || res.State != StateMissing {
		t.Fatalf("without clone: %+v %v", res, err)
	}
	res, err := dst.Apply(ctx, ApplyRequest{Project: e, Clone: true})
	if err != nil || res.State != StateCloning || res.Path != filepath.Join(cloneRoot, "shop") {
		t.Fatalf("clone: %+v %v", res, err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for len(dst.State().Clones) > 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if st := dst.State(); len(st.Clones) != 0 || len(st.Projects) != 1 {
		t.Fatalf("after clone: %+v", st)
	}
	if read(t, filepath.Join(cloneRoot, "shop"), "CLAUDE.local.md") != "notes" || len(registered) != 1 {
		t.Fatalf("the clone was not applied or not registered: %v", registered)
	}
}

func TestAFailedCloneIsReportedNotHidden(t *testing.T) {
	ctx := context.Background()
	content := []byte("x")
	e := domain.Entry{Repo: "git.example.test/acme/none", CloneURL: "https://git.example.test/acme/none.git", Icon: colour("#000000"),
		Files: []domain.File{{Path: "CLAUDE.local.md", Content: content, Size: 1, SHA256: domain.Sum(content)}}}
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url."+filepath.Join(t.TempDir(), "absent")+".insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", "https://git.example.test/")
	dst := service(t)
	root := t.TempDir()
	dst.CloneRoot = func() string { return root }
	if _, err := dst.Apply(ctx, ApplyRequest{Project: e, Clone: true}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if c := dst.State().Clones; len(c) == 1 && c[0].State == StateFailed {
			if c[0].Error == "" {
				t.Fatal("a failed clone carries no reason")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("the clone never reported failure: %+v", dst.State())
}

func TestTheManifestAndTheEntryAgreeAndAWithheldFileIsNeverDeleted(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	shop := checkout(t, filepath.Join(base, "shop"), "git@github.com:acme/shop.git")
	big := string(make([]byte, 250<<10))
	for i := 0; i < 10; i++ {
		write(t, shop, ".claude/skills/s"+string(rune('a'+i))+"/SKILL.md", big[:len(big)-i])
	}
	src := service(t, shop)
	m := src.Manifest(ctx)
	e, err := src.Entry(ctx, "github.com/acme/shop")
	if err != nil {
		t.Fatal(err)
	}
	if m.Projects[0].Revision != e.Revision || len(m.Projects[0].Files) != len(e.Files) {
		t.Fatalf("manifest %d files rev %s, entry %d files rev %s: a mirror would re-apply forever",
			len(m.Projects[0].Files), m.Projects[0].Revision, len(e.Files), e.Revision)
	}
	if len(e.Withheld) == 0 {
		t.Fatal("files over the budget should be named as withheld")
	}
	// A mirror that owns a withheld file keeps it.
	there := checkout(t, filepath.Join(base, "there"), "git@github.com:acme/shop.git")
	dst := service(t, there)
	gone := e.Withheld[0]
	write(t, there, gone, "old copy")
	if err := dst.Mirror.Put(domain.Record{Repo: e.Repo, Path: there, Icon: colour("#000000"),
		Files: map[string]string{gone: domain.Sum([]byte("old copy"))}}); err != nil {
		t.Fatal(err)
	}
	if _, err := dst.Apply(ctx, ApplyRequest{Project: e}); err != nil {
		t.Fatal(err)
	}
	if read(t, there, gone) != "old copy" {
		t.Fatal("a file the source withheld was deleted as if the source had dropped it")
	}
}

func TestAnApplyWhileCloningDoesNotWriteIntoTheHalfMadeCheckout(t *testing.T) {
	ctx := context.Background()
	content := []byte("x")
	e := domain.Entry{Repo: "git.example.test/acme/slow", CloneURL: "https://git.example.test/acme/slow.git", Icon: colour("#000000"),
		Files: []domain.File{{Path: "CLAUDE.local.md", Content: content, Size: 1, SHA256: domain.Sum(content)}}}
	dst := service(t)
	root := t.TempDir()
	dst.CloneRoot = func() string { return root }
	dst.jobs = map[string]*Job{e.Repo: {Repo: e.Repo, State: StateCloning, Dest: filepath.Join(root, "slow")}}
	// The half-made checkout already names its origin, as git clone writes it first.
	checkout(t, filepath.Join(root, "slow"), e.CloneURL)
	res, err := dst.Apply(ctx, ApplyRequest{Project: e, Clone: true})
	if err != nil || res.State != StateCloning {
		t.Fatalf("%+v %v", res, err)
	}
	if read(t, filepath.Join(root, "slow"), "CLAUDE.local.md") != "<missing>" {
		t.Fatal("an apply wrote into a checkout that was still being cloned")
	}
	if len(dst.State().Clones) != 1 {
		t.Fatal("the running clone was forgotten")
	}
}
