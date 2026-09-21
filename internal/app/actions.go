package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/artifacts"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/app/lane"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/task"
)

// Actions carries out what a person asks of one session: type into it, stop
// what it is doing, or take it away.
//
// Every one of them goes through here rather than straight to a terminal host,
// because each has a precondition that is not the terminal's to check. The
// terminal will happily close a session that still owes somebody a landing.
type Actions struct {
	Inventory Inventory
	// Reading is the machine's one reading, when the daemon wiring this holds
	// one. Find asks it for a reading taken now, because what Find answers
	// refuses a person's action by saying their session is not there; nil
	// scans through Inventory directly, which is what a test wants.
	Reading   *InventoryReading
	Terminals []ports.TerminalHost
	Store     *store.Store
	// Owed is what the sessions on this machine still owe — the broker's
	// pending landings, read at the moment of asking (D01). Nil is a daemon
	// that cannot say, and a close is then refused as unknown rather than
	// let through: an unanswered question is not an empty list.
	Owed func(ctx context.Context) ([]task.Obligation, error)
	// Pictures is what a send with pictures needs besides a terminal. A zero
	// value refuses pictures rather than dropping them.
	Pictures Pictures
	// Lanes is where every write to a terminal takes its turn: one writer per
	// terminal, one ceiling for the machine (lane, D22). Nil means the
	// process-wide set, so an Actions built anywhere still shares the one
	// lane per terminal — two sets of lanes would be two writers again.
	Lanes *lane.Lanes
}

// laneWait is the longest a write waits for its terminal's turn.
var laneWait = 10 * time.Second

// terminalLanes is the process-wide set an Actions with no Lanes uses.
var terminalLanes = lane.New(lane.DefaultLimit)

// TerminalLanes is the process-wide set, for a daemon that wires one Lanes
// through every writer and wants it to be this one.
func TerminalLanes() *lane.Lanes { return terminalLanes }

func (a Actions) lanes() *lane.Lanes {
	if a.Lanes != nil {
		return a.Lanes
	}
	return terminalLanes
}

// turn takes the terminal's lane for one whole write. A full machine, or a
// caller that gave up waiting, is `busy` — the Swift app's code for a full
// terminal queue, answered 429 — and nothing was typed.
func (a Actions) turn(ctx context.Context, s session.Session) (func(), error) {
	// The wait is bounded apart from the caller's own deadline: the broker's
	// notice pump runs inside its beat, and a beat held longer than three
	// ticks is reported stalled. Ten seconds is past any single send and
	// short of a picture send, which is a caller that can try again.
	wait, cancel := context.WithTimeout(ctx, laneWait)
	defer cancel()
	release, err := a.lanes().Acquire(wait, lane.TerminalKey(string(s.Backend), s.ID))
	if err != nil {
		return nil, Refusal{Code: "busy", Detail: err.Error(), Cause: err}
	}
	return release, nil
}

// Refusal is a typed no, in the shape every refusal on this daemon has.
type Refusal struct {
	Code   string
	Detail string
	// Reasons is filled when a close is refused, so the caller can say what is
	// in the way rather than only that something is.
	Reasons []task.CloseReason
	// Cause is the typed error underneath, when there is one — a lane.Busy,
	// so a caller that treats backpressure differently from failure can tell.
	Cause error
}

func (r Refusal) Error() string { return r.Code + ": " + r.Detail }

// Unwrap exposes Cause to errors.As.
func (r Refusal) Unwrap() error { return r.Cause }

// Find resolves one session id against a current reading of the machine.
//
// The two ways this fails are deliberately different codes. A complete reading
// that does not contain the id has proved the session is gone. An incomplete
// one has proved nothing, and answering `not_found` there would turn a terminal
// that lost accessibility for a moment into a session somebody deleted.
//
// **Which reading has to be complete is the source that would have listed this
// id, and that source alone** (D05 ③). It used to be the whole merged reading,
// whose Complete is the AND over every source — so on this Mac, where one
// iTerm2 window has answered null to `tabs()` in every reading for hours, a
// tmux pane that tmux listed completely could never be shown to have gone, and
// a session the person had closed came back from their phone as
// `session_unknown: it is not absent, it is unseen`. The id's own shape says
// which source issues it (session.SourceForID); an id in no source's shape
// falls back to the whole reading, which is where it started.
func (a Actions) Find(ctx context.Context, id string) (session.Session, error) {
	inv := a.read(ctx)
	for _, item := range inv.Sessions {
		if item.ID == id {
			return item, nil
		}
	}
	source := session.SourceForID(id)
	if proves, why := inv.ProvesAbsence(source); !proves {
		return session.Session{}, Refusal{
			Code: "session_unknown",
			Detail: fmt.Sprintf("this reading of the machine (%s) does not answer for %s, so %s is not absent, "+
				"it is unseen: %s", inv.Provenance, sourceName(source), id, why),
		}
	}
	return session.Session{}, Refusal{
		Code:   "session_not_found",
		Detail: fmt.Sprintf("no session %s in a complete reading of %s on this machine", id, sourceName(source)),
	}
}

