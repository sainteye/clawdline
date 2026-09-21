// The history half of check-private. The working tree is one moment; what
// `git push` publishes is every moment. This reads the objects of the
// commits, not the files on disk, and answers three questions the tree scan
// cannot: which commit, whether the same thing is still in the tree, and —
// when it cannot know — that it cannot know.
//
// The unit of work is a git object, not a commit: the same blob lives in every
// tree after the commit that wrote it, so scanning per commit would read this
// repository's zz_generated.go three hundred times. Objects are deduplicated
// by git itself (`rev-list --objects`), and a commit is put back on a finding
// afterwards, from one cheap pass over the raw log.
//
// Nothing here prints what a rule matched. A privacy report is a document
// somebody keeps, and a report that quotes the word it caught has moved the
// leak rather than closed it. It prints the commit, the file and the line, and
// the person looks at it with `git show`.
package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/domain/privacy"
)

// historyOptions are the flags that only mean something in -history mode.
type historyOptions struct {
	revs       string // what to scan, as git rev-list spells it
	full       bool   // read everything, whatever the checkpoint says
	checkpoint string // where the checkpoint is; "-" is none
	onlyNew    bool   // red only for a finding the checkpoint had not already recorded
}

// found is one finding, before it is printed: the match is kept to compare
// with the working tree and is never written out.
type found struct {
	blob  string
	path  string
	line  int
	rule  string
	match string
}

