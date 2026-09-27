package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/task"
)

// archiveStore is a real store that notes what the terminal had been asked
// by the time each archive row was written.
type archiveStore struct {
	*store.Store
	host         *closeHost
	callsAtWrite []int
}

func (a *archiveStore) ArchiveSession(ctx context.Context, row store.ArchiveRow, keep int) (int64, error) {
	a.callsAtWrite = append(a.callsAtWrite, len(a.host.calls))
	return a.Store.ArchiveSession(ctx, row, keep)
}

func archiveUnder(t *testing.T, h *closeHost, clock *time.Time) (Actions, *archiveStore) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	as := &archiveStore{Store: st, host: h}
	a := closeActions(h)
	a.Archives = &SessionArchive{Store: as, Now: func() time.Time { return *clock },
		Title: func(context.Context, session.Session) string { return "the row's label" }}
	return a, as
}

func archivedRows(t *testing.T, st ArchiveStore) []store.ArchiveRow {
	t.Helper()
	rows, err := st.ArchivedSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// Archive is the close first, and the record only after it: the row is
// written once the terminal has been taken away, carrying the row's label.
func TestArchiveClosesTheSessionThenRecordsIt(t *testing.T) {
	clock := time.Unix(90_000, 0)
	h := &closeHost{conversation: "conv-1"}
	a, st := archiveUnder(t, h, &clock)
	closed, row, err := a.Archive(context.Background(), "%1", false)
	if err != nil {
		t.Fatal(err)
	}
	if closed.ID != "%1" || len(h.calls) != 1 || len(st.callsAtWrite) != 1 || st.callsAtWrite[0] != 1 {
		t.Fatalf("closed %+v, terminal calls %v, calls when the row was written %v", closed, h.calls, st.callsAtWrite)
	}
	rows := archivedRows(t, st)
	if len(rows) != 1 || rows[0] != row {
		t.Fatalf("rows %+v, answered %+v", rows, row)
	}
	want := store.ArchiveRow{ConversationID: "conv-1", Assistant: "claude", CWD: "/work",
		Place: projects.PlaceID("/work"), Title: "the row's label", Backend: "tmux", ArchivedAt: clock}
	if row != want {
		t.Fatalf("row %+v, want %+v", row, want)
	}
}

// Every refusal a close gives, archive gives verbatim, and none of them
// writes a row.
func TestARefusedCloseArchivesNothing(t *testing.T) {
	clock := time.Unix(90_000, 0)
	owedByIt := []task.Obligation{{ID: "o1", Kind: task.KindLanding, Subject: "%1"}}
	for _, c := range []struct {
		name  string
		owed  func(context.Context) ([]task.Obligation, error)
		fail  error
		force bool
		code  string
	}{
		{"blocked", func(context.Context) ([]task.Obligation, error) { return owedByIt, nil }, nil, false, "close_blocked"},
		{"unknown", func(context.Context) ([]task.Obligation, error) { return nil, errors.New("no broker") }, nil, false, "closeability_unknown"},
		{"unknown, forced", func(context.Context) ([]task.Obligation, error) { return nil, errors.New("no broker") }, nil, true, "closeability_unknown"},
		{"terminal refused", nil, terminal.StillRunning{Why: "it did not leave"}, false, "close_assistant_running"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := &closeHost{conversation: "conv-1", err: c.fail}
			a, st := archiveUnder(t, h, &clock)
			if c.owed != nil {
				a.Owed = c.owed
			}
			_, _, err := a.Archive(context.Background(), "%1", c.force)
			var ref Refusal
			if !errors.As(err, &ref) || ref.Code != c.code {
				t.Fatalf("archive answered %v, want %s", err, c.code)
			}
			if _, closeErr := a.Close(context.Background(), "%1", c.force); fmt.Sprint(closeErr) != fmt.Sprint(err) {
				t.Fatalf("archive said %v and close says %v", err, closeErr)
			}
			if rows := archivedRows(t, st); len(rows) != 0 {
				t.Fatalf("a refused close was archived: %+v", rows)
			}
		})
	}
	// force goes over close_blocked, as it does for a close.
	h := &closeHost{conversation: "conv-1"}
	a, st := archiveUnder(t, h, &clock)
	a.Owed = func(context.Context) ([]task.Obligation, error) { return owedByIt, nil }
	if _, _, err := a.Archive(context.Background(), "%1", true); err != nil {
		t.Fatalf("a forced archive over an obligation: %v", err)
	}
	if rows := archivedRows(t, st); len(rows) != 1 {
		t.Fatalf("rows %+v", rows)
	}
}

// A Session with no conversation id has nothing to resume, so it is refused
// before the terminal is touched.
func TestASessionWithNoConversationIsNotArchived(t *testing.T) {
	clock := time.Unix(90_000, 0)
	h := &closeHost{}
	a, st := archiveUnder(t, h, &clock)
	_, _, err := a.Archive(context.Background(), "%1", true)
	var ref Refusal
	if !errors.As(err, &ref) || ref.Code != ArchiveNoConversation {
		t.Fatalf("answered %v", err)
	}
	if len(h.calls) != 0 || len(archivedRows(t, st)) != 0 {
		t.Fatalf("calls %v, rows %+v", h.calls, archivedRows(t, st))
	}
}

