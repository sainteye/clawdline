package app

import (
	"context"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

func codexArchiveDetail(t *testing.T, f *w4Fixture, id string) any {
	t.Helper()
	record, ok, err := f.book.Detail(context.Background(), id)
	if err != nil || !ok {
		t.Fatalf("detail: %v %v", ok, err)
	}
	return record["archive_codex_conversation"]
}

// A new Codex schedule archives its runs' conversations unless it is told
// not to; any other assistant's does not; a save that does not name the
// field keeps what the file says, and one that names it changes it.
func TestANewCodexScheduleArchivesItsConversationsByDefault(t *testing.T) {
	f := newW4(t)
	ctx := context.Background()
	f.book.Places = func(context.Context) []SchedulePlace {
		return []SchedulePlace{{ID: "p1", Label: "p", Path: f.dir}}
	}
	body := func(assistant string) map[string]any {
		return map[string]any{"title": "Hourly queue", "at": "09:00", "days": "daily", "place_id": "p1",
			"assistant": assistant, "instructions": "do it", "enabled": true}
	}
	create := func(b map[string]any) string {
		made := f.book.Create(ctx, b, ScheduleAuthority{})
		if made.Code != "" {
			t.Fatalf("create refused: %s %s", made.Code, made.Message)
		}
		return made.Body["schedule"].(map[string]any)["id"].(string)
	}
	codex := create(body("codex"))
	if got := codexArchiveDetail(t, f, codex); got != true {
		t.Fatalf("a new Codex schedule: archive_codex_conversation = %v", got)
	}
	if got := codexArchiveDetail(t, f, create(body("claude"))); got != false {
		t.Fatalf("a new Claude schedule: archive_codex_conversation = %v", got)
	}
	off := body("codex")
	off["archive_codex_conversation"] = false
	if got := codexArchiveDetail(t, f, create(off)); got != false {
		t.Fatalf("a Codex schedule made with it off: archive_codex_conversation = %v", got)
	}

	if saved := f.book.Update(ctx, codex, body("codex"), ScheduleAuthority{}); saved.Code != "" {
		t.Fatalf("save refused: %s %s", saved.Code, saved.Message)
	}
	if got := codexArchiveDetail(t, f, codex); got != true {
		t.Fatalf("a save that did not name it turned it off: %v", got)
	}
	f.set(f.at().Add(time.Minute))
	if saved := f.book.Update(ctx, codex, off, ScheduleAuthority{}); saved.Code != "" {
		t.Fatalf("save refused: %s %s", saved.Code, saved.Message)
	}
	if got := codexArchiveDetail(t, f, codex); got != false {
		t.Fatalf("a save that turned it off left %v", got)
	}

	bad := body("codex")
	bad["archive_codex_conversation"] = "yes"
	if reply := f.book.Create(ctx, bad, ScheduleAuthority{}); reply.Code != "bad_request" {
		t.Fatalf("a non-boolean answered %q %q", reply.Code, reply.Message)
	}
}

// A run whose conversation was archived is still one that can be resumed:
// the history gives its session_id and says it is archived, and the resume
// route unarchives it first.
func TestAnArchivedRunIsStillResumable(t *testing.T) {
	archivedAt := time.Date(2026, 11, 11, 3, 0, 0, 0, time.UTC)
	run := store.ScheduleRun{TaskID: "t1", State: "success", Assistant: "codex", ProjectDir: "/p",
		ConversationID: "c6900000-0000-4000-8000-000000000001", ConversationArchivedAt: archivedAt}
	b := &ScheduleBook{Conversations: RunConversations{
		Recorded: func(string, string, string) bool { return false },
	}}
	if got := b.runConversations([]store.ScheduleRun{run}); got["t1"] != "" {
		t.Fatalf("a conversation on disk nowhere was offered: %v", got)
	}
	b.Conversations.Archived = func(assistant, id string) bool { return assistant == "codex" && id == run.ConversationID }
	if got := b.runConversations([]store.ScheduleRun{run}); got["t1"] != run.ConversationID {
		t.Fatalf("an archived conversation was not offered: %v", got)
	}
}
