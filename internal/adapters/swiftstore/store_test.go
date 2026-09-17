package swiftstore

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The Swift app rewrites these files while this package reads them. A torn
// read must never become "the store is empty": the last whole reading is
// carried and marked stale, and with no whole reading at all the answer is
// that nothing is known.
func TestAHalfWrittenFileCarriesTheLastWholeReading(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "orchestrator.json")
	write := func(body string, at time.Time) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	st := Open(dir)

	write(`{"version":1,"tasks":[{"id":"a","state":"briefed","secret_hash":"x"}]}`, time.Unix(1000, 0))
	first := st.Read()
	if !first.Known || first.Stale || len(first.Tasks) != 1 || first.Tasks[0].ID != "a" {
		t.Fatalf("whole file: known=%v stale=%v tasks=%v", first.Known, first.Stale, first.Tasks)
	}

	write(`{"version":1,"tasks":[{"id":"a","sta`, time.Unix(2000, 0))
	torn := st.Read()
	if !torn.Known || !torn.Stale || len(torn.Tasks) != 1 || torn.Err == nil {
		t.Fatalf("torn file: known=%v stale=%v tasks=%d err=%v", torn.Known, torn.Stale, len(torn.Tasks), torn.Err)
	}

	write(`{"version":1,"tasks":[]}`, time.Unix(3000, 0))
	if again := st.Read(); !again.Known || again.Stale || len(again.Tasks) != 0 {
		t.Fatalf("rewritten file: known=%v stale=%v tasks=%d", again.Known, again.Stale, len(again.Tasks))
	}
}

func TestAStoreNeverReadIsUnknownAndNotEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "orchestrator.json"), []byte(`{"tasks":[`), 0o600); err != nil {
		t.Fatal(err)
	}
	snap := Open(dir).Read()
	if snap.Known || snap.Err == nil {
		t.Fatalf("known=%v err=%v", snap.Known, snap.Err)
	}
	c := snap.Closeability(CloseInput{
		Live:                Live{TerminalID: "%1", TTY: "ttys001", Assistant: "claude", PID: 7, ProcessStart: time.Unix(100, 0), ConversationID: "c"},
		TerminalState:       "idle",
		Bound:               true,
		Matches:             1,
		InventoryComplete:   true,
		InventoryObservedAt: time.Now(),
		Now:                 time.Now(),
	})
	if c.State != "unknown" || len(c.Reasons) == 0 || c.Reasons[0].Code != "swift_store_unreadable" {
		t.Fatalf("closeability over an unreadable store: %s %v", c.State, c.Reasons)
	}

	missing := Open(filepath.Join(dir, "nowhere")).Read()
	if missing.Known {
		t.Fatalf("a directory with no store reads as known")
	}
}

// A record is about this process only when all six facts agree; the start
// time within the Swift app's five seconds, and the tty in either spelling.
func TestARecordMatchesOnlyItsOwnProcess(t *testing.T) {
	claude, conv := "claude", "c"
	pid := int64(42)
	start := Seconds(1000.4)
	rec := Identity{TerminalID: "%9", TTY: "/dev/ttys009", Assistant: &claude, PID: &pid, ProcessStart: &start, ConversationID: &conv}
	live := Live{TerminalID: "%9", TTY: "ttys009", Assistant: "claude", PID: 42, ProcessStart: time.Unix(1004, 0), ConversationID: "c"}
	if !rec.Matches(live) {
		t.Fatal("same process did not match")
	}
	for name, change := range map[string]func(*Live){
		"pid":          func(l *Live) { l.PID = 43 },
		"start":        func(l *Live) { l.ProcessStart = time.Unix(1010, 0) },
		"conversation": func(l *Live) { l.ConversationID = "d" },
		"terminal":     func(l *Live) { l.TerminalID = "%10" },
		"unreadable":   func(l *Live) { l.ProcessStart = time.Time{} },
	} {
		l := live
		change(&l)
		if rec.Matches(l) {
			t.Errorf("a different %s still matched", name)
		}
	}
}
