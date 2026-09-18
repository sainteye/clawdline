package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/app"
	"github.com/sainteye/clawdline-go/internal/contract"
	"github.com/sainteye/clawdline-go/internal/domain/task"
)

// sessionVerb reads `/v1/sessions/{id}/{verb}` off the string the mux
// dispatched by (routePath, gate.go), and gives the id back as a name: one
// segment, split first and decoded once.
//
// The id is taken off the path rather than out of a body because it identifies
// the thing being acted on, and a terminal id can contain a percent sign — tmux
// panes are `%195` — so it arrives percent-encoded and is decoded here. Reading
// it raw turned `%195` into a control character and answered `not_found`, which
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
		var body contract.SendRequest
		limited := http.MaxBytesReader(w, r.Body, sendBodyLimit)
		if err := json.NewDecoder(limited).Decode(&body); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				size := fmt.Sprintf("more than %d", tooLarge.Limit)
				if r.ContentLength > 0 {
					size = fmt.Sprint(r.ContentLength)
				}
				writeRefusal(w, http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf(
					"That was %s bytes and the limit is %d. Send fewer or smaller pictures.", size, tooLarge.Limit))
				return
			}
			writeRefusal(w, http.StatusBadRequest, "bad_request", "that body is not a send")
			return
		}
		if body.Text == "" && len(body.Images) == 0 {
			writeRefusal(w, http.StatusBadRequest, "empty_text", "there is nothing to type")
			return
		}
		// The terminal half of a send with pictures can take a few seconds per
		// picture; the read of the machine before it keeps the ordinary bound.
		if len(body.Images) > 0 {
			var more context.CancelFunc
			ctx, more = context.WithTimeout(r.Context(), 45*time.Second)
			defer more()
		}
		if _, err := s.actions().SendWithPictures(ctx, id, body.Text, body.Images); err != nil {
			writeActionRefusal(w, err)
			return
		}
		writeJSON(w, contract.ActionResult{OK: true, ID: id, Action: "typed"})
	case "interrupt":
		if _, err := s.actions().Interrupt(ctx, id); err != nil {
			writeActionRefusal(w, err)
			return
		}
		writeJSON(w, contract.ActionResult{OK: true, ID: id, Action: "interrupted"})
	case "key":
		s.sessionKey(ctx, w, r, id)
	case "close":
		var body contract.CloseRequest
		// An absent body is an ordinary close. Only a malformed one is a
		// refusal, because "no options" is a legitimate thing to mean.
		if r.ContentLength > 0 {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeRefusal(w, http.StatusBadRequest, "bad_request", "that body is not a close")
				return
			}
		}
		if _, err := s.actions().Close(ctx, id, body.Force); err != nil {
			writeActionRefusal(w, err)
			return
		}
		writeJSON(w, contract.ActionResult{OK: true, ID: id, Action: "closed", Forced: body.Force})
	default:
		writeRefusal(w, http.StatusNotFound, "not_found", "no such action on a session")
	}
}

// actions builds the action surface for one request.
func (s *Server) actions() app.Actions {
	return app.Actions{Inventory: s.inventory, Terminals: s.terminals, Store: s.store,
		Pictures: app.Pictures{Drops: s.pictures.drops, Pasteboard: s.pictures.pasteboard},
		Owed: func(ctx context.Context) ([]task.Obligation, error) {
			return s.owed(ctx, s.inventory.Read(ctx).Sessions)
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
	case "empty_text", "bad_request":
		return http.StatusBadRequest
	case "busy":
		// The Swift app's answer when its terminal queue is full.
		return http.StatusTooManyRequests
	case "pictures_unavailable":
		return http.StatusServiceUnavailable
	case "backend_unsupported":
		return http.StatusNotImplemented
	case "terminal_io_failed":
		// The Swift app's status for a terminal command that did not complete.
		return http.StatusBadGateway
	}
	return http.StatusInternalServerError
}

var _ = task.CloseReason{}
