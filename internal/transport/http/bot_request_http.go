package http

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	br "github.com/sainteye/clawdline/internal/domain/botrequest"
)

// Bot request cards (docs/bot-request-cards.md §8.3), the person's side and
// the console's read:
//
//	GET  /v1/bot-request/cards?bot=&project=&state=&cursor=   most recently updated first
//	GET  /v1/bot-request/cards/<id>                           one card, its history and progress
//	POST /v1/bot-request/cards/<id>/handoff                   {"expected_version","to_bot","role","note"}
//	POST /v1/bot-request/cards/<id>/check                     {"expected_version","entries":[{"label","state","note"}]}
//	POST /v1/bot-request/cards/<id>/complete|cancel|reopen    {"expected_version","reason"}
//
// Every POST is the person's, from a device that may send, and carries an
// Idempotency-Key: a retry answers the card as it stands and writes nothing.
// Refusals carry the card's typed code (bot_request_*) and, on a version
// conflict, the current version. The Bots' own routes are not here yet.

// BotRequestView is one card as the console reads it.
type BotRequestView struct {
	br.Card
	Done          int                     `json:"done"`
	Total         int                     `json:"total"`
	ProgressState string                  `json:"progress_state"`
	Events        []store.BotRequestEvent `json:"events,omitempty"`
}

