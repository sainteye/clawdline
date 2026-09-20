package privacy

import (
	"strings"
	"testing"
)

func aCheckpoint() *Checkpoint {
	return &Checkpoint{
		Salt:    "0123456789abcdef0123456789abcdef",
		Rules:   RulesDigest(),
		Words:   WordsDigest("0123456789abcdef0123456789abcdef", []string{"acmecorp"}),
		Scanned: "2026-09-20T12:00:00Z",
		Objects: 2584,
		Tips:    []string{strings.Repeat("a", 40), strings.Repeat("b", 40)},
		Findings: []Recorded{
			{Commit: strings.Repeat("c", 40), Blob: strings.Repeat("d", 40), Path: "docs/x.md", Line: 81, Rule: "private-word"},
		},
	}
}

// TestACheckpointSurvivesBeingWrittenDown: what one run leaves behind is what
// the next one reads, field for field. A checkpoint that loses a tip lets the
// next run skip a commit nobody scanned.
func TestACheckpointSurvivesBeingWrittenDown(t *testing.T) {
	t.Parallel()
	want := aCheckpoint()
	got, err := ParseCheckpoint(want.Marshal())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Salt != want.Salt || got.Rules != want.Rules || got.Words != want.Words ||
		got.Scanned != want.Scanned || got.Objects != want.Objects {
		t.Errorf("header: got %+v", got)
	}
	if strings.Join(got.Tips, ",") != strings.Join(want.Tips, ",") {
		t.Errorf("tips: got %v", got.Tips)
	}
	if len(got.Findings) != 1 || got.Findings[0] != want.Findings[0] {
		t.Errorf("findings: got %+v", got.Findings)
	}
	if string(got.Marshal()) != string(want.Marshal()) {
		t.Errorf("a round trip changed the bytes")
	}
}

// TestAnEditedCheckpointIsRefused is the point of the seal: the only thing a
// checkpoint can do is let a run skip commits, so every way of arriving at
// one nobody wrote must end in reading them. Each of these is a refusal with
// a reason, never a partial answer.
func TestAnEditedCheckpointIsRefused(t *testing.T) {
	t.Parallel()
	whole := string(aCheckpoint().Marshal())
	for name, edit := range map[string]func(string) string{
		"a finding taken out": func(s string) string {
			return strings.Replace(s, "finding "+strings.Repeat("c", 40), "# ", 1)
		},
		"a tip put in":      func(s string) string { return strings.Replace(s, "seal ", "tip "+strings.Repeat("e", 40)+"\nseal ", 1) },
		"the words changed": func(s string) string { return strings.Replace(s, "words ", "words ffffffffffffffff\n#", 1) },
		"one byte":          func(s string) string { return strings.Replace(s, "objects 2584", "objects 2585", 1) },
		"the seal dropped":  func(s string) string { return s[:strings.LastIndex(s, "seal ")] },
		"truncated":         func(s string) string { return s[:len(s)/2] },
		"another format":    func(s string) string { return "something else\n" + s },
		"empty":             func(string) string { return "" },
	} {
		_, err := ParseCheckpoint([]byte(edit(whole)))
		if err == nil {
			t.Errorf("%s: parsed", name)
			continue
		}
		if _, ok := Refused(err); !ok {
			t.Errorf("%s: %v is not a refusal", name, err)
		}
	}
}

// TestACheckpointOnlyAnswersItsOwnQuestion: the rules and the word list are
// what the scan was, and a scan of a different question is not an answer to
// this one. A new rule re-reads the history it was not there for.
func TestACheckpointOnlyAnswersItsOwnQuestion(t *testing.T) {
	t.Parallel()
	c := aCheckpoint()
	if err := c.Usable(RulesDigest(), []string{"acmecorp"}); err != nil {
		t.Fatalf("its own question: %v", err)
	}
	if err := c.Usable(RulesDigest(), []string{"acmecorp", "nightshift"}); err == nil {
		t.Error("a longer word list was accepted")
	} else if r, _ := Refused(err); r.Reason != "words-changed" {
		t.Errorf("word list: %v", err)
	}
	if err := c.Usable("0000000000000000", []string{"acmecorp"}); err == nil {
		t.Error("another rule set was accepted")
	} else if r, _ := Refused(err); r.Reason != "rules-changed" {
		t.Errorf("rules: %v", err)
	}
	// The order and the case of the list are not the question; its content is.
	if err := c.Usable(RulesDigest(), []string{"", "AcmeCorp", "# a comment"}); err != nil {
		t.Errorf("the same list, written differently: %v", err)
	}
}

// TestTheWordsDigestDoesNotSayTheWord: the digest travels beside the list it
// is about, but it is the one field that could be run against a dictionary,
// and a salt is cheaper than deciding that nobody will.
func TestTheWordsDigestDoesNotSayTheWord(t *testing.T) {
	t.Parallel()
	words := []string{"acmecorp"}
	a := WordsDigest("0123456789abcdef0123456789abcdef", words)
	b := WordsDigest("fedcba9876543210fedcba9876543210", words)
	if a == b {
		t.Error("the same words digest the same under two salts")
	}
	if strings.Contains(a, "acme") {
		t.Errorf("the digest carries the word: %s", a)
	}
}

// TestTheRulesDigestMovesWithTheRules: it is what makes a checkpoint stop
// being believed when the question changes, so it has to notice.
func TestTheRulesDigestMovesWithTheRules(t *testing.T) {
	// Not parallel: it moves the package's own table and puts it back.
	before := RulesDigest()
	Allowed = append(Allowed, Allowance{Rule: "uuid", Text: "x", Why: "a test"})
	after := RulesDigest()
	Allowed = Allowed[:len(Allowed)-1]
	if before == after {
		t.Error("an allowance was added and the digest did not move")
	}
	if RulesDigest() != before {
		t.Error("the digest did not come back")
	}
}

// TestThereAreThreeAnswersAndNotTwo: the exit codes are the interface, and
// the third one is the whole reason this file exists.
func TestThereAreThreeAnswersAndNotTwo(t *testing.T) {
	t.Parallel()
	for a, want := range map[Answer]string{
		Clean: "clean", Found: "found", CannotCheck: "cannot check", Undetermined: "undetermined",
	} {
		if a.String() != want {
			t.Errorf("%d: %q, want %q", int(a), a.String(), want)
		}
	}
	if Clean != 0 || Found != 1 || CannotCheck != 2 || Undetermined != 3 {
		t.Error("the exit codes moved; tools/check-private.sh documents them")
	}
}
