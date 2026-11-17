package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	sb "github.com/sainteye/clawdline/internal/domain/sessionboard"
)

const sessionBoardRoute = "/v1/orchestrator/session-boards"

func (s *Server) sessionBoardActor(ctx context.Context, id string, qualified bool) (sb.Actor, string, bool) {
	var actor sb.Actor
	var cwd string
	if id == "" {
		return actor, "", false
	}
	inv := s.freshReading(ctx)
	for _, row := range inv.Sessions {
		if !row.IsAssistant() || row.ConversationID == "" {
			continue
		}
		actorID := string(row.Assistant) + ":" + row.ConversationID
		if (!qualified && row.ConversationID != id) || (qualified && actorID != id) {
			continue
		}
		if actor.ID != "" || !currentExecution(row, inv) || executionFingerprint(row, liveOf(row)) == "" {
			return sb.Actor{}, "", false
		}
		label := row.Label
		if label == "" {
			label = string(row.Assistant)
		}
		actor, cwd = sb.Actor{ID: actorID, Label: label}, row.CWD
	}
	if actor.ID == "" || cwd == "" {
		return sb.Actor{}, "", false
	}
	scope, ok := projects.ResolveScope(cwd)
	if !ok {
		return sb.Actor{}, "", false
	}
	_, ok = s.workV2Projects(ctx).resolve(scope.Key, "")
	return actor, scope.ID, ok
}

func (s *Server) sessionBoardsRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET or POST for Session message boards.")
		return
	}
	if s.store == nil {
		writeSessionBoardError(w, nil)
		return
	}
	ctx := r.Context()
	q := r.URL.Query()
	for k, vv := range q {
		if len(vv) != 1 || (k != "conversation_id" && (r.Method != http.MethodGet || (k != "board" && k != "after" && k != "before"))) {
			writeSessionBoardError(w, sb.ErrInvalid)
			return
		}
	}
	machine := machineAuthed(r)
	actor := sb.Actor{Label: "Person"}
	project := ""
	var ok bool
	if machine {
		actor, project, ok = s.sessionBoardActor(ctx, q.Get("conversation_id"), false)
		if !ok {
			writeSessionBoardError(w, sb.ErrDenied)
			return
		}
	} else if q.Has("conversation_id") {
		writeSessionBoardError(w, sb.ErrDenied)
		return
	}
	if r.Method == http.MethodGet {
		s.readSessionBoards(w, r, project, actor.ID)
		return
	}
	if !machine && !maySend(r) {
		writeSessionBoardError(w, sb.ErrDenied)
		return
	}
	var cmd sb.Command
	var extra any
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	key := r.Header.Get("Idempotency-Key")
	if dec.Decode(&cmd) != nil || dec.Decode(&extra) != io.EOF || !validSessionBoardCommand(cmd) ||
		((cmd.Action == "create" || cmd.Action == "post") && !sb.ValidText(key, sb.MaxKey)) {
		writeSessionBoardError(w, sb.ErrInvalid)
		return
	}
	var target sb.Actor
	switch cmd.Action {
	case "create":
		if machine && cmd.Target != "" {
			writeSessionBoardError(w, sb.ErrDenied)
			return
		}
		if !machine {
			target, project, ok = s.sessionBoardActor(ctx, cmd.Target, true)
			if !ok {
				writeSessionBoardError(w, sb.ErrDenied)
				return
			}
		}
	case "transfer":
		var targetProject string
		target, targetProject, ok = s.sessionBoardActor(ctx, cmd.Target, true)
		if !ok || (machine && targetProject != project) {
			writeSessionBoardError(w, sb.ErrDenied)
			return
		}
		project = targetProject
	}
	out, err := s.store.ChangeSessionBoard(ctx, project, actor, target, cmd, key, time.Now())
	if err != nil {
		writeSessionBoardError(w, err)
		return
	}
	writeJSON(w, out)
}

func validSessionBoardCommand(c sb.Command) bool {
	if c.Action == "create" {
		return c.Board == "" && c.ReplyTo == 0 && sb.ValidText(c.Name, sb.MaxName) && sb.ValidText(c.Text, sb.MaxText)
	}
	if c.Board == "" || c.Name != "" {
		return false
	}
	switch c.Action {
	case "post":
		return c.Target == "" && c.ReplyTo >= 0 && sb.ValidText(c.Text, sb.MaxText)
	case "summary":
		return c.Target == "" && c.ReplyTo == 0 && (c.Text == "" || sb.ValidText(c.Text, sb.MaxText))
	case "transfer":
		return c.Text == "" && c.ReplyTo == 0 && c.Target != ""
	case "archive", "join", "leave":
		return c.Text == "" && c.ReplyTo == 0 && c.Target == ""
	default:
		return false
	}
}

func (s *Server) readSessionBoards(w http.ResponseWriter, r *http.Request, project, actor string) {
	q := r.URL.Query()
	ctx := r.Context()
	parse := func(field string) (int64, bool) {
		if !q.Has(field) {
			return 0, true
		}
		n, err := strconv.ParseInt(q.Get(field), 10, 64)
		return n, err == nil && n > 0 && n <= sb.MaxPosts+1
	}
	after, afterOK := parse("after")
	before, beforeOK := parse("before")
	id := q.Get("board")
	if !afterOK || !beforeOK || (after > 0 && before > 0) || (id == "" && (after > 0 || before > 0)) {
		writeSessionBoardError(w, sb.ErrInvalid)
		return
	}
	if id != "" {
		page, err := s.store.SessionBoardPage(ctx, project, actor, id, after, before)
		if err != nil {
			writeSessionBoardError(w, err)
			return
		}
		writeJSON(w, page)
		return
	}
	boards, err := s.store.SessionBoards(ctx, project, actor)
	if err != nil {
		writeSessionBoardError(w, err)
		return
	}
	writeJSON(w, struct {
		Boards []sb.Board `json:"boards"`
	}{boards})
}

func writeSessionBoardError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sb.ErrInvalid):
		writeRefusal(w, http.StatusBadRequest, sb.ErrInvalid.Error(), "Check the message board fields and their limits.")
	case errors.Is(err, sb.ErrMissing):
		writeRefusal(w, http.StatusNotFound, sb.ErrMissing.Error(), "There is no message board with that ID in this scope.")
	case errors.Is(err, sb.ErrDenied):
		writeRefusal(w, http.StatusForbidden, sb.ErrDenied.Error(), "This identity cannot take that action on the message board.")
	case errors.Is(err, sb.ErrClosed):
		writeRefusal(w, http.StatusConflict, sb.ErrClosed.Error(), "This message board is archived.")
	case errors.Is(err, sb.ErrFull):
		writeRefusal(w, http.StatusConflict, sb.ErrFull.Error(), "The message board reached its storage limit.")
	case errors.Is(err, sb.ErrReplay):
		writeRefusal(w, http.StatusConflict, sb.ErrReplay.Error(), "This request key already belongs to different content.")
	case errors.Is(err, store.ErrBusy):
		w.Header().Set("Retry-After", "1")
		writeRefusal(w, http.StatusServiceUnavailable, "store_busy", "The message board store is busy. Retry with the same key.")
	default:
		writeRefusal(w, http.StatusServiceUnavailable, "session_board_unavailable", "The message board store could not answer.")
	}
}
