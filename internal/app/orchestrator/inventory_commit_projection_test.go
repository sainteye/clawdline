package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// A settled task whose branch is absent cannot offer disposal. Its commit
// count is not part of any returned row or inventory receipt.
func TestInventoryDoesNotCountHiddenSettledCommits(t *testing.T) {
	b, ctx, repo := archivedInventory(t, 8)
	records, _, err := b.Records(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range records {
		r.Worktree.Branch = "removed-delivery"
		if err := b.save(ctx, r, HashSecret("s"), "task.test"); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Store.MarkReclaim(ctx, store.Reclaim{Task: r.ID, Subject: ReclaimWorktree, Outcome: ReclaimRemoved, LastAt: time.Now()}, nil); err != nil {
			t.Fatal(err)
		}
	}
	trace := filepath.Join(t.TempDir(), "trace.jsonl")
	t.Setenv("GIT_TRACE2_EVENT", trace)
	inv, err := b.ReadInventory(ctx, repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Live)+len(inv.Unlanded)+len(inv.Droppable)+len(inv.Unreadable) != 0 {
		t.Fatalf("hidden tasks became actionable: %+v", inv)
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		var event struct {
			Event string   `json:"event"`
			Args  []string `json:"argv"`
		}
		if json.Unmarshal(line, &event) != nil || event.Event != "start" {
			continue
		}
		for _, arg := range event.Args {
			if arg == "rev-list" {
				t.Fatal("counted commits that cannot affect this inventory")
			}
		}
	}
}

func TestInventoryRetainsCommitBasedAdvice(t *testing.T) {
	for _, tc := range []struct {
		name            string
		landing         LandingState
		branch          string
		wantDo, wantWhy string
	}{
		{"empty merged disposal", LandingLanded, "main", DoDispose, WhyBranchEmpty},
		{"missing pending branch", LandingPending, "removed-delivery", DoLandOrAbandon, "this machine could not count what its delivery branch carries, and an unknown count is not permission"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, ctx, repo := archivedInventory(t, 1)
			records, _, err := b.Records(ctx)
			if err != nil {
				t.Fatal(err)
			}
			r := records[0]
			r.Landing = &Landing{State: tc.landing}
			r.Worktree.Branch = tc.branch
			if tc.landing == LandingLanded {
				r.Landing.Target = "main"
			}
			r.ChildTerminalID = "%77"
			r.ChildBackend = "tmux"
			if _, err := b.Store.MarkReclaim(ctx, store.Reclaim{Task: r.ID, Subject: ReclaimWorktree, Outcome: ReclaimRemoved, LastAt: time.Now()}, nil); err != nil {
				t.Fatal(err)
			}
			b.keepReading(reading{sessions: map[string]session.Session{"%1": {ID: "%1"}}, complete: true, sources: map[string]bool{"tmux": true}})
			b.ProcessCWDs = func(context.Context) ([]string, error) { return nil, nil }
			row := b.row(ctx, r, time.Now(), b.occupancySnapshot(ctx))
			if row.Do != tc.wantDo || row.Why != tc.wantWhy {
				t.Fatalf("do=%s why=%q want %s %q repo=%s", row.Do, row.Why, tc.wantDo, tc.wantWhy, repo)
			}
		})
	}
}
