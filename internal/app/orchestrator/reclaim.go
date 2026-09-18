package orchestrator

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/git"
	"github.com/sainteye/clawdline-go/internal/adapters/store"
)

// Reclamation: taking back the checkouts and task directories finished work
// left behind (docs/design-decisions.md D13, D25 L1; limits N10, N11; cutover
// B4).
//
// This is the one place in the broker that deletes somebody's files, so the
// rule it runs on is written as the basis a removal may rest on, not as the
// shapes that went wrong before:
//
//   - **A removal rests on positive answers only.** The owner is gone because
//     the source that owns its tab answered completely and the tab is not in
//     it; the checkout's contents are known because git wrote them into a
//     tree; the landing is proved because git answered that the checkout's
//     commit is on the recorded target. Any question that could not be asked,
//     or was answered "I don't know", keeps the thing (DG-7: "unknown never
//     authorises a removal").
//   - **Nothing is removed that is not provably this broker's.** A checkout is
//     one this store's record names, at the path this broker gave it (checked
//     as spelled and as resolved), that the repository itself lists as its
//     checkout. Anything else under the checkout root — another broker's, a
//     person's, a leftover nobody names — is foreign and is only counted.
//   - **What is not landed is kept before it is removed.** Its commits stay on
//     the delivery branch, which is never deleted here. What it holds beyond
//     its commit — edits and untracked files — is written into a commit on a
//     preservation branch and into a binary patch, and the patch is proved by
//     applying it to the commit it was taken from and getting exactly the
//     tree it was taken to. Only then, and only if a second reading of the
//     checkout still gives that tree, is the checkout removed. Files the
//     repository ignores (dependencies, build output) are the one thing not
//     kept: they are what the repository itself declares disposable.
//   - **A decision is said once** (#46): kept, removed or preserved is an
//     event the first time and when it changes, never every sweep.

// The subjects a decision is about.
const (
	ReclaimWorktree = "worktree"
	ReclaimTaskDir  = "task_dir"
)

// The outcomes. The `would_` pair is a dry run's.
const (
	ReclaimRemoved       = "removed"
	ReclaimPreserved     = "preserved_and_removed"
	ReclaimKept          = "kept"
	ReclaimWouldRemove   = "would_remove"
	ReclaimWouldPreserve = "would_preserve_and_remove"
)

// The reasons. A kept reason names the question that had no positive answer.
const (
	// removed
	WhyLanded            = "landed"              // clean, and its commit is on the recorded target
	WhyEmpty             = "empty"               // clean, and its branch has no commit over its base (D13)
	WhyCommittedOnBranch = "committed_on_branch" // clean, not landed: its commits on the delivery branch and a preservation branch
	WhyPreserved         = "preserved"           // edits kept on a preservation branch and a proved patch
	WhyWorkScratch       = "work_scratch"        // a task directory's work/, which the briefing declares disposable
	// kept
	WhyLive         = "task_live"
	WhyGrace        = "within_grace"
	WhyOwnerPresent = "owner_present"
	WhyOwnerUnknown = "owner_unknown"
	WhyPathNotOwned = "path_not_owned"
	WhyUnreadable   = "unreadable"
	WhyNested       = "nested_repository"
	WhyFiltered     = "filters_present"
	WhyPreserveFail = "preserve_failed"
	WhyChanged      = "changed_during_sweep"
	WhyUnrecorded   = "intent_not_recorded"
	WhyRemoveFailed = "remove_failed"
)

// The outcome recorded before a removal is attempted, so a removal that a
// crash interrupts still has its row (#46, and D08's intent-first rule).
const reclaimRemoving = "removing"

// Defaults. The grace is limits §4.2's `work` row: twenty-four hours after
// the task ended, and never while its owner may still be there.
const (
	ReclaimGraceDefault = 24 * time.Hour
	// reclaimEvery is how often the beat starts a sweep, and reclaimFirst how
	// long after the broker starts it runs its first — late enough that the
	// first reading of the machine is in (the Swift app's six hours and its
	// sweep at start, `Orchestrator.swift:6437`).
	reclaimEvery = 6 * time.Hour
	reclaimFirst = 2 * time.Minute
	// reclaimDeadline bounds one automatic sweep: past it, the rest wait for
	// the next. A removal already begun is never cut short by it.
	reclaimDeadline = 30 * time.Minute
	// removeTimeout bounds one `git worktree remove`, on a context no caller
	// can cancel, so a request that gives up does not kill a removal halfway.
	removeTimeout = 2 * time.Minute
	// reclaimSweepLimit is how many subjects one sweep asks git about. The
	// rest wait for the next sweep, which starts where this one stopped
	// (broker-design #29: bounded, and advancing).
	reclaimSweepLimit = 64
	// reclaimForeignLimit is how many foreign entries one report names; the
	// count is always whole.
	reclaimForeignLimit = 64
)

