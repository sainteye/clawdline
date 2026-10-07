// Package memory is a Project's shared memory: one store per repository that
// Claude Code and Codex sessions both read and write through `clawdline
// memory` (docs/project-memory.md).
//
// The store is `<state dir>/memory/<repo key>/`: one Markdown file per entry,
// in the shape Claude Code's own auto-memory uses (frontmatter `name`,
// `description`, `metadata.type`, then the body), and an index this package
// generates after every write and nobody edits. The repo key is the one the
// broker names worktree directories by (orchestrator.RepoSlug), so the store
// follows the repository, not one checkout of it.
//
// The daemon is the only writer. Codex sessions run in a sandbox that cannot
// write the state directory, so the CLI reaches the store through the
// daemon's routes, and every write here is serialized by one mutex and lands
// by temp file and rename: a reader sees the old entry or the new one, never
// half of either.
package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/sainteye/clawdline/internal/adapters/projects"
)

// The bounds of one store (internal/domain/capacity, memory.*).
const (
	// IndexByteLimit is the most of the generated index a launch carries.
	// Past it the index is cut at a line and says how many entries it left
	// out; every entry is still listed by `clawdline memory list`.
	IndexByteLimit = 8 << 10
	// EntryBodyLimit is the largest body one entry holds.
	EntryBodyLimit = 64 << 10
	// EntryCountLimit is how many entries one Project holds. One more is
	// refused; only a person (or a session they asked) forgets one.
	EntryCountLimit = 512
	// DescriptionByteLimit is the longest one-line description.
	DescriptionByteLimit = 512
	// nameByteLimit is the longest entry name.
	nameByteLimit = 64
	// entryFileReadLimit is the most of one entry file read: the body bound
	// and room for its frontmatter.
	entryFileReadLimit = EntryBodyLimit + 4<<10
)

// IndexFile is the generated index in each Project's directory.
const IndexFile = "INDEX.md"

// Type is an entry's kind, the four Claude Code's auto-memory uses.
type Type string

const (
	TypeUser      Type = "user"
	TypeFeedback  Type = "feedback"
	TypeProject   Type = "project"
	TypeReference Type = "reference"
)

// Types is every type, in the order the index groups them.
var Types = []Type{TypeUser, TypeFeedback, TypeProject, TypeReference}

// KnownType is whether t is one of Types.
func KnownType(t Type) bool {
	for _, k := range Types {
		if k == t {
			return true
		}
	}
	return false
}

// Entry is one memory. Body is empty in a listing.
type Entry struct {
	Name        string
	Description string
	Type        Type
	Body        string
}

// The typed refusals. A caller branches on these with errors.Is; anything
// else is "could not check", never "not there".
var (
	ErrInvalid   = errors.New("memory_entry_invalid")
	ErrDuplicate = errors.New("memory_entry_exists")
	ErrNotFound  = errors.New("memory_entry_not_found")
	ErrFull      = errors.New("memory_full")
	ErrBadKey    = errors.New("memory_project_key_invalid")
)

var nameShape = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidName is a kebab-case slug of at most 64 bytes: a file name on every
// platform, and a word no shell reads anything into.
func ValidName(name string) bool {
	return len(name) <= nameByteLimit && nameShape.MatchString(name)
}

var keyShape = regexp.MustCompile(`^[a-z0-9-]{1,32}-[0-9a-f]{8}$`)

// Validate is every refusal an entry can earn before anything is written.
func Validate(e Entry) error {
	switch {
	case !ValidName(e.Name):
		return fmt.Errorf("%w: a name is lowercase letters, digits and single hyphens, at most %d bytes", ErrInvalid, nameByteLimit)
	case strings.TrimSpace(e.Description) == "":
		return fmt.Errorf("%w: a description is required", ErrInvalid)
	case strings.ContainsAny(e.Description, "\r\n"):
		return fmt.Errorf("%w: a description is one line", ErrInvalid)
	case len(e.Description) > DescriptionByteLimit:
		return fmt.Errorf("%w: a description is at most %d bytes", ErrInvalid, DescriptionByteLimit)
	case !KnownType(e.Type):
		return fmt.Errorf("%w: the type is one of user, feedback, project, reference", ErrInvalid)
	case strings.TrimSpace(e.Body) == "":
		return fmt.Errorf("%w: the body is empty", ErrInvalid)
	case len(e.Body) > EntryBodyLimit:
		return fmt.Errorf("%w: the body is at most %d bytes", ErrInvalid, EntryBodyLimit)
	}
	return nil
}

