package session

import "testing"

// Each id shape is issued by exactly one source, which is what lets a reading
// say whose question an absent id is.
func TestAnIDSaysWhichSourceWouldHaveListedIt(t *testing.T) {
	for id, want := range map[string]string{
		"%8":                                   "tmux",
		"%0":                                   "tmux",
		"7A5C0000-0000-4000-8000-000000000009": "iterm",
		"ttys015":                              "ps",
		"pts/3":                                "ps",
		"":                                     "",
		"%":                                    "",
		"%abc":                                 "",
		"7A5C0000-0000-4000-8000":              "",
		"a task id nobody issued":              "",
	} {
		if got := SourceForID(id); got != want {
			t.Errorf("%q: %q, want %q", id, got, want)
		}
	}
}

// ProvesAbsence is D05 ③ in one place: the source that owns the session
// answers, and the AND over every source does not.
func TestOnlyTheSourceThatOwnsASessionSaysItIsGone(t *testing.T) {
	inv := Inventory{
		Complete: false,
		Sources:  map[string]bool{"ps": true, "tmux": true, "iterm": false},
		Gaps: []Gap{{Source: "iterm", Scope: "window", ID: "27898",
			Detail: "iTerm2 window 27898 would not list its tabs"}},
	}
	if proves, why := inv.ProvesAbsence("tmux"); !proves {
		t.Errorf("tmux read everything and still cannot say a pane is gone: %s", why)
	}
	proves, why := inv.ProvesAbsence("iterm")
	if proves {
		t.Error("iTerm2 could not read one of its windows and says a session is gone anyway")
	}
	if why != "iTerm2 window 27898 would not list its tabs" {
		t.Errorf("the refusal does not name the window: %q", why)
	}

	// A source nobody in this reading lists is the whole reading's question:
	// this one is incomplete, so it answers nothing.
	if proves, why := inv.ProvesAbsence("kitty"); proves || why == "" {
		t.Errorf("a source that was never asked, in an incomplete reading: %v %q", proves, why)
	}
	// And on a reading that did take in this whole machine, a kind of session
	// nothing on it runs is absent — which is what a machine with no tmux says
	// about a pane id.
	whole := Inventory{Complete: true, Sources: map[string]bool{"ps": true}}
	if proves, why := whole.ProvesAbsence("tmux"); !proves {
		t.Errorf("a complete reading of a machine with no tmux on it: %v %q", proves, why)
	}

	// No source at all falls back to the whole reading, which is where every
	// caller was before the sources could be told apart.
	if proves, _ := inv.ProvesAbsence(""); proves {
		t.Error("an incomplete reading proved an absence for a backend nobody owns")
	}
	inv.Complete = true
	if proves, _ := inv.ProvesAbsence(""); !proves {
		t.Error("a complete reading proved nothing")
	}
}

// A sealed gap does not stop the reading from answering, and it does not stop
// being reported: the window is still on the person's screen.
func TestASealedGapIsStillReportedAndNoLongerRefuses(t *testing.T) {
	inv := Inventory{
		Complete: true,
		Sources:  map[string]bool{"iterm": true},
		Gaps: []Gap{{Source: "iterm", Scope: "window", ID: "27898", Sealed: true,
			SealedBy: "the process table attributes 11 pseudo-terminal(s) to iTerm2 and this listing read exactly those",
			Detail:   "iTerm2 window 27898 would not list its tabs"}},
	}
	if proves, why := inv.ProvesAbsence("iterm"); !proves {
		t.Errorf("a sealed gap still refuses: %s", why)
	}
	if len(inv.OpenGaps("")) != 0 {
		t.Errorf("a sealed gap is still open: %+v", inv.OpenGaps(""))
	}
	if len(inv.Gaps) != 1 {
		t.Error("a sealed gap stopped being reported")
	}
}

// A source that is incomplete and names no region is incomplete about itself
// as a whole, and Because falls back to what it did say.
func TestAReadingThatNamedNothingStillSaysWhy(t *testing.T) {
	inv := Inventory{Complete: false, Sources: map[string]bool{"tmux": false},
		Notes: []string{"tmux failed: no server running"}}
	if proves, why := inv.ProvesAbsence("tmux"); proves || why != "tmux did not finish its reading" {
		t.Errorf("%v %q", proves, why)
	}
	if got := inv.Because(); got != "tmux failed: no server running" {
		t.Errorf("Because: %q", got)
	}
	if got := (Inventory{}).Because(); got != "this reading did not finish" {
		t.Errorf("a reading with nothing in it: %q", got)
	}
}
