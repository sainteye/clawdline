package ports

import "context"

// Asked never grants integration consent. A disabled iTerm2 integration
// refuses even a requested action.
//
// A terminal backend whose polling can be switched off (iTerm2's `iterm_scan`,
// docs/interface.md "Turning iTerm2 scanning off") still has to do what was
// asked of one session.
//
// The switch exists to stop the standing cost of the session list: a listing
// and a screen sample every few seconds, whether or not anybody is looking. A
// send is not that. It is one action, taken because a person or the broker
// named a session and wanted a line typed into it. Refusing it with the switch
// off broke replying to every assistant in a hand-opened iTerm2 tab, from the
// phone and from a Note alike, and the person's instruction (2026-10-15) was
// that the switch may cost nothing day to day and must break nothing when it
// is needed.
//
// So an action marks its context, and a backend reads the mark. The session
// list's readers never carry it; every call that types, presses or reads on
// behalf of one action does.

type askedKey struct{}

// Asked marks ctx as carrying one action somebody asked for.
func Asked(ctx context.Context) context.Context {
	return context.WithValue(ctx, askedKey{}, true)
}

// WasAsked reports whether ctx carries an action somebody asked for.
func WasAsked(ctx context.Context) bool {
	asked, _ := ctx.Value(askedKey{}).(bool)
	return asked
}

// TTYHolder is a terminal backend that can say, without asking the terminal
// application anything, whether one pseudo-terminal is a session it holds.
//
// A row only the process table saw is named by its tty. When a backend holds
// that tty — an iTerm2 tab, with iTerm2's listing switched off — an action on
// the row is that backend's to carry out, and the backend finds the session by
// the tty when it is asked to act.
type TTYHolder interface {
	HoldsTTY(ctx context.Context, tty string) bool
}
