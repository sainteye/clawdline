package projectassistantstore

import (
	"reflect"
	"sync"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestQueuedTurnCapturesLatestInstructionsOnceAtStart(t *testing.T) {
	_, s, _ := testDB(t)
	b := seed(t, s, "one")
	admission := pa.Fence{Key: b.Key, Project: b.Project, ConversationID: b.ConversationID, BindingID: "admission", Generation: 1}
	binding := pa.ExecutionBinding{Fence: admission, Agent: b.Agent, MemoryRevision: 1, InstructionRevision: 1}
	c := pa.Conversation{Key: b.Key, ID: b.ConversationID, BindingID: admission.BindingID, Generation: 1, Version: 2, MemoryRevision: 1}
	turn := pa.Turn{ID: "queued", Fence: admission, MessageID: "user", State: pa.TurnQueued, IdempotencyKey: "send"}
	message := pa.Message{ID: "user", Fence: admission, Seq: 1, TurnID: turn.ID, Kind: pa.UserMessage, Text: "hello"}
	_, err := s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.SendOperation, "queue"), ExpectedVersion: 1, Conversation: &c, Binding: &binding, Turn: &turn, Messages: []pa.Message{message}, Receipt: pa.Receipt{ID: "queued", State: pa.EffectAccepted}})
	if err != nil {
		t.Fatal(err)
	}
	edited := b
	edited.Version = 3
	edited.Instructions = pa.InstructionSnapshot{Revision: 2, Text: "New instructions"}
	if _, err = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.EditOperation, "edit"), ExpectedVersion: 2, Bot: &edited, Receipt: pa.Receipt{ID: "edited", State: pa.EffectDelivered}}); err != nil {
		t.Fatal(err)
	}
	fresh := admission
	fresh.BindingID = "process"
	fresh.Generation = 2
	binding = pa.ExecutionBinding{Fence: fresh, Agent: b.Agent, MemoryRevision: 1, InstructionRevision: 2}
	c.BindingID = fresh.BindingID
	c.Generation = 2
	c.Version = 3
	c.LastMessageSeq = 1
	c.ActiveTurnID = turn.ID
	turn.State = pa.TurnRunning
	turn.Fence = fresh
	turn.Instructions = edited.Instructions
	turn.MemoryRevision = 1
	if _, err = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.SendOperation, "start"), ExpectedVersion: 3, Conversation: &c, Binding: &binding, Turn: &turn, Receipt: pa.Receipt{ID: "started", State: pa.EffectExecuting}, Events: []pa.Event{{Version: pa.ContractVersion, ID: "started", Fence: fresh, Kind: pa.TurnUpdated, Turn: &turn}}}); err != nil {
		t.Fatal(err)
	}
	history, err := s.Messages(testContext, app.HistoryQuery{Key: b.Key, ConversationID: b.ConversationID, PageSize: 1})
	if err != nil || history.Items[0].Fence != admission {
		t.Fatalf("admission provenance changed %+v %v", history, err)
	}
	got, err := s.Turn(testContext, b.Key, turn.ID)
	if err != nil || got.Instructions != edited.Instructions || got.Fence != fresh {
		t.Fatalf("start snapshot %+v %v", got, err)
	}
	turn.Instructions.Text = "Mutated during running"
	_, err = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.SendOperation, "mutate-running"), ExpectedVersion: 4, Turn: &turn, Receipt: pa.Receipt{ID: "mutate-running", State: pa.EffectDelivered}})
	requireCode(t, err, pa.ScopeDenied)
}

func TestPendingTurnsFollowAdmissionOrderAndHistoryPages(t *testing.T) {
	_, s, _ := testDB(t)
	b := seed(t, s, "one")
	f := populate(t, s, b, pa.EffectDelivered)
	t2 := pa.Turn{ID: "earlier-sorting-id", Fence: f, MessageID: "second-message", State: pa.TurnQueued, IdempotencyKey: "send-second"}
	second := pa.Message{ID: "second-message", Fence: f, Seq: 2, TurnID: t2.ID, Kind: pa.UserMessage, Text: "second", QuoteMessageID: "message"}
	if _, err := s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.SendOperation, "second"), ExpectedVersion: 2, Turn: &t2, Messages: []pa.Message{second}, Receipt: pa.Receipt{ID: "second", State: pa.EffectAccepted}}); err != nil {
		t.Fatal(err)
	}
	pending, err := s.PendingTurns(testContext, b.Key)
	if err != nil || len(pending) != 2 || pending[0].ID != "turn" || pending[1].ID != t2.ID {
		t.Fatalf("admission order %+v %v", pending, err)
	}
	page, err := s.Messages(testContext, app.HistoryQuery{Key: b.Key, ConversationID: b.ConversationID, PageSize: 1})
	if err != nil || !page.HasMore || page.NextBeforeSeq != 2 || page.Items[0].ID != second.ID {
		t.Fatalf("history first %+v %v", page, err)
	}
	page, err = s.Messages(testContext, app.HistoryQuery{Key: b.Key, ConversationID: b.ConversationID, BeforeSeq: page.NextBeforeSeq, PageSize: 1})
	if err != nil || page.HasMore || len(page.Items) != 1 || page.Items[0].ID != "message" {
		t.Fatalf("history second %+v %v", page, err)
	}
}

