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

// Freshness says whether an observation was taken this pass, carried from an
// earlier complete pass, or never obtained. These are facts about the reading,
// not alternate session states: a session can truthfully be "working, as read
// two minutes ago" without claiming it is working now.
type Freshness string

const (
	FreshnessCurrent    Freshness = "current"
	FreshnessUnverified Freshness = "unverified"
	FreshnessMissing    Freshness = "missing"
)

// Observation is where and when one row's state was read.
type Observation struct {
	ObservedAt time.Time
	Provenance string
	Freshness  Freshness
}

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
	case "waiting":
		// A person is being asked. The registry decides *whether* a session is
		// waiting; only the screen says what it is being asked (Menu).
		return StateWaiting
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
	// Binding is how ConversationID was obtained, or — when it is empty —
	// which kind of nothing is in the way. A row with no id is not one fact
	// but three (session.Binding), and a reader deciding whether to wait,
	// fix this machine or stop asking needs to know which.
	Binding Binding `json:"binding,omitempty"`
	// BindingDetail is this machine's own sentence about that answer, in
	// the shape Activity.Detail has: what was counted and what it came to,
	// so a reader is not left to make the measurement again to find out why
	// the row has no name. Empty on a row whose id was found. Not on the
	// wire in this shape; the transport puts it where a person reads it.
	BindingDetail string `json:"-"`

	// Rungs are the parts Label was chosen from, kept so a reader holding a
	// higher rung (a name typed in the Swift app, the task that opened the tab)
	// can choose again without reading the transcript twice. Not on the wire.
	Rungs LabelRungs `json:"-"`
	// CustomTitle is the conversation's current `/rename`, which retires a
	// name typed before it. Not on the wire.
	CustomTitle string `json:"-"`

	// Menu is the question on screen while the session waits, when the screen
	// could be read as one. Nil otherwise — including a waiting session whose
	// dialog is drawn in a shape nothing here recognises.
	Menu *Menu `json:"-"`

	// Shells are the commands this session left running in the background,
	// newest first. Empty for most sessions, and always for Codex, which keeps
	// no record of them.
	Shells []Shell `json:"shells,omitempty"`

	// Activity is when this session last moved, by the one definition in
	// movement.go: the moment its own conversation record last grew. It is
	// what the list is ordered by on every device, in place of a clock each
	// browser kept for itself. Not on the wire in this shape; the transport
	// sends it as the row's `activity`.
	Activity Activity `json:"-"`

	// Observation accompanies the row's terminal state. It is filled by the
	// inventory reading cache: current for a source that answered this pass,
	// unverified for the last complete row retained across a failed pass, and
	// missing when that source has never answered (or its retained answer is
	// too old to keep describing the present).
	Observation Observation `json:"-"`
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
	// Sources is each source's own completeness, keyed by its provenance
	// ("ps", "tmux", "iterm"), on a merged reading. Complete is their AND,
	// which is the right answer to "is this list all there is" and the wrong
	// one to "is this tmux pane gone": that is tmux's question alone, and on a
	// Mac whose iTerm2 cannot be asked the AND is never true
	// (docs/design-decisions.md D05 ③). Not on the wire.
	Sources map[string]bool `json:"-"`
	// Gaps are the regions a source could not see into, each named and each
	// either sealed by another source or still open (gap.go). A source that is
	// incomplete and names no gap is incomplete about itself as a whole.
	// Incompleteness with a subject is what lets a reading go on answering for
	// the parts nothing was hiding. Not on the wire in this shape; the
	// transport puts it beside the source it belongs to.
	Gaps []Gap `json:"-"`
	// Observation says what the whole displayed reading is worth. Rows carry
	// their own because a merged reading may contain current tmux rows beside
	// retained iTerm2 rows.
	Observation Observation `json:"-"`
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
