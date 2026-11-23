package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func lessonBody(lines int, last string) string {
	var b strings.Builder
	for i := range lines {
		fmt.Fprintf(&b, "Invented lesson line %d about the same delivery.\n", i)
	}
	return b.String() + last + "\n"
}

// Two entries that say almost the same thing under different names and
// descriptions are a suggestion for the person, never an automatic move.
func TestDreamSuggestsNearDuplicatesWithoutMovingThem(t *testing.T) {
	state := t.TempDir()
	s := New(state)
	first := invented("a-delivery-record", TypeProject)
	first.Body = lessonBody(9, "Invented closing line written first.")
	second := invented("b-delivery-record", TypeProject)
	second.Description = "a different one-line description"
	second.Body = lessonBody(9, "Invented closing line written later, with one more detail.")
	unrelated := invented("c-unrelated-record", TypeProject)
	unrelated.Body = "Invented unrelated line one about something else.\nInvented unrelated line two about something else.\nInvented unrelated line three.\n"
	person := invented("a-person-preference", TypeUser)
	person.Body = first.Body
	personToo := invented("b-person-preference", TypeUser)
	personToo.Body = second.Body
	seed(t, s, first, second, unrelated, person, personToo)

	plan, err := s.PlanDream(testKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Archive) != 0 {
		t.Fatalf("near duplicates were planned for automatic archive: %+v", plan.Archive)
	}
	var near []DreamChange
	for _, row := range plan.Review {
		if row.Reason == "near_duplicate" {
			near = append(near, row)
		}
	}
	if len(near) != 1 || near[0].Name != first.Name || near[0].Replacement != second.Name {
		t.Fatalf("want a-delivery-record suggested under b-delivery-record, the longer one; got %+v", plan.Review)
	}
	if err := s.MaintainDreams(t.Context(), time.Now().Add(2*DreamQuietTime), func(string) bool { return false }); err != nil {
		t.Fatal(err)
	}
	dir, _ := s.Dir(testKey)
	raw, err := os.ReadFile(filepath.Join(dir, first.Name+".md"))
	if err != nil || strings.Contains(string(raw), storageKey) {
		t.Fatalf("maintenance moved a suggestion: %q %v", raw, err)
	}
}

// A body that points at its near copy as the newer record is the one offered
// for archive, whatever their lengths.
func TestNearDuplicateKeepsTheRecordTheOtherPointsTo(t *testing.T) {
	s := New(t.TempDir())
	older := invented("a-older-stage", TypeProject)
	older.Body = lessonBody(9, "Invented history; the latest is b-newer-stage, with a much longer closing line.")
	newer := invented("b-newer-stage", TypeProject)
	newer.Description = "another invented description"
	newer.Body = lessonBody(9, "Invented closing line.")
	seed(t, s, older, newer)
	plan, err := s.PlanDream(testKey)
	if err != nil || len(plan.Review) != 1 || plan.Review[0].Name != older.Name || plan.Review[0].Replacement != newer.Name {
		t.Fatalf("want a-older-stage under b-newer-stage: %+v %v", plan.Review, err)
	}
}

// The capacity reading carries the fullest Project's suggestions once it is
// near its bound, and nothing below it.
func TestFullestProjectReportsItsSuggestionsNearTheBound(t *testing.T) {
	s := New(t.TempDir())
	previous := invented("previous-delivery", TypeProject)
	previous.Body += "\nSuperseded-by: current-delivery\n"
	seed(t, s, previous, invented("current-delivery", TypeProject))
	live, suggestions, err := s.FullestProject()
	if err != nil || live != 2 || suggestions != -1 {
		t.Fatalf("below the notice bound: %d live, %d suggestions, %v", live, suggestions, err)
	}
	var filler []Entry
	for i := range NoticeEntryLimit - 2 {
		filler = append(filler, invented(fmt.Sprintf("filler-%04d", i), TypeUser))
	}
	seed(t, s, filler...)
	live, suggestions, err = s.FullestProject()
	if err != nil || live != NoticeEntryLimit || suggestions != 1 {
		t.Fatalf("at the notice bound: %d live, %d suggestions, %v", live, suggestions, err)
	}
}

func TestNoticeEntryLimitIsEightyFivePercentOfTheLiveBound(t *testing.T) {
	if NoticeEntryLimit*100 < LiveEntryLimit*85 || (NoticeEntryLimit-1)*100 >= LiveEntryLimit*85 {
		t.Fatalf("NoticeEntryLimit %d is not the first count at 85%% of %d", NoticeEntryLimit, LiveEntryLimit)
	}
}