// ReclaimDecision is what the sweep decided about one subject, and what it
// rested the decision on.
type ReclaimDecision struct {
	Task     string         `json:"task"`
	Subject  string         `json:"subject"`
	Path     string         `json:"path"`
	Outcome  string         `json:"outcome"`
	Reason   string         `json:"reason"`
	Bytes    int64          `json:"bytes"`
	Evidence map[string]any `json:"evidence,omitempty"`
}

// ReclaimReport is one sweep.
type ReclaimReport struct {
	At        time.Time         `json:"at"`
	DryRun    bool              `json:"dry_run"`
	Grace     time.Duration     `json:"-"`
	Decisions []ReclaimDecision `json:"decisions"`
	Foreign   []string          `json:"foreign"`
	// ForeignCount is every foreign entry, of which Foreign names at most
	// reclaimForeignLimit.
	ForeignCount int   `json:"foreign_count"`
	Removed      int   `json:"removed"`
	Preserved    int   `json:"preserved"`
	Kept         int   `json:"kept"`
	BytesFreed   int64 `json:"bytes_freed"`
	// Deferred is how many subjects were left for the next sweep by the
	// per-sweep limit.
	Deferred int `json:"deferred"`
	// Unreadable is how many stored rows could not be decoded; their files
	// are not touched, because nothing about them can be proved.
	Unreadable int `json:"unreadable"`
}

// reclaimState is the sweep's own memory: whether one is running, when the
// next is due, where the last stopped, and the last report. In memory only —
// every fact a removal rests on is read again each sweep.
type reclaimState struct {
	mu      sync.Mutex
	running bool
	next    time.Time
	cursor  int
	last    *ReclaimReport
	started time.Time
	// seen is the beat's last reading, for the inventory's `dispose` advice
	// (G22): a zero reading proves nobody gone.
	seen reading
}

// keepReading remembers the pass's reading for readers off the beat.
func (b *Broker) keepReading(rd reading) {
	b.reclaim.mu.Lock()
	b.reclaim.seen = rd
	b.reclaim.mu.Unlock()
}

// lastReading is the beat's last reading, or a zero one before the first.
func (b *Broker) lastReading() reading {
	b.reclaim.mu.Lock()
	defer b.reclaim.mu.Unlock()
	return b.reclaim.seen
}

// ErrReclaimRunning is a sweep asked for while one is already running.
var ErrReclaimRunning = refuseWith(409, "reclaim_running",
	"A reclamation sweep is already running; read its report when it ends.", withRemedy(nil, "reclaim_running"))

func (b *Broker) reclaimGrace() time.Duration {
	if b.ReclaimGrace > 0 {
		return b.ReclaimGrace
	}
	if b.ReclaimGrace < 0 {
		return 0
	}
	return ReclaimGraceDefault
}

// LastReclaim is the last sweep's report, nil before the first.
func (b *Broker) LastReclaim() *ReclaimReport {
	b.reclaim.mu.Lock()
	defer b.reclaim.mu.Unlock()
	return b.reclaim.last
}

// Reclaims is every standing decision, the longest-standing first.
func (b *Broker) Reclaims(ctx context.Context) ([]store.Reclaim, error) {
	rows, err := b.Store.Reclaims(ctx)
	if err != nil {
		return nil, storeError(err)
	}
	return rows, nil
}

// reclaimDue starts a sweep off the beat when one is due. It never runs on the
// beat itself: a sweep asks git about many checkouts, and a beat that waited
// for that would be reported stalled (DG-1) — and a stalled beat stops
// settling everybody's tasks for the sake of a cleanup.
func (b *Broker) reclaimDue(ctx context.Context) {
	if !b.ReclaimAuto {
		return
	}
	now := b.now()
	b.reclaim.mu.Lock()
	if b.reclaim.started.IsZero() {
		b.reclaim.started = now
	}
	if b.reclaim.next.IsZero() {
		b.reclaim.next = b.reclaim.started.Add(reclaimFirst)
	}
	due := !b.reclaim.running && !now.Before(b.reclaim.next)
	if due {
		b.reclaim.next = now.Add(reclaimEvery)
	}
	b.reclaim.mu.Unlock()
	if !due {
		return
	}
	go func() {
		sweep, cancel := context.WithTimeout(context.WithoutCancel(ctx), reclaimDeadline)
		defer cancel()
		_, _ = b.Reclaim(sweep, false)
	}()
}

