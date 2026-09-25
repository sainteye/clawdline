package skills

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// Both guides cut into the same fifteen parts, and the parts put back
// together are the whole guide, byte for byte: printing the guide in parts
// must not lose a sentence that printing it whole carried.
func TestThePartsAreTheWholeGuide(t *testing.T) {
	for _, lang := range Topics() {
		whole, err := Guide(lang)
		if err != nil {
			t.Fatal(err)
		}
		preamble, parts, err := guideParts(lang)
		if err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		joined := append([]byte{}, preamble...)
		for i, p := range parts {
			if len(bytes.TrimSpace(p)) == 0 {
				t.Errorf("%s: part %s is empty", lang, sections[i].Name)
			}
			joined = append(joined, p...)
		}
		if !bytes.Equal(joined, whole) {
			t.Errorf("%s: the parts put together are %d bytes and the guide is %d", lang, len(joined), len(whole))
		}
	}
}

// Each named part opens with the heading it is named for, in both languages:
// the table is matched by order, so a heading added to one guide only would
// shift every name after it.
func TestBothGuidesHaveTheSameSections(t *testing.T) {
	want := map[string]string{"swift": "## 0.", "roles": "## 1.", "connect": "## 2.", "inventory": "## 3.",
		"dispatch": "## 4.", "running": "## 5.", "landing": "## 6.", "report": "## 7.", "send": "## 8.",
		"notify": "## 9.", "board": "## 10.", "coordination": "## 11.", "refused": "## 12.",
		"cloud": "### ", "schedule": "### "}
	for _, lang := range Topics() {
		for _, name := range SectionNames() {
			text, err := Section(lang, name)
			if err != nil {
				t.Fatalf("%s %s: %v", lang, name, err)
			}
			if !strings.HasPrefix(string(text), want[name]) {
				t.Errorf("%s %s opens with %.30q, want %q", lang, name, text, want[name])
			}
		}
	}
}

// The default print is the core — much shorter than the guide — and it names
// every other part with the command that prints it, in the reader's language.
func TestTheCoreNamesEveryOtherPart(t *testing.T) {
	for _, lang := range Topics() {
		core, err := Core(lang)
		if err != nil {
			t.Fatal(err)
		}
		whole, _ := Guide(lang)
		if len(core)*4 > len(whole) {
			t.Errorf("%s: the core is %d of the guide's %d bytes", lang, len(core), len(whole))
		}
		prefix := "clawdline guide "
		if lang != DefaultTopic {
			prefix += lang + " "
		}
		for _, s := range sections {
			part, _ := Section(lang, s.Name)
			carried := bytes.Contains(core, part)
			named := strings.Contains(string(core), "`"+prefix+s.Name+"`")
			if s.Core != carried || s.Core == named {
				t.Errorf("%s %s: core=%v, carried=%v, named=%v", lang, s.Name, s.Core, carried, named)
			}
		}
		if !strings.Contains(string(core), "`"+prefix+"all`") {
			t.Errorf("%s: the core does not say how to print the whole guide", lang)
		}
	}
	if _, err := Section("en", "nope"); !errors.Is(err, ErrUnknownSection) {
		t.Errorf("unknown part = %v", err)
	}
}
