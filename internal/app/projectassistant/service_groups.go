package projectassistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// CreatePersonGroup assigns the source-owned group key at the human HTTP
// boundary. The durable principal and explicit operation key make retries
// stable, including a retry from another device; no model chooses this ID.
// CreateGroup itself remains the trusted host port used by coordinators.
func (s *Service) CreatePersonGroup(ctx context.Context, c pa.Command, in CreateGroup) (pa.Group, pa.Receipt, error) {
	return s.createGroup(ctx, c, in, true)
}

// The person's group operations. Each is authorized with the existing human
// authority for the group's key (Service.authorize), and re-checked with
// authorizeEffect immediately before its store write, as the Bot operations
// are. A group key is source-qualified like a BotKey; it names no Bot. The
// member Bots' own authority is not consulted to read the room: reading the
// room is the person's read of the group, separate from member execution.

// CreateGroup is the person's new group. The command's key is the new
// group's host-generated key; the conversation ID is host-generated too.
type CreateGroup struct {
	Target pa.ProjectSelection `json:"target"`
	Name   string              `json:"name"`
	// Description says what the group works on; empty when none was given.
	Description string      `json:"description,omitempty"`
	Members     []pa.BotKey `json:"members"`
}

// GroupHistoryQuery pages the room's lines, newest page first: BeforeSeq 0
// is the newest page.
type GroupHistoryQuery struct {
	Key       pa.BotKey
	BeforeSeq int64
	PageSize  int
}

type GroupHistoryPage struct {
	Group          pa.Group
	Room           pa.RoomState
	Lines          []pa.RoomEntry
	Reactions      map[int64][]pa.ReactionActor
	Activity       []pa.RoomActivity
	MemberOutcomes []pa.RoomMemberOutcome
}

func (s *Service) groups() (*GroupRooms, *RoomDriver, error) {
	if s.opts.Groups == nil || s.opts.Rooms == nil {
		return nil, nil, serviceFailure(pa.Unsupported, "groups")
	}
	return s.opts.Groups, s.opts.Rooms, nil
}

func person(a Authority, machine string) PersonAuthority {
	return PersonAuthority{Principal: a.PrincipalID, Machine: machine}
}

func (s *Service) CreateGroup(ctx context.Context, c pa.Command, in CreateGroup) (pa.Group, pa.Receipt, error) {
	return s.createGroup(ctx, c, in, false)
}

func (s *Service) createGroup(ctx context.Context, c pa.Command, in CreateGroup, assignKey bool) (pa.Group, pa.Receipt, error) {
	rooms, _, err := s.groups()
	if err != nil {
		return pa.Group{}, pa.Receipt{}, err
	}
	a, err := s.authorize(ctx, c.Key, true)
	if err != nil {
		return pa.Group{}, pa.Receipt{}, err
	}
	if assignKey {
		if c.IdempotencyKey == "" {
			return pa.Group{}, pa.Receipt{}, serviceFailure(pa.Invalid, "idempotency_key")
		}
		digest := sha256.Sum256([]byte(a.PrincipalID + "\x00" + c.Key.MachineID + "\x00" + c.IdempotencyKey))
		c.Key.BotID = "group-" + hex.EncodeToString(digest[:])
	}
	if in.Target.MachineID != c.Key.MachineID {
		return pa.Group{}, pa.Receipt{}, serviceFailure(pa.ScopeDenied, "target_machine")
	}
	if pa.IsPersonalWorkspaceID(in.Target.ProjectID) {
		return pa.Group{}, pa.Receipt{}, serviceFailure(pa.ScopeDenied, "group_project")
	}
	var scope pa.ProjectScope
	if in.Target.ProjectID == "" {
		scope = pa.PersonalGroupScope(c.Key.MachineID)
	} else {
		resolved, err := s.opts.Projects.Resolve(ctx, in.Target)
		if err != nil {
			return pa.Group{}, pa.Receipt{}, err
		}
		if err = pa.ValidateTarget(in.Target, resolved); err != nil {
			return pa.Group{}, pa.Receipt{}, err
		}
		scope = resolved.Scope
	}
	d := pa.GroupDraft{Key: c.Key, Project: scope, ConversationID: s.opts.ID(), Name: in.Name, Description: in.Description, Members: in.Members}
	if a, err = s.authorizeEffect(ctx, c.Key, true, a); err != nil {
		return pa.Group{}, pa.Receipt{}, err
	}
	r, err := rooms.Create(ctx, person(a, c.Key.MachineID), c, d)
	if err != nil {
		return pa.Group{}, r, err
	}
	// Creating the same Bots again opens the group they already share.
	g, err := rooms.store.Group(ctx, pa.BotKey{MachineID: c.Key.MachineID, BotID: r.ResourceID})
	return g, r, err
}

func (s *Service) EditGroup(ctx context.Context, c pa.Command, e pa.GroupEdit) (pa.Group, pa.Receipt, error) {
	rooms, _, err := s.groups()
	if err != nil {
		return pa.Group{}, pa.Receipt{}, err
	}
	a, err := s.authorize(ctx, c.Key, true)
	if err != nil {
		return pa.Group{}, pa.Receipt{}, err
	}
	if a, err = s.authorizeEffect(ctx, c.Key, true, a); err != nil {
		return pa.Group{}, pa.Receipt{}, err
	}
	r, err := rooms.Edit(ctx, person(a, c.Key.MachineID), c, e)
	if err != nil {
		return pa.Group{}, r, err
	}
	g, err := rooms.store.Group(ctx, c.Key)
	return g, r, err
}

