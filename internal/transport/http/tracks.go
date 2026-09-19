package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	boardstore "github.com/sainteye/clawdline-go/internal/adapters/board"
	"github.com/sainteye/clawdline-go/internal/domain/work"
)

// `GET /v1/board/tracks?project=&track=&cursor=`: the old app's cards placed
// on the three tracks of docs/board-redesign.md (看板／Session 待辦／Backlog),
// read-only (§9 step 1, design-decisions T1).
//
// It reads the board through the same reader `/v1/board` uses, the card logs
// beside it, and this daemon's inventory; it writes nothing, keeps nothing but
// the readers' caches, and has no command. The counts always cover every card
// of the selection; the rows come a page at a time.

// TracksPageSize is the fixed page: no byte budget and no size parameter
// (design-decisions D34).
const TracksPageSize = 200

type tracksDeps struct {
	history  *boardstore.History
	presence func(context.Context) (work.Presence, work.PresenceSource)
	now      func() time.Time
}

var tracksByServer sync.Map // *Server -> *tracksDeps

func (s *Server) tracks() *tracksDeps {
	if d, ok := tracksByServer.Load(s); ok {
		return d.(*tracksDeps)
	}
	d, _ := tracksByServer.LoadOrStore(s, &tracksDeps{
		history:  boardstore.OpenHistory(boardstore.HistoryDir(boardstore.LegacyPath())),
		presence: s.tracksPresence,
		now:      time.Now,
	})
	return d.(*tracksDeps)
}

// tracksPresence is this daemon's reading of the running assistants.
//
// A card's spans name a session by its terminal id or by its conversation id,
// so both are collected. The reading counts as complete only when every source
// answered and every assistant in it has a conversation id: an assistant whose
// conversation could not be identified may be the very session a span names,
// and it must not be read as gone.
func (s *Server) tracksPresence(ctx context.Context) (work.Presence, work.PresenceSource) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	inv := s.inventory.Read(ctx)
	p := work.Presence{Complete: inv.Complete, Sessions: map[string]bool{}}
	count := 0
	for _, item := range inv.Sessions {
		if !item.IsAssistant() {
			continue
		}
		count++
		p.Sessions[strings.ToLower(item.ID)] = true
		if item.ConversationID == "" {
			p.Complete = false
			continue
		}
		p.Sessions[strings.ToLower(item.ConversationID)] = true
	}
	return p, work.PresenceSource{From: "inventory", Complete: p.Complete, Sessions: count}
}

var tracksQueryKeys = map[string]bool{"project": true, "track": true, "cursor": true}

func (s *Server) boardTracks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "The tracks are read with GET.")
		return
	}
	values := r.URL.Query()
	for key, v := range values {
		if !tracksQueryKeys[key] || len(v) != 1 || v[0] == "" || len(v[0]) > 200 {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "Unknown board tracks query field.")
			return
		}
	}
	track := work.Track(values.Get("track"))
	if track != "" && track != work.TrackTodo && track != work.TrackBoard && track != work.TrackBacklog {
		writeRefusal(w, http.StatusBadRequest, "invalid_track", "A track is todo, board or backlog.")
		return
	}

	d := s.tracks()
	state, readAt, err := s.board().legacy.Read()
	board := work.BoardSource{Status: "ok"}
	switch {
	case errors.Is(err, boardstore.ErrLegacyAbsent):
		// No Swift board on this machine: that is known, and it is zero cards.
		board.Status, state = "absent", nil
	case errors.Is(err, boardstore.ErrLegacyDisabled):
		// Told not to read it (cutover B1): known, and zero cards read.
		board.Status, state = "disabled", nil
	case err != nil && !boardstore.IsStale(err):
		writeBoardRefusal(w, err, false)
		return
	case err != nil:
		board.Status = "stale"
	}
	if state != nil {
		board.Revision, board.Cards = state.Revision, len(state.Items)
		board.ReadAt = float64(readAt.UnixNano()) / 1e9
	}

	project := values.Get("project")
	selected := []boardstore.StoredItem{}
	if state != nil {
		for _, item := range state.Items {
			if project == "" || item.ProjectID == project {
				selected = append(selected, item)
			}
		}
	}
	if project != "" && len(selected) == 0 && !knownProject(state, project) {
		writeRefusal(w, http.StatusNotFound, "project_not_found", "This board has no such project.")
		return
	}

	ids := make([]string, 0, len(selected))
	for _, item := range selected {
		ids = append(ids, item.ID)
	}
	histories, history := d.history.Read(ids)
	var cards []work.Card
	if state != nil {
		// Progress reads a card's relations across the whole board, so the
		// selection is made after the cards are built from all of it.
		keep := map[string]bool{}
		for _, id := range ids {
			keep[id] = true
		}
		for _, c := range boardstore.TrackCards(state, histories) {
			if keep[c.ID] {
				cards = append(cards, c)
			}
		}
	}
	presence, presenceSource := d.presence(r.Context())

	projection := work.Project(cards, presence, d.now(), work.DefaultStall)
	rows, next, err := projection.Page(track, values.Get("cursor"), TracksPageSize)
	if err != nil {
		writeRefusal(w, http.StatusBadRequest, "invalid_cursor", "That cursor was not written by this route.")
		return
	}
	projection.Rows = rows
	body := work.Tracks{Projection: projection, PageSize: TracksPageSize,
		Sources: work.Sources{Board: board, History: history, Presence: presenceSource}}
	if project != "" {
		body.Project = &project
	}
	if next != "" {
		body.NextCursor = &next
	}
	out, err := json.Marshal(map[string]any{"tracks": body})
	if err != nil {
		writeRefusal(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(out)
}

// knownProject is a project the board names, whether or not it has cards.
func knownProject(state *boardstore.StoredState, id string) bool {
	if state == nil {
		return false
	}
	for _, p := range state.Projects {
		if p.ID == id {
			return true
		}
	}
	return false
}
