package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

// artifactTask is a finished child whose artifacts/ holds one file.
func artifactTask(t *testing.T, change func(*Record)) (*Broker, Record, string) {
	t.Helper()
	b, ctx, r, _ := cacheTask(t, "%child", false)
	r.Gate = nil
	if change != nil {
		change(&r)
	}
	if _, err := b.mutate(ctx, r.ID, "test.fixture", func(x *Record) error { *x = r; return nil }); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(r.Dir, "artifacts", "evidence.txt")
	if err := os.WriteFile(file, []byte("kept for 30 days"), 0o600); err != nil {
		t.Fatal(err)
	}
	return b, r, file
}

func sweepArtifactsAt(t *testing.T, b *Broker, at time.Time) ReclaimDecision {
	t.Helper()
	b.Clock = func() time.Time { return at }
	rep, err := b.Reclaim(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range rep.Decisions {
		if d.Subject == ReclaimTaskArtifacts {
			return d
		}
	}
	return ReclaimDecision{}
}

func TestSettledTaskArtifactsGoAfterThirtyDaysAndTheRecordSaysSo(t *testing.T) {
	for _, state := range []State{StateSuccess, StateFailure, StateCancelled, StateSpawnFailed} {
		t.Run(string(state), func(t *testing.T) {
			b, r, file := artifactTask(t, func(r *Record) { r.State = state })
			if d := sweepArtifactsAt(t, b, r.FinishedAt.Add(29*24*time.Hour)); d.Subject != "" {
				t.Fatalf("29 days: a decision was made about young artifacts: %+v", d)
			}
			if _, err := os.Stat(file); err != nil {
				t.Fatal("artifacts removed at 29 days", err)
			}
			if _, ok := b.ArtifactsExpired(t.Context(), r.ID); ok {
				t.Fatal("29 days: recorded as expired")
			}
			d := sweepArtifactsAt(t, b, r.FinishedAt.Add(31*24*time.Hour))
			if d.Outcome != ReclaimRemoved || d.Reason != WhyArtifactRetention || d.Bytes <= 0 {
				t.Fatalf("31 days: %+v", d)
			}
			if _, err := os.Stat(filepath.Join(r.Dir, "artifacts")); !os.IsNotExist(err) {
				t.Fatal("expired artifacts survived", err)
			}
			for _, kept := range []string{"result.json"} {
				if _, err := os.Stat(filepath.Join(r.Dir, kept)); err != nil {
					t.Fatalf("%s went with the artifacts: %v", kept, err)
				}
			}
			row, ok := b.ArtifactsExpired(t.Context(), r.ID)
			if !ok || row.Outcome != ReclaimRemoved || row.Reason != WhyArtifactRetention {
				t.Fatalf("the removal is not recorded as an expiry: %+v %v", row, ok)
			}
			current, _, err := b.Record(t.Context(), r.ID)
			if err != nil || current.Result == nil || current.Result.Summary != "Saved fixture completion" {
				t.Fatalf("summary lost: %+v %v", current.Result, err)
			}
			// A later sweep finds nothing to decide and leaves the record.
			if d := sweepArtifactsAt(t, b, r.FinishedAt.Add(40*24*time.Hour)); d.Subject != "" {
				t.Fatalf("a removed directory was decided about again: %+v", d)
			}
			if _, ok := b.ArtifactsExpired(t.Context(), r.ID); !ok {
				t.Fatal("expiry record lost by a later sweep")
			}
		})
	}
}

func TestTaskArtifactsStayWhenAnyAnswerIsMissing(t *testing.T) {
	cases := map[string]struct {
		change func(*Record)
		setup  func(*testing.T, *Broker, Record)
		why    string
	}{
		"landing pending": {change: func(r *Record) {
			r.Worktree = &Worktree{Path: filepath.Join(t.TempDir(), "gone")}
			r.Landing = &Landing{State: LandingPending}
		}, why: WhyLandingUnsettled},
		"branch without a landing record": {change: func(r *Record) {
			r.Worktree = &Worktree{Path: filepath.Join(t.TempDir(), "gone")}
		}, why: WhyLandingUnsettled},
		"end time unknown": {change: func(r *Record) { r.FinishedAt = time.Time{} }, why: WhyEndUnknown},
		"timeout":          {change: func(r *Record) { r.State = StateTimeout }, why: WhyLive},
		"pinned": {setup: func(t *testing.T, b *Broker, r Record) {
			id := uuid.NewString()
			if err := b.Store.RegisterTaskResource(t.Context(), taskresource.Resource{ID: id, Generation: uuid.NewString(),
				Owner: taskresource.Owner{Kind: "task", ID: r.ID, Project: t.TempDir()}, Class: taskresource.Artifact,
				RelativePath: id, Identity: "synthetic:pinned", CreatedAt: time.Now().Unix(), Pinned: true}); err != nil {
				t.Fatal(err)
			}
		}, why: WhyPinned},
		"being read": {setup: func(t *testing.T, b *Broker, r Record) {
			b.ProcessUses = func(_ context.Context, path string) string {
				if filepath.Base(path) == "artifacts" {
					return "present"
				}
				return "absent"
			}
		}, why: WhyOwnerPresent},
		"readers unknown": {setup: func(t *testing.T, b *Broker, r Record) {
			b.ProcessUses = func(context.Context, string) string { return "unknown" }
		}, why: WhyOwnerUnknown},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b, r, file := artifactTask(t, c.change)
			if c.setup != nil {
				c.setup(t, b, r)
			}
			at := time.Now().Add(60 * 24 * time.Hour)
			d := sweepArtifactsAt(t, b, at)
			if c.why == WhyLive {
				if d.Outcome != ReclaimKept || d.Reason != WhyLive {
					t.Fatalf("decision %+v, want kept %s", d, c.why)
				}
			} else if d.Outcome != ReclaimKept || d.Reason != c.why {
				t.Fatalf("decision %+v, want kept %s", d, c.why)
			}
			if _, err := os.Stat(file); err != nil {
				t.Fatal("artifacts removed without every answer", err)
			}
			if _, ok := b.ArtifactsExpired(t.Context(), r.ID); ok {
				t.Fatal("recorded as expired while kept")
			}
		})
	}
}
