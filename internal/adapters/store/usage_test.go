package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestAUsageRowRoundTripsAndIsFoundByWhatItNames(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	read := time.Unix(1_790_000_000, 123)
	row := UsageRow{Assistant: "claude", Conversation: "c1", Path: "/example/c1.jsonl", TaskID: "task-1",
		OpeningRead: true, State: json.RawMessage(`{"offset":10}`), Spent: json.RawMessage(`{"impl":{"input":1}}`),
		Measured: json.RawMessage(`{"input":1}`), Size: 10, ModifiedAt: read, More: true, ReadAt: read}
	sub := UsageRow{Assistant: "claude", Conversation: "agent-a", Path: "/example/c1/subagents/agent-a.jsonl",
		Parent: "c1", Reason: UsageTranscriptUnreadable}
	opened := UsageRow{Assistant: "codex", Conversation: "c2", Path: "/example/c2.jsonl", RootAssignment: "ra-1"}
	for _, r := range []UsageRow{row, sub, opened} {
		if err := s.SaveUsageRow(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	got, ok, err := s.UsageRow(ctx, "claude", "c1")
	if err != nil || !ok {
		t.Fatalf("row: %v %v", ok, err)
	}
	if got.Path != row.Path || got.TaskID != "task-1" || !got.OpeningRead || !got.More || got.Size != 10 ||
		!got.ReadAt.Equal(read) || !got.ModifiedAt.Equal(read) || string(got.State) != `{"offset":10}` {
		t.Fatalf("round trip: %+v", got)
	}
	// A second save replaces the row rather than adding one.
	row.Reason = UsageTranscriptMissing
	if err := s.SaveUsageRow(ctx, row); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s.UsageRowsForConversations(ctx, []string{"c1"}); len(rows) != 1 || rows[0].Reason != UsageTranscriptMissing {
		t.Fatalf("replaced row: %+v", rows)
	}
	if rows, _ := s.UsageRowsWithParents(ctx, []string{"c1"}); len(rows) != 1 || rows[0].Conversation != "agent-a" {
		t.Fatalf("subagents: %+v", rows)
	}
	if rows, _ := s.UsageRowsForTask(ctx, "task-1"); len(rows) != 1 || rows[0].Conversation != "c1" {
		t.Fatalf("task: %+v", rows)
	}
	if rows, _ := s.UsageRowsForRootAssignments(ctx, []string{"ra-1", ""}); len(rows) != 1 || rows[0].Conversation != "c2" {
		t.Fatalf("root assignment: %+v", rows)
	}
	// Read since: the fed row that is not already known missing.
	row.Reason = ""
	if err := s.SaveUsageRow(ctx, row); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s.UsageRowsReadSince(ctx, read.Add(-time.Second)); len(rows) != 1 || rows[0].Conversation != "c1" {
		t.Fatalf("read since: %+v", rows)
	}
	if err := s.SaveUsageRow(ctx, UsageRow{Conversation: "x"}); err == nil {
		t.Fatal("a row with no assistant was saved")
	}
}

func TestBrokerTasksDispatchedByAnswersTheRootsTasksInTheWindow(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	for _, task := range []struct {
		id, root string
		at       int64
	}{{"early", "root-1", 100}, {"inside", "root-1", 200}, {"late", "root-1", 300}, {"other", "root-2", 200}} {
		row := BrokerRow{ID: task.id, Project: "/p", Assistant: "claude", State: "running",
			CreatedAt: time.Unix(task.at, 0), UpdatedAt: time.Unix(task.at, 0), SecretHash: "h",
			Record: json.RawMessage(`{"id":"` + task.id + `","root":{"session_id":"` + task.root + `"}}`)}
		if err := s.SaveBrokerTask(ctx, row, nil); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.BrokerTasksDispatchedBy(ctx, "root-1", time.Unix(150, 0), time.Unix(250, 0))
	if err != nil || len(got) != 1 || got[0].ID != "inside" {
		t.Fatalf("window: %+v %v", got, err)
	}
	got, err = s.BrokerTasksDispatchedBy(ctx, "root-1", time.Unix(150, 0), time.Time{})
	if err != nil || len(got) != 2 || got[0].ID != "inside" || got[1].ID != "late" {
		t.Fatalf("open window: %+v %v", got, err)
	}
}