// Reclaim runs one sweep. A dry run decides everything and removes nothing:
// no ref, no patch, no removal, no event, and the next real sweep starts
// where the last real one stopped. Its one trace is the unreferenced git
// objects a snapshot writes, which are content-addressed and collected by
// git's own gc.
func (b *Broker) Reclaim(ctx context.Context, dryRun bool) (ReclaimReport, error) {
	if err := outside(); err != nil {
		return ReclaimReport{}, err
	}
	b.reclaim.mu.Lock()
	if b.reclaim.running {
		b.reclaim.mu.Unlock()
		return ReclaimReport{}, ErrReclaimRunning
	}
	b.reclaim.running = true
	cursor := b.reclaim.cursor
	b.reclaim.mu.Unlock()
	defer func() {
		b.reclaim.mu.Lock()
		b.reclaim.running = false
		b.reclaim.mu.Unlock()
	}()

	records, bad, err := b.ledger(ctx)
	if err != nil {
		return ReclaimReport{}, err
	}
	rep := ReclaimReport{At: b.now(), DryRun: dryRun, Grace: b.reclaimGrace(), Decisions: []ReclaimDecision{},
		Foreign: []string{}, Unreadable: len(bad)}
	rd := b.read(ctx)

	// Oldest first, so the sweep reaches what has waited longest; then
	// rotated by where the last sweep stopped, so a limit never pins it to
	// the same first few.
	sort.Slice(records, func(i, j int) bool {
		if !records[i].FinishedAt.Equal(records[j].FinishedAt) {
			return records[i].FinishedAt.Before(records[j].FinishedAt)
		}
		return records[i].ID < records[j].ID
	})
	if n := len(records); n > 0 {
		cursor %= n
		records = append(records[cursor:], records[:cursor]...)
	}
	asked := 0
	next := cursor
	for _, r := range records {
		if asked >= reclaimSweepLimit || ctx.Err() != nil {
			rep.Deferred++
			continue
		}
		next++
		touched := false
		if r.Worktree != nil {
			d := b.reclaimCheckout(ctx, rd, r, rep.At, dryRun)
			touched = touched || (d.Subject != "" && d.Reason != WhyLive)
			b.decide(ctx, &rep, d, dryRun)
		}
		if r.Dir != "" {
			d := b.reclaimTaskDir(ctx, rd, r, rep.At, dryRun)
			touched = touched || (d.Subject != "" && d.Reason != WhyLive)
			b.decide(ctx, &rep, d, dryRun)
		}
		if touched {
			asked++
		}
	}
	b.foreign(records, bad, &rep)

	if !dryRun {
		b.reclaim.mu.Lock()
		if len(records) > 0 {
			b.reclaim.cursor = next % len(records)
		}
		copied := rep
		b.reclaim.last = &copied
		b.reclaim.mu.Unlock()
	}
	return rep, nil
}

// decide adds one decision to the report and, on a real sweep, records it —
// with its event only when it is new or changed (#46).
func (b *Broker) decide(ctx context.Context, rep *ReclaimReport, d ReclaimDecision, dryRun bool) {
	if d.Subject == "" {
		return
	}
	rep.Decisions = append(rep.Decisions, d)
	switch d.Outcome {
	case ReclaimRemoved, ReclaimWouldRemove:
		rep.Removed++
		rep.BytesFreed += d.Bytes
	case ReclaimPreserved, ReclaimWouldPreserve:
		rep.Preserved++
		rep.BytesFreed += d.Bytes
	default:
		rep.Kept++
	}
	if dryRun || d.Reason == WhyLive {
		return
	}
	if err := b.markReclaim(ctx, d, rep.At); err != nil {
		log.Printf("orchestrator: the sweep's decision about %s %s (%s) could not be recorded: %v",
			d.Subject, d.Task, d.Outcome, err)
	}
}

// markReclaim records one decision, with its event when it is new.
func (b *Broker) markReclaim(ctx context.Context, d ReclaimDecision, at time.Time) error {
	detail, _ := json.Marshal(d.Evidence)
	payload, _ := json.Marshal(d)
	_, err := b.Store.MarkReclaim(ctx, store.Reclaim{
		Task: d.Task, Subject: d.Subject, Outcome: d.Outcome, Reason: d.Reason, Bytes: d.Bytes,
		Detail: detail, LastAt: at,
	}, &store.Event{Kind: "reclaim." + d.Subject + "." + d.Outcome, Subject: d.Task, Payload: payload})
	return err
}

// kept is a decision to leave a subject as it is.
func kept(r Record, subject, path, why string, evidence map[string]any) ReclaimDecision {
	return ReclaimDecision{Task: r.ID, Subject: subject, Path: path, Outcome: ReclaimKept, Reason: why, Evidence: evidence}
}

// ended is when a task's work stopped changing: its settlement, or its
// landing when that came later.
func ended(r Record) time.Time {
	at := r.FinishedAt
	if r.Landing != nil && r.Landing.At.After(at) {
		at = r.Landing.At
	}
	return at
}

