package skills

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

// A guide read in parts.
//
// The whole guide is 15,708 tokens in English and 18,619 in Traditional
// Chinese (measured 2026-09-25 by appending each to a system prompt and
// comparing with and without). A root that read it whole carried it on every
// later call; in a week's transcripts twenty roots read it 136 times, most of
// them a section at a time with sed and grep because that is all they needed.
// The board section alone is 30% of it.
//
// So `clawdline guide` prints the core every session needs — who it is, how to
// reach the daemon, reporting its turn, telling the person, what a refusal
// means — and a contents list, and each other section is printed by name when
// it is needed. `clawdline guide all` is the whole text, unchanged.

// section is one printable part: a name, and the heading that opens it in
// each guide. Headings are matched by their order in the file: both guides
// carry the same fifteen, and TestBothGuidesHaveTheSameSections holds them to
// it.
type section struct {
	Name    string
	Summary string
	Core    bool
}

// sections are in the guides' own order: every "## " and "### " heading, one
// entry each.
var sections = []section{
	{"swift", "if you learned Clawdline from the retired Swift app", false},
	{"roles", "root or child", true},
	{"connect", "reaching the daemon, and the commands", true},
	{"cloud", "pairing a Cloud browser", false},
	{"inventory", "before you dispatch: read what is already there", false},
	{"dispatch", "dispatching an owned child", false},
	{"running", "while a child runs, and when it finishes", false},
	{"landing", "landing, handoff, detached work and root assignments", false},
	{"schedule", "scheduling future work", false},
	{"report", "reporting your own finished turn", true},
	{"send", "talking to another session", false},
	{"notify", "telling the person", true},
	{"board", "the board: items, steps, proposals, decisions, to-dos", false},
	{"coordination", "coordination between sessions", false},
	{"refused", "what to do when something is refused", true},
}

// ErrUnknownSection is a section name this build does not carry.
var ErrUnknownSection = errors.New("no such section")

// SectionNames lists every section, in the guide's order.
func SectionNames() []string {
	out := make([]string, len(sections))
	for i, s := range sections {
		out[i] = s.Name
	}
	return out
}

// split cuts a guide at every "## " and "### " heading: the preamble before
// the first, then one part per heading.
func split(text []byte) (preamble []byte, parts [][]byte) {
	lines := bytes.SplitAfter(text, []byte("\n"))
	var cur []byte
	started := false
	for _, line := range lines {
		if bytes.HasPrefix(line, []byte("## ")) || bytes.HasPrefix(line, []byte("### ")) {
			if started {
				parts = append(parts, cur)
			} else {
				preamble = cur
				started = true
			}
			cur = nil
		}
		cur = append(cur, line...)
	}
	if started {
		parts = append(parts, cur)
	} else {
		preamble = cur
	}
	return preamble, parts
}

func guideParts(lang string) ([]byte, [][]byte, error) {
	text, err := Guide(lang)
	if err != nil {
		return nil, nil, err
	}
	preamble, parts := split(text)
	if len(parts) != len(sections) {
		return nil, nil, fmt.Errorf("the %s guide has %d sections and this build names %d", lang, len(parts), len(sections))
	}
	return preamble, parts, nil
}

// Section is one named part of the guide in lang ("" is DefaultTopic).
func Section(lang, name string) ([]byte, error) {
	_, parts, err := guideParts(lang)
	if err != nil {
		return nil, err
	}
	for i, s := range sections {
		if s.Name == name {
			return parts[i], nil
		}
	}
	return nil, fmt.Errorf("%w named %q; this build carries %s", ErrUnknownSection, name, strings.Join(SectionNames(), ", "))
}

// Core is what `clawdline guide` prints: the preamble, the sections every
// session needs, and a list of the others with the command that prints each.
func Core(lang string) ([]byte, error) {
	preamble, parts, err := guideParts(lang)
	if err != nil {
		return nil, err
	}
	prefix := "clawdline guide "
	if lang != "" && lang != DefaultTopic {
		prefix += lang + " "
	}
	var b bytes.Buffer
	b.Write(preamble)
	for i, s := range sections {
		if s.Core {
			b.Write(parts[i])
		}
	}
	b.WriteString("## The rest of this guide, one part at a time\n\n")
	b.WriteString("Print a part when the work in front of you needs it, not before:\n\n")
	for _, s := range sections {
		if !s.Core {
			fmt.Fprintf(&b, "- `%s%s` — %s\n", prefix, s.Name, s.Summary)
		}
	}
	fmt.Fprintf(&b, "\n`%sall` prints the whole guide.\n", prefix)
	return b.Bytes(), nil
}
