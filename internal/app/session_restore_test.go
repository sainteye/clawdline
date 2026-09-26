package app

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// countingStore counts the writes that reach the store.
type countingStore struct {
	*store.Store
	records, touches int
}

func (c *countingStore) RecordBoot(ctx context.Context, rd store.BootReading) (int64, error) {
	c.records++
	return c.Store.RecordBoot(ctx, rd)
}

func (c *countingStore) TouchBoot(ctx context.Context, boot string, now time.Time) error {
	c.touches++
	return c.Store.TouchBoot(ctx, boot, now)
}

func restoreStore(t *testing.T) *countingStore {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return &countingStore{Store: s}
}

func restoreUnder(st RestoreStore, boot string, clock *time.Time) *SessionRestore {
	return &SessionRestore{
		Store: st,
		Boot:  func(context.Context) (string, error) { return boot, nil },
		Now:   func() time.Time { return *clock },
	}
}

func claudeRow(terminal, conversation string) session.Session {
	return session.Session{ID: terminal, Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		CWD: "/work/" + conversation, ConversationID: conversation, Label: "label " + conversation}
}

func complete(rows ...session.Session) session.Inventory {
	return session.Inventory{Complete: true, Sessions: rows}
}

func recorded(t *testing.T, st RestoreStore, boot string) map[string]store.RestoreRow {
	t.Helper()
	rows, err := st.RestoreRows(context.Background(), boot)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]store.RestoreRow{}
	for _, r := range rows {
		out[r.ConversationID] = r
	}
	return out
}

func TestRestoreRecordingFollowsTheCurrentBootAndIgnoresIncompleteReadings(t *testing.T) {
	st := restoreStore(t)
	clock := time.Unix(10_000, 0)
	r := restoreUnder(st, "boot-now", &clock)
	ctx := context.Background()

	shell := session.Session{ID: "%9", Backend: session.BackendTmux, CWD: "/tmp"}
	nameless := claudeRow("%8", "")
	codex := session.Session{ID: "%7", Backend: session.BackendTmux, Assistant: session.AssistantCodex,
		CWD: "/work/x", ConversationID: "cx-1"}
	r.Observe(ctx, complete(claudeRow("%1", "c-1"), claudeRow("%2", "c-2"), shell, nameless, codex))
	got := recorded(t, st, "boot-now")
	if len(got) != 3 || got["c-1"].Place != projects.PlaceID("/work/c-1") || got["c-1"].Title != "label c-1" ||
		got["cx-1"].Assistant != "codex" || got["c-2"].Backend != "tmux" {
		t.Fatalf("recorded = %+v", got)
	}

	// An incomplete reading — even one with nothing in it — changes nothing.
	r.Observe(ctx, session.Inventory{Complete: false})
	if got := recorded(t, st, "boot-now"); len(got) != 3 {
		t.Fatalf("an incomplete reading changed the record: %+v", got)
	}

	// A complete reading is the whole set: what it does not show is gone,
	// and its row says since when rather than disappearing.
	clock = clock.Add(time.Second)
	gone := clock
	r.Observe(ctx, complete(claudeRow("%1", "c-1")))
	got = recorded(t, st, "boot-now")
	if len(got) != 3 || !got["c-1"].GoneAt.IsZero() || !got["c-2"].GoneAt.Equal(gone) || !got["cx-1"].GoneAt.Equal(gone) {
		t.Fatalf("recorded = %+v", got)
	}
	// A complete empty one — a shutdown's last scan, after iTerm2 quit and
	// the tmux server was killed — keeps every row.
	clock = clock.Add(time.Second)
	r.Observe(ctx, complete())
	got = recorded(t, st, "boot-now")
	if len(got) != 3 || !got["c-1"].GoneAt.Equal(clock) || !got["c-2"].GoneAt.Equal(gone) {
		t.Fatalf("a complete empty reading left: %+v", got)
	}
	// Seen again in the same boot, it is open again.
	clock = clock.Add(time.Second)
	r.Observe(ctx, complete(claudeRow("%5", "c-2")))
	if got := recorded(t, st, "boot-now"); !got["c-2"].GoneAt.IsZero() || got["c-1"].GoneAt.IsZero() {
		t.Fatalf("after c-2 came back: %+v", got)
	}
}