// ownerGone is whether the session that worked for a task is provably not on
// this machine any more, and the reason when it is not.
//
// Gone is two positive answers, from two owners of the truth:
//
//   - the tab. The source that owns the child's tab answered completely, in a
//     reading that saw at least one terminal, and the tab is not in it — or
//     the launcher itself answered that it never opened one (a spawn_failed
//     task with no tab recorded). A child whose tab was never recorded for
//     any other reason is unknown: "no id" is not "no process" (the Swift
//     app's D1 lost a working child's checkout to exactly that reading).
//   - the kernel. No process on this machine has its working directory
//     inside any of paths, as `lsof` answers it with every link resolved and
//     case folded — a person's shell in an iTerm2 tab this daemon cannot
//     read counts as much as the child. A process table that cannot be read
//     is unknown.
func (b *Broker) ownerGone(ctx context.Context, rd reading, r Record, paths ...string) (bool, string, map[string]any) {
	evidence := map[string]any{"terminal": r.ChildTerminalID, "backend": r.ChildBackend,
		"reading_sessions": len(rd.sessions), "source_complete": rd.sourceComplete(r.ChildBackend)}
	switch {
	case r.ChildTerminalID == "" && r.State == StateSpawnFailed && r.SpawnedAt.IsZero():
		evidence["tab"] = "never_opened"
	case r.ChildTerminalID == "":
		return false, WhyOwnerUnknown, evidence
	default:
		if _, present := rd.session(r.ChildTerminalID); present {
			return false, WhyOwnerPresent, evidence
		}
		if len(rd.sessions) == 0 || !rd.sourceComplete(r.ChildBackend) {
			return false, WhyOwnerUnknown, evidence
		}
		evidence["tab"] = "absent_from_complete_source"
	}
	inside, err := b.occupied(ctx, paths...)
	if err != nil {
		evidence["process_table"] = err.Error()
		return false, WhyOwnerUnknown, evidence
	}
	if inside != "" {
		evidence["working_inside"] = inside
		return false, WhyOwnerPresent, evidence
	}
	evidence["process_table"] = "no process works inside"
	return true, "", evidence
}

// occupied names a process working directory inside any of paths, or answers
// "" when the kernel's answer names none.
func (b *Broker) occupied(ctx context.Context, paths ...string) (string, error) {
	targets := []string{}
	for _, p := range paths {
		if p == "" {
			continue
		}
		targets = append(targets, foldPath(p))
	}
	if len(targets) == 0 {
		return "", nil
	}
	read := b.ProcessCWDs
	if read == nil {
		read = lsofCWDs
	}
	cwds, err := read(ctx)
	if err != nil {
		return "", err
	}
	for _, cwd := range cwds {
		folded := foldPath(cwd)
		for _, t := range targets {
			if folded == t || strings.HasPrefix(folded, t+"/") {
				return cwd, nil
			}
		}
	}
	return "", nil
}

// foldPath is a path as this Mac's file system compares it: links resolved
// — through the deepest part of it that exists, so a directory made or
// removed since the kernel answered is still compared under the same
// spelling as its parent — and case folded, because APFS by default does
// not tell /tmp/A from /tmp/a and a comparison that does would miss a person
// standing there.
func foldPath(p string) string {
	p = filepath.Clean(p)
	rest := ""
	for dir := p; ; dir = filepath.Dir(dir) {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			p = filepath.Join(resolved, rest)
			break
		}
		if parent := filepath.Dir(dir); parent == dir {
			break
		}
		rest = filepath.Join(filepath.Base(dir), rest)
	}
	return strings.ToLower(p)
}

// lsofCWDs is every process's working directory as `lsof` lists it. A list
// with nothing in it is not an answer — this daemon has a working directory
// of its own — and neither is a failure with no output.
func lsofCWDs(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "lsof", "-n", "-P", "-w", "-d", "cwd", "-Fn")
	out, err := cmd.Output()
	paths := []string{}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "n/") {
			paths = append(paths, strings.TrimPrefix(line, "n"))
		}
	}
	if len(paths) == 0 {
		if err == nil {
			err = errors.New("lsof listed no working directory at all")
		}
		return nil, err
	}
	// lsof exits 1 when some process could not be inspected; what it did
	// list is still the kernel's answer for this user's processes.
	return paths, nil
}

// ownedPath is whether path is exactly where this broker would have put the
// named subject: under root as spelled, under root as resolved, and named for
// the task. The first check is on the spelling, before any other spelling of
// it is consulted (the dispatch policy's rule for anything that deletes).
func ownedPath(root, path, taskID string) bool {
	if root == "" || path == "" || !filepath.IsAbs(path) {
		return false
	}
	root, path = filepath.Clean(root), filepath.Clean(path)
	if !strings.HasPrefix(path, root+string(filepath.Separator)) || filepath.Base(path) != taskID {
		return false
	}
	resolvedRoot, err1 := filepath.EvalSymlinks(root)
	resolved, err2 := filepath.EvalSymlinks(path)
	if err1 != nil || err2 != nil {
		return false
	}
	return strings.HasPrefix(resolved, resolvedRoot+string(filepath.Separator)) && filepath.Base(resolved) == taskID
}

// listedCheckout is the repository's own entry for a checkout, found by the
// path as recorded or as resolved. Absent is an answer; an error is not.
func (b *Broker) listedCheckout(ctx context.Context, repo, path string) (*git.WorktreeEntry, error) {
	list, err := b.Git.Worktrees(ctx, repo)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{filepath.Clean(path): true}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		want[resolved] = true
	}
	for i := range list {
		p := list[i].Path
		if want[p] {
			return &list[i], nil
		}
		if resolved, err := filepath.EvalSymlinks(p); err == nil && want[resolved] {
			return &list[i], nil
		}
	}
	return nil, nil
}

