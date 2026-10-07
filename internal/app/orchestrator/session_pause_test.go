package orchestrator

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
)

const pauseTarget = "379d0000-0000-4000-8000-000000000002"

func pauseBroker(t *testing.T) (*Broker, context.Context, *[]string) {
	t.Helper()
	b, ctx := newTestBroker(t)
	b.Live = func(context.Context) []session.Session {
		return []session.Session{
			{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation},
			{ID: "%2", Assistant: session.AssistantCodex, ConversationID: pauseTarget},
		}
	}
	lines := &[]string{}
	b.Type = func(_ context.Context, terminal, line string) error {
		if terminal != "%2" {
			t.Fatalf("typed into %s", terminal)
		}
		*lines = append(*lines, line)
		return nil
	}
	return b, ctx, lines
}

func pauseRequest() PauseRequest {
	return PauseRequest{ID: askA, Requester: rootConversation, Target: pauseTarget,
		Reason: "The release needs the compile slot", WakeCondition: "heavy_compile becomes available"}
}

func TestPauseDeliveryIsNotASafePointAndDuplicateCommandsDoNotSendTwice(t *testing.T) {
	b, ctx, lines := pauseBroker(t)
	req := pauseRequest()
	p, err := b.RequestPause(ctx, req)
	if err != nil || pauseState(p) != "delivered" || len(*lines) != 1 || !strings.Contains((*lines)[0], req.ID) {
		t.Fatalf("request = %+v, %v; lines = %v", p, err, *lines)
	}
	if _, err := b.RequestPause(ctx, req); err != nil || len(*lines) != 1 {
		t.Fatalf("duplicate sent again: %v, %v", err, *lines)
	}
	if _, err := b.PauseReceipt(ctx, req.ID, pauseTarget, "safe"); refusalCode(err) != "pause_not_observed" {
		t.Fatalf("safe before observation: %v", err)
	}
	if p, err = b.PauseReceipt(ctx, req.ID, pauseTarget, "observed"); err != nil || pauseState(p) != "observed" {
		t.Fatalf("observed = %+v, %v", p, err)
	}
	if p, err = b.PauseReceipt(ctx, req.ID, pauseTarget, "safe"); err != nil || pauseState(p) != "safe_point" {
		t.Fatalf("safe = %+v, %v", p, err)
	}
	if p, err = b.WakePause(ctx, req.ID, rootConversation); err != nil || pauseState(p) != "resuming" || len(*lines) != 2 {
		t.Fatalf("wake = %+v, %v; lines = %v", p, err, *lines)
	}
	if _, err := b.WakePause(ctx, req.ID, rootConversation); err != nil || len(*lines) != 2 {
		t.Fatalf("duplicate wake sent again: %v, %v", err, *lines)
	}
	if p, err = b.PauseReceipt(ctx, req.ID, pauseTarget, "resumed"); err != nil || pauseState(p) != "resumed" {
		t.Fatalf("resumed = %+v, %v", p, err)
	}
}

func TestFailedPauseDeliveryStaysAcceptedAndCanBeRetriedAfterRestart(t *testing.T) {
	b, ctx, lines := pauseBroker(t)
	b.Type = func(context.Context, string, string) error { return errors.New("terminal refused input") }
	req := pauseRequest()
	p, err := b.RequestPause(ctx, req)
	if err != nil || pauseState(p) != "accepted" || !strings.Contains(p.DeliveryError, "delivery_failed") {
		t.Fatalf("failed delivery = %+v, %v", p, err)
	}
	if _, err := b.PauseReceipt(ctx, req.ID, pauseTarget, "observed"); refusalCode(err) != "pause_not_delivered" {
		t.Fatalf("unseen request acknowledged: %v", err)
	}
	// A fresh broker instance uses the same durable store, as a restarted daemon does.
	next := &Broker{Store: b.Store, Live: b.Live}
	next.Type = func(_ context.Context, terminal, line string) error { *lines = append(*lines, line); return nil }
	p, err = next.RetryPauseDelivery(ctx, req.ID)
	if err != nil || pauseState(p) != "delivered" || len(*lines) != 1 {
		t.Fatalf("retry = %+v, %v; lines = %v", p, err, *lines)
	}
	if _, err := next.RetryPauseDelivery(ctx, req.ID); err != nil || len(*lines) != 1 {
		t.Fatalf("delivered retry sent twice: %v", err)
	}
}

