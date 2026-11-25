package projectassistant

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// steerSession is a native session that steers as told. native counts the
// steers that reached the agent.
type steerSession struct {
	*runtimeSession
	refuse  error
	outcome SteerOutcome
	native  *atomic.Int32
}

func (s *steerSession) Steer(context.Context, string) (SteerOutcome, error) {
	if s.refuse != nil {
		return "", s.refuse
	}
	s.native.Add(1)
	return s.outcome, nil
}

// steerFixture is a fixture whose first message is running and whose second
// waits in the queue behind it.
func steerFixture(t *testing.T, refuse error, outcome SteerOutcome) (f *runtimeFixture, running, queued string, native *atomic.Int32, before pa.Fence) {
	t.Helper()
	f = newRuntimeFixture(t)
	native = &atomic.Int32{}
	f.provider.wrap = func(s *runtimeSession) ProviderSession {
		return &steerSession{runtimeSession: s, refuse: refuse, outcome: outcome, native: native}
	}
	before = f.fence()
	first, err := f.submit(t, "one", "one")
	if err != nil {
		t.Fatal(err)
	}
	receivePrompt(t, f.provider)
	second, err := f.submit(t, "two", "two")
	if err != nil {
		t.Fatal(err)
	}
	return f, first.ResourceID, second.ResourceID, native, before
}

func (f *runtimeFixture) fence() pa.Fence {
	f.store.mu.Lock()
	defer f.store.mu.Unlock()
	return currentFence(f.store.bot, f.store.c)
}

func (f *runtimeFixture) sendNow(id string, fence pa.Fence, turnID string) (pa.Receipt, error) {
	return f.r.SendNow(context.Background(), SendNowRequest{Command: pa.Command{Key: f.key, Operation: pa.SendNowOperation, IdempotencyKey: id, ContentHash: strings.Repeat("b", 64)}, Fence: fence, TurnID: turnID})
}

func (f *runtimeFixture) turn(t *testing.T, id string) pa.Turn {
	t.Helper()
	x, err := f.store.Turn(context.Background(), f.key, id)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func failureField(err error) (pa.ErrorCode, string) {
	var f *pa.Failure
	if errors.As(err, &f) {
		return f.Code, f.Field
	}
	return pa.Code(err), ""
}

// An agent that refuses before anything reaches it leaves the message in the
// queue: no unknown outcome, no halted worker, and the page is told the agent
// cannot steer.
func TestSendNowRefusedBeforeTheAgentKeepsTheMessageQueued(t *testing.T) {
	f, running, queued, native, _ := steerFixture(t, SteerRefused(runtimeFailure(pa.Unsupported, "steering")), "")
	_, err := f.sendNow("now", f.fence(), queued)
	if code, field := failureField(err); code != pa.Unsupported || field != SteeringField {
		t.Fatalf("a refused steer answered %v", err)
	}
	if turn := f.turn(t, queued); turn.State != pa.TurnQueued || turn.Steering || turn.Failure != nil {
		t.Fatalf("the refused message left the queue: %+v", turn)
	}
	if native.Load() != 0 {
		t.Fatal("the agent received the refused message")
	}
	// The worker carries on: the message runs in its own turn.
	f.provider.session().reply <- PromptResult{FinalText: "first"}
	awaitTurn(t, f, running, pa.TurnSucceeded)
	if q := receivePrompt(t, f.provider); q.Turn.ID != queued || q.Text != "two" {
		t.Fatalf("the queued message did not run next: %+v", q)
	}
	// The same press can come again without colliding with its first records.
	f.provider.session().reply <- PromptResult{FinalText: "second"}
	awaitTurn(t, f, queued, pa.TurnSucceeded)
}

// A press that arrives after its message started answers that it is running.
func TestSendNowOnAMessageAlreadyRunningSucceeds(t *testing.T) {
	f, running, _, native, _ := steerFixture(t, nil, SteerJoined)
	receipt, err := f.sendNow("now", f.fence(), running)
	if err != nil || receipt.State != pa.EffectAccepted || receipt.ResourceID != running {
		t.Fatalf("Send now on a running message: %+v %v", receipt, err)
	}
	if native.Load() != 0 {
		t.Fatal("a running message was steered into itself")
	}
}

// A press that arrives as the running turn ends answers that the message runs
// next, not that Send now is unsupported.
func TestSendNowAsTheRunningTurnEndsSaysTheMessageRunsNext(t *testing.T) {
	f, _, queued, native, _ := steerFixture(t, nil, SteerJoined)
	f.store.mu.Lock()
	f.store.c.ActiveTurnID = ""
	f.store.mu.Unlock()
	receipt, err := f.sendNow("now", f.fence(), queued)
	if err != nil || receipt.State != pa.EffectAccepted || receipt.ResourceID != queued {
		t.Fatalf("Send now as the turn ended: %+v %v", receipt, err)
	}
	if turn := f.turn(t, queued); turn.State != pa.TurnQueued || turn.Steering || native.Load() != 0 {
		t.Fatalf("the message did not stay queued untouched: %+v", turn)
	}
}

// Every turn start mints a generation, so a phone that read the queue before
// the running turn started still names the same chat; that is enough.
func TestSendNowAcceptsAFenceFromBeforeTheRunningTurnStarted(t *testing.T) {
	f, running, queued, native, before := steerFixture(t, nil, SteerJoined)
	if before.Generation == f.fence().Generation {
		t.Fatal("the fixture did not mint a generation")
	}
	receipt, err := f.sendNow("now", before, queued)
	if err != nil || receipt.State != pa.EffectDelivered {
		t.Fatalf("Send now with an earlier generation: %+v %v", receipt, err)
	}
	if turn := f.turn(t, queued); turn.State != pa.TurnSucceeded || turn.JoinedTurnID != running || native.Load() != 1 {
		t.Fatalf("the message did not join the running turn: %+v", turn)
	}
	other := before
	other.ConversationID = "another-chat"
	if _, err = f.sendNow("elsewhere", other, queued); pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("a fence naming another chat answered %v", err)
	}
}

// A press while an earlier Agent process could not be confirmed stopped was
// never accepted; it says busy, not that its effect is unknown.
func TestSendNowAndQueueEditBehindARetainedCleanupAreNotAccepted(t *testing.T) {
	f, p := lifecycleFixture(t)
	p.closeUnknown.Store(true)
	first, err := f.submit(t, "one", "one")
	if err != nil {
		t.Fatal(err)
	}
	receivePrompt(t, f.provider)
	f.provider.session().reply <- PromptResult{FinalText: "done"}
	awaitTurn(t, f, first.ResourceID, pa.TurnUnknown)
	eventually(t, func() bool { return !f.r.Usage().Known })
	if _, err = f.sendNow("now", f.fence(), first.ResourceID); pa.Code(err) != pa.CapacityFull {
		t.Fatalf("Send now behind a retained cleanup answered %v", err)
	}
	if _, err = f.editQueue("begin", f.fence(), first.ResourceID, "", "begin", ""); pa.Code(err) != pa.CapacityFull {
		t.Fatalf("a queue edit behind a retained cleanup answered %v", err)
	}
}
