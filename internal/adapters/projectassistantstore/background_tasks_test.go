package projectassistantstore

import (
	"reflect"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestBackgroundAdmissionUsesLiveChatScopeAndKeepsItsReceipt(t *testing.T) {
	db, store, _ := testDB(t)
	bot := seed(t, store, "owner")
	fence := populate(t, store, bot, pa.EffectDelivered)
	task := pa.BackgroundTask{ID: "background", Fence: fence, OriginTurnID: "turn", Title: "Check facts", Brief: "Compare the public sources.", CreatedAt: bot.CreatedAt.Add(time.Minute)}
	cmd := command(bot.Key, pa.DispatchOperation, "background-call")
	wrong := task
	wrong.Fence.Generation++
	_, err := store.AdmitBackgroundTask(testContext, cmd, wrong)
	requireCode(t, err, pa.StaleGeneration)
	wrong = task
	wrong.OriginTurnID = "another-turn"
	_, err = store.AdmitBackgroundTask(testContext, cmd, wrong)
	requireCode(t, err, pa.StaleGeneration)

	got, err := store.AdmitBackgroundTask(testContext, cmd, task)
	if err != nil || got.OriginMessageID != "message" || got.ID != task.ID {
		t.Fatalf("accepted task = %+v, %v", got, err)
	}
	task.ID = "retry-id"
	replayed, err := New(db).AdmitBackgroundTask(testContext, cmd, task)
	if err != nil || !reflect.DeepEqual(replayed, got) {
		t.Fatalf("replay = %+v, %v", replayed, err)
	}
	changed := cmd
	changed.ContentHash = command(bot.Key, pa.DispatchOperation, "changed").ContentHash
	_, err = store.AdmitBackgroundTask(testContext, changed, task)
	requireCode(t, err, pa.IdempotencyConflict)
}

func TestBackgroundAdmissionAndReceiptRollBackTogether(t *testing.T) {
	db, store, _ := testDB(t)
	bot := seed(t, store, "owner")
	fence := populate(t, store, bot, pa.EffectDelivered)
	task := pa.BackgroundTask{ID: "background", Fence: fence, OriginTurnID: "turn", Title: "Check facts", Brief: "Compare sources.", CreatedAt: bot.CreatedAt}
	cmd := command(bot.Key, pa.DispatchOperation, "background-call")
	if _, err := db.Exec(`CREATE TRIGGER reject_background_receipt BEFORE INSERT ON project_assistant_receipts
		WHEN new.operation='dispatch' BEGIN SELECT RAISE(ABORT,'receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitBackgroundTask(testContext, cmd, task); err == nil {
		t.Fatal("admission accepted without its receipt")
	}
	_, err := readBackgroundTask(testContext, db, bot.Key, task.ID)
	requireCode(t, err, pa.NotFound)
	if _, err := db.Exec(`DROP TRIGGER reject_background_receipt`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.AdmitBackgroundTask(testContext, cmd, task); err != nil {
		t.Fatal(err)
	}

	turn, err := store.Turn(testContext, bot.Key, "turn")
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := store.Conversation(testContext, bot.Key, bot.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	turn.State = pa.TurnCancelled
	conversation.ActiveTurnID = ""
	if _, err = store.Commit(testContext, app.Mutation{
		Command: command(bot.Key, pa.StopOperation, "stop-origin"), ExpectedVersion: 2,
		Conversation: &conversation, Turn: &turn,
		Receipt: pa.Receipt{ID: "stop-origin", State: pa.EffectDelivered},
	}); err != nil {
		t.Fatal(err)
	}
	var state string
	if err = db.QueryRow(`SELECT state FROM project_assistant_background_tasks WHERE task=?`, task.ID).Scan(&state); err != nil || state != "cancelled" {
		t.Fatalf("cancelled source left task %q, %v", state, err)
	}
}

func TestBackgroundWorkerWaitsForChatAndHasIndependentQueue(t *testing.T) {
	db, store, _ := testDB(t)
	bot := seed(t, store, "owner")
	fence := populate(t, store, bot, pa.EffectDelivered)
	now := bot.CreatedAt.Add(time.Minute)
	for _, id := range []string{"first", "second", "third", "fourth"} {
		task := pa.BackgroundTask{ID: id, Fence: fence, OriginTurnID: "turn", Title: "Check facts", Brief: "Compare the public sources.", CreatedAt: now}
		if _, err := store.AdmitBackgroundTask(testContext, command(bot.Key, pa.DispatchOperation, id), task); err != nil {
			t.Fatal(err)
		}
	}
	if worker, err := store.StartBackgroundTask(testContext, bot.Key, now); worker.BotID != "" || err != nil {
		t.Fatalf("active chat allowed a start: %+v, %v", worker, err)
	}
	turn, err := store.Turn(testContext, bot.Key, "turn")
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := store.Conversation(testContext, bot.Key, bot.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	turn.State = pa.TurnSucceeded
	conversation.ActiveTurnID = ""
	if _, err = store.Commit(testContext, app.Mutation{
		Command: command(bot.Key, pa.SendOperation, "finish-origin"), ExpectedVersion: 2,
		Conversation: &conversation, Turn: &turn,
		Receipt: pa.Receipt{ID: "finish-origin", State: pa.EffectDelivered},
	}); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"first", "second", "third"} {
		worker, err := New(db).StartBackgroundTask(testContext, bot.Key, now)
		if err != nil || worker.BotID != "thread-"+id {
			t.Fatalf("worker %s = %+v, %v", id, worker, err)
		}
		pending, err := store.PendingTurns(testContext, worker)
		if err != nil || len(pending) != 1 || pending[0].State != pa.TurnQueued || pending[0].Fence.ConversationID != id {
			t.Fatalf("worker %s queue = %+v, %v", id, pending, err)
		}
	}
	if worker, err := store.StartBackgroundTask(testContext, bot.Key, now); worker.BotID != "" || err != nil {
		t.Fatalf("exceeded thread slots: %+v, %v", worker, err)
	}
	list, err := store.List(testContext, pa.ListBots{MachineID: bot.Key.MachineID, PageSize: pa.MaxPageRows})
	if err != nil || len(list.Items) != 1 || list.Items[0].Bot.Key != bot.Key {
		t.Fatalf("private worker leaked into Bot roster: %+v, %v", list, err)
	}
}
