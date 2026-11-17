package projectassistant

import (
	"context"
	"database/sql"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// InstallSchemas installs the feature schema and then the group tables, each
// in its own repeatable transaction. The host calls it wherever it installed
// the feature schema alone; the group installer refuses to run before the
// feature schema exists, so the order is part of the contract.
func InstallSchemas(ctx context.Context, db *sql.DB, feature, groups SchemaInstaller) error {
	if db == nil || feature == nil || groups == nil {
		return serviceFailure(pa.Invalid, "schema_installers")
	}
	if err := feature.Install(ctx, db); err != nil {
		return err
	}
	return groups.Install(ctx, db)
}

// GroupRoomStore is the persistence a group room needs. The SQLite
// implementation is projectassistantstore.GroupStore; it is not part of the
// Repository port until the host wires groups.
type GroupRoomStore interface {
	CreateGroup(context.Context, pa.Command, pa.GroupDraft, pa.GroupLimits, time.Time) (pa.Receipt, error)
	EditGroup(context.Context, pa.Command, pa.GroupEdit, pa.GroupLimits, time.Time) (pa.Receipt, error)
	DeleteGroup(context.Context, pa.Command, int64, pa.GroupLimits, time.Time) (pa.Receipt, error)
	Group(context.Context, pa.BotKey) (pa.Group, error)
	Room(context.Context, pa.BotKey) (pa.RoomState, error)
	UpdateRoom(context.Context, pa.BotKey, int64, func(pa.Group, pa.RoomState) (pa.RoomState, error)) (pa.RoomState, error)
	CheckTicket(context.Context, pa.RoomTicket) error
	// PersonWrote writes the person's line and starts its room turn in one
	// transaction under the room revision.
	PersonWrote(context.Context, pa.Command, int64, string, pa.BotKey, ...int64) (pa.RoomEntry, pa.RoomState, *pa.RoomTurn, error)
	Lines(context.Context, pa.BotKey, int64, int) ([]pa.RoomEntry, error)
	LinesBefore(context.Context, pa.BotKey, int64, int) ([]pa.RoomEntry, error)
	Groups(context.Context, string) ([]pa.Group, error)
	GroupPreviews(context.Context, string, int) ([]pa.GroupPreview, error)
	History(context.Context, pa.BotKey, int64, int) (GroupHistoryPage, error)
}

// GroupRooms coordinates group rooms: who is asked, which answers are still
// wanted. It never runs a turn. Each member's turn runs through the member's
// ordinary path under its own fence; a room ticket is an extra condition on
// that turn, never a substitute for it.
type GroupRooms struct {
	store  GroupRoomStore
	limits pa.GroupLimits
	clock  func() time.Time
}

func NewGroupRooms(store GroupRoomStore, limits pa.GroupLimits, clock func() time.Time) (*GroupRooms, error) {
	if store == nil || clock == nil {
		return nil, serviceFailure(pa.Invalid, "group_rooms")
	}
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	return &GroupRooms{store: store, limits: limits, clock: clock}, nil
}

// PersonAuthority is the person's already-authorized write, as the host's
// existing authorization produces it. A Bot's model identity never yields one.
type PersonAuthority struct {
	Principal string
	Machine   string
}

func (a PersonAuthority) check(key pa.BotKey) error {
	if a.Principal == "" || a.Machine == "" {
		return serviceFailure(pa.ScopeDenied, "group_authority")
	}
	if key.MachineID != a.Machine {
		return serviceFailure(pa.ScopeDenied, "group_machine")
	}
	return nil
}

func (r *GroupRooms) Create(ctx context.Context, who PersonAuthority, c pa.Command, d pa.GroupDraft) (pa.Receipt, error) {
	if err := who.check(d.Key); err != nil {
		return pa.Receipt{}, err
	}
	// The person's intent, not the host-generated conversation ID, so a
	// retried create replays instead of conflicting.
	c, err := canonicalCommand(c, pa.CreateOperation, who.Principal, struct {
		Project pa.ProjectScope `json:"project"`
		Name    string          `json:"name"`
		Members []pa.BotKey     `json:"members"`
	}{d.Project, d.Name, d.Members})
	if err != nil {
		return pa.Receipt{}, err
	}
	return r.store.CreateGroup(ctx, c, d, r.limits, r.clock())
}

func (r *GroupRooms) Edit(ctx context.Context, who PersonAuthority, c pa.Command, e pa.GroupEdit) (pa.Receipt, error) {
	if err := who.check(c.Key); err != nil {
		return pa.Receipt{}, err
	}
	c, err := canonicalCommand(c, pa.EditOperation, who.Principal, e)
	if err != nil {
		return pa.Receipt{}, err
	}
	return r.store.EditGroup(ctx, c, e, r.limits, r.clock())
}

func (r *GroupRooms) Delete(ctx context.Context, who PersonAuthority, c pa.Command, expectedVersion int64) (pa.Receipt, error) {
	if err := who.check(c.Key); err != nil {
		return pa.Receipt{}, err
	}
	c, err := canonicalCommand(c, pa.DeleteOperation, who.Principal, expectedVersion)
	if err != nil {
		return pa.Receipt{}, err
	}
	return r.store.DeleteGroup(ctx, c, expectedVersion, r.limits, r.clock())
}

// RoomStep is a room transition's stored result: the new state and the room
// turn to run now, if any.
type RoomStep struct {
	State pa.RoomState
	Turn  *pa.RoomTurn
}

func (r *GroupRooms) step(ctx context.Context, key pa.BotKey, revision int64, f func(pa.Group, pa.RoomState) (pa.RoomState, *pa.RoomTurn, error)) (RoomStep, error) {
	var turn *pa.RoomTurn
	st, err := r.store.UpdateRoom(ctx, key, revision, func(g pa.Group, s pa.RoomState) (pa.RoomState, error) {
		next, t, err := f(g, s)
		turn = t
		return next, err
	})
	return RoomStep{State: st, Turn: turn}, err
}

// Send writes the person's message into the room and starts the room turn it
// asks for, replacing the one running. replyToAuthor is the stored author of
// a quoted line, or zero. A replayed command starts nothing (Turn nil).
func (r *GroupRooms) Send(ctx context.Context, who PersonAuthority, c pa.Command, revision int64, text string, replyToAuthor pa.BotKey, quoted ...int64) (pa.RoomEntry, RoomStep, error) {
	if err := who.check(c.Key); err != nil {
		return pa.RoomEntry{}, RoomStep{}, err
	}
	if err := pa.ValidateMessage(text); err != nil {
		return pa.RoomEntry{}, RoomStep{}, err
	}
	var quoteSeq int64
	if len(quoted) > 1 || len(quoted) == 1 && quoted[0] < 1 {
		return pa.RoomEntry{}, RoomStep{}, serviceFailure(pa.Invalid, "quote_seq")
	}
	if len(quoted) == 1 {
		quoteSeq = quoted[0]
	}
	c, err := canonicalCommand(c, pa.SendOperation, who.Principal, struct {
		Text          string    `json:"text"`
		ReplyToAuthor pa.BotKey `json:"reply_to_author"`
		QuoteSeq      int64     `json:"quote_seq,omitempty"`
	}{text, replyToAuthor, quoteSeq})
	if err != nil {
		return pa.RoomEntry{}, RoomStep{}, err
	}
	e, st, t, err := r.store.PersonWrote(ctx, c, revision, text, replyToAuthor, quoted...)
	return e, RoomStep{State: st, Turn: t}, err
}

// MarkOverdue: the room stops waiting for t; its answer still lands late.
func (r *GroupRooms) MarkOverdue(ctx context.Context, t pa.RoomTicket, revision int64) (RoomStep, error) {
	return r.step(ctx, t.Group, revision, func(_ pa.Group, s pa.RoomState) (pa.RoomState, *pa.RoomTurn, error) {
		return pa.MarkOverdue(s, t), nil, nil
	})
}

// PersonWrote starts the room turn for the person's new message, replacing
// the one running.
func (r *GroupRooms) PersonWrote(ctx context.Context, key pa.BotKey, revision int64) (RoomStep, error) {
	return r.step(ctx, key, revision, func(g pa.Group, s pa.RoomState) (pa.RoomState, *pa.RoomTurn, error) {
		next, t, err := pa.StartRoomTurn(g, s)
		return next, &t, err
	})
}

func (r *GroupRooms) LateAnswer(ctx context.Context, key pa.BotKey, revision, seq int64) (RoomStep, error) {
	return r.step(ctx, key, revision, func(_ pa.Group, s pa.RoomState) (pa.RoomState, *pa.RoomTurn, error) {
		return pa.LateAnswer(s, seq, r.limits)
	})
}

func (r *GroupRooms) TurnEnded(ctx context.Context, key pa.BotKey, revision, epoch int64) (RoomStep, error) {
	return r.step(ctx, key, revision, func(_ pa.Group, s pa.RoomState) (pa.RoomState, *pa.RoomTurn, error) {
		return pa.RoomTurnEnded(s, epoch, r.limits)
	})
}

// GroupStop is the group's Stop and the members whose room turns in this
// group the host must cancel: queued ones dropped, an active one in this room
// stopped. Their turns elsewhere are untouched.
type GroupStop struct {
	State   pa.RoomState
	Stopped bool
	Cancel  []pa.BotKey
}

func (r *GroupRooms) Stop(ctx context.Context, key pa.BotKey, revision int64) (GroupStop, error) {
	var out GroupStop
	st, err := r.store.UpdateRoom(ctx, key, revision, func(g pa.Group, s pa.RoomState) (pa.RoomState, error) {
		next, stopped, err := pa.StopRoom(s, r.limits)
		out.Stopped = stopped
		if stopped {
			out.Cancel = g.MemberKeys()
		}
		return next, err
	})
	out.State = st
	return out, err
}

// Ask hands a member the ticket for the running room turn after checking it
// against stored state, so a removed member or a superseded turn is refused.
func (r *GroupRooms) Ask(ctx context.Context, key, member pa.BotKey, late bool) (pa.RoomTicket, error) {
	g, err := r.store.Group(ctx, key)
	if err != nil {
		return pa.RoomTicket{}, err
	}
	st, err := r.store.Room(ctx, key)
	if err != nil {
		return pa.RoomTicket{}, err
	}
	t, err := pa.TicketFor(g, st, member, late)
	if err != nil {
		return t, err
	}
	return t, r.store.CheckTicket(ctx, t)
}

// Admit is checked when a member's queued room turn is about to run.
func (r *GroupRooms) Admit(ctx context.Context, t pa.RoomTicket) error {
	return r.store.CheckTicket(ctx, t)
}

// Publish checks a member's final answer against current state and returns
// the room line, authored by the ticket's member. ok is false for a pass. The
// host still has to write the line in the same transaction as a final ticket
// check; this read-check alone does not make the write atomic.
func (r *GroupRooms) Publish(ctx context.Context, t pa.RoomTicket, seq int64, text string) (pa.RoomEntry, bool, error) {
	if err := r.store.CheckTicket(ctx, t); err != nil {
		return pa.RoomEntry{}, false, err
	}
	g, err := r.store.Group(ctx, t.Group)
	if err != nil {
		return pa.RoomEntry{}, false, err
	}
	st, err := r.store.Room(ctx, t.Group)
	if err != nil {
		return pa.RoomEntry{}, false, err
	}
	return pa.PublishRoomAnswer(g, st, t, seq, text)
}
