package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/bootid"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// The sessions a reboot took away (docs/session-restore.md): the record kept
// on every complete reading, and the three routes the console offers them
// back through.
//
//	GET  /v1/sessions/restorable
//	POST /v1/sessions/restorable/restore   {conversations: [id…]}
//	POST /v1/sessions/restorable/dismiss   {conversations?: [id…]}
//
// The read is read-level, like GET /v1/sessions. The two writes need a device
// that may send and an Idempotency-Key, as a resume does, because a restore is
// one resume per row and a retried POST must not open a second tab.

const (
	restorablePath = "/v1/sessions/restorable"
	restorePath    = restorablePath + "/restore"
	dismissPath    = restorablePath + "/dismiss"
)

// newSessionRestore is the recorder, with the register's limits and this
// server's own answers to "is this a broker child" and "what is it called".
func (s *Server) newSessionRestore() *app.SessionRestore {
	return &app.SessionRestore{
		Store:      s.store,
		Boot:       bootid.Read,
		Children:   s.restoreChildren,
		Title:      s.restoreTitle,
		RowsLimit:  int(CapacityLimit(capacity.SessionsRestoreRows)),
		BootsLimit: int(CapacityLimit(capacity.SessionsRestoreBoots)),
		BatchLimit: int(CapacityLimit(capacity.SessionsRestoreBatch)),
		SeenEvery:  time.Duration(CapacityLimit(capacity.SessionsRestoreSeenAge)) * time.Second,
	}
}

// restoreChildren is which rows are the broker's own children: a task names
// the row's terminal as its child's, and the process in it is that task's
// (ownChild — the same rule the task list uses, so a terminal id tmux has
// since handed to an unrelated pane is not mistaken for one).
func (s *Server) restoreChildren(ctx context.Context, rows []session.Session) map[string]bool {
	out := map[string]bool{}
	if s.broker == nil {
		return out
	}
	records, _, err := s.broker.Records(ctx)
	if err != nil {
		// Unknown is not "none": with no way to tell, every row a task could
		// have opened is left out rather than risk offering a child back.
		for _, row := range rows {
			out[row.ID] = true
		}
		return out
	}
	byTerminal := map[string]session.Session{}
	for _, row := range rows {
		byTerminal[row.ID] = row
	}
	for _, rec := range records {
		row, ok := byTerminal[rec.ChildTerminalID]
		if rec.ChildTerminalID == "" || !ok {
			continue
		}
		if ownChild(rec, liveOf(row)) {
			out[row.ID] = true
		}
	}
	return out
}

// restoreTitle is the manual title the list would show for this row, when
// there is one. The recorder falls back to the session's own name.
func (s *Server) restoreTitle(_ context.Context, row session.Session) string {
	view := s.withOwnSessionTitles(s.swift.Read(), time.Now())
	return view.TitleOf(liveOf(row), row.CustomTitle, nil).Manual
}

// restorableRoute answers the three routes, and false for any other path.
func (s *Server) restorableRoute(w http.ResponseWriter, r *http.Request) bool {
	switch routePath(r) {
	case restorablePath:
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "the restorable sessions are read with GET")
			return true
		}
		s.restorableList(w, r)
	case restorePath:
		if r.Method != http.MethodPost {
			writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "a restore is a POST")
			return true
		}
		s.restoreWriting(w, r, s.restoreSessions)
	case dismissPath:
		if r.Method != http.MethodPost {
			writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "a dismissal is a POST")
			return true
		}
		s.restoreWriting(w, r, s.dismissRestorable)
	default:
		return false
	}
	return true
}

func (s *Server) restorableList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	offer, err := s.restore.Restorable(ctx, s.reading(ctx))
	if err != nil {
		log.Printf("restore: the restorable sessions could not be read: %v", err)
		writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable",
			"This machine could not read its record of the sessions that were open.")
		return
	}
	out := contract.RestorableSessions{Available: offer.Available, Sessions: []contract.RestorableSession{},
		At: time.Now().Unix()}
	if offer.Reason != "" {
		out.Reason = contract.RestorableReason(offer.Reason)
	}
	if offer.Previous.ID != "" {
		out.PreviousBootLastSeen = offer.Previous.LastSeen.Unix()
	}
	for _, row := range offer.Rows {
		out.Sessions = append(out.Sessions, contract.RestorableSession{
			ConversationID: row.ConversationID, Assistant: row.Assistant, Place: row.Place,
			PlaceLabel: s.placeLabel(row.CWD), CWD: row.CWD, Title: row.Title, LastSeen: row.LastSeen.Unix(),
		})
	}
	writeJSON(w, out)
}

func (s *Server) placeLabel(cwd string) string {
	if s.icons != nil {
		if label := s.icons.Label(cwd); label != "" {
			return label
		}
	}
	return filepath.Base(cwd)
}

// restoreWriting is `writing` with the body in the request's digest: the same
// key for a different list of conversations is a different request, and it is
// refused rather than answered with the first list's results (D03).
func (s *Server) restoreWriting(w http.ResponseWriter, r *http.Request, body func(http.ResponseWriter, []byte)) {
	if !maySend(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "This device may read, and not send.")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeAuthRefusal(w, http.StatusBadRequest, "bad_request", "That needs an Idempotency-Key header.")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
	if err != nil {
		writeRefusal(w, http.StatusRequestEntityTooLarge, "body_too_large", "That request is larger than this route reads.")
		return
	}
	k := store.ReceiptKey{Scope: scopePlaces, Actor: accessOf(r).verdict.Device, Key: key}
	s.receipted(w, r, k, requestDigest([]byte(r.Method), []byte(routePath(r)), raw),
		// A full store is a fact about this moment, not about the request.
		func(status int) bool { return status != http.StatusServiceUnavailable },
		func(w http.ResponseWriter) { body(w, raw) })
	log.Printf("remote: %s %s by %s", r.Method, r.URL.Path, accessOf(r).verdict.Device)
}

