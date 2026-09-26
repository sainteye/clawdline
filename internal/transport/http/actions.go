package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/task"
)

// sessionVerb reads `/v1/sessions/{id}/{verb}` off the string the mux
// dispatched by (routePath, gate.go), and gives the id back as a name: one
// segment, split first and decoded once.
//
// The id is taken off the path rather than out of a body because it identifies
// the thing being acted on, and a terminal id can contain a percent sign — tmux
// panes are `%19` — so it arrives percent-encoded and is decoded here. Reading
// it raw turned `%19` into a control character and answered `not_found`, which
// is the wrong answer to the wrong question.
//
// **Split, then decode — never the other way round.** Decoding the whole path
// first is how `..%2F..%2Fv1%2Fauth%2Fx` became four segments to one reader and
// one id to another; here it is one segment holding a separator, which the gate
// has already refused and which would not parse as a route in any case.
func sessionVerb(r *http.Request) (id, verb string, ok bool) {
	rest, cut := strings.CutPrefix(routePath(r), "/v1/sessions/")
	if !cut {
		return "", "", false
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return decodeSegment(parts[0]), parts[1], true
}

// sessionVerbIs is sessionVerb for one named verb, asked for by the methods
// that route answers. Each caller names its own: a route that says GET and not
// HEAD keeps saying that.
func sessionVerbIs(r *http.Request, verb string, methods ...string) (string, bool) {
	asked := false
	for _, method := range methods {
		if r.Method == method {
			asked = true
			break
		}
	}
	if !asked {
		return "", false
	}
	id, got, ok := sessionVerb(r)
	return id, ok && got == verb
}

// sessionAction routes /v1/sessions/{id}/{verb}.
func (s *Server) sessionAction(w http.ResponseWriter, r *http.Request) {
	id, verb, ok := sessionVerb(r)
	if !ok {
		writeRefusal(w, http.StatusNotFound, "not_found", "that is not a session action")
		return
	}
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"a session action is a POST; a GET would make it something a link could do by accident")
		return
	}
	// Typing into a session runs code on this machine, so reading is not
	// enough: the device must have been granted send. A newly paired one has
	// not. The Swift app's sentence and envelope, since its page reads both.
	if !maySend(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "This device may read, and not send.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	switch verb {
	case "send":
		// Pictures travel inside this body, so it may be large; past the Swift
		// server's limit it is refused with that server's words.
		s.sessionWrite(w, r, sendBodyLimit, func(size string, limit int64) string {
			return fmt.Sprintf("That was %s bytes and the limit is %d. Send fewer or smaller pictures.", size, limit)
		}, func(w http.ResponseWriter, raw []byte) {
			var body contract.SendRequest
			if err := json.Unmarshal(raw, &body); err != nil {
				writeRefusal(w, http.StatusBadRequest, "bad_request", "that body is not a send")
				return
			}
			if body.Text == "" && len(body.Images) == 0 {
				writeRefusal(w, http.StatusBadRequest, "empty_text", "there is nothing to type")
				return
			}
			ctx := ctx
			// The terminal half of a send with pictures can take a few seconds per
			// picture; the read of the machine before it keeps the ordinary bound.
			if len(body.Images) > 0 {
				var more context.CancelFunc
				ctx, more = context.WithTimeout(r.Context(), 45*time.Second)
				defer more()
			}
			sent, err := s.actions().SendWithPictures(ctx, id, body.Text, body.Images)
			if err != nil {
				writeActionRefusal(w, err)
				return
			}
			// A person wrote to this session: the one reading that says they
			// are there (proposals.go, board-redesign §4.3), and the run the
			// session relays their words under (runs.go, U4).
			s.heardFrom(sent)
			s.issueRun(ctx, r, sent, body.Text)
			writeJSON(w, contract.ActionResult{OK: true, ID: id, Action: "typed"})
		})
	case "interrupt":
		if _, err := s.actions().Interrupt(ctx, id); err != nil {
			writeActionRefusal(w, err)
			return
		}
		writeJSON(w, contract.ActionResult{OK: true, ID: id, Action: "interrupted"})
	case "key":
		s.sessionWrite(w, r, keyBodyLimit, func(string, int64) string {
			return "That is larger than one key. A key is \"1\"…\"9\", \"tab\", \"shift+tab\" or \"submit\"."
		}, func(w http.ResponseWriter, raw []byte) {
			s.sessionKey(ctx, w, id, raw)
		})
	case "title":
		s.sessionWrite(w, r, CapacityLimit(capacity.SessionTitleRequestBytes), func(size string, limit int64) string {
			return fmt.Sprintf("That title request was %s bytes and the limit is %d.", size, limit)
		}, func(w http.ResponseWriter, raw []byte) {
			var body contract.SessionTitleRequest
			if err := json.Unmarshal(raw, &body); err != nil {
				writeRefusal(w, http.StatusBadRequest, "bad_request", "title must be a string")
				return
			}
			title := normalizedSessionTitle(body.Title)
			if !titleWithinLimit(title) {
				writeRefusal(w, http.StatusBadRequest, "bad_request",
					fmt.Sprintf("title must be at most %d characters", CapacityLimit(capacity.SessionTitleCharacters)))
				return
			}
			item, err := s.actions().Find(ctx, id)
			if err != nil {
				writeActionRefusal(w, err)
				return
			}
			title, err = s.saveSessionTitle(item, body.Title, time.Now())
			if err != nil {
				writeRefusal(w, http.StatusInternalServerError, "title_not_saved", "the session title could not be saved")
				return
			}
			writeJSON(w, contract.SessionTitleReply{
				OK: true, Title: title, DisplayTitle: s.sessionDisplayLabel(ctx, item),
				LocalApplied: true, Downstream: "local_only",
				DownstreamSynced: false,
			})
		})
	case "smart-title":
		if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
			writeRefusal(w, http.StatusBadRequest, "bad_request",
				"smart naming needs an Idempotency-Key so one confirmation can spend at most one model turn")
			return
		}
		s.sessionWrite(w, r, closeBodyLimit, func(size string, limit int64) string {
			return fmt.Sprintf("That smart-title request was %s bytes and the limit is %d.", size, limit)
		}, func(w http.ResponseWriter, raw []byte) {
			var body map[string]any
			if len(raw) > 0 && (json.Unmarshal(raw, &body) != nil || len(body) != 0) {
				writeRefusal(w, http.StatusBadRequest, "bad_request", "smart naming takes no options")
				return
			}
			s.smartSessionTitle(w, r, id, ctx)
		})
	case "close":
		s.sessionWrite(w, r, closeBodyLimit, func(size string, limit int64) string {
			return fmt.Sprintf("That was %s bytes and a close's options are at most %d.", size, limit)
		}, func(w http.ResponseWriter, raw []byte) {
			var body contract.CloseRequest
			// An absent body is an ordinary close. Only a malformed one is a
			// refusal, because "no options" is a legitimate thing to mean.
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &body); err != nil {
					writeRefusal(w, http.StatusBadRequest, "bad_request", "that body is not a close")
					return
				}
			}
			// A close walks a ladder now — the assistant's own quit word,
			// the wait for it to leave, then the terminal — and the ordinary
			// fifteen seconds is shorter than the ladder's own worst case, so
			// the route would have cut it off part way through.
			ctx, more := context.WithTimeout(r.Context(), closeBudget)
			defer more()
			if _, err := s.actions().Close(ctx, id, body.Force); err != nil {
				writeActionRefusal(w, err)
				return
			}
			writeJSON(w, contract.ActionResult{OK: true, ID: id, Action: "closed", Forced: body.Force})
		})
	default:
		writeRefusal(w, http.StatusNotFound, "not_found", "no such action on a session")
	}
}

