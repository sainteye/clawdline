// Package turnreport builds the status report a Session leaves at the end of a
// turn: one HTML file, opened from disk, that says where the work stands and
// lets the person read every file the turn added or changed.
//
// It is a local file and nothing else. The daemon does not serve it and Cloud
// never carries it: internal/adapters/documents keeps HTML out of what it will
// show, and this package does not change that.
//
// The file list comes from the commits the caller names, one at a time, never
// from a range. A shared main interleaves other Sessions' commits with this
// turn's, and a range diff would put their files in this turn's report — the
// prototype this was built from switched to per-commit reads for exactly that
// reason.
package turnreport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
)

// The report's bounds. Each is checked here and said in the report when it
// cut something.
const (
	// fullTextLimit is the largest file whose whole text the report carries;
	// a larger one shows only what the turn changed. The prototype's 70 KB.
	fullTextLimit = 70_000
	// markdownLimit is the same for a Markdown file, which is what the
	// person reads rather than skims: the prototype rendered a 158 KB
	// design document whole, and a pinned document that only shows a diff
	// is the report failing at its job.
	markdownLimit = 1 << 20
	// diffLimit is the most of one commit's diff of one file the report
	// carries; past it the diff is cut and says so.
	diffLimit = 256 << 10
	// reportLimit is the most file text and diff one report carries. Past it
	// the largest files' whole texts go first, then the largest diffs, and
	// each is listed as cut.
	reportLimit = 12 << 20
	// commitsLimit is the most commits one report reads.
	commitsLimit = 500
)

// Options is what the caller decides; nothing in it is guessed.
type Options struct {
	// Repo is any directory inside the git project.
	Repo string
	// Commits are this turn's commits, oldest first.
	Commits []string
	// At is the revision whose file contents the report shows; HEAD when
	// empty. Every commit must be an ancestor of it.
	At string
	// Exclude are paths left out on purpose, each named in the report.
	Exclude []string
	// Notes are one sentence per path, shown above the file.
	Notes map[string]string
	// Pins are paths shown as buttons above the tree; the first one present
	// opens first. Empty means DefaultPins.
	Pins []string
}

// DefaultPins are the files a reader of this kind of report opens first.
var DefaultPins = []string{"CLAUDE.md", "AGENTS.md"}

// File is one path the turn added, changed or deleted.
type File struct {
	Path    string   `json:"path"`
	Status  string   `json:"status"` // "new", "mod" or "del"
	Size    int64    `json:"size"`
	Commits []string `json:"commits"`
	Note    string   `json:"note,omitempty"`
	Kind    string   `json:"kind"` // "md" or "code"
	Diffs   []Diff   `json:"diffs"`
	Text    *string  `json:"text,omitempty"`
	HTML    *string  `json:"html,omitempty"`
	// Withheld says why Text is absent from a file that exists: "large",
	// "binary" or "budget".
	Withheld string `json:"withheld,omitempty"`
}

// Diff is one commit's change to one file.
type Diff struct {
	Commit  string `json:"commit"`
	Subject string `json:"subject"`
	Diff    string `json:"diff"`
	// Cut is "" for a whole diff, "long" when it passed diffLimit and
	// "budget" when the report's total took it.
	Cut string `json:"cut,omitempty"`
}

// Omission is a path the report does not list, or lists with less, and why.
type Omission struct {
	Path   string `json:"path"`
	Reason string `json:"reason"` // "net_zero", "excluded", "budget_text", "budget_diff"
}

// Collection is everything read from git.
type Collection struct {
	Root     string            `json:"-"`
	At       string            `json:"at"`
	Order    []string          `json:"order"`
	Subjects map[string]string `json:"subjects"`
	Files    []File            `json:"files"`
	Omitted  []Omission        `json:"omitted"`
	Pins     []string          `json:"pins"`
}

// ErrNotAncestor is a commit the shown revision does not contain.
var ErrNotAncestor = errors.New("is not an ancestor of the shown revision")