func runHistory(o historyOptions) privacy.Answer {
	start := time.Now()
	root, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot check:", err)
		return privacy.CannotCheck
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		fmt.Fprintln(os.Stderr, "cannot check: -history needs a git repository")
		return privacy.CannotCheck
	}
	words, source, err := privateWords(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot check:", err)
		return privacy.CannotCheck
	}
	g := &git{root: root}

	revs := strings.Fields(o.revs)
	if len(revs) == 0 {
		revs = []string{"HEAD"}
	}
	tips, err := g.lines("rev-list", append([]string{"--no-walk"}, revs...)...)
	if err != nil || len(tips) == 0 {
		fmt.Fprintf(os.Stderr, "cannot check: %s names no commit: %v\n", o.revs, err)
		return privacy.CannotCheck
	}

	// The checkpoint, and the one thing it is allowed to do: name commits
	// whose objects this run may skip. Every way of not believing it ends in
	// the same place — a full scan — and says which way it was.
	var (
		base     []string
		carried  []privacy.Recorded
		salt     string
		cpNote   = "no checkpoint: reading the whole history"
		cpPath   = o.checkpoint
		writeCP  = cpPath != "-"
		previous *privacy.Checkpoint
	)
	if writeCP {
		if cpPath == "" {
			cpPath = g.infoFile("private-history")
		}
		if cpPath == "" {
			writeCP = false
			cpNote = "no checkpoint: this clone has no git info directory"
		}
	} else {
		cpNote = "no checkpoint: -checkpoint=- asked for a full read"
	}
	if writeCP {
		previous, err = readCheckpoint(cpPath)
		switch {
		case err != nil:
			if r, ok := privacy.Refused(err); ok {
				cpNote = "checkpoint refused (" + r.Reason + ": " + r.Detail + "): reading the whole history"
			} else {
				cpNote = "checkpoint unreadable (" + err.Error() + "): reading the whole history"
			}
		case previous == nil:
			// absent; cpNote already says so
		default:
			salt = previous.Salt
			if err := previous.Usable(privacy.RulesDigest(), words); err != nil {
				r, _ := privacy.Refused(err)
				cpNote = "checkpoint refused (" + r.Reason + "): reading the whole history"
			} else if o.full {
				cpNote = "checkpoint ignored (-full): reading the whole history"
			} else if missing := g.missingCommits(previous.Tips); len(missing) > 0 {
				cpNote = fmt.Sprintf("checkpoint refused (missing-tip: %s is not in this clone): reading the whole history", short(missing[0]))
			} else {
				base = previous.Tips
				carried = previous.Findings
				cpNote = fmt.Sprintf("checkpoint of %s: skipping what %d tip(s) already reached, from %s",
					previous.Scanned, len(base), short(base[0]))
			}
		}
	}

	// What to read. `--not` makes git do the set difference: every object
	// reachable from the tips and not from what was already scanned.
	rangeArgs := append([]string{}, tips...)
	if len(base) > 0 {
		rangeArgs = append(rangeArgs, "--not")
		rangeArgs = append(rangeArgs, base...)
	}
	commits, err := g.lines("rev-list", rangeArgs...)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot check: git rev-list:", err)
		return privacy.CannotCheck
	}
	objects, err := g.objects(rangeArgs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot check:", err)
		return privacy.CannotCheck
	}

	scanner := privacy.New(words)
	hits, read, bytesRead, oversize, err := scanObjects(g, scanner, objects)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot check:", err)
		return privacy.CannotCheck
	}

	// Put a commit back on each finding. One pass over the raw log names the
	// commit that introduced almost every blob; a blob that only a merge
	// carries is asked for by itself.
	where := map[string]commitAt{}
	if len(hits) > 0 {
		need := map[string]bool{}
		for _, h := range hits {
			need[h.blob] = true
		}
		if where, err = g.introduced(rangeArgs, need); err != nil {
			fmt.Fprintln(os.Stderr, "cannot check:", err)
			return privacy.CannotCheck
		}
		for blob := range need {
			if _, ok := where[blob]; !ok {
				if at, ok := g.findObject(tips, blob); ok {
					where[blob] = at
				}
			}
		}
	}

	// Is it still in the tree? "In the history" and "in the history and still
	// published today" are two different jobs for whoever reads this, and the
	// second one is fixable by editing a file.
	inTree, treeErr := treeMatches(root, scanner)
	if treeErr != nil {
		fmt.Fprintln(os.Stderr, "cannot check:", treeErr)
		return privacy.CannotCheck
	}

	// Everything this run found, plus what earlier runs found in the commits
	// this one skipped. A finding does not stop being true because the commit
	// carrying it was read yesterday.
	records := map[string]privacy.Recorded{}
	stillThere := map[string][]string{}
	for _, h := range hits {
		at := where[h.blob]
		path := h.path
		if at.path != "" {
			path = at.path
		}
		commit := at.commit
		if commit == "" {
			commit = "unattributed"
		}
		r := privacy.Recorded{Commit: commit, Blob: h.blob, Path: path, Line: h.line, Rule: h.rule}
		records[recordKey(r)] = r
		if paths := inTree[matchKey(h.rule, h.match)]; len(paths) > 0 {
			stillThere[recordKey(r)] = paths
		}
	}
	// A carried finding's blob was not read this time. Read those objects —
	// one git process for all of them — so that "is it still in the tree" is
	// answered for a carried finding too, rather than left blank because of
	// an optimisation.
	var reread []privacy.Recorded
	for _, r := range carried {
		if _, ok := records[recordKey(r)]; ok {
			continue
		}
		records[recordKey(r)] = r
		reread = append(reread, r)
	}
	for key, paths := range g.carriedInTree(scanner, reread, inTree) {
		stillThere[key] = paths
	}

	all := make([]privacy.Recorded, 0, len(records))
	for _, r := range records {
		all = append(all, r)
	}
	dates := g.dates(all)
	sort.Slice(all, func(i, j int) bool {
		if dates[all[i].Commit] != dates[all[j].Commit] {
			return dates[all[i].Commit] < dates[all[j].Commit]
		}
		if all[i].Path != all[j].Path {
			return all[i].Path < all[j].Path
		}
		if all[i].Line != all[j].Line {
			return all[i].Line < all[j].Line
		}
		return all[i].Rule < all[j].Rule
	})

	// Standing or new. A history that carries a finding nobody can take out
	// without rewriting it is red for ever, and a check that is red for ever
	// is a check people stop reading. -new keeps the standing ones printed
	// and counted, and asks the only question a daily run can act on: did
	// today's commits add one?
	standing := map[string]bool{}
	for _, r := range carried {
		standing[recordKey(r)] = true
	}
	commitsWith, filesWith, live, fresh := map[string]bool{}, map[string]bool{}, 0, 0
	for _, r := range all {
		commitsWith[r.Commit] = true
		filesWith[r.Path] = true
		paths := stillThere[recordKey(r)]
		if len(paths) > 0 {
			live++
		}
		age := ""
		if !standing[recordKey(r)] {
			fresh++
			if len(standing) > 0 {
				age = " [new]"
			}
		}
		fmt.Printf("%s %s %s:%d: %s — %s%s\n", short(r.Commit), dates[r.Commit], r.Path, r.Line, r.Rule, whereNow(r.Path, paths), age)
	}

	elapsed := time.Since(start)
	wordNote := fmt.Sprintf("%d private word(s) from %s", len(words), source)
	if len(words) == 0 {
		wordNote = "no private words: the private-word rule did not run"
	}
	fmt.Printf("private-history: read %d commit(s), %d object(s), %s in %.1fs; %s\n",
		len(commits), read, megabytes(bytesRead), elapsed.Seconds(), wordNote)
	fmt.Println("private-history: " + cpNote)

	// Found before Undetermined when both are true. Both are red, and
	// "there are 558 of them" is the one somebody can act on; the line above
	// says the answer is not complete, so neither is lost.
	answer := privacy.Clean
	red := len(all)
	if o.onlyNew {
		red = fresh
	}
	switch {
	case red > 0:
		answer = privacy.Found
	case len(words) == 0, len(oversize) > 0:
		answer = privacy.Undetermined
	}

	for _, o := range oversize {
		fmt.Fprintf(os.Stderr, "private-history: %s (%s, %s) is past the %s one object may be; it was not read\n",
			short(o.sha), o.path, megabytes(o.size), megabytes(privacy.MaximumObjectBytes))
	}
	if len(words) == 0 {
		fmt.Fprintf(os.Stderr, "private-history: undetermined: no private-word list. Put one line per word in %s, or point CLAWDLINE_PRIVATE_WORDS at it. An empty list is not a clean history.\n",
			g.infoFile("private-words"))
	}
	if len(all) > 0 {
		partial := ""
		if len(words) == 0 || len(oversize) > 0 {
			partial = " This is not the whole answer: see the undetermined line above."
		}
		age := fmt.Sprintf("%d already recorded, %d new; ", len(all)-fresh, fresh)
		if len(standing) == 0 {
			age = "" // nothing was carried, so "new" would mean "all of them"
		}
		fmt.Fprintf(os.Stderr, "private-history: %d finding(s) in %d commit(s), %d file(s); %s%d the working tree still carries, %d in history only. The word itself is not printed: `git show <commit>:<file>` reads the line.%s\n",
			len(all), len(commitsWith), len(filesWith), age, live, len(all)-live, partial)
		if o.onlyNew && fresh == 0 {
			fmt.Fprintln(os.Stderr, "private-history: -new: nothing here is new since the checkpoint. The findings above still stand; only rewriting the history takes a history-only one out.")
		}
	}

	// Only a run that read everything it meant to may move the checkpoint.
	// A findings-bearing run still moves it, carrying the findings with it:
	// this repository's history has one that cannot be taken out without
	// rewriting it, and a checkpoint that refused to advance past a standing
	// finding would make every run a full one for ever.
	if writeCP && answer != privacy.Undetermined && answer != privacy.CannotCheck {
		if salt == "" {
			salt = newSalt()
		}
		next := &privacy.Checkpoint{
			Salt:     salt,
			Rules:    privacy.RulesDigest(),
			Words:    privacy.WordsDigest(salt, words),
			Scanned:  time.Now().UTC().Format(time.RFC3339),
			Objects:  read,
			Tips:     g.independent(append(append([]string{}, tips...), base...)),
			Findings: all,
		}
		if err := writeCheckpoint(cpPath, next); err != nil {
			fmt.Fprintln(os.Stderr, "private-history: the checkpoint was not written:", err)
		}
	}
	return answer
}

