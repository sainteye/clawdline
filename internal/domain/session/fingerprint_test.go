package session

import (
	"encoding/json"
	"testing"
)

// fingerprintVector is the menu both ends hash in their own tests: this one,
// and `web/console/src/session/fingerprint.test.ts`, which asserts the same
// hex. A change to the canonical form has to change both, or an answer from
// the page stops matching every question on every screen.
func fingerprintVector() Menu {
	one := 1
	checked := true
	// The caret, the ticks, the details and the button are here so that the
	// vector proves they are left out.
	return Menu{
		Question: "下午想喝什麼？ Bash command rm -rf build",
		Options: []MenuOption{
			{Number: 1, Label: "Yes", Selected: true, Detail: "a note"},
			{Number: 2, Label: "Yes, and don't ask again", Checked: &checked},
			{Number: 3, Label: "No, and tell Claude what to do differently"},
		},
		Selected: &one,
		Numbered: true,
		Submit:   &MenuSubmit{Label: "Submit"},
		Steps:    []MenuStep{{Label: "飲料", Answered: true, Answer: "Tea"}, {Label: "點心"}},
	}
}

const fingerprintVectorHex = "8eca80fffc9359d5f0fca31f3e36b741bd50218b658fc086f05931747bd4c5ce"

func TestMenuFingerprintIsTheSameHexOnBothEnds(t *testing.T) {
	if got := MenuFingerprint(fingerprintVector()); got != fingerprintVectorHex {
		t.Fatalf("fingerprint %s, want %s", got, fingerprintVectorHex)
	}
}

// What the page saw is the question and its rows. The caret, a tick, a row's
// note and the button move while the same question is up — a multi-select's
// tick is the page's own last press — and must not make a second press read
// as a different question.
func TestMenuFingerprintIgnoresWhatMovesInsideOneQuestion(t *testing.T) {
	base := MenuFingerprint(fingerprintVector())
	moved := fingerprintVector()
	three := 3
	unchecked := false
	moved.Selected = &three
	moved.Options[0].Selected = false
	moved.Options[2].Selected = true
	moved.Options[1].Checked = &unchecked
	moved.Options[0].Detail = "another note"
	moved.Submit = &MenuSubmit{Label: "Submit", Selected: true}
	moved.Steps[1].Answer = "Cake"
	if got := MenuFingerprint(moved); got != base {
		t.Fatalf("a caret or a tick moved and the question changed name: %s != %s", got, base)
	}
}

// Every part that says which question this is changes the name: the prose
// (which carries the command a permission prompt asks about), a row's words, a
// row's number, a row more or less, and a set's progress.
func TestMenuFingerprintNamesTheQuestion(t *testing.T) {
	base := MenuFingerprint(fingerprintVector())
	changes := map[string]func(m *Menu){
		"question":   func(m *Menu) { m.Question = "下午想喝什麼？ Bash command rm -rf dist" },
		"label":      func(m *Menu) { m.Options[1].Label = "Yes, and don't ask again for rm" },
		"number":     func(m *Menu) { m.Options[2].Number = 4 },
		"fewer rows": func(m *Menu) { m.Options = m.Options[:2] },
		"step":       func(m *Menu) { m.Steps[1].Answered = true },
		"step label": func(m *Menu) { m.Steps[0].Label = "點心" },
		"no steps":   func(m *Menu) { m.Steps = nil },
	}
	for name, change := range changes {
		m := fingerprintVector()
		m.Options = append([]MenuOption(nil), m.Options...)
		m.Steps = append([]MenuStep(nil), m.Steps...)
		change(&m)
		if MenuFingerprint(m) == base {
			t.Fatalf("%s changed and the fingerprint did not", name)
		}
	}
	// Separators cannot be forged by the words: a label that spells the
	// separator is not the row after it.
	a := Menu{Options: []MenuOption{{Number: 1, Label: "A\x1e o2\x1fB"}}}
	b := Menu{Options: []MenuOption{{Number: 1, Label: "A"}, {Number: 2, Label: "B"}}}
	if MenuFingerprint(a) == MenuFingerprint(b) {
		t.Fatal("a label spelling a separator named another menu")
	}
}

// The page reads the menu out of JSON, and encoding/json writes a byte that is
// not UTF-8 as U+FFFD. A screen with such a byte must hash as the page hashes
// what it was sent, or that question could never be answered.
func TestMenuFingerprintHashesWhatThePageWasSent(t *testing.T) {
	raw := Menu{Question: "caf\xe9?", Options: []MenuOption{{Number: 1, Label: "ok\xff\xfe"}}}
	wire, err := json.Marshal(map[string]any{"q": raw.Question, "l": raw.Options[0].Label})
	if err != nil {
		t.Fatal(err)
	}
	var sent struct{ Q, L string }
	if err := json.Unmarshal(wire, &sent); err != nil {
		t.Fatal(err)
	}
	seen := Menu{Question: sent.Q, Options: []MenuOption{{Number: 1, Label: sent.L}}}
	if MenuFingerprint(raw) != MenuFingerprint(seen) {
		t.Fatalf("%q and %q hash differently", raw.Question, sent.Q)
	}
}

func TestValidFingerprintIsSixtyFourLowercaseHex(t *testing.T) {
	if !ValidFingerprint(fingerprintVectorHex) {
		t.Fatal("the vector was refused")
	}
	for _, bad := range []string{"", "abc", fingerprintVectorHex + "0", "9D8C" + fingerprintVectorHex[4:], "z" + fingerprintVectorHex[1:]} {
		if ValidFingerprint(bad) {
			t.Fatalf("%q was admitted", bad)
		}
	}
}
