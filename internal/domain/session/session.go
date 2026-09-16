// Package session is the domain model for what this machine is running. It is
// pure: nothing here shells out, reads a file or knows which platform it is on.
package session

import "time"

type Backend string

const (
	BackendITerm Backend = "iterm"
	BackendTmux  Backend = "tmux"
	BackendOwned Backend = "owned" // a pty this daemon opened itself
)

type Assistant string

const (
	AssistantClaude Assistant = "claude"
	AssistantCodex  Assistant = "codex"
)

// State is what the session appears to be doing. `unknown` is a real answer and
// is deliberately not flattened into `idle`: a terminal that did not answer has
// told us nothing, which is different from a terminal that answered "nothing".
type State string

const (
	StateWorking State = "working"
	StateWaiting State = "waiting"
	StateIdle    State = "idle"
	StateUnknown State = "unknown"
)

// Evidence says HOW a reading was obtained. The Swift app carries the reading
// without the provenance, so a screen-scraped guess and a structured event are
// indistinguishable on the wire. Every field derived from observation travels
// with one of these.
type Evidence string

const (
	// EvidenceStructured came from a provider's own protocol (app-server, SDK).
	EvidenceStructured Evidence = "structured"
	// EvidenceTranscript was read out of the assistant's own record on disk.
	EvidenceTranscript Evidence = "transcript"
	// EvidenceProcess came from the process table: it proves something is
	// running, and nothing about what it is doing.
	EvidenceProcess Evidence = "process"
	// EvidenceScreen was inferred from what is drawn in the terminal.
	EvidenceScreen Evidence = "screen"
	// EvidenceRegistry came from the assistant's own live status file, which is
	// the assistant speaking about itself rather than anyone observing it.
	EvidenceRegistry Evidence = "registry"
	// EvidenceNone means nothing supported this field.
	EvidenceNone Evidence = "none"
)

// StateFromAssistantStatus maps an assistant's own word for what it is doing.
// An unrecognised word is `unknown`, never `idle`: a status we cannot read has
// told us nothing, and flattening it into "nothing is happening" is the one
// wrong answer a fleet list must not give.
func StateFromAssistantStatus(status string) State {
	switch status {
	case "busy":
		return StateWorking
	case "idle":
		return StateIdle
	default:
		return StateUnknown
	}
}

type Session struct {
	ID        string    `json:"id"`
	Backend   Backend   `json:"backend"`
	TTY       string    `json:"tty,omitempty"`
	PID       int       `json:"pid,omitempty"`
	Assistant Assistant `json:"assistant,omitempty"`
	CWD       string    `json:"cwd,omitempty"`
	Label     string    `json:"label,omitempty"`
	// Line is what a working assistant says it is doing — the spinner line,
	// with its own clock in it. Empty unless the session is working.
	Line     string   `json:"line,omitempty"`
	State    State    `json:"state"`
	Evidence Evidence `json:"evidence"`

	// ConversationID is the assistant's own id for this line of work. It
	// survives a terminal restart, which a tty and a pane id do not.
	ConversationID string `json:"conversation_id,omitempty"`

	// Shells are the commands this session left running in the background,
	// newest first. Empty for most sessions, and always for Codex, which keeps
	// no record of them.
	Shells []Shell `json:"shells,omitempty"`
}

// IsAssistant separates a Claude or Codex session from an ordinary shell.
func (s Session) IsAssistant() bool { return s.Assistant != "" }

// Inventory is one reading of the machine. Incompleteness is carried in the
// value rather than thrown, because a partial reading is still worth
// publishing — it is only never proof of absence.
type Inventory struct {
	Sessions   []Session `json:"sessions"`
	Complete   bool      `json:"complete"`
	Provenance string    `json:"provenance"`
	ObservedAt time.Time `json:"observed_at"`
	// Notes records why a reading is incomplete, in the reading itself, so a
	// reader never has to guess whether empty means empty.
	Notes []string `json:"notes,omitempty"`
}

func (i Inventory) Assistants() []Session {
	out := make([]Session, 0, len(i.Sessions))
	for _, s := range i.Sessions {
		if s.IsAssistant() {
			out = append(out, s)
		}
	}
	return out
}
