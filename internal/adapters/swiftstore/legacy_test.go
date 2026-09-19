package swiftstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const fixtureSchedule = "0f0e0d0c-0b0a-4908-8706-050403020100"

// writeLegacyFixture is a Swift store with something in every file this
// package reads, so a reader that opened any of them would find it.
func writeLegacyFixture(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"orchestrator.json": `{"version":1,"tasks":[{"id":"swift-task","state":"briefed","title":"From the Swift app","child_terminal":"%1"}]}`,
		"coordinator.json":  `{"version":1,"id":"c","label":"Clawdfather","sessionID":"%1","assistant":"claude","tty":"ttys001"}`,
		"config.json":       `{"session_titles":[{"title":"typed","terminal_id":"%1"}],"status_dir":"/somewhere"}`,
		filepath.Join("schedules", fixtureSchedule+".json"): `{"clawdline_schedule":1,"schedule_id":"` + fixtureSchedule + `","title":"Nightly"}`,
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "usage.sqlite3"), []byte("a ledger"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// With the switch off nothing of the Swift app's is read, and every reader
// says so; the control is the same fixture with the switch on, where each
// reader finds what is there — so a reader that ignored the switch is red.
func TestTheLegacySwitchOpensNothing(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	dir := t.TempDir()
	writeLegacyFixture(t, dir)

	t.Setenv(EnvLegacyStore, "on")
	on := Open(dir)
	if snap := on.Read(); !snap.Known || len(snap.Tasks) != 1 || snap.Source != SourceCurrent ||
		snap.Coordinator == nil || !snap.TitlesKnown {
		t.Fatalf("control: the fixture was not read with the switch on: %+v", snap)
	}
	if titles := on.ScheduleTitles(); titles[fixtureSchedule] != "Nightly" {
		t.Fatalf("control: schedule titles %v", titles)
	}
	if q := OpenQuotaConfig(dir).Read(); q.StatusDir != "/somewhere" {
		t.Fatalf("control: quota settings %+v", q)
	}
	if r := OpenUsageLedger(dir).Read(context.Background()); r.Missing || r.Disabled {
		t.Fatalf("control: the ledger read as missing: %+v", r)
	}

	t.Setenv(EnvLegacyStore, "off")
	off := Open(dir)
	snap := off.Read()
	if snap.Known || len(snap.Tasks) != 0 || snap.Source != SourceDisabled || snap.Coordinator != nil ||
		snap.TitlesKnown || !off.Disabled() {
		t.Fatalf("switched off, the store was read: %+v", snap)
	}
	if !snap.Usable() {
		t.Fatal("a store switched off is known to hold nothing, and must be usable")
	}
	if titles := off.ScheduleTitles(); len(titles) != 0 {
		t.Fatalf("switched off, schedule titles were read: %v", titles)
	}
	if q := OpenQuotaConfig(dir).Read(); q != (QuotaSettings{}) {
		t.Fatalf("switched off, quota settings were read: %+v", q)
	}
	if r := OpenUsageLedger(dir).Read(context.Background()); !r.Missing || !r.Disabled || r.Err != nil {
		t.Fatalf("switched off, the ledger reading is %+v", r)
	}

	// A word that is not on or off leaves the stores on: a misspelt switch
	// must not look like one that worked.
	t.Setenv(EnvLegacyStore, "of")
	if snap := Open(dir).Read(); snap.Source != SourceCurrent {
		t.Fatalf("a misspelt switch changed the reading: %s", snap.Source)
	}
}

// No store at all is known and holds nothing: the session list may draw its
// own records as the whole answer. An unreadable one is not (the control).
func TestAnAbsentStoreIsKnownAndUnreadableIsNot(t *testing.T) {
	t.Setenv(EnvLegacyStore, "")
	live := Live{TerminalID: "%1", TTY: "ttys001", Assistant: "claude", PID: 7, ProcessStart: time.Unix(100, 0), ConversationID: "c"}
	in := CloseInput{Live: live, TerminalState: "idle", Bound: true, Matches: 1, InventoryComplete: true,
		InventoryObservedAt: time.Now(), Now: time.Now()}

	absent := Open(filepath.Join(t.TempDir(), "nowhere")).Read()
	if absent.Known || absent.Source != SourceAbsent || !absent.Usable() || absent.Err != nil {
		t.Fatalf("absent store: %+v", absent)
	}
	for _, r := range absent.Closeability(in).Reasons {
		if r.Code == "swift_store_unreadable" {
			t.Fatalf("an absent store was called unreadable: %v", r)
		}
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "orchestrator.json"), []byte(`{"tasks":[`), 0o600); err != nil {
		t.Fatal(err)
	}
	bad := Open(dir).Read()
	if bad.Usable() || bad.Source != SourceUnreadable {
		t.Fatalf("an unreadable store is usable: %+v", bad)
	}
	if c := bad.Closeability(in); c.State != "unknown" || c.Reasons[0].Code != "swift_store_unreadable" {
		t.Fatalf("control: closeability over an unreadable store: %s %v", c.State, c.Reasons)
	}
}
