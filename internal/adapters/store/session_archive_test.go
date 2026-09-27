package store

import (
	"context"
	"testing"
	"time"
)

func archiveRow(id string, at int64) ArchiveRow {
	return ArchiveRow{ConversationID: id, Assistant: "claude", CWD: "/work/" + id, Place: "place-" + id,
		Title: "title " + id, Backend: "tmux", ArchivedAt: time.Unix(at, 0)}
}

// One row per conversation, newest first; archiving the same conversation
// again replaces its row rather than adding a second one.
func TestArchivedSessionsAreOneRowPerConversationNewestFirst(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	for _, row := range []ArchiveRow{archiveRow("a", 100), archiveRow("b", 200), archiveRow("c", 150)} {
		if _, err := st.ArchiveSession(ctx, row, 10); err != nil {
			t.Fatal(err)
		}
	}
	again := archiveRow("a", 300)
	again.Title, again.Persona = "renamed", "code-reviewer"
	if _, err := st.ArchiveSession(ctx, again, 10); err != nil {
		t.Fatal(err)
	}
	rows, err := st.ArchivedSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, r := range rows {
		order = append(order, r.ConversationID)
	}
	if len(rows) != 3 || order[0] != "a" || order[1] != "b" || order[2] != "c" {
		t.Fatalf("order = %v", order)
	}
	if rows[0].Title != "renamed" || rows[0].Persona != "code-reviewer" || rows[0].ArchivedAt.Unix() != 300 ||
		rows[0].Backend != "tmux" || rows[0].Place != "place-a" || rows[0].CWD != "/work/a" {
		t.Fatalf("the replaced row = %+v", rows[0])
	}
	if err := st.RemoveArchived(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	if rows, _ := st.ArchivedSessions(ctx); len(rows) != 2 {
		t.Fatalf("after removing b: %+v", rows)
	}
}

// Past the bound the oldest archived rows are dropped, and the count says how
// many.
func TestArchivedSessionsPastTheBoundDropTheOldest(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	var dropped int64
	for i, id := range []string{"a", "b", "c", "d"} {
		n, err := st.ArchiveSession(ctx, archiveRow(id, int64(100+i)), 3)
		if err != nil {
			t.Fatal(err)
		}
		dropped += n
	}
	rows, _ := st.ArchivedSessions(ctx)
	if dropped != 1 || len(rows) != 3 || rows[2].ConversationID != "b" {
		t.Fatalf("dropped %d, rows %+v", dropped, rows)
	}
}
