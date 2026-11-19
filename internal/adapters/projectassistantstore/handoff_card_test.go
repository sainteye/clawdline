package projectassistantstore

import (
	"database/sql"
	"reflect"
	"sync"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func seedHandoffSource(t *testing.T, store *Store) (pa.Bot, pa.Fence) {
	t.Helper()
	bot := seed(t, store, "handoff")
	fence := pa.Fence{Key: bot.Key, Project: bot.Project, ConversationID: bot.ConversationID, BindingID: "native-binding", Generation: 1}
	binding := pa.ExecutionBinding{Fence: fence, Agent: bot.Agent, NativeSessionID: "old-native-session", MemoryRevision: 1, InstructionRevision: 1}
	conversation := pa.Conversation{Key: bot.Key, ID: bot.ConversationID, Generation: fence.Generation, BindingID: fence.BindingID, Version: 2, MemoryRevision: 1}
	turn := pa.Turn{ID: "old-turn", Fence: fence, MessageID: "old-message", State: pa.TurnSucceeded, MemoryRevision: 1, Instructions: bot.Instructions, IdempotencyKey: "old-send"}
	message := pa.Message{ID: turn.MessageID, Fence: fence, TurnID: turn.ID, Seq: 1, Kind: pa.UserMessage, Text: "Original history"}
	if _, err := store.Commit(testContext, app.Mutation{Command: command(bot.Key, pa.SendOperation, "old-send"),
		ExpectedVersion: bot.Version, Binding: &binding, Conversation: &conversation, Turn: &turn, Messages: []pa.Message{message},
		Receipt: pa.Receipt{ID: "old-send", State: pa.EffectDelivered, ResourceID: turn.ID}}); err != nil {
		t.Fatal(err)
	}
	bot.Version++
	return bot, fence
}

func makeHandoff(t *testing.T, s *Store, fence pa.Fence) pa.HandoffCard {
	t.Helper()
	card := pa.HandoffCard{Ref: pa.HandoffRef{ID: "card", Revision: 1}, Fence: fence, State: pa.HandoffDraft,
		MemoryRevision: 1, InstructionRevision: 1, BookmarkMessageID: "old-message", Text: "Synthetic original summary"}
	view, err := s.WriteHandoff(testContext, app.HandoffWrite{
		Request: app.HandoffRequest{Command: command(fence.Key, pa.EditOperation, "prepare"), Action: app.PrepareHandoff, Fence: fence}, Preview: &card,
	})
	if err != nil || view.Card == nil || *view.Card != card {
		t.Fatalf("prepare: %+v %v", view, err)
	}
	return card
}

func handoffInput(bot pa.Bot, fence pa.Fence, id string) app.Mutation {
	turn := pa.Turn{ID: id, Fence: fence, MessageID: "message-" + id, State: pa.TurnQueued, Origin: pa.UserOrigin, IdempotencyKey: id}
	message := pa.Message{ID: turn.MessageID, Fence: fence, TurnID: turn.ID, Kind: pa.UserMessage, Seq: 2, Text: "New input"}
	return app.Mutation{Command: command(bot.Key, pa.SendOperation, id), ExpectedVersion: bot.Version,
		Turn: &turn, Messages: []pa.Message{message}, Receipt: pa.Receipt{ID: id, ResourceID: id, State: pa.EffectAccepted}}
}

func TestHandoffCardConcurrentEditKeepAndSend(t *testing.T) {
	for _, action := range []app.HandoffAction{app.EditHandoff, app.KeepHandoff} {
		t.Run(string(action), func(t *testing.T) {
			_, s, _ := testDB(t)
			bot, fence := seedHandoffSource(t, s)
			card := makeHandoff(t, s, fence)
			before, err := s.Export(testContext, bot.Key)
			if err != nil {
				t.Fatal(err)
			}
			var edit app.HandoffView
			var editErr, sendErr error
			var answer pa.Receipt
			mutation := handoffInput(bot, fence, "concurrent-send")
			change := app.HandoffRequest{Command: command(bot.Key, pa.EditOperation, "racing-edit"), Fence: fence, Action: action, Ref: &card.Ref}
			if action == app.EditHandoff {
				change.Text = "Edited summary"
			}
			ready := make(chan struct{})
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-ready
				edit, editErr = s.WriteHandoff(testContext, app.HandoffWrite{Request: change})
			}()
			go func() {
				defer wg.Done()
				<-ready
				answer, sendErr = s.CommitHandoffInput(testContext, mutation, fence, &card.Ref)
			}()
			close(ready)
			wg.Wait()
			switch {
			case editErr == nil && pa.Code(sendErr) == pa.StaleVersion:
				if edit.Card.Ref.Revision != 2 {
					t.Fatal("edit did not advance its CAS")
				}
				after, err := s.Export(testContext, bot.Key)
				if err != nil || !reflect.DeepEqual(after, before) {
					t.Fatalf("failed input changed the archive: %v", err)
				}
			case sendErr == nil && pa.Code(editErr) == pa.StaleVersion:
				kept, err := s.CommitHandoffInput(testContext, mutation, fence, &card.Ref)
				if err != nil || kept != answer {
					t.Fatalf("lost response replay changed receipt: %+v %v", kept, err)
				}
				x, err := s.HandoffForTurn(testContext, bot.Key, answer.ResourceID)
				if err != nil || x == nil || x.State != pa.HandoffAdopted || x.Text != card.Text {
					t.Fatalf("accepted input has no exact draft: %+v %v", x, err)
				}
				err = s.WithCurrent(testContext, bot.Key, 1, func(pa.ContextSnapshot) error {
					text, e := s.ClaimHandoffText(testContext, *x)
					if e != nil || text != card.Text {
						t.Fatalf("claim: %q %v", text, e)
					}
					_, e = s.ClaimHandoffText(testContext, *x)
					requireCode(t, e, pa.ContextStale)
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			default:
				t.Fatalf("exactly one operation should win: edit=%v send=%v", editErr, sendErr)
			}
		})
	}
}

func TestHandoffCardRollbackAndOldSourceReopen(t *testing.T) {
	db, s, path := testDB(t)
	bot, fence := seedHandoffSource(t, s)
	before, err := s.ExportArchive(testContext, bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`DROP TABLE project_assistant_handoff_cards; DROP TABLE project_assistant_handoff_receipts`); err != nil {
		t.Fatal(err)
	}
	if err = (Installer{}).Install(testContext, db); err != nil {
		t.Fatal(err)
	}
	card := makeHandoff(t, s, fence)
	if _, err = db.Exec(`CREATE TRIGGER reject_adoption BEFORE UPDATE ON project_assistant_handoff_cards BEGIN SELECT RAISE(ABORT,'injected adoption failure'); END`); err != nil {
		t.Fatal(err)
	}
	m := handoffInput(bot, fence, "failed-adoption")
	if _, err = s.CommitHandoffInput(testContext, m, fence, &card.Ref); err == nil {
		t.Fatal("injected terminal SQL failure did not roll back admission")
	}
	if r, err := s.Receipt(testContext, m.Command); err != nil || r != nil {
		t.Fatalf("rolled back admission exposed a receipt: %+v %v", r, err)
	}
	fresh, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	reopened := New(fresh)
	view, err := reopened.ReadHandoff(testContext, bot.Key, bot.ConversationID)
	if err != nil || view.Card == nil || *view.Card != card {
		t.Fatalf("reopened draft: %+v %v", view, err)
	}
	after, err := reopened.ExportArchive(testContext, bot.Key)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("source-local state changed portable v12 bytes: %v", err)
	}
}

func TestHandoffCardInstructionRevisionAndConversationFence(t *testing.T) {
	_, s, _ := testDB(t)
	bot, fence := seedHandoffSource(t, s)
	card := makeHandoff(t, s, fence)
	if _, err := s.ReadHandoff(testContext, bot.Key, "other-conversation"); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("another conversation could read this card: %v", err)
	}
	bot.Instructions.Revision++
	bot.Instructions.Text = "New instructions"
	if _, err := s.Commit(testContext, app.Mutation{Command: command(bot.Key, pa.EditOperation, "change-instructions"),
		ExpectedVersion: bot.Version, Bot: &bot, Receipt: pa.Receipt{ID: "change-instructions", State: pa.EffectDelivered}}); err != nil {
		t.Fatal(err)
	}
	bot.Version++
	view, err := s.ReadHandoff(testContext, bot.Key, bot.ConversationID)
	if err != nil || view.Card == nil || view.Card.State != pa.HandoffWithdrawn || view.Card.Text != "" {
		t.Fatalf("stale instructions left a draft visible: %+v %v", view, err)
	}
	m := handoffInput(bot, fence, "stale-instructions-send")
	_, err = s.CommitHandoffInput(testContext, m, fence, &card.Ref)
	requireCode(t, err, pa.StaleVersion)
	if got, err := s.Receipt(testContext, m.Command); err != nil || got != nil {
		t.Fatalf("stale instruction admission persisted: %+v %v", got, err)
	}
}
