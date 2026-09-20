package session

import (
	"strconv"
	"strings"
)

// Binding is how a running process was tied to the conversation it is having —
// or, when it was not, which kind of nothing stands in the way.
//
// One word for every kind of nameless row is what made a fresh Codex look
// absent. A session that has written nothing yet, a session whose open files
// this machine was not allowed to read, and a session whose open transcripts
// belong to two different conversations are three different facts: the first
// resolves itself the moment somebody types, the second is this machine's
// problem, and the third is the one case where naming the session at all would
// be a guess. Only the first is worth waiting for, and a reader cannot wait for
// the right one unless the answer says which it is (DG-7).
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
	// BindingAmbiguous: the open transcripts belong to more than one
	// conversation and nothing here ranks them. Calling one session by
	// another's name costs more than leaving it unnamed, so this never
	// resolves to an id — and it says what it counted (OpenTranscripts).
	//
	// It is the count of conversations and not the count of files: a Codex
	// session running sub-agents holds one transcript per thread open at
	// once, and on this Mac that is the ordinary case rather than the
	// exception. All of those threads are one conversation and say so, in
	// the `session_id` at the head of every one of their rollouts.
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

// OpenTranscripts is what one process's open transcripts came to: how many
// were open, how many conversations they turned out to belong to, how many of
// them were sub-threads folded into the thread that started them, and how many
// heads could not be read at all.
//
// It exists so that BindingAmbiguous can say why. A reader told only that the
// answer was ambiguous has to repeat the whole measurement — read the table of
// open files, then the head of every rollout in it — before they can tell one
// session running sub-agents from two sessions genuinely sharing a process.
// These four numbers are that measurement, and the second is the one that
// decides: one conversation binds, two do not.
type OpenTranscripts struct {
	Open          int
	Conversations int
	SubThreads    int
	Unread        int
}

// Detail is this machine's own sentence about an ambiguous binding, in the
// shape Activity.Detail has: what was counted, never where the files were.
func (o OpenTranscripts) Detail() string {
	var b strings.Builder
	b.WriteString(count(o.Open, "transcript") + " open on this process, belonging to " + count(o.Conversations, "conversation"))
	var aside []string
	if o.SubThreads > 0 {
		started := "the thread that started them"
		if o.SubThreads == 1 {
			started = "the thread that started it"
		}
		aside = append(aside, count(o.SubThreads, "sub-thread")+" folded into "+started)
	}
	if o.Unread > 0 {
		aside = append(aside, count(o.Unread, "head")+" this machine could not read")
	}
	if len(aside) > 0 {
		b.WriteString(" (" + strings.Join(aside, ", ") + ")")
	}
	b.WriteString("; nothing here ranks them, and naming this session after one of them would be a guess")
	return b.String()
}

// count is "1 transcript" or "3 transcripts". Every noun this says is regular.
func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

const (
	// NoTranscriptDetail is what BindingNoRecord means for Codex: the thread
	// exists and has written nothing, which somebody typing resolves.
	NoTranscriptDetail = "no transcript is open yet: Codex writes a thread's rollout at its first message, not at startup"
	// UnreadableTranscriptsDetail is BindingUnreadable: this machine could not
	// read the table of open files, which is its own fault to fix and says
	// nothing at all about the session.
	UnreadableTranscriptsDetail = "the table of open files could not be read, so this machine cannot say which conversation the process is having"
)
