// Command check-private reads every file this repository would publish and
// fails on anything that belongs to a person rather than to the project: a real
// home directory, a real task or session id, the private cloud repository, a
// credential, an email address. The rules are internal/domain/privacy; this is
// the part that finds the files.
//
// "Would publish" is what git would commit: every tracked file, and every
// untracked file that .gitignore does not exclude, because the next commit is
// the one that matters. Without git it walks the tree and skips .git,
// node_modules and dist. A tracked symlink is published as the path it points
// at, so that path is what is read.
//
// Four answers: 0 is clean, 1 is a finding, 2 is "this could not be checked",
// 3 is "this could not be decided". A run that read nothing is a 2, never a 0,
// and a run with no word list is a 3 — see below.
//
// A person's own words — project names, a client, their real name — cannot be
// written into a public checker without publishing them. They live outside the
// repository, one per line, in $CLAWDLINE_PRIVATE_WORDS or in
// `$(git rev-parse --git-common-dir)/info/private-words`, which git never
// commits and every worktree of the clone shares. That is also why an absent
// list is 3 and not 0: on another machine and on CI there is no list, the
// private-word rule cannot fire at all, and a checker that answers "clean"
// there is answering a question it did not ask.
//
// -history reads the objects of the commits instead of the files on disk,
// because a word committed on Thursday and taken out on Friday is gone from
// the tree and still in what `git push` sends (history.go).
//
// Usage:
//
//	tools/check-private.sh                 # the working tree
//	tools/check-private.sh -- ':!docs'     # git pathspecs narrow it
//	tools/check-private.sh -rules          # what each rule catches and what passes
//	tools/check-private.sh -history        # every commit, from the last checkpoint
//	tools/check-private.sh -history -new   # red only if today's commits added one
//	tools/check-private.sh -history -full  # every commit, whatever the checkpoint says
//	tools/check-private.sh -history -revs=HEAD -since=origin/main   # only what a push would add
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/privacy"
)

func main() {
	rules := flag.Bool("rules", false, "print every rule, what it catches and what passes it")
	history := flag.Bool("history", false, "read the commits, not the working tree")
	revs := flag.String("revs", "HEAD", "what -history reads, as git rev-list spells it (e.g. --all)")
	full := flag.Bool("full", false, "-history reads every commit, whatever the checkpoint says")
	checkpoint := flag.String("checkpoint", "", "where -history remembers what it read; - keeps none")
	since := flag.String("since", "", "-history reads only what -revs reaches and this does not (a push: the commit the remote already carries); it writes no checkpoint")
	onlyNew := flag.Bool("new", false, "-history is red only for a finding the checkpoint had not already recorded")
	dir := flag.String("dir", "", "read every file under this directory instead of the repository, and the printable strings of a binary (a release's unpacked artifacts, docs/releasing.md)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: check-private [-rules] [-history [-revs R] [-since R] [-full] [-new] [-checkpoint P]] [-dir D] [-- git-pathspec...]")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *rules {
		printRules()
		return
	}
	if *history {
		os.Exit(int(runHistory(historyOptions{revs: *revs, since: *since, full: *full, checkpoint: *checkpoint, onlyNew: *onlyNew})))
	}
	os.Exit(int(run(flag.Args(), *dir)))
}

func run(pathspecs []string, dir string) privacy.Answer {
	root, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot check:", err)
		return privacy.CannotCheck
	}
	read := func(p string) ([]byte, bool, bool, error) {
		data, ok, err := content(p)
		return data, ok, false, err
	}
	scanRoot := root
	var files []string
	if dir != "" {
		scanRoot, read = dir, contentOrStrings
		files, err = everyFile(dir)
	} else {
		files, err = published(root, pathspecs)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot check:", err)
		return privacy.CannotCheck
	}
	words, source, err := privateWords(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot check:", err)
		return privacy.CannotCheck
	}
	scanner := privacy.New(words)

	type hit struct {
		path string
		privacy.Finding
	}
	var hits []hit
	nread := 0
	for _, rel := range files {
		data, ok, binary, err := read(filepath.Join(scanRoot, filepath.FromSlash(rel)))
		if err != nil {
			fmt.Fprintln(os.Stderr, "cannot check:", err)
			return privacy.CannotCheck
		}
		if !ok {
			continue
		}
		nread++
		for _, f := range scanner.Scan(rel, data) {
			// A binary's strings are every literal compiled into it, its
			// dependencies' too: the fixed rules' exemptions are written for
			// source files and do not reach them. What a binary must never
			// carry is one of the person's words or this machine's home.
			if binary && f.Rule != "private-word" {
				continue
			}
			hits = append(hits, hit{rel, f})
		}
		if binary {
			if home, err := os.UserHomeDir(); err == nil && home != "" && bytes.Contains(data, []byte(home)) {
				hits = append(hits, hit{rel, privacy.Finding{Rule: "home-path", Match: "this machine's home directory"}})
			}
		}
	}
	if nread == 0 {
		fmt.Fprintln(os.Stderr, "cannot check: read no files; a run that read nothing is not a pass")
		return privacy.CannotCheck
	}

	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].path != hits[j].path {
			return hits[i].path < hits[j].path
		}
		return hits[i].Line < hits[j].Line
	})
	inFiles := map[string]bool{}
	for _, h := range hits {
		inFiles[h.path] = true
		where := h.path
		if h.Line > 0 {
			where = fmt.Sprintf("%s:%d", h.path, h.Line)
		} else {
			where += " (file name)"
		}
		fmt.Printf("%s: %s: %s\n", where, h.Rule, h.Match)
	}
	wordNote := fmt.Sprintf("%d private word(s) from %s", len(words), source)
	if len(words) == 0 {
		wordNote = "no private words: the private-word rule did not run"
	}
	if len(hits) > 0 {
		fmt.Fprintf(os.Stderr, "private: %d finding(s) in %d of %d file(s); %s. `tools/check-private.sh -rules` says what passes.\n",
			len(hits), len(inFiles), nread, wordNote)
		return privacy.Found
	}
	// An empty word list is not a clean tree. The rules that need it did not
	// run, so this run has not established what it is asked to establish, and
	// saying "clean" would be the quiet green a fresh clone and a CI runner
	// would get for ever.
	if len(words) == 0 {
		fmt.Printf("private: %d files read; nothing the fixed rules catch\n", nread)
		fmt.Fprintf(os.Stderr, "private: undetermined: no private-word list. Put one line per word in %s, or point CLAWDLINE_PRIVATE_WORDS at it. An empty list is not a clean tree.\n",
			wordsPath(root))
		return privacy.Undetermined
	}
	fmt.Printf("private: %d files clean; %s\n", nread, wordNote)
	return privacy.Clean
}

