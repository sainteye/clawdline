package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// A console string is sometimes an instruction: the Clawdfather request is typed
// into a new session, and that session acts on every path it names. Until
// 2026-09-26 the request told it to read the retired app's token under
// `~/.config/clawdline/` and to follow `docs/orchestrator.md`, a page this
// repository never had, so every new coordinator started from dead directions.
//
// This guard reads what the console ships — every catalog under
// `web/console/public/strings`, and the code (not the comments) of every script
// under `web/console/src` — and refuses two things in it: the retired app's
// state directory, and a `docs/*.md` path that is not in this repository.
// Comments are left alone on purpose: citing the retired app's files is how the
// copies say where they came from.

// retiredStateDir is the retired app's directory, and not this daemon's
// `~/.config/clawdline-next` (config.AppDir).
var retiredStateDir = regexp.MustCompile(`~/\.config/clawdline(/|\b)`)

var docPath = regexp.MustCompile(`\bdocs/[A-Za-z0-9_./-]+\.md\b`)

var shippedCode = map[string]bool{".js": true, ".ts": true, ".tsx": true}

func TestShippedStringsNameOnlyLivePaths(t *testing.T) {
	root := repoRoot(t)
	var problems []string
	check := func(where, text string) {
		if m := retiredStateDir.FindString(text); m != "" {
			problems = append(problems, where+": names the retired app's state directory "+m+
				" (this daemon's is ~/.config/clawdline-next; better, point at `clawdline guide`)")
		}
		for _, doc := range docPath.FindAllString(text, -1) {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(doc))); err != nil {
				problems = append(problems, where+": names "+doc+", which is not in this repository")
			}
		}
	}

	catalogs, err := filepath.Glob(filepath.Join(root, "web", "console", "public", "strings", "*.json"))
	if err != nil || len(catalogs) == 0 {
		t.Fatalf("no catalog under web/console/public/strings (%v)", err)
	}
	for _, path := range catalogs {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var catalog map[string]any
		if err := json.Unmarshal(body, &catalog); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		rel, _ := filepath.Rel(root, path)
		for key, value := range catalog {
			if s, ok := value.(string); ok {
				check(filepath.ToSlash(rel)+" "+key, s)
			}
		}
	}

	scripts := 0
	err = filepath.WalkDir(filepath.Join(root, "web", "console", "src"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !shippedCode[filepath.Ext(name)] || strings.Contains(name, ".test.") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		scripts++
		rel, _ := filepath.Rel(root, path)
		for i, line := range codeLines(string(body)) {
			check(filepath.ToSlash(rel)+":"+strconv.Itoa(i+1), line)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scripts == 0 {
		t.Fatal("no script under web/console/src was read")
	}
	for _, p := range problems {
		t.Error(p)
	}
}

// codeLines is the file's lines with comments blanked, so line numbers still
// match. It is a reader for this guard, not a parser: a `//` counts as a
// comment only at the start of a line or after a space, which keeps the `//`
// inside a URL in a string.
func codeLines(body string) []string {
	lines := strings.Split(body, "\n")
	inBlock := false
	for i, line := range lines {
		var out strings.Builder
		rest := line
		for rest != "" {
			if inBlock {
				end := strings.Index(rest, "*/")
				if end < 0 {
					rest = ""
					break
				}
				rest = rest[end+2:]
				inBlock = false
				continue
			}
			start := strings.Index(rest, "/*")
			line := lineComment(rest)
			switch {
			case line >= 0 && (start < 0 || line < start):
				out.WriteString(rest[:line])
				rest = ""
			case start >= 0:
				out.WriteString(rest[:start])
				rest = rest[start+2:]
				inBlock = true
			default:
				out.WriteString(rest)
				rest = ""
			}
		}
		lines[i] = out.String()
	}
	return lines
}

func lineComment(s string) int {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '/' && s[i+1] == '/' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t') {
			return i
		}
	}
	return -1
}
