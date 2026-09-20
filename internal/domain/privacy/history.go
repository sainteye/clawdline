// The working tree is one moment; a public repository publishes every moment
// it ever had. A word committed on Thursday and taken out on Friday is gone
// from the tree, and still in the history that `git push` sends. The checker
// that reads only the tree is green on that, which is the shape of the thing
// this file exists to stop.
//
// What is here is the part that has no git and no disk in it: the three
// answers a run can give, and the checkpoint that lets a daily run read only
// what is new. The git plumbing is tools/check-private.
package privacy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// MaximumObjectBytes is the largest git object one history scan reads. An
// object past it is not scanned, and a scan that could not read an object
// cannot say the history is clean: it answers Undetermined and names the
// object. Skipping it quietly would be the same green this file exists to
// stop.
const MaximumObjectBytes = 16 << 20

// MaximumCheckpointBytes is the largest checkpoint file that is read. A larger
// one is not one of ours and is refused rather than parsed; refusing it costs
// a full scan, which is the safe direction. It also bounds what a checkpoint
// may remember: a history with more findings than fit here is one that gets
// read in full every time, which is the right answer for it.
const MaximumCheckpointBytes = 1 << 20

// Answer is what one run concluded, and the process's exit code. There are
// three of them and not two: a run that could not read the person's word list
// has not found anything, and has not established that there is nothing.
type Answer int

const (
	// Clean: everything in scope was read, and no rule fired.
	Clean Answer = 0
	// Found: at least one finding. The run says where.
	Found Answer = 1
	// CannotCheck: the run could not start — no repository, a failed build, a
	// git command that did not answer.
	CannotCheck Answer = 2
	// Undetermined: the run read what it could and the answer is not known.
	// An empty word list is this, not Clean: on another machine and on CI the
	// list is absent, and the rules that need it cannot fire at all.
	Undetermined Answer = 3
)

func (a Answer) String() string {
	switch a {
	case Clean:
		return "clean"
	case Found:
		return "found"
	case CannotCheck:
		return "cannot check"
	case Undetermined:
		return "undetermined"
	}
	return "answer(" + strconv.Itoa(int(a)) + ")"
}

// Recorded is one finding a past scan made, kept so that the next scan can
// repeat it without reading the history again. It carries no matched text:
// the report of a privacy check is itself a document that gets published, and
// a report that prints the word it caught has only moved the leak. The commit,
// the blob, the path and the line are enough to look at it locally.
type Recorded struct {
	Commit string
	Blob   string
	Path   string
	Line   int
	Rule   string
}

func (r Recorded) key() string {
	return r.Commit + " " + r.Blob + " " + r.Path + " " + strconv.Itoa(r.Line) + " " + r.Rule
}

// Checkpoint is what a scan leaves behind so the next one can read only what
// is new: the commits whose whole history it read, and what it found there.
//
// It is a cache and never an authority. The only thing it can do is let a run
// skip commits reachable from Tips, and a run that skips says so and names
// them. It is sealed so that corruption, truncation and an edit are refused
// rather than believed — not so that it cannot be forged, which is not a
// question worth asking of a file sitting beside the guard it belongs to.
//
// Where it is kept and when it is written are the caller's; Usable is what
// decides whether it may be believed at all.
type Checkpoint struct {
	// Salt is made once, by the caller, and keeps Words from being a digest
	// anybody can run a word list against.
	Salt string
	// Rules is RulesDigest at the time of the scan. A changed rule set asks a
	// different question, so the answer does not carry over.
	Rules string
	// Words is WordsDigest of the list that scan used.
	Words string
	// Scanned is when, RFC3339. It is printed, never compared.
	Scanned string
	// Objects is how many git objects that scan read. Printed, never compared.
	Objects int
	// Tips are the commits whose history was scanned.
	Tips []string
	// Findings is what was found in it.
	Findings []Recorded
}

// checkpointHeader is the first line, and the format's version.
const checkpointHeader = "clawdline-private-history 1"

// Marshal writes the checkpoint, seal last. Tips and findings are sorted, so
// the same scan writes the same bytes.
func (c *Checkpoint) Marshal() []byte {
	var b strings.Builder
	b.WriteString(checkpointHeader + "\n")
	fmt.Fprintf(&b, "salt %s\n", c.Salt)
	fmt.Fprintf(&b, "rules %s\n", c.Rules)
	fmt.Fprintf(&b, "words %s\n", c.Words)
	fmt.Fprintf(&b, "scanned %s\n", c.Scanned)
	fmt.Fprintf(&b, "objects %d\n", c.Objects)
	tips := append([]string(nil), c.Tips...)
	sort.Strings(tips)
	for _, t := range tips {
		fmt.Fprintf(&b, "tip %s\n", t)
	}
	found := append([]Recorded(nil), c.Findings...)
	sort.Slice(found, func(i, j int) bool { return found[i].key() < found[j].key() })
	for _, f := range found {
		fmt.Fprintf(&b, "finding %s\n", f.key())
	}
	body := b.String()
	return []byte(body + "seal " + seal(body) + "\n")
}

// CheckpointRefusal is a checkpoint that may not be believed, and why. Every
// one of them costs a full scan; none of them is a reason to answer Clean.
type CheckpointRefusal struct {
	// Reason is one word, for a caller that wants to branch: "malformed",
	// "unsealed", "rules-changed", "words-changed".
	Reason string
	Detail string
}

func (e *CheckpointRefusal) Error() string { return e.Reason + ": " + e.Detail }

// Refused reports whether err is a refusal, and which.
func Refused(err error) (*CheckpointRefusal, bool) {
	var r *CheckpointRefusal
	ok := errors.As(err, &r)
	return r, ok
}

