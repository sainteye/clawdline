package projectassistant

import (
	"context"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// The incident: the broker accepted the task, the reply never reached the
// Bot, and the Bot only has its idempotency key. The key must find the same
// task_id, and a resend with that key must not create a second task.
func TestWorkReceiptFindsAcceptedTaskWhoseReplyWasLost(t *testing.T) {
	bridge, broker, repo, _, q, launcher := newBridgeFixture(t)
	ctx := context.Background()
	bot, _ := repo.Bot(ctx, q.Command.Key)
	if _, err := bridge.commit(ctx, bot, Mutation{Command: q.Command, Receipt: pa.Receipt{State: pa.EffectUnknown, ResourceID: q.TurnID}}); err != nil {
		t.Fatal(err)
	}
	repo.failProjection = true
	if _, err := bridge.Dispatch(ctx, q); err == nil {
		t.Fatal("the lost reply was not modelled")
	}
	repo.failProjection = false
	got, err := bridge.Receipt(ctx, q.Actor, q.Command.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	want := orchestrator.AssistantTaskID(q.Command)
	if got.State != WorkReceiptAccepted || got.TaskID != want || got.Link == nil || got.Link.TaskID != want || got.SafeToResend {
		t.Fatalf("receipt %+v, want accepted %s", got, want)
	}
	if link, err := bridge.Dispatch(ctx, q); err != nil || link.TaskID != want {
		t.Fatalf("same-key resend %+v %v", link, err)
	}
	rows, _, err := broker.Records(ctx)
	if err != nil || len(rows) != 1 || launcher.calls.Load() != 1 {
		t.Fatalf("task created more than once: rows=%d launches=%d %v", len(rows), launcher.calls.Load(), err)
	}
}

func TestWorkReceiptSaysSafeToResendWhenBrokerWasNeverAsked(t *testing.T) {
	bridge, broker, repo, _, q, launcher := newBridgeFixture(t)
	ctx := context.Background()
	bot, _ := repo.Bot(ctx, q.Command.Key)
	if _, err := bridge.commit(ctx, bot, Mutation{Command: q.Command, Receipt: pa.Receipt{State: pa.EffectUnknown, ResourceID: q.TurnID}}); err != nil {
		t.Fatal(err)
	}
	got, err := bridge.Receipt(ctx, q.Actor, q.Command.IdempotencyKey)
	if err != nil || got.State != WorkReceiptNotAccepted || !got.SafeToResend || got.TaskID != "" {
		t.Fatalf("receipt %+v %v", got, err)
	}
	link, err := bridge.Dispatch(ctx, q)
	if err != nil || link.TaskID != orchestrator.AssistantTaskID(q.Command) {
		t.Fatalf("safe resend %+v %v", link, err)
	}
	if got, err = bridge.Receipt(ctx, q.Actor, q.Command.IdempotencyKey); err != nil || got.State != WorkReceiptAccepted || got.TaskID != link.TaskID {
		t.Fatalf("after resend %+v %v", got, err)
	}
	rows, _, _ := broker.Records(ctx)
	if len(rows) != 1 || launcher.calls.Load() != 1 {
		t.Fatalf("rows=%d launches=%d", len(rows), launcher.calls.Load())
	}
}

func TestWorkReceiptStaysUndeterminedWhenBrokerAdmissionIsUnknown(t *testing.T) {
	bridge, _, repo, _, q, _ := newBridgeFixture(t)
	ctx := context.Background()
	id := orchestrator.AssistantTaskID(q.Command)
	bot, _ := repo.Bot(ctx, q.Command.Key)
	if _, err := bridge.commit(ctx, bot, Mutation{Command: q.Command, Receipt: pa.Receipt{State: pa.EffectUnknown, ResourceID: q.TurnID}}); err != nil {
		t.Fatal(err)
	}
	bot, _ = repo.Bot(ctx, q.Command.Key)
	prepare := bridgeCommand(q.Command.Key, "bridge-prepare-"+id, orchestrator.AssistantRequestHash(q))
	if _, err := bridge.commit(ctx, bot, Mutation{Command: prepare, Receipt: pa.Receipt{State: pa.EffectUnknown, ResourceID: id}}); err != nil {
		t.Fatal(err)
	}
	got, err := bridge.Receipt(ctx, q.Actor, q.Command.IdempotencyKey)
	if err != nil || got.State != WorkReceiptUndetermined || got.SafeToResend || got.Reason != "broker_admission_unknown" {
		t.Fatalf("receipt %+v %v", got, err)
	}
	bridge.dispatchMu.Lock()
	got, err = bridge.Receipt(ctx, q.Actor, q.Command.IdempotencyKey)
	bridge.dispatchMu.Unlock()
	if err != nil || got.State != WorkReceiptUndetermined || got.Reason != "dispatch_in_progress" || got.SafeToResend {
		t.Fatalf("in-flight receipt %+v %v", got, err)
	}
}

// Another Bot's key lives in another namespace; a task of this Bot owned by
// another conversation answers exactly like a key never used.
func TestWorkReceiptRefusesKeysThisDispatcherDoesNotOwn(t *testing.T) {
	bridge, _, _, current, q, _ := newBridgeFixture(t)
	ctx := context.Background()
	if _, err := bridge.Dispatch(ctx, q); err != nil {
		t.Fatal(err)
	}
	_, unused := bridge.Receipt(ctx, q.Actor, "never-used")
	if pa.Code(unused) != pa.NotFound {
		t.Fatalf("unused key: %v", unused)
	}
	moved := q.Actor.Fence
	moved.ConversationID = "another-chat"
	current.Fence = moved
	other, err := bridge.o.Broker.AssistantAuthority.Issue(ctx, moved, []pa.ActorOperation{pa.DispatchWork, pa.ReadWork}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	_, foreign := bridge.Receipt(ctx, other, q.Command.IdempotencyKey)
	if pa.Code(foreign) != pa.NotFound || foreign.Error() != unused.Error() {
		t.Fatalf("foreign owner %v differs from unused %v", foreign, unused)
	}
}