// DeleteGroup tombstones the group after stopping its room: members' queued
// and active turns in this room end; the member Bots and their own
// conversations are untouched.
func (s *Service) DeleteGroup(ctx context.Context, c pa.Command, expectedVersion int64) (pa.Receipt, error) {
	rooms, driver, err := s.groups()
	if err != nil {
		return pa.Receipt{}, err
	}
	a, err := s.authorize(ctx, c.Key, true)
	if err != nil {
		return pa.Receipt{}, err
	}
	if a, err = s.authorizeEffect(ctx, c.Key, true, a); err != nil {
		return pa.Receipt{}, err
	}
	who := person(a, c.Key.MachineID)
	r, err := rooms.Delete(ctx, who, c, expectedVersion)
	if err != nil {
		return r, err
	}
	// The store already moved the epoch; stop what members still run here.
	g, gerr := rooms.store.Group(ctx, c.Key)
	if gerr == nil {
		driver.cancelMembers(ctx, c.Key, g.MemberKeys())
	}
	return r, nil
}

// StopGroup is the group's Stop button.
func (s *Service) StopGroup(ctx context.Context, key pa.BotKey) (GroupStop, error) {
	_, driver, err := s.groups()
	if err != nil {
		return GroupStop{}, err
	}
	a, err := s.authorize(ctx, key, true)
	if err != nil {
		return GroupStop{}, err
	}
	if a, err = s.authorizeEffect(ctx, key, true, a); err != nil {
		return GroupStop{}, err
	}
	return driver.Stop(ctx, person(a, key.MachineID), key)
}

// SendGroup is the person's message into the room. The authority is checked
// again after the room read and immediately before the write.
func (s *Service) SendGroup(ctx context.Context, c pa.Command, text string, replyToAuthor pa.BotKey, quoted ...int64) (pa.RoomEntry, RoomStep, error) {
	_, driver, err := s.groups()
	if err != nil {
		return pa.RoomEntry{}, RoomStep{}, err
	}
	a, err := s.authorize(ctx, c.Key, true)
	if err != nil {
		return pa.RoomEntry{}, RoomStep{}, err
	}
	if err = pa.ValidateMessage(text); err != nil {
		return pa.RoomEntry{}, RoomStep{}, err
	}
	check := func(ctx context.Context) error {
		_, e := s.authorizeEffect(ctx, c.Key, true, a)
		return e
	}
	return driver.Send(ctx, person(a, c.Key.MachineID), c, text, replyToAuthor, check, quoted...)
}

// GroupHistory reads the room with the person's read authority for the
// group; it does not require, and does not use, any member's execution.
func (s *Service) GroupHistory(ctx context.Context, q GroupHistoryQuery) (GroupHistoryPage, error) {
	rooms, _, err := s.groups()
	if err != nil {
		return GroupHistoryPage{}, err
	}
	a, err := s.authorize(ctx, q.Key, false)
	if err != nil {
		return GroupHistoryPage{}, err
	}
	if err = pa.CheckPage(q.PageSize, false); err != nil {
		return GroupHistoryPage{}, err
	}
	if q.BeforeSeq < 0 {
		return GroupHistoryPage{}, serviceFailure(pa.Invalid, "before_seq")
	}
	page, err := rooms.store.History(ctx, q.Key, q.BeforeSeq, q.PageSize)
	if err != nil {
		return GroupHistoryPage{}, err
	}
	reader := pa.HumanReactionID(a.ReaderID)
	for _, reactions := range page.Reactions {
		for j := range reactions {
			reaction := &reactions[j]
			reaction.Own = reaction.Kind == "human" && reaction.ID == reader
		}
	}
	return page, nil
}

func (s *Service) GroupPreviews(ctx context.Context, machine string, byteBudget int) ([]pa.GroupPreview, error) {
	rooms, _, err := s.groups()
	if err != nil {
		return nil, err
	}
	if _, err = s.authorize(ctx, pa.BotKey{MachineID: machine, BotID: "groups"}, false); err != nil {
		return nil, err
	}
	if byteBudget < 1 {
		return nil, serviceFailure(pa.Invalid, "group_preview_bytes")
	}
	return rooms.store.GroupPreviews(ctx, machine, byteBudget)
}

// Groups lists one machine's groups, deleted ones included (tombstones let
// a viewer converge), for the person's read of that machine.
func (s *Service) Groups(ctx context.Context, machine string) ([]pa.Group, error) {
	rooms, _, err := s.groups()
	if err != nil {
		return nil, err
	}
	if _, err = s.authorize(ctx, pa.BotKey{MachineID: machine, BotID: "groups"}, false); err != nil {
		return nil, err
	}
	return rooms.store.Groups(ctx, machine)
}

// GroupPublication captures the person's current read grant. The HTTP owner
// calls it again after encoding, so a re-pair or revocation during a slow
// group read cannot publish the old principal's data.
func (s *Service) GroupPublication(ctx context.Context, key pa.BotKey) (func(context.Context) error, error) {
	a, err := s.authorize(ctx, key, false)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) error {
		_, err := s.authorizeEffect(ctx, key, false, a)
		return err
	}, nil
}

// CancelRooms withdraws this Host's room loops before disable/shutdown. It
// does not publish member replies, replay turns, or allocate replacement work.
// The Host closes its Engine and then calls Idle outside its own mutex.
func (d *RoomDriver) CancelRooms() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for key, cancel := range d.running {
		cancel()
		delete(d.running, key)
	}
}