// ParseCheckpoint reads one back. Anything it cannot account for — a line it
// does not know, a repeated field, a seal that does not match what it is over
// — is a refusal and not a partial answer: a checkpoint understood by halves
// is how a run skips a commit nobody scanned.
func ParseCheckpoint(data []byte) (*Checkpoint, error) {
	if len(data) > MaximumCheckpointBytes {
		return nil, &CheckpointRefusal{"malformed", fmt.Sprintf("%d bytes is past the %d this format keeps", len(data), MaximumCheckpointBytes)}
	}
	text := strings.TrimSuffix(string(data), "\n")
	if text == "" {
		return nil, &CheckpointRefusal{"malformed", "empty"}
	}
	lines := strings.Split(text, "\n")
	if lines[0] != checkpointHeader {
		return nil, &CheckpointRefusal{"malformed", "first line is not " + strconv.Quote(checkpointHeader)}
	}
	last := lines[len(lines)-1]
	want, ok := strings.CutPrefix(last, "seal ")
	if !ok {
		return nil, &CheckpointRefusal{"malformed", "no seal on the last line"}
	}
	body := strings.Join(lines[:len(lines)-1], "\n") + "\n"
	if got := seal(body); got != want {
		return nil, &CheckpointRefusal{"unsealed", "the seal is not over these bytes"}
	}

	c := &Checkpoint{}
	seen := map[string]bool{}
	once := func(field, value string) error {
		if seen[field] {
			return &CheckpointRefusal{"malformed", "two " + field + " lines"}
		}
		seen[field] = true
		if value == "" {
			return &CheckpointRefusal{"malformed", field + " is empty"}
		}
		return nil
	}
	for _, line := range lines[1 : len(lines)-1] {
		field, value, _ := strings.Cut(line, " ")
		switch field {
		case "salt", "rules", "words", "scanned", "objects":
			if err := once(field, value); err != nil {
				return nil, err
			}
		}
		switch field {
		case "salt":
			c.Salt = value
		case "rules":
			c.Rules = value
		case "words":
			c.Words = value
		case "scanned":
			c.Scanned = value
		case "objects":
			n, err := strconv.Atoi(value)
			if err != nil {
				return nil, &CheckpointRefusal{"malformed", "objects " + strconv.Quote(value)}
			}
			c.Objects = n
		case "tip":
			if !isHex(value, 40) && !isHex(value, 64) {
				return nil, &CheckpointRefusal{"malformed", "tip " + strconv.Quote(value) + " is not a commit"}
			}
			c.Tips = append(c.Tips, value)
		case "finding":
			f, err := parseRecorded(value)
			if err != nil {
				return nil, err
			}
			c.Findings = append(c.Findings, f)
		default:
			return nil, &CheckpointRefusal{"malformed", "a line this format does not know: " + strconv.Quote(line)}
		}
	}
	for _, field := range []string{"salt", "rules", "words", "scanned", "objects"} {
		if !seen[field] {
			return nil, &CheckpointRefusal{"malformed", "no " + field + " line"}
		}
	}
	if len(c.Tips) == 0 {
		return nil, &CheckpointRefusal{"malformed", "no tip line: it records no scan"}
	}
	return c, nil
}

func parseRecorded(value string) (Recorded, error) {
	parts := strings.Split(value, " ")
	if len(parts) != 5 {
		return Recorded{}, &CheckpointRefusal{"malformed", "finding " + strconv.Quote(value)}
	}
	line, err := strconv.Atoi(parts[3])
	if err != nil || line < 0 {
		return Recorded{}, &CheckpointRefusal{"malformed", "finding line " + strconv.Quote(parts[3])}
	}
	return Recorded{Commit: parts[0], Blob: parts[1], Path: parts[2], Line: line, Rule: parts[4]}, nil
}

// Usable reports whether this checkpoint answers the question being asked now.
// It is a different question when the rules changed or the word list changed,
// and a stale answer to a different question is worse than no answer.
func (c *Checkpoint) Usable(rules string, words []string) error {
	if c.Rules != rules {
		return &CheckpointRefusal{"rules-changed", "the rules are not the ones that scan used"}
	}
	if c.Words != WordsDigest(c.Salt, words) {
		return &CheckpointRefusal{"words-changed", "the word list is not the one that scan used"}
	}
	return nil
}

// RulesDigest is over every rule's name and text and every standing
// allowance. Adding a rule, widening one, or allowing a match changes it, and
// every checkpoint taken before that stops being believed — which is how a new
// rule gets run over the history that was already scanned.
func RulesDigest() string {
	h := sha256.New()
	for _, r := range Rules {
		fmt.Fprintf(h, "rule\x00%s\x00%s\x00%s\n", r.Name, r.Catches, r.Passes)
	}
	for _, a := range Allowed {
		fmt.Fprintf(h, "allow\x00%s\x00%s\x00%s\n", a.Path, a.Rule, a.Text)
	}
	for _, name := range sortedKeys(fixtureHomes) {
		fmt.Fprintf(h, "home\x00%s\n", name)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// WordsDigest says whether the list is the same list, without saying what is
// on it. It is salted because the words are short and a plain digest of a
// short word is a word anybody with a dictionary can read back.
func WordsDigest(salt string, words []string) string {
	clean := make([]string, 0, len(words))
	for _, w := range words {
		if w = strings.TrimSpace(strings.ToLower(w)); w != "" && !strings.HasPrefix(w, "#") {
			clean = append(clean, w)
		}
	}
	sort.Strings(clean)
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%d", salt, len(clean))
	for _, w := range clean {
		fmt.Fprintf(h, "\x00%s", w)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func seal(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

func isHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