// wordsPath is where the list is looked for, said back to somebody who has
// not got one. It is the same path privateWords reads.
func wordsPath(root string) string {
	if p := os.Getenv("CLAWDLINE_PRIVATE_WORDS"); p != "" {
		return p
	}
	out, err := exec.Command("git", "-C", root, "rev-parse", "--git-common-dir").Output()
	if err != nil {
		return "$(git rev-parse --git-common-dir)/info/private-words"
	}
	dir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, dir)
	}
	return filepath.Join(dir, "info", "private-words")
}

func repoRoot() (string, error) {
	if out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output(); err == nil {
		return strings.TrimSpace(string(out)), nil
	}
	// No git: the repository is wherever go.mod is, walking up from here.
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no git repository and no go.mod above the working directory")
		}
		dir = parent
	}
}

// published is every path git would commit, relative with forward slashes.
func published(root string, pathspecs []string) ([]string, error) {
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		args := append([]string{"-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--"}, pathspecs...)
		out, err := exec.Command("git", args...).Output()
		if err != nil {
			return nil, fmt.Errorf("git ls-files: %w", err)
		}
		seen := map[string]bool{}
		var files []string
		for _, p := range strings.Split(string(out), "\x00") {
			if p != "" && !seen[p] {
				seen[p] = true
				files = append(files, p)
			}
		}
		return files, nil
	}
	if len(pathspecs) > 0 {
		return nil, errors.New("pathspecs need git")
	}
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	return files, err
}

// content is what publishing path publishes: a file's bytes, or a symlink's
// target. A binary file is skipped (ok false), and so is a tracked file that
// is gone from the working tree; neither is an error.
// everyFile is every regular file and symlink under dir, slash-separated and
// relative to it.
func everyFile(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out, err
}

// contentOrStrings is content, except that a binary file is read as its
// printable strings, one per line, as `strings` would print them: a release
// binary carries every literal compiled into it, and that is what it would
// publish.
func contentOrStrings(path string) ([]byte, bool, bool, error) {
	data, ok, err := content(path)
	if err != nil || ok {
		return data, ok, false, err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, false, false, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false, false, err
	}
	return printableRuns(raw, 6), true, true, nil
}

// printableRuns is every run of at least min printable ASCII bytes, each on
// its own line.
func printableRuns(raw []byte, min int) []byte {
	var out bytes.Buffer
	start := -1
	flush := func(end int) {
		if start >= 0 && end-start >= min {
			out.Write(raw[start:end])
			out.WriteByte('\n')
		}
		start = -1
	}
	for i, b := range raw {
		if b >= 0x20 && b < 0x7f || b == '\t' {
			if start < 0 {
				start = i
			}
			continue
		}
		flush(i)
	}
	flush(len(raw))
	return out.Bytes()
}

func content(path string) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return nil, false, err
		}
		return []byte(target), true, nil
	}
	if !info.Mode().IsRegular() {
		return nil, false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	head := data
	if len(head) > 8000 {
		head = head[:8000]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return nil, false, nil
	}
	return data, true, nil
}

// privateWords reads the person's own list. Its absence is normal — a fresh
// clone and a CI runner have none — and is said, not failed.
func privateWords(root string) ([]string, string, error) {
	path := os.Getenv("CLAWDLINE_PRIVATE_WORDS")
	if path == "" {
		out, err := exec.Command("git", "-C", root, "rev-parse", "--git-common-dir").Output()
		if err != nil {
			return nil, "", nil
		}
		dir := strings.TrimSpace(string(out))
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(root, dir)
		}
		path = filepath.Join(dir, "info", "private-words")
		if _, err := os.Stat(path); err != nil {
			return nil, "", nil
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("private words: %w", err)
	}
	var words []string
	for _, w := range strings.Split(string(data), "\n") {
		if w = strings.TrimSpace(w); w != "" && !strings.HasPrefix(w, "#") {
			words = append(words, w)
		}
	}
	return words, path, nil
}

func printRules() {
	for _, r := range privacy.Rules {
		fmt.Printf("%s\n  catches: %s\n  passes:  %s\n\n", r.Name, r.Catches, r.Passes)
	}
	fmt.Println("standing exceptions (internal/domain/privacy Allowed):")
	for _, a := range privacy.Allowed {
		where := a.Path
		if where == "" {
			where = "any file"
		}
		fmt.Printf("  %s %q in %s: %s\n", a.Rule, a.Text, where, a.Why)
	}
}
