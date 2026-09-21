package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	boardstore "github.com/sainteye/clawdline/internal/adapters/board"
	"github.com/sainteye/clawdline/internal/domain/auth"
	domainboard "github.com/sainteye/clawdline/internal/domain/board"
)

// The board: `GET /v1/board` in the Swift app's envelope, and `POST /v1/board`
// for the two board-level commands the settings page sends.
//
// What is and is not here follows docs/board-design.md: the wire, the card
// allow-list and the progress rules are carried over; item writes are refused
// by name, because where an item is stored is a decision the design document
// leaves open. Cards from the Swift app are read, never written (plan.md §4).

// boardDeps is per server, and lives here rather than on the Server struct so
// that the board's wiring is one file.
type boardDeps struct {
	legacy   *boardstore.Legacy
	settings *boardstore.Settings
}

var boardByServer sync.Map // *Server -> *boardDeps

func (s *Server) board() *boardDeps {
	if d, ok := boardByServer.Load(s); ok {
		return d.(*boardDeps)
	}
	// settings is the old document, read once to carry it over (D37).
	d, _ := boardByServer.LoadOrStore(s, &boardDeps{
		legacy:   boardstore.OpenLegacy(boardstore.LegacyPath()),
		settings: boardstore.OpenSettings(s.cfg.Dir),
	})
	return d.(*boardDeps)
}

// boardViewer is `ProjectBoardHTTP.viewer`: the machine may do everything; a
// device may write when it may send, and manage when it may also administer.
func boardViewer(r *http.Request) boardstore.Viewer {
	a := accessOf(r)
	v := boardstore.Viewer{ID: "machine", CanWrite: a.machine, CanManage: a.machine,
		NarrativeProvider: "codex"}
	if a.verdict.Allowed {
		if !a.machine {
			v.ID = a.verdict.Device
		}
		v.CanWrite = v.CanWrite || a.verdict.Caps.Has(auth.Send)
		v.CanManage = (v.CanManage || a.verdict.Caps.Has(auth.Admin)) && v.CanWrite
	}
	return v
}

var boardQueryKeys = map[string]bool{"project": true, "item": true, "report": true,
	"audience": true, "cursor": true, "limit": true}

// boardRead answers the single route. The selector grammar is the Swift app's,
// including its refusals, because the console sends exactly that grammar.
func (s *Server) boardRead(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	for key, v := range values {
		if !boardQueryKeys[key] || len(v) != 1 || v[0] == "" || len(v[0]) > 200 {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "Unknown board query field.")
			return
		}
	}
	q := boardstore.Query{Project: values.Get("project"), Item: values.Get("item")}
	if values.Get("report") != "" || strings.HasPrefix(q.Item, "collection:") ||
		strings.HasPrefix(q.Item, "catalog:") || strings.HasPrefix(q.Item, "session:") ||
		strings.HasPrefix(q.Item, "report:") {
		// The continuation selectors read an item's detail collections, which
		// this daemon does not project yet. Refused by name, not answered empty.
		writeRefusal(w, http.StatusNotImplemented, "board_selector_not_implemented",
			"This daemon answers the catalog, a project and an item; report, collection, catalog-search and session selectors are not implemented yet.")
		return
	}
	if audience := values.Get("audience"); audience != "" {
		switch audience {
		case "human", "agent", "archive", "all":
		default:
			writeRefusal(w, http.StatusBadRequest, "invalid_audience_selector",
				"An audience selection needs one Project, a known audience, and no item or report.")
			return
		}
		if q.Project == "" || q.Item != "" {
			writeRefusal(w, http.StatusBadRequest, "invalid_audience_selector",
				"An audience selection needs one Project, a known audience, and no item or report.")
			return
		}
		q.Audience = audience
		q.Cursor = clampInt(values.Get("cursor"), 0, 0, 100_000)
		q.Limit = clampInt(values.Get("limit"), boardstore.DefaultPageLimit, 1, boardstore.MaximumPageLimit)
	}

	d := s.board()
	settings, err := s.boardSettings(r.Context())
	if err != nil {
		writeBoardRefusal(w, err, false)
		return
	}
	legacy, legacyAt, legacyErr := d.legacy.Read()

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	presence, paths := s.boardPresence(ctx)

	env, status := boardstore.Build(boardstore.Inputs{
		Legacy: legacy, LegacyAt: legacyAt, LegacyErr: legacyErr,
		Settings: settings, Viewer: boardViewer(r), Presence: presence,
		ProjectPath: func(id string) string { return paths[id] },
		ProjectIcon: func(path string) any { return wireIcon(s.icons.For(path)) },
	}, q)
	writeBoardEnvelope(w, env, status, false)
}

