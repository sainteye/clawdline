package orchestrator

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

// registerGateGoCache records the only isolated GOCACHE the broker itself
// creates. Ordinary task, Root Assignment and Agent launches inherit Go's
// environment and do not create a separate broker-owned cache.
func (b *Broker) registerGateGoCache(ctx context.Context, r Record) error {
	if r.Gate == nil || filepath.Clean(r.Dir) != filepath.Clean(b.Tasks.Path(r.ID)) ||
		!ownedPath(b.Tasks.Dir, r.Dir, r.ID) {
		return errors.New("the task does not own a gate scratch directory")
	}
	stored, _, err := b.Record(ctx, r.ID)
	if err != nil || stored.Gate == nil || stored.Dir != r.Dir {
		return errors.New("the gate cache owner is not a durable task")
	}
	path := b.gateScratch(r.ID).Cache
	if err := validateGateGoCache(r.Dir, path); err != nil {
		return err
	}
	return b.Store.RegisterGoCache(ctx, store.GoCache{
		Path: path, OwnerKind: "task", OwnerID: r.ID,
		Purpose: "verification_gate_build_cache", Rebuildable: true, CreatedAt: b.now(),
	})
}

// validateGateGoCache requires the precise directory the gate launcher names.
// The spelling and resolved path must both remain under the task's work/.
// This excludes a shared GOCACHE/GOPATH and a symlink into one even if the
// symlink has a task-like name.
func validateGateGoCache(taskDir, path string) error {
	work := filepath.Join(taskDir, "work")
	if path != filepath.Join(work, "cache") || !ownedPath(taskDir, work, "work") ||
		!ownedPath(work, path, "cache") {
		return errors.New("Go cache is not the exact broker-owned gate cache")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Go cache is not a directory owned by this task")
	}
	return nil
}

// recordTaskGoCacheDecision follows the work/ decision. The cache has no
// independent cleanup clock: it is a child of that proven task scratch.
func (b *Broker) recordTaskGoCacheDecision(ctx context.Context, r Record, d ReclaimDecision, at time.Time) {
	if r.Gate == nil || d.Reason == WhyLive {
		return
	}
	caches, err := b.Store.GoCachesByOwner(ctx, "task", r.ID)
	if err != nil {
		log.Printf("orchestrator: cannot read Go cache registration for %s: %v", r.ID, err)
		return
	}
	for _, c := range caches {
		outcome, reason := d.Outcome, d.Reason
		if d.Subject == "" {
			if c.LastOutcome == ReclaimRemoved || c.LastOutcome == "unknown" {
				continue
			}
			// A missing scratch after a recorded removal intent is recovery;
			// otherwise its disappearance has no proven actor.
			if c.LastOutcome == reclaimRemoving {
				outcome, reason = ReclaimRemoved, WhyWorkScratch
			} else {
				outcome, reason = "unknown", "scratch_absent"
			}
		}
		if c.LastOutcome == outcome && c.LastReason == reason {
			continue
		}
		if err := b.Store.MarkGoCacheCleanup(ctx, c.Path, "task", r.ID, outcome, reason, at); err != nil {
			log.Printf("orchestrator: cannot record Go cache cleanup for %s: %v", r.ID, err)
		}
	}
}
