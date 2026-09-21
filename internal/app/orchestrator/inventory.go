package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// What is already going on in a repository, and the receipt that says a caller
// read it.
//
// The route exists because another session's isolated checkout is invisible
// from the shared tree: a finished delivery sitting on a branch nobody merged
// shows up in no `git status` and no file listing. So "nothing here does that
// yet" is not evidence, and this is.
//
// `generation` is the receipt. It is a digest of exactly the facts a dispatcher
// needed to have read — which rows exist, in which section, on which branch,
// why, what to do about them, and what they claim — and deliberately not of the
// clock, the titles, the commits or the states within a section. A receipt that
// moved every second would be a receipt nobody could carry from a read to a
// write; one that never moved would prove nothing.

// InventorySchema is the version the digest is salted with.
const InventorySchema = 1

// Visibility is which section a row is in.
type Visibility string

const (
	VisibilityLive     Visibility = "live"
	VisibilityUnlanded Visibility = "unlanded"
	VisibilitySettled  Visibility = "settled"
	VisibilityUnmerged Visibility = "unmerged"
	// VisibilityUnreadable is a stored row nobody can decode (D05 ②). It is a
	// section of its own because it belongs to none of the others: whether it
	// is running, delivered or settled is exactly what cannot be read.
	VisibilityUnreadable Visibility = "unreadable"
)

// The `do` a row suggests.
const (
	DoCoordinate     = "coordinate_or_take_over"
	DoLandOrAbandon  = "land_or_abandon"
	DoNothingToLand  = "nothing_to_land"
	DoDispose        = "dispose"
	WhyMergedClean   = "merged_and_clean"
	WhyBranchEmpty   = "branch_empty"
	WhyOrphanedCheck = "checkout_orphaned"
	// DoInspect is the one `do` no route can carry out: a person has to look
	// at the row. The safe default while nobody does is that it stays.
	DoInspect = "inspect"
)

// InventoryRow is one row of one section, in the wire's own vocabulary.
//
// It is one struct for three sections because the digest treats them as one,
// and because a reader comparing sections should not have to compare struct
// shapes as well. The JSON each section publishes is assembled in the transport
// from these fields; a key absent from a section is absent there, not empty.
type InventoryRow struct {
	Section   Visibility
	Task      string
	Title     string
	State     State
	Assistant string
	Claims    []string
	Age       int
	Do        string
	RootLabel string
	RootKey   string
	Branch    string
	Head      string
	Landing   LandingState
	Why       string
	Path      string
	OnDisk    bool
	Branched  bool
	Overlaps  []string
	// DeclaredWrites is what the task said at dispatch it would write — the
	// landing write set — whatever its lease became (D21). Nil when it cannot
	// be known: an unreadable row, or one written before W1 by a broker that
	// erased an isolated task's list.
	DeclaredWrites []string
	LeaseScope     string
	// Target is the branch the landing record names, empty while the root
	// has not named one (D19). Merged is whether the delivery branch is on
	// that target — nil when it cannot be said, which includes every row
	// with no target: the repository's HEAD is not a stand-in for a
	// decision nobody made.
	Target string
	Merged *bool
	// Obligation is what a pending landing means now (#35), derived from the
	// beat's last reading and never stored.
	Obligation Obligation
	// StoredState and Cause are an unreadable row's lifted state column and
	// the decoder's own sentence; Project and Created are the columns stored
	// beside its record, which stay readable when the record does not.
	StoredState string
	Cause       string
	Project     string
	Created     time.Time
}

// Inventory is the whole answer.
type Inventory struct {
	Schema     int
	Repository string
	Generation string
	Live       []InventoryRow
	Unlanded   []InventoryRow
	Droppable  []InventoryRow
	Unreadable []InventoryRow
	At         time.Time
	// TaskRoot is where this daemon's task directories live. **It is this
	// daemon's own addition**: the Swift broker hardcodes /tmp/.clawdline, and
	// a caller that must write task.json before dispatching has to be able to
	// find out where. It is outside the digest, because it is a fact about the
	// daemon and not about the repository's work.
	TaskRoot string
}

