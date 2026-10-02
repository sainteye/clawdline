package session

import "testing"

// The call behind menu-ask-live.txt, as its transcript records it.
var liveAsked = []AskedQuestion{{
	Text: "What would you like this afternoon?",
	Options: []AskedOption{
		{Label: "Tea", Note: "A warm, soothing beverage with various flavor options."},
		{Label: "Water", Note: "Pure hydration, refreshing and essential."},
		{Label: "Coffee", Note: "A bold, energizing drink to boost your afternoon."},
	},
}}

// A menu made from the transcript numbers its rows as Claude Code draws them —
// checked against two captured pickers, not against a rule written down — and
// names the question the way the screen's own reading names it once the
// transcript refills it, so an answer chosen from it is accepted by `expect`
// when the screen comes back.
func TestTranscriptMenuNumbersRowsAsTheScreenDoes(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		asked   []AskedQuestion
	}{
		{"menu-ask-live.txt", liveAsked},
		{"menu-ask-clipped.txt", []AskedQuestion{{
			Text:    "Which should be ordered for the meeting room?",
			Options: []AskedOption{{Label: "Tea and biscuits (Recommended)"}, {Label: "Coffee only"}, {Label: "Fruit"}},
		}}},
	} {
		screen, ok := ReadMenu(fixture(t, tc.fixture), AssistantClaude, true)
		if !ok {
			t.Fatalf("%s: no menu on the captured screen", tc.fixture)
		}
		got, ok := MenuFromTranscript(tc.asked)
		if !ok {
			t.Fatalf("%s: no menu from the transcript", tc.fixture)
		}
		if len(got.Options) != len(screen.Options) {
			t.Fatalf("%s: %d rows, the screen draws %d", tc.fixture, len(got.Options), len(screen.Options))
		}
		for i := range got.Options {
			g, s := got.Options[i], screen.Options[i]
			if g.Number != s.Number || g.Label != RefillMenu(screen, tc.asked).Options[i].Label {
				t.Fatalf("%s: row %d is %d %q, the screen draws %d %q", tc.fixture, i, g.Number, g.Label, s.Number, s.Label)
			}
		}
		if got.Source != MenuSourceTranscript || got.Selected != nil || !got.Numbered || got.Submit != nil || len(got.Steps) != 0 {
			t.Fatalf("%s: menu %+v", tc.fixture, got)
		}
		if MenuFingerprint(got) != MenuFingerprint(RefillMenu(screen, tc.asked)) {
			t.Fatalf("%s: the transcript's menu and the refilled screen name different questions", tc.fixture)
		}
	}
}

// A shape whose numbering the transcript cannot prove gets no menu: several
// questions (which one is up is only the screen's to say), a multi-select
// (its rows tick and its button is reached by walking), and an option count
// outside what the tool accepts.
func TestTranscriptMenuRefusesShapesItCannotProve(t *testing.T) {
	two := []AskedQuestion{liveAsked[0], {Text: "要配哪些點心？", Options: []AskedOption{{Label: "Cookie"}, {Label: "Cake"}}}}
	multi := []AskedQuestion{{Text: "要配哪些點心？", Multi: true, Options: []AskedOption{{Label: "Cookie"}, {Label: "Cake"}}}}
	one := []AskedQuestion{{Text: "q", Options: []AskedOption{{Label: "only"}}}}
	five := []AskedQuestion{{Text: "q", Options: []AskedOption{{Label: "a"}, {Label: "b"}, {Label: "c"}, {Label: "d"}, {Label: "e"}}}}
	for name, asked := range map[string][]AskedQuestion{"two questions": two, "multi-select": multi, "one option": one, "five options": five, "nothing": nil} {
		if m, ok := MenuFromTranscript(asked); ok {
			t.Fatalf("%s: made %+v", name, m)
		}
	}
	// The tool's own upper bound is still a menu: Claude Code adds its two
	// rows after the fourth (5 and 6), which a digit still reaches.
	four := []AskedQuestion{{Text: "q", Options: []AskedOption{{Label: "a"}, {Label: "b"}, {Label: "c"}, {Label: "d"}}}}
	m, ok := MenuFromTranscript(four)
	if !ok || len(m.Options) != 6 || m.Options[4].Number != 5 || m.Options[4].Label != "Type something." ||
		m.Options[5].Number != 6 || m.Options[5].Label != "Chat about this" {
		t.Fatalf("four options: %v %+v", ok, m.Options)
	}
}