func TestCommitCASReplayAndUnknownSurvive(t *testing.T) {
	_, s, _ := testDB(t)
	b := seed(t, s, "one")
	populate(t, s, b, pa.EffectUnknown)
	cmd := command(b.Key, pa.SendOperation, "send")
	prior, err := s.Receipt(testContext, cmd)
	if err != nil || prior.State != pa.EffectUnknown {
		t.Fatalf("receipt: %+v %v", prior, err)
	}
	r, err := s.Commit(testContext, app.Mutation{Command: cmd, ExpectedVersion: 999})
	if err != nil || !reflect.DeepEqual(r, *prior) {
		t.Fatalf("replay: %+v %v", r, err)
	}
	cmd.ContentHash = command(b.Key, pa.SendOperation, "different").ContentHash
	_, err = s.Receipt(testContext, cmd)
	requireCode(t, err, pa.IdempotencyConflict)
	_, err = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.EditOperation, "stale"), ExpectedVersion: 1})
	requireCode(t, err, pa.StaleVersion)
	_, err = s.Observe(testContext, command(b.Key, pa.ObserveOperation, "observe"), "sent", prior.EventSeq)
	requireCode(t, err, pa.OutcomeUnknown)
}

func TestCommitRollbackOnLateSQLFailure(t *testing.T) {
	db, s, _ := testDB(t)
	b := seed(t, s, "one")
	before, err := s.Export(testContext, b.Key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TRIGGER reject_receipt BEFORE INSERT ON project_assistant_receipts BEGIN SELECT RAISE(ABORT,'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	b.Name = "Changed"
	b.Version = 2
	projection := pa.BotSummary{Bot: b}
	cmd := command(b.Key, pa.EditOperation, "failure")
	_, err = s.Commit(testContext, app.Mutation{Command: cmd, ExpectedVersion: 1, Bot: &b, Receipt: pa.Receipt{ID: "failure", State: pa.EffectDelivered}, Events: []pa.Event{{Version: pa.ContractVersion, ID: "edit", Fence: pa.Fence{Key: b.Key, Project: b.Project, ConversationID: b.ConversationID}, Kind: pa.BotUpdated, Bot: &projection}}})
	if err == nil {
		t.Fatal("expected injected failure")
	}
	after, err := s.Export(testContext, b.Key)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("transaction leaked facts/events/receipt: %v", err)
	}
}

func TestCommitRejectsForeignFactsAndDanglingRelations(t *testing.T) {
	_, s, _ := testDB(t)
	b := seed(t, s, "one")
	f := populate(t, s, b, pa.EffectDelivered)
	other := seed(t, s, "other")
	tests := []struct {
		name string
		edit func(*app.Mutation)
	}{
		{"foreign message", func(m *app.Mutation) {
			foreign := f
			foreign.Key = other.Key
			m.Messages = []pa.Message{{ID: "injected", Fence: foreign, Seq: 2, Kind: pa.AssistantMessage, Text: "foreign"}}
		}},
		{"foreign nested event", func(m *app.Mutation) {
			foreign := f
			foreign.Project.ProjectID = "other"
			msg := pa.Message{ID: "injected", Fence: foreign, Seq: 2, Kind: pa.AssistantMessage, Text: "foreign"}
			m.Events = []pa.Event{{ID: "injected", Version: pa.ContractVersion, Fence: f, Kind: pa.MessageCreated, Message: &msg}}
		}},
		{"foreign permission", func(m *app.Mutation) {
			foreign := f
			foreign.Key = other.Key
			m.Permissions = []pa.PermissionOffer{{ID: "injected", Fence: foreign, TurnID: "turn", NativeRequestID: "n"}}
		}},
		{"missing quote", func(m *app.Mutation) {
			m.Messages = []pa.Message{{ID: "injected", Fence: f, Seq: 2, Kind: pa.AssistantMessage, Text: "text", QuoteMessageID: "absent"}}
		}},
		{"missing turn", func(m *app.Mutation) {
			m.Messages = []pa.Message{{ID: "injected", Fence: f, Seq: 2, Kind: pa.AssistantMessage, Text: "text", TurnID: "absent"}}
		}},
		{"memory bypass", func(m *app.Mutation) { m.Memory = &pa.MemoryRevision{Key: b.Key, Project: b.Project, Revision: 2} }},
	}
	before, _ := s.Export(testContext, b.Key)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := app.Mutation{Command: command(b.Key, pa.EditOperation, test.name), ExpectedVersion: 2, Receipt: pa.Receipt{ID: test.name, State: pa.EffectDelivered}}
			test.edit(&m)
			if _, err := s.Commit(testContext, m); err == nil {
				t.Fatal("accepted invalid mutation")
			}
			after, err := s.Export(testContext, b.Key)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("rejection leaked writes %v", err)
			}
		})
	}
}

