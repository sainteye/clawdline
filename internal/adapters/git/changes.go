package git

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// The part of `git status` a person reading the Git panel needs, kept apart
// from the process that obtains it — the same division the Swift app's
// `GitChanges` makes, for the same reason. Porcelain v2 has fixed fields and a
// tab-separated rename, and that is exactly where an off-by-one index becomes
// the wrong file on somebody's screen. Parsing it in one place lets those
// cases be pinned by a test without making a repository on disk.

// Kind is what happened to one file.
type Kind string

const (
	KindModified  Kind = "modified"
	KindAdded     Kind = "added"
	KindDeleted   Kind = "deleted"
	KindRenamed   Kind = "renamed"
	KindUntracked Kind = "untracked"
	KindConflict  Kind = "conflict"
)

// File is one changed path.
//
// Additions and Deletions are pointers because "no measurement" is a different
// fact from "a measurement of zero": an untracked file is in no diff at all,
// and a binary side of one is a dash. The panel draws those differently.
type File struct {
	Path      string
	From      string
	Staged    bool
	Unstaged  bool
	Kind      Kind
	Additions *int
	Deletions *int
}

// Status is the branch line and the changed files.
type Status struct {
	Branch string
	Head   string
	Ahead  int
	Behind int
	Files  []File
}

// The four ways this read can end. They are separated because they are four
// different sentences to the person looking at the panel, and collapsing them
// into one error would put "this is not a repository" and "git is wedged"
// behind the same words.
var (
	// ErrNotRepository is `git status` refusing: there is no repository here.
	ErrNotRepository = errors.New("not a git repository")
	// ErrFailed is git answering a later command with a non-zero status.
	ErrFailed = errors.New("could not read that repository")
	// ErrTimedOut is the read deadline passing with a command still running.
	ErrTimedOut = errors.New("that repository did not answer inside the git read deadline")
	// ErrUnavailable is there being no git on this machine's PATH. The Swift
	// app cannot tell this apart from an empty directory and answers "not a
	// repository" to both; on a machine with no git that sentence is false of
	// every directory on it, so this one is named instead of guessed.
	ErrUnavailable = errors.New("no git on this machine")
	// ErrTooLarge is the answer being bigger than this reads. A truncated
	// porcelain answer is not a smaller reading of the repository, it is a
	// reading that stops mid-line, so it is refused rather than parsed.
	ErrTooLarge = errors.New("that repository answered with more than this read takes")
	// ErrFileNotChanged keeps the diff route from becoming an arbitrary file
	// reader: it serves only a path in the status snapshot taken immediately
	// before the patch is read.
	ErrFileNotChanged = errors.New("that path is not a changed file in this repository")
)

// Patch is one side of a file's current change. A partly staged file has two
// patches so the index and worktree are not collapsed into one view.
type Patch struct {
	Scope       string
	UnifiedDiff string
}

// Diff is the patches for one path already named by Changes.
type Diff struct {
	Path    string
	Kind    Kind
	Patches []Patch
}

// changesTimeout is the whole read's budget, and no single command may take
// more than commandTimeout of it. The Swift route uses the same two numbers.
//
// outputLimit is how much of one command's answer is kept. Nothing else in
// this repository reads an unbounded stream — every file read goes through an
// `io.LimitReader` — and this one used to: `cmd.Output()` grows a buffer until
// the command stops, and a session whose working directory is a home folder
// answers `status --porcelain=v2` with every untracked path under it. Eight
// megabytes is far past any panel anybody reads and far short of a machine.
//
// waitDelay is the other half of the same stall. `exec.CommandContext` kills
// the process it started; a child git left behind that inherited the pipe
// keeps it open, and a `Wait` that is still copying will not return until it
// closes — past every timeout above. With a delay, `Wait` gives up on the copy
// instead of the request goroutine hanging for as long as the grandchild lives.
const (
	changesTimeout = 8 * time.Second
	commandTimeout = 5 * time.Second
	outputLimit    = 8 << 20
	waitDelay      = time.Second
)

