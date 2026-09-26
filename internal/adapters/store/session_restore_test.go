package store

import (
	"context"
	"testing"
	"time"
)

func restoreRow(id, title string) RestoreRow {
	return RestoreRow{ConversationID: id, Assistant: "claude", CWD: "/work/" + id, Place: "p-" + id,
		Title: title, Backend: "tmux"}
}

func TestRestoreRowsRoundTripAndReplaceTheBootsSet(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	t0 := time.Unix(1_000_000, 0)

	if err := s.RecordBoot(ctx, "boot-a", []RestoreRow{restoreRow("c1", "one"), restoreRow("c2", "two")}, t0, 2); err != nil {
		t.Fatal(err)
	}
	// The same conversation seen later keeps when it was first seen; one that
	// is gone from the set is gone from the store.
	if err := s.RecordBoot(ctx, "boot-a", []RestoreRow{restoreRow("c1", "renamed")}, t0.Add(time.Minute), 2); err != nil {
		t.Fatal(err)
	}
	rows, err := s.RestoreRows(ctx, "boot-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ConversationID != "c1" || rows[0].Title != "renamed" ||
		!rows[0].FirstSeen.Equal(t0) || !rows[0].LastSeen.Equal(t0.Add(time.Minute)) ||
		rows[0].CWD != "/work/c1" || rows[0].Place != "p-c1" || rows[0].Backend != "tmux" || rows[0].Assistant != "claude" {
		t.Fatalf("rows = %+v", rows)
	}

	// An empty set empties the boot, and the boot is still known.
	if err := s.RecordBoot(ctx, "boot-a", nil, t0.Add(2*time.Minute), 2); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s.RestoreRows(ctx, "boot-a"); len(rows) != 0 {
		t.Fatalf("an empty reading left rows: %+v", rows)
	}
	if prev, ok, err := s.PreviousBoot(ctx, "boot-b"); err != nil || !ok || prev.ID != "boot-a" ||
		!prev.LastSeen.Equal(t0.Add(2*time.Minute)) {
		t.Fatalf("previous = %+v %v %v", prev, ok, err)
	}
}

func TestRecordBootKeepsOnlyTheCurrentAndThePreviousBoot(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	t0 := time.Unix(2_000_000, 0)
	for i, boot := range []string{"boot-1", "boot-2", "boot-3"} {
		if err := s.RecordBoot(ctx, boot, []RestoreRow{restoreRow(boot+"-c", "")}, t0.Add(time.Duration(i)*time.Hour), 2); err != nil {
			t.Fatal(err)
		}
	}
	if rows, _ := s.RestoreRows(ctx, "boot-1"); len(rows) != 0 {
		t.Fatalf("the oldest boot's rows were kept: %+v", rows)
	}
	if prev, ok, _ := s.PreviousBoot(ctx, "boot-3"); !ok || prev.ID != "boot-2" {
		t.Fatalf("previous of boot-3 = %+v %v", prev, ok)
	}
	if rows, _ := s.RestoreRows(ctx, "boot-2"); len(rows) != 1 {
		t.Fatalf("the previous boot's rows were not kept: %+v", rows)
	}
	var boots int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM restore_boots`).Scan(&boots); err != nil || boots != 2 {
		t.Fatalf("boots = %d, %v", boots, err)
	}
}

func TestResolveRestoreAnswersOnceAndAllMeansTheUnresolved(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	t0 := time.Unix(3_000_000, 0)
	if err := s.RecordBoot(ctx, "b", []RestoreRow{restoreRow("c1", ""), restoreRow("c2", ""), restoreRow("c3", "")}, t0, 2); err != nil {
		t.Fatal(err)
	}
	if n, err := s.ResolveRestore(ctx, "b", []string{"c1"}, RestoreRestored, t0); err != nil || n != 1 {
		t.Fatalf("restore c1 = %d, %v", n, err)
	}
	// All: the two still open, and not c1's first answer.
	if n, err := s.ResolveRestore(ctx, "b", nil, RestoreDismissed, t0.Add(time.Second)); err != nil || n != 2 {
		t.Fatalf("dismiss all = %d, %v", n, err)
	}
	rows, _ := s.RestoreRows(ctx, "b")
	got := map[string]string{}
	for _, r := range rows {
		got[r.ConversationID] = r.Resolution
	}
	if got["c1"] != RestoreRestored || got["c2"] != RestoreDismissed || got["c3"] != RestoreDismissed {
		t.Fatalf("resolutions = %v", got)
	}
	// A later reading of the same boot does not reopen an answered row.
	if err := s.RecordBoot(ctx, "b", []RestoreRow{restoreRow("c1", "x")}, t0.Add(time.Minute), 2); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s.RestoreRows(ctx, "b"); len(rows) != 1 || rows[0].Resolution != RestoreRestored {
		t.Fatalf("rows = %+v", rows)
	}
	if _, err := s.ResolveRestore(ctx, "b", nil, "maybe", t0); err == nil {
		t.Fatal("an unknown resolution was written")
	}
}