func TestConcurrentCASHasOneWinner(t *testing.T) {
	_, s, _ := testDB(t)
	b := seed(t, s, "one")
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, id := range []string{"a", "b"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, err := s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.EditOperation, id), ExpectedVersion: 1, Receipt: pa.Receipt{ID: id, State: pa.EffectDelivered}})
			errs <- err
		}(id)
	}
	wg.Wait()
	close(errs)
	wins, stale := 0, 0
	for err := range errs {
		if err == nil {
			wins++
		} else if pa.Code(err) == pa.StaleVersion {
			stale++
		} else {
			t.Fatal(err)
		}
	}
	if wins != 1 || stale != 1 {
		t.Fatalf("wins=%d stale=%d", wins, stale)
	}
}

func TestReaderIsolationMonotonicCursorsAndPagination(t *testing.T) {
	_, s, _ := testDB(t)
	b := seed(t, s, "a")
	populate(t, s, b, pa.EffectDelivered)
	seed(t, s, "b")
	q := pa.ListBots{MachineID: "machine", PageSize: 1}
	page, err := s.ListForReader(testContext, q, "alice")
	if err != nil || len(page.Items) != 1 || page.Items[0].UnreadCount != 1 || page.NextCursor == "" {
		t.Fatalf("first page %+v %v", page, err)
	}
	q.Cursor = page.NextCursor
	page, err = s.ListForReader(testContext, q, "alice")
	if err != nil || len(page.Items) != 1 || page.Items[0].Bot.Key.BotID != "b" || page.NextCursor != "" {
		t.Fatalf("second page %+v %v", page, err)
	}
	read := pa.ReadCursor{Key: b.Key, ConversationID: b.ConversationID, ReaderID: "alice", MessageSeq: 1}
	first, err := s.MarkRead(testContext, command(b.Key, pa.ReadOperation, "read1"), read)
	if err != nil {
		t.Fatal(err)
	}
	read.MessageSeq = 0
	got, err := s.MarkRead(testContext, command(b.Key, pa.ReadOperation, "read2"), read)
	if err != nil || got.MessageSeq != 1 {
		t.Fatalf("regression %+v %v", got, err)
	}
	again, err := s.MarkRead(testContext, command(b.Key, pa.ReadOperation, "read1"), read)
	if err != nil || again != first {
		t.Fatalf("read replay %+v %v", again, err)
	}
	q = pa.ListBots{MachineID: "machine", PageSize: pa.MaxPageRows}
	alice, _ := s.ListForReader(testContext, q, "alice")
	bob, _ := s.ListForReader(testContext, q, "bob")
	neutral, _ := s.List(testContext, q)
	if alice.Items[0].UnreadCount != 0 || bob.Items[0].UnreadCount != 1 || bob.Items[0].ReadCursor.MessageSeq != 0 || neutral.Items[0].ReadCursor.MessageSeq != 0 || neutral.Items[0].ReadCursor.ReaderID != "" {
		t.Fatalf("reader leakage alice=%+v bob=%+v neutral=%+v", alice, bob, neutral)
	}
	q.PageSize = 0
	if _, err = s.List(testContext, q); err == nil {
		t.Fatal("accepted invalid page")
	}
}

