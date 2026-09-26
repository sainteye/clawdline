package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func restoreRow(id, title string) RestoreRow {
	return RestoreRow{ConversationID: id, Assistant: "claude", CWD: "/work/" + id, Place: "p-" + id,
		Title: title, Backend: "tmux"}
}

// recordAt records one complete reading of boot taken at at.
func recordAt(s *Store, boot string, rows []RestoreRow, at time.Time) error {
	_, err := s.RecordBoot(context.Background(), BootReading{Boot: boot, Rows: rows, At: at, KeepBoots: 2, KeepRows: 200})
	return err
}

func byConversation(t *testing.T, s *Store, boot string) map[string]RestoreRow {
	t.Helper()
	rows, err := s.RestoreRows(context.Background(), boot)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]RestoreRow{}
	for _, r := range rows {
		out[r.ConversationID] = r
	}
	return out
}

func TestRestoreRowsRoundTripAndKeepWhatWentAway(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	t0 := time.Unix(1_000_000, 0)

	if err := recordAt(s, "boot-a", []RestoreRow{restoreRow("c1", "one"), restoreRow("c2", "two")}, t0); err != nil {
		t.Fatal(err)
	}
	// The same conversation seen later keeps when it was first seen; one that
	// is no longer in the set keeps its row and says when it went.
	if err := recordAt(s, "boot-a", []RestoreRow{restoreRow("c1", "renamed")}, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got := byConversation(t, s, "boot-a")
	c1, c2 := got["c1"], got["c2"]
	if len(got) != 2 || c1.Title != "renamed" ||
		!c1.FirstSeen.Equal(t0) || !c1.LastSeen.Equal(t0.Add(time.Minute)) || !c1.GoneAt.IsZero() ||
		c1.CWD != "/work/c1" || c1.Place != "p-c1" || c1.Backend != "tmux" || c1.Assistant != "claude" {
		t.Fatalf("rows = %+v", got)
	}
	if c2.Title != "two" || !c2.LastSeen.Equal(t0) || !c2.GoneAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("the conversation that went away = %+v", c2)
	}

	// A complete empty reading — what a shutdown's last scan looks like —
	// keeps every row, each gone at the first reading that did not see it,
	// and the boot is still known.
	if err := recordAt(s, "boot-a", nil, t0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	got = byConversation(t, s, "boot-a")
	if len(got) != 2 || !got["c1"].GoneAt.Equal(t0.Add(2*time.Minute)) || !got["c2"].GoneAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("an empty reading left = %+v", got)
	}
	if prev, ok, err := s.PreviousBoot(ctx, "boot-b"); err != nil || !ok || prev.ID != "boot-a" ||
		!prev.LastSeen.Equal(t0.Add(2*time.Minute)) {
		t.Fatalf("previous = %+v %v %v", prev, ok, err)
	}

	// Back in the same boot, the row is open again.
	if err := recordAt(s, "boot-a", []RestoreRow{restoreRow("c2", "two")}, t0.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	got = byConversation(t, s, "boot-a")
	if !got["c2"].GoneAt.IsZero() || !got["c2"].FirstSeen.Equal(t0) || !got["c1"].GoneAt.Equal(t0.Add(2*time.Minute)) {
		t.Fatalf("after c2 came back = %+v", got)
	}
}

func TestRecordBootDropsTheOldestGoneRowsFirstPastTheLimit(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	t0 := time.Unix(1_500_000, 0)
	record := func(at time.Time, rows ...RestoreRow) int64 {
		t.Helper()
		n, err := s.RecordBoot(ctx, BootReading{Boot: "b", Rows: rows, At: at, KeepBoots: 2, KeepRows: 3})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	record(t0, restoreRow("old", ""), restoreRow("mid", ""), restoreRow("live", ""))
	record(t0.Add(time.Minute), restoreRow("mid", ""), restoreRow("live", "")) // old goes
	record(t0.Add(2*time.Minute), restoreRow("live", ""))                      // mid goes
	if n := record(t0.Add(3*time.Minute), restoreRow("live", ""), restoreRow("new", "")); n != 1 {
		t.Fatalf("evicted = %d, want 1", n)
	}
	got := byConversation(t, s, "b")
	if _, ok := got["old"]; ok || len(got) != 3 || got["mid"].GoneAt.IsZero() {
		t.Fatalf("rows = %+v", got)
	}
}

func TestTouchBootMovesOnlyTheBootsLastSeen(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	t0 := time.Unix(1_600_000, 0)
	if err := recordAt(s, "boot-a", []RestoreRow{restoreRow("c1", "")}, t0); err != nil {
		t.Fatal(err)
	}
	if err := s.TouchBoot(ctx, "boot-a", t0.Add(90*time.Second)); err != nil {
		t.Fatal(err)
	}
	// An earlier clock never moves it back.
	if err := s.TouchBoot(ctx, "boot-a", t0.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	prev, ok, err := s.PreviousBoot(ctx, "boot-b")
	if err != nil || !ok || !prev.LastSeen.Equal(t0.Add(90*time.Second)) {
		t.Fatalf("previous = %+v %v %v", prev, ok, err)
	}
	if row := byConversation(t, s, "boot-a")["c1"]; !row.LastSeen.Equal(t0) {
		t.Fatalf("the heartbeat rewrote the row: %+v", row)
	}
}

func TestACloseThroughClawdlineOutlivesAScanThatBeganBeforeIt(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	t0 := time.Unix(1_700_000, 0)
	if err := recordAt(s, "b", []RestoreRow{restoreRow("c1", "")}, t0); err != nil {
		t.Fatal(err)
	}
	closedAt := t0.Add(10 * time.Second)
	if err := s.CloseRestore(ctx, "b", "c1", closedAt); err != nil {
		t.Fatal(err)
	}
	row := byConversation(t, s, "b")["c1"]
	if !row.ClosedAt.Equal(closedAt) || !row.GoneAt.Equal(closedAt) {
		t.Fatalf("closed row = %+v", row)
	}
	// A scan that began before the close still shows it; that is not the
	// conversation coming back.
	if _, err := s.RecordBoot(ctx, BootReading{Boot: "b", Rows: []RestoreRow{restoreRow("c1", "")},
		At: closedAt.Add(time.Second), ScannedAt: closedAt.Add(-2 * time.Second), KeepBoots: 2, KeepRows: 200}); err != nil {
		t.Fatal(err)
	}
	if row := byConversation(t, s, "b")["c1"]; !row.ClosedAt.Equal(closedAt) || !row.GoneAt.Equal(closedAt) {
		t.Fatalf("an older scan reopened the closed row: %+v", row)
	}
	// A scan that began after it does: the person resumed it.
	if err := recordAt(s, "b", []RestoreRow{restoreRow("c1", "")}, closedAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if row := byConversation(t, s, "b")["c1"]; !row.ClosedAt.IsZero() || !row.GoneAt.IsZero() {
		t.Fatalf("a resumed conversation is still closed: %+v", row)
	}
}

// The tables as the commit before gone_at shipped them, with a row in each.
const restoreSchemaBeforeGone = `
CREATE TABLE restore_boots (
  boot_id    TEXT    PRIMARY KEY,
  first_seen INTEGER NOT NULL,
  last_seen  INTEGER NOT NULL
);
CREATE TABLE restore_sessions (
  boot_id         TEXT    NOT NULL,
  conversation_id TEXT    NOT NULL,
  assistant       TEXT    NOT NULL CHECK (assistant IN ('claude', 'codex')),
  cwd             TEXT    NOT NULL,
  place           TEXT    NOT NULL,
  title           TEXT    NOT NULL DEFAULT '',
  backend         TEXT    NOT NULL DEFAULT '',
  first_seen      INTEGER NOT NULL,
  last_seen       INTEGER NOT NULL,
  resolution      TEXT    CHECK (resolution IS NULL OR resolution IN ('restored', 'dismissed')),
  resolved_at     INTEGER,
  PRIMARY KEY (boot_id, conversation_id)
);
INSERT INTO restore_boots VALUES ('boot-old', 100, 200);
INSERT INTO restore_sessions (boot_id, conversation_id, assistant, cwd, place, title, backend, first_seen, last_seen)
  VALUES ('boot-old', 'c-old', 'claude', '/work/old', 'p-old', 'kept', 'tmux', 100, 200);
`

func TestTheRestoreTablesGainGoneAndClosedWithoutLosingRows(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, DBFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(restoreSchemaBeforeGone); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	row, ok := byConversation(t, s, "boot-old")["c-old"]
	if !ok || row.Title != "kept" || !row.LastSeen.Equal(time.Unix(200, 0)) || !row.GoneAt.IsZero() || !row.ClosedAt.IsZero() {
		t.Fatalf("migrated row = %+v, %v", row, ok)
	}
	// And the new columns work on the migrated table.
	if err := recordAt(s, "boot-old", nil, time.Unix(300, 0)); err != nil {
		t.Fatal(err)
	}
	if row := byConversation(t, s, "boot-old")["c-old"]; !row.GoneAt.Equal(time.Unix(300, 0)) {
		t.Fatalf("after a reading on the migrated table = %+v", row)
	}
	// Opening again is a no-op.
	s.Close()
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	again.Close()
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
		if err := recordAt(s, boot, []RestoreRow{restoreRow(boot+"-c", "")}, t0.Add(time.Duration(i)*time.Hour)); err != nil {
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
	if err := recordAt(s, "b", []RestoreRow{restoreRow("c1", ""), restoreRow("c2", ""), restoreRow("c3", "")}, t0); err != nil {
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
	if err := recordAt(s, "b", []RestoreRow{restoreRow("c1", "x")}, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := byConversation(t, s, "b"); got["c1"].Resolution != RestoreRestored || got["c2"].Resolution != RestoreDismissed {
		t.Fatalf("rows = %+v", got)
	}
	if _, err := s.ResolveRestore(ctx, "b", nil, "maybe", t0); err == nil {
		t.Fatal("an unknown resolution was written")
	}
}
