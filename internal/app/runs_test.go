package app

import (
	"context"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// A run is issued once per message, found by its id and by the session it
// went to, outlives the process that issued it, and a relay on one that was
// never issued, or is too old, is refused by name.
func TestARunIsIssuedFoundAndChecked(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	at := time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC)
	runs := &Runs{Store: st, Now: func() time.Time { return at }}
	sess := session.Session{ID: "%4", ConversationID: "root-conv", Assistant: session.AssistantClaude}
	first, err := runs.Issue(ctx, sess, "local", "")
	if err != nil {
		t.Fatal(err)
	}
	at = at.Add(time.Minute)
	second, err := runs.Issue(ctx, sess, "device:phone", "")
	if err != nil || second.ID == first.ID || !work.RunShaped(second.ID) {
		t.Fatalf("second run %+v %v", second, err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	runs.Store = st

	got, ok, err := runs.Find(ctx, first.ID)
	if err != nil || !ok || got.Session != "root-conv" || got.Terminal != "%4" || got.Principal != "local" ||
		!got.At.Equal(first.At) {
		t.Fatalf("find after a restart: %+v %v %v", got, ok, err)
	}
	for _, name := range []string{"root-conv", "%4"} {
		if latest, ok, err := runs.Latest(ctx, name); err != nil || !ok || latest.ID != second.ID {
			t.Fatalf("latest by %s: %+v %v %v", name, latest, ok, err)
		}
	}
	if _, ok, _ := runs.Latest(ctx, "nobody"); ok {
		t.Fatal("a session nobody wrote to has a run")
	}
	if _, err := runs.Relay(ctx, first.ID); err != nil {
		t.Fatalf("control, a fresh run: %v", err)
	}
	for _, id := range []string{"run-7", newWorkID()} {
		if _, err := runs.Relay(ctx, id); codeOf(err) != "run_unknown" {
			t.Fatalf("a run nobody issued (%s): %v", id, err)
		}
	}
	at = first.At.Add(work.RelayWindow + time.Second)
	if _, err := runs.Relay(ctx, first.ID); codeOf(err) != "run_expired" {
		t.Fatalf("a day-old run: %v", err)
	}
}

// A run keeps the start of the message it was issued for across a restart,
// and a run recorded before runs kept one reads with none.
func TestARunsExcerptSurvivesARestartAndOlderRunsHaveNone(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	runs := &Runs{Store: st}
	run, err := runs.Issue(ctx, session.Session{ID: "%4", ConversationID: "root-conv"}, "local", "Create a Board item\nwith steps")
	if err != nil {
		t.Fatal(err)
	}
	const older = "0f0f0f0f-0000-4000-8000-000000000001"
	legacy := []byte(`{"run":"` + older + `","session_id":"root-conv","terminal_id":"%4","principal":"local","at":1790000000}`)
	if err := st.Append(ctx, store.Event{Kind: EventRunIssued, Subject: older, Payload: legacy}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if st, err = store.Open(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	runs.Store = st
	got, ok, err := runs.Find(ctx, run.ID)
	if err != nil || !ok || got.Excerpt != "Create a Board item with steps" {
		t.Fatalf("excerpt after a restart: %+v %v %v", got, ok, err)
	}
	old, ok, err := runs.Find(ctx, older)
	if err != nil || !ok || old.Excerpt != "" || old.Session != "root-conv" {
		t.Fatalf("an older run: %+v %v %v", old, ok, err)
	}
}