// conversationsOf reads `{conversations: [...]}`. present is false when the
// key is absent, which dismiss reads as "all"; an unknown key is refused.
func conversationsOf(raw []byte) (ids []string, present bool, err error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, false, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, false, errors.New("the body is not a JSON object")
	}
	for name := range fields {
		if name != "conversations" {
			return nil, false, errors.New("unknown field " + name)
		}
	}
	list, ok := fields["conversations"]
	if !ok {
		return nil, false, nil
	}
	if err := json.Unmarshal(list, &ids); err != nil || ids == nil {
		return nil, false, errors.New("conversations must be a list of ids")
	}
	return ids, true, nil
}

func (s *Server) restoreSessions(w http.ResponseWriter, raw []byte) {
	ids, present, err := conversationsOf(raw)
	if err != nil || !present {
		if err == nil {
			err = errors.New("conversations is required")
		}
		writeRefusal(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	inv := s.freshReading(ctx)
	reading := startReadingFrom(inv)
	results, err := s.restore.Restore(ctx, ids, inv, func(ctx context.Context, row store.RestoreRow) (app.Started, error) {
		return s.resumeRestorable(ctx, reading, row)
	})
	if errors.Is(err, app.ErrRestoreBatch) {
		writeRefusal(w, http.StatusBadRequest, "restore_batch_too_large",
			"One restore may name at most "+strconv.FormatInt(CapacityLimit(capacity.SessionsRestoreBatch), 10)+" conversations.")
		return
	}
	if err != nil {
		log.Printf("restore: %v", err)
		writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable",
			"This machine could not read its record of the sessions that were open; nothing was opened.")
		return
	}
	out := contract.RestoreSessionsAnswer{Results: []contract.RestoreResult{}, At: time.Now().Unix()}
	for _, res := range results {
		row := contract.RestoreResult{ConversationID: res.ConversationID, OK: res.OK,
			Code: contract.RestoreCode(res.Code), Message: res.Message}
		if res.OK {
			row.ID, row.Backend, row.Attach = res.Started.ID, contract.Backend(res.Started.Backend), res.Started.Attach
		}
		out.Results = append(out.Results, row)
	}
	writeJSON(w, out)
}

// resumeRestorable is one row through the same opening queue and the same
// Starter.Resume a person's resume goes through. The row's directory is added
// to the places the starter may resolve, as a live one is: after a reboot it is
// not live, and it was a place this machine had a session in.
func (s *Server) resumeRestorable(ctx context.Context, reading startReading, row store.RestoreRow) (app.Started, error) {
	release, ok := tryOpening(ctx)
	if !ok {
		return app.Started{}, app.ErrRestoreOverCapacity
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	reading.live = append(append([]string(nil), reading.live...), row.CWD)
	starter := s.starter(reading)
	place, ok := starter.Place(ctx, row.Place)
	if !ok {
		log.Printf("audit place.restore place=%.64s ok=0 why=place_unavailable", row.Place)
		return app.Started{}, app.ErrRestorePlaceUnavailable
	}
	made, err := starter.Resume(ctx, place, row.ConversationID, row.Assistant)
	if err != nil {
		log.Printf("audit place.restore place=%s cwd=%q assistant=%s session=%.64s ok=0 why=%v",
			place.ID, place.Path, row.Assistant, row.ConversationID, err)
		return app.Started{}, err
	}
	log.Printf("audit place.restore place=%s cwd=%q assistant=%s session=%s ok=1 id=%s",
		place.ID, place.Path, row.Assistant, row.ConversationID, made.ID)
	return made, nil
}

func (s *Server) dismissRestorable(w http.ResponseWriter, raw []byte) {
	ids, present, err := conversationsOf(raw)
	if err != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if !present {
		ids = nil
	} else if ids == nil {
		ids = []string{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	n, available, err := s.restore.Dismiss(ctx, ids)
	if err != nil {
		log.Printf("restore: dismiss: %v", err)
		writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable",
			"This machine could not write its record of the sessions that were open.")
		return
	}
	if !available {
		writeRefusal(w, http.StatusConflict, app.RestoreBootUnknown,
			"This machine cannot read its boot id, so it keeps no record of sessions to dismiss.")
		return
	}
	writeJSON(w, contract.DismissRestorableAnswer{OK: true, Dismissed: n, At: time.Now().Unix()})
}

// tryOpening is admitOpening for a row of a batch: a full queue is that row's
// answer (over_capacity), not the whole request's.
func tryOpening(ctx context.Context) (func(), bool) {
	select {
	case openQueue <- struct{}{}:
	default:
		return nil, false
	}
	select {
	case opening <- struct{}{}:
		return func() { <-opening; <-openQueue }, true
	case <-ctx.Done():
		<-openQueue
		return nil, false
	}
}

// restoreRowsReading is the register's reading of the recorder.
func (s *Server) restoreRowsReading() capacity.Reading {
	if s.restore == nil {
		return capacity.Reading{Known: false, Note: "the recorder is not running"}
	}
	return capacity.Reading{Known: true, Counters: capacity.Counters{Evicted: s.restore.Dropped()},
		Note: "conversations recorded per boot; evicted counts the ones left unrecorded past the limit"}
}