// sourceName is how a refusal names the source that owes the answer, including
// when there is none to name.
func sourceName(source string) string {
	if source == "" {
		return "this machine"
	}
	return source
}

// read is a reading taken for this call. An action names a session and then
// acts on it, so a held reading could refuse a tab opened a second ago as
// absent, or type into one that has since gone.
func (a Actions) read(ctx context.Context) session.Inventory {
	if a.Reading != nil {
		return a.Reading.Fresh(ctx)
	}
	return a.Inventory.Read(ctx)
}

// host returns the terminal that owns this session.
func (a Actions) host(s session.Session) (ports.TerminalHost, error) {
	for _, h := range a.Terminals {
		if h.Name() == string(s.Backend) {
			return h, nil
		}
	}
	return nil, Refusal{
		Code:   "backend_unsupported",
		Detail: fmt.Sprintf("nothing on this machine drives a %q session", s.Backend),
	}
}

// Send types one line into a session and submits it.
//
// A nil error means the bytes reached the tty. It never means the assistant
// read them, and callers must not report it as delivery: whether a turn was
// taken is a separate fact with its own evidence, and the fleet list is where
// that answer lives.
//
// A refusal given before the first byte carries terminal.Unsent (or, for a
// lane that never came free, lane.Busy); a send_failed carries the terminal's
// own error. A caller deciding whether to type the same line again — the
// broker's briefing — reads that, and only that: after an Unsent it may, and
// after anything else the line may already be there.
func (a Actions) Send(ctx context.Context, id, text string) (session.Session, error) {
	if text == "" {
		return session.Session{}, Refusal{Code: "empty_text", Detail: "there is nothing to type",
			Cause: terminal.Unsent{Why: "there is nothing to type"}}
	}
	s, err := a.Find(ctx, id)
	if err != nil {
		return session.Session{}, beforeTheFirstByte(err)
	}
	h, err := a.host(s)
	if err != nil {
		return session.Session{}, beforeTheFirstByte(err)
	}
	release, err := a.turn(ctx, s)
	if err != nil {
		return s, err
	}
	defer release()
	if err := h.Send(ctx, s, text); err != nil {
		return s, Refusal{Code: "send_failed", Detail: err.Error(), Cause: err}
	}
	a.record(ctx, "session.typed", s.ID, map[string]any{"bytes": len(text)})
	return s, nil
}

// beforeTheFirstByte marks a refusal Send gave before writing anything as
// terminal.Unsent, so it is not mistaken for a write that failed part way.
func beforeTheFirstByte(err error) error {
	r, ok := err.(Refusal)
	if !ok || r.Cause != nil {
		return err
	}
	r.Cause = terminal.Unsent{Why: r.Detail}
	return r
}

// Pictures is where a send puts its pictures and how it lends them to Claude Code.
type Pictures struct {
	Drops      *artifacts.Drops
	Pasteboard PictureLender
}

// PictureLender is the pasteboard, as a send borrows it (artifacts.Pasteboard).
type PictureLender interface {
	Available() bool
	Borrow(ctx context.Context) (*artifacts.Borrowed, error)
	Offer(ctx context.Context, b *artifacts.Borrowed, path string) error
	GiveBack(ctx context.Context, b *artifacts.Borrowed) error
}

// typist types text into a session without submitting it. Only the picture
// path needs that, because only there is the prompt assembled in pieces.
type typist interface {
	Type(ctx context.Context, s session.Session, text string) error
}

// Pauses between the pieces of a prompt with pictures in it, the Swift app's:
// the far side reads the pasteboard when it handles Ctrl-V, which is not the
// instant the byte arrives, and the last picture is still being read after the
// Return.
var (
	pictureSettle = 250 * time.Millisecond
	submitSettle  = 200 * time.Millisecond
)

// keyPaste is Ctrl-V, sent as a key and outside any paste: inside one it is
// just a character.
var keyPaste = []byte{0x16}

// The pasteboard is one thing on the machine, so picture sends take turns at
// it. A few may wait; past that the answer is `busy` before anything is typed,
// which is the difference between a queue and a pile.
var (
	pasteboardSlot    = make(chan struct{}, 1)
	pasteboardWaiting atomic.Int32
)

