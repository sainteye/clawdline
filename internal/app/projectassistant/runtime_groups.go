package projectassistant

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A group member's room turn is an ordinary turn of the member's own
// conversation, under the member's own fence and actor, plus a room ticket
// (Turn.Room). These are the runtime's only room-specific steps: the ticket
// is admitted on submission and again when the turn is dequeued, re-checked
// when one of its permission cards is answered, and checked a last time in
// the transaction that writes the room line. Nothing here retries a member
// turn whose effect is unknown.

func (r *Engine) rooms() (RoomGate, RoomRepository, error) {
	if r.o.Rooms == nil {
		return nil, nil, runtimeFailure(pa.Unsupported, "group_rooms")
	}
	repo, ok := r.o.Repository.(RoomRepository)
	if !ok {
		return nil, nil, runtimeFailure(pa.Unsupported, "group_rooms")
	}
	return r.o.Rooms, repo, nil
}

func (r *Engine) admitRoomSubmission(ctx context.Context, s Submission) error {
	gate, _, err := r.rooms()
	if err != nil {
		return err
	}
	if !s.Notice || s.Room.Member != s.Command.Key || len(s.Message.UploadIDs) > 0 || len(s.Message.Attachments) > 0 || s.Message.QuoteMessageID != "" {
		return runtimeFailure(pa.Invalid, "room_submission")
	}
	return gate.Admit(ctx, *s.Room)
}

func (r *Engine) admitRoomTurn(ctx context.Context, t pa.RoomTicket) error {
	gate, _, err := r.rooms()
	if err != nil {
		return err
	}
	return gate.Admit(ctx, t)
}

// finishRoomTurn commits the member's terminal facts and the room line in
// one transaction, then tells the room what happened. A refused ticket is
// reported, not returned: the member's own turn did finish.
func (r *Engine) finishRoomTurn(ctx context.Context, bot pa.Bot, m Mutation, text string) error {
	gate, repo, err := r.rooms()
	if err != nil {
		return err
	}
	m, err = r.prepareMutation(ctx, bot, m)
	if err != nil {
		return err
	}
	pub, err := repo.FinishRoomTurn(ctx, m, text)
	if err != nil {
		return err
	}
	gate.Finished(pub)
	return nil
}

// keepsAfterOwnChatStop: the member's own-chat Stop leaves a queued late
// room turn in place (pa.AfterOwnChatStop); everything else stops.
func keepsAfterOwnChatStop(t pa.Turn, active string) bool {
	return t.Room != nil && t.Room.Late && t.State == pa.TurnQueued && t.ID != active
}

func (r *Engine) recheckRoomCard(ctx context.Context, p pa.PermissionOffer) error {
	t, err := r.o.Repository.Turn(ctx, p.Fence.Key, p.TurnID)
	if err != nil {
		return err
	}
	if t.Room == nil {
		return nil
	}
	if t.Room.Member != p.Fence.Key {
		return runtimeFailure(pa.ScopeDenied, "room_permission")
	}
	return r.admitRoomTurn(ctx, *t.Room)
}

// CancelRoomTurns drops member's queued room turns for group, and with
// active also stops its running turn when that turn is in this room. Its
// turns elsewhere are untouched. A stopped running turn ends unknown, as any
// Stop does; it is never resent.
func (r *Engine) CancelRoomTurns(ctx context.Context, group, member pa.BotKey, active bool) error {
	a, err := r.acquire(member)
	if err != nil {
		return err
	}
	defer r.release(a)
	a.mu.Lock()
	defer a.mu.Unlock()
	pending, err := r.o.Repository.PendingTurns(ctx, member)
	if err != nil {
		return err
	}
	var errs []error
	for _, t := range pending {
		if t.Room == nil || t.Room.Group != group {
			continue
		}
		if t.State == pa.TurnQueued && a.turnID != t.ID {
			if e := r.finishLocked(ctx, a, t, pa.TurnCancelled, "", nil); e != nil {
				errs = append(errs, e)
			}
			continue
		}
		if active && a.turnID == t.ID {
			a.stopped = true
			if a.cancel != nil {
				a.cancel()
			}
			if a.session != nil {
				_ = a.session.Cancel(ctx)
			}
		}
	}
	return errors.Join(errs...)
}

