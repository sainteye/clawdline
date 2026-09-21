package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/sainteye/clawdline/internal/domain/snippet"
)

// Snippets live in this daemon's own store, one row per snippet.
//
// **Not in the Swift app's directory.** That app keeps one JSON file per
// snippet under its own state root and is still running beside this daemon on
// some machines; two writers on one directory, each with its own idea of what
// a valid file is, is the shape this rewrite exists to stop. So the rows here
// start empty on every machine, the old files are left exactly where they are,
// and docs say how to carry them over on purpose.
//
// Everything a write decides — the position a new snippet lands at, whether one
// more fits — is decided inside the transaction that writes it, because each
// of those is a read that decides: two creates that both read "the highest
// position here is 300" would both write 400.
const snippetSchema = `
CREATE TABLE IF NOT EXISTS snippets (
  id         TEXT    PRIMARY KEY,
  title      TEXT    NOT NULL,
  body       TEXT    NOT NULL,
  scope      TEXT    NOT NULL,
  project    TEXT    NOT NULL DEFAULT '',
  position   INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS snippets_by_place ON snippets(scope, project, position);
CREATE TABLE IF NOT EXISTS snippet_writes (
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS snippet_writes_by_time ON snippet_writes(at);
`

func openSnippets(db *sql.DB) error {
	_, err := db.Exec(snippetSchema)
	return err
}

// SnippetTotalLimit and SnippetScopeLimit are the defaults for the two
// registered rows (`snippets.total`, `snippets.scope`). They are the Swift
// store's numbers. The limit a route enforces comes from the register, which
// may lower them; these are what the register's rows default to.
const (
	SnippetTotalLimit = 100
	SnippetScopeLimit = 50
)

// snippetWriteLimit and snippetWriteWindow are the disk brake, as the Swift
// store has it: ten writes in ten minutes, across every snippet and every
// caller.
//
// It bounds nothing that accumulates — it refuses one write at the door and the
// next window lets it through — so it is an admission bound on the capacity
// register's baseline, not a row. A refused write answers `rate_limited`, which
// the console already has a sentence for.
const (
	snippetWriteLimit  = 10
	snippetWriteWindow = 10 * time.Minute
)

// SnippetRateLimited is the brake having refused a write. It carries no status
// of its own: the route turns it into 429 `rate_limited`.
var SnippetRateLimited = errors.New("snippet writes: ten in ten minutes already")