// Digest fields, published so a reader can see what the receipt is over.
var (
	DigestSealed   = []string{"section", "task", "branch", "why", "do", "claims"}
	DigestExcluded = []string{"age_seconds", "created", "state", "head", "dirty", "title", "root_label", "overlaps", "at",
		"declared_writes", "lease_scope", "stored_state", "cause", "target", "merged", "obligation"}
)

// ReadInventory builds the answer for one repository.
func (b *Broker) ReadInventory(ctx context.Context, project string, claims []string) (Inventory, error) {
	repo, err := b.repositoryOf(ctx, project)
	if err != nil {
		return Inventory{}, err
	}
	rows, err := b.rows(ctx, repo)
	if err != nil {
		return Inventory{}, err
	}
	inv := Inventory{
		Schema:     InventorySchema,
		Repository: repo,
		At:         b.now(),
		TaskRoot:   b.Tasks.Dir,
		Live:       []InventoryRow{},
		Unlanded:   []InventoryRow{},
		Droppable:  []InventoryRow{},
		Unreadable: []InventoryRow{},
	}
	for _, row := range rows {
		switch row.Section {
		case VisibilityLive:
			row.Overlaps = overlapWith(row.Claims, claims)
			inv.Live = append(inv.Live, row)
		case VisibilityUnlanded:
			inv.Unlanded = append(inv.Unlanded, row)
		case VisibilityUnreadable:
			inv.Unreadable = append(inv.Unreadable, row)
		}
		if row.Do == DoDispose {
			inv.Droppable = append(inv.Droppable, row)
		}
	}
	inv.Generation = generation(repo, rows)
	return inv, nil
}

// repositoryOf resolves a caller's directory to the repository the answer is
// about, refusing in the Swift app's words when it is not one.
func (b *Broker) repositoryOf(ctx context.Context, project string) (string, error) {
	if project == "" || !strings.HasPrefix(project, "/") {
		return "", refuse(http.StatusBadRequest, "bad_request",
			"project must be an absolute path inside a Git repository.")
	}
	repo, err := b.Git.Toplevel(ctx, project)
	if err != nil || repo == "" || !strings.HasPrefix(repo, "/") {
		return "", refuse(http.StatusBadRequest, "bad_request",
			"project must be an absolute path inside a Git repository.")
	}
	return repo, nil
}

// rows reads every task this broker holds for a repository and decides what
// each one is.
func (b *Broker) rows(ctx context.Context, repo string) ([]InventoryRow, error) {
	records, bad, err := b.ledger(ctx)
	if err != nil {
		return nil, err
	}
	now := b.now()
	out := []InventoryRow{}
	for _, r := range records {
		if !inRepository(r, repo) {
			continue
		}
		out = append(out, b.row(ctx, r, now))
	}
	for _, u := range bad {
		home := u.Repository
		if home == "" {
			home = u.Project
		}
		if home != repo && !strings.HasPrefix(home, repo+"/") {
			continue
		}
		age := int(now.Sub(u.CreatedAt).Seconds())
		if age < 0 {
			age = 0
		}
		out = append(out, InventoryRow{
			Section: VisibilityUnreadable, Task: u.ID, State: StateUnreadable, Assistant: u.Assistant,
			Age: age, Do: DoInspect, Why: "record_unreadable", StoredState: u.StoredState, Cause: u.Cause,
			Project: u.Project, Created: u.CreatedAt,
		})
	}
	// Ascending by id, which is what the Swift app sorts by and what makes the
	// digest reproducible without sorting inside it.
	sort.Slice(out, func(i, j int) bool { return out[i].Task < out[j].Task })
	return out, nil
}

func inRepository(r Record, repo string) bool {
	home := r.ProjectDir
	if r.Worktree != nil && r.Worktree.Repository != "" {
		home = r.Worktree.Repository
	}
	if r.Repository != "" {
		home = r.Repository
	}
	return home == repo || strings.HasPrefix(home, repo+"/")
}

