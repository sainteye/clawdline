package http

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
	sb "github.com/sainteye/clawdline/internal/domain/sessionboard"
)

const sessionBoardRoute = "/v1/orchestrator/session-boards"
const sessionBoardInboxRoute = sessionBoardRoute + "/inbox"

func (s *Server) sessionBoardActor(ctx context.Context, inv session.Inventory, id string, qualified bool) (sb.Actor, string, bool) {
	var actor sb.Actor
	var cwd string
	if id == "" {
		return actor, "", false
	}
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
		actor, cwd = sb.Actor{ID: actorID, Label: label, Persona: row.Persona}, row.CWD
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
		actor, project, ok = s.sessionBoardActor(ctx, s.freshReading(ctx), q.Get("conversation_id"), false)
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
	var members []sb.Actor
	switch cmd.Action {
	case "create":
		if machine && (cmd.Target != "" || len(cmd.Members) > 0) {
			writeSessionBoardError(w, sb.ErrDenied)
			return
		}
		if !machine {
			inv := s.freshReading(ctx)
			target, project, ok = s.sessionBoardActor(ctx, inv, cmd.Target, true)
			if !ok {
				writeSessionBoardError(w, sb.ErrDenied)
				return
			}
			members, ok = s.resolveSessionBoardMembers(ctx, inv, project, cmd.Members)
			if !ok {
				writeSessionBoardError(w, sb.ErrDenied)
				return
			}
		}
	case "add-members":
		if machine {
			writeSessionBoardError(w, sb.ErrDenied)
			return
		}
		inv := s.freshReading(ctx)
		_, project, ok = s.sessionBoardActor(ctx, inv, cmd.Members[0], true)
		if ok {
			members, ok = s.resolveSessionBoardMembers(ctx, inv, project, cmd.Members)
		}
		if !ok {
			writeSessionBoardError(w, sb.ErrDenied)
			return
		}
	case "transfer":
		var targetProject string
		target, targetProject, ok = s.sessionBoardActor(ctx, s.freshReading(ctx), cmd.Target, true)
		if !ok || (machine && targetProject != project) {
			writeSessionBoardError(w, sb.ErrDenied)
			return
		}
		project = targetProject
	}
	out, err := s.store.ChangeSessionBoard(ctx, project, actor, target, cmd, key, time.Now(), members...)
	if err != nil {
		writeSessionBoardError(w, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) resolveSessionBoardMembers(ctx context.Context, inv session.Inventory, project string, ids []string) ([]sb.Actor, bool) {
	members := make([]sb.Actor, 0, len(ids))
	for _, id := range ids {
		actor, scope, ok := s.sessionBoardActor(ctx, inv, id, true)
		if !ok || scope != project {
			return nil, false
		}
		members = append(members, actor)
	}
	return members, true
}

func validSessionBoardCommand(c sb.Command) bool {
	if len(c.Members) > sb.MaxMembers || (len(c.Members) > 0 && c.Action != "create" && c.Action != "add-members") {
		return false
	}
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
	case "add-members":
		return c.Text == "" && c.ReplyTo == 0 && c.Target == "" && len(c.Members) > 0
	case "archive", "join", "leave":
		return c.Text == "" && c.ReplyTo == 0 && c.Target == ""
	default:
		return false
	}
}

func sessionBoardCursor(q url.Values, field string) (int64, bool) {
	if !q.Has(field) {
		return 0, true
	}
	n, err := strconv.ParseInt(q.Get(field), 10, 64)
	return n, err == nil && n > 0
}

func (s *Server) sessionBoardInbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET to read a Session message board inbox.")
		return
	}
	if s.store == nil {
		writeSessionBoardError(w, nil)
		return
	}
	q := r.URL.Query()
	for key, values := range q {
		if len(values) != 1 || !slices.Contains([]string{"board", "after", "before", "conversation_id", "member"}, key) {
			writeSessionBoardError(w, sb.ErrInvalid)
			return
		}
	}
	id := q.Get("board")
	after, afterOK := sessionBoardCursor(q, "after")
	before, beforeOK := sessionBoardCursor(q, "before")
	if !sb.ValidText(id, sha256.Size*2) || !afterOK || !beforeOK || (after > 0 && before > 0) {
		writeSessionBoardError(w, sb.ErrInvalid)
		return
	}
	ctx := r.Context()
	member, project := q.Get("member"), ""
	if machineAuthed(r) {
		if q.Has("member") {
			writeSessionBoardError(w, sb.ErrDenied)
			return
		}
		actor, scope, ok := s.sessionBoardActor(ctx, s.freshReading(ctx), q.Get("conversation_id"), false)
		if !ok {
			writeSessionBoardError(w, sb.ErrDenied)
			return
		}
		member, project = actor.ID, scope
	} else if q.Has("conversation_id") || !sb.ValidText(member, sb.MaxKey) {
		writeSessionBoardError(w, sb.ErrDenied)
		return
	}
	inbox, err := s.store.SessionBoardInbox(ctx, project, member, id, after, before)
	if err != nil {
		writeSessionBoardError(w, err)
		return
	}
	s.projectSessionBoardNames(false, nil, inbox.Posts)
	writeJSON(w, inbox)
}

