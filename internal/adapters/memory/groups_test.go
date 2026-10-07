package memory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every group and entry here is invented.

func grouped(name, group string) Entry {
	e := invented(name, TypeFeedback)
	e.Group = group
	return e
}

func TestAGroupedEntryRoundTripsAndNeedsItsGroup(t *testing.T) {
	s := New(t.TempDir())
	if _, err := s.Add(testKey, grouped("in-a-group", "build-steps")); !errors.Is(err, ErrNoGroup) {
		t.Fatalf("an entry naming a group nobody set: %v", err)
	}
	if out, err := s.SetGroup(testKey, "build-steps", "before compiling or running the test suite"); err != nil || out != Created {
		t.Fatalf("set group: %v %v", out, err)
	}
	if out, err := s.SetGroup(testKey, "build-steps", "before compiling or running the test suite"); err != nil || out != Unchanged {
		t.Fatalf("the same group again: %v %v", out, err)
	}
	e := grouped("in-a-group", "build-steps")
	if _, err := s.Add(testKey, e); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(testKey, e.Name)
	if err != nil || got.Group != "build-steps" {
		t.Fatalf("get: %+v %v", got, err)
	}
	if out, err := s.Add(testKey, e); err != nil || out != Unchanged {
		t.Fatalf("a retried grouped add: %v %v", out, err)
	}
	e.Group = ""
	if out, err := s.Update(testKey, e); err != nil || out != Updated {
		t.Fatalf("moving an entry out of its group: %v %v", out, err)
	}
	if got, _ := s.Get(testKey, e.Name); got.Group != "" {
		t.Fatalf("still grouped: %+v", got)
	}
	for _, bad := range []struct{ slug, description string }{
		{"Bad Slug", "d"}, {"ok", ""}, {"ok", "two\nlines"}, {"ok", strings.Repeat("x", GroupDescriptionByteLimit+1)},
	} {
		if _, err := s.SetGroup(testKey, bad.slug, bad.description); !errors.Is(err, ErrInvalid) {
			t.Fatalf("group %q %q: %v", bad.slug, bad.description, err)
		}
	}
}

