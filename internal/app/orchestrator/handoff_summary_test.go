package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestUploadedHandoffSummaryIsWrittenAndDeliveredOnce(t *testing.T) {
	b, ctx := newTestBroker(t)
	keys := &typedKeys{}
	b.Type = keys.Type
	b.Launcher = &openingLauncher{pane: "%51"}
	b.Live = func(context.Context) []session.Session {
		return []session.Session{
			{ID: "%1", Assistant: session.AssistantCodex, ConversationID: rootConversation},
			{ID: "%51", Assistant: session.AssistantCodex},
		}
	}
	// A scheduled Codex caller remains an ordinary live sender for handoff.
	r := Record{Protocol: Protocol, ID: NewUUID(), Assistant: "codex", Title: "Scheduled export",
		State: StateBriefed, ScheduleID: NewUUID(), ChildTerminalID: "%1", ChildConversationID: rootConversation,
		PermissionMode: "full", CreatedAt: time.Now(), Claims: []string{}, ProjectDir: t.TempDir(), TimeoutMinutes: 30}
	if err := b.save(ctx, r, HashSecret("test-secret"), "task.briefed"); err != nil {
		t.Fatal(err)
	}
	yes := true
	summary := goodMilestone
	req := HandoffRequest{ID: NewUUID(), ProjectDir: r.ProjectDir, FromSession: rootConversation,
		Assistant: "codex", Plain: &yes, Milestone: &yes, Summary: &summary}
	h, replayed, err := b.OpenHandoff(ctx, req)
	if err != nil || replayed || h.State != HandoffDelivered {
		t.Fatalf("handoff: %+v replayed=%v err=%v", h, replayed, err)
	}
	path := filepath.Join(b.HandoffRoot(), req.ID, "handoff.md")
	data, err := os.ReadFile(path)
	if err != nil || string(data) != goodMilestone {
		t.Fatalf("summary = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), MilestoneObligationsFile)); err != nil {
		t.Fatal(err)
	}
	summary = strings.Replace(goodMilestone, "export route", "different route", 1)
	if _, replayed, err := b.OpenHandoff(ctx, req); err != nil || !replayed {
		t.Fatalf("replay: %v %v", replayed, err)
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != goodMilestone || keys.count("%51") != 1 {
		t.Fatalf("replay rewrote package or typed twice: %q %v %v", data, err, keys.lines)
	}
}

func TestUploadedHandoffSummaryRefusesInvalidInputBeforeWriting(t *testing.T) {
	for _, tc := range []struct {
		name, summary string
		milestone     bool
	}{
		{"malformed", "## Goal\nx\n", true},
		{"oversized", goodMilestone + strings.Repeat("x", MilestoneSummaryLimit), true},
		{"not-milestone", goodMilestone, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, ctx := newTestBroker(t)
			yes := true
			req := HandoffRequest{ID: NewUUID(), ProjectDir: t.TempDir(), FromSession: rootConversation,
				Plain: &yes, Milestone: &tc.milestone, Summary: &tc.summary}
			if _, _, err := b.OpenHandoff(ctx, req); refusalCode(err) != "bad_milestone_summary" {
				t.Fatalf("refusal: %v", err)
			}
			if _, err := os.Stat(filepath.Join(b.HandoffRoot(), req.ID)); !os.IsNotExist(err) {
				t.Fatalf("invalid summary created package: %v", err)
			}
			if hs, err := b.Handoffs(ctx); err != nil || len(hs) != 0 {
				t.Fatalf("invalid summary opened handoff: %v %v", hs, err)
			}
		})
	}
}

func TestHandoffSummaryPublicationPreservesExistingFiles(t *testing.T) {
	b, _ := newTestBroker(t)
	id := NewUUID()
	if err := b.writeHandoffSummary(id, []byte(goodMilestone)); err != nil {
		t.Fatal(err)
	}
	if err := b.writeHandoffSummary(id, []byte(goodMilestone)); err != nil {
		t.Fatalf("identical retry: %v", err)
	}
	if err := b.writeHandoffSummary(id, []byte("different")); refusalCode(err) != "handoff_package_conflict" {
		t.Fatalf("different retry: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(b.HandoffRoot(), id, "handoff.md"))
	if err != nil || string(data) != goodMilestone {
		t.Fatalf("original overwritten: %q %v", data, err)
	}
	entries, err := os.ReadDir(filepath.Join(b.HandoffRoot(), id))
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary residue: %v %v", entries, err)
	}
}

func TestHandoffSummaryPublicationReportsStorageFailure(t *testing.T) {
	b, ctx := newTestBroker(t)
	if err := os.WriteFile(b.HandoffRoot(), []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	yes := true
	summary := goodMilestone
	req := HandoffRequest{ID: NewUUID(), ProjectDir: t.TempDir(), FromSession: rootConversation,
		Plain: &yes, Milestone: &yes, Summary: &summary}
	if _, _, err := b.OpenHandoff(ctx, req); refusalCode(err) != "handoff_package_unwritable" {
		t.Fatalf("storage refusal: %v", err)
	}
	if hs, err := b.Handoffs(ctx); err != nil || len(hs) != 0 {
		t.Fatalf("storage failure opened handoff: %v %v", hs, err)
	}
}