// BusyElsewhere: member is running a turn that is not this room's, so a
// room turn would queue behind it (Codync's busy_elsewhere).
func (r *Engine) BusyElsewhere(ctx context.Context, member, group pa.BotKey) (bool, error) {
	pending, err := r.o.Repository.PendingTurns(ctx, member)
	if err != nil {
		return false, err
	}
	for _, t := range pending {
		if t.State != pa.TurnRunning && t.State != pa.TurnWaitingPermission {
			continue
		}
		if t.Room == nil || t.Room.Group != group {
			return true, nil
		}
	}
	return false, nil
}

// DefaultGroupLimits are the registered room bounds (internal/domain/capacity,
// CodyNC group.rs and rooms.rs). Upstream has no member maximum: a room asks
// members one at a time, each through a runtime actor, so the actor bound is
// the most members a room can be served by.
func DefaultGroupLimits() pa.GroupLimits {
	return pa.GroupLimits{MaxMembers: capacity.MaximumBotActors, MaxRounds: capacity.MaximumBotGroupRounds, MaxReplies: capacity.MaximumBotGroupReplies, MaxLateTurns: capacity.MaximumBotGroupLateTurns}
}

// DefaultRoomLimits are the registered driver bounds.
func DefaultRoomLimits() RoomLimits {
	return RoomLimits{MemberWait: capacity.MaximumBotGroupMemberWaitSeconds * time.Second, HistoryLines: capacity.MaximumBotGroupHistoryLines, PeerLineRunes: capacity.MaximumBotGroupPeerLineChars}
}

// DefaultManagerLimits is the registered manager bound (chat/manage.rs).
func DefaultManagerLimits() ManagerLimits {
	return ManagerLimits{MaxCreated: capacity.MaximumBotManagerCreated}
}

// The room driver.

// RoomMembers is the part of the runtime the room driver uses. *Engine
// implements it: members' room turns run through its ordinary Submit.
type RoomMembers interface {
	Submit(context.Context, Submission) (pa.Receipt, error)
	CancelRoomTurns(ctx context.Context, group, member pa.BotKey, active bool) error
	BusyElsewhere(ctx context.Context, member, group pa.BotKey) (bool, error)
}

// RoomLimits are the registered room bounds the driver needs beyond
// pa.GroupLimits (internal/domain/capacity, CodyNC group.rs).
type RoomLimits struct {
	// MemberWait is how long a room turn waits for one member before it
	// stops waiting (the answer still lands, late). It is not a retry or
	// permission deadline.
	MemberWait time.Duration
	// HistoryLines is how many room lines a member's prompt shows.
	HistoryLines int
	// PeerLineRunes clips each shown line (Codync's LINE_CHARS).
	PeerLineRunes int
}

func (l RoomLimits) Validate() error {
	if l.MemberWait <= 0 || l.HistoryLines < 1 || l.PeerLineRunes < 1 {
		return serviceFailure(pa.Invalid, "room_limits")
	}
	return nil
}

type RoomDriverOptions struct {
	Rooms *GroupRooms
	// Bots reads member Bots and their conversations (for names and fences).
	Bots Repository
	// Authorize is the runtime's background source read for a member.
	Authorize func(context.Context, pa.BotKey) (pa.FeatureState, pa.SourceStatus, error)
	Limits    RoomLimits
	ID        func() string
}

// RoomDriver runs group rooms as Codync's chat/group.rs does: speakers are
// asked one at a time, each in its own conversation under its own fence;
// @-mentions decide later rounds; a round with no speech ends the turn; late
// answers that name someone start a late room turn. It implements RoomGate.
type RoomDriver struct {
	o       RoomDriverOptions
	members RoomMembers
	mu      sync.Mutex
	waiters map[roomAsk]chan RoomPublication
	running map[pa.BotKey]context.CancelFunc
	work    sync.WaitGroup
}

