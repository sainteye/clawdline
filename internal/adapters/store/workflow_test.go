package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/workflow"
)

func sealedWorkflow(t *testing.T, instructions string) workflow.Snapshot {
	t.Helper()
	s, err := workflow.Seal(workflow.Snapshot{ProfileID: "custom.test", Version: "1", Source: "local", Instructions: instructions})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestWorkflowLaunchPersistsAndBindsOnlyRecordedTerminal(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	snap := sealedWorkflow(t, "original")
	first, err := s.PrepareWorkflowLaunch(ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.PrepareWorkflowLaunch(ctx, sealedWorkflow(t, "different"))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.SnapshotID == second.SnapshotID {
		t.Fatal("launch and snapshot identities were not independent")
	}
	if err := s.BindWorkflowConversation(ctx, first.ID, "terminal-1", "conversation-1"); !errors.Is(err, ErrWorkflowLaunchConflict) {
		t.Fatalf("unrecorded terminal: %v", err)
	}
	if err := s.RecordWorkflowTerminal(ctx, first.ID, "terminal-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordWorkflowTerminal(ctx, first.ID, "terminal-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordWorkflowTerminal(ctx, first.ID, "terminal-2"); !errors.Is(err, ErrWorkflowLaunchConflict) {
		t.Fatalf("terminal overwrite: %v", err)
	}
	if err := s.BindWorkflowConversation(ctx, first.ID, "terminal-2", "conversation-1"); !errors.Is(err, ErrWorkflowLaunchConflict) {
		t.Fatalf("wrong terminal: %v", err)
	}
	for range 2 {
		if err := s.BindWorkflowConversation(ctx, first.ID, "terminal-1", "conversation-1"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.BindWorkflowConversation(ctx, first.ID, "terminal-1", "conversation-2"); !errors.Is(err, ErrWorkflowLaunchConflict) {
		t.Fatalf("conversation overwrite: %v", err)
	}
	if err := s.FailWorkflowLaunch(ctx, first.ID); !errors.Is(err, ErrWorkflowLaunchConflict) {
		t.Fatalf("bound launch failed: %v", err)
	}
	if err := s.RecordWorkflowTerminal(ctx, second.ID, "terminal-2"); err != nil {
		t.Fatal(err)
	}
	if err := s.BindWorkflowConversation(ctx, second.ID, "terminal-2", "conversation-1"); !errors.Is(err, ErrWorkflowConversationTaken) {
		t.Fatalf("conversation taken: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, ok, err := s.WorkflowSnapshotForConversation(ctx, "conversation-1")
	if err != nil || !ok || got.Digest != snap.Digest || got.Instructions != "original" {
		t.Fatalf("reopen snapshot: %+v %t %v", got, ok, err)
	}
	launch, ok, err := s.WorkflowLaunchByID(ctx, first.ID)
	if err != nil || !ok || launch.State != "bound" || launch.TerminalID != "terminal-1" || launch.ConversationID != "conversation-1" {
		t.Fatalf("launch: %+v %t %v", launch, ok, err)
	}
	if _, ok, err := s.WorkflowSnapshotForConversation(ctx, "legacy"); err != nil || ok {
		t.Fatalf("legacy: %t %v", ok, err)
	}
	if _, err := s.PrepareWorkflowLaunch(ctx, sealedWorkflow(t, "new settings")); err != nil {
		t.Fatal(err)
	}
	got, _, err = s.WorkflowSnapshotForConversation(ctx, "conversation-1")
	if err != nil || got.Digest != snap.Digest {
		t.Fatal("historical snapshot changed")
	}
}

func TestWorkflowFailedLaunchAndInvalidSnapshotCannotBind(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	snap := sealedWorkflow(t, "")
	bad := snap
	bad.GraphifyEnabled = true
	if _, err := s.PrepareWorkflowLaunch(ctx, bad); err == nil {
		t.Fatal("accepted bad digest")
	}
	large := sealedWorkflow(t, strings.Repeat("x", MaxWorkflowSnapshotBytes))
	if _, err := s.PrepareWorkflowLaunch(ctx, large); !errors.Is(err, ErrWorkflowSnapshotTooLarge) {
		t.Fatalf("size: %v", err)
	}
	launch, err := s.PrepareWorkflowLaunch(ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.FailWorkflowLaunch(ctx, launch.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RecordWorkflowTerminal(ctx, launch.ID, "terminal"); !errors.Is(err, ErrWorkflowLaunchConflict) {
		t.Fatalf("failed terminal: %v", err)
	}
	if err := s.BindWorkflowConversation(ctx, launch.ID, "terminal", "conversation"); !errors.Is(err, ErrWorkflowLaunchConflict) {
		t.Fatalf("failed bind: %v", err)
	}
	if err := s.RecordWorkflowTerminal(ctx, "unknown", "terminal"); !errors.Is(err, ErrWorkflowLaunchUnknown) {
		t.Fatalf("unknown: %v", err)
	}
	if _, ok, err := s.WorkflowLaunchByID(ctx, "unknown"); err != nil || ok {
		t.Fatalf("unknown read: %t %v", ok, err)
	}
}

func TestWorkflowConcurrentConversationBindingHasOneOwner(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	first, err := s.PrepareWorkflowLaunch(ctx, sealedWorkflow(t, "one"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.PrepareWorkflowLaunch(ctx, sealedWorkflow(t, "two"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordWorkflowTerminal(ctx, first.ID, "one"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordWorkflowTerminal(ctx, second.ID, "two"); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, l := range []WorkflowLaunch{first, second} {
		wg.Add(1)
		go func(l WorkflowLaunch) {
			defer wg.Done()
			terminal := "one"
			if l.ID == second.ID {
				terminal = "two"
			}
			results <- s.BindWorkflowConversation(ctx, l.ID, terminal, "shared")
		}(l)
	}
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrWorkflowConversationTaken) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflicts)
	}
}

func TestWorkflowFullStoreRefusesWithoutEvictingOriginal(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	first, err := s.PrepareWorkflowLaunch(ctx, sealedWorkflow(t, "original"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.ExecContext(ctx, `WITH RECURSIVE slots(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM slots WHERE n<?)
 INSERT INTO workflow_launches(id,snapshot_id,state,created_at) SELECT 'full-'||n,?,'failed',1 FROM slots`, MaxWorkflowLaunches-1, first.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PrepareWorkflowLaunch(ctx, sealedWorkflow(t, "different")); !errors.Is(err, ErrWorkflowLaunchesFull) {
		t.Fatalf("full: %v", err)
	}
	got, ok, err := s.WorkflowLaunchByID(ctx, first.ID)
	if err != nil || !ok || got.Snapshot.Instructions != "original" {
		t.Fatalf("evicted original: %+v %t %v", got, ok, err)
	}
	var snapshots int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM workflow_snapshots`).Scan(&snapshots); err != nil || snapshots != 1 {
		t.Fatalf("orphan snapshots: %d %v", snapshots, err)
	}
}

func TestWorkflowCorruptSnapshotFailsClosed(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	first, err := s.PrepareWorkflowLaunch(ctx, sealedWorkflow(t, "original"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordWorkflowTerminal(ctx, first.ID, "terminal"); err != nil {
		t.Fatal(err)
	}
	if err := s.BindWorkflowConversation(ctx, first.ID, "terminal", "conversation"); err != nil {
		t.Fatal(err)
	}
	for _, document := range []string{`{"schema":1}`, strings.Repeat("x", MaxWorkflowSnapshotBytes+1)} {
		if _, err := s.db.ExecContext(ctx, `UPDATE workflow_snapshots SET document=? WHERE id=?`, document, first.SnapshotID); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := s.WorkflowSnapshotForConversation(ctx, "conversation"); err == nil || ok {
			t.Fatalf("corrupt conversation accepted: %t %v", ok, err)
		}
		if _, ok, err := s.WorkflowLaunchByID(ctx, first.ID); err == nil || ok {
			t.Fatalf("corrupt launch accepted: %t %v", ok, err)
		}
	}
}

func TestWorkflowResumeRetainsSnapshotAcrossNewTerminalAndRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := sealedWorkflow(t, "original")
	first, err := s.PrepareWorkflowLaunch(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordWorkflowTerminal(ctx, first.ID, "first"); err != nil {
		t.Fatal(err)
	}
	if err := s.BindWorkflowConversation(ctx, first.ID, "first", "conversation"); err != nil {
		t.Fatal(err)
	}
	resume, err := s.PrepareWorkflowLaunch(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if resume.ID == first.ID || resume.SnapshotID != first.SnapshotID {
		t.Fatal("resume did not retain isolated launch and original snapshot")
	}
	if err := s.RecordWorkflowTerminal(ctx, resume.ID, "resumed"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.BindWorkflowConversation(ctx, resume.ID, "resumed", "conversation"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, ok, err := s.WorkflowSnapshotForConversation(ctx, "conversation")
	if err != nil || !ok || got.Digest != snapshot.Digest || got.Instructions != "original" {
		t.Fatalf("resume snapshot: %+v %t %v", got, ok, err)
	}
	original, ok, err := s.WorkflowLaunchByID(ctx, first.ID)
	if err != nil || !ok || original.TerminalID != "first" {
		t.Fatalf("original overwritten: %+v %t %v", original, ok, err)
	}
}

func TestWorkflowUsageMeasuresRetainedLaunchesAndLargestSnapshot(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	usage, err := s.WorkflowUsage(ctx)
	if err != nil || usage.Launches != 0 || usage.LargestSnapshotBytes != 0 {
		t.Fatalf("fresh usage: %+v %v", usage, err)
	}
	snap := sealedWorkflow(t, "original")
	for range 2 {
		if _, err := s.PrepareWorkflowLaunch(ctx, snap); err != nil {
			t.Fatal(err)
		}
	}
	document, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	usage, err = s.WorkflowUsage(ctx)
	if err != nil || usage.Launches != 2 || usage.LargestSnapshotBytes != int64(len(document)) {
		t.Fatalf("usage: %+v %v", usage, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WorkflowUsage(ctx); err == nil {
		t.Fatal("closed store reported measured zero")
	}
}
