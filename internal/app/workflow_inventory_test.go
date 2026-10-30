package app

import (
	"context"
	"database/sql"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/workflow"
	"path/filepath"
	"testing"
)

func TestWorkflowMarkerSurvivesTerminalAndProcessMergeInEitherOrder(t *testing.T) {
	process := session.Session{ID: "ttys001", TTY: "ttys001", PID: 44, Assistant: session.AssistantCodex, ConversationID: "original", WorkflowLaunchID: "abcdefghijklmnopqrstuv"}
	terminal := session.Session{ID: "%1", TTY: "ttys001", Backend: session.BackendTmux}
	for _, pair := range [][2]session.Session{{process, terminal}, {terminal, process}} {
		got := richer(pair[0], pair[1])
		if got.WorkflowLaunchID != process.WorkflowLaunchID || got.ID != "%1" {
			t.Fatalf("merge lost immutable workflow identity: %+v", got)
		}
	}
}

func TestWorkflowReconciliationContinuesAfterAnUnrelatedConflict(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	snap, err := workflow.Seal(workflow.Snapshot{Schema: 1, ProfileID: "clawdline", Version: "1", Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	bad, err := st.PrepareWorkflowLaunch(ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RecordWorkflowTerminal(ctx, bad.ID, "%wrong"); err != nil {
		t.Fatal(err)
	}
	good, err := st.PrepareWorkflowLaunch(ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	inv := session.Inventory{Sessions: []session.Session{
		{ID: "%bad", Assistant: session.AssistantCodex, ConversationID: "bad", WorkflowLaunchID: bad.ID},
		{ID: "%good", Assistant: session.AssistantCodex, ConversationID: "good", WorkflowLaunchID: good.ID},
	}}
	if err := ReconcileWorkflowLaunches(ctx, st, inv); err == nil {
		t.Fatal("conflict was hidden")
	}
	_, found, err := st.WorkflowSnapshotForConversation(ctx, "good")
	if err != nil || !found {
		t.Fatalf("unrelated conflict blocked valid launch: %v", err)
	}
}

func TestWorkflowReconciliationContinuesAfterACorruptSnapshot(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.sqlite")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	badSnap, err := workflow.Seal(workflow.Snapshot{Schema: 1, ProfileID: "clawdline", Version: "1", Source: "bad"})
	if err != nil {
		t.Fatal(err)
	}
	goodSnap, err := workflow.Seal(workflow.Snapshot{Schema: 1, ProfileID: "clawdline", Version: "1", Source: "good"})
	if err != nil {
		t.Fatal(err)
	}
	bad, err := st.PrepareWorkflowLaunch(ctx, badSnap)
	if err != nil {
		t.Fatal(err)
	}
	good, err := st.PrepareWorkflowLaunch(ctx, goodSnap)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(path, store.DBFile))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE workflow_snapshots SET document=? WHERE id=?`, []byte(`{"schema":1}`), bad.SnapshotID); err != nil {
		t.Fatal(err)
	}
	inv := session.Inventory{Sessions: []session.Session{
		{ID: "%bad", Assistant: session.AssistantCodex, ConversationID: "bad", WorkflowLaunchID: bad.ID},
		{ID: "%good", Assistant: session.AssistantCodex, ConversationID: "good", WorkflowLaunchID: good.ID},
	}}
	if err := ReconcileWorkflowLaunches(ctx, st, inv); err == nil {
		t.Fatal("corruption was hidden")
	}
	got, found, err := st.WorkflowSnapshotForConversation(ctx, "good")
	if err != nil || !found || got.Digest != goodSnap.Digest {
		t.Fatalf("corruption blocked healthy launch: %+v %v", got, err)
	}
}
