package projectassistant_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// roomPeer is a scripted provider: one synthetic session per Bot, whose
// replies the test supplies. It never calls a native model.
type roomPeer struct {
	app.WorkBridge
	mu       sync.Mutex
	replies  map[string][]string
	prompts  map[string][]string
	hold     map[string]chan struct{}
	sessions map[string]*roomSession
	cancels  atomic.Int32
}

type roomSession struct {
	p    *roomPeer
	bot  string
	sink func(context.Context, app.ProtocolEvent) error
	last app.PromptRequest
}

func newRoomPeer() *roomPeer {
	return &roomPeer{replies: map[string][]string{}, prompts: map[string][]string{}, hold: map[string]chan struct{}{}, sessions: map[string]*roomSession{}}
}

func (p *roomPeer) reply(bot string, texts ...string) {
	p.mu.Lock()
	p.replies[bot] = append(p.replies[bot], texts...)
	p.mu.Unlock()
}

func (p *roomPeer) asked(bot string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.prompts[bot]...)
}

func (*roomPeer) Capabilities(_ context.Context, b pa.Bot) (pa.Capabilities, error) {
	return pa.Capabilities{Key: b.Key, Project: b.Project, Agent: b.Agent, CLIVersion: "scripted", AdapterVersion: "fixture", Evidence: "synthetic-only", ReadOnlyEnforced: true, NativeMemory: pa.NativeMemoryDisabled, NativeMemoryVerified: true}, nil
}

func (p *roomPeer) Open(_ context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	s := &roomSession{p: p, bot: q.Bot.Key.BotID, sink: sink}
	p.mu.Lock()
	p.sessions[s.bot] = s
	p.mu.Unlock()
	return s, nil
}

func (s *roomSession) SessionID() string { return "synthetic-" + s.bot }
func (s *roomSession) Send(_ context.Context, q app.PromptRequest) (app.PendingPrompt, error) {
	s.p.mu.Lock()
	s.p.prompts[s.bot] = append(s.p.prompts[s.bot], q.Text)
	s.last = q
	s.p.mu.Unlock()
	return s, nil
}
func (s *roomSession) Wait(ctx context.Context) (app.PromptResult, error) {
	s.p.mu.Lock()
	hold := s.p.hold[s.bot]
	s.p.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return app.PromptResult{}, ctx.Err()
		}
	}
	s.p.mu.Lock()
	defer s.p.mu.Unlock()
	text := pa.RoomPass
	if q := s.p.replies[s.bot]; len(q) > 0 {
		text, s.p.replies[s.bot] = q[0], q[1:]
	}
	return app.PromptResult{FinalText: text}, nil
}
func (*roomSession) Answer(context.Context, string, string) error { return nil }
func (s *roomSession) Cancel(context.Context) error {
	s.p.cancels.Add(1)
	return nil
}
func (*roomSession) Close(context.Context) error { return nil }

type roomResolver struct{ scope pa.ProjectScope }

func (r roomResolver) Resolve(context.Context, pa.ProjectSelection) (pa.ProjectResolution, error) {
	return pa.ProjectResolution{Scope: r.scope, Directory: "/synthetic", Available: true, Current: true}, nil
}

func roomSource(machine string) pa.SourceStatus {
	return pa.SourceStatus{MachineID: machine, State: pa.SourceOnline, Readable: true, Writable: true, Fresh: true, Access: pa.Interactive}
}

type roomHarness struct {
	path    string
	db      *sql.DB
	bots    *projectassistantstore.Store
	groups  *projectassistantstore.GroupStore
	rooms   *app.GroupRooms
	driver  *app.RoomDriver
	engine  *app.Engine
	peer    *roomPeer
	scope   pa.ProjectScope
	person  app.PersonAuthority
	limits  pa.GroupLimits
	members []pa.Bot
	ids     atomic.Int64
}