// boardPresence is the live inventory, keyed on the conversation id the board
// joins on, and the paths the running sessions name, keyed on project id.
func (s *Server) boardPresence(ctx context.Context) (boardstore.Presence, map[string]string) {
	inv := s.reading(ctx)
	presence := boardstore.Presence{Complete: inv.Complete, Fresh: true,
		ObservedAt: inv.ObservedAt, ByConversation: map[string][]boardstore.PresenceRow{}}
	paths := map[string]string{}
	for _, item := range inv.Sessions {
		if !item.IsAssistant() {
			continue
		}
		if item.CWD != "" {
			paths[domainboard.ID("project", item.CWD)] = item.CWD
		}
		if item.ConversationID == "" {
			continue
		}
		id := strings.ToLower(item.ConversationID)
		presence.ByConversation[id] = append(presence.ByConversation[id], boardstore.PresenceRow{
			TerminalID: item.ID, Title: item.Label, Provider: string(item.Assistant),
			State: string(item.State),
		})
	}
	return presence, paths
}

// boardWrite applies one command.
func (s *Server) boardWrite(w http.ResponseWriter, r *http.Request) {
	viewer := boardViewer(r)
	raw, err := io.ReadAll(io.LimitReader(r.Body, boardstore.MaximumCommandBytes+1))
	if err != nil || len(raw) > boardstore.MaximumCommandBytes {
		writeRefusal(w, http.StatusRequestEntityTooLarge, "board_command_too_large",
			"A board command is at most 64 KiB.")
		return
	}
	var c boardstore.Command
	if err := json.Unmarshal(raw, &c); err != nil {
		writeRefusal(w, http.StatusBadRequest, "invalid_command", "That body is not a board command.")
		return
	}
	if !viewer.CanWrite {
		writeRefusal(w, http.StatusForbidden, "forbidden", "This device may only read the board.")
		return
	}
	if boardstore.ManageOperations[c.Operation] && !viewer.CanManage {
		writeRefusal(w, http.StatusForbidden, "forbidden",
			"Changing board mode requires an administrative device.")
		return
	}

	d := s.board()
	outcome, answered := s.boardApply(w, r, viewer.ID, c, raw)
	if answered {
		return
	}

	// The answer is the board as it now stands, plus a small receipt that
	// cannot be mistaken for a stale read.
	settings, err := s.boardSettings(r.Context())
	if err != nil {
		writeBoardRefusal(w, err, true)
		return
	}
	legacy, legacyAt, legacyErr := d.legacy.Read()
	env, _ := boardstore.Build(boardstore.Inputs{
		Legacy: legacy, LegacyAt: legacyAt, LegacyErr: legacyErr,
		Settings: settings, Viewer: viewer,
	}, boardstore.Query{})
	env.Projects = []boardstore.ProjectRow{}
	body := map[string]any{
		"board": env, "ok": true,
		"result": map[string]any{"operation": c.Operation, "requestId": c.RequestID,
			"itemId": nil, "revision": outcome.Revision, "replayed": outcome.Replayed},
	}
	writeJSON(w, body)
}

func writeBoardEnvelope(w http.ResponseWriter, env boardstore.Envelope, status int, applied bool) {
	body, err := json.Marshal(map[string]any{"board": env})
	if err != nil {
		writeRefusal(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if len(body) > boardstore.MaximumResponseBytes {
		message := "Select a narrower board item; the response exceeds its byte budget."
		if applied {
			message = "The command was saved. " + message
		}
		writeRefusal(w, http.StatusServiceUnavailable, "board_response_too_large", message)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// writeBoardRefusal answers a typed board refusal with its own status. When a
// command was already saved, the refusal says so: a projection failure must not
// masquerade as a command that did not happen.
func writeBoardRefusal(w http.ResponseWriter, err error, applied bool) {
	var refusal boardstore.Refusal
	if errors.As(err, &refusal) {
		message := refusal.Message
		if applied {
			message = "The command was saved, but its view is unavailable: " + message
		}
		writeRefusal(w, refusal.Status, refusal.Code, message)
		return
	}
	writeRefusal(w, http.StatusServiceUnavailable, "board_unavailable", err.Error())
}

func clampInt(raw string, fallback, low, high int) int {
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	if n < low {
		return low
	}
	if n > high {
		return high
	}
	return n
}
