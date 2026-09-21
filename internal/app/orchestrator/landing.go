package orchestrator

import (
	"context"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/sainteye/clawdline/internal/domain/work"
)

// Landing has one record, and it proves what it says (W3: D17, D18, D19, D51,
// broker-design #35).
//
// The first version of this broker proved `landed` by asking one question —
// is the reported commit an ancestor of the target — and nothing tied the
// commit to the task. The branch's own base answers yes to that question for
// every task ever cut from main, and `Worktree.Head` was written once, at
// creation, as the base, so the inventory showed the base as the delivery
// too. What is proved now is that **this task's work** reached the target:
//
//   - an isolated task's delivery is its branch head. The landed commit must
//     be on the target and must carry that head, and the head must be past the
//     base the branch was cut from;
//   - a task that wrote the shared checkout has no branch of its own, so the
//     line is where the repository stood when it was admitted: the landed
//     commit must be on the target and must not already have been under that
//     line.
//
// Either way a commit that was there before the dispatch — the base above all
// — is refused by name (D51). Arm 2, the write-set containment proof, is not
// built (D17): a shared-checkout task is landed on the commit its root names,
// or recorded as `nothing_to_land`.

// landingProof is the durable Git evidence behind landed or incorporated.
type landingProof struct {
	commit       string
	repo         string
	targetCommit string
	deliveryHead string
	base         string
}

// branchHead is a delivery branch's head as git answered it. asked says the
// question was put; commit is empty when the branch is gone or could not be
// read, and known separates those two.
type branchHead struct {
	branch string
	commit string
	asked  bool
	known  bool
}

// deliveryHead asks git what an isolated task's branch names now.
func (b *Broker) deliveryHead(ctx context.Context, w *Worktree) branchHead {
	out := branchHead{branch: w.Branch, asked: true}
	exists, known := b.Git.BranchExists(ctx, w.Repository, w.Branch)
	if !known {
		return out
	}
	if !exists {
		out.known = true
		return out
	}
	commit, err := b.Git.ResolveCommit(ctx, w.Repository, "refs/heads/"+w.Branch)
	if err != nil {
		return out
	}
	out.commit, out.known = commit, true
	return out
}

// settlement is what a task is settled with: the delivery head, and what the
// branch carried past its base when it was read. Both are asked of git before
// the settling write takes the write right (D08), and only for a live
// isolated task, since a terminal one is not settled again.
func (b *Broker) settlement(ctx context.Context, id string) (branchHead, LandingSettlement) {
	r, _, err := b.Record(ctx, id)
	if err != nil || r.Worktree == nil || r.State.Terminal() {
		return branchHead{}, ""
	}
	return b.deliveryHead(ctx, r.Worktree), b.branchSettlement(ctx, r.Worktree)
}

// branchSettlement asks git what a delivery branch carries past the commit it
// was cut from. It is the same question `proveDelivery` asks a branch hours
// later, put at the one moment the answer can still be acted on.
func (b *Broker) branchSettlement(ctx context.Context, w *Worktree) LandingSettlement {
	commits, known := b.Git.Commits(ctx, w.Repository, w.Base, w.Branch)
	switch {
	case !known:
		return SettlementUnreadable
	case commits == 0:
		return SettlementEmpty
	}
	return SettlementCarried
}

// Unverifiable reports whether a pending landing is one this ledger cannot
// check. A landing is proved by asking git whether the delivery is on the
// branch the record names as its target, so a record that names no target has
// nothing to ask about: the row is owed, and whether the work is already on
// master is a question nobody here can put. A delivery branch git could not
// count when the task ended is the same kind of nothing.
//
// It is not "the work has not landed". That is the sentence this ledger has
// been saying about such rows, and it is the one thing the reading does not
// show. Everything that draws a pending landing reads this to choose between
// "owed" and "owed, and nobody can say whether it still is".
func Unverifiable(l *Landing) bool {
	if l == nil || l.State != LandingPending {
		return false
	}
	return l.Target == "" || l.Settlement == SettlementUnreadable
}

// settlementNote is the sentence a pending landing opens with: what the
// branch held, rather than the one thing every pending landing already says.
func settlementNote(s LandingSettlement) string {
	switch s {
	case SettlementEmpty:
		return "nothing was committed on its delivery branch when it ended"
	case SettlementCarried:
		return "delivered on its branch, not yet on its target"
	case SettlementUnreadable:
		return "its delivery branch could not be read when it ended"
	}
	return "not yet on its target"
}