func recordKey(r privacy.Recorded) string {
	return r.Commit + " " + r.Blob + " " + r.Path + " " + strconv.Itoa(r.Line) + " " + r.Rule
}

// matchKey is how a finding here is compared with one in the working tree.
// It never leaves the process.
func matchKey(rule, match string) string { return rule + "\x00" + strings.ToLower(match) }

// treeMatches is every rule-and-match the working tree carries right now, and
// the files carrying it. Which files, and not merely whether: a word taken
// out of one document and left in another is gone from the line being
// reported and still published, and those are two different things to do
// next.
func treeMatches(root string, scanner *privacy.Scanner) (map[string][]string, error) {
	files, err := published(root, nil)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, rel := range files {
		data, ok, err := content(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		for _, f := range scanner.Scan(rel, data) {
			key := matchKey(f.Rule, f.Match)
			if n := len(out[key]); n == 0 || out[key][n-1] != rel {
				out[key] = append(out[key], rel)
			}
		}
	}
	return out, nil
}

// where says what to do about one finding: the same thing at the same path,
// the same thing somewhere else, or nothing left in the tree at all.
func whereNow(path string, paths []string) string {
	if len(paths) == 0 {
		return "history only, not in the working tree"
	}
	for _, p := range paths {
		if p == path {
			return "still in the working tree"
		}
	}
	shown := paths
	if len(shown) > 2 {
		shown = shown[:2]
	}
	more := ""
	if len(paths) > len(shown) {
		more = fmt.Sprintf(" and %d more", len(paths)-len(shown))
	}
	return "gone from this file; the working tree carries it in " + strings.Join(shown, ", ") + more
}

type oversized struct {
	sha, path string
	size      int64
}

// scanObjects reads every blob once. A blob that is not text is skipped, as
// the tree scan skips a binary file; a blob too large to hold is not skipped
// but reported, because "I did not read it" and "there is nothing in it" are
// the two answers this whole file exists to keep apart.
func scanObjects(g *git, scanner *privacy.Scanner, objects []object) ([]found, int, int64, []oversized, error) {
	var (
		hits      []found
		oversize  []oversized
		read      int
		bytesRead int64
	)
	sizes, err := g.sizes(objects)
	if err != nil {
		return nil, 0, 0, nil, err
	}
	var want []object
	for _, o := range objects {
		s, ok := sizes[o.sha]
		if !ok {
			continue // not a blob
		}
		if s > privacy.MaximumObjectBytes {
			oversize = append(oversize, oversized{o.sha, o.path, s})
			continue
		}
		want = append(want, o)
	}
	err = g.contents(want, func(o object, data []byte) {
		head := data
		if len(head) > 8000 {
			head = head[:8000]
		}
		if bytes.IndexByte(head, 0) >= 0 {
			return
		}
		read++
		bytesRead += int64(len(data))
		for _, f := range scanner.Scan(o.path, data) {
			hits = append(hits, found{blob: o.sha, path: o.path, line: f.Line, rule: f.Rule, match: f.Match})
		}
	})
	if err != nil {
		return nil, 0, 0, nil, err
	}
	return hits, read, bytesRead, oversize, nil
}

func readCheckpoint(path string) (*privacy.Checkpoint, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return privacy.ParseCheckpoint(data)
}

func writeCheckpoint(path string, c *privacy.Checkpoint) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, c.Marshal(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func newSalt() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// A salt that is not random only makes the words digest guessable by
		// somebody who already has the file it sits next to.
		return fmt.Sprintf("%032x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func megabytes(n int64) string {
	if n < 1<<20 {
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// --- git

type git struct{ root string }

type object struct{ sha, path string }

type commitAt struct{ commit, path string }

func (g *git) run(args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", g.root}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func (g *git) lines(name string, args ...string) ([]string, error) {
	out, err := g.run(append([]string{name}, args...)...)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

// infoFile is a path in the git directory every worktree of this clone
// shares, which is where the word list lives and where nothing is committed.
func (g *git) infoFile(name string) string {
	out, err := g.run("rev-parse", "--git-common-dir")
	if err != nil {
		return ""
	}
	dir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(g.root, dir)
	}
	return filepath.Join(dir, "info", name)
}

// missingCommits are the recorded tips this clone no longer has: history was
// rewritten, or the checkpoint came from somewhere else.
func (g *git) missingCommits(tips []string) []string {
	var missing []string
	for _, t := range tips {
		if _, err := g.run("rev-parse", "--verify", "--quiet", t+"^{commit}"); err != nil {
			missing = append(missing, t)
		}
	}
	return missing
}

// independent reduces a set of tips to the ones that are not an ancestor of
// another, so the checkpoint does not grow a tip per run.
func (g *git) independent(tips []string) []string {
	seen := map[string]bool{}
	var unique []string
	for _, t := range tips {
		if !seen[t] {
			seen[t] = true
			unique = append(unique, t)
		}
	}
	if len(unique) < 2 {
		return unique
	}
	out, err := g.lines("merge-base", append([]string{"--independent"}, unique...)...)
	if err != nil || len(out) == 0 {
		return unique
	}
	sort.Strings(out)
	return out
}

// objects is every object in the range, with the path git names it by. A blob
// under several paths is listed once, under the first.
func (g *git) objects(rangeArgs []string) ([]object, error) {
	out, err := g.run(append([]string{"rev-list", "--objects"}, rangeArgs...)...)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var objects []object
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		sha, path, _ := strings.Cut(line, " ")
		if path == "" || seen[sha] {
			continue // a commit or the root tree, or a blob already listed
		}
		seen[sha] = true
		objects = append(objects, object{sha, path})
	}
	return objects, nil
}

// sizes keeps the blobs, by asking git what each object is. Trees answer here
// too and are dropped.
func (g *git) sizes(objects []object) (map[string]int64, error) {
	if len(objects) == 0 {
		return map[string]int64{}, nil
	}
	cmd := exec.Command("git", "-C", g.root, "cat-file", "--batch-check=%(objectname) %(objecttype) %(objectsize)")
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		w := bufio.NewWriter(in)
		for _, o := range objects {
			fmt.Fprintln(w, o.sha)
		}
		w.Flush()
		in.Close()
	}()
	sizes := map[string]int64{}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		parts := strings.Fields(sc.Text())
		if len(parts) != 3 || parts[1] != "blob" {
			continue
		}
		n, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			continue
		}
		sizes[parts[0]] = n
	}
	if err := sc.Err(); err != nil {
		cmd.Wait()
		return nil, fmt.Errorf("git cat-file --batch-check: %w", err)
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("git cat-file --batch-check: %w", err)
	}
	return sizes, nil
}