// Collect reads the named commits, each on its own, and the files they touched.
func Collect(ctx context.Context, opt Options) (*Collection, error) {
	if len(opt.Commits) == 0 {
		return nil, errors.New("no commits: name this turn's commits, oldest first")
	}
	if len(opt.Commits) > commitsLimit {
		return nil, fmt.Errorf("%d commits; one report reads at most %d", len(opt.Commits), commitsLimit)
	}
	g := gitRunner{dir: opt.Repo}
	top, err := g.out(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("%s is not inside a git project: %w", opt.Repo, err)
	}
	g.dir = strings.TrimSpace(top)
	at := opt.At
	if at == "" {
		at = "HEAD"
	}
	atSHA, err := g.commit(ctx, at)
	if err != nil {
		return nil, err
	}
	c := &Collection{Root: g.dir, At: atSHA, Subjects: map[string]string{}}
	seen := map[string]bool{}
	type touch struct {
		short   string
		pathset []string // the pathspec that shows this commit's change: the path, and a rename's old path
	}
	touched := map[string][]touch{}
	for _, raw := range opt.Commits {
		sha, err := g.commit(ctx, raw)
		if err != nil {
			return nil, err
		}
		if seen[sha] {
			continue
		}
		seen[sha] = true
		if _, err := g.out(ctx, "merge-base", "--is-ancestor", sha, atSHA); err != nil {
			return nil, fmt.Errorf("commit %s %w %s", raw, ErrNotAncestor, at)
		}
		short, err := g.out(ctx, "rev-parse", "--short", sha)
		if err != nil {
			return nil, err
		}
		short = strings.TrimSpace(short)
		subject, err := g.out(ctx, "log", "-1", "--format=%s", sha)
		if err != nil {
			return nil, err
		}
		c.Order = append(c.Order, short)
		c.Subjects[short] = strings.TrimSpace(subject)
		names, err := g.out(ctx, "show", "--name-status", "-z", "--format=", "--no-color", sha)
		if err != nil {
			return nil, err
		}
		for _, ch := range parseNameStatus(names) {
			touched[ch.path] = append(touched[ch.path], touch{short: short, pathset: ch.pathset})
		}
	}
	excluded := map[string]bool{}
	for _, p := range opt.Exclude {
		excluded[p] = true
	}
	paths := make([]string, 0, len(touched))
	for p := range touched {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if excluded[p] {
			c.Omitted = append(c.Omitted, Omission{Path: p, Reason: "excluded"})
			continue
		}
		ts := touched[p]
		first := ts[0].short
		before := g.exists(ctx, first+"^", p)
		now := g.exists(ctx, atSHA, p)
		f := File{Path: p, Note: opt.Notes[p], Kind: kindOf(p)}
		switch {
		case !before && !now:
			// Added and taken out again inside the turn: no net change.
			c.Omitted = append(c.Omitted, Omission{Path: p, Reason: "net_zero"})
			continue
		case !now:
			f.Status = "del"
		case before:
			f.Status = "mod"
		default:
			f.Status = "new"
		}
		for _, t := range ts {
			f.Commits = append(f.Commits, t.short)
			args := append([]string{"show", "--format=", "--no-color", "--no-ext-diff", "--no-textconv", "--unified=3", t.short, "--"}, t.pathset...)
			d, cut, err := g.bounded(ctx, diffLimit, args...)
			if err != nil {
				return nil, err
			}
			diff := Diff{Commit: t.short, Subject: c.Subjects[t.short], Diff: d}
			if cut {
				diff.Cut = "long"
			}
			f.Diffs = append(f.Diffs, diff)
		}
		if now {
			if err := g.fillText(ctx, &f, atSHA); err != nil {
				return nil, err
			}
		}
		c.Files = append(c.Files, f)
	}
	present := map[string]bool{}
	for _, f := range c.Files {
		present[f.Path] = true
	}
	pins := opt.Pins
	if len(pins) == 0 {
		pins = DefaultPins
	}
	for _, p := range pins {
		if present[p] {
			c.Pins = append(c.Pins, p)
		}
	}
	c.fitBudget(reportLimit)
	return c, nil
}

// Has says whether the report lists path.
func (c *Collection) Has(path string) bool {
	for _, f := range c.Files {
		if f.Path == path {
			return true
		}
	}
	return false
}

// fillText puts a file's text, and for Markdown its rendering, in the entry,
// unless it is binary or larger than its limit.
func (g gitRunner) fillText(ctx context.Context, f *File, at string) error {
	spec := at + ":" + f.Path
	sz, err := g.out(ctx, "cat-file", "-s", spec)
	if err != nil {
		return err
	}
	f.Size, _ = strconv.ParseInt(strings.TrimSpace(sz), 10, 64)
	limit := fullTextLimit
	if f.Kind == "md" {
		limit = markdownLimit
	}
	if f.Size > int64(limit) {
		f.Withheld = "large"
		return nil
	}
	body, _, err := g.bounded(ctx, limit, "cat-file", "blob", spec)
	if err != nil {
		return err
	}
	head := body
	if len(head) > 8000 {
		head = head[:8000]
	}
	if strings.IndexByte(head, 0) >= 0 {
		f.Withheld = "binary"
		return nil
	}
	f.Text = &body
	if f.Kind == "md" {
		h, err := renderMarkdown([]byte(body))
		if err != nil {
			return fmt.Errorf("rendering %s: %w", f.Path, err)
		}
		f.HTML = &h
	}
	return nil
}

