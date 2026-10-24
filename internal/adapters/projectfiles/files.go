// Package projectfiles lists the instruction and skill files a project owner
// may inspect. It inventories disk candidates, not a running assistant's
// effective prompt or enabled skill catalog.
package projectfiles

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	MaxFiles             = 128
	MaxFileBytes         = 128 << 10
	MaxWriteBytes        = 256 << 10
	MaxScanEntries       = 1024
	MaxSkillSummaryRunes = 240
)

var (
	ErrUnknown  = errors.New("file is not in this project's inventory")
	ErrUnsafe   = errors.New("path is not a plain file within its allowed directory")
	ErrLarge    = errors.New("file exceeds the text size limit")
	ErrText     = errors.New("file is not UTF-8 text")
	ErrChanged  = errors.New("file changed since it was opened")
	ErrReadOnly = errors.New("this file is read-only")
)

// File is a disk candidate. Location is relative to the project or the home
// directory and never contains a person's absolute directory name.
type File struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Location    string   `json:"location"`
	Source      string   `json:"source"`
	Assistant   string   `json:"assistant"`
	Assistants  []string `json:"assistants,omitempty"`
	readOnly    bool
	Kind        string `json:"kind"`
	Status      string `json:"status"`
	Editable    bool   `json:"editable"`
	Size        int64  `json:"size,omitempty"`
	root        string
	rel         string
}

type Listing struct {
	Files     []File   `json:"files"`
	Truncated bool     `json:"truncated"`
	Skipped   []string `json:"skipped"`
}

type Content struct {
	File    File   `json:"file"`
	Text    string `json:"text"`
	Version string `json:"version"`
}

var locks sync.Map // each key serializes this API's writes to one existing file

// Set only by focused package tests to replace a directory after inspection.
var testAfterDirectoryLstat func(string)