// BotRequestPage is one page of cards.
type BotRequestPage struct {
	Cards      []BotRequestView `json:"cards"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

func botRequestView(c br.Card, events []store.BotRequestEvent) BotRequestView {
	done, total := c.Progress()
	if c.Checklist == nil {
		c.Checklist = []br.Entry{}
	}
	return BotRequestView{Card: c, Done: done, Total: total, ProgressState: c.ProgressState(), Events: events}
}

func (s *Server) botRequestRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(routePath(r), "/v1/bot-request/"), "/")
	parts := strings.Split(rest, "/")
	switch {
	case rest == "cards" && r.Method == http.MethodGet:
		s.botRequestList(w, r)
	case len(parts) == 2 && parts[0] == "cards" && r.Method == http.MethodGet:
		s.botRequestRead(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "cards" && r.Method == http.MethodPost:
		s.botRequestPersonWrite(w, r, parts[1], parts[2])
	default:
		writeRefusal(w, http.StatusNotFound, "not_found", "No such Bot request card route.")
	}
}

func (s *Server) botRequestList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state := q.Get("state")
	switch state {
	case "", br.StateOpen, br.StateDone, br.StateCancelled, br.StateMerged:
	default:
		writeRawRefusal(w, http.StatusBadRequest, br.CodeInvalid, "state")
		return
	}
	cards, next, err := s.store.BotRequestCards(r.Context(), store.BotRequestFilter{Bot: q.Get("bot"), ProjectID: q.Get("project"), State: state}, q.Get("cursor"), br.MaxPageRows)
	if err != nil {
		s.writeBotRequestError(w, err)
		return
	}
	page := BotRequestPage{Cards: make([]BotRequestView, 0, len(cards)), NextCursor: next}
	for _, c := range cards {
		page.Cards = append(page.Cards, botRequestView(c, nil))
	}
	writeJSON(w, page)
}

func (s *Server) botRequestRead(w http.ResponseWriter, r *http.Request, id string) {
	c, err := s.store.BotRequestCard(r.Context(), id)
	if err != nil {
		s.writeBotRequestError(w, err)
		return
	}
	events, err := s.store.BotRequestEvents(r.Context(), id)
	if err != nil {
		s.writeBotRequestError(w, err)
		return
	}
	writeJSON(w, botRequestView(c, events))
}

type botRequestPersonBody struct {
	ExpectedVersion int64     `json:"expected_version"`
	ToBot           string    `json:"to_bot,omitempty"`
	Role            string    `json:"role,omitempty"`
	Note            string    `json:"note,omitempty"`
	Reason          string    `json:"reason,omitempty"`
	Entries         []br.Mark `json:"entries,omitempty"`
}

func (s *Server) botRequestPersonWrite(w http.ResponseWriter, r *http.Request, id, action string) {
	if _, ok := requirePersonWorkV2(w, r); !ok {
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" || len(key) > 200 {
		writeRawRefusal(w, http.StatusBadRequest, br.CodeInvalid, "idempotency_key")
		return
	}
	var body botRequestPersonBody
	if _, ok := readWorkV2Body(w, r, &body); !ok {
		return
	}
	if body.ExpectedVersion <= 0 {
		writeRawRefusal(w, http.StatusBadRequest, br.CodeInvalid, "expected_version")
		return
	}
	if len(body.Reason) > br.MaxNoteBytes {
		writeRawRefusal(w, http.StatusRequestEntityTooLarge, br.CodeFull, "reason")
		return
	}
	person := br.Actor{Person: true}
	now := time.Now()
	var apply func(*br.Card) ([]br.Event, error)
	switch action {
	case "handoff":
		apply = func(c *br.Card) ([]br.Event, error) {
			if err := c.Handoff(person, body.ToBot, body.Role, body.Note); err != nil {
				return nil, err
			}
			return []br.Event{{Kind: br.EventHanded, Detail: body.Note}}, nil
		}
	case "check":
		apply = func(c *br.Card) ([]br.Event, error) {
			changed, err := c.Check(person, body.Entries, now)
			if err != nil || !changed {
				return nil, err
			}
			return []br.Event{{Kind: br.EventChecklistMark}}, nil
		}
	case "complete", "cancel", "reopen":
		kind := map[string]string{"complete": br.EventDone, "cancel": br.EventCancelled, "reopen": br.EventReopened}[action]
		apply = func(c *br.Card) ([]br.Event, error) {
			var changed bool
			var err error
			switch action {
			case "complete":
				changed, err = c.Done(person, now)
			case "cancel":
				changed, err = c.Cancel(person, now)
			default:
				changed, err = c.Reopen(person)
			}
			if err != nil || !changed {
				return nil, err
			}
			return []br.Event{{Kind: kind, Detail: body.Reason}}, nil
		}
	default:
		writeRefusal(w, http.StatusNotFound, "not_found", "No such Bot request card route.")
		return
	}
	res, err := s.store.UpdateBotRequest(r.Context(), store.BotRequestWrite{
		CardID: id, Expected: body.ExpectedVersion, Actor: person, Key: "http/" + key, Now: now, Apply: apply,
	})
	if err != nil {
		s.writeBotRequestError(w, err)
		return
	}
	events, err := s.store.BotRequestEvents(r.Context(), id)
	if err != nil {
		s.writeBotRequestError(w, err)
		return
	}
	writeJSON(w, struct {
		Card     BotRequestView `json:"card"`
		Changed  bool           `json:"changed"`
		Replayed bool           `json:"replayed,omitempty"`
	}{botRequestView(res.Card, events), res.Changed, res.Replayed})
}

// botRequestRefusal is a card refusal on the wire: the shared refusal shape
// plus the card's current version on a conflict.
type botRequestRefusal struct {
	Error   string `json:"error"`
	Detail  string `json:"detail"`
	Version int64  `json:"version,omitempty"`
}

func (s *Server) writeBotRequestError(w http.ResponseWriter, err error) {
	var ref *br.Refusal
	switch {
	case errors.As(err, &ref):
		status := http.StatusBadRequest
		switch ref.Code {
		case br.CodeForbidden, br.CodeNoAuthority, br.CodeHandoffScope:
			status = http.StatusForbidden
		case br.CodeVersionConflict, br.CodeChecklistOpen, br.CodeClosed, br.CodeAlreadyMerged:
			status = http.StatusConflict
		case br.CodeFull:
			status = http.StatusTooManyRequests
		case br.CodeNotFound:
			status = http.StatusNotFound
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		writeJSON(w, botRequestRefusal{Error: ref.Code, Detail: ref.Field, Version: ref.Version})
	case errors.Is(err, store.ErrNoBotRequest):
		writeRawRefusal(w, http.StatusNotFound, br.CodeNotFound, "card")
	default:
		// Could not check: never a success-shaped answer.
		writeRawRefusal(w, http.StatusServiceUnavailable, "bot_request_unavailable", err.Error())
	}
}
