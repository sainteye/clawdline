package projectfiles

import (
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// The Ops documents list (docs/ops-documents.md) creates, saves and renames
// plain files beneath a Project's canonical root with the same bound-handle
// rules this package reads them with.

var (
	// ErrRenameCaseOnly is a rename that changes only letter case, which a
	// case-insensitive volume cannot tell from no rename at all.
	ErrRenameCaseOnly = errors.New("a rename must change more than letter case")
	// ErrRenameInstruction is an instruction, Skill or command file, which an
	// assistant finds by its name.
	ErrRenameInstruction = errors.New("instruction, Skill and command files keep their names")
	// ErrRenameUnsupported is a volume that cannot make the second link a
	// rename is made with.
	ErrRenameUnsupported = errors.New("this volume cannot rename the file safely")
)

// CreateTreeText creates dir/name beneath place with text. dir is made as a
// plain directory when it is absent. A name that exists, or appears while the
// file is written, is ErrNameTaken: an existing file is never replaced.
func CreateTreeText(place, dir, name, text string) (TreeContent, error) {
	rel, err := TreePath(dir+"/"+name, true)
	if err != nil {
		return TreeContent{}, err
	}
	root, err := os.OpenRoot(place)
	if err != nil {
		return TreeContent{}, err
	}
	defer root.Close()
	if err := ensureDirectory(root, dir); err != nil {
		return TreeContent{}, err
	}
	parent, err := openDirectory(root, filepath.Dir(rel))
	if err != nil {
		return TreeContent{}, err
	}
	defer parent.Close()
	mu := lockFor(filepath.Join(place, rel))
	mu.Lock()
	err = createText(parent, name, text, 0o644)
	mu.Unlock()
	if err != nil {
		return TreeContent{}, err
	}
	return ReadTree(place, filepath.ToSlash(rel))
}

// SaveTree replaces one plain UTF-8 file beneath place, refused with
// ErrChanged when it is no longer the version the caller read.
func SaveTree(place, value, expected, text string) (TreeContent, error) {
	rel, err := TreePath(value, true)
	if err != nil {
		return TreeContent{}, err
	}
	if len(text) > MaxFileBytes {
		return TreeContent{}, ErrLarge
	}
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return TreeContent{}, ErrText
	}
	content, err := replaceText(candidate(place, rel, "project", "", ""), expected, text)
	if err != nil {
		return TreeContent{}, err
	}
	return TreeContent{Path: value, Text: content.Text, Size: content.File.Size, Version: content.Version}, nil
}

// TreeState is what a Project-relative path names on disk right now.
type TreeState int

const (
	TreeMissing TreeState = iota
	TreeRegular
	TreeOther
)

// StatTree says whether value is a plain file beneath place, without
// following a link at any level.
func StatTree(place, value string) (TreeState, int64, error) {
	rel, err := TreePath(value, true)
	if err != nil {
		return TreeOther, 0, err
	}
	root, err := os.OpenRoot(place)
	if err != nil {
		return TreeOther, 0, err
	}
	defer root.Close()
	parent, err := openDirectory(root, filepath.Dir(rel))
	if errors.Is(err, os.ErrNotExist) {
		return TreeMissing, 0, nil
	}
	if err != nil {
		return TreeOther, 0, err
	}
	defer parent.Close()
	st, err := parent.Lstat(filepath.Base(rel))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return TreeMissing, 0, nil
	case err != nil:
		return TreeOther, 0, err
	case st.Mode().IsRegular():
		return TreeRegular, st.Size(), nil
	}
	return TreeOther, 0, nil
}

// SniffText says whether a plain file's first bytes are UTF-8 text.
func SniffText(place, value string) bool {
	rel, err := TreePath(value, true)
	if err != nil {
		return false
	}
	root, err := os.OpenRoot(place)
	if err != nil {
		return false
	}
	defer root.Close()
	parent, f, _, err := openPlain(root, rel)
	if err != nil {
		return false
	}
	defer parent.Close()
	defer f.Close()
	buf := make([]byte, 4096)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return false
	}
	buf = buf[:n]
	// A rune cut at the end of the sample is not evidence of binary.
	for i := 0; i < 3 && len(buf) > 0 && !utf8.Valid(buf); i++ {
		buf = buf[:len(buf)-1]
	}
	return utf8.Valid(buf) && !strings.ContainsRune(string(buf), 0)
}