// Changes reads one repository for the Git panel.
//
// Every invocation is read-only and lock-free: this is opened by a person
// pressing a menu item, and it must not leave an index lock behind on a
// checkout somebody else is working in. It is a reading taken now, with no
// cache — the panel asks again when it is opened again.
func (g *Git) Changes(ctx context.Context, cwd string) (Status, error) {
	ctx, cancel := context.WithTimeout(ctx, changesTimeout)
	defer cancel()

	// A non-zero status from the first command is the repository question
	// being answered: `git status` outside a work tree is the refusal, not a
	// failure. From the two diffs it is a failure, because by then this is a
	// repository and git still would not answer.
	// Name every untracked file. Git's default collapses an untracked directory
	// to one row ending in '/', but that row has no file patch for the panel to
	// open. The status line promises file diffs, so its authorization snapshot
	// must carry the actual leaf paths too.
	status, err := g.readOnly(ctx, cwd, "status", "--porcelain=v2", "--branch", "--untracked-files=all")
	if err != nil {
		if errors.Is(err, ErrTimedOut) || errors.Is(err, ErrUnavailable) || errors.Is(err, ErrTooLarge) {
			return Status{}, err
		}
		return Status{}, ErrNotRepository
	}
	unstaged, err := g.readOnly(ctx, cwd, "diff", "--numstat")
	if err != nil {
		return Status{}, err
	}
	staged, err := g.readOnly(ctx, cwd, "diff", "--cached", "--numstat")
	if err != nil {
		return Status{}, err
	}
	return Assemble(status, unstaged, staged), nil
}

// FileDiff reads the patch for one path in the repository's current status.
// The status lookup is also the authorization boundary: callers cannot use a
// read-level route to name an unchanged or arbitrary file under the session's
// working directory.
func (g *Git) FileDiff(ctx context.Context, cwd, path string) (Diff, error) {
	ctx, cancel := context.WithTimeout(ctx, changesTimeout)
	defer cancel()

	status, err := g.Changes(ctx, cwd)
	if err != nil {
		return Diff{}, err
	}
	var file *File
	for i := range status.Files {
		if status.Files[i].Path == path {
			file = &status.Files[i]
			break
		}
	}
	if file == nil {
		return Diff{}, ErrFileNotChanged
	}

	out := Diff{Path: file.Path, Kind: file.Kind, Patches: []Patch{}}
	read := func(scope string, allowDifference bool, args ...string) error {
		patch, err := g.readOnlyWithDifference(ctx, cwd, allowDifference, args...)
		if err != nil {
			return err
		}
		if patch != "" {
			out.Patches = append(out.Patches, Patch{Scope: scope, UnifiedDiff: patch})
		}
		return nil
	}
	if file.Kind == KindUntracked {
		if err := read("untracked", true, "diff", "--no-index", "--", "/dev/null", file.Path); err != nil {
			return Diff{}, err
		}
		return out, nil
	}
	if file.Staged {
		if err := read("staged", false, "diff", "--cached", "--", file.Path); err != nil {
			return Diff{}, err
		}
	}
	if file.Unstaged || file.Kind == KindConflict {
		if err := read("unstaged", false, "diff", "--", file.Path); err != nil {
			return Diff{}, err
		}
	}
	return out, nil
}

// readOnly runs one bounded git command and returns its stdout.
func (g *Git) readOnly(ctx context.Context, cwd string, args ...string) (string, error) {
	return g.readOnlyWithDifference(ctx, cwd, false, args...)
}