// reclaimCheckout decides one task's checkout and, on a real sweep, carries
// the decision out. The questions are asked cheapest first, and the first
// that has no positive answer keeps the checkout.
func (b *Broker) reclaimCheckout(ctx context.Context, rd reading, r Record, now time.Time, dryRun bool) ReclaimDecision {
	w := r.Worktree
	path := w.Path
	if !r.State.Terminal() {
		return kept(r, ReclaimWorktree, path, WhyLive, nil)
	}
	if !dirExists(path) {
		// Nothing to take — removed by an earlier sweep, or never made. Not a
		// decision, and not said again every sweep (#46).
		return ReclaimDecision{}
	}
	if !ownedPath(b.WorktreeRoot(), path, r.ID) {
		return kept(r, ReclaimWorktree, path, WhyPathNotOwned, map[string]any{"root": b.WorktreeRoot()})
	}
	if at := ended(r); now.Sub(at) < b.reclaimGrace() {
		return kept(r, ReclaimWorktree, path, WhyGrace, map[string]any{"ended_at": at.Unix(),
			"grace_seconds": int(b.reclaimGrace().Seconds())})
	}
	gone, why, owner := b.ownerGone(ctx, rd, r, path, r.Dir)
	if !gone {
		return kept(r, ReclaimWorktree, path, why, map[string]any{"owner": owner})
	}
	entry, err := b.listedCheckout(ctx, w.Repository, path)
	if err != nil {
		return kept(r, ReclaimWorktree, path, WhyUnreadable, map[string]any{"worktree_list": err.Error()})
	}
	if entry == nil || entry.Locked {
		return kept(r, ReclaimWorktree, path, WhyPathNotOwned,
			map[string]any{"listed_by_repository": entry != nil, "locked": entry != nil && entry.Locked})
	}
	// A repository inside the checkout holds work its snapshot cannot see,
	// and a filter decides what "unchanged" means by running a program the
	// repository chose: either way the checkout's contents are not known.
	if nested, err := git.NestedRepository(path); err != nil || nested != "" {
		ev := map[string]any{"nested": nested}
		if err != nil {
			return kept(r, ReclaimWorktree, path, WhyUnreadable, map[string]any{"walk": err.Error()})
		}
		return kept(r, ReclaimWorktree, path, WhyNested, ev)
	}
	if filtered, err := b.Git.Filtered(ctx, path); err != nil || filtered {
		if err != nil {
			return kept(r, ReclaimWorktree, path, WhyUnreadable, map[string]any{"attributes": err.Error()})
		}
		return kept(r, ReclaimWorktree, path, WhyFiltered, nil)
	}
	snap, err := b.Git.SnapshotCheckout(ctx, path)
	if err != nil {
		return kept(r, ReclaimWorktree, path, WhyUnreadable, map[string]any{"snapshot": err.Error()})
	}
	tip, tipErr := b.Git.RefCommit(ctx, w.Repository, "refs/heads/"+w.Branch)
	evidence := map[string]any{
		"owner": owner, "repository": w.Repository, "branch": w.Branch, "base": w.Base,
		"head": snap.Head, "head_tree": snap.HeadTree, "tree": snap.Tree, "dirty": snap.Dirty(),
		"listed_branch": entry.Branch, "detached": entry.Detached,
	}
	if tipErr == nil {
		evidence["branch_tip"] = tip
	}
	bytes, _ := dirBytes(path)
	decision := ReclaimDecision{Task: r.ID, Subject: ReclaimWorktree, Path: path, Bytes: bytes, Evidence: evidence}

	onBranch := tipErr == nil && tip == snap.Head
	if !snap.Dirty() && onBranch {
		// Everything the checkout holds is a commit on the delivery branch,
		// which stays. Landed and empty need nothing more; anything else also
		// gets a preservation branch, because once the checkout is gone git
		// no longer refuses to delete the branch it had checked out.
		switch {
		case landedOnTarget(ctx, b, r, snap.Head, evidence):
			decision.Reason = WhyLanded
		default:
			n, known := b.Git.Commits(ctx, w.Repository, w.Base, w.Branch)
			if known {
				evidence["commits_over_base"] = n
			}
			if known && n == 0 {
				decision.Reason = WhyEmpty
			} else {
				decision.Reason = WhyCommittedOnBranch
			}
		}
		if dryRun {
			decision.Outcome = ReclaimWouldRemove
			return decision
		}
		if decision.Reason == WhyCommittedOnBranch {
			if err := b.keepRef(ctx, r, snap.Head, snap.Tree); err != nil {
				evidence["preserve"] = err.Error()
				return kept(r, ReclaimWorktree, path, WhyPreserveFail, evidence)
			}
			evidence["preservation_branch"] = PreservationBranch(r.ID)
		}
		return b.removeCheckout(ctx, r, snap, decision)
	}

	// Edits, untracked files, or a checkout that moved off its branch: kept
	// on a preservation branch and a proved patch before anything is removed.
	if dryRun {
		decision.Outcome, decision.Reason = ReclaimWouldPreserve, WhyPreserved
		return decision
	}
	if err := b.preserve(ctx, r, snap, evidence); err != nil {
		evidence["preserve"] = err.Error()
		return kept(r, ReclaimWorktree, path, WhyPreserveFail, evidence)
	}
	decision.Reason = WhyPreserved
	d := b.removeCheckout(ctx, r, snap, decision)
	if d.Outcome == ReclaimRemoved {
		d.Outcome = ReclaimPreserved
	}
	return d
}

