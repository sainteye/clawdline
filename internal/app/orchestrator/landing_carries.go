package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
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

// landingEquivalentNote is the note an incorporated landing the broker found
// by patch equivalence carries; %s is the target and the commits it found.
const landingEquivalentNote = "recorded by the broker: git found every commit of this delivery " +
	"patch-equivalent on %s (%s), and no path it changed is back as it was before it"

// detectEquivalent records `incorporated` for a delivery that reached target
// as other commits — cherry-picked, rebased or squashed one for one — with no
// carrier declaring it (result.json `carries`). Read on 2026-11-25: 3d6fde6c's
// three commits were on main by patch-id through a cherry-pick whose task
// declared nothing, and it read pending, "not yet on its target", for good.
//
// Git answers it, through the gate's own proof (proveEquivalent): every commit
// of the delivery has a patch-equivalent commit on target, and the target's
// tree still keeps at least one path the delivery changed and has none back as
// it was before. A delivery cherry-picked and then reverted is still found by
// git cherry upstream of the revert, so the trees are what refuse it. Any
// answer git cannot give leaves the landing pending.
//
// false with a nil error is "not equivalent, or not the broker's to say".
func (b *Broker) detectEquivalent(ctx context.Context, r Record, head, target string) (bool, error) {
	w := r.Worktree
	repo := w.Repository
	tip, err := b.Git.ResolveCommit(ctx, repo, "refs/heads/"+target)
	if err != nil {
		return false, errors.New("its target " + target + " could not be resolved")
	}
	missing, total, err := b.Git.PatchesIn(ctx, repo, tip, head, w.Base)
	if err != nil {
		return false, errors.New("git could not compare its commits with " + target + ": " + err.Error())
	}
	if total == 0 || missing > 0 {
		return false, nil
	}
	found, err := b.Git.UpstreamEquivalents(ctx, repo, tip, head, w.Base)
	if err != nil {
		return false, errors.New("git could not name the commits on " + target + " equivalent to its delivery: " + err.Error())
	}
	short := make([]string, 0, len(found))
	for _, c := range found {
		short = append(short, shortCommit(c))
	}
	note := truncate(fmt.Sprintf(landingEquivalentNote, target, strings.Join(short, ", ")), summaryLimit)
	_, err = b.land(ctx, r.ID, LandingRequest{State: string(LandingIncorporated), Target: target, Commit: tip,
		Note: note, Machine: true, equivalent: true})
	if err != nil {
		var ref Refusal
		if errors.As(err, &ref) {
			reason, _ := ref.Extra["reason"].(string)
			// Written meanwhile, or git showed it is not this delivery on
			// the target — backed out, or not every patch there — which is
			// an answer, not a failure: it stays pending. A proof git could
			// not finish is said, below.
			if ref.Code == "stale_write" || (ref.Code == "unverified_landing" &&
				(reason == UnverifiedReverted || reason == UnverifiedNotCarried || reason == UnverifiedAlreadyCarried)) {
				return false, nil
			}
		}
		return false, errors.New("the landing gate refused a patch-equivalent delivery: " + err.Error())
	}
	return true, nil
}

