package projectassistantstore

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

const concurrentToolActivity = "concurrent-tool"

type sendLockOrderStore struct {
	*Store
	memoryLocked, runSend, actorLocked, commitTool chan struct{}
}

func (s *sendLockOrderStore) WithCurrent(ctx context.Context, key pa.BotKey, revision int64, send func(pa.ContextSnapshot) error) error {
	return s.Store.WithCurrent(ctx, key, revision, func(snapshot pa.ContextSnapshot) error {
		close(s.memoryLocked)
		select {
		case <-s.runSend:
			return send(snapshot)
		case <-ctx.Done():
			return ctx.Err()
		}
	})
}

func (s *sendLockOrderStore) Commit(ctx context.Context, m app.Mutation) (pa.Receipt, error) {
	if m.Receipt.ResourceID == "tool-activity-"+concurrentToolActivity {
		// The Engine holds the actor here. Pause before the real Commit so
		// a failing test can release the actor without leaving a deadlock.
		close(s.actorLocked)
		select {
		case <-s.commitTool:
		case <-ctx.Done():
			return pa.Receipt{}, ctx.Err()
		}
	}
	return s.Store.Commit(ctx, m)
}

func TestSendDoesNotWaitForActorDuringToolJournalCommit(t *testing.T) {
	_, store, _ := testDB(t)
	bot := seed(t, store, "one")
	repo := &sendLockOrderStore{
		Store: store, memoryLocked: make(chan struct{}), runSend: make(chan struct{}),
		actorLocked: make(chan struct{}), commitTool: make(chan struct{}),
	}
	peer := &recoveryPeer{sent: make(chan app.PromptRequest, 2), result: make(chan app.PromptResult, 1)}
	var ids atomic.Int64
	runtime, err := app.NewRuntime(app.RuntimeOptions{
		Repository: repo, Memory: repo, WorkBridge: peer, ProjectResolver: recoveryResolver{bot.Project}, Provider: peer,
		Clock: time.Now, ID: func() string { return fmt.Sprintf("lock-order-%d", ids.Add(1)) },
		Authorize: func(context.Context, pa.BotKey) (pa.FeatureState, pa.SourceStatus, error) {
			return pa.FeatureState{Enabled: true}, recoverySource(bot.Key), nil
		},
		Limits: app.RuntimeLimits{MaxBots: 1, MaxProcesses: 1, PromptTimeout: 10 * time.Second, CleanupTimeout: time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(testContext, 3*time.Second)
		defer cancel()
		if err := runtime.Close(ctx); err != nil {
			t.Error(err)
		}
	})

	toolContext, cancelTool := context.WithCancel(testContext)
	defer cancelTool()
	deadline := time.NewTimer(4 * time.Second)
	defer deadline.Stop()

	receipt, err := runtime.Submit(testContext, recoverySubmission(t, store, bot, "input"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-repo.memoryLocked:
	case <-deadline.C:
		t.Fatal("native send did not reach the memory guard")
	}
	turn, err := store.Turn(testContext, bot.Key, receipt.ResourceID)
	if err != nil {
		t.Fatal(err)
	}

	toolDone := make(chan error, 1)
	go func() {
		toolDone <- runtime.RecordToolActivity(toolContext, turn.Fence, turn.ID, concurrentToolActivity)
	}()
	select {
	case <-repo.actorLocked:
	case err := <-toolDone:
		t.Fatalf("tool journal returned before entering the commit: %v", err)
	case <-deadline.C:
		t.Fatal("tool journal did not reach the commit while owning the actor")
	}

	close(repo.runSend)
	select {
	case <-peer.sent:
	case <-deadline.C:
		t.Fatal("native send waited for the actor while holding the store lock")
	}
	close(repo.commitTool)
	select {
	case err := <-toolDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-deadline.C:
		t.Fatal("tool journal did not commit after native send")
	}

	peer.result <- app.PromptResult{FinalText: "Synthetic reply"}
	recoveryAwait(t, func() bool {
		current, err := store.Turn(testContext, bot.Key, turn.ID)
		return err == nil && current.State == pa.TurnSucceeded
	})
	record, err := store.Export(testContext, bot.Key)
	if err != nil {
		t.Fatal(err)
	}
	activity := 0
	for _, event := range record.Events {
		if event.Trace != nil && event.Trace.Category == "tool_activity" && event.Trace.Text == concurrentToolActivity {
			activity++
		}
	}
	if sends := peer.sends.Load(); sends != 1 || activity != 1 {
		t.Fatalf("native sends = %d, durable tool activities = %d, want one of each", sends, activity)
	}
}
