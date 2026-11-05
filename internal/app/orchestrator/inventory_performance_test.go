package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// An archived task whose checkout was removed outside the broker has no
// clean-checkout receipt. It cannot offer disposal, regardless of Git refs.
func archivedInventory(t *testing.T, count int) (*Broker, context.Context, string) {
	t.Helper()
	b, ctx := newTestBroker(t)
	repo := gitRepo(t)
	base := gitIn(t, repo, "rev-parse", "HEAD")
	for i := 0; i < count; i++ {
		r := Record{Protocol: Protocol, ID: fmt.Sprintf("a9000000-0000-4000-8000-%012d", i), Assistant: "claude", Title: "Archived task", State: StateSuccess, CreatedAt: time.Now(), ProjectDir: repo, Repository: repo,
			Worktree: &Worktree{Repository: repo, Path: filepath.Join(b.Dir, "removed", fmt.Sprint(i)), Branch: "main", Base: base, Head: base},
			Landing:  &Landing{State: LandingLanded, Target: "main"},
		}
		if err := b.save(ctx, r, HashSecret("s"), "task.test"); err != nil {
			t.Fatal(err)
		}
	}
	return b, ctx, repo
}

func TestArchivedInventoryDoesNotRecheckGit(t *testing.T) {
	b, ctx, repo := archivedInventory(t, 32)
	trace := filepath.Join(t.TempDir(), "git-trace.jsonl")
	t.Setenv("GIT_TRACE2_EVENT", trace)
	inv, err := b.ReadInventory(ctx, repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Live)+len(inv.Unlanded)+len(inv.Droppable)+len(inv.Unreadable) != 0 {
		t.Fatalf("archived tasks became actionable: %+v", inv)
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	commands := 0
	for _, line := range bytes.Split(data, []byte("\n")) {
		var event struct {
			Event string `json:"event"`
		}
		if json.Unmarshal(line, &event) == nil && event.Event == "start" {
			commands++
		}
	}
	// The sole Git read resolves the repository. Retired checkouts with no
	// cleanup receipt cannot become disposal candidates by inspecting refs.
	if commands != 1 {
		t.Fatalf("archived inventory ran %d Git commands; want only repository resolution", commands)
	}
}

func TestArchivedInventoryTiming(t *testing.T) {
	if testing.Short() {
		t.Skip("timing experiment")
	}
	b, ctx, repo := archivedInventory(t, 128)
	for i := 0; i < 3; i++ {
		start := time.Now()
		inv, err := b.ReadInventory(ctx, repo, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(inv.Live)+len(inv.Unlanded)+len(inv.Droppable)+len(inv.Unreadable) != 0 {
			t.Fatal("archived inventory is actionable")
		}
		t.Logf("archived=128 sample=%d elapsed=%s", i, time.Since(start))
	}
}

// The fast path must not hide unresolved landings or a checkout that has
// reappeared since it was reclaimed. Git failures must withhold disposal.
func TestArchivedInventoryKeepsCleanupAndLandingEvidence(t *testing.T) {
	for _, tc := range []struct {
		name       string
		landing    LandingState
		receipt    string
		checkout   bool
		brokenGit  bool
		want       Visibility
		disposable bool
	}{
		{name: "settled without receipt", landing: LandingLanded, want: VisibilitySettled},
		{name: "preserved changes", landing: LandingLanded, receipt: ReclaimPreserved, want: VisibilitySettled},
		{name: "clean removal receipt", landing: LandingLanded, receipt: ReclaimRemoved, want: VisibilitySettled, disposable: true},
		{name: "pending without receipt", landing: LandingPending, want: VisibilityUnlanded},
		{name: "pending with clean receipt", landing: LandingPending, receipt: ReclaimRemoved, want: VisibilityUnlanded},
		{name: "legacy without landing", want: VisibilityUnlanded},
		{name: "checkout recreated dirty", landing: LandingLanded, receipt: ReclaimRemoved, checkout: true, want: VisibilitySettled},
		{name: "Git unavailable", landing: LandingLanded, receipt: ReclaimRemoved, brokenGit: true, want: VisibilitySettled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, ctx, _ := archivedInventory(t, 1)
			records, _, err := b.Records(ctx)
			if err != nil {
				t.Fatal(err)
			}
			r := records[0]
			if tc.landing == "" {
				r.Landing = nil
			} else {
				r.Landing.State = tc.landing
			}
			r.ChildTerminalID = "%77"
			r.ChildBackend = "tmux"
			b.keepReading(reading{sessions: map[string]session.Session{"%1": {ID: "%1"}}, sources: map[string]bool{"tmux": true}, complete: true})
			b.ProcessCWDs = func(context.Context) ([]string, error) { return []string{"/"}, nil }
			if tc.receipt != "" {
				if _, err := b.Store.MarkReclaim(ctx, store.Reclaim{Task: r.ID, Subject: ReclaimWorktree, Outcome: tc.receipt, LastAt: time.Now()}, nil); err != nil {
					t.Fatal(err)
				}
			}
			if tc.checkout {
				if err := os.MkdirAll(r.Worktree.Path, 0700); err != nil {
					t.Fatal(err)
				}
				gitIn(t, r.Worktree.Path, "init", "-q")
				if err := os.WriteFile(filepath.Join(r.Worktree.Path, "dirty.txt"), []byte("changes"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.brokenGit {
				b.Git.Binary = filepath.Join(t.TempDir(), "missing-git")
			}
			row := b.row(ctx, r, time.Now(), b.occupancySnapshot(ctx))
			if row.Section != tc.want || (row.Do == DoDispose) != tc.disposable {
				t.Fatalf("section=%s do=%s, want %s disposal=%v", row.Section, row.Do, tc.want, tc.disposable)
			}
			if tc.landing == LandingPending && row.Do != DoLandOrAbandon {
				t.Fatalf("pending landing lost its advice: %+v", row)
			}
		})
	}
}

func TestInventoryReadsProcessOccupancyOncePerRequest(t *testing.T) {
	b, ctx, repo := archivedInventory(t, 3)
	records, _, err := b.Records(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range records {
		r.ChildTerminalID = "%77"
		r.ChildBackend = "tmux"
		if err := b.save(ctx, r, HashSecret("s"), "task.test"); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Store.MarkReclaim(ctx, store.Reclaim{Task: r.ID, Subject: ReclaimWorktree, Outcome: ReclaimRemoved, LastAt: time.Now()}, nil); err != nil {
			t.Fatal(err)
		}
	}
	b.keepReading(reading{sessions: map[string]session.Session{"%1": {ID: "%1"}}, sources: map[string]bool{"tmux": true}, complete: true})
	calls := 0
	var failure error
	var cwds []string
	b.ProcessCWDs = func(context.Context) ([]string, error) { calls++; return cwds, failure }
	read := func(want int) {
		t.Helper()
		calls = 0
		inv, err := b.ReadInventory(ctx, repo, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(inv.Droppable) != want {
			t.Fatalf("disposal candidates=%d, want %d", len(inv.Droppable), want)
		}
		if calls != 1 {
			t.Fatalf("process-table reads=%d, want one per inventory request", calls)
		}
	}
	read(3)
	// A failure remains unknown for every row, and the next request retries
	// from fresh evidence rather than reusing the previous success or failure.
	failure = fmt.Errorf("process table unavailable")
	read(0)
	failure = nil
	cwds = []string{records[0].Worktree.Path}
	read(2)
	cwds = nil
	read(3)
}