// row decides one task's section, its `do` and its `why`.
//
// Every git fact here can be unknown, and unknown never authorises a removal:
// a branch listing that failed keeps the row visible, and a `dirty` nobody
// could read is not permission to dispose of a checkout. That rule is the
// reason this reads as much as it does.
func (b *Broker) row(ctx context.Context, r Record, now time.Time) InventoryRow {
	row := InventoryRow{
		Task:      r.ID,
		Title:     r.Title,
		State:     r.State,
		Assistant: r.Assistant,
		// `claims` is the lease: what this task reserves in the shared tree.
		// An isolated task reserves nothing there, and its declared list
		// travels as declared_writes instead (D21).
		Claims:     r.Lease(),
		Age:        r.Age(now),
		LeaseScope: r.Scope(),
	}
	if declared, known := r.DeclaredWrites(); known {
		row.DeclaredWrites = declared
	}
	if r.Root != nil {
		row.RootLabel = r.Root.Label
		row.RootKey = rootKey(r.Root)
	}
	if r.Worktree != nil {
		row.Branch = r.Worktree.Branch
		row.Head = r.Worktree.Head
		row.Path = r.Worktree.Path
		row.OnDisk = dirExists(r.Worktree.Path)
	}
	if r.Landing != nil {
		row.Landing = r.Landing.State
		row.Target = r.Landing.Target
		row.Obligation = b.Obligation(r)
	}

	if !r.State.Terminal() {
		row.Section = VisibilityLive
		row.Do = DoCoordinate
		return row
	}

	switch row.Landing {
	case LandingLanded, LandingIncorporated, LandingAbandoned, LandingNothingToLand:
		row.Section = VisibilitySettled
	}

	if r.Worktree == nil {
		if row.Section == "" {
			row.Section = VisibilitySettled
		}
		return row
	}

	exists, branchKnown := b.Git.BranchExists(ctx, r.Worktree.Repository, r.Worktree.Branch)
	row.Branched = exists
	// Merged into the target the record names, and into nothing else (D19).
	// The first version asked about the main checkout's HEAD, which is
	// whatever branch somebody last checked out there: a delivery could read
	// as merged because a person was standing on it, and as unmerged because
	// they were not. No target on the record is no answer.
	merged, mergedKnown := false, false
	if branchKnown && exists && row.Target != "" {
		merged, mergedKnown = b.Git.Merged(ctx, r.Worktree.Repository, r.Worktree.Branch, "refs/heads/"+row.Target)
	}
	if mergedKnown {
		row.Merged = &merged
	}
	commits, commitsKnown := b.Git.Commits(ctx, r.Worktree.Repository, r.Worktree.Base, r.Worktree.Branch)
	dirty, dirtyKnown := false, false
	kept := ""
	if row.OnDisk {
		dirty, dirtyKnown = b.Git.Dirty(ctx, r.Worktree.Path)
	} else {
		// A checkout the sweep took is not unknown: what it held was
		// recorded before it went (reclaim.go).
		dirty, kept, dirtyKnown = b.reclaimedDirty(ctx, r.ID)
	}

	// Droppable is decided first and fails safe: the branch must have been
	// readable, and the checkout must have been readable and clean. Anything
	// unknown is not permission.
	//
	// And never while the landing record says pending (D01): that record is
	// the one answer to "did this land", and a row filed as settled because
	// git shows the branch merged would be a second answer beside it — the
	// shape of the Swift app's 53 / 24 / 17. A merged branch whose landing
	// is still pending is unlanded, with `merged: true` saying what is left
	// to do: record it.
	if row.Landing != LandingPending && branchKnown && dirtyKnown && !dirty {
		switch {
		case !exists && row.OnDisk:
			row.Do = DoDispose
			row.Why = WhyOrphanedCheck
		case exists && mergedKnown && merged:
			row.Do = DoDispose
			if commitsKnown && commits == 0 {
				row.Why = WhyBranchEmpty
			} else {
				row.Why = WhyMergedClean
			}
		}
	}
	// And never while the session that worked in the checkout may still be
	// there (D13, G22): `dispose` is advice to remove it, and advice the
	// sweep would refuse is not advice. The beat's last reading answers.
	if row.Do == DoDispose {
		if gone, _, _ := b.ownerGone(ctx, b.lastReading(), r, r.Worktree.Path); !gone {
			row.Do, row.Why = "", ""
		}
	}
	if row.Do == DoDispose {
		row.Section = VisibilitySettled
		return row
	}

	if row.Section == VisibilitySettled {
		return row
	}
	if row.Landing == LandingPending {
		row.Section = VisibilityUnlanded
	} else if branchKnown && !exists {
		row.Section = VisibilitySettled
		return row
	} else if mergedKnown && merged {
		row.Section = VisibilitySettled
		return row
	} else {
		row.Section = VisibilityUnlanded
	}

	row.Do, row.Why = landingAdvice(r, commits, commitsKnown, dirty, dirtyKnown, kept)
	return row
}