// closeBodyLimit is what a close's options weigh: `{"force":true}` and the
// closeability version a Cloud viewer carries, with room to spare.
const closeBodyLimit = 16 << 10

// closeBudget is the whole of a close: looking for the session, typing the
// assistant's quit word and watching for it to be read, the polite wait and
// the two signal rungs after it, one last look, and the terminal's own close
// (terminal.farewell). Every one of those is separately bounded; this is the
// ceiling over all of them, and a close that reaches it has taken nothing away.
const closeBudget = 45 * time.Second

// scopeSessions is the receipt scope of a session's writes, and how long one
// of them answers a retry.
//
// **Shorter than the default day** (`store.ReceiptWindow`). The store holds
// `store.ReceiptLimit` receipts per scope inside its window and refuses a new
// request past that, and these are the most frequent writes this daemon has:
// every message and every menu answer files one. Two hours is far longer than
// any retry of a card somebody is looking at, and short enough that a busy day
// cannot fill the scope and start refusing new messages. A retry past it is
// told `receipt_expired`, which the page reads as "not known" and offers a
// look at (`session/outcome.ts`) — never a silent second send.
const scopeSessions = "sessions"

const sessionReceiptWindow = 2 * time.Hour

// sessionWrite answers one write to a session — a message, a menu answer, a
// close — once per Idempotency-Key, when the caller names one (D03, F2).
//
// These are the writes a caller is least sure of. Across Clawdline Cloud the
// answer can be lost after the Mac acted, and "try again" was then the same
// words typed a second time. So a key names one request — the route and the
// exact body — and a retry under it is answered with the first answer instead
// of being carried out again; the same key with another body is refused.
// Without a key a write is carried out each time it is asked, as an older page
// expects.
//
// The body is read here, once, because it is part of what the key names.
func (s *Server) sessionWrite(w http.ResponseWriter, r *http.Request, limit int64,
	tooLargeWords func(size string, limit int64) string, act func(http.ResponseWriter, []byte)) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			size := fmt.Sprintf("more than %d", tooLarge.Limit)
			if r.ContentLength > 0 {
				size = fmt.Sprint(r.ContentLength)
			}
			writeRefusal(w, http.StatusRequestEntityTooLarge, "too_large", tooLargeWords(size, tooLarge.Limit))
			return
		}
		writeRefusal(w, http.StatusBadRequest, "bad_request", "that body could not be read")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" || s.store == nil {
		act(w, raw)
		return
	}
	k := store.ReceiptKey{Scope: scopeSessions, Actor: accessOf(r).verdict.Device, Key: key}
	s.receiptedFor(w, r, k, requestDigest([]byte(r.Method), []byte(routePath(r)), raw),
		store.ReceiptPolicy{Window: sessionReceiptWindow}, sessionWriteFiled,
		func(w http.ResponseWriter) { act(w, raw) })
}

