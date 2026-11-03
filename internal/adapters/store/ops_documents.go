package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/sainteye/clawdline/internal/domain/opsdocs"
)

// The Ops documents list (docs/ops-documents.md). Its rows live in this
// database beside the Session to-dos, so that asking a Session to read a link
// changes the entry and adds the to-do in one Work v2 transaction.

const opsSchema = `
CREATE TABLE IF NOT EXISTS ops_collections (
  id          TEXT PRIMARY KEY,
  project_key TEXT NOT NULL,
  name        TEXT NOT NULL,
  position    INTEGER NOT NULL,
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS ops_collections_project ON ops_collections(project_key);
CREATE TABLE IF NOT EXISTS ops_entries (
  id               TEXT PRIMARY KEY,
  project_key      TEXT NOT NULL,
  kind             TEXT NOT NULL CHECK (kind IN ('file','link')),
  title            TEXT NOT NULL,
  title_source     TEXT NOT NULL,
  collection       TEXT NOT NULL DEFAULT '',
  position         INTEGER NOT NULL,
  path             TEXT NOT NULL DEFAULT '',
  url              TEXT NOT NULL DEFAULT '',
  type             TEXT NOT NULL,
  status           TEXT NOT NULL,
  access_note      TEXT NOT NULL DEFAULT '',
  resolve_terminal TEXT NOT NULL DEFAULT '',
  resolve_todo     TEXT NOT NULL DEFAULT '',
  created_at       INTEGER NOT NULL,
  updated_at       INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS ops_entries_project ON ops_entries(project_key);
CREATE TABLE IF NOT EXISTS ops_pending_renames (
  entry_id    TEXT PRIMARY KEY,
  project_key TEXT NOT NULL,
  from_path   TEXT NOT NULL,
  to_path     TEXT NOT NULL
);
`

func openOps(db *sql.DB) error {
	_, err := db.Exec(opsSchema)
	return err
}

// ErrNoOps is an Ops entry or collection id this store does not hold.
var ErrNoOps = errors.New("no Ops entry or collection has that id")

// ErrOpsFull is a Project already holding as many entries or collections as
// it keeps; nothing was written.
var ErrOpsFull = errors.New("the Project's Ops list is full")

const opsEntryColumns = `id,project_key,kind,title,title_source,collection,position,path,url,type,status,
access_note,resolve_terminal,resolve_todo,created_at,updated_at`

func scanOpsEntry(r rowScanner) (opsdocs.Entry, error) {
	var e opsdocs.Entry
	err := r.Scan(&e.ID, &e.ProjectKey, &e.Kind, &e.Title, &e.TitleSource, &e.Collection, &e.Position,
		&e.Path, &e.URL, &e.Type, &e.Status, &e.AccessNote, &e.ResolveTerminal, &e.ResolveTodo,
		&e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNoOps
	}
	return e, err
}

func scanOpsCollection(r rowScanner) (opsdocs.Collection, error) {
	var c opsdocs.Collection
	err := r.Scan(&c.ID, &c.ProjectKey, &c.Name, &c.Position)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNoOps
	}
	return c, err
}

type opsQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func opsList(ctx context.Context, q opsQuerier, key string) ([]opsdocs.Collection, []opsdocs.Entry, error) {
	collections := []opsdocs.Collection{}
	rows, err := q.QueryContext(ctx, `SELECT id,project_key,name,position FROM ops_collections
    WHERE project_key=? ORDER BY position, rowid`, key)
	if err != nil {
		return nil, nil, classify(err)
	}
	for rows.Next() {
		c, err := scanOpsCollection(rows)
		if err != nil {
			rows.Close()
			return nil, nil, err
		}
		collections = append(collections, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	entries := []opsdocs.Entry{}
	rows, err = q.QueryContext(ctx, `SELECT `+opsEntryColumns+` FROM ops_entries
    WHERE project_key=? ORDER BY collection, position, rowid`, key)
	if err != nil {
		return nil, nil, classify(err)
	}
	defer rows.Close()
	for rows.Next() {
		e, err := scanOpsEntry(rows)
		if err != nil {
			return nil, nil, err
		}
		entries = append(entries, e)
	}
	return collections, entries, rows.Err()
}

// OpsList is every collection and entry filed under one canonical key.
func (s *Store) OpsList(ctx context.Context, key string) ([]opsdocs.Collection, []opsdocs.Entry, error) {
	if err := reading(); err != nil {
		return nil, nil, err
	}
	return opsList(ctx, s.rd, key)
}

// OpsEntry is one entry by id, whatever Project it belongs to.
func (s *Store) OpsEntry(ctx context.Context, id string) (opsdocs.Entry, error) {
	if err := reading(); err != nil {
		return opsdocs.Entry{}, err
	}
	return scanOpsEntry(s.rd.QueryRowContext(ctx, `SELECT `+opsEntryColumns+` FROM ops_entries WHERE id=?`, id))
}

// OpsPendingRenames is every rename recorded and not yet settled.
func (s *Store) OpsPendingRenames(ctx context.Context) ([]opsdocs.PendingRename, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.rd.QueryContext(ctx, `SELECT entry_id,project_key,from_path,to_path FROM ops_pending_renames ORDER BY rowid`)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	var out []opsdocs.PendingRename
	for rows.Next() {
		var p opsdocs.PendingRename
		if err := rows.Scan(&p.EntryID, &p.ProjectKey, &p.From, &p.To); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// OpsTodoState is where one direct to-do is: unsent, sent, read, done or
// missing.
func (s *Store) OpsTodoState(ctx context.Context, id string) (string, error) {
	if err := reading(); err != nil {
		return "", err
	}
	var sent, read, completed sql.NullInt64
	err := s.rd.QueryRowContext(ctx, `SELECT sent_at,read_at,completed_at FROM session_direct_todos WHERE id=?`, id).
		Scan(&sent, &read, &completed)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "missing", nil
	case err != nil:
		return "", err
	case completed.Valid && completed.Int64 > 0:
		return "done", nil
	case read.Valid && read.Int64 > 0:
		return "read", nil
	case sent.Valid && sent.Int64 > 0:
		return "sent", nil
	}
	return "unsent", nil
}

// OpsList is OpsList inside a write.
func (t *WorkV2Tx) OpsList(key string) ([]opsdocs.Collection, []opsdocs.Entry, error) {
	return opsList(t.ctx, t.tx, key)
}

func (t *WorkV2Tx) OpsEntry(id string) (opsdocs.Entry, error) {
	return scanOpsEntry(t.tx.QueryRowContext(t.ctx, `SELECT `+opsEntryColumns+` FROM ops_entries WHERE id=?`, id))
}

func (t *WorkV2Tx) OpsCollection(id string) (opsdocs.Collection, error) {
	return scanOpsCollection(t.tx.QueryRowContext(t.ctx,
		`SELECT id,project_key,name,position FROM ops_collections WHERE id=?`, id))
}

// OpsNextPosition is one past the last position in a collection.
func (t *WorkV2Tx) OpsNextPosition(key, collection string) (int64, error) {
	var n sql.NullInt64
	err := t.tx.QueryRowContext(t.ctx, `SELECT MAX(position) FROM ops_entries WHERE project_key=? AND collection=?`,
		key, collection).Scan(&n)
	if err != nil || !n.Valid {
		return 0, err
	}
	return n.Int64 + 1, nil
}

// InsertOpsEntry adds an entry, refused with ErrOpsFull at the Project's
// bound.
func (t *WorkV2Tx) InsertOpsEntry(e opsdocs.Entry) error {
	var n int64
	if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM ops_entries WHERE project_key=?`, e.ProjectKey).Scan(&n); err != nil {
		return err
	}
	if n >= opsdocs.EntriesPerProjectLimit {
		return ErrOpsFull
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO ops_entries (`+opsEntryColumns+`)
    VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, e.ID, e.ProjectKey, e.Kind, e.Title, e.TitleSource, e.Collection,
		e.Position, e.Path, e.URL, e.Type, e.Status, e.AccessNote, e.ResolveTerminal, e.ResolveTodo, e.CreatedAt, e.UpdatedAt)
	if err == nil {
		t.wrote++
	}
	return err
}

// PutOpsEntry replaces an existing entry's mutable fields.
func (t *WorkV2Tx) PutOpsEntry(e opsdocs.Entry) error {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE ops_entries SET title=?,title_source=?,collection=?,position=?,path=?,
    url=?,type=?,status=?,access_note=?,resolve_terminal=?,resolve_todo=?,updated_at=? WHERE id=?`,
		e.Title, e.TitleSource, e.Collection, e.Position, e.Path, e.URL, e.Type, e.Status, e.AccessNote,
		e.ResolveTerminal, e.ResolveTodo, e.UpdatedAt, e.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrNoOps
	}
	t.wrote++
	return nil
}

func (t *WorkV2Tx) DeleteOpsEntry(id string) error {
	res, err := t.tx.ExecContext(t.ctx, `DELETE FROM ops_entries WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrNoOps
	}
	if _, err := t.tx.ExecContext(t.ctx, `DELETE FROM ops_pending_renames WHERE entry_id=?`, id); err != nil {
		return err
	}
	t.wrote++
	return nil
}

// InsertOpsCollection adds a collection at the end, refused with ErrOpsFull
// at the Project's bound.
func (t *WorkV2Tx) InsertOpsCollection(c opsdocs.Collection, now int64) (opsdocs.Collection, error) {
	var n int64
	var last sql.NullInt64
	if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*),MAX(position) FROM ops_collections WHERE project_key=?`,
		c.ProjectKey).Scan(&n, &last); err != nil {
		return c, err
	}
	if n >= opsdocs.CollectionsPerProjectLimit {
		return c, ErrOpsFull
	}
	if last.Valid {
		c.Position = last.Int64 + 1
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO ops_collections (id,project_key,name,position,created_at,updated_at)
    VALUES (?,?,?,?,?,?)`, c.ID, c.ProjectKey, c.Name, c.Position, now, now)
	if err == nil {
		t.wrote++
	}
	return c, err
}

func (t *WorkV2Tx) PutOpsCollection(c opsdocs.Collection, now int64) error {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE ops_collections SET name=?,position=?,updated_at=? WHERE id=?`,
		c.Name, c.Position, now, c.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrNoOps
	}
	t.wrote++
	return nil
}

// DeleteOpsCollection removes a collection and moves its entries to no
// collection, after the entries already there.
func (t *WorkV2Tx) DeleteOpsCollection(c opsdocs.Collection, now int64) error {
	next, err := t.OpsNextPosition(c.ProjectKey, "")
	if err != nil {
		return err
	}
	if _, err := t.tx.ExecContext(t.ctx, `UPDATE ops_entries SET collection='',position=position+?,updated_at=?
    WHERE project_key=? AND collection=?`, next, now, c.ProjectKey, c.ID); err != nil {
		return err
	}
	res, err := t.tx.ExecContext(t.ctx, `DELETE FROM ops_collections WHERE id=?`, c.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrNoOps
	}
	t.wrote++
	return nil
}

// PutOpsPendingRename records a rename before the disk is touched.
func (t *WorkV2Tx) PutOpsPendingRename(p opsdocs.PendingRename) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT OR REPLACE INTO ops_pending_renames (entry_id,project_key,from_path,to_path)
    VALUES (?,?,?,?)`, p.EntryID, p.ProjectKey, p.From, p.To)
	if err == nil {
		t.wrote++
	}
	return err
}

func (t *WorkV2Tx) DeleteOpsPendingRename(entryID string) error {
	_, err := t.tx.ExecContext(t.ctx, `DELETE FROM ops_pending_renames WHERE entry_id=?`, entryID)
	if err == nil {
		t.wrote++
	}
	return err
}
