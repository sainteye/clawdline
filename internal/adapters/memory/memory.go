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
	"encoding/json"
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
	// nameByteLimit is the longest entry name, and the longest group slug.
	nameByteLimit = 64
	// GroupCountLimit is how many groups one Project holds. The index a
	// launch carries always names every group, so the groups together must
	// fit in it with room left for resident entries: sixteen of the longest
	// lines are about 6 KiB of the 8 KiB.
	GroupCountLimit = 16
	// GroupDescriptionByteLimit is the longest "read this when" line of a
	// group.
	GroupDescriptionByteLimit = 256
	// groupsFileReadLimit is the most of the groups file read: every group
	// at its longest, JSON-escaped, with room to spare.
	groupsFileReadLimit = 64 << 10
	// entryFileReadLimit is the most of one entry file read: the body bound
	// and room for its frontmatter.
	entryFileReadLimit = EntryBodyLimit + 4<<10
)

// IndexFile is the generated index in each Project's directory.
const IndexFile = "INDEX.md"

// GroupsFile holds each group's slug and description. Membership is not in
// it: an entry names its own group in its frontmatter.
const GroupsFile = "GROUPS.json"

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

// Entry is one memory. Body is empty in a listing. An entry with no Group
// is resident: the index a launch carries lists it by name. An entry in a
// group is listed by `clawdline memory list --group <slug>`, and the index
// carries one line for the whole group.
type Entry struct {
	Name        string
	Description string
	Type        Type
	Group       string
	Body        string
}

// Group is a named set of entries a session reads when its description
// applies. Entries is how many entries name it.
type Group struct {
	Slug        string
	Description string
	Entries     int
}

