// Package documents publishes the Markdown and text files a project and its
// tasks wrote down, and refuses everything else.
//
// It is a port of the Swift app's `ProjectDocuments` (Sources/ProjectArtifact.swift),
// rule for rule, because this is a route that reads files off this machine's
// disk on behalf of a paired device: the only thing standing between a name a
// phone typed and `~/.ssh/id_ed25519` is the set of clauses below, and a
// clause invented here that the original does not have — or missing here that
// the original does have — is a hole nobody would see in a screenshot.
//
// The boundary, in the original's own words: a name that arrives from a device
// may only choose *within* a root this code computed, the root is settled
// before the name is looked at, the file must be one of three extensions that
// carry no active content, and it must be under the size cap.
//
// What that premise does not cover is stated there too and is inherited here:
// somebody who can also *write* inside a root is not bounded by it (a hard link
// to a file outside, or a name replaced by a symlink between the check and the
// read), because both cost a local write into a directory whose contents this
// route exists to publish.
package documents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Text, and nothing a browser will execute. HTML is deliberately absent: a
// directory anybody may drop a file into is not a place to serve a program
// from.
var readableExtensions = map[string]bool{"md": true, "markdown": true, "txt": true}

const (
	// MaximumBytes is `document-links.js`'s DOCUMENT_MAX_BYTES as well: the
	// page refuses an answer larger than this, so serving one would be a byte
	// count nobody can read.
	MaximumBytes = 2 * 1024 * 1024
	// MaximumListed keeps a listing a menu rather than a backup, and
	// MaximumWalked keeps a walk that never ends from being a denial of
	// service against the queue this shares with the rest of the daemon.
	MaximumListed = 200
	MaximumWalked = 4000
	MaximumDepth  = 6
	// MaximumTasksListed is newest first, and only as far back as somebody
	// would scroll.
	MaximumTasksListed = 60
	maximumPath        = 512
)

// Refusal is a typed reason a document is not being served. Its string is the
// wire code; `Status` is the status the original answers with.
type Refusal string

const (
	NotFound Refusal = "document_not_found"
	TooLarge Refusal = "document_too_large"
)

func (r Refusal) Error() string { return string(r) }

func (r Refusal) Status() int {
	if r == TooLarge {
		return 413
	}
	return 404
}

func (r Refusal) Message() string {
	if r == TooLarge {
		return "That document is too large to serve."
	}
	return "No document named that."
}

// Document is one row of a listing.
type Document struct {
	Path     string
	Bytes    int64
	Modified float64
}

// Located is one document that may be read, and what was true of it when it
// was checked.
type Located struct {
	Path     string
	Full     string
	Bytes    int64
	Modified float64
}