func (s *Server) readSessionBoards(w http.ResponseWriter, r *http.Request, project, actor string) {
	q := r.URL.Query()
	ctx := r.Context()
	after, afterOK := sessionBoardCursor(q, "after")
	before, beforeOK := sessionBoardCursor(q, "before")
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
		board := []sb.Board{page.Board}
		s.projectSessionBoardNames(actor == "", board, page.Posts)
		page.Board = board[0]
		writeJSON(w, page)
		return
	}
	boards, err := s.store.SessionBoards(ctx, project, actor)
	if err != nil {
		writeSessionBoardError(w, err)
		return
	}
	s.projectSessionBoardNames(actor == "", boards, nil)
	writeJSON(w, struct {
		Boards []sb.Board `json:"boards"`
	}{boards})
}

// Project display names from one shared list snapshot. Only the paired roster
// read that already needed presence can request a list build. Inbox and native
// reads use the published snapshot, when present, without waiting for a build.
func (s *Server) projectSessionBoardNames(presence bool, boards []sb.Board, posts []sb.Post) {
	hasMembers := slices.ContainsFunc(boards, func(board sb.Board) bool { return len(board.Members) > 0 })
	if !hasMembers && len(posts) == 0 {
		return
	}
	lists := s.lists()
	var snapshot sessionsSnapshotWire
	if presence && hasMembers {
		snapshot = lists.snapshot()
		setSessionBoardPresence(snapshot, boards)
	} else {
		snapshot, _ = lists.sessions.peek()
	}
	labels := make(map[string]string, len(snapshot.Sessions))
	for _, row := range snapshot.Sessions {
		if row.Assistant == "" || row.SessionID == "" {
			continue
		}
		id := string(row.Assistant) + ":" + row.SessionID
		_, duplicate := labels[id]
		labels[id] = ""
		if !duplicate && row.Source != nil && row.Source.Freshness == contract.SourceFreshnessCurrent {
			labels[id] = row.Label
		}
	}
	apply := func(actor *sb.Actor) {
		if label := labels[actor.ID]; label != "" {
			actor.Label = label
		}
	}
	for _, board := range boards {
		for i := range board.Members {
			apply(&board.Members[i])
		}
	}
	for i := range posts {
		apply(&posts[i].Author)
	}
}

