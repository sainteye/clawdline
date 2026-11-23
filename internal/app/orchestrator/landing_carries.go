package orchestrator

import (
	"context"
	"fmt"
)

// incorporateCarried settles the deliveries a just-landed task declared it
// carried (result.json `carries`). Each one still owing a pending landing is
// recorded incorporated, with the carrier's landing commit, only when git
// cherry finds every one of its commits patch-equivalent in that commit, and
// only through land(), so proveIncorporated's checks (same repository and
// target, exact carrier commit, a delivery that is not an ancestor) still
// gate it. Otherwise it stays pending and its note says why. A failure here
// never undoes the carrier's own landing.
func (b *Broker) incorporateCarried(ctx context.Context, carrier Record) {
	if carrier.Landing == nil || carrier.Landing.State != LandingLanded || carrier.Landing.Commit == "" || carrier.Result == nil {
		return
	}
	for _, id := range carrier.Result.Carries {
		if id == carrier.ID || !IsTaskID(id) {
			continue
		}
		r, _, err := b.Record(ctx, id)
		if err != nil || !r.State.Terminal() || r.Landing == nil || r.Landing.State != LandingPending {
			continue
		}
		why := b.carriedPatchesRefusal(ctx, r, carrier)
		if why == "" {
			_, err = b.land(ctx, id, LandingRequest{State: string(LandingIncorporated), Target: carrier.Landing.Target,
				Commit: carrier.Landing.Commit, CarrierTask: carrier.ID, Machine: true,
				Note: fmt.Sprintf("Declared as carried by task %s; git cherry found every commit of this delivery patch-equivalent in %s.", carrier.ID, shortCommit(carrier.Landing.Commit))})
			if err == nil {
				continue
			}
			why = "the broker refused incorporated: " + err.Error()
		}
		// Best effort: the reason is advisory, and a failed write leaves
		// the landing pending exactly as it was.
		_, _ = b.land(ctx, id, LandingRequest{State: string(LandingPending), Target: r.Landing.Target, Machine: true,
			Note: fmt.Sprintf("Declared as carried by task %s, not recorded incorporated: %s.", carrier.ID, why)})
	}
}

// carriedPatchesRefusal answers why r's commits cannot be shown to be in the
// carrier's landing commit, or "" when every one is there by patch-id.
func (b *Broker) carriedPatchesRefusal(ctx context.Context, r, carrier Record) string {
	if r.Worktree == nil || r.Worktree.Base == "" {
		return "it has no isolated delivery to compare"
	}
	head := b.deliveryHead(ctx, r.Worktree).commit
	if head == "" {
		head = r.Worktree.Head
	}
	if head == "" {
		return "its delivery head is unknown"
	}
	missing, total, err := b.Git.PatchesIn(ctx, landingRepository(r), carrier.Landing.Commit, head, r.Worktree.Base)
	switch {
	case err != nil:
		return "git could not compare its commits with the carrier's landing commit"
	case total == 0:
		return "it has no commits to compare"
	case missing > 0:
		return fmt.Sprintf("%d of its %d commits have no patch-equivalent commit in %s", missing, total, shortCommit(carrier.Landing.Commit))
	}
	return ""
}
