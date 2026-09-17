package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/app"
	"github.com/sainteye/clawdline-go/internal/contract"
	"github.com/sainteye/clawdline-go/internal/domain/task"
)

// sessionAction routes /v1/sessions/{id}/{verb}.
//
// The id is taken off the path rather than out of a body because it identifies
// the thing being acted on, and a terminal id can contain a percent sign — tmux
// panes are `%195` — so it arrives percent-encoded and is decoded here. Reading
// it raw turned `%195` into a control character and answered `not_found`, which
// is the wrong answer to the wrong question.
func (s *Server) sessionAction(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/v1/sessions/")
	cut := strings.LastIndex(rest, "/")
	if cut < 0 {
		writeRefusal(w, http.StatusNotFound, "not_found", "that is not a session action")
		return
	}
	id, verb := rest[:cut], rest[cut+1:]
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
		var body contract.SendRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "that body is not a send")
			return
		}
		if _, err := s.actions().Send(ctx, id, body.Text); err != nil {
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
	return app.Actions{Inventory: s.inventory, Terminals: s.terminals, Store: s.store}
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
	case "backend_unsupported":
		return http.StatusNotImplemented
	case "terminal_io_failed":
		// The Swift app's status for a terminal command that did not complete.
		return http.StatusBadGateway
	}
	return http.StatusInternalServerError
}

var _ = task.CloseReason{}