// contents hands each blob's bytes to visit, in one git process.
func (g *git) contents(objects []object, visit func(object, []byte)) error {
	if len(objects) == 0 {
		return nil
	}
	cmd := exec.Command("git", "-C", g.root, "cat-file", "--batch")
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	outPipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		w := bufio.NewWriter(in)
		for _, o := range objects {
			fmt.Fprintln(w, o.sha)
		}
		w.Flush()
		in.Close()
	}()
	r := bufio.NewReaderSize(outPipe, 1<<16)
	byPath := map[string]object{}
	for _, o := range objects {
		byPath[o.sha] = o
	}
	for range objects {
		header, err := r.ReadString('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			cmd.Wait()
			return fmt.Errorf("git cat-file --batch: %w", err)
		}
		parts := strings.Fields(strings.TrimSpace(header))
		if len(parts) != 3 {
			continue // "<sha> missing"
		}
		size, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			cmd.Wait()
			return fmt.Errorf("git cat-file --batch: %q", header)
		}
		data := make([]byte, size+1) // git writes a newline after the object
		if _, err := io.ReadFull(r, data); err != nil {
			cmd.Wait()
			return fmt.Errorf("git cat-file --batch: %w", err)
		}
		visit(byPath[parts[0]], data[:size])
	}
	return cmd.Wait()
}