// landingAdvice is `nothing_to_land` and the sentence that refuses it.
//
// The order is the Swift app's and each clause is a separate sentence on
// purpose: "this task declared paths" and "its branch carries commits" send a
// person to two different places. What the checkout itself shows is one
// sentence shared with the gate that admits `nothing_to_land`
// (deliveryEvidence, nothingToLandRefusal), because a reason the advice gives
// and a reason the refusal gives are answers to the same question and were
// two copies of it.
func landingAdvice(r Record, commits int, commitsKnown, dirty, dirtyKnown bool, kept string) (string, string) {
	// The lease, not the declared list: for a task in the shared tree they
	// are the same paths, and for an isolated one its branch is the evidence
	// of what it wrote, as it was before D21 kept the list.
	if n := len(r.Lease()); n > 0 {
		return DoLandOrAbandon, "this task declared " + strconv.Itoa(n) + " path(s) to write"
	}
	if r.Landing != nil && r.Landing.Target != "" {
		return DoLandOrAbandon, "its landing obligation already names the target " + r.Landing.Target
	}
	if r.Worktree != nil {
		if why := deliveryEvidence(commits, commitsKnown, dirty, dirtyKnown, kept); why != "" {
			return DoLandOrAbandon, why
		}
	}
	return DoNothingToLand, ""
}

// generation is the receipt a dispatch must carry back.
func generation(repo string, rows []InventoryRow) string {
	lines := []string{
		"clawdline-inventory-v" + strconv.Itoa(InventorySchema),
		"repository=" + repo,
	}
	rowLines := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.Section == VisibilitySettled && row.Do != DoDispose {
			continue
		}
		section := string(row.Section)
		if row.Do == DoDispose {
			section = "droppable"
		}
		paths := append([]string{}, row.Claims...)
		sort.Strings(paths)
		if row.Do == DoDispose {
			paths = nil
		}
		rowLines = append(rowLines, strings.Join([]string{
			section, row.Task, row.Branch, row.Why, row.Do, strings.Join(paths, ","),
		}, "\t"))
	}
	sort.Strings(rowLines)
	sum := sha256.Sum256([]byte(strings.Join(append(lines, rowLines...), "\n")))
	return hex.EncodeToString(sum[:])[:16]
}

// overlapWith is the caller's own claims intersected with a row's, by
// ancestry: a directory claim conflicts with a file inside it.
func overlapWith(rowClaims, asked []string) []string {
	if len(asked) == 0 {
		return nil
	}
	shared := []string{}
	for _, own := range rowClaims {
		for _, want := range asked {
			if covers(own, want) || covers(want, own) {
				shared = append(shared, own)
				break
			}
		}
	}
	sort.Strings(shared)
	return shared
}

// covers is claim ancestry.
func covers(parent, child string) bool {
	p := strings.TrimSuffix(parent, "/")
	c := strings.TrimSuffix(child, "/")
	return p == c || strings.HasPrefix(c, p+"/")
}

// rootKey is the first eight hex characters of the root's canonical name, the
// short handle a person uses to tell two roots apart on a busy machine.
func rootKey(root *RootRef) string {
	if root == nil || root.SessionID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(root.Assistant + "\x00" + root.SessionID))
	return hex.EncodeToString(sum[:])[:8]
}

// ParseClaims reads the `claims=` query: comma separated, trimmed, empties
// dropped.
func ParseClaims(raw string) []string {
	out := []string{}
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
