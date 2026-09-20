package work

import (
	"strings"
	"testing"
	"time"
)

// The rules a leftover is admitted by, and the two properties everything
// downstream rests on: a title names exactly one leftover of one result, and
// nothing a child wrote can put a newline inside the completion notice.
func TestParseLeftovers(t *testing.T) {
	long := strings.Repeat("x", LeftoverTitleLimit+1)
	for _, c := range []struct {
		why  string
		in   []Leftover
		code string
	}{
		{"none at all", nil, ""},
		{"an empty list", []Leftover{}, ""},
		{"a title only", []Leftover{{Title: "the flaky test"}}, ""},
		{"the whole shape", []Leftover{{Title: "a", Why: "b", Acceptance: "c"}}, ""},
		{"more than the bound", make([]Leftover, LeftoversLimit+1), "too_many_leftovers"},
		{"an empty title", []Leftover{{Title: "  "}}, "invalid_leftover"},
		{"a title past its bound", []Leftover{{Title: long}}, "invalid_leftover"},
		{"a why past its bound", []Leftover{{Title: "a", Why: strings.Repeat("y", LeftoverWhyLimit+1)}}, "invalid_leftover"},
		{"two of one title", []Leftover{{Title: "a"}, {Title: " a "}}, "invalid_leftover"},
	} {
		out, err := ParseLeftovers(c.in)
		var got string
		if r, ok := err.(*Refusal); ok {
			got = r.Code
		}
		switch {
		case got != c.code:
			t.Errorf("%s: %v", c.why, err)
		case c.code == "" && len(out) != len(c.in):
			t.Errorf("%s: %d of %d rows survived", c.why, len(out), len(c.in))
		}
	}

	// Every field is one line, whatever the child wrote: these strings are
	// encoded into the completion notice, which must be one physical line.
	out, err := ParseLeftovers([]Leftover{{Title: " the\tflaky \n test ", Why: "a\nb"}})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Title != "the flaky test" || out[0].Why != "a b" {
		t.Fatalf("%q / %q", out[0].Title, out[0].Why)
	}
	if _, ok := FindLeftover(out, "the flaky test"); !ok {
		t.Fatal("a leftover is not found by the title it was kept under")
	}
}

// A delivery that named three leftovers becomes three subjects. Without this
// the second would be refused as a duplicate of the first, because they share
// one task id and PriorProposals reads by that id.
func TestPriorLeftoversNarrowsToOneSubject(t *testing.T) {
	at := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	all := []Proposal{
		{ID: "1", TaskID: "t", Title: "first", Signals: []Signal{SignalLeftover}, CreatedAt: at},
		{ID: "2", TaskID: "t", Title: "second", Signals: []Signal{SignalLeftover}, CreatedAt: at},
		{ID: "3", TaskID: "t", Title: "first", Signals: []Signal{SignalCrossSession}, CreatedAt: at},
	}
	got := PriorLeftovers(all, " first ")
	if len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("%+v", got)
	}
	if !all[0].Leftover() || all[2].Leftover() {
		t.Fatal("a proposal's own signals say whether it is a leftover")
	}
}
