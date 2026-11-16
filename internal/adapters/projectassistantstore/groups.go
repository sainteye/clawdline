package projectassistantstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

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
	if err = s.at("group"); err != nil {
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
	return s.at("members")
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

// LeaveGroups takes a deleted Bot out of every live group on its machine. The
// host's Bot deletion calls it after the Bot row is gone.
func (s *GroupStore) LeaveGroups(ctx context.Context, member pa.BotKey, at time.Time) ([]pa.BotKey, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
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
		if err = s.writeGroup(ctx, tx, next, g.Version); err != nil {
			return nil, err
		}
		left = append(left, g.Key)
	}
	return left, tx.Commit()
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