// An archive close is a close through Clawdline, so the reboot record marks
// the conversation closed and the next boot does not offer it as well.
func TestAnArchivedConversationIsNotOfferedAfterAReboot(t *testing.T) {
	rs := restoreStore(t)
	clock := time.Unix(80_000, 0)
	h := &closeHost{conversation: "conv-1"}
	a := closeActions(h)
	a.Restore = restoreUnder(rs, "boot-before", &clock)
	a.Archives = &SessionArchive{Store: rs.Store, Now: func() time.Time { return clock }}
	ctx := context.Background()
	a.Restore.Observe(ctx, complete(session.Session{ID: "%1", Backend: session.BackendTmux,
		Assistant: session.AssistantClaude, CWD: "/work", ConversationID: "conv-1"}))
	clock = clock.Add(time.Second)
	if _, _, err := a.Archive(ctx, "%1", false); err != nil {
		t.Fatal(err)
	}
	after := restoreUnder(rs, "boot-after", &clock)
	offer, err := after.Restorable(ctx, session.Inventory{Complete: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(offer.Rows) != 0 {
		t.Fatalf("the archived conversation is offered after a reboot: %+v", offer.Rows)
	}
}

// A restore answers one result per name, opens nothing for a name with no
// row or a conversation already open, and removes only the rows it opened.
func TestRestoringArchivedRemovesOnlyTheRowsThatOpened(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	for i, id := range []string{"opens", "gone-dir", "open-now", "refused"} {
		row := store.ArchiveRow{ConversationID: id, Assistant: "claude", CWD: "/work/" + id, Place: "p-" + id,
			ArchivedAt: time.Unix(int64(100+i), 0)}
		if _, err := st.ArchiveSession(ctx, row, 10); err != nil {
			t.Fatal(err)
		}
	}
	arch := &SessionArchive{Store: st}
	live := session.Inventory{Complete: true, Sessions: []session.Session{{ID: "%9", ConversationID: "open-now"}}}
	var asked []string
	results, err := arch.RestoreArchived(ctx, []string{"opens", "gone-dir", "open-now", "refused", "never", "opens"}, live,
		func(_ context.Context, row store.ArchiveRow) (Started, error) {
			asked = append(asked, row.ConversationID)
			switch row.ConversationID {
			case "gone-dir":
				return Started{}, ErrRestorePlaceUnavailable
			case "refused":
				return Started{}, StartRefusal{Code: "not_found", Message: "No conversation named that"}
			}
			return Started{ID: "%70", Backend: "tmux"}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range results {
		got = append(got, fmt.Sprintf("%s:%v:%s:%s", r.ConversationID, r.OK, r.Code, r.Started.ID))
	}
	want := "opens:true::%70 gone-dir:false:place_unavailable: open-now:false:already_open: " +
		"refused:false:conversation_not_found: never:false:not_archived:"
	if strings.Join(got, " ") != want {
		t.Fatalf("results\n %s\nwant\n %s", strings.Join(got, " "), want)
	}
	if strings.Join(asked, ",") != "opens,gone-dir,refused" {
		t.Fatalf("resumed %v", asked)
	}
	var left []string
	for _, r := range archivedRows(t, st) {
		left = append(left, r.ConversationID)
	}
	if strings.Join(left, ",") != "refused,open-now,gone-dir" {
		t.Fatalf("rows left %v", left)
	}
	arch.BatchLimit = 2
	if _, err := arch.RestoreArchived(ctx, []string{"a", "b", "c"}, live, nil); !errors.Is(err, ErrArchiveBatch) {
		t.Fatalf("an oversized restore: %v", err)
	}
}

// The past list Resume admits on stops at the newest 400 transcripts of a
// directory (200 conversations). A conversation archived long ago in a busy
// directory has fallen off it: Resume refuses it, and ResumeRecorded opens it
// because its transcript is on disk.
func TestAnArchivedConversationOffThePastListStillResumes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	place := projects.Place{Path: t.TempDir()}
	dir := filepath.Join(home, ".claude", "projects", projects.Slug(place.Path))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","message":{"role":"user","content":"look at the build"}}` + "\n"
	old := "0f1e2d3c-0000-4000-8000-000000000000"
	start := time.Now().Add(-1000 * time.Hour)
	for i := 0; i <= 450; i++ {
		id := fmt.Sprintf("0f1e2d3c-0000-4000-8000-%012d", i)
		path := filepath.Join(dir, id+".jsonl")
		if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
		at := start.Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	term := &personaTerminal{}
	s := Starter{
		Terminal: func() projects.TerminalChoice { return projects.TerminalTmux },
		Launcher: term,
		Past: func(context.Context, projects.Place, string) []projects.Past {
			return projects.ClaudePast(place, nil, projects.PastTitles{}, 200, 400)
		},
		Recorded: projects.Recorded,
	}
	ctx := context.Background()
	var refusal StartRefusal
	if _, err := s.Resume(ctx, place, old, "claude", ""); !errors.As(err, &refusal) || refusal.Code != "not_found" {
		t.Fatalf("the ordinary resume of a conversation 451 transcripts back: %v", err)
	}
	if _, err := s.ResumeRecorded(ctx, place, old, "claude", ""); err != nil {
		t.Fatalf("the archived conversation did not resume: %v", err)
	}
	if !strings.Contains(term.command, "--resume "+old) {
		t.Fatalf("command %s", term.command)
	}
	// A conversation with no transcript is still refused.
	missing := "0f1e2d3c-0000-4000-8000-000000000999"
	if _, err := s.ResumeRecorded(ctx, place, missing, "claude", ""); !errors.As(err, &refusal) || refusal.Code != "not_found" {
		t.Fatalf("a conversation with no transcript: %v", err)
	}
}