// sessionWriteFiled is which answers a key keeps.
//
// An answer given before the terminal was touched — a refusal, a full lane, a
// question that moved — is about that moment, and the key is given back so the
// retry is carried out. An answer after the terminal was touched is kept: the
// words were typed, or the terminal failed part-way (`terminal_io_failed`, a
// `send_failed` or `close_failed` behind a 500) and some of them may have been.
// A retry of either is told what happened, never handed a second go.
func sessionWriteFiled(status int) bool {
	return status >= 200 && status < 300 ||
		status == http.StatusBadGateway || status == http.StatusInternalServerError
}

// actions builds the action surface for one request.
func (s *Server) actions() app.Actions {
	return app.Actions{Inventory: s.inventory, Reading: s.readings, Terminals: s.terminals, Store: s.store,
		Restore:  s.restore,
		Pictures: app.Pictures{Drops: s.pictures.drops, Pasteboard: s.pictures.pasteboard},
		Owed: func(ctx context.Context) ([]task.Obligation, error) {
			return s.owed(ctx, s.reading(ctx).Sessions)
		}}
}

// writeActionRefusal gives each typed refusal the status that describes it, and
// carries a blocked close's reasons out to the caller.
//
// The reasons matter: a screen that can only say "refused" sends a person to
// the terminal to find out why, which is the round trip this whole daemon
// exists to remove.
func writeActionRefusal(w http.ResponseWriter, err error) {
	ref, ok := err.(app.Refusal)
	if !ok {
		writeRefusal(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	status := actionStatus(ref.Code)
	if ref.Code == "close_blocked" {
		reasons := make([]contract.CloseReason, 0, len(ref.Reasons))
		for _, r := range ref.Reasons {
			reasons = append(reasons, contract.CloseReason{
				Kind:        "obligation",
				Code:        string(r.Kind),
				SubjectID:   r.Mover.ID,
				SubjectKind: string(r.Mover.Kind),
				Mover:       wireCloseMover(r.Mover, ""),
			})
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(contract.CloseRefusal{
			Error: ref.Code, Detail: ref.Detail, Reasons: reasons,
		})
		return
	}
	writeRefusal(w, status, ref.Code, ref.Detail)
}

func actionStatus(code string) int {
	switch code {
	case "session_not_found":
		return http.StatusNotFound
	case "session_unknown", "closeability_unknown":
		// Not 404. The machine did not say this is absent; it said it could not
		// see. 409 is the honest shape: try again when the reading is whole.
		return http.StatusConflict
	case "close_blocked":
		return http.StatusConflict
	case "close_nothing_there":
		// The session was gone before the close reached it. The caller's
		// reading was one moment behind the machine, and asking again against
		// a fresh one is the whole remedy.
		return http.StatusNotFound
	case "close_occupied", "close_unreadable":
		// Nothing was typed, nothing was signalled and nothing was closed:
		// what holds that terminal is not this close's to end, or could not be
		// read at all. 409, because a fresh reading is what changes the answer.
		return http.StatusConflict
	case "close_assistant_running":
		// The assistant was asked to leave, by its own word and then by a
		// signal, and did not. The terminal was left open on purpose and a
		// fresh attempt is a reasonable thing for a person to make, so this
		// is the one rung of the close whose idempotency key is handed back.
		return http.StatusConflict
	case "close_needs_a_person", "close_unconfirmed", "close_quit_refused":
		// The terminal was touched and did not finish: a question is on the
		// Mac's screen, or the close went unanswered and could still land, or
		// the quit word may have reached the composer. The Swift server's
		// status for a terminal command that did not complete, and the one
		// that keeps a retry under the same key from asking a second question
		// on the same screen.
		return http.StatusBadGateway
	case "empty_text", "bad_request":
		return http.StatusBadRequest
	case "busy":
		// The Swift app's answer when its terminal queue is full.
		return http.StatusTooManyRequests
	case "menu_moved", "menu_unreadable":
		// The question answered is not the one on the screen, or the screen
		// could not be read to say: nothing was typed, and asking again
		// against a fresh reading is the remedy.
		return http.StatusConflict
	case "session_asking":
		// A question is on the session's screen and nothing was typed: the
		// person answers it from the row's buttons, and the same send then
		// goes. 409, and not kept under the key, because a fresh reading is
		// what changes the answer.
		return http.StatusConflict
	case "pictures_unavailable":
		return http.StatusServiceUnavailable
	case "nobody_attached":
		// The selection landed and no screen is showing it: a tmux session with
		// no client attached. 409 for the same reason the two readings above
		// are — the state, not the request, is what refused, and attaching a
		// terminal and asking again is the remedy.
		return http.StatusConflict
	case "backend_unsupported":
		return http.StatusNotImplemented
	case "send_unsubmitted":
		// The line was typed and Enter was held back: it is in the session's
		// input line. 502, like every write the terminal did not finish, so
		// the answer is kept and a retry under the same key is told this
		// rather than typing the line a second time.
		return http.StatusBadGateway
	case "terminal_io_failed":
		// The Swift app's status for a terminal command that did not complete.
		return http.StatusBadGateway
	}
	return http.StatusInternalServerError
}

var _ = task.CloseReason{}