const pasteboardQueue = 4

// SendWithPictures is `sendTerminal` with `images`: text, pictures, or both.
//
// Each picture arrives as a `data:` URL, is drawn and written out again as PNG
// (artifacts.Normalize) and saved in the drop cache. Into a Claude Code session
// each one is then lent to the pasteboard and pasted with Ctrl-V, so it arrives
// as `[Image #1]`; anywhere else — Codex, a platform with no pasteboard, a
// pasteboard that would not take it — the assistant is given the file's path,
// which is plainer and never wrong. A picture that cannot be decoded is left
// out; a message none of whose pictures can be is refused.
//
// A nil error means the bytes reached the tty, as for Send. Files that never
// reached one are removed; the rest are kept for the program to read later.
func (a Actions) SendWithPictures(ctx context.Context, id, text string, images []string) (session.Session, error) {
	if len(images) == 0 {
		return a.Send(ctx, id, text)
	}
	policy := artifacts.ProductionPolicy
	if len(images) > policy.MaxImagesPerMessage {
		return session.Session{}, Refusal{Code: "bad_request",
			Detail: fmt.Sprintf("one message carries at most %d pictures", policy.MaxImagesPerMessage)}
	}
	if a.Pictures.Drops == nil {
		return session.Session{}, Refusal{Code: "pictures_unavailable",
			Detail: "this daemon was started without a place to keep pictures"}
	}
	s, err := a.Find(ctx, id)
	if err != nil {
		return session.Session{}, err
	}
	h, err := a.host(s)
	if err != nil {
		return session.Session{}, err
	}
	var paths []string
	for _, url := range images {
		raw, ok := artifacts.DecodeDataURL(url)
		if !ok {
			continue
		}
		pic, err := artifacts.Normalize(ctx, raw, policy)
		if err != nil {
			log.Printf("send: a picture was left out: %v", err)
			continue
		}
		path, err := a.Pictures.Drops.Store(pic.PNG, time.Now())
		if err != nil {
			log.Printf("send: a picture could not be kept: %v", err)
			continue
		}
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		return s, Refusal{Code: "bad_request", Detail: "None of those were images I could read."}
	}
	how, err := a.deliverPictures(ctx, s, h, text, paths)
	if err != nil {
		a.Pictures.Drops.Discard(paths)
		return s, err
	}
	a.record(ctx, "session.typed", s.ID, map[string]any{
		"bytes": len(text), "images": len(paths), "skipped": len(images) - len(paths), "delivery": how,
	})
	return s, nil
}

