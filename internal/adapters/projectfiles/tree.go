package projectfiles

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	MaxTreePathBytes = 4096
	MaxTreeDepth     = 64
)

// TreeEntry describes one immediate child of a Project directory. A link is
// visible by name but cannot be opened through this browser.
type TreeEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Kind string `json:"kind"`
	Size int64  `json:"size,omitempty"`
}

type TreeListing struct {
	Directory string      `json:"directory"`
	Entries   []TreeEntry `json:"entries"`
	Truncated bool        `json:"truncated"`
}

type TreeContent struct {
	Path    string `json:"path"`
	Text    string `json:"text"`
	Size    int64  `json:"size"`
	Version string `json:"version"`
}

// TreePath accepts only a canonical slash-separated relative path. The Git
// database is implementation state, not a Project document.
func TreePath(value string, file bool) (string, error) {
	if value == "" && !file {
		return ".", nil
	}
	if value == "" || len(value) > MaxTreePathBytes || !utf8.ValidString(value) ||
		strings.ContainsAny(value, "\\\x00") || strings.HasPrefix(value, "/") {
		return "", ErrUnsafe
	}
	parts := strings.Split(value, "/")
	if len(parts) > MaxTreeDepth {
		return "", ErrUnsafe
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.EqualFold(part, ".git") {
			return "", ErrUnsafe
		}
	}
	rel := filepath.FromSlash(value)
	if !filepath.IsLocal(rel) {
		return "", ErrUnsafe
	}
	return rel, nil
}

// ListTree reads one directory on demand. It never scans descendant folders
// or reads file content while constructing the tree.
func ListTree(place, directory string) (TreeListing, error) {
	rel, err := TreePath(directory, false)
	if err != nil {
		return TreeListing{}, err
	}
	root, err := os.OpenRoot(place)
	if err != nil {
		return TreeListing{}, err
	}
	defer root.Close()
	dir, err := openDirectory(root, rel)
	if err != nil {
		return TreeListing{}, err
	}
	defer dir.Close()
	stream, err := dir.Open(".")
	if err != nil {
		return TreeListing{}, err
	}
	defer stream.Close()
	names, err := stream.Readdirnames(MaxScanEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return TreeListing{}, err
	}
	out := TreeListing{Directory: directory, Entries: []TreeEntry{}}
	if len(names) > MaxScanEntries {
		names, out.Truncated = names[:MaxScanEntries], true
	}
	sort.Strings(names)
	for _, name := range names {
		if name == ".git" || name == "" || strings.ContainsAny(name, "/\\") {
			continue
		}
		info, err := dir.Lstat(name)
		if err != nil {
			// Another process changed this directory while it was being read.
			// A refresh supplies its current entries without hiding the rest.
			continue
		}
		entry := TreeEntry{Name: name, Path: name}
		if directory != "" {
			entry.Path = directory + "/" + name
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			entry.Kind = "link"
		case info.IsDir():
			entry.Kind = "directory"
		case info.Mode().IsRegular():
			entry.Kind, entry.Size = "file", info.Size()
		default:
			entry.Kind = "other"
		}
		out.Entries = append(out.Entries, entry)
	}
	sort.Slice(out.Entries, func(i, j int) bool {
		a, b := out.Entries[i], out.Entries[j]
		if (a.Kind == "directory") != (b.Kind == "directory") {
			return a.Kind == "directory"
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name) ||
			strings.EqualFold(a.Name, b.Name) && a.Name < b.Name
	})
	return out, nil
}

// ReadTree reads only a regular UTF-8 text file beneath the selected Project.
// The existing file reader binds every directory and the final file to handles
// and rechecks their identities after opening them.
func ReadTree(place, path string) (TreeContent, error) {
	rel, err := TreePath(path, true)
	if err != nil {
		return TreeContent{}, err
	}
	content, err := read(candidate(place, rel, "project", "", ""))
	if err != nil {
		return TreeContent{}, err
	}
	return TreeContent{Path: path, Text: content.Text, Size: content.File.Size, Version: content.Version}, nil
}
