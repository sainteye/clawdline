package memory

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

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

// ClaudeMemoryIndex is the file Claude Code's auto-memory keeps its index in.
const ClaudeMemoryIndex = "MEMORY.md"

// ClaudeGroup is a sub-index MEMORY.md links to, as the group the import
// makes of it: Members are the names of the entries it links, in its order.
type ClaudeGroup struct {
	Slug        string
	Description string
	File        string
	Members     []string
}

// ClaudeImport is what an import would copy. Entries carry the group the
// source filed them under; an entry with none is resident.
type ClaudeImport struct {
	Entries []Entry
	Groups  []ClaudeGroup
	// Unlinked are the entries neither MEMORY.md nor a sub-index it links
	// names. They are imported as resident. Empty without a MEMORY.md, when
	// every entry is resident by default.
	Unlinked []string
	// HasIndex is whether the source has a MEMORY.md.
	HasIndex bool
	Skipped  []Skipped
}

// claudeLink is a Markdown link to a file in the same directory:
// `[Title](file.md)`.
var claudeLink = regexp.MustCompile(`\[[^\]]*\]\(([^()\s/\\]+\.md)\)`)

type claudeFile struct {
	name  string
	entry Entry
	// isEntry is whether the file is an entry this store would accept.
	isEntry bool
	// body is the text after its frontmatter, or all of it without one.
	body string
	// links are the files in the directory its body links to, in order.
	links []string
	// index is whether the file is an index rather than an entry.
	index bool
}