func setSessionBoardPresence(snapshot sessionsSnapshotWire, boards []sb.Board) {
	complete := cloudListComplete(snapshot.Scan)
	present := make(map[string]sb.Presence, len(snapshot.Sessions))
	unbound := make(map[string]bool)
	for _, row := range snapshot.Sessions {
		if row.Assistant == "" {
			continue
		}
		assistant := string(row.Assistant)
		current := row.Source != nil && row.Source.Freshness == contract.SourceFreshnessCurrent
		if row.SessionID == "" {
			// An observed new Session with no conversation record cannot be an enrolled identity.
			if !current || row.Identity != contract.IdentityBindingNoRecord {
				unbound[assistant] = true
			}
			continue
		}
		id := assistant + ":" + row.SessionID
		if current {
			present[id] = sb.Listed
		} else if _, known := present[id]; !known {
			present[id] = ""
		}
	}
	for _, board := range boards {
		for i := range board.Members {
			member := &board.Members[i]
			presence, found := present[member.ID]
			assistant, _, _ := strings.Cut(member.ID, ":")
			if !found && complete && !unbound[assistant] {
				presence = sb.NotListed
			}
			member.Presence = presence
		}
	}
}

// Use the scheduler cadence and the authoritative Session list producer. Two
// distinct complete readings keep a brief terminal transition from archiving
// a board. An intervening inconclusive reading clears the previous evidence.
func (s *Server) maintainSessionBoardActivity(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	var previous map[string]sb.Board
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			next, err := s.checkInactiveSessionBoards(ctx, previous, now)
			if err != nil && ctx.Err() == nil {
				log.Printf("session boards: pre-archive activity check failed: %v", err)
			}
			previous = next
		}
	}
}

func (s *Server) checkInactiveSessionBoards(ctx context.Context, previous map[string]sb.Board, now time.Time) (map[string]sb.Board, error) {
	boards, err := s.store.SessionBoards(ctx, "", "")
	if err != nil {
		return nil, err
	}
	boards = slices.DeleteFunc(boards, func(b sb.Board) bool { return b.ArchivedAt > 0 })
	if len(boards) == 0 {
		return nil, nil
	}
	snapshot, err := s.lists().sessions.since(time.Now())
	if err != nil || ctx.Err() != nil {
		return nil, errors.Join(err, ctx.Err())
	}
	return s.archiveInactiveSessionBoards(ctx, previous, boards, snapshot, now)
}

func (s *Server) archiveInactiveSessionBoards(ctx context.Context, previous map[string]sb.Board, boards []sb.Board, snapshot sessionsSnapshotWire, now time.Time) (map[string]sb.Board, error) {
	// A disabled or unreadable source can still hold a board member. Only
	// the complete inventory authorizes an archive.
	if !snapshot.Scan.Complete {
		return nil, nil
	}
	setSessionBoardPresence(snapshot, boards)
	absent := make(map[string]sb.Board)
	var failures error
	for _, board := range boards {
		if slices.ContainsFunc(board.Members, func(member sb.Actor) bool { return member.Presence != sb.NotListed }) {
			continue
		}
		absent[board.ID] = board
		if before, ok := previous[board.ID]; ok && before.Latest == board.Latest && sb.SameMembers(before.Members, board.Members) {
			failures = errors.Join(failures, s.store.ArchiveInactiveSessionBoard(ctx, board, now))
		}
	}
	return absent, failures
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
	case errors.Is(err, sb.ErrPostExpired):
		writeRefusal(w, http.StatusConflict, sb.ErrPostExpired.Error(), "The post for this request is outside the message board’s retention window.")
	case errors.Is(err, sb.ErrReplay):
		writeRefusal(w, http.StatusConflict, sb.ErrReplay.Error(), "This request key already belongs to different content.")
	case errors.Is(err, store.ErrBusy):
		w.Header().Set("Retry-After", "1")
		writeRefusal(w, http.StatusServiceUnavailable, "store_busy", "The message board store is busy. Retry with the same key.")
	default:
		writeRefusal(w, http.StatusServiceUnavailable, "session_board_unavailable", "The message board store could not answer.")
	}
}