// instructionName is a file name an assistant finds by name wherever it is.
func instructionName(rel string) bool {
	slash := filepath.ToSlash(rel)
	switch path.Base(slash) {
	case "AGENTS.md", "CLAUDE.md", "CLAUDE.local.md", "GEMINI.md", "SKILL.md":
		return true
	}
	for _, dir := range []string{".claude/", ".agents/", ".codex/", ".gemini/", ".cursor/"} {
		if strings.HasPrefix(slash, dir) || strings.Contains(slash, "/"+dir) {
			return true
		}
	}
	return false
}

// Instruction says whether value is in this Project's instruction, Skill or
// command inventory (List), or has a name an assistant reads by name.
func Instruction(place, home, value string) bool {
	if instructionName(value) {
		return true
	}
	list, err := List(place, home)
	if err != nil {
		// Unknown is not permission to rename.
		return true
	}
	for _, f := range list.Files {
		if f.Source == "project" && f.Location == value {
			return true
		}
	}
	return false
}

// RenameTree renames the plain file from to newName in the same directory.
// The new name is made as a second link, which fails rather than replaces
// when it exists; record then runs, and only when it succeeds is the old
// name removed. When record fails the new link is removed and the old name
// kept. Both names are locked, in sorted order, for the whole rename.
func RenameTree(place, from, newName string, record func(to string) error) (string, error) {
	rel, err := TreePath(from, true)
	if err != nil {
		return "", err
	}
	if newName == "" || strings.ContainsAny(newName, "/\\") || newName == "." || newName == ".." {
		return "", ErrUnsafe
	}
	to := path.Join(path.Dir(from), newName)
	if path.Dir(from) == "." {
		to = newName
	}
	toRel, err := TreePath(to, true)
	if err != nil {
		return "", err
	}
	oldName := filepath.Base(rel)
	if strings.EqualFold(oldName, newName) {
		return "", ErrRenameCaseOnly
	}
	if instructionName(from) || instructionName(to) {
		return "", ErrRenameInstruction
	}
	names := []string{filepath.Join(place, rel), filepath.Join(place, toRel)}
	sort.Strings(names)
	for _, n := range names {
		mu := lockFor(n)
		mu.Lock()
		defer mu.Unlock()
	}
	root, err := os.OpenRoot(place)
	if err != nil {
		return "", err
	}
	defer root.Close()
	parent, err := openDirectory(root, filepath.Dir(rel))
	if err != nil {
		return "", err
	}
	defer parent.Close()
	before, err := parent.Lstat(oldName)
	if err != nil {
		return "", err
	}
	if !before.Mode().IsRegular() {
		return "", ErrUnsafe
	}
	if err := absent(parent, newName); err != nil {
		return "", err
	}
	if testBeforeRenameLink != nil {
		testBeforeRenameLink(filepath.Join(place, toRel))
	}
	if err := parent.Link(oldName, newName); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", ErrNameTaken
		}
		return "", errors.Join(ErrRenameUnsupported, err)
	}
	linked, err := parent.Lstat(newName)
	again, againErr := parent.Lstat(oldName)
	if err != nil || againErr != nil || !os.SameFile(before, linked) || !os.SameFile(before, again) {
		_ = parent.Remove(newName)
		return "", ErrChanged
	}
	if err := record(to); err != nil {
		_ = parent.Remove(newName)
		return "", err
	}
	if err := parent.Remove(oldName); err != nil {
		// The record names the new link already; the old name is the
		// leftover a pending rename settles.
		return to, err
	}
	return to, nil
}

// SettleRename finishes or undoes a rename the daemon stopped in. current is
// the path the record holds now. It returns the path the record should
// hold: to when the new name is the file, from when the rename is undone.
func SettleRename(place, from, to, current string) (string, error) {
	fromRel, err := TreePath(from, true)
	if err != nil {
		return current, err
	}
	toRel, err := TreePath(to, true)
	if err != nil {
		return current, err
	}
	root, err := os.OpenRoot(place)
	if err != nil {
		return current, err
	}
	defer root.Close()
	a, aErr := root.Lstat(fromRel)
	b, bErr := root.Lstat(toRel)
	both := aErr == nil && bErr == nil && os.SameFile(a, b)
	switch {
	case current == to && both:
		return to, root.Remove(fromRel)
	case current == from && both:
		return from, root.Remove(toRel)
	case current == from && errors.Is(aErr, os.ErrNotExist) && bErr == nil:
		return to, nil
	}
	return current, nil
}

// Set only by focused tests to take the target name just before the link.
var testBeforeRenameLink func(string)

// Set only by focused tests to take a new file's name just before its link.
var testBeforeCreateLink func(string)
