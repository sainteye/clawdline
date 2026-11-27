package projectassistantstore

import (
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// startTurn commits the person's (or a routine's) message and its running
// turn; shape adjusts the turn before it is written.
func startTurn(t *testing.T, s *Store, b pa.Bot, shape func(*pa.Turn)) (pa.Fence, pa.Turn) {
	t.Helper()
	f := pa.Fence{Key: b.Key, Project: b.Project, ConversationID: b.ConversationID, BindingID: "binding", Generation: 1}
	c := pa.Conversation{Key: b.Key, ID: b.ConversationID, BindingID: f.BindingID, Generation: 1, Version: 2, MemoryRevision: 1, ActiveTurnID: "turn", LastMessageSeq: 1}
	binding := pa.ExecutionBinding{Fence: f, Agent: b.Agent, MemoryRevision: 1, InstructionRevision: 1}
	turn := pa.Turn{ID: "turn", Fence: f, MessageID: "message", State: pa.TurnRunning, Instructions: b.Instructions, MemoryRevision: 1, IdempotencyKey: "send"}
	if shape != nil {
		shape(&turn)
	}
	m := pa.Message{ID: "message", Fence: f, Seq: 1, TurnID: turn.ID, Kind: pa.UserMessage, Text: "Hello", At: b.CreatedAt}
	if _, err := s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.SendOperation, "send"), ExpectedVersion: 1, Conversation: &c, Binding: &binding, Turn: &turn, Messages: []pa.Message{m}, Receipt: pa.Receipt{ID: "sent", State: pa.EffectDelivered, ResourceID: "turn"}, Events: []pa.Event{{Version: pa.ContractVersion, ID: b.Key.BotID + "/message", Fence: f, Kind: pa.MessageCreated, Message: &m}}}); err != nil {
		t.Fatal(err)
	}
	return f, turn
}

// finishTurn ends the turn successfully with its final reply, as the runtime
// records it.
func finishTurn(t *testing.T, s *Store, b pa.Bot, f pa.Fence, turn pa.Turn, reply string) {
	t.Helper()
	current, err := s.Bot(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Conversation(testContext, b.Key, b.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	c.ActiveTurnID = ""
	turn.State = pa.TurnSucceeded
	var messages []pa.Message
	events := []pa.Event{{Version: pa.ContractVersion, ID: b.Key.BotID + "/done", Fence: f, Kind: pa.TurnUpdated, Turn: &turn}}
	if reply != "" {
		m := pa.Message{ID: "reply-" + turn.ID, Fence: f, Seq: c.LastMessageSeq + 1, TurnID: turn.ID, Kind: pa.AssistantMessage, Text: reply, At: b.CreatedAt}
		messages = append(messages, m)
		events = append(events, pa.Event{Version: pa.ContractVersion, ID: b.Key.BotID + "/reply", Fence: f, Kind: pa.MessageCreated, Message: &m})
	}
	if _, err = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.SendOperation, "finish"), ExpectedVersion: current.Version, Conversation: &c, Turn: &turn, Messages: messages, Receipt: pa.Receipt{ID: "finished", State: pa.EffectDelivered, ResourceID: turn.ID}, Events: events}); err != nil {
		t.Fatal(err)
	}
}

// unreadBoth is the reader's unread count in the list and in the streamed
// roster, which compute it on separate paths.
func unreadBoth(t *testing.T, s *Store, views *ViewStore, key pa.BotKey) (int64, int64) {
	t.Helper()
	q := rosterQuery()
	list, err := s.ListForReader(testContext, pa.ListBots{MachineID: key.MachineID, PageSize: pa.MaxPageRows}, q.ReaderID)
	if err != nil {
		t.Fatal(err)
	}
	listed := int64(-1)
	for _, item := range list.Items {
		if item.Bot.Key == key {
			listed = item.UnreadCount
		}
	}
	streamed := int64(-1)
	for _, change := range rosterRead(t, views, q).Frame.Changes {
		if change.Key == key && change.Summary != nil {
			streamed = change.Summary.UnreadCount
		}
	}
	return listed, streamed
}

// A routine's reply is not something the person asked for: on its own it does
// not raise the badge. The reply to the person's own message does.
func TestOnlyTheReplyToThePersonsOwnMessageIsUnread(t *testing.T) {
	db, s, _ := testDB(t)
	views := newTestViews(t, db)
	direct := seed(t, s, "direct")
	f, turn := startTurn(t, s, direct, nil)
	finishTurn(t, s, direct, f, turn, "Here is your answer")
	if listed, streamed := unreadBoth(t, s, views, direct.Key); listed != 1 || streamed != 1 {
		t.Fatalf("the reply to the person's message: list=%d stream=%d, want 1", listed, streamed)
	}
	routine := seed(t, s, "routine")
	f, turn = startTurn(t, s, routine, func(t *pa.Turn) { t.RoutineRunID = "run" })
	finishTurn(t, s, routine, f, turn, "Checked the inbox; nothing new")
	if listed, streamed := unreadBoth(t, s, views, routine.Key); listed != 0 || streamed != 0 {
		t.Fatalf("a routine's reply raised the badge: list=%d stream=%d, want 0", listed, streamed)
	}
}

