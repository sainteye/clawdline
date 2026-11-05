package store

import (
	"context"
	"database/sql"
	"errors"
)

// One row per native conversation, independent of terminal and browser reuse.
// An attempt is recorded before typing; a crash leaves unknown rather than
// silently providing the same instructions again. No prompt text is stored.
const codegraphGuidanceSchema = `CREATE TABLE IF NOT EXISTS codegraph_guidance (
 conversation TEXT PRIMARY KEY, enabled INTEGER NOT NULL, revision INTEGER NOT NULL,
 version TEXT NOT NULL, language TEXT NOT NULL, state TEXT NOT NULL,
 source TEXT NOT NULL
);`

var ErrCodegraphGuidanceChanged = errors.New("codegraph_guidance_changed")

type CodegraphGuidance struct {
	Enabled                          bool
	Revision                         int64
	Version, Language, State, Source string
}

func (s *Store) ensureCodegraphGuidance(ctx context.Context) error {
	var exists bool
	if err := s.rd.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type='table' AND name='codegraph_guidance')`).Scan(&exists); err != nil {
		return classify(err)
	}
	if exists {
		return nil
	}
	_, err := s.db.ExecContext(ctx, codegraphGuidanceSchema)
	return classify(err)
}
func readCodegraphGuidance(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, conversation string) (CodegraphGuidance, bool, error) {
	var g CodegraphGuidance
	err := q.QueryRowContext(ctx, `SELECT enabled,revision,version,language,state,source FROM codegraph_guidance WHERE conversation=?`, conversation).Scan(&g.Enabled, &g.Revision, &g.Version, &g.Language, &g.State, &g.Source)
	if errors.Is(err, sql.ErrNoRows) {
		return g, false, nil
	}
	return g, err == nil, classify(err)
}
func (s *Store) CodegraphGuidance(ctx context.Context, conversation string) (CodegraphGuidance, bool, error) {
	if err := s.ensureCodegraphGuidance(ctx); err != nil {
		return CodegraphGuidance{}, false, err
	}
	return readCodegraphGuidance(ctx, s.rd, conversation)
}

// Configure uses a compare-and-swap so two browsers cannot overwrite an unseen
// preference. Launch defaults are inserted only when no explicit choice exists.
func (s *Store) ConfigureCodegraphGuidance(ctx context.Context, conversation string, expected int64, enabled, again bool, version, language string, launch bool) (CodegraphGuidance, error) {
	if conversation == "" {
		return CodegraphGuidance{}, ErrCodegraphGuidanceChanged
	}
	if err := s.ensureCodegraphGuidance(ctx); err != nil {
		return CodegraphGuidance{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CodegraphGuidance{}, classify(err)
	}
	defer tx.Rollback()
	g, found, err := readCodegraphGuidance(ctx, tx, conversation)
	if err != nil {
		return g, err
	}
	if launch && found {
		return g, nil
	}
	if !launch && g.Revision != expected {
		return g, ErrCodegraphGuidanceChanged
	}
	if found && g.Enabled == enabled && !again && g.Version == version {
		return g, nil
	}
	state, source := "pending", "session"
	if launch {
		state, source = "launch", "launch"
	} else if !found && !enabled && !again {
		state = "off"
	}
	g = CodegraphGuidance{Enabled: enabled, Revision: g.Revision + 1, Version: version, Language: language, State: state, Source: source}
	_, err = tx.ExecContext(ctx, `INSERT INTO codegraph_guidance(conversation,enabled,revision,version,language,state,source) VALUES(?,?,?,?,?,?,?) ON CONFLICT(conversation) DO UPDATE SET enabled=excluded.enabled,revision=excluded.revision,version=excluded.version,language=excluded.language,state=excluded.state,source=excluded.source`, conversation, g.Enabled, g.Revision, g.Version, g.Language, g.State, g.Source)
	if err != nil {
		return g, classify(err)
	}
	return g, classify(tx.Commit())
}

// Claim atomically consumes the pending revision before any terminal effect.
// Only a proved pre-byte refusal may restore it. Unknown attempts require an
// explicit new revision; automatic retries never infer absence from a graph.
func (s *Store) ClaimCodegraphGuidance(ctx context.Context, conversation string) (CodegraphGuidance, bool, error) {
	if err := s.ensureCodegraphGuidance(ctx); err != nil {
		return CodegraphGuidance{}, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CodegraphGuidance{}, false, classify(err)
	}
	defer tx.Rollback()
	g, found, err := readCodegraphGuidance(ctx, tx, conversation)
	if err != nil || !found || g.State != "pending" {
		return g, false, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE codegraph_guidance SET state='unknown' WHERE conversation=? AND revision=? AND state='pending'`, conversation, g.Revision)
	if err != nil {
		return g, false, classify(err)
	}
	return g, true, classify(tx.Commit())
}
func (s *Store) FinishCodegraphGuidance(ctx context.Context, conversation string, revision int64, state string) error {
	if state != "pending" && state != "provided" && state != "unknown" {
		return ErrCodegraphGuidanceChanged
	}
	_, err := s.db.ExecContext(ctx, `UPDATE codegraph_guidance SET state=? WHERE conversation=? AND revision=? AND state='unknown'`, state, conversation, revision)
	return classify(err)
}
