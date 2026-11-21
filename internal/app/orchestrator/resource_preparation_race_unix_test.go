//go:build darwin || linux

package orchestrator

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

// A poll that read the receipts while a preparation was still measuring, and
// decided to start one, must not start a second measurement when that
// preparation finished — and wrote its receipt — in the meantime. The next
// poll uses that receipt. Before the fix this interleaving measured twice,
// which made TestPreparationRestartRequiresAFreshServiceProof fail about one
// run in ten.
func TestAPollThatReadAnOldReceiptDoesNotMeasureAgain(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	b.AsyncResourceAdmission = true
	var calls atomic.Int32
	release := make(chan struct{})
	b.Resources.OtherUsage = func(context.Context) (taskresource.Usage, error) {
		if calls.Add(1) == 1 {
			<-release
		}
		return taskresource.Usage{Known: true}, nil
	}
	req := LeaseRequest{Resource: ResourceCompile, RequestID: uuid.NewString(), Holder: "synthetic interleaving"}
	if a, err := b.Acquire(ctx, req); err != nil || a.State != "queued" {
		t.Fatal(a, err)
	}
	var once sync.Once
	b.resourcePreparation.beforeRestart = func() {
		once.Do(func() {
			// This poll has read no fresh receipt. Let the running
			// preparation finish and write one before the poll goes on.
			close(release)
			waitPreparationIdle(t, b)
		})
	}
	if _, err := b.Acquire(ctx, req); err != nil {
		t.Fatal(err)
	}
	b.resourcePreparation.beforeRestart = nil
	preparedLease(t, b, ctx, req)
	waitPreparationIdle(t, b)
	if got := calls.Load(); got != 1 {
		t.Fatalf("one request was measured %d times", got)
	}
}