// fitBudget keeps the carried text and diffs under limit: the largest
// whole texts go first, then the largest diffs, each named in Omitted.
func (c *Collection) fitBudget(limit int) {
	total := 0
	for _, f := range c.Files {
		total += fileWeight(f)
	}
	if total <= limit {
		return
	}
	order := make([]int, len(c.Files))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return textWeight(c.Files[order[a]]) > textWeight(c.Files[order[b]])
	})
	for _, i := range order {
		if total <= limit {
			break
		}
		f := &c.Files[i]
		w := textWeight(*f)
		if w == 0 {
			continue
		}
		total -= w
		f.Text, f.HTML, f.Withheld = nil, nil, "budget"
		c.Omitted = append(c.Omitted, Omission{Path: f.Path, Reason: "budget_text"})
	}
	type ref struct{ file, diff int }
	var diffs []ref
	for i, f := range c.Files {
		for j := range f.Diffs {
			diffs = append(diffs, ref{i, j})
		}
	}
	sort.SliceStable(diffs, func(a, b int) bool {
		return len(c.Files[diffs[a].file].Diffs[diffs[a].diff].Diff) > len(c.Files[diffs[b].file].Diffs[diffs[b].diff].Diff)
	})
	for _, r := range diffs {
		if total <= limit {
			break
		}
		d := &c.Files[r.file].Diffs[r.diff]
		total -= len(d.Diff)
		d.Diff, d.Cut = "", "budget"
		c.Omitted = append(c.Omitted, Omission{Path: c.Files[r.file].Path + " @ " + d.Commit, Reason: "budget_diff"})
	}
}

func textWeight(f File) int {
	n := 0
	if f.Text != nil {
		n += len(*f.Text)
	}
	if f.HTML != nil {
		n += len(*f.HTML)
	}
	return n
}

func fileWeight(f File) int {
	n := textWeight(f)
	for _, d := range f.Diffs {
		n += len(d.Diff)
	}
	return n
}

// kindOf is "md" for a Markdown file, which the report renders, and "code"
// for everything else, which it shows as text.
func kindOf(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".md", ".markdown":
		return "md"
	}
	return "code"
}

type change struct {
	path    string
	pathset []string
}

// parseNameStatus reads `git show --name-status -z`: a status, then one path,
// or two for a rename or copy, each ended by NUL.
func parseNameStatus(out string) []change {
	fields := strings.Split(out, "\x00")
	var res []change
	for i := 0; i < len(fields); i++ {
		st := strings.TrimSpace(fields[i])
		if st == "" {
			continue
		}
		switch st[0] {
		case 'R', 'C':
			if i+2 >= len(fields) {
				return res
			}
			old, nw := fields[i+1], fields[i+2]
			i += 2
			set := []string{nw}
			if st[0] == 'R' {
				set = append(set, old)
			}
			res = append(res, change{path: nw, pathset: set})
		default:
			if i+1 >= len(fields) {
				return res
			}
			p := fields[i+1]
			i++
			res = append(res, change{path: p, pathset: []string{p}})
		}
	}
	return res
}

// gitRunner runs git in one directory, with pathspecs read literally.
type gitRunner struct{ dir string }

func (g gitRunner) cmd(ctx context.Context, args ...string) *exec.Cmd {
	full := append([]string{"--literal-pathspecs", "-C", g.dir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Env = append(cmd.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_PAGER=cat")
	return cmd
}

func (g gitRunner) out(ctx context.Context, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := g.cmd(ctx, args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", fmt.Errorf("git %s: %w", args[0], err)
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return stdout.String(), nil
}

// bounded is out, keeping at most limit bytes and saying whether it cut.
func (g gitRunner) bounded(ctx context.Context, limit int, args ...string) (string, bool, error) {
	var stderr bytes.Buffer
	w := &capped{limit: limit}
	cmd := g.cmd(ctx, args...)
	cmd.Stdout, cmd.Stderr = w, &stderr
	if err := cmd.Run(); err != nil {
		return "", false, fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(stderr.String()))
	}
	return w.buf.String(), w.cut, nil
}

// commit resolves a name the caller gave to a full commit id. A name that
// begins with "-" is refused before git can read it as an option.
func (g gitRunner) commit(ctx context.Context, name string) (string, error) {
	if name == "" || strings.HasPrefix(name, "-") {
		return "", fmt.Errorf("%q is not a commit", name)
	}
	sha, err := g.out(ctx, "rev-parse", "--verify", "--quiet", name+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("%q is not a commit in %s", name, g.dir)
	}
	return strings.TrimSpace(sha), nil
}

// exists says whether rev holds path.
func (g gitRunner) exists(ctx context.Context, rev, p string) bool {
	return g.cmd(ctx, "cat-file", "-e", rev+":"+p).Run() == nil
}

// capped keeps the first limit bytes written to it and discards the rest.
type capped struct {
	buf   bytes.Buffer
	limit int
	cut   bool
}

func (c *capped) Write(p []byte) (int, error) {
	room := c.limit - c.buf.Len()
	if room <= 0 {
		if len(p) > 0 {
			c.cut = true
		}
		return len(p), nil
	}
	if len(p) > room {
		c.buf.Write(p[:room])
		c.cut = true
		return len(p), nil
	}
	c.buf.Write(p)
	return len(p), nil
}
