//go:build darwin || linux

package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

func TestCallbackAcceptanceDoesNotWaitForResourceAdmission(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	b.Resources.OtherUsage = func(c context.Context) (taskresource.Usage, error) {
		close(entered)
		select {
		case <-release:
			return taskresource.Usage{Known: true}, nil
		case <-c.Done():
			return taskresource.Usage{}, c.Err()
		}
	}
	req := callbackRequest(t, "e9000000-0000-4000-8000-000000000001", "sh", "-c", `printf execution >> "$1"`, "probe", filepath.Join(t.TempDir(), "execution"))
	req.Intent = CallbackIntentHeavy
	caller, cancel := context.WithCancel(ctx)
	defer cancel()
	type answer struct {
		out Dispatched
		err error
	}
	done := make(chan answer, 1)
	go func() { out, err := b.StartCallback(caller, req); done <- answer{out, err} }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("resource admission was not reached")
	}
	var got answer
	select {
	case got = <-done:
	case <-time.After(300 * time.Millisecond):
		once.Do(func() { close(release) })
		got = <-done
		t.Error("accepted callback waited for resource admission")
		return
	}
	if got.err != nil || got.out.Record.State != StateBriefed || got.out.Record.Callback.PID != 0 {
		t.Fatal(got.out.Record.State, got.err)
	}
	cancel()
	replay, err := b.StartCallback(ctx, req)
	if err != nil || !replay.Replayed {
		t.Fatal(replay.Replayed, err)
	}
	once.Do(func() { close(release) })
	r := settledWithin(t, b, ctx, req.TaskID, 5*time.Second)
	if r.State != StateSuccess {
		t.Fatal(r.Verdict)
	}
	raw, err := os.ReadFile(req.Argv[len(req.Argv)-1])
	if err != nil || string(raw) != "execution" {
		t.Fatal("command did not run exactly once", string(raw), err)
	}
}

func TestCallbackAdmissionFailureIsRecordedAfterItsContextExpires(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	r, files := stranded(t, b, ctx, "e9000000-0000-4000-8000-000000000002", 0)
	r.Callback.Intent = CallbackIntentHeavy
	if err := b.save(ctx, r, HashSecret("fixture"), "task.briefed"); err != nil {
		t.Fatal(err)
	}
	attempt, cancel := context.WithCancel(ctx)
	defer cancel()
	b.Resources.OtherUsage = func(context.Context) (taskresource.Usage, error) {
		cancel()
		return taskresource.Usage{}, context.DeadlineExceeded
	}
	runCallbackStart(attempt, b, store.Effect{Kind: EffectCallbackStart, Subject: r.ID, Attempts: 0})
	got, _, err := b.Record(ctx, r.ID)
	if err != nil || got.State != StateFailure || !got.Callback.NotStarted {
		t.Fatal("expired admission lost its failure receipt", got.State, err)
	}
	if !strings.Contains(got.Verdict, "resource_measurement_interrupted") {
		t.Fatal(got.Verdict)
	}
	if attempted(files) {
		t.Fatal("failed admission started the command")
	}
}

func TestCallbackCancelledDuringAdmissionNeverStarts(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	b.Resources.OtherUsage = func(context.Context) (taskresource.Usage, error) {
		close(entered)
		<-release
		close(finished)
		return taskresource.Usage{Known: true}, nil
	}
	marker := filepath.Join(t.TempDir(), "must-not-run")
	req := callbackRequest(t, "e9000000-0000-4000-8000-000000000003", "touch", marker)
	req.Intent = CallbackIntentHeavy
	if _, err := b.StartCallback(ctx, req); err != nil {
		t.Fatal(err)
	}
	<-entered
	if _, err := b.CancelTask(ctx, req.TaskID, "cancel during preparation", "", CancelCaller{Person: true, Principal: "local"}); err != nil {
		t.Fatal(err)
	}
	once.Do(func() { close(release) })
	<-finished
	deadline := time.Now().Add(3 * time.Second)
	for b.startOpen(ctx, req.TaskID) {
		if time.Now().After(deadline) {
			t.Fatal("cancelled admission effect did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	got := settledWithin(t, b, ctx, req.TaskID, 3*time.Second)
	if got.State != StateCancelled {
		t.Fatal(got.State)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("cancelled preparation executed its command", err)
	}
}
