package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// A root landing is one row per item, repository, commit and target, and an
// item holds at most WorkV2RootLandingLimit of them: the next is refused and
// nothing is evicted.
func TestRootLandingsAreOneRowEachAndBounded(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	record := func(commit, tip string) (RootLanding, error) {
		var out RootLanding
		err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
			var err error
			out, err = tx.RecordRootLanding(RootLanding{WorkID: "w1", SessionID: "s", Repository: "/r", Target: "main",
				Commit: commit, TargetCommit: tip, Remote: "origin", RemoteCommit: tip, RecordedAt: time.Unix(100, 0)})
			return err
		})
		return out, err
	}
	first, err := record("c0", "t1")
	if err != nil {
		t.Fatal(err)
	}
	again, err := record("c0", "t2")
	if err != nil || again != first {
		t.Fatalf("the same landing again = %+v %v, want %+v", again, err, first)
	}
	for n := 1; n < WorkV2RootLandingLimit; n++ {
		if _, err := record(fmt.Sprintf("c%d", n), "t"); err != nil {
			t.Fatalf("row %d: %v", n, err)
		}
	}
	if _, err := record("one-too-many", "t"); !errors.Is(err, ErrRootLandingsFull) {
		t.Fatalf("past the limit: %v", err)
	}
	rows, err := s.RootLandings(ctx, "w1")
	if err != nil || len(rows) != WorkV2RootLandingLimit {
		t.Fatalf("rows = %d %v", len(rows), err)
	}
	kept := false
	for _, r := range rows {
		kept = kept || r == first
	}
	if !kept {
		t.Fatalf("the first landing is gone from %+v", rows)
	}
}