// readOnlyWithDifference allows git diff --no-index's documented exit 1:
// unlike every other command here, one means that it successfully found the
// difference it was asked to print.
func (g *Git) readOnlyWithDifference(ctx context.Context, cwd string, allowDifference bool, args ...string) (string, error) {
	if ctx.Err() != nil {
		return "", ErrTimedOut
	}
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	// **This reading of somebody's repository runs git, and nothing that
	// repository asked to have run.** A directory is opened here because a
	// session is sitting in it and somebody pressed a menu item — a device
	// that may only read is enough — and the repository under it may have been
	// cloned by an agent an hour ago. Several git settings name a program and
	// git runs it: `core.fsmonitor` during `status`, an external driver or a
	// `textconv` filter during a diff. So they are turned off on the command
	// line, where they outrank the repository's own `.git/config`, and the
	// system-wide file is left out with them. This is the rule rather than the
	// one spelling somebody found: what may decide this reading is this
	// process's own argument list, and nothing written inside the directory
	// being read.
	if len(args) == 0 {
		return "", ErrFailed
	}
	full := []string{"-c", "core.fsmonitor=", args[0]}
	if args[0] == "diff" {
		full = append(full, "--no-ext-diff", "--no-textconv")
	}
	full = append(full, args[1:]...)
	cmd := exec.CommandContext(ctx, g.Binary, full...)
	cmd.Dir = cwd
	// GIT_OPTIONAL_LOCKS=0 is what keeps this lock-free: without it `status`
	// refreshes the index and takes index.lock, which is the one thing a
	// panel opening in the background must never do to somebody's checkout.
	//
	// The C locale is not decoration either. This output is parsed, git ships
	// translations, and a machine with them installed renames every word these
	// readers match on. The porcelain formats are stable by contract; this
	// makes that true of the whole invocation rather than of the flags
	// somebody remembered to pass.
	//
	// GIT_CONFIG_NOSYSTEM keeps /etc/gitconfig out of a reading that is not
	// this machine's administrator asking anything.
	cmd.Env = append(cmd.Environ(),
		"GIT_OPTIONAL_LOCKS=0", "LC_ALL=C", "LANG=C", "GIT_CONFIG_NOSYSTEM=1")
	out := &capped{limit: outputLimit}
	cmd.Stdout = out
	// stderr is read by nobody here: the refusals below are this package's own
	// sentences, and git's are not shown to a paired device.
	cmd.Stderr = nil
	cmd.WaitDelay = waitDelay
	err := cmd.Run()
	if out.over {
		return "", ErrTooLarge
	}
	if err != nil {
		if ctx.Err() != nil {
			return "", ErrTimedOut
		}
		// Nothing ran, so nothing was learned about this directory.
		if errors.Is(err, exec.ErrNotFound) {
			return "", ErrUnavailable
		}
		if allowDifference {
			var exit *exec.ExitError
			if errors.As(err, &exit) && exit.ExitCode() == 1 {
				return out.String(), nil
			}
		}
		return "", ErrFailed
	}
	return out.String(), nil
}

// capped is a bounded sink for one command's stdout: it keeps what fits and
// remembers that there was more, so the caller refuses rather than parsing
// half a line as a whole one.
type capped struct {
	buf   strings.Builder
	limit int
	over  bool
}

func (c *capped) Write(p []byte) (int, error) {
	if c.over {
		return len(p), nil
	}
	if c.buf.Len()+len(p) > c.limit {
		c.over = true
		return len(p), errors.New("that repository answered with more than this read takes")
	}
	return c.buf.Write(p)
}

func (c *capped) String() string { return c.buf.String() }

// Assemble joins the two numstat views onto the status rows. It is pure, so a
// partially staged file or a binary one can be pinned without a repository.
func Assemble(status, unstaged, staged string) Status {
	out := ParseStatus(status)
	worktree := ParseNumstat(unstaged)
	index := ParseNumstat(staged)
	for i := range out.Files {
		path := out.Files[i].Path
		w, inWorktree := worktree[path]
		x, inIndex := index[path]
		var counts *numstat
		switch {
		case inWorktree && inIndex:
			counts = merge(&w, &x)
		case inWorktree:
			counts = &w
		case inIndex:
			counts = &x
		}
		if counts != nil {
			out.Files[i].Additions = counts.additions
			out.Files[i].Deletions = counts.deletions
		}
	}
	return out
}

// numstat is one file's two counts, either of which may be unknown.
type numstat struct {
	additions *int
	deletions *int
}

