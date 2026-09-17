package session

// Confirmation is what to do with the screen a moment after a digit was typed
// at a picker (Targets.Confirmation).
type Confirmation string

const (
	// ConfirmSend: the same question, with the wanted row highlighted. Return
	// commits it.
	ConfirmSend Confirmation = "send"
	// ConfirmMovedOn: a different question is up. The digit already answered,
	// and a Return now would land on a question nobody has read.
	ConfirmMovedOn Confirmation = "moved_on"
	// ConfirmNotYet: the same question, not there yet. Look again.
	ConfirmNotYet Confirmation = "not_yet"
)

// Confirm decides whether a Return may follow a digit (Targets.confirmation).
//
// A digit no longer confirms on its own: at a row with a preview it only moves
// the highlight, and at an ordinary row of a set it answers and puts the next
// question up. The tab bar is the evidence, because it moves for exactly one of
// the two; compared whole, so a tick, a new question or the review screen all
// read as having moved on. With nothing read before the keystroke, a lone
// question confirms as it always did and a set is refused on its own evidence.
func Confirm(want int, asked *Menu, now Menu) Confirmation {
	if asked == nil {
		if len(now.Steps) != 0 {
			return ConfirmMovedOn
		}
		if now.Selected != nil && *now.Selected == want {
			return ConfirmSend
		}
		return ConfirmNotYet
	}
	if !sameAnswered(asked.Steps, now.Steps) || now.Question != asked.Question {
		return ConfirmMovedOn
	}
	if now.Selected != nil && *now.Selected == want {
		return ConfirmSend
	}
	return ConfirmNotYet
}

func sameAnswered(a, b []MenuStep) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Answered != b[i].Answered {
			return false
		}
	}
	return true
}

// Walk is the keystroke that moves a highlight from one row to another and how
// many of them: `j` down, `k` up. Plain letters rather than `ESC [ B`, so no
// escape sequence ever travels this path (Targets.walk).
func Walk(here, want int) (key byte, times int) {
	if want >= here {
		return 'j', want - here
	}
	return 'k', here - want
}