// deliveryEvidence is the sentence that says what a task's own checkout shows
// it wrote. Empty means it shows it wrote nothing, and that is the one answer
// `nothing_to_land` may rest on.
//
// **Two facts, asked of two different things.** What the branch carries is
// asked of the repository, which is still there; what the checkout holds
// uncommitted is asked of a directory the sweep takes within the day. So one
// of them is routinely unknown while the other is known exactly, and a
// sentence that reports either unknown as "no commit count" sends a reader to
// look at something that has no problem. Measured on 2026-09-20: nineteen
// unlanded rows all read "this machine has no commit count for its checkout",
// while every one of their branches was still there and every count was
// known — what was missing was the worktree (work-system-review §3.2, G2).
//
// A positive answer from either fact beats an unknown from the other: a branch
// that carries three commits has something to land whether or not its checkout
// can be read.
func deliveryEvidence(commits int, commitsKnown, dirty, dirtyKnown bool, kept string) string {
	if commitsKnown && commits > 0 {
		return "its branch carries " + strconv.Itoa(commits) + " commit(s)"
	}
	if dirtyKnown && dirty {
		if kept != "" {
			return "its checkout had uncommitted changes, kept on the branch " + kept + " when it was reclaimed"
		}
		return "its checkout has uncommitted changes"
	}
	if !commitsKnown {
		return "this machine could not count what its delivery branch carries, and an unknown count is not permission"
	}
	if !dirtyKnown {
		return "its branch carries nothing past its base, and this machine could not read its checkout to say " +
			"whether anything is uncommitted there"
	}
	return ""
}

// unverified is the refusal every failed proof gives. The code is the one a
// caller already matches on (cutover A5); `reason` says which step refused,
// because "not on the target" and "that commit was there before this task"
// send a person to different places.
func unverified(reason, message string) Refusal {
	return refuseWith(http.StatusConflict, "unverified_landing", message, map[string]any{"reason": reason})
}

// The reasons a landing is not proved.
const (
	UnverifiedCommit     = "commit_unresolved"
	UnverifiedTarget     = "target_unresolved"
	UnverifiedNotOn      = "not_on_target"
	UnverifiedBase       = "base_unknown"
	UnverifiedPredates   = "predates_dispatch"
	UnverifiedDelivery   = "delivery_unknown"
	UnverifiedNothing    = "nothing_delivered"
	UnverifiedNotCarried = "not_the_delivery"

	UnverifiedCarrierRequired = "carrier_required"
	UnverifiedCarrierSelf     = "carrier_is_delivery"
	UnverifiedCarrierMissing  = "carrier_unresolved"
	UnverifiedCarrierLanded   = "carrier_not_landed"
	UnverifiedCarrierRepo     = "carrier_repository_mismatch"
	UnverifiedCarrierTarget   = "carrier_target_mismatch"
	UnverifiedCarrierCommit   = "carrier_commit_mismatch"
	UnverifiedAlreadyCarried  = "delivery_is_ancestor"
)

// proveDelivery proves a landed commit is this task's work on the target, or
// says which step it failed. head is the delivery branch as the proof read it,
// for the landing write to record (G17).
//
// Every git answer that cannot be had refuses: an unknown is not a proof.
func (b *Broker) proveDelivery(ctx context.Context, r Record, commit, target string) (landingProof, branchHead, error) {
	repo := r.Repository
	if r.Worktree != nil && r.Worktree.Repository != "" {
		repo = r.Worktree.Repository
	}
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
			"The commit must resolve in the task repository and be contained by the named local target branch.")
	}

	base := r.DispatchBase
	if r.Worktree != nil {
		base = r.Worktree.Base
	}
	if base == "" {
		return landingProof{}, branchHead{}, unverified(UnverifiedBase,
			"This task has no recorded base — it was stored before the broker kept one, or its repository "+
				"could not be read when it was dispatched — so nothing tells its work apart from what was "+
				"already there. It cannot be proved landed; record it abandoned, or nothing_to_land.")
	}
	// The base itself, or anything under it, was in the repository before this
	// task was admitted, and landing it proves nothing about this task (D51).
	if under, err := b.Git.IsAncestor(ctx, repo, c, base); err != nil || under {
		return landingProof{}, branchHead{}, refuseWith(http.StatusConflict, "unverified_landing",
			"That commit was already in the repository when this task was dispatched (base "+shortCommit(base)+
				"), so it is not this task's work. Name the commit that carries the delivery onto the target.",
			map[string]any{"reason": UnverifiedPredates, "base": base})
	}
	proof := landingProof{commit: c, repo: repo, targetCommit: t, base: base}
	if r.Worktree == nil {
		return proof, branchHead{}, nil
	}

	// The delivery is the branch: what it holds now if git can say, and
	// otherwise what it held when the task settled. A root that commits a
	// child's work onto the branch after it settled has added to the
	// delivery, and the proof follows the branch, not the moment.
	head := b.deliveryHead(ctx, r.Worktree)
	h := head.commit
	if h == "" {
		h = r.Worktree.Head
	}
	if h == "" {
		return landingProof{}, head, unverified(UnverifiedDelivery,
			"The delivery branch "+r.Worktree.Branch+" could not be read and no head was recorded when the "+
				"task settled, so there is no delivery to prove.")
	}
	if under, err := b.Git.IsAncestor(ctx, repo, h, base); err != nil || under {
		return landingProof{}, head, refuseWith(http.StatusConflict, "unverified_landing",
			"The delivery branch "+r.Worktree.Branch+" carries nothing past its base "+shortCommit(base)+
				"; there is no delivery to land. If this task wrote nothing, record nothing_to_land.",
			map[string]any{"reason": UnverifiedNothing, "base": base, "delivery_head": h})
	}
	if carried, err := b.Git.IsAncestor(ctx, repo, h, c); err != nil || !carried {
		return landingProof{}, head, refuseWith(http.StatusConflict, "unverified_landing",
			"The commit does not carry this task's delivery head "+shortCommit(h)+
				"; name the commit on the target that contains the delivery branch.",
			map[string]any{"reason": UnverifiedNotCarried, "delivery_head": h})
	}
	proof.deliveryHead = h
	return proof, head, nil
}