var roomRuntimeLimits = app.RoomLimits{MemberWait: 2 * time.Second, HistoryLines: 24, PeerLineRunes: 2000}

func newRoomHarness(t *testing.T, names ...string) *roomHarness {
	t.Helper()
	return newRoomHarnessWithLimits(t, roomLimits, names...)
}

func newRoomHarnessWithLimits(t *testing.T, limits pa.GroupLimits, names ...string) *roomHarness {
	t.Helper()
	h := &roomHarness{path: filepath.Join(t.TempDir(), "rooms.db"), peer: newRoomPeer(), limits: limits, person: app.PersonAuthority{Principal: "person", Machine: "m"},
		scope: pa.ProjectScope{MachineID: "m", ProjectID: "p", CanonicalID: "repo", Kind: pa.RepositoryProject}}
	h.db = openRoomDB(t, h.path)
	h.bots = projectassistantstore.New(h.db)
	for _, n := range names {
		h.members = append(h.members, storeBot(t, h.bots, strings.ToLower(n[:1]), n, h.scope))
	}
	h.start(t)
	return h
}

func (h *roomHarness) start(t *testing.T) {
	t.Helper()
	var err error
	h.groups = projectassistantstore.NewGroupStore(h.db)
	if h.rooms, err = app.NewGroupRooms(h.groups, h.limits, func() time.Time { return roomAt }); err != nil {
		t.Fatal(err)
	}
	// Unique across restarts, as the host's generated IDs are.
	id := func() string { return fmt.Sprintf("room-test-%d", h.ids.Add(1)) }
	authorize := func(_ context.Context, k pa.BotKey) (pa.FeatureState, pa.SourceStatus, error) {
		return pa.FeatureState{Enabled: true}, roomSource(k.MachineID), nil
	}
	if h.driver, err = app.NewRoomDriver(app.RoomDriverOptions{Rooms: h.rooms, Bots: h.bots, Authorize: authorize, Limits: roomRuntimeLimits, ID: id}); err != nil {
		t.Fatal(err)
	}
	h.engine, err = app.NewRuntime(app.RuntimeOptions{Repository: h.bots, Memory: h.bots, WorkBridge: h.peer, ProjectResolver: roomResolver{h.scope}, Provider: h.peer,
		Clock: time.Now, ID: id, Authorize: authorize, Rooms: h.driver,
		Limits: app.RuntimeLimits{MaxBots: 8, PromptTimeout: 5 * time.Second, CleanupTimeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	h.driver.Attach(h.engine)
	engine := h.engine
	t.Cleanup(func() { _ = engine.Close(context.Background()) })
}

func (h *roomHarness) group(t *testing.T, id string, members ...pa.Bot) pa.BotKey {
	t.Helper()
	key := pa.BotKey{MachineID: "m", BotID: id}
	var keys []pa.BotKey
	for _, m := range members {
		keys = append(keys, m.Key)
	}
	if _, err := h.rooms.Create(roomCtx, h.person, roomCommand(key, pa.CreateOperation, "create-"+id), pa.GroupDraft{Key: key, Project: h.scope, ConversationID: "room/" + id, Name: "Room " + id, Members: keys}); err != nil {
		t.Fatal(err)
	}
	return key
}

func awaitRoom(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (h *roomHarness) lines(t *testing.T, g pa.BotKey) []pa.RoomEntry {
	t.Helper()
	l, err := h.groups.LinesBefore(roomCtx, g, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func (h *roomHarness) roomTurns(t *testing.T, b pa.Bot) []pa.Turn {
	t.Helper()
	x, err := h.bots.Export(roomCtx, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	var out []pa.Turn
	for _, turn := range x.Turns {
		if turn.Room != nil {
			out = append(out, turn)
		}
	}
	return out
}

func terminal(s pa.TurnState) bool {
	return s == pa.TurnSucceeded || s == pa.TurnFailed || s == pa.TurnCancelled || s == pa.TurnUnknown
}

// A room turn: serial speakers in their own conversations, a mention that
// asks a second member in round two, a pass that ends it, and the line's
// author taken from the ticket.
func TestRoomTurnRunsEachMemberInItsOwnConversation(t *testing.T) {
	h := newRoomHarness(t, "Alice", "Bob", "Carol")
	alice, bob, carol := h.members[0], h.members[1], h.members[2]
	g := h.group(t, "g", alice, bob, carol)
	h.peer.reply("a", "I can take it. @Bob please check the tests.")
	h.peer.reply("b", "(pass)")
	if _, _, err := h.driver.Send(roomCtx, h.person, roomCommand(g, pa.SendOperation, "say-1"), "@Alice can you look at the build?", pa.BotKey{}, nil); err != nil {
		t.Fatal(err)
	}
	awaitRoom(t, "Bob's room turn to finish", func() bool {
		turns := h.roomTurns(t, bob)
		return len(turns) == 1 && terminal(turns[0].State)
	})
	h.driver.Idle()
	lines := h.lines(t, g)
	if len(lines) != 2 || lines[0].Kind != pa.RoomPersonEntry || lines[1].Kind != pa.RoomMemberEntry || lines[1].Author != alice.Key || !strings.Contains(lines[1].Text, "@Bob") {
		t.Fatalf("room lines %+v", lines)
	}
	if got := h.peer.asked("c"); len(got) != 0 {
		t.Fatalf("Carol was asked although nobody named her: %q", got)
	}
	for _, b := range []pa.Bot{alice, bob} {
		turns := h.roomTurns(t, b)
		if len(turns) != 1 || turns[0].State != pa.TurnSucceeded || turns[0].Fence.Key != b.Key || turns[0].Room.Member != b.Key || turns[0].Room.Group != g || turns[0].Origin != pa.HostNoticeOrigin {
			t.Fatalf("%s's room turn %+v", b.Name, turns)
		}
	}
	prompt := h.peer.asked("b")[0]
	for _, want := range []string{"You are Bob", "Alice: I can take it.", "User: @Alice can you look", "reply exactly \"(pass)\""} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("Bob's prompt lacks %q:\n%s", want, prompt)
		}
	}
	if len(h.roomTurns(t, carol)) != 0 {
		t.Fatal("Carol has a room turn")
	}
	// Each member's own conversation holds its notice and its reply.
	x, err := h.bots.Export(roomCtx, alice.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(x.Messages) != 2 || x.Messages[0].Kind != pa.NoticeMessage || x.Messages[1].Kind != pa.AssistantMessage {
		t.Fatalf("Alice's own conversation %+v", x.Messages)
	}
}

func TestExactRoomQuoteReachesTheMemberWithoutChangingUserText(t *testing.T) {
	h := newRoomHarness(t, "Alice", "Bob")
	alice, bob := h.members[0], h.members[1]
	g := h.group(t, "g", alice, bob)
	h.peer.reply("a", "Synthetic original finding")
	if _, _, err := h.driver.Send(roomCtx, h.person, roomCommand(g, pa.SendOperation, "source"), "@Alice give one finding", pa.BotKey{}, nil); err != nil {
		t.Fatal(err)
	}
	awaitRoom(t, "source member answer", func() bool { return len(h.lines(t, g)) == 2 })
	h.driver.Idle()
	original := h.lines(t, g)[1]
	h.peer.reply("a", "(pass)")
	entry, _, err := h.driver.Send(roomCtx, h.person, roomCommand(g, pa.SendOperation, "quote"), "Please explain this finding", pa.BotKey{}, nil, original.Seq)
	if err != nil {
		t.Fatal(err)
	}
	awaitRoom(t, "quoted member turn", func() bool {
		turns := h.roomTurns(t, alice)
		return len(turns) == 2 && terminal(turns[1].State)
	})
	h.driver.Idle()
	prompts := h.peer.asked("a")
	want := `User: [Clawdline: this replies to Alice's message: "Synthetic original finding"] Please explain this finding`
	if len(prompts) != 2 || !strings.Contains(prompts[1], want) || entry.Text != "Please explain this finding" || entry.Quote == nil || entry.Quote.Seq != original.Seq || entry.Quote.Author != alice.Key {
		t.Fatalf("trusted quote was lost or merged into user text: entry=%+v prompts=%q", entry, prompts)
	}
	if len(h.peer.asked("b")) != 0 {
		t.Fatal("a quote without a mention asked an unrelated member")
	}
}

// Refusals: a ticket for another member's fence, a member that was deleted,
// and a ticket the room superseded, also after reopening the database.
func TestRoomTicketsRefuseOtherFencesDeletedMembersAndStaleEpochs(t *testing.T) {
	h := newRoomHarness(t, "Alice", "Bob")
	alice, bob := h.members[0], h.members[1]
	g := h.group(t, "g", alice, bob)
	h.peer.reply("a", "(pass)")
	h.peer.reply("b", "(pass)")
	if _, _, err := h.driver.Send(roomCtx, h.person, roomCommand(g, pa.SendOperation, "say-2"), "hello", pa.BotKey{}, nil); err != nil {
		t.Fatal(err)
	}
	awaitRoom(t, "the first room turn", func() bool { return len(h.roomTurns(t, bob)) == 1 && terminal(h.roomTurns(t, bob)[0].State) })
	h.driver.Idle()
	stale := *h.roomTurns(t, alice)[0].Room

	// A room ticket submitted for Bob's command but naming Alice.
	c, err := h.bots.Conversation(roomCtx, bob.Key, bob.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	wrong := stale
	_, err = h.engine.Submit(roomCtx, app.Submission{Room: &wrong, Notice: true, Feature: pa.FeatureState{Enabled: true}, Source: roomSource("m"), Command: roomCommand(bob.Key, pa.SendOperation, "wrong-member"),
		Message: pa.SendMessage{Fence: pa.Fence{Key: bob.Key, Project: bob.Project, ConversationID: c.ID, BindingID: c.BindingID, Generation: c.Generation}, Text: "room"}})
	if pa.Code(err) != pa.Invalid {
		t.Fatalf("a ticket for another member's fence: %v", err)
	}

	// The person writes again: the earlier epoch is superseded.
	if _, _, err = h.driver.Send(roomCtx, h.person, roomCommand(g, pa.SendOperation, "say-3"), "again", pa.BotKey{}, nil); err != nil {
		t.Fatal(err)
	}
	awaitRoom(t, "the second room turn", func() bool { return len(h.roomTurns(t, bob)) == 2 && terminal(h.roomTurns(t, bob)[1].State) })
	h.driver.Idle()
	if err = h.driver.Admit(roomCtx, stale); pa.Code(err) != pa.StaleGeneration {
		t.Fatalf("superseded ticket admitted: %v", err)
	}

	// Reopen: the refusal is read from the file, not from memory.
	h.engine.Close(roomCtx)
	h.db.Close()
	h.db = openRoomDB(t, h.path)
	h.bots = projectassistantstore.New(h.db)
	h.start(t)
	if err = h.driver.Admit(roomCtx, stale); pa.Code(err) != pa.StaleGeneration {
		t.Fatalf("superseded ticket admitted after reopen: %v", err)
	}

	// A deleted member leaves the group with its row; its tickets are revoked
	// and the room no longer asks it.
	st, _ := h.groups.Room(roomCtx, g)
	grp, _ := h.groups.Group(roomCtx, g)
	held, err := pa.TicketFor(grp, st, bob.Key, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.bots.DeleteBot(roomCtx, bob.Key); err != nil {
		t.Fatal(err)
	}
	if err = h.driver.Admit(roomCtx, held); pa.Code(err) != pa.Revoked {
		t.Fatalf("deleted member's ticket: %v", err)
	}
	before := len(h.peer.asked("b"))
	h.peer.reply("a", "(pass)")
	if _, _, err = h.driver.Send(roomCtx, h.person, roomCommand(g, pa.SendOperation, "say-4"), "@all anyone?", pa.BotKey{}, nil); err != nil {
		t.Fatal(err)
	}
	awaitRoom(t, "Alice's third room turn", func() bool { return len(h.roomTurns(t, alice)) == 3 && terminal(h.roomTurns(t, alice)[2].State) })
	h.driver.Idle()
	if len(h.peer.asked("b")) != before {
		t.Fatal("a deleted member was asked")
	}
}

// The group's Stop cancels a member's running room turn and its queued
// ones; the member's own-chat Stop keeps a late room turn; a permission card
// raised in a room turn is the member's and refuses after the room moved on.
func TestRoomStopAndPermissionCardsBelongToTheMember(t *testing.T) {
	h := newRoomHarness(t, "Alice", "Bob")
	alice, bob := h.members[0], h.members[1]
	g := h.group(t, "g", alice, bob)
	hold := make(chan struct{})
	h.peer.mu.Lock()
	h.peer.hold["a"] = hold
	h.peer.mu.Unlock()
	if _, _, err := h.driver.Send(roomCtx, h.person, roomCommand(g, pa.SendOperation, "say-5"), "@Alice deploy?", pa.BotKey{}, nil); err != nil {
		t.Fatal(err)
	}
	awaitRoom(t, "Alice's prompt", func() bool { return len(h.peer.asked("a")) == 1 })
	h.peer.mu.Lock()
	s := h.peer.sessions["a"]
	q := s.last
	h.peer.mu.Unlock()
	if q.Turn.Room == nil || q.Turn.Fence.Key != alice.Key {
		t.Fatalf("the provider saw %+v", q.Turn)
	}
	offer := pa.PermissionOffer{ID: "offer", Fence: q.Turn.Fence, TurnID: q.Turn.ID, NativeRequestID: "7", ExpiresAt: time.Now().Add(time.Minute), Options: []pa.PermissionOption{{ID: "once", Kind: pa.AllowOnce, Allow: true, ReadOnly: true}}}
	if err := s.sink(roomCtx, app.ProtocolEvent{ID: "offer", Fence: q.Turn.Fence, TurnID: q.Turn.ID, Permission: &offer}); err != nil {
		t.Fatal(err)
	}
	stored, err := h.bots.Permission(roomCtx, alice.Key, "offer")
	if err != nil || stored.Fence.Key != alice.Key {
		t.Fatalf("the card is not Alice's: %+v %v", stored, err)
	}
	if _, err = h.bots.Permission(roomCtx, g, "offer"); err == nil {
		t.Fatal("the card is filed under the group key")
	}

	// The person writes again: Alice's earlier room turn is no longer wanted,
	// so its card is refused (the ticket is re-checked at answer time).
	h.peer.mu.Lock()
	h.peer.hold["b"] = make(chan struct{})
	h.peer.mu.Unlock()
	if _, _, err = h.driver.Send(roomCtx, h.person, roomCommand(g, pa.SendOperation, "say-6"), "@Bob never mind", pa.BotKey{}, nil); err != nil {
		t.Fatal(err)
	}
	answer := app.AnswerRequest{Command: roomCommand(alice.Key, pa.AnswerOperation, "answer"), Answer: pa.PermissionAnswer{Fence: q.Turn.Fence, TurnID: q.Turn.ID, OfferID: "offer", OptionID: "once"}}
	if _, err = h.engine.Answer(roomCtx, answer); pa.Code(err) != pa.StaleGeneration {
		t.Fatalf("a stale room turn's card was answered: %v", err)
	}
	awaitRoom(t, "Bob's prompt", func() bool { return len(h.peer.asked("b")) == 1 })

	// Group Stop: Bob's running room turn is stopped and ends without a line.
	stop, err := h.driver.Stop(roomCtx, h.person, g)
	if err != nil || !stop.Stopped || len(stop.Cancel) != 2 {
		t.Fatalf("stop %+v %v", stop, err)
	}
	awaitRoom(t, "Bob's stopped turn", func() bool {
		turns := h.roomTurns(t, bob)
		return len(turns) == 1 && terminal(turns[0].State)
	})
	if got := h.roomTurns(t, bob)[0].State; got == pa.TurnSucceeded {
		t.Fatal("a stopped room turn succeeded")
	}
	if h.peer.cancels.Load() == 0 {
		t.Fatal("the running member session was not cancelled")
	}
	close(hold)
	h.driver.Idle()
	for _, l := range h.lines(t, g) {
		if l.Kind == pa.RoomMemberEntry {
			t.Fatalf("a line landed after Stop: %+v", l)
		}
	}
	if _, err = (&app.RoomDriver{}).Stop(roomCtx, app.PersonAuthority{}, g); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("stop without the person's authority: %v", err)
	}
}

// The member's own-chat Stop keeps a late room turn queued behind its own
// work; the group's Stop then drops it.
func TestOwnChatStopKeepsALateRoomTurn(t *testing.T) {
	h := newRoomHarness(t, "Alice", "Bob")
	alice, bob := h.members[0], h.members[1]
	g := h.group(t, "g", alice, bob)
	hold := make(chan struct{})
	h.peer.mu.Lock()
	h.peer.hold["a"] = hold
	h.peer.mu.Unlock()
	c, err := h.bots.Conversation(roomCtx, alice.Key, alice.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	fence := pa.Fence{Key: alice.Key, Project: alice.Project, ConversationID: c.ID, BindingID: c.BindingID, Generation: c.Generation}
	if _, err = h.engine.Submit(roomCtx, app.Submission{Feature: pa.FeatureState{Enabled: true}, Source: roomSource("m"), Command: roomCommand(alice.Key, pa.SendOperation, "own"), Message: pa.SendMessage{Fence: fence, Text: "your own work"}}); err != nil {
		t.Fatal(err)
	}
	awaitRoom(t, "Alice busy", func() bool { return len(h.peer.asked("a")) == 1 })
	h.peer.reply("b", "(pass)")
	if _, _, err = h.driver.Send(roomCtx, h.person, roomCommand(g, pa.SendOperation, "say-7"), "@Alice when free", pa.BotKey{}, nil); err != nil {
		t.Fatal(err)
	}
	awaitRoom(t, "Alice's late room turn queued", func() bool {
		turns := h.roomTurns(t, alice)
		return len(turns) == 1 && turns[0].Room.Late && turns[0].State == pa.TurnQueued
	})
	h.driver.Idle()
	c, _ = h.bots.Conversation(roomCtx, alice.Key, alice.ConversationID)
	fence.BindingID, fence.Generation = c.BindingID, c.Generation
	if _, err = h.engine.Stop(roomCtx, app.StopRequest{Command: roomCommand(alice.Key, pa.StopOperation, "own-stop"), Fence: fence}); err != nil {
		t.Fatal(err)
	}
	if turns := h.roomTurns(t, alice); len(turns) != 1 || turns[0].State != pa.TurnQueued {
		t.Fatalf("own-chat Stop dropped the late room turn: %+v", turns)
	}
	if _, err = h.driver.Stop(roomCtx, h.person, g); err != nil {
		t.Fatal(err)
	}
	if turns := h.roomTurns(t, alice); len(turns) != 1 || turns[0].State != pa.TurnCancelled {
		t.Fatalf("group Stop kept the late room turn: %+v", turns)
	}
	close(hold)
	h.driver.Idle()
}

func TestRoomFirstPassReachesEveryoneThePersonAsked(t *testing.T) {
	h := newRoomHarnessWithLimits(t, app.DefaultGroupLimits(),
		"Alice", "Bob", "Carol", "Dan", "Emma", "Fran", "Grace", "Hank", "Isaac", "Jin", "Kim", "Lea")
	g := h.group(t, "all", h.members...)
	for _, bot := range h.members {
		h.peer.reply(bot.Key.BotID, "Answer from "+bot.Name)
	}
	if _, _, err := h.driver.Send(roomCtx, h.person, roomCommand(g, pa.SendOperation, "say-all"), "@everyone status", pa.BotKey{}, nil); err != nil {
		t.Fatal(err)
	}
	h.driver.Idle()
	lines := h.lines(t, g)
	if len(lines) != len(h.members)+1 {
		t.Fatalf("room has %d replies, want %d", len(lines)-1, len(h.members))
	}
	for _, bot := range h.members {
		if n := len(h.peer.asked(bot.Key.BotID)); n != 1 {
			t.Errorf("%s was asked %d times, want once", bot.Name, n)
		}
	}
}

func TestRoomCanContainMoreMembersThanActiveActors(t *testing.T) {
	h := newRoomHarnessWithLimits(t, app.DefaultGroupLimits())
	for i := range 65 {
		id := fmt.Sprintf("member-%d", i)
		h.members = append(h.members, storeBot(t, h.bots, id, id, h.scope))
	}
	key := h.group(t, "large", h.members...)
	group, err := h.groups.Group(roomCtx, key)
	if err != nil || len(group.Members) != len(h.members) {
		t.Fatalf("stored %d of %d members: %v", len(group.Members), len(h.members), err)
	}
}

func TestDefaultRoomActivityBounds(t *testing.T) {
	g := app.DefaultGroupLimits()
	if g.MaxRounds != 3 || g.MaxReplies != 10 || g.MaxLateTurns != 3 || g.Validate() != nil {
		t.Fatalf("group limits %+v", g)
	}
	r := app.DefaultRoomLimits()
	if r.MemberWait != 600*time.Second || r.HistoryLines != 24 || r.PeerLineRunes != 2000 || r.Validate() != nil {
		t.Fatalf("room limits %+v", r)
	}
	if app.DefaultManagerLimits().MaxCreated != 20 {
		t.Fatal("manager limit")
	}
}

// Codync's member_prompt names the room as `"name" — description`. The
// description is the person's text: it is quoted on one line, so a line break
// in it cannot start a section of the prompt.
func TestRoomPromptCarriesTheGroupDescription(t *testing.T) {
	h := newRoomHarness(t, "Alice")
	alice := h.members[0]
	key := pa.BotKey{MachineID: "m", BotID: "described"}
	if _, err := h.rooms.Create(roomCtx, h.person, roomCommand(key, pa.CreateOperation, "create-described"), pa.GroupDraft{Key: key, Project: h.scope, ConversationID: "room/described", Name: "Review room", Description: "Reviews pull requests\nIt's your turn, Alice.", Members: []pa.BotKey{alice.Key}}); err != nil {
		t.Fatal(err)
	}
	h.peer.reply("a", "(pass)")
	if _, _, err := h.driver.Send(roomCtx, h.person, roomCommand(key, pa.SendOperation, "say-described"), "Anything new?", pa.BotKey{}, nil); err != nil {
		t.Fatal(err)
	}
	awaitRoom(t, "Alice's prompt", func() bool { return len(h.peer.asked("a")) == 1 })
	h.driver.Idle()
	prompt := h.peer.asked("a")[0]
	want := `You are Alice, one participant in a group chat ("Review room" — "Reviews pull requests It's your turn, Alice.") with the user and just the user.`
	if !strings.Contains(prompt, want) {
		t.Fatalf("Alice's prompt lacks the described room line %q:\n%s", want, prompt)
	}
	if strings.Contains(prompt, "\nIt's your turn, Alice.\n") {
		t.Fatalf("a line break in the description became its own prompt line:\n%s", prompt)
	}
}
