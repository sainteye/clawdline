package projectassistantstore

import (
	"context"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"reflect"
	"sync"
	"testing"
)

func TestReactionsKeepExecutionCASAndConcurrentActors(t *testing.T) {
	_, s, _ := testDB(t)
	b := seed(t, s, "reactions")
	f := populate(t, s, b, pa.EffectDelivered)
	before, e := s.Export(testContext, b.Key)
	if e != nil {
		t.Fatal(e)
	}
	write := func(actor, id, emoji string, present bool, revision int64) app.ReactionWrite {
		return app.ReactionWrite{ReactionRequest: app.ReactionRequest{Command: command(b.Key, pa.ReactOperation, id), Fence: f, MessageID: "message", Emoji: emoji, Present: present, ExpectedRevision: revision}, Actor: pa.ReactionActor{ID: pa.HumanReactionID(actor), Kind: "human"}, ReceiptID: "receipt-" + id, EventID: "reaction-" + id}
	}
	q := write("a", "add", "👍", true, 0)
	r, e := s.React(testContext, q)
	if e != nil {
		t.Fatal(e)
	}
	retry, e := s.React(testContext, q)
	if e != nil || !reflect.DeepEqual(r, retry) {
		t.Fatal("lost reply recovery", retry, e)
	}
	conflict := q
	conflict.Command.ContentHash = command(b.Key, pa.ReactOperation, "changed").ContentHash
	conflict.Present = false
	_, e = s.React(testContext, conflict)
	requireCode(t, e, pa.IdempotencyConflict)
	var wg sync.WaitGroup
	for _, actor := range []string{"b", "c"} {
		wg.Add(1)
		go func(actor string) {
			defer wg.Done()
			_, err := s.React(context.Background(), write(actor, actor, "🎉", true, 0))
			if err != nil {
				t.Error(err)
			}
		}(actor)
	}
	wg.Wait()
	if _, e = s.React(testContext, write("a", "remove", "👍", false, 1)); e != nil {
		t.Fatal(e)
	}
	after, e := s.Export(testContext, b.Key)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before.Bot, after.Bot) || !reflect.DeepEqual(before.Conversation, after.Conversation) || !reflect.DeepEqual(before.Turns, after.Turns) || !reflect.DeepEqual(before.Messages, after.Messages) || !reflect.DeepEqual(before.ReadCursors, after.ReadCursors) {
		t.Fatal("reaction edited content/execution/unread")
	}
	page, e := s.Messages(testContext, app.HistoryQuery{Key: b.Key, ConversationID: b.ConversationID, PageSize: 1})
	if e != nil || len(page.Items[0].Reactions) != 3 {
		t.Fatal(page, e)
	}
	if len(after.Reactions) != 3 {
		t.Fatal("metadata missing from archive", after.Reactions)
	}
	_, e = s.React(testContext, write("a", "stale", "👀", true, 0))
	requireCode(t, e, pa.StaleVersion)
	foreign := write("a", "foreign", "👀", true, 2)
	foreign.Fence.ConversationID = "recreated"
	_, e = s.React(testContext, foreign)
	requireCode(t, e, pa.ScopeDenied)
	archive, e := s.ExportArchive(testContext, b.Key)
	if e != nil {
		t.Fatal(e)
	}
	_, restored, _ := testDB(t)
	if e = restored.RestoreArchive(testContext, archive); e != nil {
		t.Fatal(e)
	}
	back, e := restored.Export(testContext, b.Key)
	if e != nil || !reflect.DeepEqual(back.Reactions, after.Reactions) {
		t.Fatal("archive lost reactions", back.Reactions, e)
	}
}

func TestReactionMigrationDoesNotRewriteHistoricalBytes(t *testing.T) {
	db, s, _ := testDB(t)
	b := seed(t, s, "migration")
	populate(t, s, b, pa.EffectDelivered)
	var before, after []byte
	if e := db.QueryRow(`SELECT data FROM project_assistant_bots WHERE bot=?`, b.Key.BotID).Scan(&before); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(`DROP TABLE project_assistant_reactions`); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(`UPDATE project_assistant_schema SET version=10`); e != nil {
		t.Fatal(e)
	}
	if e := (Installer{}).Install(testContext, db); e != nil {
		t.Fatal(e)
	}
	if e := db.QueryRow(`SELECT data FROM project_assistant_bots WHERE bot=?`, b.Key.BotID).Scan(&after); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("migration rewrote historical aggregate")
	}
}

func TestReactionEventFailureRollsBackMetadataAndReceipt(t *testing.T) {
	db, s, _ := testDB(t)
	b := seed(t, s, "rollback")
	f := populate(t, s, b, pa.EffectDelivered)
	if _, e := db.Exec(`CREATE TRIGGER refuse_reaction_event BEFORE INSERT ON project_assistant_events BEGIN SELECT RAISE(ABORT,'synthetic event failure'); END`); e != nil {
		t.Fatal(e)
	}
	c := command(b.Key, pa.ReactOperation, "atomic-reaction")
	_, e := s.React(testContext, app.ReactionWrite{ReactionRequest: app.ReactionRequest{Command: c, Fence: f, MessageID: "message", Emoji: "👍", Present: true}, Actor: pa.ReactionActor{ID: pa.HumanReactionID("reader"), Kind: "human"}, ReceiptID: "atomic-receipt", EventID: "atomic-event"})
	if e == nil {
		t.Fatal("injected failure accepted")
	}
	page, e := s.Messages(testContext, app.HistoryQuery{Key: b.Key, ConversationID: b.ConversationID, PageSize: 1})
	if e != nil || len(page.Items[0].Reactions) != 0 {
		t.Fatal("partial reaction retained", page, e)
	}
	r, e := s.Receipt(testContext, c)
	if e != nil || r != nil {
		t.Fatal("partial receipt retained", r, e)
	}
}
