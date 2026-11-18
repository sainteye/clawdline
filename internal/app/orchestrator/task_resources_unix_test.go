//go:build darwin || linux

package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

func enableTestResources(t *testing.T, b *Broker) {
	t.Helper()
	// Policy fixtures own their process observation. Native adapters are
	// exercised separately; a hosted runner may have unreadable processes.
	b.ProcessUses = func(context.Context, string) string { return "absent" }
	if err := b.EnableTaskResources(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Resources.FS.Close() })
	b.Resources.FreeSpace = func() (int64, error) { return 1 << 40, nil }
}

func TestManagedCallbackUsesRegisteredPathsAndKeepsItsResults(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	id := "d0000000-0000-4000-8000-000000000001"
	req := callbackRequest(t, id, "sh", "-c", `printf temporary > "$TMPDIR/scratch"; printf rebuildable > "$GOCACHE/cache"; printf delivered > "$CLAWDLINE_ARTIFACT_DIR/result.txt"`)
	if _, err := b.StartCallback(ctx, req); err != nil {
		t.Fatal(err)
	}
	r := settledWithin(t, b, ctx, id, 10*time.Second)
	if r.State != StateSuccess {
		t.Fatal(r.Verdict)
	}
	rows, _, err := b.Store.TaskResourcesByOwnerPage(ctx, "task", id, "")
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	for _, res := range rows {
		if res.Owner.ID != id || res.Owner.Project != req.Dir {
			t.Fatal(res)
		}
		if _, err := b.Resources.Seal(ctx, ResourceOwnerScope(), res.ID); err != nil {
			t.Fatal(err)
		}
	}
	// Isolate the retention clock; actual command and file-use checks stay real.
	b.Resources.Clock = func() time.Time { return r.FinishedAt.Add(23 * time.Hour) }
	page, err := b.Resources.Inventory(ctx, ResourceOwnerScope(), "", taskresource.PageRowsLimit)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range page.Items {
		if i.Resource.Class == taskresource.Cache {
			if i.Decision.Outcome != "eligible" {
				t.Fatal(i.Decision)
			}
			continue
		}
		if i.Decision.Reason != "within_retention" {
			t.Fatal(i.Decision)
		}
	}
	b.Resources.Clock = func() time.Time { return r.FinishedAt.Add(25 * time.Hour) }
	result, _, err := b.SweepTaskResources(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 2 {
		t.Fatal(result)
	}
	for _, item := range result.Items {
		if item.Outcome != "removed" {
			t.Fatal(item)
		}
	}
	raw, err := os.ReadFile(filepath.Join(r.Dir, "artifacts", "result.txt"))
	if err != nil || string(raw) != "delivered" {
		t.Fatal(string(raw), err)
	}
	stored, _, err := b.Record(ctx, id)
	if err != nil || stored.Callback.Exit == nil {
		t.Fatal(stored, err)
	}
}

func TestManagedCallbackRefusesBeforeStartingOnDiskPressure(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	b.Resources.FreeSpace = func() (int64, error) { return 1, nil }
	req := callbackRequest(t, "d0000000-0000-4000-8000-000000000002", "sh", "-c", "touch should-not-run")
	req.Intent = CallbackIntentHeavy
	out, err := b.StartCallback(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if out.Record.State != StateBriefed {
		t.Fatal(out.Record.State)
	}
	if got := settledWithin(t, b, ctx, req.TaskID, 10*time.Second); got.State != StateFailure {
		t.Fatal(got.State)
	}
	if _, err := os.Stat(filepath.Join(req.Dir, "should-not-run")); !os.IsNotExist(err) {
		t.Fatal("command ran", err)
	}
	rows, _, _ := b.Store.TaskResourcesByOwnerPage(ctx, "task", req.TaskID, "")
	if len(rows) != 0 {
		t.Fatal(rows)
	}
}

func TestManagedStoragePressureKeepsCurrentCompileLease(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	free := int64(1 << 40)
	reads := 0
	b.Resources.FreeSpace = func() (int64, error) { reads++; return free, nil }
	holder := LeaseRequest{Resource: ResourceCompile, RequestID: uuid.NewString(), Holder: "existing build"}
	if a, err := b.Acquire(ctx, holder); err != nil || a.State != "granted" {
		t.Fatal(a, err)
	}
	before := reads
	free = 0
	if a, err := b.Acquire(ctx, holder); err != nil || a.State != "granted" {
		t.Fatal("renewal refused", a, err)
	}
	waiter := holder
	waiter.RequestID = uuid.NewString()
	waiter.Holder = "waiting build"
	for i := 0; i < 3; i++ {
		if a, err := b.Acquire(ctx, waiter); err != nil || a.State != "queued" {
			t.Fatal("waiting work was prematurely refused", a, err)
		}
	}
	if reads != before {
		t.Fatal("queue polling rescanned storage", before, reads)
	}
	if _, err := b.Release(ctx, LeaseOwner{Resource: ResourceCompile, RequestID: holder.RequestID}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Acquire(ctx, waiter); refusalCode(err) != "disk_reserve_unavailable" {
		t.Fatal("new grant ignored storage pressure", err)
	}
	if reads <= before {
		t.Fatal("grant did not check storage")
	}
}

func TestManagedRootNeedsBothSafeCloseAndSavedDelivery(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	a := storedRootForClose(t, b, ctx)
	if err := os.MkdirAll(filepath.Join(filepath.Dir(a.BriefPath), "artifacts"), 0o700); err != nil {
		t.Fatal(err)
	}
	alloc, err := b.Resources.Allocate(ctx, ResourceOwnerScope(), "root", a.ID, taskresource.Scratch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Resources.Seal(ctx, ResourceOwnerScope(), alloc.Resource.ID); err == nil {
		t.Fatal("unreported Root output sealed")
	}
	closed := session.Session{ID: a.Executor.TerminalID, Backend: session.BackendTmux, Assistant: session.AssistantCodex, ConversationID: uuid.NewString(), CWD: a.ProjectDir}
	active := true
	b.Reading = func(context.Context) session.Inventory {
		rows := []session.Session{{ID: "%other", Backend: session.BackendTmux, Assistant: session.AssistantClaude}}
		if active {
			rows = append(rows, closed)
		}
		return session.Inventory{Sessions: rows, Sources: map[string]bool{"tmux": true}}
	}
	b.Live = func(ctx context.Context) []session.Session { return b.Reading(ctx).Sessions }
	b.ProcessCWDs = func(context.Context) ([]string, error) { return []string{"/"}, nil }
	if _, err := b.ReportSessionDelivery(ctx, closed.ID, "Saved Root result."); err != nil {
		t.Fatal(err)
	}
	if o, err := b.resourceOwner(ctx, "root", a.ID); err != nil || o.Complete || o.OutputsSaved {
		t.Fatal("turn delivery completed Root", o, err)
	}
	if err := b.RecordRootClosure(ctx, a.ID, closed, "close", false); err != nil {
		t.Fatal(err)
	}
	active = false
	o, err := b.resourceOwner(ctx, "root", a.ID)
	if err != nil || !o.Complete || !o.OutputsSaved || o.Presence != "absent" {
		t.Fatal(o, err)
	}
	if _, err := b.Resources.Seal(ctx, ResourceOwnerScope(), alloc.Resource.ID); err != nil {
		t.Fatal(err)
	}
	b.Resources.Clock = func() time.Time { return time.Unix(o.CompletedAt, 0).Add(25 * time.Hour) }
	result, _, err := b.SweepTaskResources(ctx, "")
	if err != nil || len(result.Items) != 1 || result.Items[0].Outcome != "removed" {
		t.Fatal(result, err)
	}
	if _, err := os.Stat(o.OutputRoot); err != nil {
		t.Fatal("Root outputs removed", err)
	}

}

func TestManagedCallbackKeepsDescendantThatClosedItsInheritedLock(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	req := callbackRequest(t, "d0000000-0000-4000-8000-000000000003", "sh", "-c", `sleep 30 3>&- & exit 0`)
	if _, err := b.StartCallback(ctx, req); err != nil {
		t.Fatal(err)
	}
	r := settledWithin(t, b, ctx, req.TaskID, 10*time.Second)
	if r.Callback.PGID <= 0 {
		t.Fatal("callback process identity missing")
	}
	t.Cleanup(func() { _ = syscall.Kill(-r.Callback.PGID, syscall.SIGKILL) })
	if presence := b.callbackResourcePresence(r); presence != "present" {
		t.Fatal("descendant was mistaken for completion", presence)
	}
	rows, _, err := b.Store.TaskResourcesByOwnerPage(ctx, "task", r.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	b.Resources.Clock = func() time.Time { return r.FinishedAt.Add(25 * time.Hour) }
	for _, res := range rows {
		if _, err := b.Resources.Seal(ctx, ResourceOwnerScope(), res.ID); err != nil {
			t.Fatal(err)
		}
	}
	page, err := b.Resources.Inventory(ctx, ResourceOwnerScope(), "", taskresource.PageRowsLimit)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range page.Items {
		if i.Decision.Reason != "owner_present" {
			t.Fatal(i.Decision)
		}
	}
}

func TestManagedCallbackWithMissingExecutionIdentityIsUnknown(t *testing.T) {
	b, ctx := newTestBroker(t)
	r, files := stranded(t, b, ctx, "d0000000-0000-4000-8000-000000000004", 0)
	r, err := b.Settle(ctx, r.ID, StateSuccess, "exit status recovered without launch identity", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(files.log(), []byte("retained unknown execution"), 0o600); err != nil {
		t.Fatal(err)
	}
	if presence := b.callbackResourcePresence(r); presence != "unknown" {
		t.Fatal(presence)
	}
	decision := b.reclaimCallback(ctx, r, r.FinishedAt.Add(8*24*time.Hour), false)
	if decision.Outcome != ReclaimKept || decision.Reason != WhyOwnerUnknown {
		t.Fatal(decision)
	}
	if _, err := os.Stat(files.log()); err != nil {
		t.Fatal("unknown execution log removed", err)
	}
}

func TestManagedResourceUnavailabilityFencesNewHeavyWorkWithoutStoppingReads(t *testing.T) {
	b, ctx := newTestBroker(t)
	b.ResourceServiceRequired = true
	_, err := b.Acquire(ctx, LeaseRequest{Resource: ResourceCompile, RequestID: uuid.NewString(), Holder: "new build"})
	if refusalCode(err) != "resource_service_unavailable" {
		t.Fatal(err)
	}
	req := callbackRequest(t, "d0000000-0000-4000-8000-000000000005", "true")
	if _, err := b.StartCallback(ctx, req); err != nil {
		t.Fatal("ordinary wait refused", err)
	}
	r := settledWithin(t, b, ctx, req.TaskID, 10*time.Second)
	if r.State != StateSuccess {
		t.Fatal(r.Verdict)
	}
	if _, _, err := b.Record(ctx, req.TaskID); err != nil {
		t.Fatal("result read refused", err)
	}
}

func TestManagedCallbackRawLogKeepsAnOpenReaderAfterSevenDays(t *testing.T) {
	b, ctx := newTestBroker(t)
	req := callbackRequest(t, "d0000000-0000-4000-8000-000000000006", "sh", "-c", "echo kept")
	if _, err := b.StartCallback(ctx, req); err != nil {
		t.Fatal(err)
	}
	r := settledWithin(t, b, ctx, req.TaskID, 10*time.Second)
	file, err := os.Open((callbackFiles{r.Dir}).log())
	if err != nil {
		t.Fatal(err)
	}
	decision := b.reclaimCallback(ctx, r, r.FinishedAt.Add(8*24*time.Hour), false)
	_ = file.Close()
	if decision.Outcome != ReclaimKept || (decision.Reason != WhyOwnerPresent && decision.Reason != WhyOwnerUnknown) {
		t.Fatal(decision)
	}
	if _, err := os.Stat((callbackFiles{r.Dir}).log()); err != nil {
		t.Fatal("open log removed", err)
	}
}

func TestProcessUseObservationProtectsManagedScratchAndRawLogs(t *testing.T) {
	b, ctx := newTestBroker(t)
	enableTestResources(t, b)
	req := callbackRequest(t, "d0000000-0000-4000-8000-000000000007", "sh", "-c", `echo temporary > "$TMPDIR/data"; echo result > "$CLAWDLINE_ARTIFACT_DIR/result.txt"`)
	if _, err := b.StartCallback(ctx, req); err != nil {
		t.Fatal(err)
	}
	r := settledWithin(t, b, ctx, req.TaskID, 10*time.Second)
	rows, _, err := b.Store.TaskResourcesByOwnerPage(ctx, "task", r.ID, "")
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	for _, row := range rows {
		if _, err := b.Resources.Seal(ctx, ResourceOwnerScope(), row.ID); err != nil {
			t.Fatal(err)
		}
	}
	now := r.FinishedAt.Add(8 * 24 * time.Hour)
	b.Resources.Clock = func() time.Time { return now }
	for _, observation := range []string{"unknown", "present"} {
		b.ProcessUses = func(context.Context, string) string { return observation }
		page, err := b.Resources.Inventory(ctx, ResourceOwnerScope(), "", 2)
		if err != nil || len(page.Items) != 2 {
			t.Fatal(page, err)
		}
		for _, item := range page.Items {
			if item.Decision.Outcome != "kept" || item.Decision.Reason != "use_"+observation {
				t.Fatal(item.Decision)
			}
			if _, err := os.Stat(filepath.Join(b.Resources.FS.Path(), item.Resource.ID)); err != nil {
				t.Fatal(err)
			}
		}
		if d := b.reclaimCallback(ctx, r, now, false); d.Outcome != ReclaimKept {
			t.Fatal(d)
		}
		if _, err := os.Stat((callbackFiles{r.Dir}).log()); err != nil {
			t.Fatal("protected raw log removed", err)
		}
	}
	b.ProcessUses = func(context.Context, string) string { return "absent" }
	result, _, err := b.SweepTaskResources(ctx, "")
	if err != nil || len(result.Items) != 2 {
		t.Fatal(result, err)
	}
	for _, item := range result.Items {
		if item.Outcome != "removed" {
			t.Fatal(item)
		}
	}
	if d := b.reclaimCallback(ctx, r, now, false); d.Outcome != ReclaimRemoved {
		t.Fatal(d)
	}
	if raw, err := os.ReadFile(filepath.Join(r.Dir, "artifacts", "result.txt")); err != nil || string(raw) != "result\n" {
		t.Fatal("retained output changed", string(raw), err)
	}
}
