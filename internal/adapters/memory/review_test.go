package memory

import (
	"errors"
	"strings"
	"testing"
)

func TestRelatedDeliveriesRequireReviewAndMergePreservesTheOriginal(t *testing.T) {
	s := New(t.TempDir())
	old := invented("feature-local-20261001", TypeProject)
	next := invented("feature-shipped-20261002", TypeProject)
	old.Body = "Original-only validation evidence.\n"
	seed(t, s, old, next)
	plan, err := s.PlanDream(testKey)
	if err != nil || len(plan.Archive) != 0 || len(plan.Review) != 1 || plan.Review[0].Reason != "related_delivery" {
		t.Fatalf("plan: %+v %v", plan, err)
	}
	before := ReviewRequest{Action: "merge", Expected: EntryRevision(old), Replacement: next.Name,
		ExpectedReplacement: EntryRevision(next), Body: "Combined delivery and validation.\n"}
	next.Body = "Newly observed deployment evidence.\n"
	if _, err := s.Update(testKey, next); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Review(testKey, old.Name, before); !errors.Is(err, ErrReviewStale) {
		t.Fatalf("stale review: %v", err)
	}
	before.ExpectedReplacement = EntryRevision(next)
	if result, err := s.Review(testKey, old.Name, before); result != Updated || err != nil {
		t.Fatalf("merge: %s %v", result, err)
	}
	if result, err := s.Review(testKey, old.Name, before); result != Unchanged || err != nil {
		t.Fatalf("merge retry: %s %v", result, err)
	}
	archived, err := s.Get(testKey, old.Name)
	if err != nil || archived.Body != old.Body || archived.Storage != archivedReviewed {
		t.Fatalf("archive: %+v %v", archived, err)
	}
	got, err := s.Get(testKey, next.Name)
	if err != nil || got.Body != before.Body {
		t.Fatalf("replacement: %+v %v", got, err)
	}
	names, err := s.Search(testKey, "ORIGINAL-ONLY")
	if err != nil || len(names) != 1 || names[0] != old.Name {
		t.Fatalf("archive search: %v %v", names, err)
	}
	if _, err := s.Restore(testKey, old.Name); err != nil {
		t.Fatal(err)
	}
	plan, err = s.PlanDream(testKey)
	if err != nil || len(plan.Review)+len(plan.Archive) != 0 {
		t.Fatalf("restored original was suggested again: %+v %v", plan, err)
	}
}

func TestKeepingAMemoryRemovesItFromSuggestions(t *testing.T) {
	s := New(t.TempDir())
	a := invented("old-local", TypeProject)
	b := invented("old-shipped", TypeProject)
	b.Description = a.Description
	seed(t, s, b, a)
	if _, err := s.Review(testKey, b.Name, ReviewRequest{Action: "keep", Expected: EntryRevision(b)}); err != nil {
		t.Fatal(err)
	}
	plan, err := s.PlanDream(testKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, suggestions := range [][]DreamChange{plan.Archive, plan.Review} {
		for _, suggestion := range suggestions {
			if strings.EqualFold(suggestion.Name, b.Name) {
				t.Fatalf("retained memory was suggested: %+v", suggestion)
			}
		}
	}
}