// proveIncorporated proves the part of a semantic integration the ledger can
// prove without pretending Git understands program meaning:
//
//   - this task has a non-empty, resolvable delivery that is not itself an
//     ancestor of the named commit (otherwise ordinary `landed` is the word);
//   - another task in the same repository has a broker-verified
//     `landed` record for this exact target and commit.
//
// The link, repository, target and commits are facts. Whether a conflict
// resolution preserves every intended behaviour of the first delivery is a
// review judgement and remains in Landing.Note; comparing trees cannot prove
// it when the point of the integration was to rewrite conflicting changes.
func (b *Broker) proveIncorporated(ctx context.Context, r Record, carrierID, commit, target string) (landingProof, branchHead, error) {
	if carrierID == "" {
		return landingProof{}, branchHead{}, unverified(UnverifiedCarrierRequired,
			"incorporated must name the other task whose verified landing carried this delivery.")
	}
	if !IsTaskID(carrierID) {
		return landingProof{}, branchHead{}, unverified(UnverifiedCarrierMissing,
			"carrier_task must be a lowercase task UUID recorded by this broker.")
	}
	if carrierID == r.ID {
		return landingProof{}, branchHead{}, unverified(UnverifiedCarrierSelf,
			"A delivery cannot be its own integration carrier; use landed when its own commit reached the target.")
	}
	carrier, _, err := b.Record(ctx, carrierID)
	if err != nil {
		return landingProof{}, branchHead{}, unverified(UnverifiedCarrierMissing,
			"The named carrier task is not a readable record in this broker.")
	}
	if carrier.Landing == nil || carrier.Landing.State != LandingLanded || carrier.Landing.Commit == "" {
		return landingProof{}, branchHead{}, unverified(UnverifiedCarrierLanded,
			"The named carrier task has no broker-verified landed commit.")
	}

	repo := landingRepository(r)
	carrierRepo := landingRepository(carrier)
	if repo == "" || carrierRepo == "" || filepath.Clean(repo) != filepath.Clean(carrierRepo) {
		return landingProof{}, branchHead{}, unverified(UnverifiedCarrierRepo,
			"The delivery and its carrier must belong to the same repository.")
	}
	if carrier.Landing.Target != target {
		return landingProof{}, branchHead{}, unverified(UnverifiedCarrierTarget,
			"The carrier task's verified landing names another target.")
	}
	c, err := b.Git.ResolveCommit(ctx, repo, commit)
	if err != nil || c != carrier.Landing.Commit {
		return landingProof{}, branchHead{}, unverified(UnverifiedCarrierCommit,
			"The commit must be the exact commit in the carrier task's verified landing.")
	}

	if r.Worktree == nil || r.Worktree.Base == "" {
		return landingProof{}, branchHead{}, unverified(UnverifiedDelivery,
			"This task has no isolated delivery head and base to distinguish from the carrier's work.")
	}
	head := b.deliveryHead(ctx, r.Worktree)
	h := head.commit
	if h == "" {
		h = r.Worktree.Head
	}
	if h == "" {
		return landingProof{}, head, unverified(UnverifiedDelivery,
			"The original delivery branch is gone and no delivery head was recorded when the task settled.")
	}
	resolvedHead, err := b.Git.ResolveCommit(ctx, repo, h)
	if err != nil {
		return landingProof{}, head, unverified(UnverifiedDelivery,
			"The original delivery head no longer resolves in the task's repository.")
	}
	base, err := b.Git.ResolveCommit(ctx, repo, r.Worktree.Base)
	if err != nil {
		return landingProof{}, head, unverified(UnverifiedBase,
			"The original delivery's recorded base no longer resolves in the task's repository.")
	}
	if under, err := b.Git.IsAncestor(ctx, repo, resolvedHead, base); err != nil || under {
		return landingProof{}, head, unverified(UnverifiedNothing,
			"The original delivery carries nothing past its recorded base.")
	}
	if carried, err := b.Git.IsAncestor(ctx, repo, resolvedHead, c); err != nil {
		return landingProof{}, head, unverified(UnverifiedDelivery,
			"Git could not compare the original delivery with the carrier commit.")
	} else if carried {
		return landingProof{}, head, unverified(UnverifiedAlreadyCarried,
			"The carrier commit already contains the delivery commit by ancestry; record it as landed instead.")
	}
	return landingProof{commit: c, repo: repo, targetCommit: carrier.Landing.TargetCommit,
		deliveryHead: resolvedHead, base: base}, head, nil
}