type roomAsk struct {
	group, member pa.BotKey
	epoch, joined int64
}

func askOf(t pa.RoomTicket) roomAsk {
	return roomAsk{group: t.Group, member: t.Member, epoch: t.Epoch, joined: t.JoinedRevision}
}

var _ RoomGate = (*RoomDriver)(nil)

func NewRoomDriver(o RoomDriverOptions) (*RoomDriver, error) {
	if o.Rooms == nil || o.Bots == nil || o.Authorize == nil || o.ID == nil {
		return nil, serviceFailure(pa.Invalid, "room_driver")
	}
	if err := o.Limits.Validate(); err != nil {
		return nil, err
	}
	return &RoomDriver{o: o, waiters: map[roomAsk]chan RoomPublication{}, running: map[pa.BotKey]context.CancelFunc{}}, nil
}

// Attach supplies the runtime once it is built (it needs this driver as its
// RoomGate first).
func (d *RoomDriver) Attach(m RoomMembers) {
	d.mu.Lock()
	d.members = m
	d.mu.Unlock()
}

func (d *RoomDriver) runtime() (RoomMembers, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.members == nil {
		return nil, serviceFailure(pa.Unsupported, "group_rooms")
	}
	return d.members, nil
}

// Idle waits until no room turn is running; for orderly shutdown and tests.
func (d *RoomDriver) Idle() { d.work.Wait() }

func (d *RoomDriver) Admit(ctx context.Context, t pa.RoomTicket) error {
	return d.o.Rooms.Admit(ctx, t)
}

// Finished hands a member's outcome to the room turn waiting for it, or, when
// nobody waits (a busy or overdue member), treats it as a late answer.
func (d *RoomDriver) Finished(p RoomPublication) {
	d.mu.Lock()
	k := askOf(p.Ticket)
	ch, waiting := d.waiters[k]
	if waiting && !p.Ticket.Late {
		delete(d.waiters, k)
	}
	d.mu.Unlock()
	if waiting && !p.Ticket.Late {
		ch <- p
		return
	}
	if p.Entry == nil {
		return
	}
	d.go_(func(ctx context.Context) { d.lateAnswer(ctx, p) })
}

func (d *RoomDriver) go_(f func(context.Context)) {
	d.work.Add(1)
	go func() {
		defer d.work.Done()
		f(context.Background())
	}()
}

const roomRetries = 4

// retryStale runs f under the current room revision, again when a concurrent
// transition moved it. f's store write is a CAS, so a stale attempt wrote
// nothing.
func (d *RoomDriver) retryStale(ctx context.Context, key pa.BotKey, f func(rev int64) error) error {
	var err error
	for range roomRetries {
		var st pa.RoomState
		if st, err = d.o.Rooms.store.Room(ctx, key); err != nil {
			return err
		}
		if err = f(st.Revision); pa.Code(err) != pa.StaleVersion {
			return err
		}
	}
	return err
}

// Send is the person's message: written and its room turn started. check,
// when set, re-establishes the person's authority after the room read and
// immediately before the write (the AuthorityCheck pattern of Service.Send).
func (d *RoomDriver) Send(ctx context.Context, who PersonAuthority, c pa.Command, text string, replyToAuthor pa.BotKey, check func(context.Context) error) (pa.RoomEntry, RoomStep, error) {
	if _, err := d.runtime(); err != nil {
		return pa.RoomEntry{}, RoomStep{}, err
	}
	var entry pa.RoomEntry
	var step RoomStep
	err := d.retryStale(ctx, c.Key, func(rev int64) error {
		if check != nil {
			if e := check(ctx); e != nil {
				return e
			}
		}
		var e error
		entry, step, e = d.o.Rooms.Send(ctx, who, c, rev, text, replyToAuthor)
		return e
	})
	if err != nil || step.Turn == nil {
		return entry, step, err
	}
	d.start(c.Key, *step.Turn)
	return entry, step, nil
}

