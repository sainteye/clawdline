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
)

// changesTimeout is the whole read's budget, and no single command may take
// more than commandTimeout of it. The Swift route uses the same two numbers.
const (
	changesTimeout = 8 * time.Second
	commandTimeout = 5 * time.Second
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
	status, err := g.readOnly(ctx, cwd, "status", "--porcelain=v2", "--branch")
	if err != nil {
		if errors.Is(err, ErrTimedOut) || errors.Is(err, ErrUnavailable) {
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

// readOnly runs one bounded git command and returns its stdout.
func (g *Git) readOnly(ctx context.Context, cwd string, args ...string) (string, error) {
	if ctx.Err() != nil {
		return "", ErrTimedOut
	}
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, g.Binary, args...)
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
	cmd.Env = append(cmd.Environ(), "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C", "LANG=C")
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ErrTimedOut
		}
		// Nothing ran, so nothing was learned about this directory.
		if errors.Is(err, exec.ErrNotFound) {
			return "", ErrUnavailable
		}
		return "", ErrFailed
	}
	return string(out), nil
}

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