func TestObserveIsScopedIdempotentAndHistoryReadable(t *testing.T) {
	_, s, _ := testDB(t)
	b := seed(t, s, "one")
	populate(t, s, b, pa.EffectDelivered)
	prior, _ := s.Receipt(testContext, command(b.Key, pa.SendOperation, "send"))
	cmd := command(b.Key, pa.ObserveOperation, "seen")
	r, err := s.Observe(testContext, cmd, "sent", prior.EventSeq)
	if err != nil || r.State != pa.EffectObserved {
		t.Fatalf("observe %+v %v", r, err)
	}
	r2, err := s.Observe(testContext, cmd, "sent", prior.EventSeq)
	if err != nil || r != r2 {
		t.Fatalf("replay %+v %v", r2, err)
	}
	other := seed(t, s, "other")
	_, err = s.Observe(testContext, command(other.Key, pa.ObserveOperation, "foreign"), "sent", prior.EventSeq)
	requireCode(t, err, pa.NotFound)
	page, err := s.Messages(testContext, app.HistoryQuery{Key: b.Key, ConversationID: b.ConversationID, PageSize: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].Text != "Hello" {
		t.Fatalf("messages %+v %v", page, err)
	}
	if turns, err := s.PendingTurns(testContext, b.Key); err != nil || len(turns) != 1 {
		t.Fatalf("pending %+v %v", turns, err)
	}
	if _, err = s.Binding(testContext, b.Key, "binding"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Permission(testContext, b.Key, "permission"); err != nil {
		t.Fatal(err)
	}
}

func TestPendingTurnsRetainUnknownAsQueueBarrier(t *testing.T) {
	_, s, _ := testDB(t)
	b := seed(t, s, "one")
	f := populate(t, s, b, pa.EffectUnknown)
	unknown, err := s.Turn(testContext, b.Key, "turn")
	if err != nil {
		t.Fatal(err)
	}
	unknown.State = pa.TurnUnknown
	if _, err = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.SendOperation, "mark-unknown"), ExpectedVersion: 2, Turn: &unknown, Receipt: pa.Receipt{ID: "unknown", State: pa.EffectUnknown}}); err != nil {
		t.Fatal(err)
	}
	queued := pa.Turn{ID: "queued", Fence: f, MessageID: "later-message", State: pa.TurnQueued, IdempotencyKey: "later"}
	message := pa.Message{ID: queued.MessageID, Fence: f, Seq: 2, TurnID: queued.ID, Kind: pa.UserMessage, Text: "later"}
	if _, err = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.SendOperation, "later"), ExpectedVersion: 3, Turn: &queued, Messages: []pa.Message{message}, Receipt: pa.Receipt{ID: "later", State: pa.EffectAccepted}}); err != nil {
		t.Fatal(err)
	}
	for _, reader := range []*Store{s, New(s.db)} {
		turns, err := reader.PendingTurns(testContext, b.Key)
		if err != nil || len(turns) != 2 || turns[0].ID != unknown.ID || turns[0].State != pa.TurnUnknown || turns[1].ID != queued.ID {
			t.Fatalf("unknown barrier lost before queued turn: %+v, %v", turns, err)
		}
		receipt, err := reader.Receipt(testContext, command(b.Key, pa.SendOperation, "send"))
		if err != nil || receipt == nil || receipt.State != pa.EffectUnknown {
			t.Fatalf("original unknown intent changed: %+v, %v", receipt, err)
		}
	}
}

// Answering a native permission records unknown before the native write and
// resolved after it. On 2026-11-14 every real "Allow once" failed the second
// commit (pa_stale_version, permission) because only pending could change.
func TestAnAnsweredPermissionMovesFromUnknownToResolvedOnly(t *testing.T) {
	_, s, _ := testDB(t)
	b := seed(t, s, "answer")
	populate(t, s, b, pa.EffectDelivered)
	step := func(id string, version int64, state pa.PermissionState) error {
		x, err := s.Export(testContext, b.Key)
		if err != nil {
			t.Fatal(err)
		}
		var p pa.PermissionOffer
		for _, offer := range x.Permissions {
			if offer.ID == "permission" {
				p = offer
			}
		}
		p.State, p.SelectedOptionID = state, "reject"
		next := x.Bot
		next.Version = version + 1
		_, err = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.AnswerOperation, id), ExpectedVersion: version, Bot: &next, Permissions: []pa.PermissionOffer{p}, Receipt: pa.Receipt{ID: id, State: pa.EffectDelivered, ResourceID: p.ID}})
		return err
	}
	if err := step("unknown", 2, pa.PermissionUnknown); err != nil {
		t.Fatal(err)
	}
	if err := step("resolved", 3, pa.PermissionResolved); err != nil {
		t.Fatalf("a delivered answer could not be recorded: %v", err)
	}
	requireCode(t, step("reopened", 4, pa.PermissionPending), pa.StaleVersion)
}