// proveEquivalent proves a delivery is on target as patch-equivalent commits
// rather than by ancestry, or says which step it failed. Every git answer
// that cannot be had refuses, as in proveDelivery.
//
//   - the delivery is non-empty, resolvable and not an ancestor of the commit
//     (otherwise ordinary `landed` is the word);
//   - every one of its commits has a patch-equivalent commit reachable from
//     the named commit (git cherry);
//   - in the named commit's tree and in the target's tree as it stands, at
//     least one path the delivery changed is kept and none is back as it was
//     before the delivery (git.DeliveryPresence). A path edited since reads
//     unknown, which does not refuse; a reverted one does, which is stricter
//     than revertedRefusal, because no carrier vouches for this one.
func (b *Broker) proveEquivalent(ctx context.Context, r Record, commit, target string) (landingProof, branchHead, error) {
	repo := landingRepository(r)
	if repo == "" || !b.Git.ValidBranchName(ctx, target) {
		return landingProof{}, branchHead{}, unverified(UnverifiedTarget,
			"The target must be a local branch of the task's repository.")
	}
	c, err := b.Git.ResolveCommit(ctx, repo, commit)
	if err != nil {
		return landingProof{}, branchHead{}, unverified(UnverifiedCommit,
			"The commit does not resolve in the task's repository.")
	}
	t, err := b.Git.ResolveCommit(ctx, repo, "refs/heads/"+target)
	if err != nil {
		return landingProof{}, branchHead{}, unverified(UnverifiedTarget,
			"The target branch does not resolve in the task's repository.")
	}
	if on, err := b.Git.IsAncestor(ctx, repo, c, t); err != nil || !on {
		return landingProof{}, branchHead{}, unverified(UnverifiedNotOn,
			"The commit must be contained by the named local target branch.")
	}
	if r.Worktree == nil || r.Worktree.Base == "" {
		return landingProof{}, branchHead{}, unverified(UnverifiedDelivery,
			"This task has no isolated delivery head and base to compare.")
	}
	head := b.deliveryHead(ctx, r.Worktree)
	h := head.commit
	if h == "" {
		h = r.Worktree.Head
	}
	if h == "" {
		return landingProof{}, head, unverified(UnverifiedDelivery,
			"The delivery branch is gone and no delivery head was recorded when the task settled.")
	}
	resolvedHead, err := b.Git.ResolveCommit(ctx, repo, h)
	if err != nil {
		return landingProof{}, head, unverified(UnverifiedDelivery,
			"The delivery head no longer resolves in the task's repository.")
	}
	base, err := b.Git.ResolveCommit(ctx, repo, r.Worktree.Base)
	if err != nil {
		return landingProof{}, head, unverified(UnverifiedBase,
			"The delivery's recorded base no longer resolves in the task's repository.")
	}
	if under, err := b.Git.IsAncestor(ctx, repo, resolvedHead, base); err != nil || under {
		return landingProof{}, head, unverified(UnverifiedNothing,
			"The delivery carries nothing past its recorded base.")
	}
	if carried, err := b.Git.IsAncestor(ctx, repo, resolvedHead, c); err != nil {
		return landingProof{}, head, unverified(UnverifiedDelivery,
			"Git could not compare the delivery with the commit.")
	} else if carried {
		return landingProof{}, head, unverified(UnverifiedAlreadyCarried,
			"The commit already contains the delivery commit by ancestry; record it as landed instead.")
	}
	missing, total, err := b.Git.PatchesIn(ctx, repo, c, resolvedHead, base)
	switch {
	case err != nil:
		return landingProof{}, head, unverified(UnverifiedDelivery,
			"Git could not compare the delivery's commits with the commit.")
	case total == 0:
		return landingProof{}, head, unverified(UnverifiedNothing,
			"The delivery has no commits to compare.")
	case missing > 0:
		return landingProof{}, head, unverified(UnverifiedNotCarried, fmt.Sprintf(
			"%d of the delivery's %d commits have no patch-equivalent commit in %s.", missing, total, shortCommit(c)))
	}
	trees := []string{c}
	if t != c {
		trees = append(trees, t)
	}
	for _, at := range trees {
		p, err := b.Git.DeliveryPresence(ctx, repo, base, resolvedHead, at)
		if err != nil {
			return landingProof{}, head, unverified(UnverifiedDelivery,
				"Git could not compare the delivery "+shortCommit(resolvedHead)+" with the tree of "+shortCommit(at)+".")
		}
		if p.Reverted > 0 || p.Kept == 0 {
			return landingProof{}, head, refuseRawWith(http.StatusConflict, "unverified_landing", fmt.Sprintf(
				"The tree of %s does not keep the delivery %s: %d of the paths it changed are back as they were "+
					"before it and %d still hold its change. Equivalent patches upstream of a revert are not a landing.",
				shortCommit(at), shortCommit(resolvedHead), p.Reverted, p.Kept),
				map[string]any{"reason": UnverifiedReverted, "delivery_head": resolvedHead, "tree": at,
					"paths_reverted": p.Reverted, "paths_kept": p.Kept, "paths_unknown": p.Unknown})
		}
	}
	return landingProof{commit: c, repo: repo, targetCommit: t, deliveryHead: resolvedHead, base: base}, head, nil
}
