package store

import (
	"cmp"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strings"
	"time"

	sb "github.com/sainteye/clawdline/internal/domain/sessionboard"
)

const sessionBoardSchema = `
CREATE TABLE IF NOT EXISTS session_boards (
 id TEXT PRIMARY KEY, project TEXT NOT NULL, name TEXT NOT NULL, purpose TEXT NOT NULL,
 lead TEXT NOT NULL, summary TEXT NOT NULL DEFAULT '', archived_at INTEGER NOT NULL DEFAULT 0,
 latest INTEGER NOT NULL DEFAULT 0, create_digest TEXT NOT NULL, created_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS session_board_members (
 board TEXT NOT NULL REFERENCES session_boards(id) ON DELETE CASCADE,
 actor TEXT NOT NULL, label TEXT NOT NULL, persona TEXT NOT NULL DEFAULT '', PRIMARY KEY (board, actor)
);
CREATE TABLE IF NOT EXISTS session_board_posts (
 board TEXT NOT NULL REFERENCES session_boards(id) ON DELETE CASCADE,
 seq INTEGER NOT NULL, actor TEXT NOT NULL, label TEXT NOT NULL, persona TEXT NOT NULL DEFAULT '', text TEXT NOT NULL,
 reply_to INTEGER NOT NULL, reply_actor TEXT NOT NULL DEFAULT '', at INTEGER NOT NULL, request_key TEXT NOT NULL,
 PRIMARY KEY (board, seq), UNIQUE (board, actor, request_key)
);
CREATE TABLE IF NOT EXISTS session_board_post_keys (
 board TEXT NOT NULL REFERENCES session_boards(id) ON DELETE CASCADE,
 actor TEXT NOT NULL, request_key TEXT NOT NULL, PRIMARY KEY (board, actor, request_key)
) WITHOUT ROWID;
`

type sessionBoardQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func openSessionBoards(db *sql.DB) error {
	if _, err := db.Exec(sessionBoardSchema); err != nil {
		return err
	}
	hasCreatedAt, err := hasColumn(db, "session_boards", "created_at")
	if err != nil {
		return err
	}
	if !hasCreatedAt {
		if _, err := db.Exec("ALTER TABLE session_boards ADD COLUMN created_at INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
	}
	for _, table := range []string{"session_board_members", "session_board_posts"} {
		has, err := hasColumn(db, table, "persona")
		if err != nil {
			return err
		}
		if !has {
			if _, err := db.Exec("ALTER TABLE " + table + " ADD COLUMN persona TEXT NOT NULL DEFAULT ''"); err != nil {
				return err
			}
		}
	}
	hasReplyActor, err := hasColumn(db, "session_board_posts", "reply_actor")
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if !hasReplyActor {
		if _, err := tx.Exec("ALTER TABLE session_board_posts ADD COLUMN reply_actor TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
		// Preserve the Inbox recipient when an older post is later dropped.
		if _, err := tx.Exec(`UPDATE session_board_posts AS reply SET reply_actor=original.actor
			FROM session_board_posts AS original
			WHERE original.board=reply.board AND original.seq=reply.reply_to`); err != nil {
			return err
		}
	}
	// Legacy rosters have no join timestamps. Keep the newest active board
	// each Session already joined, falling back to insertion order.
	if _, err := tx.Exec(`WITH ranked AS (
		SELECT m.board, m.actor, row_number() OVER (
			PARTITION BY m.actor ORDER BY b.created_at DESC, b.rowid DESC
		) AS position
		FROM session_board_members m JOIN session_boards b ON b.id=m.board
		WHERE b.archived_at=0
	)
	DELETE FROM session_board_members WHERE (board, actor) IN (
		SELECT board, actor FROM ranked WHERE position>1
	)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE session_boards SET lead=''
		WHERE archived_at=0 AND lead<>'' AND NOT EXISTS (
			SELECT 1 FROM session_board_members m WHERE m.board=session_boards.id AND m.actor=session_boards.lead
		)`); err != nil {
		return err
	}
	return tx.Commit()
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
	rows, err := q.QueryContext(ctx, "SELECT actor,label,persona FROM session_board_members WHERE board=? ORDER BY actor", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []sb.Actor{}
	for rows.Next() {
		var a sb.Actor
		if err := rows.Scan(&a.ID, &a.Label, &a.Persona); err != nil {
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
	// Old boards have no creation timestamp. Their newest post still supplies
	// activity, and rowid preserves insertion order when neither has a date.
	rows, err := tx.QueryContext(ctx, query+` ORDER BY archived_at>0,
		CASE WHEN archived_at>0 THEN archived_at ELSE max(created_at,COALESCE((
			SELECT at FROM session_board_posts WHERE board=session_boards.id AND seq=session_boards.latest
		),0)) END DESC, rowid DESC`, args...)
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

// ArchiveInactiveSessionBoard confirms the two witnessed scans still name
// the same roster and message frontier before closing the board. Keep a
// captured handoff intact while the broker resolves its receiving Session.
func (s *Store) ArchiveInactiveSessionBoard(ctx context.Context, board sb.Board, now time.Time) error {
	return s.write(ctx, func(tx *sql.Tx) (int64, error) {
		current, err := scanSessionBoard(tx.QueryRowContext(ctx, "SELECT "+boardColumns+" FROM session_boards WHERE id=?", board.ID))
		if errors.Is(err, sb.ErrMissing) {
			return 0, nil
		}
		if err != nil {
			return 0, err
		}
		if current.ArchivedAt > 0 || current.Latest != board.Latest {
			return 0, nil
		}
		members, err := sessionBoardMembers(ctx, tx, board.ID)
		if err != nil {
			return 0, err
		}
		if !sb.SameMembers(members, board.Members) {
			return 0, nil
		}

		var pending bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM broker_handoffs h
			WHERE h.state IN ('opening','delivered') AND CASE WHEN json_valid(h.record) THEN
				COALESCE(json_extract(h.record,'$.board_transferred_at'),0)=0
				AND EXISTS (SELECT 1 FROM json_each(h.record,'$.session_boards.boards') WHERE value=?)
			ELSE 1 END
		)`, board.ID).Scan(&pending)
		if err != nil || pending {
			return 0, err
		}
		_, err = tx.ExecContext(ctx, "UPDATE session_boards SET archived_at=? WHERE id=?", now.Unix(), board.ID)
		return 1, err
	})
}

// ActiveSessionBoardIDs snapshots the current membership in this Project before a handoff opens.
func (s *Store) ActiveSessionBoardIDs(ctx context.Context, project, actor string) ([]string, error) {
	rows, err := s.rd.QueryContext(ctx, `SELECT b.id FROM session_boards b
		JOIN session_board_members m ON m.board=b.id
		WHERE b.project=? AND b.archived_at=0 AND m.actor=? ORDER BY b.id`, project, actor)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, classify(err)
		}
		ids = append(ids, id)
	}
	return ids, classify(rows.Err())
}

// TransferSessionBoards moves the still-active membership captured by the
// handoff. Old handoff records may list multiple boards. Deleting the sender
// first leaves room on a full board.
func (t *WorkV2Tx) TransferSessionBoards(project, from string, to sb.Actor, ids []string) error {
	for _, id := range ids {
		res, err := t.tx.ExecContext(t.ctx, `DELETE FROM session_board_members WHERE board=? AND actor=?
			AND EXISTS (SELECT 1 FROM session_boards WHERE id=? AND project=? AND archived_at=0)`, id, from, id, project)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			continue
		}
		members, err := sessionBoardMembers(t.ctx, t.tx, id)
		if err != nil {
			return err
		}
		if err := insertSessionBoardMembers(t.ctx, t.tx, id, members, []sb.Actor{to}); err != nil {
			return err
		}
		if _, err := t.tx.ExecContext(t.ctx, `UPDATE session_boards SET lead=? WHERE id=? AND lead=?`, to.ID, id, from); err != nil {
			return err
		}
		t.wrote++
	}
	return nil
}

// SessionBoardLeadMayClose checks the current role and both memberships in one
// Project. An archived board never delegates the power to close a Session.
func (s *Store) SessionBoardLeadMayClose(ctx context.Context, project, lead, target string) (bool, error) {
	var allowed bool
	err := s.rd.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM session_boards b
		JOIN session_board_members caller ON caller.board=b.id AND caller.actor=b.lead
		JOIN session_board_members member ON member.board=b.id AND member.actor=?
		WHERE b.project=? AND b.lead=? AND b.archived_at=0
	)`, target, project, lead).Scan(&allowed)
	return allowed, classify(err)
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
	b, err := sessionBoardForReader(ctx, tx, project, actor, id)
	if err != nil {
		return sb.Page{}, err
	}
	query := "SELECT p.seq,p.actor,p.label,p.persona,p.text,p.reply_to,p.at FROM session_board_posts p WHERE p.board=?"
	posts, more, err := querySessionBoardPosts(ctx, tx, query, []any{id}, after, before)
	if err != nil {
		return sb.Page{}, err
	}
	return sb.Page{Board: b, Posts: posts, More: more}, tx.Commit()
}

func sessionBoardForReader(ctx context.Context, tx *sql.Tx, project, actor, id string) (sb.Board, error) {
	b, err := scanSessionBoard(tx.QueryRowContext(ctx, "SELECT "+boardColumns+" FROM session_boards WHERE id=?", id))
	if err != nil {
		return sb.Board{}, err
	}
	if actor != "" && b.Project != project {
		return sb.Board{}, sb.ErrMissing
	}
	b.Members, err = sessionBoardMembers(ctx, tx, id)
	if err != nil {
		return sb.Board{}, err
	}
	if actor != "" && !boardMember(b.Members, actor) {
		return sb.Board{}, sb.ErrDenied
	}
	return b, nil
}

type sessionBoardThread struct {
	sb.Thread
	replies []int64
}

// SessionBoardThreads reads the retained reply edges and then hydrates one
// bounded page with a single batch query in the same SQLite snapshot.
func (s *Store) SessionBoardThreads(ctx context.Context, project, actor, id string, cursor sb.ThreadCursor) (sb.ThreadPage, error) {
	tx, err := s.rd.BeginTx(ctx, nil)
	if err != nil {
		return sb.ThreadPage{}, err
	}
	defer tx.Rollback()
	b, err := sessionBoardForReader(ctx, tx, project, actor, id)
	if err != nil {
		return sb.ThreadPage{}, err
	}
	threads, roots, err := sessionBoardThreads(ctx, tx, id, cursor.Seen)
	if err != nil {
		return sb.ThreadPage{}, err
	}

	page := sb.ThreadPage{Posts: []sb.Post{}}
	var selected []int64
	if cursor.Root != 0 {
		if thread := threads[cursor.Root]; thread != nil {
			replies := thread.replies
			start, end := 0, len(replies)
			if cursor.From > 0 {
				start, _ = slices.BinarySearch(replies, cursor.From)
			}
			if cursor.Before > 0 {
				end, _ = slices.BinarySearch(replies, cursor.Before)
				start = max(0, end-sb.MaxPageRows)
			}
			last := min(end, start+sb.MaxPageRows)
			selected = replies[start:last]
			page.Earlier, page.More = start > 0, last < len(replies)
		}
	} else {
		page.Board = &b
		page.Threads = []sb.Thread{}
		for _, root := range roots {
			thread := threads[root]
			if cursor.Before > 0 && thread.Latest >= cursor.Before {
				continue
			}
			if len(page.Threads) == sb.MaxPageRows {
				page.More = true
				break
			}
			page.Threads = append(page.Threads, thread.Thread)
			selected = append(selected, root)
		}
	}
	if len(selected) > 0 {
		args := make([]any, 0, len(selected)+1)
		args = append(args, id)
		for _, seq := range selected {
			args = append(args, seq)
		}
		query := "SELECT p.seq,p.actor,p.label,p.persona,p.text,p.reply_to,p.at FROM session_board_posts p WHERE p.board=? AND p.seq IN (" +
			strings.Repeat("?,", len(selected)-1) + "?)"
		page.Posts, _, err = querySessionBoardPosts(ctx, tx, query, args, 0, 0)
		if err != nil {
			return sb.ThreadPage{}, err
		}
	}
	return page, tx.Commit()
}

func sessionBoardThreads(ctx context.Context, tx *sql.Tx, id string, seen int64) (map[int64]*sessionBoardThread, []int64, error) {
	rows, err := tx.QueryContext(ctx, "SELECT seq,reply_to FROM session_board_posts WHERE board=? ORDER BY seq", id)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	rootByPost := make(map[int64]int64)
	threads := make(map[int64]*sessionBoardThread)
	var roots []int64
	for rows.Next() {
		var seq, parent int64
		if err := rows.Scan(&seq, &parent); err != nil {
			return nil, nil, err
		}
		root := seq
		if parent > 0 {
			root = rootByPost[parent]
			if root == 0 {
				root = parent
			}
		}
		rootByPost[seq] = root
		thread := threads[root]
		if thread == nil {
			thread = &sessionBoardThread{Thread: sb.Thread{Root: root}}
			threads[root] = thread
			roots = append(roots, root)
		}
		thread.Latest = seq
		if seq != root {
			thread.replies = append(thread.replies, seq)
			thread.Replies++
			if seq > seen {
				thread.Unread++
				if thread.FirstUnread == 0 {
					thread.FirstUnread = seq
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	slices.SortFunc(roots, func(a, b int64) int { return cmp.Compare(threads[b].Latest, threads[a].Latest) })
	return threads, roots, nil
}

// SessionBoardInbox reads direct replies written by others to an enrolled
// member. Empty project is the paired person's view; an Agent is project-bound.
func (s *Store) SessionBoardInbox(ctx context.Context, project, actor, id string, after, before int64) (sb.Inbox, error) {
	if actor == "" || id == "" || (after > 0 && before > 0) {
		return sb.Inbox{}, sb.ErrInvalid
	}
	tx, err := s.rd.BeginTx(ctx, nil)
	if err != nil {
		return sb.Inbox{}, err
	}
	defer tx.Rollback()
	b, err := scanSessionBoard(tx.QueryRowContext(ctx, "SELECT "+boardColumns+" FROM session_boards WHERE id=?", id))
	if err != nil {
		return sb.Inbox{}, err
	}
	if project != "" && b.Project != project {
		return sb.Inbox{}, sb.ErrMissing
	}
	var belongs int
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM session_board_members WHERE board=? AND actor=?", id, actor).Scan(&belongs)
	if errors.Is(err, sql.ErrNoRows) {
		return sb.Inbox{}, sb.ErrDenied
	}
	if err != nil {
		return sb.Inbox{}, err
	}

	query := `SELECT p.seq,p.actor,p.label,p.persona,p.text,p.reply_to,p.at
		FROM session_board_posts p
		WHERE p.board=? AND p.reply_actor=? AND p.actor<>p.reply_actor`
	posts, more, err := querySessionBoardPosts(ctx, tx, query, []any{id, actor}, after, before)
	if err != nil {
		return sb.Inbox{}, err
	}
	return sb.Inbox{Posts: posts, More: more}, tx.Commit()
}

func querySessionBoardPosts(ctx context.Context, q sessionBoardQuery, query string, args []any, after, before int64) ([]sb.Post, bool, error) {
	switch {
	case after > 0:
		query += " AND p.seq>? ORDER BY p.seq ASC"
		args = append(args, after)
	case before > 0:
		query += " AND p.seq<? ORDER BY p.seq DESC"
		args = append(args, before)
	default:
		query += " ORDER BY p.seq DESC"
	}
	rows, err := q.QueryContext(ctx, query+" LIMIT ?", append(args, sb.MaxPageRows+1)...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	posts := []sb.Post{}
	for rows.Next() {
		var p sb.Post
		if err := rows.Scan(&p.Seq, &p.Author.ID, &p.Author.Label, &p.Author.Persona, &p.Text, &p.ReplyTo, &p.At); err != nil {
			return nil, false, err
		}
		posts = append(posts, p)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(posts) > sb.MaxPageRows
	if more {
		posts = posts[:sb.MaxPageRows]
	}
	if after == 0 {
		slices.Reverse(posts)
	}
	return posts, more, nil
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
		if _, err := tx.ExecContext(ctx, "INSERT INTO session_boards(id,project,name,purpose,lead,create_digest,created_at) VALUES(?,?,?,?,?,?,?)",
			id, project, c.Name, c.Text, lead.ID, digest, now.Unix()); err != nil {
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
		var expired bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM session_board_post_keys WHERE board=? AND actor=? AND request_key=?
		)`, b.ID, postActor, key).Scan(&expired); err != nil {
			return result, err
		}
		if expired {
			return result, sb.ErrPostExpired
		}
	}
	if c.Action == "archive" && lead && b.ArchivedAt != 0 {
		return result, nil
	}
	if b.ArchivedAt != 0 {
		return result, sb.ErrClosed
	}

	switch c.Action {
	case "move-members":
		if !person {
			return result, sb.ErrDenied
		}
		err = moveSessionBoardRoster(ctx, tx, c.Source, b, members)
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
		if b.Latest == math.MaxInt64 {
			return result, sb.ErrFull
		}
		if c.ReplyTo < 0 {
			return result, sb.ErrInvalid
		}
		var recipient string
		if c.ReplyTo > 0 {
			err := tx.QueryRowContext(ctx, "SELECT actor FROM session_board_posts WHERE board=? AND seq=?", b.ID, c.ReplyTo).Scan(&recipient)
			if errors.Is(err, sql.ErrNoRows) {
				return result, sb.ErrInvalid
			}
			if err != nil {
				return result, err
			}
		}
		result.Seq = b.Latest + 1
		if _, err := tx.ExecContext(ctx, `INSERT INTO session_board_posts
			(board,seq,actor,label,persona,text,reply_to,reply_actor,at,request_key) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			b.ID, result.Seq, postActor, actor.Label, actor.Persona, c.Text, c.ReplyTo, recipient, now.Unix(), key); err != nil {
			return result, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE session_boards SET latest=? WHERE id=?", result.Seq, b.ID); err != nil {
			return result, err
		}
		if cutoff := result.Seq - sb.MaxPosts; cutoff > 0 {
			// The request identity survives without retaining the post or replaying it.
			if _, err := tx.ExecContext(ctx, `INSERT INTO session_board_post_keys(board,actor,request_key)
				SELECT board,actor,request_key FROM session_board_posts WHERE board=? AND seq<=?`, b.ID, cutoff); err != nil {
				return result, err
			}
			_, err = tx.ExecContext(ctx, "DELETE FROM session_board_posts WHERE board=? AND seq<=?", b.ID, cutoff)
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
		if i := slices.IndexFunc(current, func(member sb.Actor) bool { return member.ID == actor.ID }); i >= 0 {
			if current[i].Persona == "" && actor.Persona != "" {
				if _, err := tx.ExecContext(ctx, "UPDATE session_board_members SET persona=? WHERE board=? AND actor=?", actor.Persona, board, actor.ID); err != nil {
					return err
				}
				current[i].Persona = actor.Persona
			}
			continue
		}
		if len(current) >= sb.MaxMembers {
			return sb.ErrFull
		}
		if _, err := tx.ExecContext(ctx, `UPDATE session_boards SET lead=''
			WHERE id<>? AND archived_at=0 AND lead=?`, board, actor.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM session_board_members WHERE actor=? AND board IN (
			SELECT id FROM session_boards WHERE id<>? AND archived_at=0
		)`, actor.ID, board); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO session_board_members(board,actor,label,persona) VALUES(?,?,?,?)", board, actor.ID, actor.Label, actor.Persona); err != nil {
			return err
		}
		current = append(current, actor)
	}
	return nil
}

// moveSessionBoardRoster carries saved members even if their Sessions are
// offline. Posts and their author identities stay on the source board.
func moveSessionBoardRoster(ctx context.Context, tx *sql.Tx, sourceID string, target sb.Board, current []sb.Actor) error {
	if sourceID == target.ID {
		return sb.ErrInvalid
	}
	source, err := scanSessionBoard(tx.QueryRowContext(ctx, "SELECT "+boardColumns+" FROM session_boards WHERE id=?", sourceID))
	if err != nil {
		return err
	}
	if source.Project != target.Project {
		return sb.ErrDenied
	}
	members, err := sessionBoardMembers(ctx, tx, source.ID)
	if err != nil || len(members) == 0 {
		return err
	}
	if err := insertSessionBoardMembers(ctx, tx, target.ID, current, members); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM session_board_members WHERE board=?", source.ID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE session_boards SET lead='' WHERE id=?", source.ID)
	return err
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
		query = "SELECT coalesce(max(n),0) FROM (SELECT count(*) n FROM session_board_posts GROUP BY board)"
	default:
		return 0, sb.ErrInvalid
	}
	var n int64
	err := s.rd.QueryRowContext(ctx, query).Scan(&n)
	return n, err
}