func TestTheBootsHeartbeatBeatsAtMostOnceAMinute(t *testing.T) {
	st := restoreStore(t)
	clock := time.Unix(25_000, 0)
	r := restoreUnder(st, "boot-now", &clock)
	ctx := context.Background()
	inv := complete(claudeRow("%1", "c-1"))
	r.Observe(ctx, inv) // the first write stamps the boot itself
	// Twelve beats of the broker, five seconds apart, is one minute.
	for i := 0; i < 12; i++ {
		clock = clock.Add(5 * time.Second)
		r.Observe(ctx, inv)
	}
	if st.records != 1 || st.touches != 1 {
		t.Fatalf("a minute of identical readings: %d writes, %d heartbeats; want 1 and 1", st.records, st.touches)
	}
	prev, _, err := st.PreviousBoot(ctx, "boot-after")
	if err != nil || !prev.LastSeen.Equal(clock) {
		t.Fatalf("the boot was last seen %v, want %v (%v)", prev.LastSeen, clock, err)
	}
	// Another 55 seconds is no beat yet; the sixtieth is.
	for i := 0; i < 11; i++ {
		clock = clock.Add(5 * time.Second)
		r.Observe(ctx, inv)
	}
	if st.touches != 1 {
		t.Fatalf("heartbeats after 55 more seconds = %d", st.touches)
	}
	clock = clock.Add(5 * time.Second)
	r.Observe(ctx, inv)
	if st.touches != 2 {
		t.Fatalf("heartbeats after a second minute = %d", st.touches)
	}
	// An incomplete reading is no evidence the machine was seen.
	clock = clock.Add(2 * time.Minute)
	r.Observe(ctx, session.Inventory{Complete: false})
	if st.touches != 2 {
		t.Fatalf("an incomplete reading beat the heart: %d", st.touches)
	}
}

// A boot that ended with a shutdown: the person closed c-early long before,
// c-final and c-last went in the final wave, c-never was never seen to go.
func TestRestorableOffersTheFinalWaveAndWhatNeverWentButNotWhatClosedEarlier(t *testing.T) {
	st := restoreStore(t)
	clock := time.Unix(50_000, 0)
	before := restoreUnder(st, "boot-before", &clock)
	ctx := context.Background()
	early, final, last, never := claudeRow("%1", "c-early"), claudeRow("%2", "c-final"), claudeRow("%3", "c-last"), claudeRow("%4", "c-never")
	before.Observe(ctx, complete(early, final, last, never))
	clock = clock.Add(time.Minute)
	before.Observe(ctx, complete(final, last, never)) // c-early closed
	// An hour of the same set, heartbeat and all.
	for i := 0; i < 720; i++ {
		clock = clock.Add(5 * time.Second)
		before.Observe(ctx, complete(final, last, never))
	}
	// The shutdown: iTerm2 quits, then the tmux server goes.
	clock = clock.Add(5 * time.Second)
	before.Observe(ctx, complete(last, never))
	clock = clock.Add(5 * time.Second)
	// An incomplete reading (a source that timed out) is all that sees
	// c-never's end; then the power goes.
	before.Observe(ctx, session.Inventory{Complete: false})

	clock = clock.Add(time.Hour)
	offer, err := restoreUnder(st, "boot-after", &clock).Restorable(ctx, complete())
	if err != nil || !offer.Available {
		t.Fatalf("offer = %+v, %v", offer, err)
	}
	got := map[string]bool{}
	for _, row := range offer.Rows {
		got[row.ConversationID] = true
	}
	if !got["c-final"] || !got["c-last"] || !got["c-never"] || got["c-early"] || len(got) != 3 {
		t.Fatalf("offered %v, want c-final, c-last and c-never", got)
	}
}