// keepRef points the task's preservation branch at commit, or finds it
// already there holding exactly tree.
func (b *Broker) keepRef(ctx context.Context, r Record, commit, tree string) error {
	ref := "refs/heads/" + PreservationBranch(r.ID)
	err := b.Git.CreateRef(ctx, r.Worktree.Repository, ref, commit)
	if !errors.Is(err, git.ErrRefExists) {
		return err
	}
	// A sweep that died after the ref and before the removal left it. It may
	// stand for this snapshot only if it holds exactly this tree.
	existing, err := b.Git.RefCommit(ctx, r.Worktree.Repository, ref)
	if err != nil {
		return err
	}
	have, err := b.Git.Tree(ctx, r.Worktree.Repository, existing)
	if err != nil || have != tree {
		return errors.New("the preservation branch " + PreservationBranch(r.ID) + " already holds something else")
	}
	return nil
}

// landedOnTarget is the landing proof a removal may rest on: the record says
// landed, it names a target, and git answers that the checkout's commit is on
// that target now.
func landedOnTarget(ctx context.Context, b *Broker, r Record, head string, evidence map[string]any) bool {
	if r.Landing == nil || r.Landing.State != LandingLanded || r.Landing.Target == "" {
		return false
	}
	evidence["landing_state"] = r.Landing.State
	evidence["landing_target"] = r.Landing.Target
	evidence["landing_commit"] = r.Landing.Commit
	on, err := b.Git.IsAncestor(ctx, r.Worktree.Repository, head, "refs/heads/"+r.Landing.Target)
	if err != nil {
		evidence["landing_proof"] = err.Error()
		return false
	}
	evidence["head_on_target"] = on
	if tc, err := b.Git.RefCommit(ctx, r.Worktree.Repository, "refs/heads/"+r.Landing.Target); err == nil {
		evidence["target_commit"] = tc
	}
	return on
}

// PreservationBranch is where a reclaimed checkout's edits are kept.
func PreservationBranch(taskID string) string { return "clawdline/reclaimed/" + taskID }

// ReclaimedDir is where a reclaimed checkout's patch and manifest are kept:
// beside the checkouts, under this daemon's state root.
func (b *Broker) ReclaimedDir() string { return filepath.Join(b.Dir, "reclaimed") }