func TestRetryDuringDeliveryDoesNotQueueAnotherNotice(t *testing.T) {
	b, ctx, _ := pauseBroker(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	count := 0
	b.Type = func(context.Context, string, string) error {
		mu.Lock()
		count++
		mu.Unlock()
		close(entered)
		<-release
		return nil
	}
	req := pauseRequest()
	done := make(chan error, 1)
	go func() { _, err := b.RequestPause(ctx, req); done <- err }()
	<-entered
	if _, err := b.RetryPauseDelivery(ctx, req.ID); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if count != 1 {
		t.Fatalf("typed %d times", count)
	}
}

func TestUnreachableSessionIsExplicitAndDoesNotBecomePaused(t *testing.T) {
	b, ctx, _ := pauseBroker(t)
	b.Live = func(context.Context) []session.Session { return nil }
	p, err := b.RequestPause(ctx, pauseRequest())
	if err != nil || pauseState(p) != "accepted" || p.DeliveryError != "session_unreachable" {
		t.Fatalf("unreachable = %+v, %v", p, err)
	}
}

func TestConcurrentPauseRequestsLeaveOneActiveReceipt(t *testing.T) {
	b, ctx, _ := pauseBroker(t)
	var mu sync.Mutex
	b.Type = func(context.Context, string, string) error { mu.Lock(); defer mu.Unlock(); return nil }
	reqs := []PauseRequest{pauseRequest(), pauseRequest()}
	reqs[1].ID = askB
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, req := range reqs {
		wg.Add(1)
		go func(r PauseRequest) { defer wg.Done(); _, err := b.RequestPause(ctx, r); results <- err }(req)
	}
	wg.Wait()
	close(results)
	accepted, conflict := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else if refusalCode(err) == "session_pause_active" {
			conflict++
		} else {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if accepted != 1 || conflict != 1 {
		t.Fatalf("accepted %d, conflict %d", accepted, conflict)
	}
	rows, err := b.Store.Pauses(ctx)
	if err != nil || len(rows) != 1 || rows[0].SafeAt != (store.PauseRow{}).SafeAt {
		t.Fatalf("rows = %+v, %v", rows, err)
	}
}

func TestWakeWaitsForTheExclusiveLeaseToClear(t *testing.T) {
	b, ctx, _ := pauseBroker(t)
	lease := ask(t, b, ctx, LeaseRequest{Resource: ResourceCompile, RequestID: askB,
		Holder: "another compile", Session: rootConversation})
	if lease.State != "granted" {
		t.Fatal(lease)
	}
	req := pauseRequest()
	req.WakeCondition = "lease:heavy_compile"
	if _, err := b.RequestPause(ctx, req); err != nil {
		t.Fatal(err)
	}
	if _, err := b.PauseReceipt(ctx, req.ID, pauseTarget, "observed"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.PauseReceipt(ctx, req.ID, pauseTarget, "safe"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.WakePause(ctx, req.ID, rootConversation); refusalCode(err) != "lease_held" {
		t.Fatalf("woke through holder: %v", err)
	}
	if _, err := b.Release(ctx, LeaseOwner{Resource: ResourceCompile, RequestID: askB}); err != nil {
		t.Fatal(err)
	}
	if p, err := b.WakePause(ctx, req.ID, rootConversation); err != nil || pauseState(p) != "resuming" {
		t.Fatalf("after release: %+v, %v", p, err)
	}
	if got := ask(t, b, ctx, LeaseRequest{Resource: ResourceCompile, RequestID: askB,
		Holder: "next compile", Session: rootConversation}); got.State != "granted" {
		t.Fatal(got)
	}
	if _, err := b.WakePause(ctx, req.ID, rootConversation); err != nil {
		t.Fatalf("duplicate wake should keep its receipt after lease changes: %v", err)
	}
}

func TestRestartAndOtherExclusiveOperationsCannotOverlap(t *testing.T) {
	b, ctx, _ := pauseBroker(t)
	compile := ask(t, b, ctx, LeaseRequest{Resource: ResourceCompile, RequestID: askA,
		Holder: "compile", Session: rootConversation})
	if compile.State != "granted" {
		t.Fatal(compile)
	}
	restart := ask(t, b, ctx, LeaseRequest{Resource: ResourceRestart, RequestID: askB,
		Holder: "restart", Session: pauseTarget})
	if restart.State != "queued" || restart.HoldReason != "conflicting_resource_held" {
		t.Fatal(restart)
	}
	if _, err := b.Release(ctx, LeaseOwner{Resource: ResourceCompile, RequestID: askA}); err != nil {
		t.Fatal(err)
	}
	restart = ask(t, b, ctx, LeaseRequest{Resource: ResourceRestart, RequestID: askB,
		Holder: "restart", Session: pauseTarget})
	if restart.State != "granted" {
		t.Fatal(restart)
	}
	landing := ask(t, b, ctx, LeaseRequest{Resource: ResourceLanding, Checkout: t.TempDir(), RequestID: askA,
		Holder: "landing", Session: rootConversation})
	if landing.State != "queued" || landing.HoldReason != "conflicting_resource_held" {
		t.Fatal(landing)
	}
}

func TestUnstartedPauseNotificationRecoversWithoutAnotherModelTurn(t *testing.T) {
	b, ctx, lines := pauseBroker(t)
	started := time.Now().UTC()
	b.Clock = func() time.Time { return started }
	req := pauseRequest()
	_, ids, err := b.Store.DecidePause(ctx, req.Target, func(*store.PauseRow) (*store.PauseRow, []store.Effect, error) {
		p := store.PauseRow{Target: req.Target, ID: req.ID, Requester: req.Requester,
			Reason: req.Reason, WakeCondition: req.WakeCondition, AcceptedAt: started}
		return &p, []store.Effect{pauseDelivery(p, "pause")}, nil
	})
	if err != nil || len(ids) != 1 {
		t.Fatalf("recorded: %v, %v", ids, err)
	}
	if len(*lines) != 0 {
		t.Fatal("notification ran before recovery")
	}
	b.Clock = func() time.Time { return started.Add(time.Minute) }
	if n := b.RecoverEffects(ctx); n != 1 {
		t.Fatalf("recovered %d effects", n)
	}
	p, err := b.Store.Pause(ctx, req.ID)
	if err != nil || pauseState(p) != "delivered" || len(*lines) != 1 {
		t.Fatalf("recovered row %+v, %v, lines %v", p, err, *lines)
	}
	if n := b.RecoverEffects(ctx); n != 0 || len(*lines) != 1 {
		t.Fatalf("duplicate recovery %d, lines %v", n, *lines)
	}
}