func TestRestorableDrawsTheGraceLineFromTheBootsLastSeen(t *testing.T) {
	st := restoreStore(t)
	clock := time.Unix(60_000, 0)
	before := restoreUnder(st, "boot-before", &clock)
	ctx := context.Background()
	inside, outside := claudeRow("%1", "inside"), claudeRow("%2", "outside")
	before.Observe(ctx, complete(inside, outside))
	clock = clock.Add(time.Minute)
	before.Observe(ctx, complete(inside)) // outside goes here
	clock = clock.Add(time.Second)
	insideGone := clock
	before.Observe(ctx, complete()) // inside goes one second later
	// Nothing changes for exactly grace, so the boot's last sight is a
	// heartbeat: inside went exactly grace before it, outside one second more.
	clock = clock.Add(restoreGraceLimit)
	before.Observe(ctx, complete())
	if prev, _, _ := st.PreviousBoot(ctx, "boot-after"); !prev.LastSeen.Equal(insideGone.Add(restoreGraceLimit)) {
		t.Fatalf("the boot was last seen %v", prev.LastSeen)
	}

	clock = clock.Add(time.Hour)
	offer, err := restoreUnder(st, "boot-after", &clock).Restorable(ctx, complete())
	if err != nil || len(offer.Rows) != 1 || offer.Rows[0].ConversationID != "inside" {
		t.Fatalf("offer = %+v, %v", offer, err)
	}
}

func TestASessionClosedThroughClawdlineIsNeverOffered(t *testing.T) {
	st := restoreStore(t)
	clock := time.Unix(70_000, 0)
	before := restoreUnder(st, "boot-before", &clock)
	ctx := context.Background()
	kept, closed := claudeRow("%1", "kept"), claudeRow("%2", "closed")
	before.Observe(ctx, complete(kept, closed))
	clock = clock.Add(5 * time.Second)
	before.Closed(ctx, closed)
	// A scan that began before the close still showed it.
	clock = clock.Add(5 * time.Second)
	stale := complete(kept, closed)
	stale.ObservedAt = clock.Add(-7 * time.Second)
	before.Observe(ctx, stale)
	clock = clock.Add(5 * time.Second)
	before.Observe(ctx, complete()) // the shutdown, seconds later

	clock = clock.Add(time.Hour)
	offer, err := restoreUnder(st, "boot-after", &clock).Restorable(ctx, complete())
	if err != nil || len(offer.Rows) != 1 || offer.Rows[0].ConversationID != "kept" {
		t.Fatalf("offer = %+v, %v", offer, err)
	}
}

func TestRestoreRecordingWritesOnlyWhenTheSetChanges(t *testing.T) {
	st := restoreStore(t)
	clock := time.Unix(20_000, 0)
	r := restoreUnder(st, "boot-now", &clock)
	ctx := context.Background()
	inv := complete(claudeRow("%1", "c-1"))
	for i := 0; i < 5; i++ {
		clock = clock.Add(5 * time.Second)
		r.Observe(ctx, inv)
	}
	if st.records != 1 {
		t.Fatalf("five identical readings wrote %d times", st.records)
	}
	// A stale last_seen is refreshed on its own clock.
	clock = clock.Add(restoreSeenLimit)
	r.Observe(ctx, inv)
	if st.records != 2 {
		t.Fatalf("a reading past the seen interval wrote %d times in all", st.records)
	}
	clock = clock.Add(time.Second)
	r.Observe(ctx, complete(claudeRow("%1", "c-1"), claudeRow("%2", "c-2")))
	if st.records != 3 {
		t.Fatalf("a changed set wrote %d times in all", st.records)
	}
}