// RepoKey is the directory name a repository's memory lives under, spelled
// exactly as orchestrator.RepoSlug spells a repository's worktree directory:
// the basename lowercased and punctuation-flattened, then eight hex of its
// canonical path. A test there holds the two to one spelling.
func RepoKey(repo string) string {
	canonical := repo
	if resolved, err := filepath.EvalSymlinks(repo); err == nil {
		canonical = resolved
	}
	base := strings.ToLower(filepath.Base(canonical))
	flat := make([]rune, 0, len(base))
	for _, r := range base {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			flat = append(flat, r)
		} else {
			flat = append(flat, '-')
		}
	}
	name := string(flat)
	if len(name) > 32 {
		name = name[:32]
	}
	if name == "" {
		name = "repo"
	}
	sum := sha256.Sum256([]byte(canonical))
	return name + "-" + hex.EncodeToString(sum[:])[:8]
}

// KeyFor is the key of the repository dir is in. A linked worktree resolves
// to the repository it came from (projects.CanonicalProjectKey), so a
// dispatched child reads the same store as the Project it works for. A
// directory outside Git is a Project of its own.
func KeyFor(dir string) (string, bool) {
	repo, ok := projects.CanonicalProjectKey(dir)
	if !ok {
		return "", false
	}
	return RepoKey(repo), true
}

// Store is every Project's memory under one state directory.
type Store struct {
	root string
	// mu serializes every write in every Project. Writes are a person's or a
	// session's deliberate act, a few a day; one lock is simpler than one per
	// Project and serializes each Project at least as strictly.
	mu sync.Mutex
}

// New is the store under stateDir.
func New(stateDir string) *Store { return &Store{root: filepath.Join(stateDir, "memory")} }

// Dir is the directory of one Project's memory.
func (s *Store) Dir(key string) (string, error) {
	if !keyShape.MatchString(key) {
		return "", ErrBadKey
	}
	return filepath.Join(s.root, key), nil
}

// List is every entry without its body, in index order. A Project with no
// directory has no entries; a directory that could not be read is an error,
// never an empty list.
func (s *Store) List(key string) ([]Entry, error) {
	dir, err := s.Dir(key)
	if err != nil {
		return nil, err
	}
	return list(dir)
}

// MostEntries is the number of entries in the fullest Project's store, which
// is what the memory.entries row reports. No store at all is a known zero; a
// root or a Project directory that cannot be read is an error.
func (s *Store) MostEntries() (int, error) {
	keys, err := os.ReadDir(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	most := 0
	for _, k := range keys {
		if !k.IsDir() || !keyShape.MatchString(k.Name()) {
			continue
		}
		files, err := os.ReadDir(filepath.Join(s.root, k.Name()))
		if err != nil {
			return 0, err
		}
		n := 0
		for _, f := range files {
			if name, ok := strings.CutSuffix(f.Name(), ".md"); ok && f.Name() != IndexFile && ValidName(name) && f.Type().IsRegular() {
				n++
			}
		}
		most = max(most, n)
	}
	return most, nil
}

func list(dir string) ([]Entry, error) {
	names, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Entry{}
	for _, d := range names {
		name, ok := strings.CutSuffix(d.Name(), ".md")
		if !ok || d.Name() == IndexFile || !ValidName(name) || !d.Type().IsRegular() {
			continue
		}
		e, err := readEntry(filepath.Join(dir, d.Name()))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", d.Name(), err)
		}
		if e.Name != name {
			return nil, fmt.Errorf("%s names itself %q", d.Name(), e.Name)
		}
		e.Body = ""
		out = append(out, e)
	}
	sortEntries(out)
	return out, nil
}

func sortEntries(entries []Entry) {
	rank := map[Type]int{}
	for i, t := range Types {
		rank[t] = i
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if rank[entries[i].Type] != rank[entries[j].Type] {
			return rank[entries[i].Type] < rank[entries[j].Type]
		}
		return entries[i].Name < entries[j].Name
	})
}

