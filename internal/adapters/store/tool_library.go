package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/sainteye/clawdline/internal/domain/toollibrary"
)

const toolLibrarySchema = `
CREATE TABLE IF NOT EXISTS tool_library_state (id INTEGER PRIMARY KEY CHECK(id=1), version INTEGER NOT NULL);
INSERT OR IGNORE INTO tool_library_state(id,version) VALUES(1,0);
CREATE TABLE IF NOT EXISTS tool_library_projects (id TEXT PRIMARY KEY, body TEXT NOT NULL);
`

type ToolLibrary struct {
	Version int64                 `json:"version"`
	Saved   []toollibrary.Project `json:"saved"`
}

var ErrToolLibraryVersion = errors.New("tool_library_version_conflict")
var ErrToolLibraryFull = errors.New("tool_library_full")

func toolLibraryRead(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (ToolLibrary, error) {
	out := ToolLibrary{Saved: []toollibrary.Project{}}
	if err := db.QueryRowContext(ctx, `SELECT version FROM tool_library_state WHERE id=1`).Scan(&out.Version); err != nil {
		return out, classify(err)
	}
	rows, err := db.QueryContext(ctx, `SELECT body FROM tool_library_projects ORDER BY id`)
	if err != nil {
		return out, classify(err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var p toollibrary.Project
		if err = rows.Scan(&raw); err != nil {
			return out, err
		}
		if err = json.Unmarshal([]byte(raw), &p); err != nil {
			return out, err
		}
		out.Saved = append(out.Saved, p)
	}
	return out, rows.Err()
}

// ToolLibrary reads one WAL snapshot so a version can never describe another
// writer's rows. The catalog remains private to this machine's paired readers.
func (s *Store) ToolLibrary(ctx context.Context) (ToolLibrary, error) {
	tx, err := s.rd.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ToolLibrary{}, classify(err)
	}
	defer tx.Rollback()
	out, err := toolLibraryRead(ctx, tx)
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}

// ChangeToolProject uses the same immediate transaction for revision, capacity,
// and bytes. Nothing about an installation is inferred from this collection.
func (s *Store) ChangeToolProject(ctx context.Context, expected int64, id string, p *toollibrary.Project, limit int64) (ToolLibrary, error) {
	var raw []byte
	if p != nil {
		normalized, err := toollibrary.Normalize(*p)
		if err != nil {
			return ToolLibrary{}, err
		}
		p = &normalized
		id = p.ID
		raw, err = json.Marshal(p)
		if err != nil {
			return ToolLibrary{}, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ToolLibrary{}, classify(err)
	}
	defer tx.Rollback()
	current, err := toolLibraryRead(ctx, tx)
	if err != nil {
		return current, err
	}
	if current.Version != expected {
		return current, ErrToolLibraryVersion
	}
	if p != nil {
		exists := false
		for _, saved := range current.Saved {
			if saved.ID == id {
				exists = true
			}
		}
		if !exists && int64(len(current.Saved)) >= min(limit, int64(toollibrary.MaxSavedProjects)) {
			return current, ErrToolLibraryFull
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO tool_library_projects(id,body) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body`, id, string(raw))
	} else {
		_, err = tx.ExecContext(ctx, `DELETE FROM tool_library_projects WHERE id=?`, id)
	}
	if err != nil {
		return current, classify(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tool_library_state SET version=version+1 WHERE id=1`); err != nil {
		return current, classify(err)
	}
	out, err := toolLibraryRead(ctx, tx)
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}