// Stop is the group's Stop: the room turn ends, members' queued turns in this
// room are dropped and an active one in this room is stopped; late answers
// start nothing until the person writes again.
func (d *RoomDriver) Stop(ctx context.Context, who PersonAuthority, key pa.BotKey) (GroupStop, error) {
	if err := who.check(key); err != nil {
		return GroupStop{}, err
	}
	if _, err := d.runtime(); err != nil {
		return GroupStop{}, err
	}
	var out GroupStop
	if err := d.retryStale(ctx, key, func(rev int64) error {
		var e error
		out, e = d.o.Rooms.Stop(ctx, key, rev)
		return e
	}); err != nil {
		return out, err
	}
	return out, d.cancelMembers(ctx, key, out.Cancel)
}

// cancelMembers ends the running room loop and the named members' turns in
// this room: queued ones are dropped, an active one here is stopped.
func (d *RoomDriver) cancelMembers(ctx context.Context, key pa.BotKey, members []pa.BotKey) error {
	m, err := d.runtime()
	if err != nil {
		return err
	}
	d.mu.Lock()
	if cancel := d.running[key]; cancel != nil {
		cancel()
		delete(d.running, key)
	}
	for k, ch := range d.waiters {
		if k.group == key {
			delete(d.waiters, k)
			close(ch)
		}
	}
	d.mu.Unlock()
	var errs []string
	for _, member := range members {
		if e := m.CancelRoomTurns(ctx, key, member, true); e != nil {
			errs = append(errs, e.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("group stop: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (d *RoomDriver) start(key pa.BotKey, turn pa.RoomTurn) {
	ctx, cancel := context.WithCancel(context.Background())
	d.mu.Lock()
	if prior := d.running[key]; prior != nil {
		prior()
	}
	d.running[key] = cancel
	d.mu.Unlock()
	d.work.Add(1)
	go func() {
		defer d.work.Done()
		defer cancel()
		d.run(ctx, key, turn)
		d.mu.Lock()
		if d.running[key] != nil && ctx.Err() == nil {
			delete(d.running, key)
		}
		d.mu.Unlock()
	}()
}

// run drives one room turn and the late turns that follow it.
func (d *RoomDriver) run(ctx context.Context, key pa.BotKey, turn pa.RoomTurn) {
	asked := map[pa.BotKey]int64{}
	for {
		d.runTurn(ctx, key, turn, asked)
		if ctx.Err() != nil {
			return
		}
		var step RoomStep
		err := d.retryStale(ctx, key, func(rev int64) error {
			var e error
			step, e = d.o.Rooms.TurnEnded(ctx, key, rev, turn.Epoch)
			return e
		})
		if err != nil || step.Turn == nil {
			return
		}
		turn = *step.Turn
	}
}

func (d *RoomDriver) current(ctx context.Context, key pa.BotKey, epoch int64) bool {
	st, err := d.o.Rooms.store.Room(ctx, key)
	return err == nil && pa.RoomTurnCurrent(st, epoch)
}

func (d *RoomDriver) speakers(ctx context.Context, g pa.Group) []pa.RoomSpeaker {
	c := pa.GroupCatalog{Bots: map[pa.BotKey]pa.Bot{}}
	for _, m := range g.Members {
		if b, err := d.o.Bots.Bot(ctx, m.Key); err == nil {
			c.Bots[m.Key] = b
		}
	}
	return pa.RoomSpeakers(g, c)
}

// since is the room from the person's last message on, or, for a late turn,
// from the oldest late answer on.
func (d *RoomDriver) since(ctx context.Context, key pa.BotKey, turn pa.RoomTurn) ([]pa.RoomEntry, error) {
	if turn.Late != 0 {
		return d.o.Rooms.store.Lines(ctx, key, turn.Late-1, d.o.Limits.HistoryLines)
	}
	lines, err := d.o.Rooms.store.LinesBefore(ctx, key, 0, d.o.Limits.HistoryLines)
	if err != nil {
		return nil, err
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i].Kind == pa.RoomPersonEntry {
			return lines[i:], nil
		}
	}
	return lines, nil
}

func (d *RoomDriver) runTurn(ctx context.Context, key pa.BotKey, turn pa.RoomTurn, asked map[pa.BotKey]int64) {
	limits := d.o.Rooms.limits
	var run pa.RoomRun
	later := map[pa.BotKey]bool{}
	for {
		g, err := d.o.Rooms.store.Group(ctx, key)
		if err != nil || g.Deleted {
			return
		}
		speakers := d.speakers(ctx, g)
		since, err := d.since(ctx, key, turn)
		if err != nil {
			return
		}
		for _, s := range pa.RoomSpeakersFor(speakers, since, turn.Late != 0, run.Round, asked) {
			if ctx.Err() != nil || !d.current(ctx, key, turn.Epoch) {
				return
			}
			if run.Replies >= limits.MaxReplies {
				return
			}
			if later[s.Key] {
				continue
			}
			entry, busy := d.ask(ctx, g, speakers, s, asked)
			if busy {
				later[s.Key] = true
				continue
			}
			if !run.Answered(entry, limits) {
				return
			}
		}
		if !run.EndRound(limits) {
			return
		}
	}
}

// ask runs one member's room turn and waits for it, at most MemberWait. A
// refused or failed turn counts as a pass and is never resent; a busy member
// answers late on its own.
func (d *RoomDriver) ask(ctx context.Context, g pa.Group, speakers []pa.RoomSpeaker, s pa.RoomSpeaker, asked map[pa.BotKey]int64) (*pa.RoomEntry, bool) {
	m, err := d.runtime()
	if err != nil {
		return nil, false
	}
	busy, err := m.BusyElsewhere(ctx, s.Key, g.Key)
	if err != nil {
		return nil, false
	}
	ticket, err := d.o.Rooms.Ask(ctx, g.Key, s.Key, busy)
	if err != nil {
		return nil, false
	}
	// Asking again replaces the member's earlier turn in this room.
	if err = m.CancelRoomTurns(ctx, g.Key, s.Key, false); err != nil {
		return nil, false
	}
	bot, err := d.o.Bots.Bot(ctx, s.Key)
	if err != nil {
		return nil, false
	}
	c, err := d.o.Bots.Conversation(ctx, s.Key, bot.ConversationID)
	if err != nil {
		return nil, false
	}
	prompt, shown, err := d.prompt(ctx, g, speakers, s)
	if err != nil {
		return nil, false
	}
	asked[s.Key] = shown
	feature, source, err := d.o.Authorize(ctx, s.Key)
	if err != nil {
		return nil, false
	}
	ch := make(chan RoomPublication, 1)
	k := askOf(ticket)
	if !busy {
		d.mu.Lock()
		d.waiters[k] = ch
		d.mu.Unlock()
	}
	unwait := func() {
		d.mu.Lock()
		if d.waiters[k] == ch {
			delete(d.waiters, k)
		}
		d.mu.Unlock()
	}
	id := "room-" + d.o.ID()
	ticketCopy := ticket
	sub := Submission{Room: &ticketCopy, Notice: true, Feature: feature, Source: source,
		Command: runtimeCommand(s.Key, pa.SendOperation, id, struct {
			Ticket pa.RoomTicket
			Text   string
		}{ticket, prompt}),
		Message: pa.SendMessage{Fence: currentFence(bot, c), Text: prompt}}
	if _, err = m.Submit(ctx, sub); err != nil {
		unwait()
		return nil, false
	}
	if busy {
		return nil, true
	}
	timer := time.NewTimer(d.o.Limits.MemberWait)
	defer timer.Stop()
	select {
	case p, ok := <-ch:
		if !ok {
			return nil, false
		}
		return p.Entry, false
	case <-timer.C:
		unwait()
		// Overdue: the room stops waiting; the answer still lands, late.
		_ = d.retryStale(ctx, g.Key, func(rev int64) error {
			_, e := d.o.Rooms.MarkOverdue(ctx, ticket, rev)
			return e
		})
		return nil, false
	case <-ctx.Done():
		unwait()
		return nil, false
	}
}

func (d *RoomDriver) lateAnswer(ctx context.Context, p RoomPublication) {
	g, err := d.o.Rooms.store.Group(ctx, p.Ticket.Group)
	if err != nil || g.Deleted {
		return
	}
	if len(pa.RoomNamedBy(d.speakers(ctx, g), p.Entry.Author, p.Entry.Text)) == 0 {
		return
	}
	var step RoomStep
	if err = d.retryStale(ctx, g.Key, func(rev int64) error {
		var e error
		step, e = d.o.Rooms.LateAnswer(ctx, g.Key, rev, p.Entry.Seq)
		return e
	}); err != nil || step.Turn == nil {
		return
	}
	d.start(g.Key, *step.Turn)
}

// prompt is Codync's member_prompt: who is in the room, how to answer, and
// the newest lines, each clipped. shown is the newest seq it shows.
func (d *RoomDriver) prompt(ctx context.Context, g pa.Group, speakers []pa.RoomSpeaker, me pa.RoomSpeaker) (string, int64, error) {
	lines, err := d.o.Rooms.store.LinesBefore(ctx, g.Key, 0, d.o.Limits.HistoryLines)
	if err != nil {
		return "", 0, err
	}
	var shown int64
	if len(lines) > 0 {
		shown = lines[len(lines)-1].Seq
	}
	// Lines since this member last spoke, within the shown window.
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i].Kind == pa.RoomMemberEntry && lines[i].Author == me.Key {
			lines = lines[i+1:]
			break
		}
	}
	names := map[pa.BotKey]string{}
	var peers []string
	for _, s := range speakers {
		names[s.Key] = s.Name
		if s.Key != me.Key {
			peers = append(peers, s.Name)
		}
	}
	peerNames := "just the user"
	if len(peers) > 0 {
		peerNames = strings.Join(peers, ", ")
	}
	out := []string{fmt.Sprintf("You are %s, one participant in a group chat (%q) with the user and %s.", me.Name, g.Name, peerNames)}
	if len(peers) > 0 {
		out = append(out, "Other participants in the room:")
		for _, p := range peers {
			out = append(out, "- "+p)
		}
	}
	out = append(out, fmt.Sprintf("You have your full toolkit here. Do the work first, then answer: your final reply of this turn is the one message the room sees. Keep it short and conversational. After this round, only the participants someone @-mentions answer again: @-mention another participant when you need their answer or action, and only then. If you have nothing new worth adding, reply exactly %q. Never reveal private one-on-one context.", pa.RoomPass), "")
	out = append(out, fmt.Sprintf("[Group chat: %q - with %s]", g.Name, peerNames))
	if len(lines) == 0 {
		out = append(out, "No new messages in the room since your last turn.")
	} else {
		out = append(out, "New messages in the room (oldest first):")
		for _, e := range lines {
			text := clipRunes(e.Text, d.o.Limits.PeerLineRunes)
			switch {
			case e.Kind == pa.RoomPersonEntry:
				out = append(out, "User: "+text)
			case e.Author == me.Key:
				out = append(out, me.Name+" (you): "+text)
			default:
				name := names[e.Author]
				if name == "" {
					name = "A former participant"
				}
				out = append(out, name+": "+text)
			}
		}
	}
	out = append(out, "", fmt.Sprintf("It's your turn, %s. Reply to the room if you have something worth adding, or reply exactly %q if you don't.", me.Name, pa.RoomPass))
	return strings.Join(out, "\n"), shown, nil
}

func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}
