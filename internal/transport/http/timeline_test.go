package http

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"

	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
)

func timelineAsk(project string) timelineQuery {
	return timelineQuery{project: project, environment: "production", upcoming: true, limit: 500, page: 40}
}

// The whole point of the feature: a delivery that landed is in Git, and a
// delivery that did not is not deployed. Neither is ever available, because
// nothing on this machine produces that evidence.
func TestATimelineSaysInGitAndNotDeployedAndNeverAvailable(t *testing.T) {
	at := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	records := []orchestrator.Record{
		{ID: "landed", Title: "The one that landed", Repository: "/repo", CreatedAt: at, FinishedAt: at,
			Result:  &taskdir.Result{Status: "success", Summary: "did the thing"},
			Landing: &orchestrator.Landing{State: orchestrator.LandingLanded, Commit: "0123456789abcdef", At: at.Add(time.Hour), Repo: "/repo"}},
		{ID: "delivered", Title: "The one that has not", Repository: "/repo", CreatedAt: at, FinishedAt: at,
			Result: &taskdir.Result{Status: "success"}},
		{ID: "running", Title: "Still going", Repository: "/repo", CreatedAt: at},
	}
	snap := buildTimeline(records, timelineAsk("/repo"))
	if len(snap.Entries) != 2 {
		t.Fatalf("a task that has not delivered is not a thing that happened: %d entries", len(snap.Entries))
	}
	byID := map[string]contract.TimelineEntry{}
	for _, e := range snap.Entries {
		byID[e.ID] = e
		if e.Projection.AvailableTargets != 0 {
			t.Errorf("%s: nothing proves availability here", e.ID)
		}
	}
	if byID["landed"].Projection.Status != contract.TimelineStatusLandedToGit {
		t.Errorf("landed: %v", byID["landed"].Projection.Status)
	}
	if byID["delivered"].Projection.Status != contract.TimelineStatusUpcoming {
		t.Errorf("delivered but not landed: %v", byID["delivered"].Projection.Status)
	}
	if revs := byID["landed"].SourceRevisions; len(revs) != 1 || revs[0].ShortCommit != "01234567" {
		t.Errorf("the landing's commit: %+v", revs)
	}
	// A path is not a forge, so no link is invented for one.
	if byID["landed"].SourceRevisions[0].GithubUrl != "" {
		t.Errorf("a GitHub link built from a path: %q", byID["landed"].SourceRevisions[0].GithubUrl)
	}
	if snap.Viewer.CanManage {
		t.Error("a projection has nothing to manage")
	}
}

// A Project nothing has delivered in has a counter of nought, not a number
// from the year 1.
func TestAnEmptyTimelineHasNoRevisionRatherThanTheZeroTime(t *testing.T) {
	snap := buildTimeline(nil, timelineAsk("/repo"))
	if snap.Revision != 0 {
		t.Errorf("revision %d", snap.Revision)
	}
	if snap.Entries == nil {
		t.Error("an empty timeline is an empty list, never a missing one")
	}
	if snap.Capacity.EntryLimit != 500 || snap.Capacity.EntryCount != 0 {
		t.Errorf("the bound is on the wire: %+v", snap.Capacity)
	}
}

// At the bound the oldest is left out and the answer says so. Nothing is
// deleted: the entries are re-derived from the broker's records every read.
func TestATimelineAtItsBoundSaysSoRatherThanGoingQuiet(t *testing.T) {
	at := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	records := []orchestrator.Record{}
	for i := 0; i < 5; i++ {
		records = append(records, orchestrator.Record{
			ID: string(rune('a' + i)), Repository: "/repo",
			CreatedAt: at.Add(time.Duration(i) * time.Minute), FinishedAt: at.Add(time.Duration(i) * time.Minute),
			Result: &taskdir.Result{Status: "success"},
		})
	}
	ask := timelineAsk("/repo")
	ask.limit = 3
	snap := buildTimeline(records, ask)
	if snap.Capacity.EntryCount != 3 {
		t.Errorf("held %d", snap.Capacity.EntryCount)
	}
	if len(snap.Checkpoints) != 1 || snap.Checkpoints[0].HistoryStatus != contract.TimelineHistoryStatusCapacity {
		t.Errorf("the checkpoint: %+v", snap.Checkpoints)
	}
	// Newest first, so what is left out is the oldest.
	if snap.Entries[0].ID != "e" {
		t.Errorf("newest first: %s", snap.Entries[0].ID)
	}
}