func TestOneGroupPastTheBoundIsRefused(t *testing.T) {
	s := New(t.TempDir())
	for i := 0; i < GroupCountLimit; i++ {
		if _, err := s.SetGroup(testKey, fmt.Sprintf("topic-%02d", i), "invented"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.SetGroup(testKey, "one-more", "invented"); !errors.Is(err, ErrFull) {
		t.Fatalf("one group past the bound: %v", err)
	}
	if out, err := s.SetGroup(testKey, "topic-00", "changed"); err != nil || out != Updated {
		t.Fatalf("changing a group in a full store: %v %v", out, err)
	}
}

func TestAGroupsFileThatCannotBeReadIsAnErrorNotNoGroups(t *testing.T) {
	s := New(t.TempDir())
	if _, err := s.Add(testKey, invented("a-lesson", TypeUser)); err != nil {
		t.Fatal(err)
	}
	dir, _ := s.Dir(testKey)
	if err := os.WriteFile(filepath.Join(dir, GroupsFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Listing(testKey); err == nil {
		t.Fatal("an unreadable groups file listed as no groups")
	}
}

// The index lists every resident entry, then one line per group with its
// description, its count and the command that lists it.
func TestTheIndexListsResidentEntriesThenEveryGroup(t *testing.T) {
	s := New(t.TempDir())
	groups := map[string]string{"alpha-topic": "before touching the alpha subsystem", "beta-topic": "when a beta check fails"}
	for slug, d := range groups {
		if _, err := s.SetGroup(testKey, slug, d); err != nil {
			t.Fatal(err)
		}
	}
	add := func(e Entry) {
		t.Helper()
		if _, err := s.Add(testKey, e); err != nil {
			t.Fatal(err)
		}
	}
	add(invented("always-one", TypeUser))
	add(invented("always-two", TypeProject))
	for i := 0; i < 40; i++ {
		add(grouped(fmt.Sprintf("alpha-%02d", i), "alpha-topic"))
	}
	for i := 0; i < 3; i++ {
		add(grouped(fmt.Sprintf("beta-%02d", i), "beta-topic"))
	}
	text, n, cut, err := s.Index(testKey)
	if err != nil || n != 45 || cut {
		t.Fatalf("index: %d %v %v", n, cut, err)
	}
	want := indexHeading +
		"\n## user\n\n- always-one — an invented lesson about always-one\n" +
		"\n## project\n\n- always-two — an invented lesson about always-two\n" +
		groupsHeading +
		"- alpha-topic — before touching the alpha subsystem — 40 entries: `clawdline memory list --group alpha-topic`\n" +
		"- beta-topic — when a beta check fails — 3 entries: `clawdline memory list --group beta-topic`\n"
	if text != want {
		t.Fatalf("index:\n%s\nwant:\n%s", text, want)
	}
	dir, _ := s.Dir(testKey)
	if onDisk, _ := os.ReadFile(filepath.Join(dir, IndexFile)); string(onDisk) != want {
		t.Fatalf("INDEX.md:\n%s", onDisk)
	}
	l, err := s.Listing(testKey)
	if err != nil || len(l.Entries) != 45 || len(l.Groups) != 2 || l.Groups[0].Entries != 40 || l.Groups[1].Entries != 3 {
		t.Fatalf("listing: %+v %v", l.Groups, err)
	}
}

// Resident entries alone past the bound are cut with a note, and every
// group line is still there.
func TestResidentEntriesPastTheBoundAreCutAndGroupsKept(t *testing.T) {
	s := New(t.TempDir())
	for i := 0; i < GroupCountLimit; i++ {
		if _, err := s.SetGroup(testKey, fmt.Sprintf("topic-%02d", i), strings.Repeat("w", GroupDescriptionByteLimit)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Add(testKey, grouped(fmt.Sprintf("member-%02d", i), fmt.Sprintf("topic-%02d", i))); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 100; i++ {
		e := invented(fmt.Sprintf("resident-%03d", i), TypeFeedback)
		e.Description = strings.Repeat("d", 80)
		if _, err := s.Add(testKey, e); err != nil {
			t.Fatal(err)
		}
	}
	text, _, cut, err := s.Index(testKey)
	if err != nil || !cut {
		t.Fatalf("index: %v %v", cut, err)
	}
	if len(text) > IndexByteLimit {
		t.Fatalf("the index is %d bytes, past %d", len(text), IndexByteLimit)
	}
	listed := strings.Count(text, "\n- resident-")
	if listed == 0 || listed == 100 {
		t.Fatalf("%d resident entries listed", listed)
	}
	if !strings.Contains(text, fmt.Sprintf("… %d more resident entries are not shown", 100-listed)) {
		t.Fatalf("no cut note:\n%s", text)
	}
	for i := 0; i < GroupCountLimit; i++ {
		if !strings.Contains(text, fmt.Sprintf("`clawdline memory list --group topic-%02d`", i)) {
			t.Fatalf("group topic-%02d is missing from a cut index", i)
		}
	}
}

func TestLaunchTextCarriesTheGroupLines(t *testing.T) {
	state := t.TempDir()
	repo, _ := filepath.EvalSymlinks(t.TempDir())
	s := New(state)
	key, _ := KeyFor(repo)
	if _, err := s.SetGroup(key, "release-steps", "before cutting a release"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(key, grouped("tag-first", "release-steps")); err != nil {
		t.Fatal(err)
	}
	got, err := s.LaunchText(repo)
	if err != nil || !strings.Contains(got, "- release-steps — before cutting a release — 1 entries: `clawdline memory list --group release-steps`") ||
		strings.Contains(got, "tag-first") {
		t.Fatalf("launch text %q %v", got, err)
	}
}

// An invented Claude Code auto-memory tree: MEMORY.md with two resident
// entries and two sub-indexes, each linking entries, and one entry no file
// links.
func claudeTreeFixture(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := func(name, kind string) string {
		return "---\nname: " + name + "\ndescription: invented " + name + "\nmetadata:\n  type: " + kind + "\n---\n\nInvented body; see [[other]].\n"
	}
	files := map[string]string{
		"MEMORY.md": "Resident index.\n\n- [Git rules](index-git-rules.md) — before committing\n" +
			"- [Deploys](index-deploys.md) — before deploying\n\nAlways:\n" +
			"- [Keep it short](keep-short.md) — always applies\n- [Ask first](ask-first.md) — also always\n",
		"index-git-rules.md": "---\nname: index-git-rules\ndescription: Read before committing, merging or cleaning a worktree\nmetadata:\n  type: reference\n---\n\n" +
			"# Git rules\n\n- [Stage by name](stage-by-name.md) — hook\n- [No bare reset](no-bare-reset.md) — hook\n- [Keep it short](keep-short.md) — also here\n",
		"index-deploys.md": "---\nname: index-deploys\ndescription: Read before deploying\ntype: reference\n---\n\n- [Check the stamp](check-stamp.md) — hook\n",
		"keep-short.md":    entry("keep-short", "feedback"),
		"ask-first.md":     entry("ask-first", "user"),
		"stage-by-name.md": entry("stage-by-name", "feedback"),
		"no-bare-reset.md": entry("no-bare-reset", "feedback"),
		"check-stamp.md":   entry("check-stamp", "project"),
		"orphan-lesson.md": entry("orphan-lesson", "reference"),
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestReadClaudeKeepsMemoryMdsGroups(t *testing.T) {
	dir := claudeTreeFixture(t)
	before := snapshot(t, dir)
	plan, err := ReadClaude(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.HasIndex || len(plan.Groups) != 2 {
		t.Fatalf("groups %+v", plan.Groups)
	}
	git, deploys := plan.Groups[0], plan.Groups[1]
	if git.Slug != "git-rules" || git.Description != "Read before committing, merging or cleaning a worktree" ||
		git.File != "index-git-rules.md" || strings.Join(git.Members, ",") != "stage-by-name,no-bare-reset" {
		t.Fatalf("git group %+v", git)
	}
	if deploys.Slug != "deploys" || strings.Join(deploys.Members, ",") != "check-stamp" {
		t.Fatalf("deploys group %+v", deploys)
	}
	groupOf := map[string]string{}
	for _, e := range plan.Entries {
		groupOf[e.Name] = e.Group
	}
	want := map[string]string{"keep-short": "", "ask-first": "", "stage-by-name": "git-rules", "no-bare-reset": "git-rules",
		"check-stamp": "deploys", "orphan-lesson": ""}
	if fmt.Sprint(groupOf) != fmt.Sprint(want) {
		t.Fatalf("entries %v, want %v", groupOf, want)
	}
	if strings.Join(plan.Unlinked, ",") != "orphan-lesson" {
		t.Fatalf("unlinked %v", plan.Unlinked)
	}
	why := map[string]string{}
	for _, s := range plan.Skipped {
		why[s.File] = s.Reason
	}
	if !strings.Contains(why["index-git-rules.md"], "group git-rules") || !strings.Contains(why["index-deploys.md"], "group deploys") {
		t.Fatalf("index files were not skipped as groups: %v", why)
	}
	if after := snapshot(t, dir); after != before {
		t.Fatal("reading the source changed it")
	}
}

func TestMostGroupsIsTheProjectWithTheMost(t *testing.T) {
	s := New(t.TempDir())
	if n, err := s.MostGroups(); n != 0 || err != nil {
		t.Fatalf("no store: %d %v", n, err)
	}
	for i, key := range []string{testKey, "other-project-89abcdef"} {
		for j := 0; j <= i*2; j++ {
			if _, err := s.SetGroup(key, fmt.Sprintf("topic-%d", j), "invented"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if n, err := s.MostGroups(); n != 3 || err != nil {
		t.Fatalf("most groups: %d %v", n, err)
	}
}
