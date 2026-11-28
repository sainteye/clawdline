package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

func TestReclaimChecksProvableOwnersBeforeUnknownOwners(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Now()
	b.Clock = func() time.Time { return now }
	sources := map[string]bool{"iterm": false, "tmux": true}
	b.Reading = func(_ context.Context) session.Inventory {
		return session.Inventory{
			Sessions: []session.Session{{ID: "%live"}},
			Sources:  sources,
		}
	}
	b.ProcessCWDs = func(_ context.Context) ([]string, error) { return []string{"/"}, nil }
	b.ProcessUses = func(_ context.Context, _ string) string { return "absent" }

	var blockedFile, eligibleFile string
	for i := range reclaimSweepLimit + 8430 {
		id := fmt.Sprintf("ee3c0000-0000-4000-8000-%012x", i)
		finishedAt := now.Add(-artifactRetention() - 48*time.Hour).Add(time.Duration(i) * time.Second)
		r := Record{Protocol: Protocol, ID: id, State: StateSuccess, CreatedAt: finishedAt, FinishedAt: finishedAt,
			Dir: b.Tasks.Path(id), ChildTerminalID: "%finished", ChildBackend: "iterm"}
		switch {
		case i < reclaimSweepLimit:
			file := reclaimFixtureArtifact(t, r)
			if i == 0 {
				blockedFile = file
			}
		case i == reclaimSweepLimit+8429:
			r.ChildBackend = "tmux"
			eligibleFile = reclaimFixtureArtifact(t, r)
		}
		if err := b.save(ctx, r, HashSecret("s"), "test.fixture"); err != nil {
			t.Fatal(err)
		}
	}

	rep, err := b.Reclaim(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("removed=%d deferred=%d kept=%d", rep.Removed, rep.Deferred, rep.Kept)
	if rep.Removed != 1 || rep.Deferred != 1 || rep.Kept != reclaimSweepLimit-1 {
		t.Fatalf("reclaim should remove the provably idle artifact before the older blocked tasks and defer only the unchecked candidate")
	}
	if _, err := os.Stat(eligibleFile); !os.IsNotExist(err) {
		t.Fatalf("eligible artifact survived: %v", err)
	}
	if _, err := os.Stat(blockedFile); err != nil {
		t.Fatalf("artifact with unverified owner went away: %v", err)
	}
	keptID := filepath.Base(filepath.Dir(filepath.Dir(blockedFile)))
	if d := w6Decision(rep, keptID, ReclaimTaskArtifacts); d.Reason != WhyOwnerUnknown {
		t.Fatalf("missing owner-source reason: %+v", d)
	}

	sources["iterm"] = true
	rep, err = b.Reclaim(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("source healthy: removed=%d deferred=%d kept=%d", rep.Removed, rep.Deferred, rep.Kept)
	if rep.Removed != reclaimSweepLimit || rep.Deferred != 0 {
		t.Fatalf("artifacts are still waiting after their source became readable")
	}
}

func TestReclaimRecordsTimedOutOwnerProofAndChecksTheNextTask(t *testing.T) {
	b, ctx := newTestBroker(t)
	finishedAt := time.Now().Add(-artifactRetention() - time.Hour)
	b.Reading = func(_ context.Context) session.Inventory {
		return session.Inventory{
			Sessions: []session.Session{{ID: "%live"}},
			Sources:  map[string]bool{"tmux": true},
		}
	}
	firstRead := true
	b.ProcessCWDs = func(_ context.Context) ([]string, error) {
		if firstRead {
			firstRead = false
			return nil, fmt.Errorf("injected process-table failure: %w", context.DeadlineExceeded)
		}
		return []string{"/"}, nil
	}
	b.ProcessUses = func(_ context.Context, _ string) string { return "absent" }

	var records [2]Record
	var paths [2]string
	for i := range records {
		id := fmt.Sprintf("ee3d0000-0000-4000-8000-%012x", i)
		r := Record{Protocol: Protocol, ID: id, State: StateSuccess, CreatedAt: finishedAt, FinishedAt: finishedAt,
			Dir: b.Tasks.Path(id), ChildTerminalID: "%finished", ChildBackend: "tmux"}
		records[i] = r
		paths[i] = reclaimFixtureArtifact(t, r)
		if err := b.save(ctx, r, HashSecret("s"), "test.fixture"); err != nil {
			t.Fatal(err)
		}
	}

	rep, err := b.Reclaim(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("injected process-table timeout: removed=%d deferred=%d kept=%d", rep.Removed, rep.Deferred, rep.Kept)
	if rep.Removed != 1 || rep.Deferred != 0 || rep.Kept != 1 {
		t.Fatalf("a deadline on one candidate should preserve it and let the next one proceed")
	}
	if d := w6Decision(rep, records[0].ID, ReclaimTaskArtifacts); d.Reason != taskresource.ProcessTableTimeout {
		t.Fatalf("missing deadline in sweep report: %+v", d)
	}
	row, err := b.Store.ReclaimOf(ctx, records[0].ID, ReclaimTaskArtifacts)
	if err != nil || row.Outcome != ReclaimKept || row.Reason != taskresource.ProcessTableTimeout {
		t.Fatalf("missing standing deadline decision: %+v, %v", row, err)
	}
	if _, err := os.Stat(paths[0]); err != nil {
		t.Fatalf("timed out proof removed its artifact: %v", err)
	}
	if _, err := os.Stat(paths[1]); !os.IsNotExist(err) {
		t.Fatalf("the next task did not reclaim its artifact: %v", err)
	}
	rep, err = b.Reclaim(ctx, false)
	if err != nil || rep.Removed != 1 || rep.Deferred != 0 {
		t.Fatalf("the next sweep did not retry the timed out resource: %+v, %v", rep, err)
	}
}

func TestReclaimRecordsSweepDeadlineAndDefersTheNextTask(t *testing.T) {
	b, ctx := newTestBroker(t)
	finishedAt := time.Now().Add(-artifactRetention() - time.Hour)
	b.Reading = func(context.Context) session.Inventory {
		return session.Inventory{Sessions: []session.Session{{ID: "%live"}}, Sources: map[string]bool{"tmux": true}}
	}
	b.ProcessCWDs = func(ctx context.Context) ([]string, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	for i := range 2 {
		id := fmt.Sprintf("ee3d0000-0000-4000-8000-%012x", i)
		r := Record{Protocol: Protocol, ID: id, State: StateSuccess, CreatedAt: finishedAt, FinishedAt: finishedAt,
			Dir: b.Tasks.Path(id), ChildTerminalID: "%finished", ChildBackend: "tmux"}
		reclaimFixtureArtifact(t, r)
		if err := b.save(ctx, r, HashSecret("s"), "test.fixture"); err != nil {
			t.Fatal(err)
		}
	}
	sweep, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	rep, err := b.Reclaim(sweep, false)
	if err != nil || rep.Removed != 0 || rep.Deferred != 1 || rep.Kept != 1 ||
		len(rep.Decisions) != 1 || rep.Decisions[0].Reason != WhyReclaimDeadline {
		t.Fatalf("expected one sweep deadline and one deferred task: %+v, %v", rep, err)
	}
	row, err := b.Store.ReclaimOf(ctx, rep.Decisions[0].Task, ReclaimTaskArtifacts)
	if err != nil || row.Outcome != ReclaimKept || row.Reason != WhyReclaimDeadline {
		t.Fatalf("missing standing sweep deadline: %+v, %v", row, err)
	}
	t.Logf("whole-sweep deadline: removed=%d deferred=%d kept=%d", rep.Removed, rep.Deferred, rep.Kept)
}

func reclaimFixtureArtifact(t *testing.T, r Record) string {
	t.Helper()
	dir := filepath.Join(r.Dir, "artifacts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "evidence.txt")
	if err := os.WriteFile(path, []byte("fixture output"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