// deliverPictures is `Targets.send(_ pieces:to:)`, and answers how the
// pictures went: "paste" or "path".
func (a Actions) deliverPictures(ctx context.Context, s session.Session, h ports.TerminalHost,
	text string, paths []string) (string, error) {
	asPaths := func(pending []string) string {
		parts := []string{}
		if text != "" {
			parts = append(parts, text)
		}
		for _, p := range pending {
			parts = append(parts, quotedPath(p))
		}
		return strings.Join(parts, " ")
	}
	sendPaths := func() (string, error) {
		if err := h.Send(ctx, s, asPaths(paths)); err != nil {
			return "", Refusal{Code: "send_failed", Detail: err.Error()}
		}
		return "path", nil
	}
	// Paths are one line, typed in the terminal's own turn (D22).
	byPath := func() (string, error) {
		release, err := a.turn(ctx, s)
		if err != nil {
			return "", err
		}
		defer release()
		return sendPaths()
	}
	keys, canKey := h.(ports.KeyHost)
	typer, canType := h.(typist)
	lender := a.Pictures.Pasteboard
	// Claude Code specifically: `[Image #1]` is its convention, and in a shell
	// Ctrl-V is readline's quoted-insert.
	if s.Assistant != session.AssistantClaude || lender == nil || !lender.Available() || !canKey || !canType {
		return byPath()
	}

	release, err := takePasteboard(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	// Then the terminal's turn, for the whole prompt — words, pastes, Return
	// — because a notice typed between two of its pieces would land inside
	// it. Always in this order, pasteboard then terminal: a send holding a
	// terminal never waits for the pasteboard, so the two cannot wait on
	// each other, and the pasteboard's own queue (1 + 4, then busy) is still
	// the first thing a picture send meets.
	turn, err := a.turn(ctx, s)
	if err != nil {
		return "", err
	}
	defer turn()
	// Once the pasteboard is ours the prompt is typed to the end, whatever the
	// caller does meanwhile: half a prompt left in somebody's input line is
	// worse than a late answer.
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	defer cancel()
	borrowed, err := lender.Borrow(work)
	if err != nil {
		log.Printf("send: the pasteboard could not be borrowed, sending paths: %v", err)
		return sendPaths()
	}
	defer func() {
		time.Sleep(submitSettle)
		if err := lender.GiveBack(work, borrowed); err != nil {
			log.Printf("send: the pasteboard was not given back: %v", err)
		}
	}()

	typed := false
	if text != "" {
		if err := typer.Type(work, s, text); err != nil {
			return "", Refusal{Code: "send_failed", Detail: err.Error()}
		}
		typed = true
	}
	asPath := 0
	for _, p := range paths {
		if err := lender.Offer(work, borrowed, p); err != nil {
			// Its bytes would not load. The path still works and is only plainer.
			asPath++
			words := quotedPath(p)
			if typed {
				words = " " + words
			}
			if err := typer.Type(work, s, words); err != nil {
				return "", Refusal{Code: "send_failed", Detail: err.Error()}
			}
			typed = true
			continue
		}
		if err := keys.Keystroke(work, s, keyPaste); err != nil {
			return "", Refusal{Code: "send_failed", Detail: err.Error()}
		}
		typed = true
		time.Sleep(pictureSettle)
	}
	if err := keys.Keystroke(work, s, keyReturn); err != nil {
		return "", Refusal{Code: "send_failed", Detail: err.Error()}
	}
	if asPath > 0 {
		log.Printf("send: %d image(s) went as paths", asPath)
		return "paste+path", nil
	}
	return "paste", nil
}

// takePasteboard waits for the pasteboard, a little. The refusal says nothing
// was typed, which is true: nothing is, until the pasteboard is held.
func takePasteboard(ctx context.Context) (func(), error) {
	if pasteboardWaiting.Add(1) > pasteboardQueue {
		pasteboardWaiting.Add(-1)
		return nil, Refusal{Code: "busy", Detail: fmt.Sprintf(
			"This machine already has %d picture sends in hand. Try again after they drain.", pasteboardQueue)}
	}
	defer pasteboardWaiting.Add(-1)
	select {
	case pasteboardSlot <- struct{}{}:
		return func() { <-pasteboardSlot }, nil
	case <-ctx.Done():
		return nil, Refusal{Code: "busy",
			Detail: "the pasteboard was still lent to another send; nothing was typed"}
	}
}

// quotedPath is `Drop.quoted`: quoted only when it has to be, because the
// prompt is something a person is about to read.
func quotedPath(path string) string {
	safe := true
	for _, r := range path {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-+=@~", r)) {
			safe = false
			break
		}
	}
	if safe {
		return path
	}
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}

// Interrupt stops the current turn without closing the session.
func (a Actions) Interrupt(ctx context.Context, id string) (session.Session, error) {
	s, err := a.Find(ctx, id)
	if err != nil {
		return session.Session{}, err
	}
	h, err := a.host(s)
	if err != nil {
		return session.Session{}, err
	}
	release, err := a.turn(ctx, s)
	if err != nil {
		return s, err
	}
	defer release()
	if err := h.Interrupt(ctx, s); err != nil {
		return s, Refusal{Code: "interrupt_failed", Detail: err.Error()}
	}
	a.record(ctx, "session.interrupted", s.ID, nil)
	return s, nil
}

// Close takes the session away, once nothing is owed.
//
// The precondition is the same projection the fleet list draws, asked of the
// same function, so a row that reads `safe` closes and a row that reads
// `blocked` refuses with the very reasons it was showing. `force` exists
// because a person may know something the obligations do not, but it cannot
// override `unknown`: overriding a refusal is a decision, and there is nothing
// to decide about when the list could not be read.
func (a Actions) Close(ctx context.Context, id string, force bool) (session.Session, error) {
	s, err := a.Find(ctx, id)
	if err != nil {
		return session.Session{}, err
	}
	owed, owedErr := a.owed(ctx)
	c := task.Closeability(s.ID, owed, owedErr)
	switch c.State {
	case task.CloseUnknown:
		return s, Refusal{
			Code:   "closeability_unknown",
			Detail: "what this session owes could not be read, and an unreadable list is not an empty one",
		}
	case task.CloseBlocked:
		if !force {
			return s, Refusal{
				Code:    "close_blocked",
				Detail:  fmt.Sprintf("still owed: %s", summarise(c.Reasons)),
				Reasons: c.Reasons,
			}
		}
	}
	h, err := a.host(s)
	if err != nil {
		return session.Session{}, err
	}
	// A close types into the session now — the assistant's own quit word,
	// before anything is taken away (terminal.farewell) — so it takes the
	// terminal's lane like every other write. Without it a close and a message
	// would be two writers on one terminal, which is the one thing the lane
	// exists to prevent.
	release, err := a.turn(ctx, s)
	if err != nil {
		return s, err
	}
	defer release()
	if err := h.Close(ctx, s); err != nil {
		return s, closeRefusal(err)
	}
	a.record(ctx, "session.closed", s.ID, map[string]any{"forced": force, "owed": len(c.Reasons)})
	return s, nil
}

