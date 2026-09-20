package transcript

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/session"
)

func writeTranscript(t *testing.T, path string, stamps ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := ""
	for _, s := range stamps {
		body += `{"type":"assistant","timestamp":"` + s + `","message":{"role":"assistant"}}` + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func claudeHost(t *testing.T) (*Host, string) {
	t.Helper()
	home := t.TempDir()
	return &Host{Home: home, titles: NewTitles(), shells: NewShells(), movements: NewMovements()}, home
}

func claudeSession(cwd, id string) session.Session {
	return session.Session{ID: "t1", Assistant: session.AssistantClaude, CWD: cwd, ConversationID: id}
}

// The reason this reads the record and not the file: something on this machine
// touches transcripts without appending a turn, and three sessions nobody had
// spoken to since the previous day all carried the same afternoon mtime.
func TestTheFilesClockIsNotTheAnswer(t *testing.T) {
	h, home := claudeHost(t)
	cwd := "/code/example"
	id := "c6000001-0000-4000-8000-000000000001"
	path := ClaudePath(home, cwd, id)
	writeTranscript(t, path, "2026-09-19T06:53:38.306Z")

	// Touched now, with nothing appended.
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}

	got := h.LastActivity(context.Background(), claudeSession(cwd, id))
	if !got.Known() {
		t.Fatalf("a readable record answered %+v", got)
	}
	want := time.Date(2026, 9, 19, 6, 53, 38, 0, time.UTC)
	if got.At.Unix() != want.Unix() {
		t.Fatalf("the answer was %v, want the newest turn's own time %v", got.At.UTC(), want)
	}
	if got.Evidence != session.EvidenceTranscript {
		t.Fatalf("evidence was %q", got.Evidence)
	}
}

// A turn that is appended does move the answer, and the newest one wins.
func TestANewTurnMovesTheAnswer(t *testing.T) {
	h, home := claudeHost(t)
	cwd := "/code/example"
	id := "c6000002-0000-4000-8000-000000000002"
	path := ClaudePath(home, cwd, id)
	writeTranscript(t, path, "2026-09-19T06:53:38.306Z")
	s := claudeSession(cwd, id)

	first := h.LastActivity(context.Background(), s)
	writeTranscript(t, path, "2026-09-19T06:53:38.306Z", "2026-09-20T11:00:00.100Z")
	second := h.LastActivity(context.Background(), s)

	if !second.At.After(first.At) {
		t.Fatalf("an appended turn did not move the answer: %v then %v", first.At, second.At)
	}
	if second.At.UTC().Format(time.RFC3339) != "2026-09-20T11:00:00Z" {
		t.Fatalf("the newest turn did not win: %v", second.At.UTC())
	}
}

// The record is opened only when it has changed: an unchanged file is one
// stat, which is what keeps a list redrawn every two seconds off the disk.
func TestAnUnchangedRecordIsNotOpened(t *testing.T) {
	h, home := claudeHost(t)
	cwd := "/code/example"
	id := "c6000003-0000-4000-8000-000000000003"
	path := ClaudePath(home, cwd, id)
	writeTranscript(t, path, "2026-09-19T06:53:38.306Z")
	s := claudeSession(cwd, id)

	first := h.LastActivity(context.Background(), s)
	// The file is replaced with one that has no readable turn in it at all,
	// at the same size and modification time. A reading that opened it would
	// change its answer; one that trusted the cache does not.
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	filler := make([]byte, st.Size())
	for i := range filler {
		filler[i] = 'x'
	}
	if err := os.WriteFile(path, filler, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}

	again := h.LastActivity(context.Background(), s)
	if !again.Known() || !again.At.Equal(first.At) {
		t.Fatalf("an unchanged record was opened again: %+v then %+v", first, again)
	}
}

// Every kind of nothing is named, and none of them is a time.
func TestEveryKindOfNothingIsNamed(t *testing.T) {
	h, home := claudeHost(t)
	cwd := "/code/example"

	cases := []struct {
		name string
		in   session.Session
		want session.ActivityReason
		set  func()
	}{
		{
			name: "no conversation yet",
			in:   claudeSession(cwd, ""),
			want: session.ActivityNoRecord,
		},
		{
			name: "the record is not there",
			in:   claudeSession(cwd, "c6000006-0000-4000-8000-000000000006"),
			want: session.ActivityNoRecord,
		},
		{
			name: "the record carries no turn this can read",
			in:   claudeSession(cwd, "c6000004-0000-4000-8000-000000000004"),
			want: session.ActivityUnreadable,
			set: func() {
				p := ClaudePath(home, cwd, "c6000004-0000-4000-8000-000000000004")
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("not a transcript\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "an assistant this cannot answer for",
			in:   session.Session{ID: "t", Assistant: session.Assistant("something-else")},
			want: session.ActivityUnsupported,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.set != nil {
				c.set()
			}
			got := h.LastActivity(context.Background(), c.in)
			if got.Known() {
				t.Fatalf("answered with a time: %+v", got)
			}
			if !got.At.IsZero() {
				t.Fatalf("carried a time anyway: %v", got.At)
			}
			if got.Unknown() != c.want {
				t.Fatalf("said %q, want %q", got.Unknown(), c.want)
			}
			if got.Detail == "" {
				t.Fatalf("said %q with no sentence a reader can act on", got.Unknown())
			}
		})
	}
}

// A half-written last line is stepped over rather than read as the answer.
func TestATruncatedLastRecordIsSteppedOver(t *testing.T) {
	h, home := claudeHost(t)
	cwd := "/code/example"
	id := "c6000005-0000-4000-8000-000000000005"
	path := ClaudePath(home, cwd, id)
	writeTranscript(t, path, "2026-09-19T06:53:38.306Z")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	// An append caught mid-flight: the key is there and its value is not.
	if _, err := f.WriteString(`{"type":"assistant","timestamp":"2026-09-2`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	got := h.LastActivity(context.Background(), claudeSession(cwd, id))
	if !got.Known() {
		t.Fatalf("a truncated tail lost the whole answer: %+v", got)
	}
	if got.At.UTC().Format(time.RFC3339) != "2026-09-19T06:53:38Z" {
		t.Fatalf("read the truncated value: %v", got.At.UTC())
	}
}
