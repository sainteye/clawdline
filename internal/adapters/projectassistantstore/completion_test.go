package projectassistantstore

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A turn that ends without a message for the person used to push "finished"
// anyway, and the tap led to a chat with nothing new in it. The person is told
// only of the reply, inside the commit that writes it, once.
func TestPersonIntentCommitsWithTheReplyAndNeverForAnEmptyTurn(t *testing.T) {
	db, s, _ := testDB(t)
	b := seed(t, s, "completion")
	_, err := db.Exec(`CREATE TABLE completion_intents (turn TEXT PRIMARY KEY)`)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Conversation(testContext, b.Key, b.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	fence := pa.Fence{Key: b.Key, Project: b.Project, ConversationID: c.ID, BindingID: "binding", Generation: 1}
	c.BindingID, c.Generation, c.ActiveTurnID, c.LastMessageSeq = "binding", 1, "turn", 1
	c.Version++
	turn := pa.Turn{ID: "turn", IdempotencyKey: "synthetic-turn", Fence: fence, MessageID: "message", State: pa.TurnRunning, MemoryRevision: 1, Instructions: b.Instructions}
	binding := pa.ExecutionBinding{Fence: fence, Agent: b.Agent, MemoryRevision: 1, InstructionRevision: 1}
	_, err = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.SendOperation, "start"), ExpectedVersion: 1, Conversation: &c, Binding: &binding, Turn: &turn, Messages: []pa.Message{{ID: "message", Seq: 1, Fence: fence, TurnID: turn.ID, Kind: pa.UserMessage, Text: "Synthetic task"}}, Receipt: pa.Receipt{ID: "start", State: pa.EffectExecuting}})
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("intent transaction failed")
	fail := true
	s.PersonIntent = func(ctx context.Context, tx *sql.Tx, _ pa.Bot, told pa.Message, reply bool) error {
		if !reply || told.ForPerson == nil || !*told.ForPerson || told.ID != "reply-turn" {
			t.Fatalf("told the person of %+v", told)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO completion_intents(turn) VALUES(?)`, told.TurnID); err != nil {
			return err
		}
		if fail {
			return injected
		}
		return nil
	}
	c.Version++
	c.ActiveTurnID = ""
	turn.State = pa.TurnSucceeded
	fail = false
	quiet := seed(t, s, "quiet")
	qf, qt := startTurn(t, s, quiet, nil)
	finishTurn(t, s, quiet, qf, qt, "")
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM completion_intents`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("a turn that wrote nothing told the person: %d %v", count, err)
	}
	fail = true
	c.LastMessageSeq = 2
	reply := pa.Message{ID: "reply-turn", Seq: 2, Fence: fence, TurnID: turn.ID, Kind: pa.AssistantMessage, Text: "Done"}
	m := app.Mutation{Command: command(b.Key, pa.SendOperation, "finish"), ExpectedVersion: 2, Conversation: &c, Turn: &turn, Messages: []pa.Message{reply}, Receipt: pa.Receipt{ID: "finished", State: pa.EffectDelivered}}
	if _, err = s.Commit(testContext, m); !errors.Is(err, injected) {
		t.Fatalf("failure injection: %v", err)
	}
	saved, _ := s.Turn(testContext, b.Key, turn.ID)
	if saved.State != pa.TurnRunning {
		t.Fatal("failed intent changed the turn")
	}
	if err = db.QueryRow(`SELECT count(*) FROM completion_intents`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback %d %v", count, err)
	}
	fail = false
	if _, err = s.Commit(testContext, m); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Commit(testContext, m); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM completion_intents`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("replay %d %v", count, err)
	}
	views := newTestViews(t, db)
	page := rosterRead(t, views, rosterQuery())
	var marked *pa.BotSummary
	for _, change := range page.Frame.Changes {
		if change.Key == b.Key {
			marked = change.Summary
		}
	}
	if marked == nil || marked.CompletedTurnID != turn.ID {
		t.Fatalf("durable roster marker %+v", page.Frame)
	}
	// A different command cannot enqueue the same terminal turn again.
	fresh, _ := s.Bot(testContext, b.Key)
	m.ExpectedVersion = fresh.Version
	m.Command = command(b.Key, pa.SendOperation, "repeat-terminal")
	m.Receipt.ID = "repeat-terminal"
	m.Messages = nil
	if _, err = s.Commit(testContext, m); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM completion_intents`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("terminal repeat %d %v", count, err)
	}
}

func TestOnlySuccessfulVisibleMainTasksNotify(t *testing.T) {
	bot := pa.Bot{}
	turn := pa.Turn{State: pa.TurnSucceeded}
	if !pa.NotifiableCompletion(bot, turn) {
		t.Fatal("successful main turn was excluded")
	}
	for _, state := range []pa.TurnState{pa.TurnFailed, pa.TurnCancelled, pa.TurnUnknown, pa.TurnRunning, pa.TurnQueued} {
		copy := turn
		copy.State = state
		if pa.NotifiableCompletion(bot, copy) {
			t.Fatalf("notified state %s", state)
		}
	}
	hidden := bot
	hidden.Hidden = true
	if pa.NotifiableCompletion(hidden, turn) {
		t.Fatal("hidden Bot notified")
	}
	peer := turn
	peer.Origin = pa.BotRequestOrigin
	if pa.NotifiableCompletion(bot, peer) {
		t.Fatal("internal peer request notified")
	}
	member := turn
	member.Room = &pa.RoomTicket{}
	if pa.NotifiableCompletion(bot, member) {
		t.Fatal("individual room member notified")
	}
	joined := turn
	joined.JoinedTurnID = "original"
	if pa.NotifiableCompletion(bot, joined) {
		t.Fatal("joined input notified")
	}
}