// closeRefusal names which rung of the close stopped it.
//
// **One `close_failed` for every way a close can end is a refusal nobody can
// act on.** The four things a person can do about a close that did not happen
// are different things — wait for the assistant to finish, look at what else
// is running in that terminal, answer the question iTerm2 put on the screen,
// or simply look because nothing here can say — and a code that does not
// distinguish them sends every one of them to the terminal to find out which.
//
// Each of these is a terminal refusal given *before* the terminal was taken
// away, and every one of them leaves the session exactly as it was.
func closeRefusal(err error) error {
	var (
		unreadable  terminal.Unreadable
		occupied    terminal.Occupied
		quitRefused terminal.QuitRefused
		running     terminal.StillRunning
		unconfirmed terminal.Unconfirmed
		unsent      terminal.Unsent
		failure     terminal.Failure
	)
	switch {
	case errors.As(err, &unreadable):
		return Refusal{Code: "close_unreadable", Detail: unreadable.Why, Cause: err}
	case errors.As(err, &occupied):
		return Refusal{Code: "close_occupied", Detail: occupied.Why, Cause: err}
	case errors.As(err, &quitRefused):
		return Refusal{Code: "close_quit_refused", Detail: quitRefused.Why, Cause: err}
	case errors.As(err, &running):
		return Refusal{Code: "close_assistant_running", Detail: running.Why, Cause: err}
	case errors.As(err, &unconfirmed):
		// The terminal was asked and did not answer, and a look afterwards
		// could not settle it either way. Neither done nor failed: an iTerm2
		// close that ran out of time has been seen to land later, when
		// somebody answered the sheet it was waiting behind — and when that
		// is what happened, the refusal says so, because a person can end it
		// by walking to the machine.
		if unconfirmed.Attention {
			return Refusal{Code: "close_needs_a_person", Detail: unconfirmed.Why, Cause: err}
		}
		return Refusal{Code: "close_unconfirmed", Detail: unconfirmed.Why, Cause: err}
	case errors.As(err, &unsent):
		// Nothing was there to close. The reading the caller acted on is one
		// moment behind the machine, which is not an error on anybody's part.
		return Refusal{Code: "close_nothing_there", Detail: unsent.Why, Cause: err}
	case errors.As(err, &failure) && failure.Attention:
		// Something on the Mac's screen is waiting for a person — a close
		// confirmation, a refused automation permission — and until they
		// answer it, nothing else can happen to that terminal.
		return Refusal{Code: "close_needs_a_person", Detail: failure.Message, Cause: err}
	}
	return Refusal{Code: "close_failed", Detail: err.Error(), Cause: err}
}

var errOwedUnwired = errors.New("this daemon has no reader for what sessions owe")

func (a Actions) owed(ctx context.Context) ([]task.Obligation, error) {
	if a.Owed == nil {
		return nil, errOwedUnwired
	}
	return a.Owed(ctx)
}

// summarise names what is in the way, rather than counting it.
//
// A count sends a person looking; a list is the answer. The reasons travel on
// the refusal too, so this is the sentence, not the data.
func summarise(reasons []task.CloseReason) string {
	kinds := make([]string, 0, len(reasons))
	seen := map[task.Kind]bool{}
	for _, r := range reasons {
		if seen[r.Kind] {
			continue
		}
		seen[r.Kind] = true
		kinds = append(kinds, string(r.Kind))
	}
	return strings.Join(kinds, ", ")
}

// record writes what happened. A failure to record is logged by the store and
// does not undo the action: the bytes are already typed, and pretending
// otherwise would make the record less true, not more.
func (a Actions) record(ctx context.Context, kind, subject string, payload map[string]any) {
	if a.Store == nil {
		return
	}
	var raw json.RawMessage
	if payload != nil {
		raw, _ = json.Marshal(payload)
	}
	_ = a.Store.Append(ctx, store.Event{Kind: kind, Subject: subject, Payload: raw})
}
