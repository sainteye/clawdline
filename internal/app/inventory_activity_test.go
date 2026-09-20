package app

import (
	"context"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/app/ports"
	"github.com/sainteye/clawdline-go/internal/domain/capacity"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// activitySource is an identity source that answers a prepared activity per
// session id, and counts how many times it was asked.
type activitySource struct {
	answers map[string]session.Activity
	asked   int
}

func (a *activitySource) ForSession(context.Context, session.Session) (ports.Identity, bool) {
	return ports.Identity{}, false
}

func (a *activitySource) LastActivity(_ context.Context, s session.Session) session.Activity {
	a.asked++
	return a.answers[s.ID]
}

// silentSource answers the identity port and nothing else: the optional
// activity port is not implemented.
type silentSource struct{}

func (silentSource) ForSession(context.Context, session.Session) (ports.Identity, bool) {
	return ports.Identity{}, false
}

func rows(n int) []session.Session {
	out := make([]session.Session, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, session.Session{
			ID:        string(rune('a' + i)),
			TTY:       "/dev/ttys00" + string(rune('0'+i)),
			Assistant: session.AssistantClaude,
			State:     session.StateIdle,
		})
	}
	return out
}

func inventoryOf(host ports.IdentityHost, sessions []session.Session, reads *ActivityReads) session.Inventory {
	in := Inventory{
		Process:  stubProcess{inv: session.Inventory{Sessions: sessions, Complete: true, Provenance: "ps"}},
		Identity: host,
		Activity: reads,
	}
	return in.Read(context.Background())
}

type stubProcess struct{ inv session.Inventory }

func (s stubProcess) Scan(context.Context) (session.Inventory, error) { return s.inv, nil }

// The defect this field exists to stop: a row nobody could read must not
// arrive looking like a row that has been quiet for a week.
func TestUnreadableActivityIsNotAnOldTime(t *testing.T) {
	quiet := time.Now().Add(-7 * 24 * time.Hour)
	src := &activitySource{answers: map[string]session.Activity{
		"a": {At: quiet, Evidence: session.EvidenceTranscript},
		"b": {Reason: session.ActivityUnreadable, Detail: "could not be read"},
		"c": {Reason: session.ActivityNoRecord, Detail: "nothing written yet"},
	}}
	inv := inventoryOf(src, rows(3), NewActivityReads())

	by := map[string]session.Activity{}
	for _, s := range inv.Sessions {
		by[s.ID] = s.Activity
	}
	if !by["a"].Known() || !by["a"].At.Equal(quiet) {
		t.Fatalf("a quiet session lost its time: %+v", by["a"])
	}
	for _, id := range []string{"b", "c"} {
		got := by[id]
		if got.Known() {
			t.Fatalf("%s: an unreadable time was reported as known: %+v", id, got)
		}
		if !got.At.IsZero() {
			t.Fatalf("%s: an unreadable time carried a time anyway: %v", id, got.At)
		}
	}
	// And the two kinds of nothing stay apart: only one of them is this
	// machine's own fault to fix.
	if by["b"].Unknown() == by["c"].Unknown() {
		t.Fatalf("two different kinds of nothing were flattened into %q", by["b"].Unknown())
	}
	// A time older than any of them is still a time, and outranks both as an
	// answer: it says the session was read and found silent.
	if by["a"].Unknown() != session.ActivityRead {
		t.Fatalf("a read session claimed a reason: %q", by["a"].Unknown())
	}
}

// The reading's own ceiling: rows past it are told apart from rows that have
// nothing, and are never given a time.
func TestActivityReadsAreBounded(t *testing.T) {
	const limit = 3
	answers := map[string]session.Activity{}
	for _, s := range rows(6) {
		answers[s.ID] = session.Activity{At: time.Now(), Evidence: session.EvidenceTranscript}
	}
	src := &activitySource{answers: answers}
	reads := NewActivityReads()
	reads.SetLimit(limit)

	inv := inventoryOf(src, rows(6), reads)
	if src.asked != limit {
		t.Fatalf("the source was asked %d times, past the bound of %d", src.asked, limit)
	}
	var unread int
	for _, s := range inv.Sessions {
		if s.Activity.Unknown() == session.ActivityUnread {
			unread++
			if !s.Activity.At.IsZero() {
				t.Fatalf("%s: an unread row carried a time", s.ID)
			}
		}
	}
	if unread != 3 {
		t.Fatalf("%d rows said unread, want 3", unread)
	}

	got := reads.Reading()
	if !got.Known || got.Used != limit {
		t.Fatalf("the capacity row read %+v, want Used=%d", got, limit)
	}
	if got.Counters.Refused != 3 {
		t.Fatalf("the capacity row counted %d refusals, want 3", got.Counters.Refused)
	}
	if got.Counters.LastActionAt.IsZero() {
		t.Fatalf("the capacity row refused something and recorded no moment")
	}
}

// A source that cannot answer at all says so, rather than leaving a zero that
// would read as a time from 1970.
func TestActivityWithoutASourceIsNamed(t *testing.T) {
	inv := inventoryOf(silentSource{}, rows(1), NewActivityReads())
	got := inv.Sessions[0].Activity
	if got.Known() || got.Unknown() != session.ActivityUnsupported {
		t.Fatalf("a source with no activity port answered %+v", got)
	}
}

// The default bound is the register's, so the number has one spelling.
func TestActivityReadLimitIsTheRegistersRow(t *testing.T) {
	if got := capacity.Default(capacity.SessionsActivityReads); got != ActivityReadLimit {
		t.Fatalf("the register says %d and internal/app says %d", got, ActivityReadLimit)
	}
}