// The keyset cursor carries the page, and a page cannot skip an entry that
// arrived while the reader was on the previous one.
func TestATimelinePageIsContinuedByItsCursor(t *testing.T) {
	at := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	records := []orchestrator.Record{}
	for i := 0; i < 5; i++ {
		records = append(records, orchestrator.Record{
			ID: string(rune('a' + i)), Repository: "/repo",
			CreatedAt: at.Add(time.Duration(i) * time.Minute), FinishedAt: at.Add(time.Duration(i) * time.Minute),
			Result: &taskdir.Result{Status: "success"},
		})
	}
	ask := timelineAsk("/repo")
	ask.page = 2
	first := buildTimeline(records, ask)
	if len(first.Entries) != 2 || first.NextCursor == "" {
		t.Fatalf("first page: %d entries, cursor %q", len(first.Entries), first.NextCursor)
	}
	ask.cursor = first.NextCursor
	second := buildTimeline(records, ask)
	if len(second.Entries) != 2 || second.Entries[0].ID != "c" {
		t.Fatalf("second page: %v", func() []string {
			out := []string{}
			for _, e := range second.Entries {
				out = append(out, e.ID)
			}
			return out
		}())
	}
}

// A task whose whole declared write set is documentation or tooling is an
// operation; anything that touches the product is a Feature, which is the
// reading that overstates nothing.
func TestATimelineCallsADocsOnlyDeliveryAnOperation(t *testing.T) {
	at := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	records := []orchestrator.Record{
		{ID: "docs", Repository: "/repo", CreatedAt: at, FinishedAt: at, Claims: []string{"docs/limits.md"},
			Result: &taskdir.Result{Status: "success"}},
		{ID: "code", Repository: "/repo", CreatedAt: at.Add(time.Minute), FinishedAt: at.Add(time.Minute),
			Claims: []string{"docs/limits.md", "internal/app"}, Result: &taskdir.Result{Status: "success"}},
	}
	snap := buildTimeline(records, timelineAsk("/repo"))
	got := map[string]contract.TimelineCategory{}
	for _, e := range snap.Entries {
		got[e.ID] = e.PrimaryCategory
	}
	if got["docs"] != contract.TimelineCategoryOperation || got["code"] != contract.TimelineCategoryFeature {
		t.Errorf("categories: %v", got)
	}
}

// With `upcoming` off the question is narrower — what actually reached the
// target — and only landings answer it.
func TestATimelineWithoutUpcomingShowsOnlyWhatLanded(t *testing.T) {
	at := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	records := []orchestrator.Record{
		{ID: "landed", Repository: "/repo", CreatedAt: at, FinishedAt: at,
			Result:  &taskdir.Result{Status: "success"},
			Landing: &orchestrator.Landing{State: orchestrator.LandingLanded, Commit: "abcdef1234", At: at}},
		{ID: "delivered", Repository: "/repo", CreatedAt: at, FinishedAt: at,
			Result: &taskdir.Result{Status: "success"}},
	}
	ask := timelineAsk("/repo")
	ask.upcoming = false
	snap := buildTimeline(records, ask)
	if len(snap.Entries) != 1 || snap.Entries[0].ID != "landed" {
		t.Errorf("entries: %+v", snap.Entries)
	}
}

// The Board hands this page `project-<digest>`, a link carries the path and a
// person types the directory's name. All three name the same Project, and a
// route that knew only one answered the Board's own tab with an empty list —
// "nothing was delivered here", said by accident.
func TestATimelineKnowsAProjectByAllThreeOfItsSpellings(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	records := []orchestrator.Record{{
		ID: "one", Repository: repo, CreatedAt: at, FinishedAt: at,
		Result: &taskdir.Result{Status: "success"},
	}}
	canonical, ok := projects.CanonicalProjectKey(repo)
	if !ok {
		t.Fatal("the temporary repository has no canonical key")
	}
	for _, spelling := range []string{repo, filepath.Base(canonical), projects.ProjectID(canonical)} {
		snap := buildTimeline(records, timelineAsk(spelling))
		if len(snap.Entries) != 1 {
			t.Errorf("%q: %d entries", spelling, len(snap.Entries))
		}
		if snap.Project.Label != filepath.Base(canonical) {
			t.Errorf("%q: a heading that reads as a digest: %q", spelling, snap.Project.Label)
		}
	}
}
