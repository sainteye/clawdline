package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"

	sb "github.com/sainteye/clawdline/internal/domain/sessionboard"
)

const sessionBoardSchema = `
CREATE TABLE IF NOT EXISTS session_boards (
 id TEXT PRIMARY KEY, project TEXT NOT NULL, name TEXT NOT NULL, purpose TEXT NOT NULL,
 lead TEXT NOT NULL, summary TEXT NOT NULL DEFAULT '', archived_at INTEGER NOT NULL DEFAULT 0,
 latest INTEGER NOT NULL DEFAULT 0, create_digest TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS session_board_members (
 board TEXT NOT NULL REFERENCES session_boards(id) ON DELETE CASCADE,
 actor TEXT NOT NULL, label TEXT NOT NULL, PRIMARY KEY (board, actor)
);
CREATE TABLE IF NOT EXISTS session_board_posts (
 board TEXT NOT NULL REFERENCES session_boards(id) ON DELETE CASCADE,
 seq INTEGER NOT NULL, actor TEXT NOT NULL, label TEXT NOT NULL, text TEXT NOT NULL,
 reply_to INTEGER NOT NULL, at INTEGER NOT NULL, request_key TEXT NOT NULL,
 PRIMARY KEY (board, seq), UNIQUE (board, actor, request_key)
);
`

type sessionBoardQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const boardColumns = "id,project,name,purpose,lead,summary,archived_at,latest"

func scanSessionBoard(row interface{ Scan(...any) error }) (sb.Board, error) {
	var b sb.Board
	err := row.Scan(&b.ID, &b.Project, &b.Name, &b.Purpose, &b.Lead, &b.Summary, &b.ArchivedAt, &b.Latest)
	if errors.Is(err, sql.ErrNoRows) {
		err = sb.ErrMissing
	}
	return b, err
}