// Snippets is every snippet on this machine, in the order the sheet draws one
// group: the person's own order, then arrival.
func (s *Store) Snippets(ctx context.Context) ([]snippet.Record, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, body, scope, project, position, created_at, updated_at FROM snippets`)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	out := []snippet.Record{}
	for rows.Next() {
		var r snippet.Record
		var scope, project string
		if err := rows.Scan(&r.ID, &r.Title, &r.Body, &scope, &project,
			&r.Position, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Scope, r.Project = snippet.Scope(scope), project
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	snippet.Sort(out)
	return out, nil
}

// SnippetCounts is how full the two registered rows are: how many snippets
// there are, and the fullest single group with its name.
func (s *Store) SnippetCounts(ctx context.Context) (total int64, fullest int64, group string, err error) {
	if err := reading(); err != nil {
		return 0, 0, "", err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM snippets`).Scan(&total); err != nil {
		return 0, 0, "", classify(err)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT scope, project, COUNT(*) FROM snippets GROUP BY scope, project
		 ORDER BY COUNT(*) DESC, scope, project LIMIT 1`)
	if err != nil {
		return total, 0, "", classify(err)
	}
	defer rows.Close()
	if rows.Next() {
		var scope, project string
		if err := rows.Scan(&scope, &project, &fullest); err != nil {
			return total, 0, "", err
		}
		group = scope
		if project != "" {
			group = scope + " " + project
		}
	}
	return total, fullest, group, rows.Err()
}

// CreateSnippet writes one new snippet.
//
// The id is the caller's, so the route that mints it is the one place an id is
// made. The position and both limits are decided in here, under the write
// right, because each is a read that decides.
func (s *Store) CreateSnippet(ctx context.Context, id string, f snippet.Fields,
	limits snippet.Limits, now time.Time) (snippet.Record, *snippet.Refusal, error) {
	var made snippet.Record
	var refusal *snippet.Refusal
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		// Built once against a position of zero, so that a malformed body is
		// refused before the brake is spent on it and before the group is
		// counted.
		candidate, ref := snippet.New(id, f, 0, now.Unix())
		if ref != nil {
			refusal = ref
			return 0, nil
		}
		total, group, err := countSnippets(ctx, tx, candidate.Scope, candidate.Project)
		if err != nil {
			return 0, err
		}
		if ref := snippet.LimitRefusal(total, group, limits); ref != nil {
			refusal = ref
			return 0, nil
		}
		if err := takeSnippetWrite(ctx, tx, now); err != nil {
			return 0, err
		}
		highest, err := highestSnippetPosition(ctx, tx, candidate.Scope, candidate.Project)
		if err != nil {
			return 0, err
		}
		candidate.Position = snippet.NextPosition(highest)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO snippets (id, title, body, scope, project, position, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			candidate.ID, candidate.Title, candidate.Body, string(candidate.Scope),
			candidate.Project, candidate.Position, candidate.CreatedAt, candidate.UpdatedAt); err != nil {
			return 0, err
		}
		made = candidate
		return 1, nil
	})
	return made, refusal, err
}

// UpdateSnippet saves whichever fields a body named over the row that is
// there. A snippet that changed group lands at the end of the one it arrives
// in, which is the Swift store's rule and the reason the sheet re-reads rather
// than moving a row itself.
func (s *Store) UpdateSnippet(ctx context.Context, id string, f snippet.Fields,
	limits snippet.Limits, now time.Time) (snippet.Record, *snippet.Refusal, error) {
	var saved snippet.Record
	var refusal *snippet.Refusal
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		current, found, err := oneSnippet(ctx, tx, id)
		if err != nil {
			return 0, err
		}
		if !found {
			refusal = snippet.NotFound()
			return 0, nil
		}
		next, ref := snippet.Patch(current, f, now.Unix())
		if ref != nil {
			refusal = ref
			return 0, nil
		}
		if snippet.Moved(current, next) {
			// The group it is arriving in is counted without it, exactly as a
			// create counts: a snippet moving into a full group is refused,
			// and one staying where it is never is.
			total, group, err := countSnippets(ctx, tx, next.Scope, next.Project)
			if err != nil {
				return 0, err
			}
			if ref := snippet.LimitRefusal(total-1, group, limits); ref != nil {
				refusal = ref
				return 0, nil
			}
		}
		if err := takeSnippetWrite(ctx, tx, now); err != nil {
			return 0, err
		}
		if snippet.Moved(current, next) {
			highest, err := highestSnippetPosition(ctx, tx, next.Scope, next.Project)
			if err != nil {
				return 0, err
			}
			next.Position = snippet.NextPosition(highest)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE snippets SET title = ?, body = ?, scope = ?, project = ?, position = ?, updated_at = ?
			 WHERE id = ?`,
			next.Title, next.Body, string(next.Scope), next.Project,
			next.Position, next.UpdatedAt, next.ID); err != nil {
			return 0, err
		}
		saved = next
		return 1, nil
	})
	return saved, refusal, err
}

