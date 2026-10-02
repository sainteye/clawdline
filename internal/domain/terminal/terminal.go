// Package terminal is the rules for an ordinary shell this daemon opens and a
// person types into: its identity, what one look at its screen carries, the
// refusals every layer above it answers with, and how a numbered input is
// decided against the ones already typed.
//
// It imports no adapter, no transport and no store. The tmux server that
// holds the shells is internal/adapters/terminal/owned; the leases, the
// grants and the routes that decide who may type are the next layer up
// (plan v3 §5).
package terminal

import (
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"time"
)

// ID is `trm_<uuid>`. It is the whole identity of a terminal: the tmux session
// that holds it carries it as a user option, and there is no table beside it.
type ID string

var idPattern = regexp.MustCompile(`^trm_[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// NewID is a fresh random terminal id.
func NewID() ID {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail on any platform this builds for; a
		// terminal with a guessable id would be worse than no terminal.
		panic("terminal: no randomness: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return ID(fmt.Sprintf("trm_%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]))
}

// Valid is whether id has the shape NewID makes. Anything else is refused
// before it reaches a tmux target, where it would be a pattern.
func (id ID) Valid() bool { return idPattern.MatchString(string(id)) }

// SessionName is the tmux session that holds this terminal: `clt-` and the
// first twelve hex digits of the uuid. Twelve are enough to be unique among
// the terminals one machine may hold at once, and short enough to read in a
// `tmux ls` somebody runs against this server by hand.
func (id ID) SessionName() string {
	if !id.Valid() {
		return ""
	}
	return "clt-" + string(id)[4:12] + string(id)[13:17]
}

// Status is where a terminal is in its life.
type Status string

const (
	// Running: the shell is there.
	Running Status = "running"
	// Exited: the shell ended and the pane is still held. With the server's
	// `remain-on-exit off` a pane goes when its shell does, so this is only
	// seen in the moment between the two.
	Exited Status = "exited"
	// Closed: there is no such terminal any more — closed by somebody, or
	// its shell ended and took it away.
	Closed Status = "closed"
	// Unreachable: whether it is there could not be read. Never a reason to
	// call it closed.
	Unreachable Status = "unreachable"
)

// Terminal is one terminal as its server lists it.
type Terminal struct {
	ID        ID
	ProjectID string
	Created   time.Time
	Status    Status
	Cols      int
	Rows      int
	// Dir is the directory the shell is in now, which starts as the
	// project's and follows every `cd`.
	Dir string
}

// MouseMode is which mouse reporting the program in the terminal asked for.
type MouseMode string

const (
	MouseNone     MouseMode = "none"
	MouseStandard MouseMode = "standard" // DECSET 1000
	MouseButton   MouseMode = "button"   // DECSET 1002
	MouseAny      MouseMode = "any"      // DECSET 1003
)

// CursorShape is the DECSCUSR shape, as tmux names it. Empty is a tmux that
// does not say (the formats arrived after 3.0), which is drawn as the default.
type CursorShape string

const (
	CursorDefault   CursorShape = "default"
	CursorBlock     CursorShape = "block"
	CursorUnderline CursorShape = "underline"
	CursorBar       CursorShape = "bar"
)

// Cursor is where the cursor is and how it is drawn.
type Cursor struct {
	X, Y     int
	Visible  bool
	Shape    CursorShape
	Blinking bool
}

// Modes are the terminal modes a program set that change what a key sends or
// what the screen is: the viewer's terminal must be put in the same modes
// before a key pressed in it means what the program expects (plan v3 D2).
type Modes struct {
	// AppCursor is DECCKM: the arrow keys send `ESC O A` rather than `ESC [ A`.
	AppCursor bool
	// AppKeypad is DECKPAM.
	AppKeypad bool
	Mouse     MouseMode
	// MouseSGR is DECSET 1006.
	MouseSGR bool
	// Alt is the alternate screen.
	Alt bool
}

// Frame is one whole look at a terminal's visible screen. A newer frame
// replaces an older one; nothing is ever applied on top of another.
type Frame struct {
	// Rev is a digest of everything below but At: two frames with the same
	// Rev draw the same screen.
	Rev string
	// At is when the screen was read.
	At     time.Time
	Cols   int
	Rows   int
	Cursor Cursor
	Modes  Modes
	// Lines is the visible screen, one entry per screen row, with the SGR
	// escapes kept. A line longer than the terminal is as many rows as it
	// wrapped onto, never one joined line.
	Lines []string
	// Dead is a pane whose shell has ended.
	Dead bool
}

// RefusalCode is a typed refusal every layer above the backend passes on
// unchanged (plan v3 D4). The strings are the wire.
type RefusalCode string

const (
	CodeForbidden           RefusalCode = "terminal_forbidden"
	CodeNotController       RefusalCode = "not_controller"
	CodeControlled          RefusalCode = "terminal_controlled"
	CodeLeaseSuperseded     RefusalCode = "lease_superseded"
	CodeLeaseExpired        RefusalCode = "lease_expired"
	CodeInputGap            RefusalCode = "input_gap"
	CodeInputStateUnknown   RefusalCode = "input_state_unknown"
	CodeInputTooLarge       RefusalCode = "input_too_large"
	CodeBusy                RefusalCode = "terminal_busy"
	CodeClosed              RefusalCode = "terminal_closed"
	CodeUnreachable         RefusalCode = "terminal_unreachable"
	CodeUnsupported         RefusalCode = "terminal_unsupported"
	CodeFull                RefusalCode = "terminals_full"
	CodeViewersFull         RefusalCode = "terminal_viewers_full"
	CodeAccessRevoked       RefusalCode = "terminal_access_revoked"
	CodeCloudNotSupported   RefusalCode = "terminal_cloud_not_supported"
	CodeHistoryLineTooLarge RefusalCode = "terminal_history_line_too_large"
	CodeHistoryTooLarge     RefusalCode = "terminal_history_too_large"
	// CodeSocketPathTooLong is a CLAWDLINE_NEXT_DIR so deep that the
	// server's socket path does not fit a Unix socket address (plan v3 D1).
	CodeSocketPathTooLong RefusalCode = "terminal_socket_path_too_long"
	// CodeInvalid is a request whose own values are wrong: a size of zero, a
	// directory that is not one, an empty key, an id of the wrong shape.
	CodeInvalid RefusalCode = "terminal_invalid"
)

// RefusalCodes is every code above, for the tests that hold a table of them
// to account.
var RefusalCodes = []RefusalCode{
	CodeForbidden, CodeNotController, CodeControlled, CodeLeaseSuperseded, CodeLeaseExpired,
	CodeInputGap, CodeInputStateUnknown, CodeInputTooLarge, CodeBusy, CodeClosed, CodeUnreachable,
	CodeUnsupported, CodeFull, CodeViewersFull, CodeAccessRevoked, CodeCloudNotSupported,
	CodeSocketPathTooLong, CodeInvalid,
}

// Refusal is a request this terminal layer did not carry out, by code, with
// the sentence that says why on this machine.
type Refusal struct {
	Code   RefusalCode
	Detail string
	// Err is the backend's own failure behind it, when there was one.
	Err error
}

func (r *Refusal) Error() string {
	if r.Detail == "" {
		return string(r.Code)
	}
	return string(r.Code) + ": " + r.Detail
}

func (r *Refusal) Unwrap() error { return r.Err }

// Refuse is a Refusal as an error.
func Refuse(code RefusalCode, detail string) error { return &Refusal{Code: code, Detail: detail} }

// CodeOf is the refusal code err carries, if it carries one.
func CodeOf(err error) (RefusalCode, bool) {
	var r *Refusal
	if errors.As(err, &r) {
		return r.Code, true
	}
	return "", false
}

// The bounds on one machine's terminals (docs/limits.md N59). Each is a
// registered row in internal/domain/capacity.
const (
	// MaxTerminals is how many terminals one machine holds open at once.
	MaxTerminals = 8
	// MaxInputBytes is the most one keystroke batch may carry.
	MaxInputBytes = 4 << 10
	// MaxPasteBytes is the most one paste may carry.
	MaxPasteBytes = 1 << 20
	// MaxHistoryLines is the most lines one history read answers.
	MaxHistoryLines = 2000
)