// ReadClaude reads Claude Code's auto-memory in dir, keeping the shape its
// MEMORY.md gives it. An entry MEMORY.md links directly is resident. A file
// MEMORY.md links that is itself an index — its body is mostly links to
// other files in the directory — becomes a group: its slug is its file name
// without `index-` and `.md`, its description its frontmatter description
// (or the words after its link in MEMORY.md), and every entry it links joins
// it. An index is never imported as an entry. An entry linked from nowhere
// is resident and listed in Unlinked. A file that is not an entry is listed
// in Skipped with its reason. A missing directory is os.ErrNotExist;
// anything unreadable is an error, never an empty answer. Nothing under dir
// is written.
func ReadClaude(dir string) (ClaudeImport, error) {
	var out ClaudeImport
	info, err := os.Stat(dir)
	if err != nil {
		return out, err
	}
	if !info.IsDir() {
		return out, fmt.Errorf("%s is not a directory", dir)
	}
	f, err := os.Open(dir)
	if err != nil {
		return out, err
	}
	names, err := f.Readdirnames(claudeImportFileLimit + 1)
	f.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		return out, err
	}
	if len(names) > claudeImportFileLimit {
		return out, fmt.Errorf("%s holds more than %d files; nothing was imported", dir, claudeImportFileLimit)
	}
	sort.Strings(names)
	files := map[string]*claudeFile{}
	var order []string
	var root string
	for _, name := range names {
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		path := filepath.Join(dir, name)
		st, err := os.Lstat(path)
		if err != nil {
			return out, err
		}
		if !st.Mode().IsRegular() {
			out.Skipped = append(out.Skipped, Skipped{File: name, Reason: "not a regular file"})
			continue
		}
		text, err := readClaudeFile(path)
		if err != nil {
			out.Skipped = append(out.Skipped, Skipped{File: name, Reason: err.Error()})
			continue
		}
		if name == ClaudeMemoryIndex {
			root = text
			out.HasIndex = true
			continue
		}
		cf := &claudeFile{name: name, body: text}
		if e, err := Parse(text); err == nil {
			cf.entry, cf.body = e, e.Body
		}
		cf.links = claudeLinks(cf.body)
		files[name] = cf
		order = append(order, name)
	}
	// Links count only to files that are there; an index is a file whose
	// body is mostly such links, or one named index-*.md with any.
	for _, name := range order {
		cf := files[name]
		var present []string
		for _, l := range cf.links {
			if _, ok := files[l]; ok && l != name {
				present = append(present, l)
			}
		}
		cf.links = present
		lines, linked := 0, 0
		for _, line := range strings.Split(cf.body, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			lines++
			if len(claudeLinks(line)) > 0 {
				linked++
			}
		}
		cf.index = len(present) > 0 && (strings.HasPrefix(name, "index-") || 2*linked >= lines)
		if cf.index {
			continue
		}
		e := cf.entry
		switch {
		case e.Name == "":
			out.Skipped = append(out.Skipped, Skipped{File: name, Reason: "not an entry (no name in its frontmatter)"})
		default:
			if verr := Validate(e); verr != nil {
				out.Skipped = append(out.Skipped, Skipped{File: name, Reason: strings.TrimPrefix(verr.Error(), ErrInvalid.Error()+": ")})
				continue
			}
			cf.isEntry = true
		}
	}
	group := map[string]string{}
	resident := map[string]bool{}
	slugs := map[string]bool{}
	groupOf := map[string]*ClaudeGroup{}
	if root != "" {
		out.Skipped = append(out.Skipped, Skipped{File: ClaudeMemoryIndex, Reason: "the index; its links decide which entries are resident and which are grouped"})
		// Direct links first, so an entry MEMORY.md names itself stays
		// resident even when a sub-index names it too.
		for _, line := range strings.Split(root, "\n") {
			for _, l := range claudeLinks(line) {
				if cf, ok := files[l]; ok && cf.isEntry {
					resident[l] = true
				}
			}
		}
		for _, line := range strings.Split(root, "\n") {
			for _, l := range claudeLinks(line) {
				cf, ok := files[l]
				if !ok || !cf.index || groupOf[l] != nil {
					continue
				}
				slug := strings.TrimSuffix(strings.TrimPrefix(l, "index-"), ".md")
				if !ValidName(slug) {
					slug = strings.TrimPrefix(cf.entry.Name, "index-")
				}
				if !ValidName(slug) || slugs[slug] {
					continue
				}
				description := oneLine(cf.entry.Description)
				if description == "" {
					description = claudeHook(line)
				}
				if description == "" {
					description = "Entries filed under " + l
				}
				g := &ClaudeGroup{Slug: slug, Description: clip(description, GroupDescriptionByteLimit), File: l}
				slugs[slug] = true
				groupOf[l] = g
				for _, m := range cf.links {
					mf := files[m]
					if !mf.isEntry || resident[m] || group[m] != "" {
						continue
					}
					group[m] = slug
					g.Members = append(g.Members, mf.entry.Name)
				}
				out.Groups = append(out.Groups, *g)
			}
		}
	}
	for _, name := range order {
		cf := files[name]
		switch {
		case cf.index && groupOf[name] != nil:
			out.Skipped = append(out.Skipped, Skipped{File: name, Reason: "a sub-index; it becomes group " + groupOf[name].Slug})
		case cf.index && root != "":
			out.Skipped = append(out.Skipped, Skipped{File: name, Reason: "an index MEMORY.md does not link; the entries it lists are imported on their own"})
		case cf.index:
			out.Skipped = append(out.Skipped, Skipped{File: name, Reason: "an index; with no MEMORY.md every entry is imported as resident"})
		case cf.isEntry:
			e := cf.entry
			e.Group = group[name]
			if root != "" && !resident[name] && e.Group == "" {
				out.Unlinked = append(out.Unlinked, e.Name)
			}
			out.Entries = append(out.Entries, e)
		}
	}
	return out, nil
}

func readClaudeFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, entryFileReadLimit+1))
	if err != nil {
		return "", err
	}
	if len(data) > entryFileReadLimit {
		return "", fmt.Errorf("%s is larger than an entry may be", filepath.Base(path))
	}
	return string(data), nil
}

func claudeLinks(text string) []string {
	var out []string
	for _, m := range claudeLink.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	return out
}

// claudeHook is what a `- [Title](file.md) — hook` line says after its link.
func claudeHook(line string) string {
	loc := claudeLink.FindStringIndex(line)
	if loc == nil {
		return ""
	}
	rest := strings.TrimSpace(line[loc[1]:])
	for _, dash := range []string{"—", "–", "-", ":"} {
		if r, ok := strings.CutPrefix(rest, dash); ok {
			return strings.TrimSpace(r)
		}
	}
	return rest
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// clip is s at most limit bytes, cut at a character and marked when cut.
func clip(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	const mark = "…"
	cut := limit - len(mark)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + mark
}