// Which final reply counts depends on who started the turn; a notify_person
// message counts wherever it was written, and every new message records an
// answer so it never falls back to the rule for older messages.
func TestStampForPersonByTurnOrigin(t *testing.T) {
	type want struct{ reply, notify bool }
	cases := []struct {
		name  string
		shape func(*pa.Turn)
		quiet bool
		want  want
	}{
		{"the person's message", nil, false, want{true, true}},
		{"a routine", func(t *pa.Turn) { t.RoutineRunID = "run" }, false, want{false, true}},
		{"another Bot's request", func(t *pa.Turn) { t.Origin = pa.BotRequestOrigin }, false, want{false, true}},
		{"a reply from another Bot", func(t *pa.Turn) { t.Origin = pa.BotReplyOrigin }, false, want{false, true}},
		{"a host notice", func(t *pa.Turn) { t.Origin = pa.HostNoticeOrigin }, false, want{false, true}},
		{"a joined steer", func(t *pa.Turn) { t.JoinedTurnID = "other" }, false, want{false, true}},
		{"a silent completion", nil, true, want{false, true}},
	}
	for _, c := range cases {
		turn := pa.Turn{ID: "turn", State: pa.TurnRunning}
		if c.shape != nil {
			c.shape(&turn)
		}
		a := aggregate{Turns: map[string]pa.Turn{"turn": turn}}
		a.Messages = []pa.Message{{ID: "message", TurnID: "turn", Kind: pa.UserMessage}, {ID: "notify", TurnID: "turn", Kind: pa.AssistantMessage, ForPerson: pa.ForPersonFlag(true)}}
		told := stampForPerson(&a, app.Mutation{}, 2, "")
		if len(told) != 1 || told[0].ID != "notify" || *a.Messages[0].ForPerson {
			t.Fatalf("%s: notify_person told %+v", c.name, told)
		}
		finished := turn
		finished.State = pa.TurnSucceeded
		a.Turns["turn"] = finished
		a.Messages = append(a.Messages, pa.Message{ID: "reply-turn", TurnID: "turn", Kind: pa.AssistantMessage}, pa.Message{ID: "quiet", TurnID: "turn", Kind: pa.AssistantMessage, Output: &pa.Output{Quiet: true}})
		told = stampForPerson(&a, app.Mutation{Turn: &finished, QuietCompletion: c.quiet}, 2, pa.TurnRunning)
		reply := a.Messages[2]
		if reply.ForPerson == nil || *reply.ForPerson != c.want.reply || (len(told) == 1) != c.want.reply || *a.Messages[3].ForPerson {
			t.Fatalf("%s: reply for the person %v, told %+v", c.name, reply.ForPerson != nil && *reply.ForPerson, told)
		}
		again := stampForPerson(&a, app.Mutation{Turn: &finished}, 0, pa.TurnSucceeded)
		if len(again) != 0 {
			t.Fatalf("%s: an already finished turn told the person again: %+v", c.name, again)
		}
	}
}

// A routine's reply is news: the row shows a grey dot without a number, the
// same in the list and in the streamed roster, and reading the chat clears it.
func TestARoutineReplyShowsActivityUntilTheChatIsRead(t *testing.T) {
	db, s, _ := testDB(t)
	views := newTestViews(t, db)
	b := seed(t, s, "routine")
	f, turn := startTurn(t, s, b, func(t *pa.Turn) { t.RoutineRunID = "run" })
	finishTurn(t, s, b, f, turn, "Checked the inbox; nothing new")
	q := rosterQuery()
	rows := func() (pa.BotSummary, pa.BotSummary) {
		t.Helper()
		list, err := s.ListForReader(testContext, pa.ListBots{MachineID: "machine", PageSize: pa.MaxPageRows}, q.ReaderID)
		if err != nil {
			t.Fatal(err)
		}
		var streamed pa.BotSummary
		for _, change := range rosterRead(t, views, q).Frame.Changes {
			if change.Key == b.Key && change.Summary != nil {
				streamed = *change.Summary
			}
		}
		return list.Items[0], streamed
	}
	listed, streamed := rows()
	if listed.UnreadCount != 0 || streamed.UnreadCount != 0 || !listed.HasActivity || !streamed.HasActivity {
		t.Fatalf("routine reply: list unread=%d activity=%v, stream unread=%d activity=%v", listed.UnreadCount, listed.HasActivity, streamed.UnreadCount, streamed.HasActivity)
	}
	if _, err := s.MarkRead(testContext, command(b.Key, pa.ReadOperation, "read"), pa.ReadCursor{Key: b.Key, ConversationID: b.ConversationID, ReaderID: q.ReaderID, MessageSeq: 2}); err != nil {
		t.Fatal(err)
	}
	listed, streamed = rows()
	if listed.HasActivity || streamed.HasActivity || listed.UnreadCount != 0 || streamed.UnreadCount != 0 {
		t.Fatalf("reading kept the dot: list %v stream %v", listed.HasActivity, streamed.HasActivity)
	}
}