func landingRepository(r Record) string {
	if r.Worktree != nil && r.Worktree.Repository != "" {
		return r.Worktree.Repository
	}
	return r.Repository
}

// --- pending, split -------------------------------------------------------

// presence is who the beat's last reading showed, kept for the one question a
// pending landing asks of it: is the session that owes it still here. It is an
// observation, held in memory and replaced by every pass (D04).
type presence struct {
	at time.Time
	// conversations is every assistant session's conversation id in the
	// reading, to its assistant.
	conversations map[string]string
	// anonymous is an assistant session in the reading whose conversation
	// could not be named. It could be anybody's, so while there is one,
	// nobody's absence is proved.
	anonymous bool
	// processes is whether the process table answered completely. Every
	// assistant runs as a process, whatever terminal it is drawn in, so this
	// is the one source whose silence about a conversation means it is not
	// running — unlike the whole reading's completeness, which an iTerm2
	// that cannot be asked keeps false on this Mac for ever (D05 ③).
	processes bool
}

// presenceFresh is how old the last reading may be and still answer. A beat
// that has stopped has not said anybody left.
const presenceFresh = 30 * time.Second

func presenceOf(rd reading) presence {
	p := presence{at: rd.at, conversations: map[string]string{}, processes: rd.sourceComplete("ps")}
	for _, s := range rd.sessions {
		if !s.IsAssistant() {
			continue
		}
		if s.ConversationID == "" {
			p.anonymous = true
			continue
		}
		p.conversations[s.ConversationID] = string(s.Assistant)
	}
	return p
}

// Obligation is what a pending landing means now; empty for any other.
//
// Live is positive evidence — the owning root's conversation is in the last
// reading. Orphaned is positive evidence too: the task has no root that could
// land it, or the process table answered completely, named every assistant it
// listed, and none of them is the root. Anything short of that is unknown.
func (b *Broker) Obligation(r Record) Obligation {
	if r.Landing == nil || r.Landing.State != LandingPending {
		return ""
	}
	if r.Root == nil || r.Root.SessionID == "" {
		return ObligationOrphaned
	}
	switch b.ownerLiveness(r.Root.SessionID, r.Root.Assistant) {
	case work.Live:
		return ObligationLive
	case work.Gone:
		return ObligationOrphaned
	}
	return ObligationUnknown
}

// ownerLiveness is where the beat's last reading places one conversation: the
// one answer to "is the session that owes this still here", read by a pending
// landing and by a to-do alike (todos.go), in the three-valued vocabulary the
// board's tracks use (internal/domain/work).
//
// Live is the conversation in a fresh reading. Gone is a fresh reading whose
// process table answered completely and named every assistant it listed,
// without it. Anything short of that — a stale reading, a partial process
// table, an assistant nobody could name — is unknown, and unknown never
// proves anybody left.
func (b *Broker) ownerLiveness(conversation, assistant string) work.Liveness {
	p := b.observed.presence()
	if p.at.IsZero() || b.now().Sub(p.at) > presenceFresh {
		return work.Unknown
	}
	if a, ok := p.conversations[conversation]; ok && (assistant == "" || a == assistant) {
		return work.Live
	}
	if p.processes && !p.anonymous {
		return work.Gone
	}
	return work.Unknown
}