// preserve keeps what a checkout holds beyond its commit, and proves it did.
//
// Two copies, because each survives what the other does not: a commit on a
// branch in the repository (the tree exactly, reachable with every git tool,
// and gone if somebody deletes the branch), and a patch under this daemon's
// state root with a manifest naming its hash (readable without the
// repository's refs, and gone if somebody deletes the directory). The patch is
// proved before either is trusted: applied to the commit it was taken from, it
// must give exactly the snapshot's tree.
func (b *Broker) preserve(ctx context.Context, r Record, snap git.Snapshot, evidence map[string]any) error {
	repo := r.Worktree.Repository
	commit, err := b.Git.CommitTree(ctx, repo, snap.Tree, snap.Head,
		"Clawdline: what task "+r.ID+"'s checkout held when it was reclaimed\n\n"+
			"Branch "+r.Worktree.Branch+", checkout "+r.Worktree.Path+".\n")
	if err != nil {
		return err
	}
	if err := b.keepRef(ctx, r, commit, snap.Tree); err != nil {
		return err
	}
	if existing, err := b.Git.RefCommit(ctx, repo, "refs/heads/"+PreservationBranch(r.ID)); err == nil {
		commit = existing
	}
	patch, err := b.Git.BinaryDiff(ctx, repo, snap.Head, commit)
	if err != nil {
		return err
	}
	proved, err := b.Git.ApplyToTree(ctx, repo, snap.Head, patch)
	if err != nil {
		return errors.New("the patch did not apply to the commit it was taken from: " + err.Error())
	}
	if proved != snap.Tree {
		return errors.New("the patch applied to " + snap.Head + " gives tree " + proved + ", not " + snap.Tree)
	}
	dir := filepath.Join(b.ReclaimedDir(), r.ID, strings.Join([]string{
		time.Now().UTC().Format("20060102T150405Z"), shortRandom()}, "-"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	sum := sha256.Sum256(patch)
	manifest := map[string]any{
		"clawdline_reclaimed_checkout": 1,
		"task":                         r.ID,
		"repository":                   repo,
		"checkout":                     r.Worktree.Path,
		"branch":                       r.Worktree.Branch,
		"base":                         r.Worktree.Base,
		"head":                         snap.Head,
		"tree":                         snap.Tree,
		"preservation_branch":          PreservationBranch(r.ID),
		"preservation_commit":          commit,
		"patch": map[string]any{
			"file": "delta.patch", "bytes": len(patch), "sha256": hex.EncodeToString(sum[:]),
			"applies_to": snap.Head, "gives_tree": proved,
		},
		"not_kept":   "files the repository ignores",
		"created_at": time.Now().UTC().Format(time.RFC3339),
	}
	body, _ := json.MarshalIndent(manifest, "", "  ")
	if err := writeFileSync(filepath.Join(dir, "delta.patch"), patch); err != nil {
		return err
	}
	if err := writeFileSync(filepath.Join(dir, "manifest.json"), append(body, '\n')); err != nil {
		return err
	}
	// Read back: the file on disk is the one whose hash the manifest names.
	if back, err := os.ReadFile(filepath.Join(dir, "delta.patch")); err != nil || sha256.Sum256(back) != sum {
		return errors.New("the saved patch does not read back as written")
	}
	evidence["preservation_branch"] = PreservationBranch(r.ID)
	evidence["preservation_commit"] = commit
	evidence["patch"] = filepath.Join(dir, "delta.patch")
	evidence["patch_sha256"] = hex.EncodeToString(sum[:])
	evidence["patch_bytes"] = len(patch)
	evidence["patch_gives_tree"] = proved
	return nil
}

// removeCheckout removes a checkout whose contents were just accounted for —
// but only if a second reading still gives the tree the decision rested on,
// the kernel still names nobody inside it, and the intent to remove it is
// recorded first. A child or a person writing into it, or standing in it,
// between the two readings keeps it.
func (b *Broker) removeCheckout(ctx context.Context, r Record, snap git.Snapshot, d ReclaimDecision) ReclaimDecision {
	again, err := b.Git.SnapshotCheckout(ctx, d.Path)
	if err != nil || again.Head != snap.Head || again.Tree != snap.Tree {
		ev := d.Evidence
		if err != nil {
			ev["second_snapshot"] = err.Error()
		} else {
			ev["second_head"], ev["second_tree"] = again.Head, again.Tree
		}
		return kept(r, ReclaimWorktree, d.Path, WhyChanged, ev)
	}
	if inside, err := b.occupied(ctx, d.Path); err != nil || inside != "" {
		d.Evidence["second_process_table"] = inside
		if err != nil {
			d.Evidence["second_process_table"] = err.Error()
			return kept(r, ReclaimWorktree, d.Path, WhyOwnerUnknown, d.Evidence)
		}
		return kept(r, ReclaimWorktree, d.Path, WhyOwnerPresent, d.Evidence)
	}
	if err := outside(); err != nil {
		return kept(r, ReclaimWorktree, d.Path, WhyRemoveFailed, map[string]any{"error": err.Error()})
	}
	intent := d
	intent.Outcome = reclaimRemoving
	if err := b.markReclaim(ctx, intent, b.now()); err != nil {
		d.Evidence["intent"] = err.Error()
		return kept(r, ReclaimWorktree, d.Path, WhyUnrecorded, d.Evidence)
	}
	// Its own clock, on a context no caller can cancel: a request that gives
	// up must not kill a removal halfway through.
	rm, cancel := context.WithTimeout(context.WithoutCancel(ctx), removeTimeout)
	defer cancel()
	if err := b.Git.RemoveWorktree(rm, r.Worktree.Repository, d.Path); err != nil {
		d.Evidence["remove"] = err.Error()
		return kept(r, ReclaimWorktree, d.Path, WhyRemoveFailed, d.Evidence)
	}
	_ = b.Git.PruneWorktrees(rm, r.Worktree.Repository)
	d.Outcome = ReclaimRemoved
	return d
}

// reclaimTaskDir decides one task directory's `work/` — the scratch the
// briefing tells the child is deleted when the task ends — once the task is
// over and its owner provably gone: at once on success, after the grace
// otherwise.
//
// Only `work/`. The rest of the directory is the task's evidence: the brief,
// the result a completion notice tells the root to read, and `artifacts/`,
// which the briefing names as what the child wants kept. D25 puts the whole
// directory in the workspace class, but nothing here can preserve those files
// before removing them, and a root told to read a result.json that a sweep
// took is the Swift app's silent loss over again; so the directory stays, and
// whether it may go is left to a person (report.md).
func (b *Broker) reclaimTaskDir(ctx context.Context, rd reading, r Record, now time.Time, dryRun bool) ReclaimDecision {
	scratch := filepath.Join(r.Dir, "work")
	if !r.State.Terminal() {
		return kept(r, ReclaimTaskDir, scratch, WhyLive, nil)
	}
	if !dirExists(scratch) {
		return ReclaimDecision{}
	}
	if !ownedPath(b.Tasks.Dir, r.Dir, r.ID) || !ownedPath(r.Dir, scratch, "work") {
		return kept(r, ReclaimTaskDir, scratch, WhyPathNotOwned, map[string]any{"root": b.Tasks.Dir})
	}
	grace := b.reclaimGrace()
	if r.State == StateSuccess {
		grace = 0
	}
	if now.Sub(r.FinishedAt) < grace {
		return kept(r, ReclaimTaskDir, scratch, WhyGrace, map[string]any{"ended_at": r.FinishedAt.Unix(),
			"grace_seconds": int(grace.Seconds())})
	}
	checkout := ""
	if r.Worktree != nil {
		checkout = r.Worktree.Path
	}
	gone, why, owner := b.ownerGone(ctx, rd, r, r.Dir, checkout)
	if !gone {
		return kept(r, ReclaimTaskDir, scratch, why, map[string]any{"owner": owner})
	}
	bytes, _ := dirBytes(scratch)
	d := ReclaimDecision{Task: r.ID, Subject: ReclaimTaskDir, Path: scratch, Reason: WhyWorkScratch, Bytes: bytes,
		Evidence: map[string]any{"owner": owner}}
	if dryRun {
		d.Outcome = ReclaimWouldRemove
		return d
	}
	intent := d
	intent.Outcome = reclaimRemoving
	if err := b.markReclaim(ctx, intent, b.now()); err != nil {
		d.Evidence["intent"] = err.Error()
		return kept(r, ReclaimTaskDir, scratch, WhyUnrecorded, d.Evidence)
	}
	if err := removeOwned(r.Dir, scratch, "work"); err != nil {
		d.Evidence["remove"] = err.Error()
		return kept(r, ReclaimTaskDir, scratch, WhyRemoveFailed, d.Evidence)
	}
	d.Outcome = ReclaimRemoved
	return d
}

// removeOwned removes path only after proving, again and as spelled, that it
// is `root/<name>` — the check is repeated here so no caller can reach
// RemoveAll with a path that was proved about a different spelling.
func removeOwned(root, path, name string) error {
	if !ownedPath(root, path, name) {
		return errors.New("refusing to remove a path this broker cannot prove it owns: " + path)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("refusing to remove something that is not a directory: " + path)
	}
	return os.RemoveAll(path)
}

// foreign counts what is under the checkout and task roots that no record
// names. It is never touched: nothing about it can be proved to be ours.
func (b *Broker) foreign(records []Record, bad []Unreadable, rep *ReclaimReport) {
	known := map[string]bool{}
	for _, r := range records {
		if r.Worktree != nil {
			known[filepath.Clean(r.Worktree.Path)] = true
		}
		if r.Dir != "" {
			known[filepath.Clean(r.Dir)] = true
		}
	}
	unreadable := map[string]bool{}
	for _, u := range bad {
		unreadable[u.ID] = true
	}
	note := func(path string) {
		if known[filepath.Clean(path)] {
			return
		}
		if unreadable[filepath.Base(path)] {
			return
		}
		rep.ForeignCount++
		if len(rep.Foreign) < reclaimForeignLimit {
			rep.Foreign = append(rep.Foreign, path)
		}
	}
	if slugs, err := os.ReadDir(b.WorktreeRoot()); err == nil {
		for _, slug := range slugs {
			if !slug.IsDir() {
				continue
			}
			entries, err := os.ReadDir(filepath.Join(b.WorktreeRoot(), slug.Name()))
			if err != nil {
				continue
			}
			for _, e := range entries {
				note(filepath.Join(b.WorktreeRoot(), slug.Name(), e.Name()))
			}
		}
	}
	if entries, err := os.ReadDir(b.Tasks.Dir); err == nil {
		for _, e := range entries {
			note(filepath.Join(b.Tasks.Dir, e.Name()))
		}
	}
}

// dirBytes is the apparent size of everything under path, without following
// a link. The second answer is false when part of it could not be read.
func dirBytes(path string) (int64, bool) {
	var total int64
	whole := true
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			whole = false
			return nil
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total, whole
}

func writeFileSync(path string, body []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(body); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func shortRandom() string {
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// reclaimedDirty is what the sweep recorded about a checkout it took: whether
// the checkout held anything beyond its commit, and where that was kept. The
// second answer is false when the sweep has no record of taking it — a
// checkout gone some other way is unknown, as it always was.
func (b *Broker) reclaimedDirty(ctx context.Context, taskID string) (dirty bool, kept string, known bool) {
	row, err := b.Store.ReclaimOf(ctx, taskID, ReclaimWorktree)
	if err != nil {
		return false, "", false
	}
	switch row.Outcome {
	case ReclaimPreserved:
		return true, PreservationBranch(taskID), true
	case ReclaimRemoved:
		return false, "", true
	}
	return false, "", false
}
