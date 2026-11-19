package projectassistantstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"
	"unicode/utf8"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// GroupSchemaVersion is the group tables' own version, separate from the
// feature schema the host already installs.
const GroupSchemaVersion = 1

// GroupInstaller adds the group tables to a database whose feature schema is
// already installed. It is explicit and opt-in: nothing calls it on import or
// at startup until the host's lifecycle wires it. It is transactional and
// repeatable, and refuses an unknown group schema version.
type GroupInstaller struct{}

var _ app.SchemaInstaller = GroupInstaller{}

// Version is the group tables' own version, not pa.SchemaVersion.
func (GroupInstaller) Version() int { return GroupSchemaVersion }

func (GroupInstaller) Install(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var feature int
	if err = tx.QueryRowContext(ctx, `SELECT version FROM project_assistant_schema WHERE id=1`).Scan(&feature); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return failure(pa.Unsupported, "schema_version")
		}
		return err
	}
	if feature != pa.SchemaVersion {
		return failure(pa.Unsupported, "schema_version")
	}
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='project_assistant_group_schema'`).Scan(&exists); err != nil {
		return err
	}
	if exists != 0 {
		var v int
		if err = tx.QueryRowContext(ctx, `SELECT version FROM project_assistant_group_schema WHERE id=1`).Scan(&v); err != nil {
			return err
		}
		if v != GroupSchemaVersion {
			return failure(pa.Unsupported, "group_schema_version")
		}
	}
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS project_assistant_group_schema (id INTEGER PRIMARY KEY CHECK(id=1), version INTEGER NOT NULL)`,
		`INSERT OR IGNORE INTO project_assistant_group_schema(id,version) VALUES(1,1)`,
		`CREATE TABLE IF NOT EXISTS project_assistant_groups (machine TEXT NOT NULL, grp TEXT NOT NULL, project TEXT NOT NULL, deleted INTEGER NOT NULL, version INTEGER NOT NULL CHECK(version>0), data BLOB NOT NULL, PRIMARY KEY(machine,grp))`,
		`CREATE TABLE IF NOT EXISTS project_assistant_group_members (machine TEXT NOT NULL, grp TEXT NOT NULL, bot TEXT NOT NULL, PRIMARY KEY(machine,grp,bot))`,
		`CREATE INDEX IF NOT EXISTS project_assistant_group_member_bot ON project_assistant_group_members(machine,bot)`,
		`CREATE TABLE IF NOT EXISTS project_assistant_group_rooms (machine TEXT NOT NULL, grp TEXT NOT NULL, revision INTEGER NOT NULL, data BLOB NOT NULL, PRIMARY KEY(machine,grp))`,
		`CREATE TABLE IF NOT EXISTS project_assistant_group_lines (machine TEXT NOT NULL, grp TEXT NOT NULL, seq INTEGER NOT NULL CHECK(seq>0), data BLOB NOT NULL, PRIMARY KEY(machine,grp,seq))`,
		groupReactionsSchema,
		`CREATE TABLE IF NOT EXISTS project_assistant_group_receipts (machine TEXT NOT NULL, grp TEXT NOT NULL, operation TEXT NOT NULL, command_key TEXT NOT NULL, data BLOB NOT NULL, PRIMARY KEY(machine,grp,operation,command_key))`,
	} {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GroupStore keeps groups and their room state beside the Bots in the same
// database. Every write runs in one transaction that first takes SQLite's
// write lock, so the member Bots it validates against cannot change before
// it commits.
type GroupStore struct {
	db *sql.DB
	// hook, when set by a test, fails a write at the named stage.
	hook func(stage string) error
}

func NewGroupStore(db *sql.DB) *GroupStore { return &GroupStore{db: db} }

func (s *GroupStore) at(stage string) error {
	if s.hook == nil {
		return nil
	}
	return s.hook(stage)
}

type execer interface {
	reader
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (s *GroupStore) begin(ctx context.Context) (*sql.Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	// Take the write lock before reading anything validation depends on.
	if _, err = tx.ExecContext(ctx, `UPDATE project_assistant_group_schema SET version=version WHERE id=1`); err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

func projectText(p pa.ProjectScope) string {
	b, _ := json.Marshal(p)
	return string(b)
}

func groupRead(ctx context.Context, r reader, key pa.BotKey) (pa.Group, error) {
	var raw []byte
	err := r.QueryRowContext(ctx, `SELECT data FROM project_assistant_groups WHERE machine=? AND grp=?`, key.MachineID, key.BotID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return pa.Group{}, failure(pa.NotFound, "group")
	}
	var g pa.Group
	if err == nil {
		err = json.Unmarshal(raw, &g)
	}
	return g, err
}

func roomRead(ctx context.Context, r reader, key pa.BotKey) (pa.RoomState, error) {
	var raw []byte
	err := r.QueryRowContext(ctx, `SELECT data FROM project_assistant_group_rooms WHERE machine=? AND grp=?`, key.MachineID, key.BotID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return pa.RoomState{Group: key}, nil
	}
	var st pa.RoomState
	if err == nil {
		err = json.Unmarshal(raw, &st)
	}
	return st, err
}

func (s *GroupStore) Group(ctx context.Context, key pa.BotKey) (pa.Group, error) {
	return groupRead(ctx, s.db, key)
}

func (s *GroupStore) Room(ctx context.Context, key pa.BotKey) (pa.RoomState, error) {
	if _, err := groupRead(ctx, s.db, key); err != nil {
		return pa.RoomState{}, err
	}
	return roomRead(ctx, s.db, key)
}

// Groups lists one machine's groups, deleted ones included.
func (s *GroupStore) Groups(ctx context.Context, machine string) ([]pa.Group, error) {
	return groupsRead(ctx, s.db, machine)
}

// GroupPreviews reads real metadata, room state and the newest stored line in
// one SQLite snapshot. It does not ask each room for a history page.
func (s *GroupStore) GroupPreviews(ctx context.Context, machine string, byteBudget int) ([]pa.GroupPreview, error) {
	if byteBudget < 1 {
		return nil, failure(pa.Invalid, "group_preview_bytes")
	}
	// Window only lengths/keys first. Optional data returned to Go is bounded
	// by the caller's existing read budget, even with many large room lines.
	rows, err := s.db.QueryContext(ctx, `WITH sizes AS MATERIALIZED (
 SELECT g.machine,g.grp,l.seq,coalesce(length(r.data),0) room_bytes,coalesce(length(l.data),0) line_bytes
 FROM project_assistant_groups g
 LEFT JOIN project_assistant_group_rooms r ON r.machine=g.machine AND r.grp=g.grp
 LEFT JOIN project_assistant_group_lines l ON l.machine=g.machine AND l.grp=g.grp
  AND l.seq=(SELECT max(seq) FROM project_assistant_group_lines WHERE machine=g.machine AND grp=g.grp)
 WHERE g.machine=?
), fitting AS MATERIALIZED (
 SELECT *,CASE WHEN room_bytes<=? THEN room_bytes ELSE 0 END room_fit,
 CASE WHEN line_bytes<=? THEN line_bytes ELSE 0 END line_fit FROM sizes
), budgeted AS (
 SELECT *,sum(room_fit+line_fit) OVER(ORDER BY grp) spent FROM fitting
)
SELECT g.data,CASE WHEN b.room_fit>0 AND b.spent-b.line_fit<=? THEN r.data END,
 CASE WHEN b.line_fit>0 AND b.spent<=? THEN l.data END
FROM budgeted b JOIN project_assistant_groups g ON g.machine=b.machine AND g.grp=b.grp
LEFT JOIN project_assistant_group_rooms r ON r.machine=b.machine AND r.grp=b.grp
LEFT JOIN project_assistant_group_lines l ON l.machine=b.machine AND l.grp=b.grp AND l.seq=b.seq
ORDER BY g.grp`, machine, byteBudget, byteBudget, byteBudget, byteBudget)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []pa.GroupPreview{}
	for rows.Next() {
		var raw, room, line []byte
		if err = rows.Scan(&raw, &room, &line); err != nil {
			return nil, err
		}
		var p pa.GroupPreview
		if err = json.Unmarshal(raw, &p.Group); err != nil {
			return nil, err
		}
		if len(room) > 0 {
			var st pa.RoomState
			if err = json.Unmarshal(room, &st); err != nil {
				return nil, err
			}
			p.Room = &st
		}
		if len(line) > 0 {
			var e pa.RoomEntry
			if err = json.Unmarshal(line, &e); err != nil {
				return nil, err
			}
			p.LastEntry = &e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func groupsRead(ctx context.Context, r execer, machine string) ([]pa.Group, error) {
	rows, err := r.QueryContext(ctx, `SELECT data FROM project_assistant_groups WHERE machine=? ORDER BY grp`, machine)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pa.Group
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var g pa.Group
		if err = json.Unmarshal(raw, &g); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// catalog reads the stored Bots a member list names and every group key.
func catalog(ctx context.Context, r execer, machine string, keys []pa.BotKey) (pa.GroupCatalog, error) {
	c := pa.GroupCatalog{Bots: map[pa.BotKey]pa.Bot{}, Groups: map[pa.BotKey]bool{}}
	for _, k := range keys {
		if k.MachineID != machine {
			continue
		}
		a, err := load(ctx, r, k)
		if pa.Code(err) == pa.NotFound {
			continue
		}
		if err != nil {
			return c, err
		}
		c.Bots[k] = a.Bot
	}
	rows, err := r.QueryContext(ctx, `SELECT grp FROM project_assistant_groups WHERE machine=?`, machine)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return c, err
		}
		c.Groups[pa.BotKey{MachineID: machine, BotID: id}] = true
	}
	return c, rows.Err()
}

func groupReceipt(ctx context.Context, r reader, c pa.Command) (*pa.Receipt, error) {
	if _, err := pa.DecideReplay(c, nil); err != nil {
		return nil, err
	}
	var raw []byte
	err := r.QueryRowContext(ctx, `SELECT data FROM project_assistant_group_receipts WHERE machine=? AND grp=? AND operation=? AND command_key=?`, c.Key.MachineID, c.Key.BotID, c.Operation, c.IdempotencyKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var x pa.Receipt
	if err = json.Unmarshal(raw, &x); err != nil {
		return nil, err
	}
	_, err = pa.DecideReplay(c, &x)
	return &x, err
}

func receiptID(c pa.Command) string {
	h := sha256.Sum256([]byte(c.Key.MachineID + "\x00" + c.Key.BotID + "\x00" + string(c.Operation) + "\x00" + c.IdempotencyKey))
	return "group-" + hex.EncodeToString(h[:12])
}

func (s *GroupStore) writeGroup(ctx context.Context, tx *sql.Tx, g pa.Group, prior int64) error {
	return writeGroupTx(ctx, tx, g, prior, s.at)
}

func writeGroupTx(ctx context.Context, tx *sql.Tx, g pa.Group, prior int64, hook func(string) error) error {
	at := func(stage string) error {
		if hook == nil {
			return nil
		}
		return hook(stage)
	}
	raw, err := json.Marshal(g)
	if err != nil {
		return err
	}
	if prior == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_groups(machine,grp,project,deleted,version,data) VALUES(?,?,?,?,?,?)`, g.Key.MachineID, g.Key.BotID, projectText(g.Project), g.Deleted, g.Version, raw)
	} else {
		var res sql.Result
		res, err = tx.ExecContext(ctx, `UPDATE project_assistant_groups SET deleted=?,version=?,data=? WHERE machine=? AND grp=? AND version=?`, g.Deleted, g.Version, raw, g.Key.MachineID, g.Key.BotID, prior)
		if err == nil {
			var n int64
			if n, err = res.RowsAffected(); err == nil && n != 1 {
				err = failure(pa.StaleVersion, "group")
			}
		}
	}
	if err != nil {
		return err
	}
	if err = at("group"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM project_assistant_group_members WHERE machine=? AND grp=?`, g.Key.MachineID, g.Key.BotID); err != nil {
		return err
	}
	if !g.Deleted {
		for _, m := range g.Members {
			if _, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_group_members(machine,grp,bot) VALUES(?,?,?)`, g.Key.MachineID, g.Key.BotID, m.Key.BotID); err != nil {
				return err
			}
		}
	}
	return at("members")
}

func (s *GroupStore) writeRoom(ctx context.Context, tx *sql.Tx, st pa.RoomState, prior int64) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE project_assistant_group_rooms SET revision=?,data=? WHERE machine=? AND grp=? AND revision=?`, st.Revision, raw, st.Group.MachineID, st.Group.BotID, prior)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		if prior != 0 {
			return failure(pa.StaleVersion, "room")
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_group_rooms(machine,grp,revision,data) VALUES(?,?,?,?)`, st.Group.MachineID, st.Group.BotID, st.Revision, raw); err != nil {
			return err
		}
	}
	return s.at("room")
}

func (s *GroupStore) finish(ctx context.Context, tx *sql.Tx, c pa.Command, resource string) (pa.Receipt, error) {
	r := pa.Receipt{Command: c, ID: receiptID(c), State: pa.EffectDelivered, ResourceID: resource}
	raw, err := json.Marshal(r)
	if err != nil {
		return r, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_group_receipts(machine,grp,operation,command_key,data) VALUES(?,?,?,?,?)`, c.Key.MachineID, c.Key.BotID, c.Operation, c.IdempotencyKey, raw); err != nil {
		return r, err
	}
	if err = s.at("receipt"); err != nil {
		return r, err
	}
	return r, tx.Commit()
}

// replayed returns the stored receipt for the same command, a conflict for a
// changed payload under the same key, or nil to execute.
func replayed(ctx context.Context, tx *sql.Tx, c pa.Command, op pa.Operation) (*pa.Receipt, error) {
	if c.Operation != op {
		return nil, failure(pa.Invalid, "operation")
	}
	return groupReceipt(ctx, tx, c)
}

// CreateGroup creates d, or, when a live group of the same Project already
// has exactly these members, records a receipt naming that one (Codync's
// rule). The command is keyed by the new group's key.
func (s *GroupStore) CreateGroup(ctx context.Context, c pa.Command, d pa.GroupDraft, limits pa.GroupLimits, at time.Time) (pa.Receipt, error) {
	if c.Key != d.Key {
		return pa.Receipt{}, failure(pa.Invalid, "group_command")
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return pa.Receipt{}, err
	}
	defer tx.Rollback()
	if r, err := replayed(ctx, tx, c, pa.CreateOperation); err != nil || r != nil {
		return deref(r), err
	}
	cat, err := catalog(ctx, tx, d.Key.MachineID, append([]pa.BotKey{d.Key}, d.Members...))
	if err != nil {
		return pa.Receipt{}, err
	}
	g, err := pa.NewGroup(d, cat, limits, at)
	if err != nil {
		return pa.Receipt{}, err
	}
	all, err := groupsRead(ctx, tx, d.Key.MachineID)
	if err != nil {
		return pa.Receipt{}, err
	}
	if existing, ok := pa.GroupWithMembers(all, d.Project, d.Members); ok {
		return s.finish(ctx, tx, c, existing.Key.BotID)
	}
	if err = s.writeGroup(ctx, tx, g, 0); err != nil {
		return pa.Receipt{}, err
	}
	if err = s.writeRoom(ctx, tx, pa.RoomState{Group: g.Key}, 0); err != nil {
		return pa.Receipt{}, err
	}
	return s.finish(ctx, tx, c, g.Key.BotID)
}

func deref(r *pa.Receipt) pa.Receipt {
	if r == nil {
		return pa.Receipt{}
	}
	return *r
}

// EditGroup replaces the name and members under the expected version.
func (s *GroupStore) EditGroup(ctx context.Context, c pa.Command, e pa.GroupEdit, limits pa.GroupLimits, at time.Time) (pa.Receipt, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return pa.Receipt{}, err
	}
	defer tx.Rollback()
	if r, err := replayed(ctx, tx, c, pa.EditOperation); err != nil || r != nil {
		return deref(r), err
	}
	g, err := groupRead(ctx, tx, c.Key)
	if err != nil {
		return pa.Receipt{}, err
	}
	cat, err := catalog(ctx, tx, c.Key.MachineID, e.Members)
	if err != nil {
		return pa.Receipt{}, err
	}
	next, err := pa.EditGroup(g, e, cat, limits, at)
	if err != nil {
		return pa.Receipt{}, err
	}
	if err = s.writeGroup(ctx, tx, next, g.Version); err != nil {
		return pa.Receipt{}, err
	}
	return s.finish(ctx, tx, c, g.Key.BotID)
}

// DeleteGroup tombstones the group and moves its room epoch so nothing asked
// before can publish. Member Bots and their conversations are not touched.
func (s *GroupStore) DeleteGroup(ctx context.Context, c pa.Command, expectedVersion int64, limits pa.GroupLimits, at time.Time) (pa.Receipt, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return pa.Receipt{}, err
	}
	defer tx.Rollback()
	if r, err := replayed(ctx, tx, c, pa.DeleteOperation); err != nil || r != nil {
		return deref(r), err
	}
	g, err := groupRead(ctx, tx, c.Key)
	if err != nil {
		return pa.Receipt{}, err
	}
	next, err := pa.DeleteGroup(g, expectedVersion, at)
	if err != nil {
		return pa.Receipt{}, err
	}
	if err = s.writeGroup(ctx, tx, next, g.Version); err != nil {
		return pa.Receipt{}, err
	}
	st, err := roomRead(ctx, tx, c.Key)
	if err != nil {
		return pa.Receipt{}, err
	}
	if stopped, ok, err := pa.StopRoom(st, limits); err != nil {
		return pa.Receipt{}, err
	} else if ok {
		if err = s.writeRoom(ctx, tx, stopped, st.Revision); err != nil {
			return pa.Receipt{}, err
		}
	}
	return s.finish(ctx, tx, c, g.Key.BotID)
}

// LeaveGroups takes a deleted Bot out of every live group on its machine in
// its own transaction. Store.DeleteBot uses leaveGroupsTx instead, so the Bot
// row and its memberships go together.
func (s *GroupStore) LeaveGroups(ctx context.Context, member pa.BotKey, at time.Time) ([]pa.BotKey, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	left, err := leaveGroupsTx(ctx, tx, member, at, s.at)
	if err != nil {
		return nil, err
	}
	return left, tx.Commit()
}

func leaveGroupsTx(ctx context.Context, tx *sql.Tx, member pa.BotKey, at time.Time, hook func(string) error) ([]pa.BotKey, error) {
	all, err := groupsRead(ctx, tx, member.MachineID)
	if err != nil {
		return nil, err
	}
	var left []pa.BotKey
	for _, g := range all {
		next, ok := pa.LeaveGroup(g, member, at)
		if !ok {
			continue
		}
		if err = writeGroupTx(ctx, tx, next, g.Version, hook); err != nil {
			return nil, err
		}
		left = append(left, g.Key)
	}
	return left, nil
}

// groupsInstalled reports whether the group tables exist; a database whose
// host has not installed them yet has no groups to leave or export.
func groupsInstalled(ctx context.Context, r reader) (bool, error) {
	var n int
	err := r.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='project_assistant_group_schema'`).Scan(&n)
	return n != 0, err
}

// UpdateRoom applies one pure room transition under the stored revision: the
// transition sees the group and room as stored now, and a concurrent change
// makes the write refuse instead of overwriting.
func (s *GroupStore) UpdateRoom(ctx context.Context, key pa.BotKey, expectedRevision int64, step func(pa.Group, pa.RoomState) (pa.RoomState, error)) (pa.RoomState, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return pa.RoomState{}, err
	}
	defer tx.Rollback()
	g, err := groupRead(ctx, tx, key)
	if err != nil {
		return pa.RoomState{}, err
	}
	if g.Deleted {
		return pa.RoomState{}, failure(pa.NotFound, "group")
	}
	st, err := roomRead(ctx, tx, key)
	if err != nil {
		return pa.RoomState{}, err
	}
	if st.Revision != expectedRevision {
		return st, failure(pa.StaleVersion, "room")
	}
	next, err := step(g, st)
	if err != nil {
		return st, err
	}
	if next.Group != key {
		return st, failure(pa.Invalid, "room")
	}
	if next.Revision == st.Revision {
		return st, nil
	}
	if next.Revision < st.Revision {
		return st, failure(pa.Invalid, "room_revision")
	}
	if err = s.writeRoom(ctx, tx, next, st.Revision); err != nil {
		return st, err
	}
	return next, tx.Commit()
}

// CheckTicket reads the group, the room and the member's Bot together and
// checks a room ticket: before the member's turn runs and before its answer
// is published. A member whose Bot was deleted is revoked.
func (s *GroupStore) CheckTicket(ctx context.Context, t pa.RoomTicket) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	g, err := groupRead(ctx, tx, t.Group)
	if err != nil {
		return err
	}
	st, err := roomRead(ctx, tx, t.Group)
	if err != nil {
		return err
	}
	if err = pa.CheckRoomTicket(g, st, t); err != nil {
		return err
	}
	a, err := load(ctx, tx, t.Member)
	if pa.Code(err) == pa.NotFound {
		return failure(pa.Revoked, "group_member")
	}
	if err != nil {
		return err
	}
	if a.Bot.Project != g.Project {
		return failure(pa.ScopeDenied, "group_member_project")
	}
	return nil
}

// readMemberGroups is the export's view of the groups key belongs to, live
// or deleted while it was a member, with their room state.
func readMemberGroups(ctx context.Context, tx *sql.Tx, key pa.BotKey) ([]app.GroupExport, error) {
	installed, err := groupsInstalled(ctx, tx)
	if err != nil || !installed {
		return nil, err
	}
	all, err := groupsRead(ctx, tx, key.MachineID)
	if err != nil {
		return nil, err
	}
	var out []app.GroupExport
	for _, g := range all {
		if _, ok := g.Member(key); !ok {
			continue
		}
		st, err := roomRead(ctx, tx, g.Key)
		if err != nil {
			return nil, err
		}
		out = append(out, app.GroupExport{Group: g, Room: st})
	}
	return out, nil
}

var _ app.RoomRepository = (*Store)(nil)

func nextLineSeq(ctx context.Context, r reader, key pa.BotKey) (int64, error) {
	var seq int64
	err := r.QueryRowContext(ctx, `SELECT coalesce(max(seq),0) FROM project_assistant_group_lines WHERE machine=? AND grp=?`, key.MachineID, key.BotID).Scan(&seq)
	return seq + 1, err
}

func writeLine(ctx context.Context, tx *sql.Tx, group pa.BotKey, e pa.RoomEntry) error {
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_group_lines(machine,grp,seq,data) VALUES(?,?,?,?)`, group.MachineID, group.BotID, e.Seq, raw)
	return err
}

func writeRoomTx(ctx context.Context, tx *sql.Tx, st pa.RoomState, prior int64) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE project_assistant_group_rooms SET revision=?,data=? WHERE machine=? AND grp=? AND revision=?`, st.Revision, raw, st.Group.MachineID, st.Group.BotID, prior)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return errors.Join(err, failure(pa.StaleVersion, "room"))
	}
	return nil
}

// FinishRoomTurn commits a member's terminal room turn and, in the same
// transaction, re-checks its ticket against the group, the room and the
// member's own row and writes the room line. A ticket that no longer holds
// withholds the line and still finishes the member's turn. A failed line
// write rolls back everything, the member's terminal facts included.
func (s *Store) FinishRoomTurn(ctx context.Context, m app.Mutation, text string) (app.RoomPublication, error) {
	if m.Turn == nil || m.Turn.Room == nil {
		return app.RoomPublication{}, failure(pa.Invalid, "room_finish")
	}
	t := *m.Turn
	out := app.RoomPublication{Ticket: *t.Room, TurnID: t.ID, State: t.State}
	unlock := s.lock(m.Command.Key)
	defer unlock()
	_, err := s.commitWith(ctx, m, func(ctx context.Context, tx *sql.Tx, a aggregate) error {
		if t.State != pa.TurnSucceeded {
			return nil
		}
		ticket := *t.Room
		if a.Bot.Key != ticket.Member {
			return failure(pa.ScopeDenied, "room_member")
		}
		g, err := groupRead(ctx, tx, ticket.Group)
		if err != nil {
			out.Refusal = &pa.Failure{Code: pa.Code(err), Field: "group"}
			return nil
		}
		st, err := roomRead(ctx, tx, ticket.Group)
		if err != nil {
			return err
		}
		if g.Project != a.Bot.Project {
			out.Refusal = &pa.Failure{Code: pa.ScopeDenied, Field: "group_member_project"}
			return nil
		}
		seq, err := nextLineSeq(ctx, tx, ticket.Group)
		if err != nil {
			return err
		}
		entry, ok, err := pa.PublishRoomAnswer(g, st, ticket, seq, text)
		if err != nil {
			var f *pa.Failure
			if errors.As(err, &f) {
				out.Refusal = f
				return nil
			}
			return err
		}
		next, _ := pa.Landed(st, ticket)
		replySeq := int64(0)
		if ok {
			replySeq = entry.Seq
		}
		next = pa.RecordRoomPublication(g, next, ticket, t.ID, replySeq)
		if next.Revision != st.Revision {
			if err = writeRoomTx(ctx, tx, next, st.Revision); err != nil {
				return err
			}
		}
		if !ok {
			return nil
		}
		// The terminal mutation carries the runtime's source clock.
		entry.At = a.Bot.UpdatedAt.UTC()
		if err = writeLine(ctx, tx, ticket.Group, entry); err != nil {
			return err
		}
		if s.groupHook != nil {
			if err = s.groupHook("line"); err != nil {
				return err
			}
		}
		out.Entry = &entry
		return nil
	})
	if err != nil {
		return app.RoomPublication{}, err
	}
	return out, nil
}

// PersonWrote writes the person's line and starts the room turn it asks
// for, in one transaction under the room revision, with the command's
// receipt. A replay returns the stored line's seq and starts nothing.
func (s *GroupStore) PersonWrote(ctx context.Context, c pa.Command, expectedRevision int64, text string, replyToAuthor pa.BotKey, quoted ...int64) (pa.RoomEntry, pa.RoomState, *pa.RoomTurn, error) {
	if len(quoted) > 1 || len(quoted) == 1 && quoted[0] < 1 {
		return pa.RoomEntry{}, pa.RoomState{}, nil, failure(pa.Invalid, "quote_seq")
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return pa.RoomEntry{}, pa.RoomState{}, nil, err
	}
	defer tx.Rollback()
	key := c.Key
	if r, err := replayed(ctx, tx, c, pa.SendOperation); err != nil {
		return pa.RoomEntry{}, pa.RoomState{}, nil, err
	} else if r != nil {
		seq, _ := strconv.ParseInt(r.ResourceID, 10, 64)
		st, err := roomRead(ctx, tx, key)
		if err != nil {
			return pa.RoomEntry{}, st, nil, err
		}
		entry, err := roomLine(ctx, tx, key, seq)
		return entry, st, nil, err
	}
	g, err := groupRead(ctx, tx, key)
	if err != nil {
		return pa.RoomEntry{}, pa.RoomState{}, nil, err
	}
	var quote *pa.RoomQuote
	if len(quoted) == 1 {
		original, e := roomLine(ctx, tx, key, quoted[0])
		if e != nil {
			return pa.RoomEntry{}, pa.RoomState{}, nil, e
		}
		if original.Kind != pa.RoomPersonEntry && original.Kind != pa.RoomMemberEntry {
			return pa.RoomEntry{}, pa.RoomState{}, nil, failure(pa.Invalid, "quote_seq")
		}
		if replyToAuthor != (pa.BotKey{}) && replyToAuthor != original.Author {
			return pa.RoomEntry{}, pa.RoomState{}, nil, failure(pa.Invalid, "reply_to_author")
		}
		quote = &pa.RoomQuote{Seq: original.Seq, Kind: original.Kind, Author: original.Author, Text: original.Text}
		// A retained excerpt uses the existing person-message byte budget.
		// The immutable source line remains available at its exact sequence.
		if len(quote.Text) > pa.MaxMessageBytes {
			end := pa.MaxMessageBytes
			for !utf8.RuneStart(quote.Text[end]) {
				end--
			}
			quote.Text, quote.Truncated = quote.Text[:end], true
		}
		replyToAuthor = original.Author
	} else if replyToAuthor != (pa.BotKey{}) {
		if _, ok := g.Member(replyToAuthor); !ok {
			return pa.RoomEntry{}, pa.RoomState{}, nil, failure(pa.Invalid, "reply_to_author")
		}
	}
	st, err := roomRead(ctx, tx, key)
	if err != nil {
		return pa.RoomEntry{}, pa.RoomState{}, nil, err
	}
	if st.Revision != expectedRevision {
		return pa.RoomEntry{}, st, nil, failure(pa.StaleVersion, "room")
	}
	next, turn, err := pa.StartRoomTurn(g, st)
	if err != nil {
		return pa.RoomEntry{}, st, nil, err
	}
	seq, err := nextLineSeq(ctx, tx, key)
	if err != nil {
		return pa.RoomEntry{}, st, nil, err
	}
	entry := pa.RoomEntry{Seq: seq, Kind: pa.RoomPersonEntry, At: time.Now().UTC(), Text: text, ReplyToAuthor: replyToAuthor, Quote: quote}
	if err = writeLine(ctx, tx, key, entry); err != nil {
		return pa.RoomEntry{}, st, nil, err
	}
	if err = s.at("line"); err != nil {
		return pa.RoomEntry{}, st, nil, err
	}
	if err = s.writeRoom(ctx, tx, next, st.Revision); err != nil {
		return pa.RoomEntry{}, st, nil, err
	}
	if _, err = s.finish(ctx, tx, c, strconv.FormatInt(seq, 10)); err != nil {
		return pa.RoomEntry{}, st, nil, err
	}
	return entry, next, &turn, nil
}

func roomLine(ctx context.Context, r reader, key pa.BotKey, seq int64) (pa.RoomEntry, error) {
	var raw []byte
	err := r.QueryRowContext(ctx, `SELECT data FROM project_assistant_group_lines WHERE machine=? AND grp=? AND seq=?`, key.MachineID, key.BotID, seq).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return pa.RoomEntry{}, failure(pa.NotFound, "quote_seq")
	}
	if err != nil {
		return pa.RoomEntry{}, err
	}
	var entry pa.RoomEntry
	err = json.Unmarshal(raw, &entry)
	return entry, err
}

// Lines reads a room's lines with seq > after, oldest first, at most limit;
// limit < 1 reads the newest page before before (when before > 0).
func (s *GroupStore) Lines(ctx context.Context, key pa.BotKey, after int64, limit int) ([]pa.RoomEntry, error) {
	if limit < 1 {
		return nil, failure(pa.Invalid, "room_lines")
	}
	if _, err := groupRead(ctx, s.db, key); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT data FROM project_assistant_group_lines WHERE machine=? AND grp=? AND seq>? ORDER BY seq LIMIT ?`, key.MachineID, key.BotID, after, limit)
	if err != nil {
		return nil, err
	}
	return scanLines(rows)
}

// LinesBefore reads the newest limit lines with seq < before (all when
// before is 0), oldest first: the room's history page.
func (s *GroupStore) LinesBefore(ctx context.Context, key pa.BotKey, before int64, limit int) ([]pa.RoomEntry, error) {
	if limit < 1 {
		return nil, failure(pa.Invalid, "room_lines")
	}
	if _, err := groupRead(ctx, s.db, key); err != nil {
		return nil, err
	}
	return linesBefore(ctx, s.db, key, before, limit)
}

func linesBefore(ctx context.Context, r execer, key pa.BotKey, before int64, limit int) ([]pa.RoomEntry, error) {
	if before < 1 {
		before = 1 << 62
	}
	rows, err := r.QueryContext(ctx, `SELECT data FROM (SELECT seq,data FROM project_assistant_group_lines WHERE machine=? AND grp=? AND seq<? ORDER BY seq DESC LIMIT ?) ORDER BY seq`, key.MachineID, key.BotID, before, limit)
	if err != nil {
		return nil, err
	}
	return scanLines(rows)
}

func scanLines(rows *sql.Rows) ([]pa.RoomEntry, error) {
	defer rows.Close()
	var out []pa.RoomEntry
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var e pa.RoomEntry
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