// DeleteSnippet takes one away. A snippet nobody has is `snippet_not_found`
// rather than a silent success: the sheet that asked is about to redraw, and
// "it is gone" and "it was never here" are different answers to give it.
func (s *Store) DeleteSnippet(ctx context.Context, id string, now time.Time) (*snippet.Refusal, error) {
	var refusal *snippet.Refusal
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		_, found, err := oneSnippet(ctx, tx, id)
		if err != nil {
			return 0, err
		}
		if !found {
			refusal = snippet.NotFound()
			return 0, nil
		}
		if err := takeSnippetWrite(ctx, tx, now); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM snippets WHERE id = ?`, id); err != nil {
			return 0, err
		}
		return 1, nil
	})
	return refusal, err
}

// OrderSnippets writes one group's complete order.
//
// One brake token for the whole group, as the Swift store spends one: this is
// one press, however many rows it moves.
func (s *Store) OrderSnippets(ctx context.Context, scope snippet.Scope, project string,
	ids []string, now time.Time) ([]snippet.Record, *snippet.Refusal, error) {
	var ordered []snippet.Record
	var refusal *snippet.Refusal
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		current, err := groupSnippets(ctx, tx, scope, project)
		if err != nil {
			return 0, err
		}
		next, ref := snippet.Order(current, ids)
		if ref != nil {
			refusal = ref
			return 0, nil
		}
		if err := takeSnippetWrite(ctx, tx, now); err != nil {
			return 0, err
		}
		for i := range next {
			next[i].UpdatedAt = now.Unix()
			if _, err := tx.ExecContext(ctx,
				`UPDATE snippets SET position = ?, updated_at = ? WHERE id = ?`,
				next[i].Position, next[i].UpdatedAt, next[i].ID); err != nil {
				return 0, err
			}
		}
		ordered = next
		return int64(len(next)), nil
	})
	return ordered, refusal, err
}

func oneSnippet(ctx context.Context, tx *sql.Tx, id string) (snippet.Record, bool, error) {
	var r snippet.Record
	var scope, project string
	err := tx.QueryRowContext(ctx,
		`SELECT id, title, body, scope, project, position, created_at, updated_at
		 FROM snippets WHERE id = ?`, id).
		Scan(&r.ID, &r.Title, &r.Body, &scope, &project, &r.Position, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return snippet.Record{}, false, nil
	}
	if err != nil {
		return snippet.Record{}, false, err
	}
	r.Scope, r.Project = snippet.Scope(scope), project
	return r, true, nil
}

func groupSnippets(ctx context.Context, tx *sql.Tx, scope snippet.Scope, project string) ([]snippet.Record, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT id, title, body, scope, project, position, created_at, updated_at
		 FROM snippets WHERE scope = ? AND project = ?`, string(scope), project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []snippet.Record{}
	for rows.Next() {
		var r snippet.Record
		var gotScope, gotProject string
		if err := rows.Scan(&r.ID, &r.Title, &r.Body, &gotScope, &gotProject,
			&r.Position, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Scope, r.Project = snippet.Scope(gotScope), gotProject
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	snippet.Sort(out)
	return out, nil
}

func countSnippets(ctx context.Context, tx *sql.Tx, scope snippet.Scope, project string) (total, group int64, err error) {
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM snippets`).Scan(&total); err != nil {
		return 0, 0, err
	}
	err = tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM snippets WHERE scope = ? AND project = ?`,
		string(scope), project).Scan(&group)
	return total, group, err
}

func highestSnippetPosition(ctx context.Context, tx *sql.Tx, scope snippet.Scope, project string) (int64, error) {
	var highest sql.NullInt64
	err := tx.QueryRowContext(ctx,
		`SELECT MAX(position) FROM snippets WHERE scope = ? AND project = ?`,
		string(scope), project).Scan(&highest)
	if err != nil {
		return 0, err
	}
	if !highest.Valid {
		return 0, nil
	}
	return highest.Int64, nil
}

// takeSnippetWrite spends one token of the disk brake, inside the transaction
// the write is in: a write that is rolled back has not spent one, because the
// row this reads and writes is rolled back with it.
//
// The window is kept as rows rather than in memory so that it survives a
// restart. A machine that was just told to slow down and then restarted would
// otherwise be told nothing.
func takeSnippetWrite(ctx context.Context, tx *sql.Tx, now time.Time) error {
	from := now.Add(-snippetWriteWindow).Unix()
	if _, err := tx.ExecContext(ctx, `DELETE FROM snippet_writes WHERE at < ?`, from); err != nil {
		return err
	}
	var spent int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM snippet_writes`).Scan(&spent); err != nil {
		return err
	}
	if spent >= snippetWriteLimit {
		return SnippetRateLimited
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO snippet_writes (at) VALUES (?)`, now.Unix())
	return err
}
