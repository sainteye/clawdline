package session

import "time"

// Shell is one command a session started in the background and has not
// finished.
//
// It is the reason an idle row is not a finished one. The terminal says "1
// shell still running" once, where the turn ended, and that line scrolls away;
// after it the screen reads exactly like a session that is done, while a build
// it started is still going.
type Shell struct {
	// ID is Claude Code's own id for it, which is also its output file's name.
	ID string `json:"id"`
	// At is when it last printed something — a live clock for a command that
	// prints as it goes, and its start for one that does not.
	At time.Time `json:"at"`
	// Command is the command line that started it. Empty when the two
	// transcript records it is joined from straddled a read.
	Command string `json:"command,omitempty"`
	// What is the description written beside the command, when there was one.
	What string `json:"what,omitempty"`
	// Doing is the last line it printed.
	Doing string `json:"doing,omitempty"`
}