// ResolvedPath is the one place a path is normalised, so both sides of a
// containment test are normalised the same way.
//
// **It has to be a fixed point**, and Go's `filepath.EvalSymlinks` is one:
// unlike Foundation's `resolvingSymlinksInPath()`, which strips a leading
// `/private`, this walks *towards* the real file, so on this Mac `/tmp` comes
// back as `/private/tmp` and `/private/tmp` comes back as itself. `Walk` below
// depends on that: it resolves the root once and then resolves every entry the
// walk hands back, one normalisation more than the base got, and against an
// alternating call no entry would begin with its own root and every listing
// would come back empty.
//
// A path that does not exist cannot be resolved, so the cleaned path is
// returned instead and the caller's own existence check answers. That is the
// same shape the original has, where an absent file resolves to itself.
func ResolvedPath(path string) string {
	if path == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

// IsInside reports whether one already-resolved path is *below* another. The
// trailing separator is the whole of it: without it `…/artifacts-elsewhere`
// passes as being inside `…/artifacts`.
func IsInside(path, base string) bool {
	if base == "" || path == "" {
		return false
	}
	sep := string(filepath.Separator)
	if !strings.HasSuffix(base, sep) {
		base += sep
	}
	return strings.HasPrefix(path, base)
}

// ProjectRoot is the project's root: `<session cwd>/artifacts`. The empty
// string when there is no such directory, which is the ordinary case.
//
// **The symlink is followed, and there is a floor under it.** The app being
// replicated follows it unconditionally (`ProjectArtifact.projectRoot`, "with
// its symlink followed") and its reasoning is written down: a person put that
// symlink there to say "my documents live over there", so what it resolves to
// becomes the root. That is a real arrangement on this machine —
// `~/code/clawdline/artifacts -> ../clawdline-cloud/artifacts`, and the
// documents page reads 101 rows through it — so following stays the default.
//
// What does not hold here is the assumption underneath it, that a person is
// the only one writing in a session's working directory: this daemon dispatches
// agents that work in those directories, and `ln -s ~ artifacts` would hand
// every `.md`, `.markdown` and `.txt` within `MaximumDepth` of a home
// directory to any paired device that may only read. So the floor: **a project
// root may point elsewhere, but not at somewhere that contains the person
// asking.** A directory that holds the home directory, the session's own
// working directory, or the whole filesystem is not "this project's
// documents", it is everything, and it is refused in both modes.
//
// `contain` is the stricter rule — the enclosure `TaskRoot` applies, where the
// resolved root must still be inside the directory it was named from — and it
// is what `documents_contain_project_root: true` in this daemon's own
// `config.json` asks for, on a machine that would rather lose the symlink than
// trust it.
func ProjectRoot(directory string, contain bool) string {
	if contain {
		return root(directory, directory)
	}
	path := root(directory, "")
	if path == "" || holdsTheAsker(path, directory) {
		return ""
	}
	return path
}

// holdsTheAsker is the floor under a followed project root: whether what it
// resolved to contains the session's own working directory, the home
// directory, or is the filesystem root.
func holdsTheAsker(path, directory string) bool {
	if path == "" {
		return true
	}
	if filepath.Dir(path) == path {
		return true
	}
	if IsInside(ResolvedPath(directory), path) {
		return true
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		resolvedHome := ResolvedPath(home)
		if path == resolvedHome || IsInside(resolvedHome, path) {
			return true
		}
	}
	return false
}

// TaskRoot is a task's root: `<task directory>/artifacts`, and **a symlink that
// leaves the task directory is not followed** — that root is empty instead.
//
// The two roots are the same directory name resolved the same way, and they
// are not the same claim. Nobody puts the symlink here: the task directory is
// computed from an id that cannot hold a dot, and the `artifacts` directory
// inside it is created by the child whose deliverables it holds. So
// `artifacts -> /somewhere/else` under a task directory is not a person saying
// where their documents are kept; it is the one party this boundary exists to
// bound choosing a new root, after which every `md`, `markdown` and `txt`
// beneath whatever it names is readable by any paired device.
func TaskRoot(directory string) string {
	return root(directory, directory)
}

// root is both of those, with the one clause that separates them: a non-empty
// `enclosure` means the resolved root must still be inside that resolved
// directory.
func root(directory, enclosure string) string {
	if directory == "" {
		return ""
	}
	path := ResolvedPath(filepath.Join(directory, "artifacts"))
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return ""
	}
	if enclosure != "" {
		base := ResolvedPath(enclosure)
		if !IsInside(path, base) {
			return ""
		}
	}
	return path
}

