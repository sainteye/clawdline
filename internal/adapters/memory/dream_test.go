package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDreamArchivesOnlySupportedCandidatesAndRestoresOriginalBytes(t *testing.T) {
	state := t.TempDir()
	s := New(state)
	original := invented("a-reference", TypeReference)
	duplicate := original
	duplicate.Name = "b-reference"
	ambiguous := original
	ambiguous.Name = "c-reference"
	ambiguous.Body = "Different content.\n"
	previous := invented("previous-delivery", TypeProject)
	current := invented("current-delivery", TypeProject)
	previous.Body += "\nSuperseded-by: current-delivery\n"
	protected := invented("a-preference", TypeUser)
	protectedDuplicate := protected
	protectedDuplicate.Name = "b-preference"
	missing := invented("missing-successor", TypeProject)
	missing.Body += "\nSuperseded-by: no-such-note\n"
	seed(t, s, original, duplicate, ambiguous, previous, current, protected, protectedDuplicate, missing)

	dir, err := s.Dir(testKey)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, previous.Name+".md")
	legacy := strings.Replace(Format(previous), "metadata:\n", "metadata:\n  record: historic\n", 1) + "\n"
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	preview, err := s.PlanDream(testKey)
	if err != nil || len(preview.Archive) != 2 || len(preview.Review) != 1 || preview.Review[0].Name != ambiguous.Name {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	if before, _ := os.ReadFile(path); string(before) != legacy {
		t.Fatal("preview wrote to an original")
	}
	now := time.Now().Add(2 * DreamQuietTime)
	if err := s.MaintainDreams(t.Context(), now, func(key string) bool { return key == testKey }); err != nil {
		t.Fatal(err)
	}
	if skipped, _ := s.PlanDream(testKey); skipped.LastRun != nil {
		t.Fatal("busy Project was altered")
	}
	if err := s.MaintainDreams(t.Context(), now, func(string) bool { return false }); err != nil {
		t.Fatal(err)
	}
	s = New(state)
	after, err := s.PlanDream(testKey)
	if err != nil || len(after.Archived) != 2 || after.LastRun == nil || !after.LastRun.Equal(now) {
		t.Fatalf("archive after restart: %+v %v", after, err)
	}
	if got, err := s.Get(testKey, previous.Name); err != nil || got.Storage != ArchivedSuperseded || got.Replacement != current.Name {
		t.Fatalf("read archived original: %+v %v", got, err)
	}
	if index, _, _, err := s.Index(testKey); err != nil || strings.Contains(index, previous.Name) || !strings.Contains(index, current.Name) {
		t.Fatalf("archive leaked into launch index: %v\n%s", err, index)
	}
	if result, err := s.Restore(testKey, previous.Name); err != nil || result != Updated {
		t.Fatalf("restore: %s %v", result, err)
	}
	if result, err := s.Restore(testKey, previous.Name); err != nil || result != Unchanged {
		t.Fatalf("restoring twice: %s %v", result, err)
	}
	restored, err := os.ReadFile(path)
	if err != nil || strings.Replace(string(restored), "  clawdline-storage: retained\n", "", 1) != legacy {
		t.Fatalf("restore altered the original bytes: %v\n%s", err, restored)
	}
	if result, err := s.PlanDream(testKey); err != nil || len(result.Archive) != 0 || len(result.Archived) != 1 {
		t.Fatalf("restored entry was proposed for archival: %+v %v", result, err)
	}
}

func TestDreamUsesPressureToSettlePendingAndSkipsAnUnchangedStore(t *testing.T) {
	s := New(t.TempDir())
	entries := make([]Entry, 0, EntryCountLimit)
	for i := range EntryCountLimit {
		entries = append(entries, invented(fmt.Sprintf("fixture-%04d", i), TypeProject))
	}
	seed(t, s, entries...)
	idle := func(string) bool { return false }
	now := time.Now()
	if err := s.MaintainDreams(t.Context(), now, idle); err != nil {
		t.Fatal(err)
	}
	if plan, _ := s.PlanDream(testKey); plan.LastRun != nil {
		t.Fatal("a recent write did not delay the initial pass")
	}
	start := now.Add(2 * DreamQuietTime)
	if err := s.MaintainDreams(t.Context(), start, idle); err != nil {
		t.Fatal(err)
	}
	if err := s.MaintainDreams(t.Context(), start.Add(DreamQuietTime), idle); err != nil {
		t.Fatal(err)
	}
	if plan, _ := s.PlanDream(testKey); plan.LastRun == nil || !plan.LastRun.Equal(start) {
		t.Fatal("an unchanged full store was reviewed again before the weekly deadline")
	}
	pending := invented("new-durable-memory", TypeUser)
	if result, err := s.Add(testKey, pending); err != nil || result != Created {
		t.Fatalf("new entry: %s %v", result, err)
	}
	duplicate := entries[1]
	duplicate.Description, duplicate.Body = entries[0].Description, entries[0].Body
	if _, err := s.Update(testKey, duplicate); err != nil {
		t.Fatal(err)
	}
	early := start.Add(2 * DreamQuietTime)
	if err := s.MaintainDreams(t.Context(), early, idle); err != nil {
		t.Fatal(err)
	}
	if plan, err := s.PlanDream(testKey); err != nil || plan.Counts != (StorageCounts{Active: EntryCountLimit, Archived: 1}) ||
		plan.LastRun == nil || !plan.LastRun.Equal(early) {
		t.Fatalf("earlier pass after crossing pressure: %+v %v", plan, err)
	}
	if got, err := s.Get(testKey, pending.Name); err != nil || got != pending {
		t.Fatalf("pending entry was lost or left out: %+v %v", got, err)
	}
	if index, _, _, err := s.Index(testKey); err != nil || !strings.Contains(index, pending.Name) {
		t.Fatalf("promoted entry missing from index: %v", err)
	}
	weekly := early.Add(DreamInterval)
	if err := s.MaintainDreams(t.Context(), weekly, idle); err != nil {
		t.Fatal(err)
	}
	if plan, _ := s.PlanDream(testKey); plan.LastRun == nil || !plan.LastRun.Equal(weekly) {
		t.Fatal("weekly review did not run")
	}
}