// Get is one entry in full.
func (s *Store) Get(key, name string) (Entry, error) {
	dir, err := s.Dir(key)
	if err != nil {
		return Entry{}, err
	}
	if !ValidName(name) {
		return Entry{}, fmt.Errorf("%w: %q is not an entry name", ErrInvalid, name)
	}
	e, err := readEntry(filepath.Join(dir, name+".md"))
	if errors.Is(err, os.ErrNotExist) {
		return Entry{}, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	return e, err
}

// Outcome is what a write did.
type Outcome string

const (
	Created   Outcome = "created"
	Updated   Outcome = "updated"
	Unchanged Outcome = "unchanged"
	Forgotten Outcome = "forgotten"
)

// Add writes a new entry. The same entry sent twice — a retry after a lost
// answer — is Unchanged; a different entry under a name already there is
// ErrDuplicate, and nothing is overwritten.
func (s *Store) Add(key string, e Entry) (Outcome, error) {
	return s.write(key, e, false)
}

// Update replaces an entry that is there; an unknown name is ErrNotFound.
// The same content again is Unchanged.
func (s *Store) Update(key string, e Entry) (Outcome, error) {
	return s.write(key, e, true)
}

func (s *Store) write(key string, e Entry, replace bool) (Outcome, error) {
	if err := Validate(e); err != nil {
		return "", err
	}
	// As Format writes it and Parse reads it back, so a retry compares equal.
	e.Body = strings.Trim(e.Body, "\n") + "\n"
	dir, err := s.Dir(key)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(dir, e.Name+".md")
	old, err := readEntry(path)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if exists && old == e {
		return Unchanged, nil
	}
	if exists && !replace {
		return "", fmt.Errorf("%w: %s; use update to change it", ErrDuplicate, e.Name)
	}
	if !exists && replace {
		return "", fmt.Errorf("%w: %s", ErrNotFound, e.Name)
	}
	if !exists {
		current, err := list(dir)
		if err != nil {
			return "", err
		}
		if len(current) >= EntryCountLimit {
			return "", fmt.Errorf("%w: this Project already holds %d entries; forget one first", ErrFull, EntryCountLimit)
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := writeAtomic(path, []byte(Format(e))); err != nil {
		return "", err
	}
	if err := writeIndex(dir); err != nil {
		return "", err
	}
	if exists {
		return Updated, nil
	}
	return Created, nil
}

// Forget removes an entry; an unknown name is ErrNotFound.
func (s *Store) Forget(key, name string) (Outcome, error) {
	dir, err := s.Dir(key)
	if err != nil {
		return "", err
	}
	if !ValidName(name) {
		return "", fmt.Errorf("%w: %q is not an entry name", ErrInvalid, name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err = os.Remove(filepath.Join(dir, name+".md"))
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	if err != nil {
		return "", err
	}
	if err := writeIndex(dir); err != nil {
		return "", err
	}
	return Forgotten, nil
}

// Index is the generated index of one Project, cut to IndexByteLimit, with
// how many entries it lists and whether it was cut. No entries is "".
func (s *Store) Index(key string) (text string, entries int, cut bool, err error) {
	dir, err := s.Dir(key)
	if err != nil {
		return "", 0, false, err
	}
	all, err := list(dir)
	if err != nil {
		return "", 0, false, err
	}
	text, cut = RenderIndex(all)
	return text, len(all), cut, nil
}

const indexHeading = "# Project memory index\n\nGenerated by Clawdline from the entries; do not edit.\n"

// RenderIndex is the index text for entries already in index order: one
// line per entry, `name — description`, under a heading per type. Past
// IndexByteLimit it stops at a whole line and says how many it left out.
func RenderIndex(entries []Entry) (string, bool) {
	if len(entries) == 0 {
		return "", false
	}
	var b strings.Builder
	b.WriteString(indexHeading)
	var group Type
	for i, e := range entries {
		var chunk strings.Builder
		if e.Type != group {
			group = e.Type
			chunk.WriteString("\n## " + string(group) + "\n\n")
		}
		chunk.WriteString("- " + e.Name + " — " + e.Description + "\n")
		left := len(entries) - i
		tail := fmt.Sprintf("\n… %d more entries are not shown; run `clawdline memory list` for all of them.\n", left)
		if b.Len()+chunk.Len()+len(tail) > IndexByteLimit {
			b.WriteString(tail)
			return b.String(), true
		}
		b.WriteString(chunk.String())
	}
	return b.String(), false
}

// LaunchInstruction is what a launched session is told before the index.
const LaunchInstruction = "This Project's shared memory index is below; read an entry with `clawdline memory show <name>`; " +
	"record a lesson for this Project with `clawdline memory add`, not in your own assistant's memory."

// LaunchText is what a session launched in dir is given, or "" when the
// Project has no entries — and a launch with "" is exactly the launch
// without memory. An index that could not be read is an error for the
// caller to log; it is never an empty index.
func (s *Store) LaunchText(dir string) (string, error) {
	key, ok := KeyFor(dir)
	if !ok {
		return "", nil
	}
	index, entries, _, err := s.Index(key)
	if err != nil || entries == 0 {
		return "", err
	}
	return LaunchInstruction + "\n\n" + index, nil
}

// LaunchTextLimit is the longest LaunchText can be, which projects.Admit
// checks a launch's memory against.
const LaunchTextLimit = len(LaunchInstruction) + 2 + IndexByteLimit

func writeIndex(dir string) error {
	all, err := list(dir)
	if err != nil {
		return err
	}
	text, _ := RenderIndex(all)
	if text == "" {
		err := os.Remove(filepath.Join(dir, IndexFile))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return writeAtomic(filepath.Join(dir, IndexFile), []byte(text))
}

// writeAtomic lands body at path by a synced temp file in the same directory
// and a rename.
func writeAtomic(path string, body []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(body)
	if err == nil {
		err = f.Chmod(0o600)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

func readEntry(path string) (Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return Entry{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, entryFileReadLimit+1))
	if err != nil {
		return Entry{}, err
	}
	if len(data) > entryFileReadLimit {
		return Entry{}, fmt.Errorf("%s is larger than an entry may be", filepath.Base(path))
	}
	return Parse(string(data))
}

// Format is an entry as its file holds it.
func Format(e Entry) string {
	body := strings.Trim(e.Body, "\n")
	description := e.Description
	// Parse takes one pair of outer quotes off, and reads a lone `>` or `|`
	// as a folded block; a description that opens like either is quoted so
	// it reads back as it was written.
	if strings.HasPrefix(description, `"`) || strings.HasPrefix(description, "'") ||
		strings.HasPrefix(description, ">") || strings.HasPrefix(description, "|") {
		description = `"` + description + `"`
	}
	return "---\nname: " + e.Name + "\ndescription: " + description +
		"\nmetadata:\n  type: " + string(e.Type) + "\n---\n\n" + body + "\n"
}

// Parse reads an entry file: this store's own, or one of Claude Code's
// auto-memory files, whose `type` may also sit at the top level and whose
// description may be quoted or folded over several lines.
func Parse(text string) (Entry, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return Entry{}, fmt.Errorf("%w: no frontmatter", ErrInvalid)
	}
	front, body, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		front, ok = strings.CutSuffix(rest, "\n---")
		if !ok {
			return Entry{}, fmt.Errorf("%w: the frontmatter does not end", ErrInvalid)
		}
		body = ""
	}
	fields := map[string]string{}
	lines := strings.Split(front, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		indented := strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
		k, v, found := strings.Cut(strings.TrimSpace(line), ":")
		if !found {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if indented && k != "type" {
			continue
		}
		if v == ">" || v == "|" || v == ">-" || v == "|-" {
			var parts []string
			for i+1 < len(lines) && (strings.HasPrefix(lines[i+1], " ") || strings.HasPrefix(lines[i+1], "\t")) {
				i++
				parts = append(parts, strings.TrimSpace(lines[i]))
			}
			v = strings.Join(parts, " ")
		}
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		if _, seen := fields[k]; !seen || indented {
			fields[k] = v
		}
	}
	return Entry{Name: fields["name"], Description: fields["description"], Type: Type(fields["type"]),
		Body: strings.TrimLeft(body, "\n")}, nil
}