func TestRestoreRecordingLeavesOutTheBrokersChildren(t *testing.T) {
	st := restoreStore(t)
	clock := time.Unix(30_000, 0)
	r := restoreUnder(st, "boot-now", &clock)
	r.Children = func(_ context.Context, rows []session.Session) map[string]bool {
		return map[string]bool{"%2": true}
	}
	r.Observe(context.Background(), complete(claudeRow("%1", "c-1"), claudeRow("%2", "child-conv")))
	got := recorded(t, st, "boot-now")
	if _, child := got["child-conv"]; child || len(got) != 1 {
		t.Fatalf("recorded = %+v", got)
	}
}

func TestRestoreWithNoBootIdRecordsNothingAndSaysUnavailable(t *testing.T) {
	st := restoreStore(t)
	r := &SessionRestore{Store: st,
		Boot: func(context.Context) (string, error) { return "", errors.New("no source") }}
	r.Observe(context.Background(), complete(claudeRow("%1", "c-1")))
	if st.records != 0 {
		t.Fatalf("an unknown boot wrote %d times", st.records)
	}
	offer, err := r.Restorable(context.Background(), complete())
	if err != nil || offer.Available || offer.Reason != RestoreBootUnknown || len(offer.Rows) != 0 {
		t.Fatalf("offer = %+v, %v", offer, err)
	}
	results, err := r.Restore(context.Background(), []string{"c-1"}, complete(),
		func(context.Context, store.RestoreRow) (Started, error) {
			t.Fatal("a restore with no boot id opened something")
			return Started{}, nil
		})
	if err != nil || len(results) != 1 || results[0].Code != RestoreNotRestorable {
		t.Fatalf("results = %+v, %v", results, err)
	}
}

// previousBoot records rows under boot-before and answers a restore for the
// boot after it.
func previousBoot(t *testing.T, rows ...session.Session) (*countingStore, *SessionRestore, *time.Time) {
	t.Helper()
	st := restoreStore(t)
	clock := time.Unix(40_000, 0)
	restoreUnder(st, "boot-before", &clock).Observe(context.Background(), complete(rows...))
	clock = clock.Add(time.Hour)
	return st, restoreUnder(st, "boot-after", &clock), &clock
}

func TestRestorableIsThePreviousBootLessAnsweredAndLiveRows(t *testing.T) {
	st, after, _ := previousBoot(t, claudeRow("%1", "c-1"), claudeRow("%2", "c-2"), claudeRow("%3", "c-3"))
	ctx := context.Background()

	// The same boot offers nothing: a daemon restart leaves tmux where it was.
	clock := time.Unix(40_000, 0)
	same, err := restoreUnder(st, "boot-before", &clock).Restorable(ctx, complete())
	if err != nil || !same.Available || len(same.Rows) != 0 {
		t.Fatalf("same boot offered = %+v, %v", same, err)
	}

	if _, err := st.ResolveRestore(ctx, "boot-before", []string{"c-2"}, store.RestoreDismissed, clock); err != nil {
		t.Fatal(err)
	}
	offer, err := after.Restorable(ctx, complete(claudeRow("%40", "c-3")))
	if err != nil || !offer.Available || offer.Previous.ID != "boot-before" {
		t.Fatalf("offer = %+v, %v", offer, err)
	}
	if len(offer.Rows) != 1 || offer.Rows[0].ConversationID != "c-1" {
		t.Fatalf("offered = %+v", offer.Rows)
	}
}