// File is one document inside one root, or the reason it is not being served.
//
// The clauses are the original's and in its order, so that `..`, `.`, every
// dotfile, an absolute path, an over-deep path and an extension outside the
// allowlist are all refused *before* the filesystem is asked anything, and the
// containment test is made against resolved paths afterwards.
func File(root, name string) (Located, error) {
	if root == "" {
		return Located{}, NotFound
	}
	if name == "" || len(name) > maximumPath || strings.Contains(name, "\x00") ||
		strings.HasPrefix(name, "/") {
		return Located{}, NotFound
	}
	segments := strings.Split(name, "/")
	if len(segments) == 0 || len(segments) > MaximumDepth {
		return Located{}, NotFound
	}
	for _, segment := range segments {
		// `..`, `.` and every dotfile in one clause.
		if segment == "" || strings.HasPrefix(segment, ".") {
			return Located{}, NotFound
		}
		// The original runs on one operating system, where `/` is the only
		// separator. This does not: on Windows a segment holding a backslash
		// is a second separator that the split above never saw, so it is
		// refused here rather than left for the containment test to catch.
		if strings.ContainsRune(segment, filepath.Separator) || strings.Contains(segment, `\`) {
			return Located{}, NotFound
		}
	}
	relative := strings.Join(segments, "/")
	if !readableExtensions[extensionOf(relative)] {
		return Located{}, NotFound
	}
	candidate := root
	for _, segment := range segments {
		candidate = filepath.Join(candidate, segment)
	}
	base := ResolvedPath(root)
	resolved := ResolvedPath(candidate)
	if !IsInside(resolved, base) {
		return Located{}, NotFound
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return Located{}, NotFound
	}
	if info.Size() > MaximumBytes {
		return Located{}, TooLarge
	}
	return Located{
		Path:     relative,
		Full:     resolved,
		Bytes:    info.Size(),
		Modified: float64(info.ModTime().UnixNano()) / 1e9,
	}, nil
}

// Read is File and then the bytes, with the media type the extension chooses.
//
// A file that vanished or changed shape between the two is the original's
// `document_not_found`: the check said what was true of a name, and the read
// is what is true of it now.
func Read(root, name string) (Located, string, []byte, error) {
	located, err := File(root, name)
	if err != nil {
		return Located{}, "", nil, err
	}
	body, err := os.ReadFile(located.Full)
	if err != nil {
		return Located{}, "", nil, NotFound
	}
	return located, MediaType(located.Path), body, nil
}

// MediaType is the original's one-line choice: `txt` is plain, and the other
// two are Markdown. Both are the exact strings `document-links.js` accepts.
func MediaType(path string) string {
	if extensionOf(path) == "txt" {
		return "text/plain; charset=utf-8"
	}
	return "text/markdown; charset=utf-8"
}

// Walk is what is in a root, newest first.
//
// **The listing is a subset of what the read will serve, and only that
// direction holds.** Every candidate goes back through `File`, so a listing
// cannot offer a document the read would then refuse — which is what makes the
// list safe to hand to a page that turns each row into a link. The other
// direction is deliberately false: past `MaximumListed` the rows are cut while
// the documents behind them stay readable, so a page that draws this list is
// drawing a menu, and a menu is allowed to be shorter than the kitchen.
//
// **It takes the caller's context because it is somebody's request.** One
// listing is up to `MaximumTasksListed` roots of `MaximumWalked` entries, and
// every entry costs a `ResolvedPath` — an `EvalSymlinks`, which is a syscall
// per component. Without the context that whole walk carried on after the
// phone that asked had closed the tab, on the request's own goroutine, and the
// answer went nowhere. Now it stops where the request stopped.
func Walk(ctx context.Context, root string) ([]Document, Cut) {
	if root == "" {
		return nil, Cut{}
	}
	base := ResolvedPath(root)
	prefix := base
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	out := []Document{}
	walked := 0
	var cut Cut
	stop := errors.New("walked enough")
	err := filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			// One unreadable directory is not the end of the listing, as the
			// original's enumerator carries on past one too.
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path == base {
			return nil
		}
		if ctx.Err() != nil {
			return stop
		}
		// `.skipsHiddenFiles`, which in the original skips hidden directories
		// whole as well.
		if strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		walked++
		if walked > MaximumWalked {
			cut.Walked = true
			return stop
		}
		if entry.IsDir() {
			return nil
		}
		resolved := ResolvedPath(path)
		if !strings.HasPrefix(resolved, prefix) {
			return nil
		}
		located, refusal := File(root, filepath.ToSlash(strings.TrimPrefix(resolved, prefix)))
		if refusal != nil {
			return nil
		}
		out = append(out, Document{Path: located.Path, Bytes: located.Bytes, Modified: located.Modified})
		return nil
	})
	if err != nil && !errors.Is(err, stop) {
		return out, cut
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Modified == out[j].Modified {
			return out[i].Path < out[j].Path
		}
		return out[i].Modified > out[j].Modified
	})
	if len(out) > MaximumListed {
		cut.Listed = len(out) - MaximumListed
		out = out[:MaximumListed]
	}
	return out, cut
}

// Cut is how a listing is shorter than what is there (limits N28). Both halves
// used to be silent: the rows past MaximumListed, and a walk that stopped at
// MaximumWalked entries, read exactly like a folder that held no more.
type Cut struct {
	// Listed is how many readable documents were found and left off because
	// the listing keeps the newest MaximumListed.
	Listed int
	// Walked says the walk stopped at MaximumWalked entries, so part of the
	// folder was never looked at and how much it held is not known.
	Walked bool
	// Tasks is how many tasks' folders were not walked at all because the
	// listing takes only the newest MaximumTasksListed.
	Tasks int
}

// Add is both cuts of one listing made of several walks.
func (c Cut) Add(o Cut) Cut {
	return Cut{Listed: c.Listed + o.Listed, Walked: c.Walked || o.Walked, Tasks: c.Tasks + o.Tasks}
}

// Any says the listing is shorter than what is there.
func (c Cut) Any() bool { return c.Listed > 0 || c.Walked || c.Tasks > 0 }

// Header is the cut as the listing's `X-Clawdline-Truncated` header spells it:
// `listed=<n>` documents found and left off, `walked=<MaximumWalked>` when a
// walk stopped before the end of its folder, `tasks=<n>` tasks not walked.
// Only the parts that happened, joined by "; ". The listing's body cannot
// carry it: the copied page and the Cloud bridge both refuse a listing object
// with any key but `documents`.
func (c Cut) Header() string {
	parts := []string{}
	if c.Listed > 0 {
		parts = append(parts, "listed="+strconv.Itoa(c.Listed))
	}
	if c.Walked {
		parts = append(parts, "walked="+strconv.Itoa(MaximumWalked))
	}
	if c.Tasks > 0 {
		parts = append(parts, "tasks="+strconv.Itoa(c.Tasks))
	}
	return strings.Join(parts, "; ")
}

// TruncatedHeader is the header a listing that is shorter than what is there
// carries.
const TruncatedHeader = "X-Clawdline-Truncated"

// Escaped is the path as it appears in a URL, with the original's allowed set
// (`CharacterSet.urlPathAllowed`) rather than Go's stricter one, so an ordinary
// document keeps an ordinary address. A name is split on `/` before it gets
// here, so no separator can survive the escaping, and `?` and `#` are not in
// that set.
func Escaped(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		parts[i] = escapeSegment(part)
	}
	return strings.Join(parts, "/")
}

const urlPathAllowedPunctuation = "!$&'()*+,-./:;=@_~"

func escapeSegment(segment string) string {
	var out strings.Builder
	for i := 0; i < len(segment); i++ {
		c := segment[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			strings.IndexByte(urlPathAllowedPunctuation, c) >= 0:
			out.WriteByte(c)
		default:
			const hex = "0123456789ABCDEF"
			out.WriteByte('%')
			out.WriteByte(hex[c>>4])
			out.WriteByte(hex[c&0x0f])
		}
	}
	return out.String()
}

// extensionOf is the lowercased extension of the last segment, with no dot.
// A name whose only dot is its first character has no extension, which is the
// answer Foundation's `pathExtension` gives for a dotfile — and dotfiles are
// refused a clause earlier anyway.
func extensionOf(path string) string {
	name := path
	if cut := strings.LastIndex(name, "/"); cut >= 0 {
		name = name[cut+1:]
	}
	dot := strings.LastIndex(name, ".")
	if dot <= 0 {
		return ""
	}
	return strings.ToLower(name[dot+1:])
}
