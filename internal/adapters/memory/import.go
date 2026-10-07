package memory

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/projects"
)

// Importing Claude Code's auto-memory is a person's one-time act: the CLI
// reads `~/.claude/projects/<slug>/memory/` and copies what it finds into the
// store through the daemon. Nothing here writes, renames or deletes anything
// under the source; it is opened read-only and read once.

// claudeImportFileLimit is how many files one import reads from the source
// directory. Past it the import refuses rather than taking a part of it.
const claudeImportFileLimit = 2048

// ClaudeSources are the directories Claude Code may have filed repo's
// auto-memory under, most likely first: Claude names the folder after the
// working directory as it was given, so both the path as written and its
// canonical spelling are tried.
func ClaudeSources(home string, dirs ...string) []string {
	var out []string
	seen := map[string]bool{}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		p := filepath.Join(home, ".claude", "projects", projects.Slug(d), "memory")
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// Skipped is a source file that will not be imported, and why.
type Skipped struct {
	File   string
	Reason string
}

// ReadClaude reads every entry file in dir. A file that is not an entry —
// the MEMORY.md index, a sub-index, a file whose name or fields this store
// would refuse — is listed in skipped with its reason. A missing directory
// is os.ErrNotExist; anything unreadable is an error, never an empty answer.
func ReadClaude(dir string) (entries []Entry, skipped []Skipped, err error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("%s is not a directory", dir)
	}
	f, err := os.Open(dir)
	if err != nil {
		return nil, nil, err
	}
	names, err := f.Readdirnames(claudeImportFileLimit + 1)
	f.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, nil, err
	}
	if len(names) > claudeImportFileLimit {
		return nil, nil, fmt.Errorf("%s holds more than %d files; nothing was imported", dir, claudeImportFileLimit)
	}
	sort.Strings(names)
	for _, name := range names {
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		path := filepath.Join(dir, name)
		st, err := os.Lstat(path)
		if err != nil {
			return nil, nil, err
		}
		if !st.Mode().IsRegular() {
			skipped = append(skipped, Skipped{File: name, Reason: "not a regular file"})
			continue
		}
		e, err := readEntry(path)
		if err != nil && !errors.Is(err, ErrInvalid) {
			skipped = append(skipped, Skipped{File: name, Reason: err.Error()})
			continue
		}
		if err != nil || e.Name == "" {
			skipped = append(skipped, Skipped{File: name, Reason: "not an entry (no name in its frontmatter)"})
			continue
		}
		if verr := Validate(e); verr != nil {
			skipped = append(skipped, Skipped{File: name, Reason: strings.TrimPrefix(verr.Error(), ErrInvalid.Error()+": ")})
			continue
		}
		entries = append(entries, e)
	}
	return entries, skipped, nil
}
