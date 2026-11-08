package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

func TestInflightDoesNotInspectSettledCleanup(t *testing.T) {
	b, ctx, repo := archivedInventory(t, 8)
	records, _, err := b.Records(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range records {
		if _, err := b.Store.MarkReclaim(ctx, store.Reclaim{Task: r.ID, Subject: ReclaimWorktree, Outcome: ReclaimRemoved, LastAt: time.Now()}, nil); err != nil {
			t.Fatal(err)
		}
	}
	trace := filepath.Join(t.TempDir(), "trace.jsonl")
	t.Setenv("GIT_TRACE2_EVENT", trace)
	rows, err := b.Inflight(ctx, repo, "")
	if err != nil || len(rows) != 0 {
		t.Fatalf("inflight=%+v err=%v", rows, err)
	}
	data, err := os.ReadFile(trace)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		var event struct {
			Event string `json:"event"`
		}
		if json.Unmarshal(line, &event) == nil && event.Event == "start" {
			t.Fatal("inflight inspected Git for settled cleanup that it never returns")
		}
	}
}

func TestInflightMatchesFullProjection(t *testing.T) {
	b, ctx, repo := archivedInventory(t, 1)
	records, _, err := b.Records(ctx)
	if err != nil {
		t.Fatal(err)
	}
	template := records[0]
	gitIn(t, repo, "checkout", "-q", "-b", "pending")
	gitIn(t, repo, "commit", "-q", "--allow-empty", "-m", "Outstanding fixture")
	gitIn(t, repo, "checkout", "-q", "main")
	cases := []struct {
		state    State
		landing  LandingState
		worktree bool
	}{
		{StateBriefed, LandingLanded, true}, // A live task stays live even with an old receipt.
		{StateSuccess, LandingLanded, true}, {StateSuccess, LandingIncorporated, true},
		{StateSuccess, LandingAbandoned, true}, {StateSuccess, LandingNothingToLand, true},
		{StateSuccess, LandingPending, true}, {StateFailure, "", true},
		{StateSuccess, LandingState("future-state"), true},
		{StateSuccess, LandingPending, false}, {StateBriefed, "", false},
	}
	for i, tc := range cases {
		r := template
		r.ID = fmt.Sprintf("a9100000-0000-4000-8000-%012d", i)
		r.State = tc.state
		if tc.landing == "" {
			r.Landing = nil
		} else {
			r.Landing = &Landing{State: tc.landing, Target: "main"}
		}
		if tc.worktree {
			w := *template.Worktree
			w.Branch = "pending"
			r.Worktree = &w
		} else {
			r.Worktree = nil
		}
		if err := b.save(ctx, r, HashSecret("s"), "task.test"); err != nil {
			t.Fatal(err)
		}
	}
	// Keep unreadable work visible too; the projection cannot infer its state.
	broken := template
	broken.ID = "a9200000-0000-4000-8000-000000000001"
	if err := b.save(ctx, broken, HashSecret("s"), "task.test"); err != nil {
		t.Fatal(err)
	}
	corrupt(t, b.Dir, broken.ID)
	for _, exclude := range []string{"", "a9100000-0000-4000-8000-000000000005", broken.ID} {
		all, err := b.rows(ctx, repo)
		if err != nil {
			t.Fatal(err)
		}
		want := []InventoryRow{}
		for _, row := range all {
			if row.Task == exclude {
				continue
			}
			switch row.Section {
			case VisibilityLive, VisibilityUnreadable:
				want = append(want, row)
			case VisibilityUnlanded:
				row.Section = VisibilityUnmerged
				want = append(want, row)
			}
		}
		got, err := b.Inflight(ctx, repo, exclude)
		if err != nil {
			t.Fatal(err)
		}
		for i := range want {
			want[i].Age = 0
		}
		for i := range got {
			got[i].Age = 0
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("exclude=%q inflight=%+v want=%+v", exclude, got, want)
		}
	}
	// A new write must be visible on the next query; this is no result cache.
	r := template
	r.ID = "a9300000-0000-4000-8000-000000000001"
	r.State = StateBriefed
	if err := b.save(ctx, r, HashSecret("s"), "task.test"); err != nil {
		t.Fatal(err)
	}
	rows, err := b.Inflight(context.Background(), repo, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Task == r.ID {
			return
		}
	}
	t.Fatal("newly outstanding work was hidden")
}