// introduced names, for each wanted blob, the first commit that wrote it and
// the path it wrote it at, from one pass over the raw log.
//
// It walks with -m, which diffs a merge against each of its parents. Without
// it a merge shows no diff at all, and a resolution that wrote something
// neither side had — which is how this repository's cutover.md got one of its
// versions — belongs to no commit the log ever prints. Measured here: the
// pass costs 0.15s either way, and the fallback below costs 0.4s per blob.
func (g *git) introduced(rangeArgs []string, want map[string]bool) (map[string]commitAt, error) {
	args := append([]string{"log", "--format=commit %H", "--raw", "-z", "-m", "--no-abbrev", "--no-renames", "--root"}, rangeArgs...)
	out, err := g.run(args...)
	if err != nil {
		return nil, err
	}
	at := map[string]commitAt{}
	commit := ""
	fields := strings.Split(string(out), "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if f == "" {
			continue
		}
		// A commit line and the first raw entry after it arrive in one field,
		// separated by newlines: "commit <sha>\n\n:100644 100644 <a> <b> M".
		for _, line := range strings.Split(f, "\n") {
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "commit "):
				commit = strings.TrimPrefix(line, "commit ")
			case strings.HasPrefix(line, ":"):
				parts := strings.Fields(line)
				if len(parts) < 5 || i+1 >= len(fields) {
					continue
				}
				i++
				path := fields[i]
				blob := parts[3]
				if want[blob] {
					at[blob] = commitAt{commit, path} // the log walks newest first; the last write wins, which is the oldest
				}
			}
		}
	}
	return at, nil
}

