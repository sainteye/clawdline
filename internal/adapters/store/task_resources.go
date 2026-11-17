package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

const taskResourceSchema = `
CREATE TABLE IF NOT EXISTS broker_task_resources (
 id TEXT PRIMARY KEY,
 generation TEXT NOT NULL UNIQUE,
 owner_kind TEXT NOT NULL,
 owner_id TEXT NOT NULL,
 class TEXT NOT NULL,
 relative_path TEXT NOT NULL UNIQUE,
 state TEXT NOT NULL,
 record TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS broker_task_resources_owner ON broker_task_resources(owner_kind,owner_id);
CREATE INDEX IF NOT EXISTS broker_task_resources_state ON broker_task_resources(state,id);
`

var ErrResourceConflict = errors.New("resource_identity_conflict")

func validResource(r taskresource.Resource) bool {
	id, err := uuid.Parse(r.ID)
	gen, gerr := uuid.Parse(r.Generation)
	_, oerr := uuid.Parse(r.Owner.ID)
	class := r.Class == taskresource.Cache || r.Class == taskresource.Scratch || r.Class == taskresource.Log || r.Class == taskresource.Artifact || r.Class == taskresource.Evidence
	return err == nil && gerr == nil && oerr == nil && id.String() == r.ID && gen.String() == r.Generation &&
		(r.Owner.Kind == "task" || r.Owner.Kind == "root") && filepath.IsAbs(r.Owner.Project) &&
		r.RelativePath == r.ID && r.Identity != "" && r.CreatedAt > 0 && class
}

func sameResourceIdentity(a, b taskresource.Resource) bool {
	return a.ID == b.ID && a.Generation == b.Generation && a.Owner == b.Owner && a.Class == b.Class &&
		a.RelativePath == b.RelativePath && a.Identity == b.Identity && a.CreatedAt == b.CreatedAt
}

// RegisterTaskResource never adopts an arbitrary path. The app must first prove
// allocation under its own managed root; this layer protects immutable ownership
// from conflicting registrations, including across daemon restarts.
func (s *Store) RegisterTaskResource(ctx context.Context, r taskresource.Resource) error {
	if !validResource(r) || r.LastOutcome != "" || r.LastExecution != "" || r.LastActor != "" || r.LastPlan != "" || r.LastTargets != "" || r.LastReason != "" || r.LastAt != 0 || r.Manifest != "" {
		return errors.New("resource_registration_invalid")
	}
	return s.write(ctx, func(tx *sql.Tx) (int64, error) {
		var raw string
		err := tx.QueryRowContext(ctx, "SELECT record FROM broker_task_resources WHERE id=? OR relative_path=?", r.ID, r.RelativePath).Scan(&raw)
		if err == nil {
			old, decodeErr := decodeTaskResource(raw)
			if decodeErr != nil || !sameResourceIdentity(old, r) {
				return 0, ErrResourceConflict
			}
			return 0, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		rawBytes, err := json.Marshal(r)
		if err != nil {
			return 0, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO broker_task_resources
		 (id,generation,owner_kind,owner_id,class,relative_path,state,record,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			r.ID, r.Generation, r.Owner.Kind, r.Owner.ID, string(r.Class), r.RelativePath, "registered", string(rawBytes), r.CreatedAt, r.CreatedAt)
		if err != nil {
			return 0, err
		}
		if err := insertEvents(ctx, tx, []Event{{Kind: "resource.registered", Subject: r.ID, Payload: rawBytes}}); err != nil {
			return 0, err
		}
		return 2, nil
	})
}

func (s *Store) TaskResource(ctx context.Context, id string) (taskresource.Resource, error) {
	if err := reading(); err != nil {
		return taskresource.Resource{}, err
	}
	var raw string
	if err := s.rd.QueryRowContext(ctx, "SELECT record FROM broker_task_resources WHERE id=?", id).Scan(&raw); err != nil {
		return taskresource.Resource{}, classify(err)
	}
	return decodeTaskResource(raw)
}

func decodeTaskResource(raw string) (taskresource.Resource, error) {
	var r taskresource.Resource
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&r) != nil {
		return taskresource.Resource{}, errors.New("resource_record_unreadable")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return taskresource.Resource{}, errors.New("resource_record_unreadable")
	}

	var protection struct {
		Pinned  *bool   `json:"pinned"`
		Outcome *string `json:"last_outcome"`
	}
	if json.Unmarshal([]byte(raw), &protection) != nil || protection.Pinned == nil || protection.Outcome == nil {
		return taskresource.Resource{}, errors.New("resource_record_unreadable")
	}

	if !validResource(r) {
		return r, errors.New("resource_record_unreadable")
	}
	return r, nil
}

// TaskResourcePage advances by immutable ID, independently of creation or cleanup
// timestamps. Removed rows remain readable but do not crowd the active sweep.
func (s *Store) TaskResourcePage(ctx context.Context, after string, limit int, activeOnly bool) ([]taskresource.Resource, string, error) {
	if limit <= 0 || limit > taskresource.PageRowsLimit {
		return nil, "", errors.New("resource_page_invalid")
	}
	if after != "" {
		if _, err := uuid.Parse(after); err != nil {
			return nil, "", errors.New("resource_cursor_invalid")
		}
	}
	if err := reading(); err != nil {
		return nil, "", err
	}
	q := "SELECT record FROM broker_task_resources WHERE id>?"
	if activeOnly {
		q += " AND state!='removed'"
	}
	q += " ORDER BY id LIMIT ?"
	rows, err := s.rd.QueryContext(ctx, q, after, limit)
	if err != nil {
		return nil, "", classify(err)
	}
	defer rows.Close()
	out := []taskresource.Resource{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, "", classify(err)
		}
		r, err := decodeTaskResource(raw)
		if err != nil {
			return nil, "", err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, "", classify(err)
	}
	next := ""
	if len(out) == limit {
		next = out[len(out)-1].ID
	}
	return out, next, nil
}

// UpdateTaskResource atomically records protection or cleanup facts with the event
// that explains them. Ownership, storage class and directory identity cannot be
// changed, even by a privileged caller trying to downgrade retained evidence.
func (s *Store) UpdateTaskResource(ctx context.Context, id, event string, at time.Time, change func(*taskresource.Resource) error) (taskresource.Resource, error) {
	var result taskresource.Resource
	if event == "" || at.IsZero() {
		return result, errors.New("resource_update_invalid")
	}
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		var raw string
		if err := tx.QueryRowContext(ctx, "SELECT record FROM broker_task_resources WHERE id=?", id).Scan(&raw); err != nil {
			return 0, err
		}
		r, err := decodeTaskResource(raw)
		if err != nil {
			return 0, err
		}
		old := r
		if err := change(&r); err != nil {
			return 0, err
		}
		if !sameResourceIdentity(old, r) {
			return 0, ErrResourceConflict
		}
		result = r
		if old == r {
			return 0, nil
		}
		body, err := json.Marshal(r)
		if err != nil {
			return 0, err
		}
		state := r.LastOutcome
		if state == "" {
			state = "registered"
		}
		if _, err := tx.ExecContext(ctx, "UPDATE broker_task_resources SET state=?,record=?,updated_at=? WHERE id=?", state, string(body), at.Unix(), id); err != nil {
			return 0, err
		}
		if err := insertEvents(ctx, tx, []Event{{Kind: event, Subject: id, Payload: body}}); err != nil {
			return 0, err
		}
		return 2, nil
	})
	return result, err
}