func TestRestoreOpensEachRowThroughResumeAndAnswersTypedFailures(t *testing.T) {
	st, after, _ := previousBoot(t, claudeRow("%1", "ok"), claudeRow("%2", "gone-dir"),
		claudeRow("%3", "gone-conv"), claudeRow("%4", "broken"), claudeRow("%5", "busy"))
	ctx := context.Background()
	var asked []string
	resume := func(_ context.Context, row store.RestoreRow) (Started, error) {
		asked = append(asked, row.ConversationID)
		switch row.ConversationID {
		case "ok":
			if row.CWD != "/work/ok" || row.Assistant != "claude" {
				t.Errorf("resume was handed %+v", row)
			}
			return Started{ID: "%50", Backend: "tmux"}, nil
		case "gone-dir":
			return Started{}, ErrRestorePlaceUnavailable
		case "gone-conv":
			return Started{}, notFound("No conversation named that")
		case "busy":
			return Started{}, ErrRestoreOverCapacity
		}
		return Started{}, StartRefusal{Status: http.StatusBadGateway, Code: "terminal_io_failed", Message: "tmux said no"}
	}
	results, err := after.Restore(ctx, []string{"ok", "gone-dir", "gone-conv", "broken", "busy", "never", "ok"},
		complete(), resume)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"ok": "", "gone-dir": RestorePlaceUnavailable, "gone-conv": RestoreConversationNotFound,
		"broken": RestoreOpenFailed, "busy": RestoreOverCapacity, "never": RestoreNotRestorable}
	if len(results) != len(want) {
		t.Fatalf("results = %+v", results)
	}
	for _, res := range results {
		code, ok := want[res.ConversationID]
		if !ok || res.Code != code || res.OK != (code == "") {
			t.Errorf("%s answered %+v, want code %q", res.ConversationID, res, code)
		}
	}
	if results[0].Started.ID != "%50" {
		t.Fatalf("the opened row did not carry its terminal: %+v", results[0])
	}
	if len(asked) != 5 {
		t.Fatalf("resume was asked for %v; a row not on offer must not be opened, and a repeat once", asked)
	}
	rows := recorded(t, st, "boot-before")
	if rows["ok"].Resolution != store.RestoreRestored || rows["broken"].Resolution != "" {
		t.Fatalf("resolutions: ok=%q broken=%q", rows["ok"].Resolution, rows["broken"].Resolution)
	}
	// Restored is no longer offered.
	offer, _ := after.Restorable(ctx, complete())
	for _, row := range offer.Rows {
		if row.ConversationID == "ok" {
			t.Fatal("a restored row is still on offer")
		}
	}

	many := make([]string, restoreBatchLimit+1)
	for i := range many {
		many[i] = string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	if _, err := after.Restore(ctx, many, complete(), resume); !errors.Is(err, ErrRestoreBatch) {
		t.Fatalf("an oversized batch = %v", err)
	}
}

func TestDismissAnswersNamedRowsOrEveryRowOnOffer(t *testing.T) {
	_, after, _ := previousBoot(t, claudeRow("%1", "c-1"), claudeRow("%2", "c-2"), claudeRow("%3", "c-3"))
	ctx := context.Background()
	if n, ok, err := after.Dismiss(ctx, []string{"c-1", "c-1"}); err != nil || !ok || n != 1 {
		t.Fatalf("dismiss c-1 = %d %v %v", n, ok, err)
	}
	if n, ok, err := after.Dismiss(ctx, nil); err != nil || !ok || n != 2 {
		t.Fatalf("dismiss all = %d %v %v", n, ok, err)
	}
	if offer, _ := after.Restorable(ctx, complete()); len(offer.Rows) != 0 {
		t.Fatalf("still offered: %+v", offer.Rows)
	}
}

func TestInventoryReadingShowsObserversTheRawReading(t *testing.T) {
	scan := func(context.Context) session.Inventory {
		return session.Inventory{Complete: true, ObservedAt: time.Now(), Sessions: []session.Session{claudeRow("%1", "c-1")}}
	}
	r := NewInventoryReading(scan, time.Hour)
	seen := make(chan session.Inventory, 4)
	r.Observe(func(_ context.Context, inv session.Inventory) { seen <- inv })
	r.Fresh(context.Background())
	select {
	case inv := <-seen:
		if !inv.Complete || len(inv.Sessions) != 1 {
			t.Fatalf("observer saw %+v", inv)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the observer was never shown the scan")
	}
}