// findObject is the expensive way to place one blob, for one the pass above
// still did not reach. It asks only about the blob, and only accepts a commit
// that wrote it: --find-object matches the version before a change as well as
// the one after, and answering with the commit that replaced a blob would
// name a file that no longer holds the thing being reported.
func (g *git) findObject(tips []string, blob string) (commitAt, bool) {
	args := append([]string{"log", "--format=commit %H", "--raw", "-m", "--no-abbrev", "--no-renames", "--find-object=" + blob}, tips...)
	out, err := g.run(args...)
	if err != nil {
		return commitAt{}, false
	}
	at, commit := commitAt{}, ""
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "commit "); ok {
			commit = after
		}
		if strings.HasPrefix(line, ":") {
			if parts := strings.Fields(line); len(parts) >= 6 && parts[3] == blob {
				at = commitAt{commit, parts[5]} // newest first: the last one kept is the oldest
			}
		}
	}
	return at, at.commit != ""
}

// carriedInTree answers, for findings this run did not read again, which of
// them the working tree still carries. The blobs are read in one batch: a git
// process per finding turned a run that reads nothing into an eleven-second
// one, which is the cost the checkpoint exists to remove.
func (g *git) carriedInTree(scanner *privacy.Scanner, carried []privacy.Recorded, inTree map[string][]string) map[string][]string {
	out := map[string][]string{}
	if len(carried) == 0 {
		return out
	}
	byBlob := map[string][]privacy.Recorded{}
	var objects []object
	for _, r := range carried {
		if _, seen := byBlob[r.Blob]; !seen {
			objects = append(objects, object{r.Blob, r.Path})
		}
		byBlob[r.Blob] = append(byBlob[r.Blob], r)
	}
	type at struct {
		line int
		rule string
	}
	err := g.contents(objects, func(o object, data []byte) {
		live := map[at][]string{}
		for _, f := range scanner.Scan(o.path, data) {
			if paths := inTree[matchKey(f.Rule, f.Match)]; len(paths) > 0 {
				live[at{f.Line, f.Rule}] = paths
			}
		}
		for _, r := range byBlob[o.sha] {
			if paths := live[at{r.Line, r.Rule}]; len(paths) > 0 {
				out[recordKey(r)] = paths
			}
		}
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "private-history: a carried finding could not be read again:", err)
	}
	return out
}

// dates is each finding's commit date, so the report reads oldest first.
func (g *git) dates(records []privacy.Recorded) map[string]string {
	out := map[string]string{}
	for _, r := range records {
		if r.Commit == "" || out[r.Commit] != "" {
			continue
		}
		if b, err := g.run("show", "-s", "--format=%cs", r.Commit); err == nil {
			out[r.Commit] = strings.TrimSpace(string(b))
		} else {
			out[r.Commit] = "?"
		}
	}
	return out
}
