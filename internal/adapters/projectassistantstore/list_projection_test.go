package projectassistantstore

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func openBenchStore(tb testing.TB) *Store {
	tb.Helper()
	path := filepath.Join(tb.TempDir(), "bench.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		tb.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	tb.Cleanup(func() { db.Close() })
	if err = (Installer{}).Install(testContext, db); err != nil {
		tb.Fatal(err)
	}
	return New(db)
}

func seedBenchBot(tb testing.TB, s *Store, id string) pa.Bot {
	tb.Helper()
	scope := pa.ProjectScope{MachineID: "machine", ProjectID: "project", CanonicalID: "canonical", Kind: pa.RepositoryProject}
	b := pa.Bot{
		Key: pa.BotKey{MachineID: "machine", BotID: id}, Project: scope, ConversationID: "chat/" + id,
		Name: "Bot " + id, Appearance: pa.Appearance{Shape: pa.Blob, Color: "#e39567"},
		Agent: pa.ClaudeCode, PermissionPolicy: pa.Ask,
		Instructions: pa.InstructionSnapshot{Revision: 1, Text: "Be helpful"},
		Version:      1, CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	c := pa.Conversation{Key: b.Key, ID: b.ConversationID, Version: 1, MemoryRevision: 1}
	cmd := command(b.Key, pa.CreateOperation, "create/"+id)
	sum := pa.BotSummary{Bot: b}
	_, err := s.Commit(testContext, app.Mutation{
		Command: cmd, Bot: &b, Conversation: &c,
		Receipt: pa.Receipt{ID: "created/" + id, State: pa.EffectDelivered, ResourceID: id},
		Events:  []pa.Event{{Version: pa.ContractVersion, ID: "created/" + id, Fence: pa.Fence{Key: b.Key, Project: b.Project, ConversationID: b.ConversationID}, Kind: pa.BotCreated, Bot: &sum}},
	})
	if err != nil {
		tb.Fatal(err)
	}
	return b
}

func seedRosterBenchmarkStore(tb testing.TB, botCount, msgsPerBot int) *Store {
	tb.Helper()
	s := openBenchStore(tb)
	body := strings.Repeat("Implemented Cloud Bot read optimization and verified baseline distribution. ", 20) // ~1.5 KB
	baseTime := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	for i := 0; i < botCount; i++ {
		botID := fmt.Sprintf("bench-bot-%02d", i)
		b := seedBenchBot(tb, s, botID)
		f := pa.Fence{Key: b.Key, Project: b.Project, ConversationID: b.ConversationID, BindingID: "bind-1", Generation: 1}
		tx, err := s.db.BeginTx(testContext, nil)
		if err != nil {
			tb.Fatal(err)
		}
		a, err := load(testContext, tx, b.Key)
		if err != nil {
			tx.Rollback()
			tb.Fatal(err)
		}
		a.Conversation.BindingID = f.BindingID
		a.Conversation.Generation = f.Generation
		a.Conversation.LastMessageSeq = int64(msgsPerBot)
		a.Conversation.Version = 2
		a.Bot.Version = 2
		a.Bindings[f.BindingID] = pa.ExecutionBinding{Fence: f, Agent: b.Agent, MemoryRevision: 1, InstructionRevision: 1}
		for seq := 1; seq <= msgsPerBot; seq++ {
			turnID := fmt.Sprintf("turn-%04d", (seq-1)/2)
			msgID := fmt.Sprintf("msg-%04d", seq)
			kind := pa.UserMessage
			if seq%2 == 0 {
				kind = pa.AssistantMessage
			} else {
				a.Turns[turnID] = pa.Turn{
					ID: turnID, Fence: f, MessageID: msgID, State: pa.TurnSucceeded,
					IdempotencyKey: "idem-" + turnID, MemoryRevision: 1, Instructions: b.Instructions,
				}
			}
			a.Messages = append(a.Messages, pa.Message{
				ID: msgID, Fence: f, Seq: int64(seq), TurnID: turnID, Kind: kind,
				Text: body, At: baseTime.Add(time.Duration(seq) * time.Second),
			})
		}
		readSeq := int64(msgsPerBot)
		if i%2 == 1 {
			readSeq = int64(msgsPerBot - 2)
		}
		a.ReadCursors["reader"] = pa.ReadCursor{Key: b.Key, ConversationID: b.ConversationID, ReaderID: "reader", MessageSeq: readSeq}
		if err := persist(testContext, tx, a, 1); err != nil {
			tx.Rollback()
			tb.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			tb.Fatal(err)
		}
	}
	return s
}

func BenchmarkListForReader(b *testing.B) {
	s := seedRosterBenchmarkStore(b, 10, 500)
	q := pa.ListBots{MachineID: "machine", PageSize: pa.MaxPageRows}
	if _, err := s.ListForReader(testContext, q, "reader"); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		page, err := s.ListForReader(testContext, q, "reader")
		if err != nil || len(page.Items) != 10 {
			b.Fatalf("ListForReader: len=%d err=%v", len(page.Items), err)
		}
	}
}

// Verify that ListForReader matches the reference full-aggregate summary across
// own-chat messages, room turns, withdrawn turns, quiet messages, joined turns,
// pending permissions, decision offers, work links and read cursors.
func TestListForReaderMatchesAggregateSummary(t *testing.T) {
	_, s, _ := testDB(t)
	b1 := seed(t, s, "bot-one")
	f1 := pa.Fence{Key: b1.Key, Project: b1.Project, ConversationID: b1.ConversationID, BindingID: "b1", Generation: 1}
	t1 := pa.Turn{ID: "turn-1", Fence: f1, MessageID: "m1", State: pa.TurnSucceeded, IdempotencyKey: "i1", MemoryRevision: 1, Instructions: b1.Instructions}
	tRoom := pa.Turn{ID: "turn-room", Fence: f1, MessageID: "m3", State: pa.TurnSucceeded, IdempotencyKey: "i-room", MemoryRevision: 1, Instructions: b1.Instructions, Room: &pa.RoomTicket{Group: pa.BotKey{MachineID: "machine", BotID: "g1"}, Member: b1.Key, Epoch: 1, JoinedRevision: 1}}
	tWithdrawn := pa.Turn{ID: "turn-cancelled", Fence: f1, MessageID: "m4", State: pa.TurnCancelled, IdempotencyKey: "i-cancel"}
	tJoined := pa.Turn{ID: "turn-joined", Fence: f1, MessageID: "m5", State: pa.TurnSucceeded, IdempotencyKey: "i-join", JoinedTurnID: "turn-1"}
	tFailed := pa.Turn{ID: "turn-failed", Fence: f1, MessageID: "m6", State: pa.TurnFailed, IdempotencyKey: "i-fail", Failure: &pa.Failure{Code: pa.Offline, Field: "prompt_timeout"}}
	now := time.Now().UTC()
	pDecision := pa.PermissionOffer{
		ID: "dec-1", Fence: f1, Kind: pa.DecisionOfferKind, AnchorMessageSeq: 2,
		State: pa.PermissionPending, ExpiresAt: now.Add(time.Hour),
		Options:  []pa.PermissionOption{{ID: "o1", Label: "Yes", Allow: true, Kind: pa.AllowOnce}},
		Decision: &pa.DecisionRequest{Question: "Deploy now?\nSecond line"},
	}
	w1 := pa.WorkLink{ID: "w1", Fence: f1, TurnID: "turn-1", TaskID: "task-1", State: pa.WorkExecuting}
	tx, err := s.db.BeginTx(testContext, nil)
	if err != nil {
		t.Fatal(err)
	}
	a1, err := load(testContext, tx, b1.Key)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	a1.Conversation.BindingID = f1.BindingID
	a1.Conversation.Generation = f1.Generation
	a1.Conversation.LastMessageSeq = 6
	a1.Conversation.Version = 2
	a1.Bot.Version = 2
	a1.Bindings[f1.BindingID] = pa.ExecutionBinding{Fence: f1, Agent: b1.Agent, MemoryRevision: 1, InstructionRevision: 1}
	a1.Turns[t1.ID] = t1
	a1.Turns[tRoom.ID] = tRoom
	a1.Turns[tWithdrawn.ID] = tWithdrawn
	a1.Turns[tJoined.ID] = tJoined
	a1.Turns[tFailed.ID] = tFailed
	a1.Permissions[pDecision.ID] = pDecision
	a1.WorkLinks[w1.ID] = w1
	a1.Messages = []pa.Message{
		{ID: "m1", Fence: f1, Seq: 1, TurnID: t1.ID, Kind: pa.UserMessage, Text: "hi", At: now.Add(-6 * time.Minute)},
		{ID: "m2", Fence: f1, Seq: 2, TurnID: t1.ID, Kind: pa.AssistantMessage, Text: "assistant reply", At: now.Add(-5 * time.Minute)},
		{ID: "m3", Fence: f1, Seq: 3, TurnID: tRoom.ID, Kind: pa.UserMessage, Text: "room reply", At: now.Add(-4 * time.Minute)},
		{ID: "m4", Fence: f1, Seq: 4, TurnID: tWithdrawn.ID, Kind: pa.UserMessage, Text: "cancelled", At: now.Add(-3 * time.Minute)},
		{ID: "m5", Fence: f1, Seq: 5, TurnID: tJoined.ID, Kind: pa.UserMessage, Text: "joined follow-up", At: now.Add(-2 * time.Minute)},
		{ID: "m6", Fence: f1, Seq: 6, TurnID: tFailed.ID, Kind: pa.UserMessage, Text: "", Files: []pa.InputFile{{ID: "00000000-0000-4000-8000-000000000001", Name: "report.pdf", Bytes: 12, SHA256: strings.Repeat("a", 64)}}, At: now.Add(-1 * time.Minute)},
	}
	a1.ReadCursors["reader"] = pa.ReadCursor{Key: b1.Key, ConversationID: b1.ConversationID, ReaderID: "reader", MessageSeq: 1}
	if err := persist(testContext, tx, a1, 1); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	b2 := seed(t, s, "bot-two")
	f2 := pa.Fence{Key: b2.Key, Project: b2.Project, ConversationID: b2.ConversationID, BindingID: "b2", Generation: 1}
	tRun := pa.Turn{ID: "turn-run", Fence: f2, MessageID: "u1", State: pa.TurnRunning, IdempotencyKey: "i-run", MemoryRevision: 1, Instructions: b2.Instructions}
	_, err = s.Commit(testContext, app.Mutation{
		Command: command(b2.Key, pa.SendOperation, "run-1"), ExpectedVersion: 1,
		Conversation: &pa.Conversation{Key: b2.Key, ID: b2.ConversationID, BindingID: f2.BindingID, Generation: 1, LastMessageSeq: 1, ActiveTurnID: tRun.ID, Version: 2, MemoryRevision: 1},
		Binding:      &pa.ExecutionBinding{Fence: f2, Agent: b2.Agent, MemoryRevision: 1, InstructionRevision: 1},
		Turn:         &tRun,
		Messages:     []pa.Message{{ID: "u1", Fence: f2, Seq: 1, TurnID: tRun.ID, Kind: pa.UserMessage, Text: "go", At: now}},
		Events:       []pa.Event{{Version: pa.ContractVersion, ID: "tr-1", Fence: f2, Kind: pa.TraceUpdated, Trace: &pa.Trace{TurnID: tRun.ID, Category: "agent_message_chunk", Text: "draft"}}},
		Receipt:      pa.Receipt{ID: "run-1", State: pa.EffectExecuting},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, readerID := range []string{"", "reader", "other"} {
		page, err := s.ListForReader(testContext, pa.ListBots{MachineID: "machine", PageSize: pa.MaxPageRows}, readerID)
		if err != nil {
			t.Fatalf("reader=%q: %v", readerID, err)
		}
		if len(page.Items) != 2 {
			t.Fatalf("reader=%q: len=%d", readerID, len(page.Items))
		}
		for _, got := range page.Items {
			refTx, err := s.reader.BeginTx(testContext, nil)
			if err != nil {
				t.Fatal(err)
			}
			full, err := load(testContext, refTx, got.Bot.Key)
			refTx.Rollback()
			if err != nil {
				t.Fatal(err)
			}
			want := summary(full, readerID)
			if got.LatestMessage != want.LatestMessage || got.LatestMessageKind != want.LatestMessageKind ||
				!got.ActivityAt.Equal(want.ActivityAt) || got.UnreadCount != want.UnreadCount ||
				got.HasActivity != want.HasActivity || got.PendingPermissions != want.PendingPermissions ||
				got.ActiveWork != want.ActiveWork || got.Status != want.Status ||
				got.Activity != want.Activity || got.PendingDecisionKind != want.PendingDecisionKind ||
				got.CompletedTurnID != want.CompletedTurnID || got.ReadCursor != want.ReadCursor {
				t.Fatalf("reader=%q bot=%s mismatch:\n got=%+v\nwant=%+v", readerID, got.Bot.Key.BotID, got, want)
			}
			if (got.Failure == nil) != (want.Failure == nil) || (got.Failure != nil && *got.Failure != *want.Failure) {
				t.Fatalf("reader=%q bot=%s failure mismatch: got=%+v want=%+v", readerID, got.Bot.Key.BotID, got.Failure, want.Failure)
			}
			if (got.DispatchedWork == nil) != (want.DispatchedWork == nil) || (got.DispatchedWork != nil && *got.DispatchedWork != *want.DispatchedWork) {
				t.Fatalf("reader=%q bot=%s dispatched mismatch: got=%+v want=%+v", readerID, got.Bot.Key.BotID, got.DispatchedWork, want.DispatchedWork)
			}
		}
	}
}
