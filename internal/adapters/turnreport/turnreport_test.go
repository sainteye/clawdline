package turnreport

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// repo is a throwaway git project; commit answers the new commit's id.
type repo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	r := &repo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", "main")
	r.git("config", "user.email", "t@example.invalid")
	r.git("config", "user.name", "t")
	r.git("config", "commit.gpgsign", "false")
	return r
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", r.dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *repo) write(path, text string) {
	r.t.Helper()
	p := filepath.Join(r.dir, path)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repo) commit(msg string) string {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "-m", msg)
	return r.git("rev-parse", "--short", "HEAD")
}

func paths(c *Collection) map[string]File {
	out := map[string]File{}
	for _, f := range c.Files {
		out[f.Path] = f
	}
	return out
}

// Another Session's commit between two of this turn's is not this turn's:
// its file stays out, and a file both touched shows only this turn's diffs.
func TestCollectReadsOnlyTheNamedCommits(t *testing.T) {
	r := newRepo(t)
	r.write("shared.go", "package a\n")
	r.commit("base")
	r.write("mine.go", "package a\n")
	r.write("shared.go", "package a\n\n// mine\n")
	a := r.commit("mine one")
	r.write("theirs.go", "package a\n")
	r.write("shared.go", "package a\n\n// mine\n// theirs\n")
	r.commit("theirs")
	r.write("mine.md", "# Mine\n")
	b := r.commit("mine two")

	c, err := Collect(context.Background(), Options{Repo: r.dir, Commits: []string{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	got := paths(c)
	if _, ok := got["theirs.go"]; ok {
		t.Errorf("another Session's file is in the report: %v", c.Files)
	}
	for _, p := range []string{"mine.go", "mine.md", "shared.go"} {
		if _, ok := got[p]; !ok {
			t.Errorf("%s is missing; got %v", p, c.Files)
		}
	}
	sh := got["shared.go"]
	if len(sh.Diffs) != 1 || strings.Contains(sh.Diffs[0].Diff, "theirs") {
		t.Errorf("shared.go carries another Session's change: %+v", sh.Diffs)
	}
	if strings.Join(c.Order, " ") != a+" "+b {
		t.Errorf("order = %v, want %s %s", c.Order, a, b)
	}
}

// New is a file that did not exist before this turn first touched it; changed
// is one that did; a file this turn added and removed again is not listed but
// named; a file it deleted is listed as deleted.
func TestCollectSaysNewChangedDeletedAndNetZero(t *testing.T) {
	r := newRepo(t)
	r.write("old.go", "package a\n")
	r.write("gone.go", "package a\n")
	r.commit("base")
	r.write("old.go", "package a\n// changed\n")
	r.write("fresh.go", "package a\n")
	r.write("uv.lock", "lock\n")
	one := r.commit("one")
	r.write("other.go", "package a\n")
	r.commit("someone else adds other.go")
	r.write("other.go", "package a\n// mine\n")
	if err := os.Remove(filepath.Join(r.dir, "uv.lock")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(r.dir, "gone.go")); err != nil {
		t.Fatal(err)
	}
	two := r.commit("two")

	c, err := Collect(context.Background(), Options{Repo: r.dir, Commits: []string{one, two}})
	if err != nil {
		t.Fatal(err)
	}
	got := paths(c)
	for p, want := range map[string]string{"old.go": "mod", "fresh.go": "new", "other.go": "mod", "gone.go": "del"} {
		if got[p].Status != want {
			t.Errorf("%s is %q, want %q", p, got[p].Status, want)
		}
	}
	if got["gone.go"].Text != nil {
		t.Error("a deleted file carries a text")
	}
	if _, ok := got["uv.lock"]; ok {
		t.Error("a file added and removed inside the turn is listed")
	}
	if len(c.Omitted) != 1 || c.Omitted[0] != (Omission{Path: "uv.lock", Reason: "net_zero"}) {
		t.Errorf("omitted = %v, want uv.lock net_zero", c.Omitted)
	}
}

// A code file past fullTextLimit carries its diff and not its text; a
// Markdown file is read up to markdownLimit; a diff past diffLimit is cut and
// says so.
func TestALargeFileCarriesOnlyItsChanges(t *testing.T) {
	r := newRepo(t)
	big := strings.Repeat("x = 1\n", fullTextLimit/6+10)
	r.write("big.py", big)
	r.write("doc.md", "# Doc\n\n"+strings.Repeat("A line of prose.\n", fullTextLimit/17+10))
	r.write("huge.txt", strings.Repeat("y\n", diffLimit))
	base := r.commit("big files")
	c, err := Collect(context.Background(), Options{Repo: r.dir, Commits: []string{base}})
	if err != nil {
		t.Fatal(err)
	}
	got := paths(c)
	if f := got["big.py"]; f.Text != nil || f.Withheld != "large" || len(f.Diffs) != 1 || f.Diffs[0].Diff == "" {
		t.Errorf("big.py: text=%v withheld=%q diffs=%d", f.Text != nil, f.Withheld, len(f.Diffs))
	}
	if f := got["doc.md"]; f.Text == nil || f.HTML == nil || f.Size <= fullTextLimit {
		t.Errorf("doc.md (%d bytes) is not carried whole: withheld=%q", f.Size, f.Withheld)
	}
	if f := got["huge.txt"]; f.Diffs[0].Cut != "long" || len(f.Diffs[0].Diff) != diffLimit {
		t.Errorf("huge.txt diff: cut=%q len=%d, want long and %d", f.Diffs[0].Cut, len(f.Diffs[0].Diff), diffLimit)
	}
}

// Over the report's limit the largest whole texts go first, then the largest
// diffs, and each is named.
func TestTheBudgetDropsTheLargestFirstAndSaysSo(t *testing.T) {
	text := func(n int) *string { s := strings.Repeat("a", n); return &s }
	c := &Collection{Files: []File{
		{Path: "small", Text: text(10), Diffs: []Diff{{Commit: "1", Diff: strings.Repeat("d", 10)}}},
		{Path: "large", Text: text(100), Diffs: []Diff{{Commit: "1", Diff: strings.Repeat("d", 50)}}},
	}}
	c.fitBudget(80)
	if c.Files[1].Text != nil || c.Files[1].Withheld != "budget" || c.Files[0].Text == nil {
		t.Fatalf("the largest text was not the one dropped: %+v", c.Files)
	}
	if c.Files[1].Diffs[0].Diff == "" {
		t.Errorf("a diff was dropped although dropping one text was enough: %+v", c.Files[1].Diffs)
	}
	c.fitBudget(80)
	want := []Omission{{"large", "budget_text"}}
	if len(c.Omitted) != 1 || c.Omitted[0] != want[0] {
		t.Errorf("omitted = %v, want %v", c.Omitted, want)
	}

	c = &Collection{Files: []File{
		{Path: "a", Text: text(10), Diffs: []Diff{{Commit: "1", Diff: strings.Repeat("d", 300)}}},
	}}
	c.fitBudget(100)
	if c.Files[0].Diffs[0].Cut != "budget" || len(c.Omitted) != 2 || c.Omitted[1].Reason != "budget_diff" {
		t.Errorf("diff over the budget: %+v, omitted %v", c.Files[0].Diffs, c.Omitted)
	}
}

// A quoted document cannot act: raw HTML is shown as text, an image is not
// loaded, a javascript: link has no target.
func TestMarkdownEscapesWhatAQuotedFileSays(t *testing.T) {
	src := "# Title\n\n<script>alert(1)</script>\n\nInline <img src=x onerror=alert(2)> here.\n\n" +
		"![logo](https://example.com/logo.png)\n\n[bad](javascript:alert(3))\n\n| a | b |\n|---|---|\n| 1 | ~~2~~ |\n"
	h, err := renderMarkdown([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"<script", "<img"} {
		if strings.Contains(h, bad) {
			t.Errorf("rendered Markdown carries %q:\n%s", bad, h)
		}
	}
	for _, want := range []string{"&lt;script&gt;alert(1)&lt;/script&gt;", "&lt;img src=x onerror=alert(2)&gt;", "<table>", "<del>2</del>", "[image: logo]"} {
		if !strings.Contains(h, want) {
			t.Errorf("rendered Markdown lacks %q:\n%s", want, h)
		}
	}
	if regexp.MustCompile(`<[a-z]+[^>]*\son[a-z]+=`).MatchString(h) {
		t.Errorf("an element carries an event handler:\n%s", h)
	}
	if strings.Contains(h, `href="javascript`) {
		t.Errorf("a javascript: link survived:\n%s", h)
	}
}

// The page loads nothing from anywhere, runs only its own script, and a file
// that says </script> cannot end the data early.
func TestTheReportLoadsNothingAndRunsOnlyItsOwnScript(t *testing.T) {
	r := newRepo(t)
	r.write("CLAUDE.md", "# Rules\n\n<script src=\"https://evil.example/x.js\"></script>\n\n![x](http://evil.example/p.png)\n")
	r.write("a.js", "var s = '</script><script>alert(1)</script>';\n")
	one := r.commit("one")
	c, err := Collect(context.Background(), Options{Repo: r.dir, Commits: []string{one}})
	if err != nil {
		t.Fatal(err)
	}
	st, err := ParseStatus("# T\n\n## ✅ Done\nIt `works` <script>x</script>.\n")
	if err != nil {
		t.Fatal(err)
	}
	page, err := Render(c, st, "zh-TW", "2026-10-05", "demo")
	if err != nil {
		t.Fatal(err)
	}
	s := string(page)
	external := regexp.MustCompile(`(?i)<(script|link|img|iframe|source|video|audio)\b[^>]*\b(src|href)\s*=\s*["']?(https?:)?//`)
	if m := external.FindString(s); m != "" {
		t.Errorf("the report loads an external resource: %s", m)
	}
	if n := strings.Count(s, "<script"); n != 2 {
		t.Errorf("the page has %d <script elements, want its data and its code", n)
	}
	if n := strings.Count(s, "</script>"); n != 2 {
		t.Errorf("the page has %d </script>, want 2: a quoted file closed one", n)
	}
	sum := sha256.Sum256([]byte(scriptBody(template)))
	if !strings.Contains(s, "script-src 'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'") {
		t.Error("the Content-Security-Policy does not name the page's own script")
	}
	if !strings.Contains(s, "default-src 'none'") {
		t.Error("the Content-Security-Policy does not refuse everything else")
	}
	if !strings.Contains(s, `"pins":["CLAUDE.md"]`) {
		t.Error("CLAUDE.md is not pinned")
	}
}

func TestCollectRefusesWhatItCannotTrust(t *testing.T) {
	r := newRepo(t)
	r.write("a", "1\n")
	r.commit("a")
	r.git("checkout", "-q", "-b", "side")
	r.write("b", "1\n")
	side := r.commit("b")
	r.git("checkout", "-q", "main")
	ctx := context.Background()
	if _, err := Collect(ctx, Options{Repo: r.dir, Commits: []string{side}}); !errors.Is(err, ErrNotAncestor) {
		t.Errorf("a commit HEAD does not contain: %v, want ErrNotAncestor", err)
	}
	if _, err := Collect(ctx, Options{Repo: r.dir, Commits: []string{"--output=/tmp/x"}}); err == nil {
		t.Error("an option passed as a commit was accepted")
	}
	if _, err := Collect(ctx, Options{Repo: r.dir}); err == nil {
		t.Error("no commits was accepted")
	}
	if _, err := Collect(ctx, Options{Repo: t.TempDir(), Commits: []string{"HEAD"}}); err == nil {
		t.Error("a directory outside any git project was accepted")
	}
}

func TestParseStatusAndNotes(t *testing.T) {
	st, err := ParseStatus("# Title\nunder it\n\n## ✅ One\nbody one\n\n```\n## not a card\n```\n## 🟡 Two\nbody two\n")
	if err != nil {
		t.Fatal(err)
	}
	if st.Title != "Title" || st.Subtitle != "under it" || len(st.Cards) != 2 ||
		st.Cards[0].Title != "✅ One" || !strings.Contains(st.Cards[0].Body, "## not a card") || st.Cards[1].Body != "body two" {
		t.Errorf("status = %+v", st)
	}
	if _, err := ParseStatus("# Only a title\n"); err == nil {
		t.Error("a status with no cards was accepted")
	}
	notes, err := ParseNotes("a/b.go: One sentence: with a colon.\n\n`c.md`: Two.\n")
	if err != nil {
		t.Fatal(err)
	}
	if notes["a/b.go"] != "One sentence: with a colon." || notes["c.md"] != "Two." {
		t.Errorf("notes = %v", notes)
	}
	if _, err := ParseNotes("no separator here\n"); err == nil {
		t.Error("a note line without `path: ` was accepted")
	}
}
