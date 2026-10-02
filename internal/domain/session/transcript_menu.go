package session

// MenuSource is where a menu's rows came from.
type MenuSource string

const (
	// MenuSourceTranscript is a menu put together from the session's own
	// transcript because no screen could be read: an inference about what is
	// drawn, not a reading of it.
	MenuSourceTranscript MenuSource = "transcript"
)

// ScreenReading is whether a reading had a screen to look at.
type ScreenReading string

const (
	// ScreenRead is a screen captured this reading.
	ScreenRead ScreenReading = "read"
	// ScreenUnavailable is a terminal that gave no screen up this reading.
	ScreenUnavailable ScreenReading = "unavailable"
)

// The rows Claude Code adds after an AskUserQuestion call's own options, as
// v2.1.274–v2.1.287 draw them: a free answer, then (under a rule) a way to
// talk about the question instead. Captured in menu-ask-live.txt and
// menu-ask-clipped.txt (testdata), and on 2026-10-02 as 5 and 6 after four.
const (
	transcriptTypeRow = "Type something."
	transcriptChatRow = "Chat about this"
)

// MenuFromTranscript is the menu an open AskUserQuestion call draws, when it
// is drawn somewhere this daemon cannot see.
//
// It answers only for the shape whose numbers a capture has shown: one
// question, one answer, two to four options — the call's options are 1…n and
// Claude Code's own two rows follow. Several questions are not: which one is
// up moves as they are answered on the machine, and the transcript does not
// record that until all are. A multi-select is not either: its rows tick and
// its button is reached by walking the highlight, which needs a screen to
// read back. Nothing is made rather than a guess.
//
// The caret is not claimed (Selected stays nil), because where it is was
// drawn and not written down; and the menu says it came from here
// (MenuSourceTranscript). What a press does is unchanged: Actions.Key reads
// the screen before it types and refuses a question it cannot see.
func MenuFromTranscript(asked []AskedQuestion) (Menu, bool) {
	if len(asked) != 1 || asked[0].Multi {
		return Menu{}, false
	}
	question := asked[0]
	if len(question.Options) < 2 || len(question.Options) > 4 {
		return Menu{}, false
	}
	out := Menu{Question: question.Text, Numbered: true, Source: MenuSourceTranscript}
	for i, option := range question.Options {
		if option.Label == "" {
			return Menu{}, false
		}
		out.Options = append(out.Options, MenuOption{Number: i + 1, Label: option.Label, Detail: option.Note})
	}
	n := len(question.Options)
	out.Options = append(out.Options,
		MenuOption{Number: n + 1, Label: transcriptTypeRow},
		MenuOption{Number: n + 2, Label: transcriptChatRow})
	return out, true
}
