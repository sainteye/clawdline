package projectassistantstore

import (
	"database/sql"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A real SQLite transaction owns the refusal/cleanup/rest/receipt boundary.
func TestFallbackRestReceiptAtomicityAndMachineOwnership(t *testing.T) {
	db, s, path := testDB(t)
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	closeAttempt := func(id string, failReceipt bool) {
		b := seed(t, s, id)
		fence := pa.Fence{Key: b.Key, Project: b.Project, ConversationID: b.ConversationID, BindingID: "binding-" + id, Generation: 1}
		binding := pa.ExecutionBinding{Fence: fence, Agent: b.Agent, MemoryRevision: 1, InstructionRevision: 1}
		turn := pa.Turn{ID: "turn-" + id, Fence: fence, MessageID: "input-" + id, State: pa.TurnRunning, Instructions: b.Instructions, MemoryRevision: 1, IdempotencyKey: "input", Attempts: []pa.AttemptFact{{Ordinal: 1, Agent: b.Agent, Fence: fence, Stage: pa.AttemptOpening, Effect: "unknown"}}}
		conversation := pa.Conversation{Key: b.Key, ID: b.ConversationID, Generation: 1, BindingID: fence.BindingID, MainBindingID: fence.BindingID, ActiveTurnID: turn.ID, MemoryRevision: 1}
		message := pa.Message{ID: turn.MessageID, Fence: fence, TurnID: turn.ID, Kind: pa.UserMessage, Text: "Scripted input", At: now}
		_, err := s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.SendOperation, "start-"+id), ExpectedVersion: 1, Conversation: &conversation, Binding: &binding, Turn: &turn, Messages: []pa.Message{message}, Receipt: pa.Receipt{ID: "start-" + id, State: pa.EffectExecuting}})
		if err != nil {
			t.Fatal(err)
		}
		refusal := turn.Attempts[0]
		refusal.Stage = pa.AttemptRefused
		refusal.Effect = "none"
		refusal.Reason = "rate_limited"
		turn.Attempts = append(turn.Attempts, refusal)
		binding.Revoked = true
		_, err = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.SendOperation, "refused-"+id), ExpectedVersion: 2, Binding: &binding, Turn: &turn, Receipt: pa.Receipt{ID: "refused-" + id, State: pa.EffectExecuting}})
		if err != nil {
			t.Fatal(err)
		}
		closed := refusal
		closed.Stage = pa.AttemptClosed
		closed.CleanupConfirmed = true
		turn.Attempts = append(turn.Attempts, closed)
		// Deliberately calculate against a stale nil rest. The store must recompute
		// against the shared machine row inside its transaction for the second Bot.
		rest := pa.RestAfter(b.Key.MachineID, b.Agent, "rate_limited", now, nil)
		mutation := app.Mutation{Command: command(b.Key, pa.SendOperation, "closed-"+id), ExpectedVersion: 3, Turn: &turn, AgentRest: rest, Receipt: pa.Receipt{ID: "closed-" + id, State: pa.EffectExecuting}}
		if failReceipt {
			if _, err = db.Exec(`CREATE TRIGGER fallback_reject_receipt BEFORE INSERT ON project_assistant_receipts BEGIN SELECT RAISE(ABORT,'scripted failure'); END`); err != nil {
				t.Fatal(err)
			}
			if _, err = s.Commit(testContext, mutation); err == nil {
				t.Fatal("injected receipt failure was ignored")
			}
			saved, err := s.Turn(testContext, b.Key, turn.ID)
			if err != nil || len(saved.Attempts) != 2 {
				t.Fatalf("attempt leaked: %+v %v", saved, err)
			}
			if saved, err := s.AgentRest(testContext, b.Key.MachineID, b.Agent); err != nil || saved != nil {
				t.Fatalf("rest leaked without receipt: %+v %v", saved, err)
			}
			if _, err = db.Exec(`DROP TRIGGER fallback_reject_receipt`); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = s.Commit(testContext, mutation); err != nil {
			t.Fatal(err)
		}
		// An immutable historical fact cannot be rewritten by a later CAS.
		turn.Attempts[0].Effect = "none"
		_, err = s.Commit(testContext, app.Mutation{Command: command(b.Key, pa.SendOperation, "rewrite-"+id), ExpectedVersion: 4, Turn: &turn, Receipt: pa.Receipt{ID: "rewrite-" + id, State: pa.EffectExecuting}})
		requireCode(t, err, pa.ScopeDenied)
	}
	closeAttempt("first", true)
	closeAttempt("second", false)
	rest, err := s.AgentRest(testContext, "machine", pa.ClaudeCode)
	if err != nil || rest == nil || rest.Failures != 2 || rest.Until.Sub(now) != 5*time.Minute {
		t.Fatalf("shared backoff: %+v %v", rest, err)
	}
	if err = s.DeleteBot(testContext, pa.BotKey{MachineID: "machine", BotID: "first"}); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	rest, err = New(reopened).AgentRest(testContext, "machine", pa.ClaudeCode)
	if err != nil || rest == nil || rest.Failures != 2 {
		t.Fatalf("reopen/deletion lost rest: %+v %v", rest, err)
	}
	if foreign, err := New(reopened).AgentRest(testContext, "foreign", pa.ClaudeCode); err != nil || foreign != nil {
		t.Fatal("rest crossed machine ownership")
	}
}