func sessionBoardMembers(ctx context.Context, q sessionBoardQuery, id string) ([]sb.Actor, error) {
	rows, err := q.QueryContext(ctx, "SELECT actor,label FROM session_board_members WHERE board=? ORDER BY actor", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []sb.Actor{}
	for rows.Next() {
		var a sb.Actor
		if err := rows.Scan(&a.ID, &a.Label); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SessionBoards returns discoverable headers. A Session sees full metadata
// only on boards it joined; a paired person can inspect the machine's boards.
func (s *Store) SessionBoards(ctx context.Context, project, actor string) ([]sb.Board, error) {
	tx, err := s.rd.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	query := "SELECT " + boardColumns + " FROM session_boards"
	args := []any{}
	if actor != "" {
		query += " WHERE project=? AND (archived_at=0 OR EXISTS (SELECT 1 FROM session_board_members m WHERE m.board=session_boards.id AND m.actor=?))"
		args = append(args, project, actor)
	}
	rows, err := tx.QueryContext(ctx, query+" ORDER BY archived_at ASC, name, id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []sb.Board{}
	for rows.Next() {
		b, err := scanSessionBoard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i := range out {
		b := &out[i]
		members, err := sessionBoardMembers(ctx, tx, b.ID)
		if err != nil {
			return nil, err
		}
		if actor == "" || boardMember(members, actor) {
			b.Members = members
		} else {
			b.Summary, b.Latest = "", 0
		}
	}
	return out, tx.Commit()
}

func boardMember(members []sb.Actor, id string) bool {
	return slices.ContainsFunc(members, func(a sb.Actor) bool { return a.ID == id })
}

func (s *Store) SessionBoardPage(ctx context.Context, project, actor, id string, after, before int64) (sb.Page, error) {
	tx, err := s.rd.BeginTx(ctx, nil)
	if err != nil {
		return sb.Page{}, err
	}
	defer tx.Rollback()
	b, err := scanSessionBoard(tx.QueryRowContext(ctx, "SELECT "+boardColumns+" FROM session_boards WHERE id=?", id))
	if err != nil {
		return sb.Page{}, err
	}
	if actor != "" && b.Project != project {
		return sb.Page{}, sb.ErrMissing
	}
	b.Members, err = sessionBoardMembers(ctx, tx, id)
	if err != nil {
		return sb.Page{}, err
	}
	if actor != "" && !boardMember(b.Members, actor) {
		return sb.Page{}, sb.ErrDenied
	}
	query := "SELECT seq,actor,label,text,reply_to,at FROM session_board_posts WHERE board=?"
	args := []any{id}
	switch {
	case after > 0:
		query += " AND seq>? ORDER BY seq ASC"
		args = append(args, after)
	case before > 0:
		query += " AND seq<? ORDER BY seq DESC"
		args = append(args, before)
	default:
		query += " ORDER BY seq DESC"
	}
	rows, err := tx.QueryContext(ctx, query+" LIMIT ?", append(args, sb.MaxPageRows+1)...)
	if err != nil {
		return sb.Page{}, err
	}
	defer rows.Close()
	page := sb.Page{Board: b, Posts: []sb.Post{}}
	for rows.Next() {
		var p sb.Post
		if err := rows.Scan(&p.Seq, &p.Author.ID, &p.Author.Label, &p.Text, &p.ReplyTo, &p.At); err != nil {
			return sb.Page{}, err
		}
		page.Posts = append(page.Posts, p)
	}
	if err := rows.Err(); err != nil {
		return sb.Page{}, err
	}
	rows.Close()
	if len(page.Posts) > sb.MaxPageRows {
		page.More = true
		page.Posts = page.Posts[:sb.MaxPageRows]
	}
	if after == 0 {
		slices.Reverse(page.Posts)
	}
	return page, tx.Commit()
}

func (s *Store) ChangeSessionBoard(ctx context.Context, project string, actor, target sb.Actor, c sb.Command, key string, now time.Time, additions ...sb.Actor) (sb.Result, error) {
	var result sb.Result
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		var err error
		result, err = changeSessionBoard(ctx, tx, project, actor, target, c, key, now, additions)
		return 1, err
	})
	return result, err
}

func boardHash(parts ...string) string {
	raw, _ := json.Marshal(parts)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func changeSessionBoard(ctx context.Context, tx *sql.Tx, project string, actor, target sb.Actor, c sb.Command, key string, now time.Time, additions []sb.Actor) (sb.Result, error) {
	person := actor.ID == ""
	if c.Action == "create" {
		lead := actor
		if person {
			lead = target
		}
		id := boardHash("session-board", project, actor.ID, key)
		digestFields := []string{lead.ID, c.Name, c.Text}
		for _, id := range c.Members {
			digestFields = append(digestFields, id)
		}
		digest := boardHash(digestFields...)
		var saved string
		err := tx.QueryRowContext(ctx, "SELECT create_digest FROM session_boards WHERE id=?", id).Scan(&saved)
		if err == nil {
			if saved != digest {
				return sb.Result{}, sb.ErrReplay
			}
			return sb.Result{ID: id}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return sb.Result{}, err
		}
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM session_boards").Scan(&n); err != nil {
			return sb.Result{}, err
		}
		if n >= sb.MaxBoards {
			return sb.Result{}, sb.ErrFull
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO session_boards(id,project,name,purpose,lead,create_digest) VALUES(?,?,?,?,?,?)",
			id, project, c.Name, c.Text, lead.ID, digest); err != nil {
			return sb.Result{}, err
		}
		err = insertSessionBoardMembers(ctx, tx, id, nil, append([]sb.Actor{lead}, additions...))
		return sb.Result{ID: id}, err
	}

	result := sb.Result{ID: c.Board}
	b, err := scanSessionBoard(tx.QueryRowContext(ctx, "SELECT "+boardColumns+" FROM session_boards WHERE id=?", c.Board))
	if err != nil {
		return result, err
	}
	if !person && b.Project != project {
		return result, sb.ErrMissing
	}
	members, err := sessionBoardMembers(ctx, tx, b.ID)
	if err != nil {
		return result, err
	}
	member := boardMember(members, actor.ID)
	lead := person || b.Lead == actor.ID
	postActor := actor.ID
	if person {
		postActor = "person"
	}
	if c.Action == "post" && (person || member) {
		var text string
		var reply int64
		err := tx.QueryRowContext(ctx, "SELECT seq,text,reply_to FROM session_board_posts WHERE board=? AND actor=? AND request_key=?", b.ID, postActor, key).Scan(&result.Seq, &text, &reply)
		if err == nil {
			if text != c.Text || reply != c.ReplyTo {
				return result, sb.ErrReplay
			}
			return result, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return result, err
		}
	}
	if c.Action == "archive" && lead && b.ArchivedAt != 0 {
		return result, nil
	}
	if b.ArchivedAt != 0 {
		return result, sb.ErrClosed
	}

	switch c.Action {
	case "add-members":
		if !person || b.Project != project {
			return result, sb.ErrDenied
		}
		err = insertSessionBoardMembers(ctx, tx, b.ID, members, additions)
	case "join":
		switch {
		case person:
			return result, sb.ErrDenied
		}
		err = insertSessionBoardMembers(ctx, tx, b.ID, members, []sb.Actor{actor})
	case "leave":
		if person || lead {
			return result, sb.ErrDenied
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM session_board_members WHERE board=? AND actor=?", b.ID, actor.ID)
	case "post":
		if !person && !member {
			return result, sb.ErrDenied
		}
		if b.Latest >= sb.MaxPosts {
			return result, sb.ErrFull
		}
		if c.ReplyTo < 0 || c.ReplyTo > b.Latest {
			return result, sb.ErrInvalid
		}
		result.Seq = b.Latest + 1
		_, err = tx.ExecContext(ctx, "INSERT INTO session_board_posts(board,seq,actor,label,text,reply_to,at,request_key) VALUES(?,?,?,?,?,?,?,?)",
			b.ID, result.Seq, postActor, actor.Label, c.Text, c.ReplyTo, now.Unix(), key)
		if err == nil {
			_, err = tx.ExecContext(ctx, "UPDATE session_boards SET latest=? WHERE id=?", result.Seq, b.ID)
		}
	case "summary":
		if !lead {
			return result, sb.ErrDenied
		}
		_, err = tx.ExecContext(ctx, "UPDATE session_boards SET summary=? WHERE id=?", c.Text, b.ID)
	case "transfer":
		if !lead || b.Project != project || !boardMember(members, target.ID) {
			return result, sb.ErrDenied
		}
		_, err = tx.ExecContext(ctx, "UPDATE session_boards SET lead=? WHERE id=?", c.Target, b.ID)
	case "archive":
		if !lead {
			return result, sb.ErrDenied
		}
		_, err = tx.ExecContext(ctx, "UPDATE session_boards SET archived_at=? WHERE id=?", now.Unix(), b.ID)
	default:
		return result, sb.ErrInvalid
	}
	return result, err
}

func insertSessionBoardMembers(ctx context.Context, tx *sql.Tx, board string, current, additions []sb.Actor) error {
	for _, actor := range additions {
		if boardMember(current, actor.ID) {
			continue
		}
		if len(current) >= sb.MaxMembers {
			return sb.ErrFull
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO session_board_members(board,actor,label) VALUES(?,?,?)", board, actor.ID, actor.Label); err != nil {
			return err
		}
		current = append(current, actor)
	}
	return nil
}

func (s *Store) pruneSessionBoards(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM session_boards WHERE archived_at>0 AND archived_at<=?", now.AddDate(0, 0, -sb.MaxArchiveDays).Unix())
	return classify(err)
}

func (s *Store) SessionBoardOccupancy(ctx context.Context, kind string) (int64, error) {
	var query string
	switch kind {
	case "boards":
		query = "SELECT count(*) FROM session_boards"
	case "members":
		query = "SELECT coalesce(max(n),0) FROM (SELECT count(*) n FROM session_board_members GROUP BY board)"
	case "posts":
		query = "SELECT coalesce(max(latest),0) FROM session_boards"
	default:
		return 0, sb.ErrInvalid
	}
	var n int64
	err := s.rd.QueryRowContext(ctx, query).Scan(&n)
	return n, err
}
