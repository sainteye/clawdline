package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline-go/internal/domain/privacy"
)

// The history scan, driven against a repository built for it. The live case —
// this repository's own docs/cutover.md at ef067d70 — is what the work was
// measured on and cannot be a test: the word it carries is the person's, and
// a fixture that spelled it would publish the thing the checker exists to
// stop. So the fixture invents a word of its own, and what is tested is the
// mechanism: a word committed and taken out is still found, a word still in
// the tree is told apart from it, and a run with no list does not say clean.

const fixtureWord = "acmecorp" // not on anybody's list; this file is published

type repo struct {
	t   *testing.T
	dir string
}

func fixtureRepo(t *testing.T) *repo {
	t.Helper()
	// A repository of our own, with none of this machine's git configuration
	// in it: a global hook or a signing key would decide whether the test
	// passes.
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	r := &repo{t: t, dir: t.TempDir()}
	r.git("init", "-b", "main")
	r.git("config", "user.name", "Fixture")
	r.git("config", "user.email", "fixture@example.invalid")
	return r
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", r.dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// commit writes each file and commits them, and answers the commit's sha.
func (r *repo) commit(message string, files map[string]string) string {
	r.t.Helper()
	for path, body := range files {
		full := filepath.Join(r.dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			r.t.Fatal(err)
		}
		if body == "" {
			os.Remove(full)
			continue
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			r.t.Fatal(err)
		}
	}
	r.git("add", "-A")
	r.git("commit", "-q", "-m", message)
	return r.git("rev-parse", "HEAD")
}

// words writes a private-word list and points the checker at it.
func (r *repo) words(list ...string) {
	r.t.Helper()
	path := filepath.Join(r.t.TempDir(), "private-words")
	if err := os.WriteFile(path, []byte(strings.Join(list, "\n")+"\n"), 0o600); err != nil {
		r.t.Fatal(err)
	}
	r.t.Setenv("CLAWDLINE_PRIVATE_WORDS", path)
}

// scan runs the history checker inside the repository and answers its verdict
// and everything it printed.
func (r *repo) scan(o historyOptions) (privacy.Answer, string) {
	r.t.Helper()
	r.t.Chdir(r.dir)
	out, err := os.CreateTemp(r.t.TempDir(), "out")
	if err != nil {
		r.t.Fatal(err)
	}
	stdout, stderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = out, out
	answer := runHistory(o)
	os.Stdout, os.Stderr = stdout, stderr
	out.Close()
	printed, err := os.ReadFile(out.Name())
	if err != nil {
		r.t.Fatal(err)
	}
	return answer, string(printed)
}

func noCheckpoint(r *repo) historyOptions {
	return historyOptions{revs: "HEAD", checkpoint: filepath.Join(r.t.TempDir(), "cp")}
}

// TestAWordTakenOutOfTheTreeIsStillInTheHistory is the whole case: the tree
// scan is green on this and `git push` publishes it anyway.
func TestAWordTakenOutOfTheTreeIsStillInTheHistory(t *testing.T) {
	r := fixtureRepo(t)
	r.words(fixtureWord)
	first := r.commit("the note", map[string]string{
		"docs/note.md": "one\ntwo\nthe " + fixtureWord + " migration\nfour\n",
	})
	r.commit("take it out", map[string]string{"docs/note.md": "one\ntwo\nthe migration\nfour\n"})

	answer, printed := r.scan(noCheckpoint(r))
	if answer != privacy.Found {
		t.Fatalf("answer %s, want found\n%s", answer, printed)
	}
	want := fmt.Sprintf("%s %s docs/note.md:3: private-word", first[:8], r.git("show", "-s", "--format=%cs", first))
	if !strings.Contains(printed, want) {
		t.Errorf("nothing named %q:\n%s", want, printed)
	}
	if !strings.Contains(printed, "docs/note.md:3: private-word — history only, not in the working tree") {
		t.Errorf("it did not say the tree is clean of it:\n%s", printed)
	}
	// The report is a document somebody keeps. It may not carry the word.
	if strings.Contains(printed, fixtureWord) {
		t.Errorf("the report printed the word it caught:\n%s", printed)
	}
}

// TestTheTreeAnswerIsTheOtherHalf: a word still on disk is a different job
// from one only the history has, and the report has to say which. There is a
// third case between them, which the first shape of this check got wrong: a
// word taken out of one file and left in another is gone from the line being
// reported and still published, and "still in the working tree" pointed at a
// file that no longer holds it.
func TestTheTreeAnswerIsTheOtherHalf(t *testing.T) {
	r := fixtureRepo(t)
	r.words(fixtureWord)
	r.commit("the note", map[string]string{"docs/note.md": "a " + fixtureWord + " line\n"})
	r.commit("take it out of one of them", map[string]string{
		"docs/note.md": "a line\n",
		"docs/live.md": "still the " + fixtureWord + " here\n",
	})

	answer, printed := r.scan(noCheckpoint(r))
	if answer != privacy.Found {
		t.Fatalf("answer %s, want found\n%s", answer, printed)
	}
	if !strings.Contains(printed, "docs/live.md:1: private-word — still in the working tree") {
		t.Errorf("the live one was not called live:\n%s", printed)
	}
	if !strings.Contains(printed, "docs/note.md:1: private-word — gone from this file; the working tree carries it in docs/live.md") {
		t.Errorf("the moved one was not told apart from a fixed one:\n%s", printed)
	}
	if !strings.Contains(printed, "2 the working tree still carries, 0 in history only") {
		t.Errorf("the summary does not count them apart:\n%s", printed)
	}
}

// TestNoWordListIsNotACleanHistory: on another machine and on CI there is no
// list. The rules that need one cannot fire, and green would be a lie.
func TestNoWordListIsNotACleanHistory(t *testing.T) {
	r := fixtureRepo(t)
	r.words() // an empty list, which is what an absent one amounts to
	r.commit("the note", map[string]string{"docs/note.md": "a " + fixtureWord + " line\n"})

	answer, printed := r.scan(noCheckpoint(r))
	if answer != privacy.Undetermined {
		t.Fatalf("answer %s, want undetermined\n%s", answer, printed)
	}
	if !strings.Contains(printed, "undetermined: no private-word list") {
		t.Errorf("it did not say why:\n%s", printed)
	}
}

// TestTheSecondRunReadsOnlyWhatIsNew, and says everything the first one did.
// A checkpoint that dropped a standing finding would turn a red history green
// on its second run, which is the failure this whole file is about.
func TestTheSecondRunReadsOnlyWhatIsNew(t *testing.T) {
	r := fixtureRepo(t)
	r.words(fixtureWord)
	first := r.commit("the note", map[string]string{"docs/note.md": "a " + fixtureWord + " line\n"})
	r.commit("take it out", map[string]string{"docs/note.md": "a line\n"})
	cp := filepath.Join(t.TempDir(), "cp")

	if answer, printed := r.scan(historyOptions{revs: "HEAD", checkpoint: cp}); answer != privacy.Found {
		t.Fatalf("first run: %s\n%s", answer, printed)
	}
	answer, printed := r.scan(historyOptions{revs: "HEAD", checkpoint: cp})
	if answer != privacy.Found {
		t.Fatalf("second run: %s, want found\n%s", answer, printed)
	}
	if !strings.Contains(printed, "read 0 commit(s)") {
		t.Errorf("the second run read commits it had read:\n%s", printed)
	}
	if !strings.Contains(printed, first[:8]+" ") {
		t.Errorf("the second run forgot the finding:\n%s", printed)
	}

	// A third commit is the only thing the third run reads, and the standing
	// finding is still reported beside it.
	third := r.commit("another", map[string]string{"docs/other.md": "another " + fixtureWord + " line\n"})
	answer, printed = r.scan(historyOptions{revs: "HEAD", checkpoint: cp})
	if answer != privacy.Found {
		t.Fatalf("third run: %s\n%s", answer, printed)
	}
	if !strings.Contains(printed, "read 1 commit(s)") {
		t.Errorf("the third run did not read exactly the new commit:\n%s", printed)
	}
	for _, want := range []string{first[:8] + " ", third[:8] + " "} {
		if !strings.Contains(printed, want) {
			t.Errorf("the third run did not name %s:\n%s", want, printed)
		}
	}
}

// TestAnEditedCheckpointCostsAFullRead: the one thing a checkpoint can do is
// let a run skip commits, so a checkpoint nobody wrote must not be able to.
func TestAnEditedCheckpointCostsAFullRead(t *testing.T) {
	r := fixtureRepo(t)
	r.words(fixtureWord)
	first := r.commit("the note", map[string]string{"docs/note.md": "a " + fixtureWord + " line\n"})
	r.commit("take it out", map[string]string{"docs/note.md": "a line\n"})
	cp := filepath.Join(t.TempDir(), "cp")
	if answer, printed := r.scan(historyOptions{revs: "HEAD", checkpoint: cp}); answer != privacy.Found {
		t.Fatalf("first run: %s\n%s", answer, printed)
	}

	// Take the finding out of the checkpoint, as somebody who wanted a green
	// board would. The seal is over it.
	body, err := os.ReadFile(cp)
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	for _, line := range strings.Split(string(body), "\n") {
		if !strings.HasPrefix(line, "finding ") {
			kept = append(kept, line)
		}
	}
	if err := os.WriteFile(cp, []byte(strings.Join(kept, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}

	answer, printed := r.scan(historyOptions{revs: "HEAD", checkpoint: cp})
	if answer != privacy.Found {
		t.Fatalf("an edited checkpoint answered %s, want found\n%s", answer, printed)
	}
	if !strings.Contains(printed, "checkpoint refused (unsealed") {
		t.Errorf("it did not say the checkpoint was refused:\n%s", printed)
	}
	if !strings.Contains(printed, "read 2 commit(s)") {
		t.Errorf("a refused checkpoint did not cost a full read:\n%s", printed)
	}
	if !strings.Contains(printed, first[:8]+" ") {
		t.Errorf("the finding did not come back:\n%s", printed)
	}
}

// TestAMergeResolutionIsSomebodysCommitToo: `git log --raw` shows a merge no
// diff, so a resolution that wrote something neither side had belongs to no
// commit the ordinary walk prints. This repository has one; without -m the
// finding is reported against whichever commit later touched the blob.
func TestAMergeResolutionIsSomebodysCommitToo(t *testing.T) {
	r := fixtureRepo(t)
	r.words(fixtureWord)
	r.commit("base", map[string]string{"docs/note.md": "base\n"})
	base := r.git("rev-parse", "HEAD")
	r.commit("on main", map[string]string{"docs/note.md": "main\n"})
	r.git("checkout", "-q", "-b", "side", base)
	r.commit("on side", map[string]string{"docs/note.md": "side\n"})
	r.git("checkout", "-q", "main")
	if out, err := exec.Command("git", "-C", r.dir, "merge", "--no-commit", "side").CombinedOutput(); err == nil {
		t.Fatalf("the merge did not conflict, so there is nothing to resolve: %s", out)
	}
	// The resolution writes a line neither branch had.
	merge := r.commit("Merge side", map[string]string{"docs/note.md": "resolved with " + fixtureWord + "\n"})
	r.commit("take it out", map[string]string{"docs/note.md": "resolved\n"})

	answer, printed := r.scan(noCheckpoint(r))
	if answer != privacy.Found {
		t.Fatalf("answer %s, want found\n%s", answer, printed)
	}
	if !strings.Contains(printed, merge[:8]+" ") {
		t.Errorf("the merge that wrote it was not named:\n%s", printed)
	}
	if strings.Contains(printed, "unattributed") {
		t.Errorf("a finding belongs to no commit:\n%s", printed)
	}
}

// TestAnObjectTooBigToReadIsNotAnObjectWithNothingInIt: the one rule this
// whole file is written around. An object past MaximumObjectBytes is not
// skipped quietly — the run says which object, and answers undetermined.
func TestAnObjectTooBigToReadIsNotAnObjectWithNothingInIt(t *testing.T) {
	r := fixtureRepo(t)
	r.words(fixtureWord)
	big := strings.Repeat("a line of perfectly ordinary text\n", (privacy.MaximumObjectBytes/34)+1000)
	r.commit("a generated file nobody reads", map[string]string{"docs/big.txt": big})

	answer, printed := r.scan(noCheckpoint(r))
	if answer != privacy.Undetermined {
		t.Fatalf("answer %s, want undetermined\n%s", answer, printed)
	}
	if !strings.Contains(printed, "docs/big.txt") || !strings.Contains(printed, "it was not read") {
		t.Errorf("it did not name the object it could not read:\n%s", printed)
	}
}