func lockFor(path string) *sync.Mutex {
	v, _ := locks.LoadOrStore(path, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func candidate(root, rel, source, assistant, kind string) File {
	sum := sha256.Sum256([]byte(root + "\x00" + rel))
	f := File{ID: hex.EncodeToString(sum[:16]), Name: filepath.Base(rel),
		Location: filepath.ToSlash(rel), Source: source, Assistant: assistant,
		Kind: kind, root: root, rel: rel}
	f.Assistants = []string{assistant}
	if assistant == "antigravity" {
		f.readOnly = true
	}
	if source == "project" && assistant == "codex" && (filepath.Base(rel) == "AGENTS.md" || strings.Contains(filepath.ToSlash(rel), ".agents/skills/")) {
		f.Assistants = []string{"codex", "antigravity"}
	}
	if kind == "skill" {
		f.Title = filepath.Base(filepath.Dir(rel))
	} else if kind == "command" {
		f.Title = strings.TrimSuffix(filepath.Base(rel), ".md")
	}
	return f
}

func skillField(body, field string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	if !strings.HasPrefix(body, "---\n") {
		return ""
	}
	end := strings.Index(body[4:], "\n---")
	if end < 0 {
		return ""
	}
	lines := strings.Split(body[4:4+end], "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, field+":") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, field+":"))
		if value == ">" || value == "|" || value == ">-" || value == "|-" {
			parts := []string{}
			for _, continued := range lines[i+1:] {
				if continued != "" && continued[0] != ' ' && continued[0] != '\t' {
					break
				}
				if strings.TrimSpace(continued) != "" {
					parts = append(parts, strings.TrimSpace(continued))
				}
			}
			return strings.Join(parts, " ")
		}
		return strings.Trim(value, "\"'")
	}
	return ""
}

func skillSummary(value string) string {
	runes := []rune(strings.Join(strings.Fields(value), " "))
	if len(runes) > MaxSkillSummaryRunes {
		runes = runes[:MaxSkillSummaryRunes]
	}
	return string(runes)
}

// projectLayers finds the nearest repository root, then includes every
// directory down to the chosen place. A place without .git is one layer.
func projectLayers(place string) (string, []string, error) {
	place, err := filepath.Abs(place)
	if err != nil {
		return "", nil, err
	}
	st, err := os.Stat(place)
	if err != nil || !st.IsDir() {
		return "", nil, ErrUnknown
	}
	root := place
	for at := place; ; at = filepath.Dir(at) {
		if _, err := os.Lstat(filepath.Join(at, ".git")); err == nil {
			root = at
			break
		}
		if filepath.Dir(at) == at {
			break
		}
	}
	layers := []string{}
	for at := place; ; at = filepath.Dir(at) {
		rel, err := filepath.Rel(root, at)
		if err != nil {
			return "", nil, err
		}
		layers = append(layers, rel)
		if at == root {
			break
		}
	}
	for i, j := 0, len(layers)-1; i < j; i, j = i+1, j-1 {
		layers[i], layers[j] = layers[j], layers[i]
	}
	return root, layers, nil
}

// openDirectory binds each directory component to a handle and verifies its
// identity after opening it. An in-root symlink swapped between Lstat and
// OpenRoot may be followed by os.Root, but its handle has a different identity
// and is rejected before a child entry is opened.
func openDirectory(root *os.Root, rel string) (*os.Root, error) {
	current, err := root.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	if rel == "." || rel == "" {
		return current, nil
	}
	for _, name := range strings.Split(filepath.Clean(rel), string(filepath.Separator)) {
		if name == "." || name == ".." || name == "" {
			current.Close()
			return nil, ErrUnsafe
		}
		before, err := current.Lstat(name)
		if err != nil {
			current.Close()
			return nil, err
		}
		if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			current.Close()
			return nil, ErrUnsafe
		}
		if testAfterDirectoryLstat != nil {
			testAfterDirectoryLstat(name)
		}
		next, err := current.OpenRoot(name)
		if err != nil {
			current.Close()
			return nil, err
		}
		after, err := next.Stat(".")
		current.Close()
		if err != nil || !os.SameFile(before, after) {
			next.Close()
			return nil, ErrUnsafe
		}
		current = next
	}
	return current, nil
}

func openPlain(root *os.Root, rel string) (*os.Root, *os.File, os.FileInfo, error) {
	parent, err := openDirectory(root, filepath.Dir(rel))
	if err != nil {
		return nil, nil, nil, err
	}
	name := filepath.Base(rel)
	before, err := parent.Lstat(name)
	if err != nil {
		parent.Close()
		return nil, nil, nil, err
	}
	if !before.Mode().IsRegular() {
		parent.Close()
		return nil, nil, nil, ErrUnsafe
	}
	f, err := parent.Open(name)
	if err != nil {
		parent.Close()
		return nil, nil, nil, err
	}
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() {
		f.Close()
		parent.Close()
		return nil, nil, nil, ErrUnsafe
	}
	return parent, f, after, nil
}

func inspect(f *File) {
	root, err := os.OpenRoot(f.root)
	if err != nil {
		f.Status = "unreadable"
		return
	}
	defer root.Close()
	parent, file, st, err := openPlain(root, f.rel)
	if parent != nil {
		defer parent.Close()
	}
	if file != nil {
		defer file.Close()
	}
	switch {
	case errors.Is(err, os.ErrNotExist):
		f.Status = "missing"
	case errors.Is(err, ErrUnsafe):
		f.Status = "unsafe"
	case err != nil:
		f.Status = "unreadable"
	case st.Size() > MaxFileBytes:
		f.Status, f.Size = "too_large", st.Size()
	default:
		f.Status, f.Size = "ready", st.Size()
		f.Editable = f.Source == "project" && !f.readOnly && st.Mode().Perm()&0200 != 0
		if f.Kind == "skill" || f.Kind == "command" {
			body, readErr := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
			if readErr == nil && utf8.Valid(body) && !strings.ContainsRune(string(body), 0) {
				if title := skillSummary(skillField(string(body), "name")); title != "" {
					f.Title = title
				}
				f.Description = skillSummary(skillField(string(body), "description"))
			}
		}
	}
}

func add(out *Listing, f File) {
	if len(out.Files) >= MaxFiles {
		out.Truncated = true
		return
	}
	inspect(&f)
	out.Files = append(out.Files, f)
}

func skills(out *Listing, root, rel, source, assistant string) {
	base, err := os.OpenRoot(root)
	if err != nil {
		out.Skipped = append(out.Skipped, filepath.ToSlash(rel))
		return
	}
	defer base.Close()
	dir, err := openDirectory(base, rel)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		out.Skipped = append(out.Skipped, filepath.ToSlash(rel))
		return
	}
	defer dir.Close()
	f, err := dir.Open(".")
	if err != nil {
		out.Skipped = append(out.Skipped, filepath.ToSlash(rel))
		return
	}
	defer f.Close()
	names, err := f.Readdirnames(MaxScanEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		out.Skipped = append(out.Skipped, filepath.ToSlash(rel))
		return
	}
	if len(names) > MaxScanEntries {
		names, out.Truncated = names[:MaxScanEntries], true
	}
	sort.Strings(names)
	for _, name := range names {
		if name == "" || strings.HasPrefix(name, ".") || strings.ContainsAny(name, `/\`) {
			continue
		}
		child, err := openDirectory(dir, name)
		if err != nil {
			out.Skipped = append(out.Skipped, filepath.ToSlash(filepath.Join(rel, name)))
			continue
		}
		_, err = child.Lstat("SKILL.md")
		child.Close()
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			out.Skipped = append(out.Skipped, filepath.ToSlash(filepath.Join(rel, name)))
			continue
		}
		fileRel := filepath.Join(rel, name, "SKILL.md")
		add(out, candidate(root, fileRel, source, assistant, "skill"))
	}
}

func commands(out *Listing, root, rel, source string) {
	markdownFiles(out, root, rel, source, "claude", "command")
}

func markdownFiles(out *Listing, root, rel, source, assistant, kind string) {
	base, err := os.OpenRoot(root)
	if err != nil {
		out.Skipped = append(out.Skipped, filepath.ToSlash(rel))
		return
	}
	defer base.Close()
	dir, err := openDirectory(base, rel)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		out.Skipped = append(out.Skipped, filepath.ToSlash(rel))
		return
	}
	defer dir.Close()
	f, err := dir.Open(".")
	if err != nil {
		out.Skipped = append(out.Skipped, filepath.ToSlash(rel))
		return
	}
	defer f.Close()
	names, err := f.Readdirnames(MaxScanEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		out.Skipped = append(out.Skipped, filepath.ToSlash(rel))
		return
	}
	if len(names) > MaxScanEntries {
		names, out.Truncated = names[:MaxScanEntries], true
	}
	sort.Strings(names)
	for _, name := range names {
		if name == "" || strings.HasPrefix(name, ".") || strings.ContainsAny(name, `/\`) || !strings.HasSuffix(name, ".md") {
			continue
		}
		add(out, candidate(root, filepath.Join(rel, name), source, assistant, kind))
	}
}

// List does not read skill manifests to decide whether a running assistant
// enabled them. It reports direct project and home skill files as candidates.
func List(place, home string) (Listing, error) {
	root, layers, err := projectLayers(place)
	if err != nil {
		return Listing{}, err
	}
	out := Listing{Files: []File{}, Skipped: []string{}}
	for _, layer := range layers {
		for _, spec := range []struct{ name, assistant string }{
			{"AGENTS.md", "codex"}, {"CLAUDE.md", "claude"}, {"CLAUDE.local.md", "claude"},
			{filepath.Join(".claude", "CLAUDE.md"), "claude"}, {filepath.Join(".claude", "CLAUDE.local.md"), "claude"},
		} {
			add(&out, candidate(root, filepath.Join(layer, spec.name), "project", spec.assistant, "instruction"))
		}
		for _, spec := range []struct{ dir, assistant string }{{filepath.Join(".agents", "skills"), "codex"}, {filepath.Join(".claude", "skills"), "claude"}} {
			skills(&out, root, filepath.Join(layer, spec.dir), "project", spec.assistant)
		}
		commands(&out, root, filepath.Join(layer, ".claude", "commands"), "project")
		nativeProjectFiles(&out, root, layer)
	}
	for _, spec := range []struct{ name, assistant string }{
		{filepath.Join(".codex", "AGENTS.md"), "codex"},
		{filepath.Join(".claude", "CLAUDE.md"), "claude"},
		{filepath.Join(".claude", "CLAUDE.local.md"), "claude"},
	} {
		add(&out, candidate(home, spec.name, "global", spec.assistant, "instruction"))
	}
	for _, spec := range []struct{ dir, assistant string }{
		{filepath.Join(".agents", "skills"), "codex"}, {filepath.Join(".codex", "skills"), "codex"}, {filepath.Join(".claude", "skills"), "claude"},
	} {
		skills(&out, home, spec.dir, "global", spec.assistant)
	}
	commands(&out, home, filepath.Join(".claude", "commands"), "global")
	for _, rel := range []string{".gemini/AGENTS.md", ".gemini/GEMINI.md", ".gemini/config/AGENTS.md", ".gemini/config/GEMINI.md"} {
		addNativeFile(&out, home, rel, "global")
	}
	for _, rel := range []string{".gemini/config/rules", ".gemini/antigravity-cli/rules"} {
		markdownFiles(&out, home, filepath.FromSlash(rel), "global", "antigravity", "instruction")
	}
	skills(&out, home, filepath.FromSlash(".gemini/antigravity-cli/skills"), "global", "antigravity")
	return out, nil
}

func find(place, home, id string) (File, error) {
	list, err := List(place, home)
	if err != nil {
		return File{}, err
	}
	for _, f := range list.Files {
		if f.ID == id {
			return f, nil
		}
	}
	return File{}, ErrUnknown
}

func read(f File) (Content, error) {
	root, err := os.OpenRoot(f.root)
	if err != nil {
		return Content{}, err
	}
	defer root.Close()
	parent, file, st, err := openPlain(root, f.rel)
	if err != nil {
		return Content{}, err
	}
	defer parent.Close()
	defer file.Close()
	if st.Size() > MaxFileBytes {
		return Content{}, ErrLarge
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
	if err != nil {
		return Content{}, err
	}
	if len(data) > MaxFileBytes {
		return Content{}, ErrLarge
	}
	if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
		return Content{}, ErrText
	}
	sum := sha256.Sum256(data)
	f.Status, f.Size = "ready", int64(len(data))
	f.Editable = f.Source == "project" && !f.readOnly && st.Mode().Perm()&0200 != 0
	return Content{File: f, Text: string(data), Version: hex.EncodeToString(sum[:])}, nil
}

func Read(place, home, id string) (Content, error) {
	f, err := find(place, home, id)
	if err != nil {
		return Content{}, err
	}
	return read(f)
}

// Save serializes writes from this API. It checks the original file twice,
// writes through a bound parent directory, and replaces the name atomically.
func Save(place, home, id, expected, text string) (Content, error) {
	f, err := find(place, home, id)
	if err != nil {
		return Content{}, err
	}
	if f.Source != "project" || f.readOnly {
		return Content{}, ErrReadOnly
	}
	if len(text) > MaxFileBytes {
		return Content{}, ErrLarge
	}
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return Content{}, ErrText
	}
	return replaceText(f, expected, text)
}

// replaceText is Save after its inventory and text checks: the file must
// still be the version the caller read, and the new text replaces it by a
// rename in the same bound directory. Unify edits CLAUDE.md through it too.
func replaceText(f File, expected, text string) (Content, error) {
	mu := lockFor(filepath.Join(f.root, f.rel))
	mu.Lock()
	defer mu.Unlock()
	current, err := read(f)
	if err != nil {
		return Content{}, err
	}
	if current.Version != expected {
		return Content{}, ErrChanged
	}
	root, err := os.OpenRoot(f.root)
	if err != nil {
		return Content{}, err
	}
	defer root.Close()
	parent, original, before, err := openPlain(root, f.rel)
	if err != nil {
		return Content{}, err
	}
	defer parent.Close()
	defer original.Close()
	if before.Mode().Perm()&0200 == 0 {
		return Content{}, ErrReadOnly
	}
	name := filepath.Base(f.rel)
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return Content{}, err
	}
	tmpName := "." + name + "." + hex.EncodeToString(random) + ".tmp"
	tmp, err := parent.OpenFile(tmpName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return Content{}, err
	}
	defer parent.Remove(tmpName)
	if _, err := io.WriteString(tmp, text); err != nil {
		tmp.Close()
		return Content{}, err
	}
	if err := tmp.Chmod(before.Mode().Perm()); err != nil {
		tmp.Close()
		return Content{}, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return Content{}, err
	}
	if err := tmp.Close(); err != nil {
		return Content{}, err
	}
	checkParent, check, after, err := openPlain(root, f.rel)
	if err != nil {
		return Content{}, err
	}
	checkParent.Close()
	if !os.SameFile(before, after) {
		check.Close()
		return Content{}, ErrChanged
	}
	data, err := io.ReadAll(io.LimitReader(check, MaxFileBytes+1))
	check.Close()
	if err != nil {
		return Content{}, err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != expected {
		return Content{}, ErrChanged
	}
	if err := parent.Rename(tmpName, name); err != nil {
		return Content{}, err
	}
	return read(f)
}

func Refusal(err error) (int, string, string) {
	switch {
	case errors.Is(err, ErrUnknown), errors.Is(err, os.ErrNotExist):
		return 404, "file_not_found", "The file is no longer in this Project. Refresh its list."
	case errors.Is(err, ErrUnsafe):
		return 422, "unsafe_file", "The file or its directory is linked or no longer plain. Refresh its list."
	case errors.Is(err, ErrLarge):
		return 413, "file_too_large", "The file is too large to edit here."
	case errors.Is(err, ErrText):
		return 422, "not_text", "The file is not UTF-8 text."
	case errors.Is(err, ErrChanged):
		return 409, "file_changed", "The file changed elsewhere. Read it again before saving."
	case errors.Is(err, ErrReadOnly):
		return 403, "file_read_only", "This file is read-only here."
	default:
		return 503, "file_unavailable", "The file could not be opened. Check it on the machine and try again."
	}
}

// Native-only candidates are listed read-only. Unify owns neither their layout
// nor their activation modes. Only direct documented children are inspected.
func addNativeFile(out *Listing, root, rel, source string) {
	base, err := os.OpenRoot(root)
	if err != nil {
		return
	}
	defer base.Close()
	if _, err := lstatBeneath(base, rel); errors.Is(err, os.ErrNotExist) {
		return
	}
	add(out, candidate(root, rel, source, "antigravity", "instruction"))
}

func nativeProjectFiles(out *Listing, root, layer string) {
	for _, rel := range []string{"GEMINI.md", ".agents/AGENTS.md", ".agents/GEMINI.md"} {
		addNativeFile(out, root, filepath.Join(layer, filepath.FromSlash(rel)), "project")
	}
	for _, rel := range []string{".agents/rules", ".agent/rules"} {
		markdownFiles(out, root, filepath.Join(layer, filepath.FromSlash(rel)), "project", "antigravity", "instruction")
	}
	skills(out, root, filepath.Join(layer, ".agent", "skills"), "project", "antigravity")
}
