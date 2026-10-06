package http

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// The Sessions a person archived (docs/session-archive.md): closed through
// the close path to give their memory back, recorded, and resumable again.
//
//	POST /v1/sessions/{id}/archive          {force?}         (actions.go)
//	GET  /v1/sessions/archived
//	POST /v1/sessions/archived/restore      {conversations: [id…]}
//
// The read is read-level, like GET /v1/sessions. The writes need a device
// that may send and an Idempotency-Key: an archive is a close, and a restore
// is one resume per row, and a retried POST must do neither twice.

const (
	archivedPath        = "/v1/sessions/archived"
	archivedRestorePath = archivedPath + "/restore"
)

// newSessionArchive is the record, with the register's limits and the label
// the Session row shows.
func (s *Server) newSessionArchive() *app.SessionArchive {
	if s.store == nil {
		return nil
	}
	return &app.SessionArchive{
		Store:      s.store,
		Title:      s.sessionDisplayLabel,
		RowsLimit:  int(CapacityLimit(capacity.SessionsArchiveRows)),
		BatchLimit: int(CapacityLimit(capacity.SessionsArchiveBatch)),
	}
}

// archivedRoute answers the two fixed paths, and false for any other.
func (s *Server) archivedRoute(w http.ResponseWriter, r *http.Request) bool {
	switch routePath(r) {
	case archivedPath:
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "the archived sessions are read with GET")
			return true
		}
		s.archivedList(w, r)
	case archivedRestorePath:
		if r.Method != http.MethodPost {
			writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "a restore is a POST")
			return true
		}
		s.restoreWriting(w, r, s.restoreArchived)
	default:
		return false
	}
	return true
}

// archivedWire is one row as the console reads it, the project's mark
// computed now from its directory.
func (s *Server) archivedWire(row store.ArchiveRow) contract.ArchivedSession {
	out := contract.ArchivedSession{ConversationID: row.ConversationID, Assistant: row.Assistant,
		Place: row.Place, PlaceLabel: s.placeLabel(row.CWD), CWD: row.CWD, Title: row.Title,
		Persona: restorablePersona(row.Persona), ArchivedAt: row.ArchivedAt.Unix()}
	if s.icons != nil {
		out.Icon = wireIcon(s.icons.For(row.CWD))
	}
	return out
}

func (s *Server) archivedList(w http.ResponseWriter, r *http.Request) {
	if s.archive == nil {
		writeRefusal(w, http.StatusServiceUnavailable, app.ArchiveUnavailable, "This machine keeps no archive record.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	rows, err := s.archive.Archived(ctx)
	if err != nil {
		log.Printf("archive: the archived sessions could not be read: %v", err)
		writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable",
			"This machine could not read its record of archived sessions.")
		return
	}
	out := contract.ArchivedSessions{Sessions: make([]contract.ArchivedSession, 0, len(rows)), At: time.Now().Unix()}
	for _, row := range rows {
		out.Sessions = append(out.Sessions, s.archivedWire(row))
	}
	writeJSON(w, out)
}

// archiveSession is POST /v1/sessions/{id}/archive, inside sessionWrite: the
// close's own refusals come back through writeActionRefusal unchanged.
func (s *Server) archiveSession(w http.ResponseWriter, r *http.Request, id string, raw []byte) {
	var body contract.ArchiveRequest
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "that body is not an archive")
			return
		}
	}
	item, err := s.actions().Find(r.Context(), id)
	if err != nil {
		writeActionRefusal(w, err)
		return
	}
	if item.ConversationID == "" {
		writeRefusal(w, http.StatusConflict, app.ArchiveNoConversation, "That Session has no conversation to archive.")
		return
	}
	if !s.closeEvidence(w, r.Context(), id, body.ExpectedCloseabilityVersion, body.Force) {
		return
	}
	// The whole close ladder, as a close has it.
	ctx, cancel := context.WithTimeout(r.Context(), closeBudget)
	defer cancel()
	_, row, err := s.closeActions(r).Archive(ctx, id, body.Force)
	if err != nil {
		writeActionRefusal(w, err)
		return
	}
	writeJSON(w, contract.ArchiveAnswer{OK: true, ID: id, Action: "archived", Forced: body.Force,
		Archived: s.archivedWire(row)})
}

func (s *Server) restoreArchived(w http.ResponseWriter, raw []byte) {
	if s.archive == nil {
		writeRefusal(w, http.StatusServiceUnavailable, app.ArchiveUnavailable, "This machine keeps no archive record.")
		return
	}
	ids, present, err := conversationsOf(raw)
	if err != nil || !present {
		if err == nil {
			err = errors.New("conversations is required")
		}
		writeConversationsRefusal(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	inv := s.freshReading(ctx)
	reading := startReadingFrom(inv)
	results, err := s.archive.RestoreArchived(ctx, ids, inv, func(ctx context.Context, row store.ArchiveRow) (app.Started, error) {
		return s.resumeConversation(ctx, reading, resumeTarget{verb: "unarchive", cwd: row.CWD, place: row.Place,
			conversation: row.ConversationID, assistant: row.Assistant, persona: row.Persona, recorded: true})
	})
	if errors.Is(err, app.ErrArchiveBatch) {
		writeRawRefusal(w, http.StatusBadRequest, "archive_batch_too_large",
			"One restore may name at most "+strconv.FormatInt(CapacityLimit(capacity.SessionsArchiveBatch), 10)+" conversations.")
		return
	}
	if err != nil {
		log.Printf("archive: restore: %v", err)
		writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable",
			"This machine could not read its record of archived sessions; nothing was opened.")
		return
	}
	out := contract.RestoreArchivedAnswer{Results: []contract.RestoreArchivedResult{}, At: time.Now().Unix()}
	for _, res := range results {
		row := contract.RestoreArchivedResult{ConversationID: res.ConversationID, OK: res.OK,
			Code: contract.RestoreArchivedCode(res.Code), Message: res.Message}
		if res.OK {
			row.ID, row.Backend, row.Attach = res.Started.ID, contract.Backend(res.Started.Backend), res.Started.Attach
		}
		out.Results = append(out.Results, row)
	}
	writeJSON(w, out)
}

// archiveRowsReading is the register's reading of the record.
func (s *Server) archiveRowsReading() capacity.Reading {
	if s.archive == nil {
		return capacity.Reading{Known: true, Note: "no archive record on this server"}
	}
	return capacity.Reading{Known: true, Counters: capacity.Counters{Evicted: s.archive.Dropped()},
		Note: "archived conversations kept; evicted counts the oldest rows dropped past the limit"}
}
