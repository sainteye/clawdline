package session

import "strings"

// Binding is how a running process was tied to the conversation it is having —
// or, when it was not, which kind of nothing stands in the way.
//
// One word for every kind of nameless row is what made a fresh Codex look
// absent. A session that has written nothing yet, a session whose open files
// this machine was not allowed to read, and a session holding two transcripts
// open at once are three different facts: the first resolves itself the moment
// somebody types, the second is this machine's problem, and the third is the
// one case where naming the session at all would be a guess. Only the first is
// worth waiting for, and a reader cannot wait for the right one unless the
// answer says which it is (DG-7).
type Binding string

const (
	// BindingCommandLine: the conversation id is in the process's own argv,
	// which is how a resumed session of either assistant carries it.
	BindingCommandLine Binding = "command_line"
	// BindingOpenFile: the process holds exactly one of its own transcripts
	// open. The kernel says which file that is, so it is proof rather than a
	// correlation between two clocks.
	BindingOpenFile Binding = "open_file"
	// BindingRegistry: the assistant's own live record names it. Claude Code
	// writes one per pid; Codex writes none.
	BindingRegistry Binding = "registry"
	// BindingNoRecord: nothing this pid can be matched against exists yet. A
	// Codex session writes its rollout at the first message, not at startup,
	// so a session nobody has typed into is this and stays this.
	BindingNoRecord Binding = "no_record"
	// BindingUnreadable: there may well be a record; the table of open files
	// could not be read, so this machine does not know.
	BindingUnreadable Binding = "unreadable"
	// BindingAmbiguous: more than one transcript is open and nothing here
	// ranks them. Calling one session by another's name costs more than
	// leaving it unnamed, so this never resolves to an id.
	BindingAmbiguous Binding = "ambiguous"
)

// Bound says whether the binding produced a conversation id. The three that do
// not are deliberately not one value: see Binding.
func (b Binding) Bound() bool {
	switch b {
	case BindingCommandLine, BindingOpenFile, BindingRegistry:
		return true
	}
	return false
}

// Coordinate is where a session is, as a name of last resort: the assistant it
// is running and the terminal it is running in.
//
// It is the bottom rung and never competes with a real name — LabelRungs takes
// it only when every rung above it is empty. What it replaces is a row with no
// label at all, which reads as a session that is not there.
func Coordinate(s Session) string {
	assistant := strings.TrimSpace(string(s.Assistant))
	if assistant == "" {
		return ""
	}
	where := s.TTY
	if where == "" {
		where = s.ID
	}
	name := strings.ToUpper(assistant[:1]) + assistant[1:]
	if where == "" {
		return name
	}
	return name + " · " + where
}