// The typed refusals. A caller branches on these with errors.Is; anything
// else is "could not check", never "not there".
var (
	ErrInvalid   = errors.New("memory_entry_invalid")
	ErrDuplicate = errors.New("memory_entry_exists")
	ErrNotFound  = errors.New("memory_entry_not_found")
	ErrFull      = errors.New("memory_full")
	ErrBadKey    = errors.New("memory_project_key_invalid")
	// ErrNoGroup is an entry that names a group nobody has set.
	ErrNoGroup = errors.New("memory_group_not_found")
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
	case e.Group != "" && !ValidName(e.Group):
		return fmt.Errorf("%w: a group is lowercase letters, digits and single hyphens, at most %d bytes", ErrInvalid, nameByteLimit)
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

// MostGroups is the number of groups in the Project with the most, which is
// what the memory.groups row reports. No store is a known zero; a groups
// file that cannot be read is an error.
func (s *Store) MostGroups() (int, error) {
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
		groups, err := readGroups(filepath.Join(s.root, k.Name()))
		if err != nil {
			return 0, err
		}
		most = max(most, len(groups))
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
	if e.Group != "" {
		groups, err := readGroups(dir)
		if err != nil {
			return "", err
		}
		if _, ok := groups[e.Group]; !ok {
			return "", fmt.Errorf("%w: %s; create it first with `clawdline memory group set %s --description \"…\"`", ErrNoGroup, e.Group, e.Group)
		}
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

// Listing is everything a Project's memory route answers: every entry
// without its body, every group with its count, and the index a launch is
// given.
type Listing struct {
	Entries []Entry
	Groups  []Group
	Index   string
	// Cut is whether the index left resident entries out.
	Cut bool
}

// Listing is one Project's entries, groups and index. A Project with no
// directory has none of them; one that cannot be read is an error.
func (s *Store) Listing(key string) (Listing, error) {
	dir, err := s.Dir(key)
	if err != nil {
		return Listing{}, err
	}
	return listing(dir)
}

func listing(dir string) (Listing, error) {
	entries, err := list(dir)
	if err != nil {
		return Listing{}, err
	}
	described, err := readGroups(dir)
	if err != nil {
		return Listing{}, err
	}
	groups := countGroups(entries, described)
	text, cut := RenderIndex(entries, groups)
	return Listing{Entries: entries, Groups: groups, Index: text, Cut: cut}, nil
}

// countGroups is every group that is described or named by an entry,
// sorted by slug, with how many entries name it. A group an entry names but
// nobody described is still listed, so its entries stay reachable.
func countGroups(entries []Entry, described map[string]string) []Group {
	count := map[string]int{}
	for _, e := range entries {
		if e.Group != "" {
			count[e.Group]++
		}
	}
	slugs := make([]string, 0, len(described)+len(count))
	for slug := range described {
		slugs = append(slugs, slug)
	}
	for slug := range count {
		if _, ok := described[slug]; !ok {
			slugs = append(slugs, slug)
		}
	}
	sort.Strings(slugs)
	out := make([]Group, 0, len(slugs))
	for _, slug := range slugs {
		out = append(out, Group{Slug: slug, Description: described[slug], Entries: count[slug]})
	}
	return out
}

// Groups is every group of one Project with its entry count.
func (s *Store) Groups(key string) ([]Group, error) {
	l, err := s.Listing(key)
	return l.Groups, err
}

// ValidateGroup is every refusal a group's slug and description can earn.
func ValidateGroup(slug, description string) error {
	switch {
	case !ValidName(slug):
		return fmt.Errorf("%w: a group is lowercase letters, digits and single hyphens, at most %d bytes", ErrInvalid, nameByteLimit)
	case strings.TrimSpace(description) == "":
		return fmt.Errorf("%w: a group's description says when to read it, and is required", ErrInvalid)
	case strings.ContainsAny(description, "\r\n"):
		return fmt.Errorf("%w: a group's description is one line", ErrInvalid)
	case len(description) > GroupDescriptionByteLimit:
		return fmt.Errorf("%w: a group's description is at most %d bytes", ErrInvalid, GroupDescriptionByteLimit)
	}
	return nil
}

// SetGroup creates a group or changes its description. The same description
// again is Unchanged; one group more than GroupCountLimit is ErrFull.
func (s *Store) SetGroup(key, slug, description string) (Outcome, error) {
	if err := ValidateGroup(slug, description); err != nil {
		return "", err
	}
	dir, err := s.Dir(key)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	groups, err := readGroups(dir)
	if err != nil {
		return "", err
	}
	old, exists := groups[slug]
	if exists && old == description {
		return Unchanged, nil
	}
	if !exists && len(groups) >= GroupCountLimit {
		return "", fmt.Errorf("%w: this Project already holds %d groups", ErrFull, GroupCountLimit)
	}
	groups[slug] = description
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := writeGroups(dir, groups); err != nil {
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

type groupsFile struct {
	Groups []groupRecord `json:"groups"`
}

type groupRecord struct {
	Slug        string `json:"slug"`
	Description string `json:"description"`
}

// readGroups is each described group's description by slug. No file is no
// groups; a file that cannot be read or parsed is an error.
func readGroups(dir string) (map[string]string, error) {
	f, err := os.Open(filepath.Join(dir, GroupsFile))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, groupsFileReadLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > groupsFileReadLimit {
		return nil, fmt.Errorf("%s is larger than the groups file may be", GroupsFile)
	}
	var file groupsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("%s: %w", GroupsFile, err)
	}
	out := map[string]string{}
	for _, g := range file.Groups {
		if ValidateGroup(g.Slug, g.Description) != nil {
			return nil, fmt.Errorf("%s holds a group this store would refuse: %q", GroupsFile, g.Slug)
		}
		out[g.Slug] = g.Description
	}
	return out, nil
}

func writeGroups(dir string, groups map[string]string) error {
	file := groupsFile{Groups: []groupRecord{}}
	for slug, description := range groups {
		file.Groups = append(file.Groups, groupRecord{Slug: slug, Description: description})
	}
	sort.Slice(file.Groups, func(i, j int) bool { return file.Groups[i].Slug < file.Groups[j].Slug })
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dir, GroupsFile), append(data, '\n'))
}

// Index is the generated index of one Project, within IndexByteLimit, with
// how many entries the Project holds and whether resident entries were cut.
// No entries is "".
func (s *Store) Index(key string) (text string, entries int, cut bool, err error) {
	l, err := s.Listing(key)
	if err != nil {
		return "", 0, false, err
	}
	return l.Index, len(l.Entries), l.Cut, nil
}

const indexHeading = "# Project memory index\n\nGenerated by Clawdline from the entries; do not edit.\n"

const groupsHeading = "\n## Groups\n\nEach group holds more entries. When a group's description applies, list it with " +
	"`clawdline memory list --group <slug>` and read the entries that matter.\n\n"

// GroupLine is one group's line in the index.
func GroupLine(g Group) string {
	description := g.Description
	if description == "" {
		description = "(no description)"
	}
	return fmt.Sprintf("- %s — %s — %d entries: `clawdline memory list --group %s`\n", g.Slug, description, g.Entries, g.Slug)
}

// RenderIndex is the index text for entries already in index order. Every
// resident entry (one with no group) is one line, `name — description`,
// under a heading per type; then every group with entries or a description
// is one line saying when to read it, how many entries it holds and the
// command that lists them. The group lines are always kept: when the
// resident lines would push the index past IndexByteLimit, they stop at a
// whole line and say how many they left out, and cut is true.
func RenderIndex(entries []Entry, groups []Group) (string, bool) {
	if len(entries) == 0 {
		return "", false
	}
	var tail strings.Builder
	if len(groups) > 0 {
		tail.WriteString(groupsHeading)
		for i, g := range groups {
			line := GroupLine(g)
			// The group bounds keep this from happening; if it ever did,
			// the index still ends within its bound and says what is left.
			if len(indexHeading)+tail.Len()+len(line)+256 > IndexByteLimit {
				fmt.Fprintf(&tail, "\n… %d more groups are not shown; run `clawdline memory group list` for all of them.\n", len(groups)-i)
				break
			}
			tail.WriteString(line)
		}
	}
	var residents []Entry
	for _, e := range entries {
		if e.Group == "" {
			residents = append(residents, e)
		}
	}
	var b strings.Builder
	b.WriteString(indexHeading)
	var heading Type
	for i, e := range residents {
		var chunk strings.Builder
		if e.Type != heading {
			heading = e.Type
			chunk.WriteString("\n## " + string(heading) + "\n\n")
		}
		chunk.WriteString("- " + e.Name + " — " + e.Description + "\n")
		note := fmt.Sprintf("\n… %d more resident entries are not shown; run `clawdline memory list` for all of them.\n", len(residents)-i)
		if b.Len()+chunk.Len()+len(note)+tail.Len() > IndexByteLimit {
			b.WriteString(note)
			b.WriteString(tail.String())
			return b.String(), true
		}
		b.WriteString(chunk.String())
	}
	b.WriteString(tail.String())
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
	l, err := listing(dir)
	if err != nil {
		return err
	}
	text := l.Index
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
	group := ""
	if e.Group != "" {
		group = "\n  group: " + e.Group
	}
	return "---\nname: " + e.Name + "\ndescription: " + description +
		"\nmetadata:\n  type: " + string(e.Type) + group + "\n---\n\n" + body + "\n"
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
		if indented && k != "type" && k != "group" {
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
		Group: fields["group"], Body: strings.TrimLeft(body, "\n")}, nil
}