// ParseStatus reads `git status --porcelain=v2 --branch`.
func ParseStatus(text string) Status {
	var out Status
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, "# branch.oid "):
			value := strings.TrimPrefix(line, "# branch.oid ")
			// A repository with no commit yet says `(initial)`, which is not a
			// commit id and must not be shown as one.
			if value != "(initial)" {
				out.Head = value
			}
		case strings.HasPrefix(line, "# branch.head "):
			out.Branch = strings.TrimPrefix(line, "# branch.head ")
		case strings.HasPrefix(line, "# branch.ab "):
			for _, word := range strings.Fields(strings.TrimPrefix(line, "# branch.ab ")) {
				if n, err := strconv.Atoi(word[1:]); err == nil {
					switch word[0] {
					case '+':
						out.Ahead = n
					case '-':
						out.Behind = n
					}
				}
			}
		case strings.HasPrefix(line, "1 "):
			// `1 XY sub mH mI mW hH hI path`: only the path may contain spaces.
			fields := strings.SplitN(line, " ", 9)
			if len(fields) != 9 {
				continue
			}
			xy := fields[1]
			out.Files = append(out.Files, file(fields[8], "", xy, kindOf(xy)))
		case strings.HasPrefix(line, "2 "):
			// A rename's last field is `new path<TAB>old path`; spaces in either
			// are ordinary path characters, which is why the split stops before it.
			fields := strings.SplitN(line, " ", 10)
			if len(fields) != 10 {
				continue
			}
			paths := strings.SplitN(fields[9], "\t", 2)
			if len(paths) != 2 {
				continue
			}
			out.Files = append(out.Files, file(paths[0], paths[1], fields[1], KindRenamed))
		case strings.HasPrefix(line, "u "):
			// `u XY sub m1 m2 m3 mW h1 h2 h3 path`.
			fields := strings.SplitN(line, " ", 11)
			if len(fields) != 11 {
				continue
			}
			out.Files = append(out.Files, file(fields[10], "", fields[1], KindConflict))
		case strings.HasPrefix(line, "? "):
			out.Files = append(out.Files, File{
				Path: strings.TrimPrefix(line, "? "), Unstaged: true, Kind: KindUntracked,
			})
		}
	}
	return out
}

// ParseNumstat reads `git diff --numstat`. A dash is git's answer for a binary
// side of a diff; once one side is unknowable both figures stay nil rather
// than presenting half a measurement.
func ParseNumstat(text string) map[string]numstat {
	out := map[string]numstat{}
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			continue
		}
		path := renamedDestination(fields[2])
		next := numstat{additions: count(fields[0]), deletions: count(fields[1])}
		if prior, seen := out[path]; seen {
			out[path] = *merge(&prior, &next)
			continue
		}
		out[path] = next
	}
	return out
}

func count(field string) *int {
	n, err := strconv.Atoi(field)
	if err != nil {
		return nil
	}
	return &n
}

func merge(first, second *numstat) *numstat {
	if first == nil {
		return second
	}
	if second == nil {
		return first
	}
	if first.additions == nil || second.additions == nil ||
		first.deletions == nil || second.deletions == nil {
		return &numstat{}
	}
	a := *first.additions + *second.additions
	d := *first.deletions + *second.deletions
	return &numstat{additions: &a, deletions: &d}
}

func file(path, from, xy string, kind Kind) File {
	states := []rune(xy)
	x, y := '.', '.'
	if len(states) > 0 {
		x = states[0]
	}
	if len(states) > 1 {
		y = states[1]
	}
	return File{Path: path, From: from, Staged: x != '.', Unstaged: y != '.', Kind: kind}
}

func kindOf(xy string) Kind {
	switch {
	case strings.Contains(xy, "U"):
		return KindConflict
	case strings.Contains(xy, "R"), strings.Contains(xy, "C"):
		return KindRenamed
	case strings.Contains(xy, "A"):
		return KindAdded
	case strings.Contains(xy, "D"):
		return KindDeleted
	}
	return KindModified
}

// renamedDestination expands numstat's abbreviation of a rename. It writes
// either `old => new` or `dir/{old => new}.ext`; status names the destination,
// so only that side is expanded before the two answers are joined.
func renamedDestination(path string) string {
	arrow := strings.Index(path, " => ")
	if arrow < 0 {
		return path
	}
	before, after := path[:arrow], path[arrow+len(" => "):]
	open := strings.LastIndex(before, "{")
	closed := strings.Index(after, "}")
	if open >= 0 && closed >= 0 {
		return before[:open] + after[:closed] + after[closed+1:]
	}
	return after
}
