package projectassistant

import (
	"testing"
	"time"
)

func TestIdleBotStateEvaluation(t *testing.T) {
	now := time.Now()
	threshold := 10 * time.Minute

	// Safe idle: no active turns, no pending work, last active 15m ago
	s1 := IdleBotState{
		LastActiveAt: now.Add(-15 * time.Minute),
		ActiveTurns:  0,
		PendingWork:  0,
	}
	if !s1.IsIdle(now, threshold) {
		t.Fatal("expected s1 to be idle")
	}

	// Busy due to active turn
	s2 := IdleBotState{
		LastActiveAt: now.Add(-15 * time.Minute),
		ActiveTurns:  1,
		PendingWork:  0,
	}
	if s2.IsIdle(now, threshold) {
		t.Fatal("expected s2 not to be idle (active turns)")
	}

	// Busy due to pending work
	s3 := IdleBotState{
		LastActiveAt: now.Add(-15 * time.Minute),
		ActiveTurns:  0,
		PendingWork:  1,
	}
	if s3.IsIdle(now, threshold) {
		t.Fatal("expected s3 not to be idle (pending work)")
	}

	// Not rested long enough
	s4 := IdleBotState{
		LastActiveAt: now.Add(-5 * time.Minute),
		ActiveTurns:  0,
		PendingWork:  0,
	}
	if s4.IsIdle(now, threshold) {
		t.Fatal("expected s4 not to be idle (insufficient rest)")
	}
}

func TestCompactMemoryEntries(t *testing.T) {
	key := BotKey{MachineID: "m1", BotID: "b1"}
	proj := ProjectScope{MachineID: "m1", ProjectID: "p1"}
	s1 := MemorySource{ID: "src-1", Key: key, Project: proj, Kind: "user", Revision: 1}
	s2 := MemorySource{ID: "src-2", Key: key, Project: proj, Kind: "bot", Revision: 2}

	entries := []MemoryEntry{
		{ID: "e1", Key: key, Project: proj, Text: "pref: user prefers concise answers", Sources: []MemorySource{s1}, Revision: 1},
		{ID: "e2", Key: key, Project: proj, Text: "pref: user uses dark theme", Sources: []MemorySource{s2}, Revision: 2},
		{ID: "e3", Key: key, Project: proj, Text: "unrelated note about something else", Sources: []MemorySource{s1}, Revision: 2},
	}

	plan, changed := CompactMemoryEntries(entries, 2)
	if !changed {
		t.Fatal("expected compaction plan to produce changes")
	}
	if len(plan.AggregatedEntries) != 1 {
		t.Fatalf("expected 1 aggregated entry, got %d", len(plan.AggregatedEntries))
	}
	agg := plan.AggregatedEntries[0]
	if len(agg.Sources) != 2 {
		t.Fatalf("expected 2 merged sources, got %d", len(agg.Sources))
	}
	if len(plan.TombstoneIDs) != 2 {
		t.Fatalf("expected 2 tombstone IDs, got %d", len(plan.TombstoneIDs))
	}
	if plan.TombstoneIDs[0] != "e1" || plan.TombstoneIDs[1] != "e2" {
		t.Fatalf("unexpected tombstone IDs: %v", plan.TombstoneIDs)
	}
}
