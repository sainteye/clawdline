package orchestrator

import (
	"context"
	"errors"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// The landings the broker records by itself.
//
// A landing was settled only when somebody asked: a root that forgot, or a
// root that tried to close. Read in root transcripts on 2026-09-25: one root
// was refused sixteen times, the person pressed close six times and got
// `409 close_blocked` each time, and settling took 48 calls through
// `commit_unresolved`, `bad_request`, `carrier_commit_mismatch` and
// `nothing_delivered` — for nineteen rows whose code had long been merged.
// Only the record was missing, and the record is a question git answers.
//
// So the beat asks it. For a finished task whose landing is still pending and
// whose delivery branch can be read, when that branch's head is an ancestor of
// its target, the broker records `landed` through the route's own gate
// (land): whatever the route would refuse, this refuses too, because it is the
// same code. It records nothing else. An empty delivery, a cherry-picked one
// (not an ancestor), a branch that is gone, a repository that cannot be read
// and a target it cannot name are each left pending, for a root to decide.
//
// The target is the record's when the root named one (D19). A pending landing
// opens without one, and the broker does not fill it with the repository's
// HEAD. It names a target only when git proves there is exactly one branch it
// could be: the one local branch, outside the broker's own task branches and
// outside every branch checked out in a linked worktree (somebody's
// integration still in progress), whose history holds the delivery. Two such
// branches, or none, is a root's decision.

const (
	// landingDetectEvery is how many passes apart the beat looks, starting
	// with the first: once a minute at the five-second tick. A merge is not
	// in a hurry to be recorded, and the question costs a few git processes a
	// task.
	landingDetectEvery = 12
	// landingDetectLimit is how many pending landings one look puts git
	// questions to. The rest are the next look's, which starts after the
	// last one examined, so every row is reached however long the list is.
	landingDetectLimit = 16
	// landingDetectTimeout bounds the git questions about one task. One
	// repository that hangs costs one task's slot, not the pass.
	landingDetectTimeout = 10 * time.Second
)

// landingDetectNote is the note a detected landing carries.
const landingDetectNote = "recorded by the broker: the delivery branch was merged into its target"

// landingDetectState is the detector's memory: where the last look stopped,
// and what it last said about each task in the log. In memory only — a restart
// looks again from the start and may say each failure once more.
type landingDetectState struct {
	mu     sync.Mutex
	cursor string
	logged map[string]string
	// said is how many lines it has logged, for a test to count.
	said int
}

// detectLandingsDue runs a look on the passes it is due.
func (b *Broker) detectLandingsDue(ctx context.Context, number int64) int {
	if number%landingDetectEvery != 1 {
		return 0
	}
	return b.detectLandings(ctx)
}

// detectLandings is one look: at most landingDetectLimit pending landings,
// each asked of git under its own deadline. It answers how many it recorded.
func (b *Broker) detectLandings(ctx context.Context) int {
	// Only the rows that owe a landing: the beat's cost stays what is still
	// open, never the length of the history (G33).
	rows, err := b.Store.BrokerTasksOwingLanding(ctx)
	if err != nil {
		return 0
	}
	var owed []Record
	for _, row := range rows {
		if r, err := Decode(row.Record); err == nil && detectable(r) {
			owed = append(owed, r)
		}
	}
	sort.Slice(owed, func(i, j int) bool { return owed[i].ID < owed[j].ID })

	s := &b.landingDetect
	s.mu.Lock()
	cursor := s.cursor
	// A task no longer owed has nothing left to be said about it.
	still := make(map[string]bool, len(owed))
	for _, r := range owed {
		still[r.ID] = true
	}
	for id := range s.logged {
		if !still[id] {
			delete(s.logged, id)
		}
	}
	s.mu.Unlock()

	// Round robin: start after the last one examined, wrap once.
	start := sort.Search(len(owed), func(i int) bool { return owed[i].ID > cursor })
	landed := 0
	for n := 0; n < len(owed) && n < landingDetectLimit; n++ {
		if ctx.Err() != nil {
			break
		}
		r := owed[(start+n)%len(owed)]
		s.mu.Lock()
		s.cursor = r.ID
		s.mu.Unlock()
		ok, err := b.detectLanding(ctx, r)
		b.sayOnce(r.ID, err)
		if ok {
			landed++
		}
	}
	return landed
}

// detectable is a record the detector may look at: finished, owing a
// pending landing, on a branch of its own.
func detectable(r Record) bool {
	return r.State.Terminal() && r.Landing != nil && r.Landing.State == LandingPending &&
		r.Worktree != nil && r.Worktree.Branch != "" && r.Worktree.Repository != "" && r.Worktree.Base != ""
}

// detectLanding asks git about one task and records `landed` when the delivery
// is on its target. false with a nil error is "not landed yet, or not the
// broker's to say"; an error is a question git could not answer, or a gate
// that refused what git had just shown, and is said once.
func (b *Broker) detectLanding(parent context.Context, r Record) (bool, error) {
	ctx, cancel := context.WithTimeout(parent, landingDetectTimeout)
	defer cancel()
	w := r.Worktree
	repo := w.Repository

	head := b.deliveryHead(ctx, w)
	if !head.known {
		return false, errors.New("its delivery branch " + w.Branch + " could not be read")
	}
	if head.commit == "" {
		// The branch is gone: what it held is a root's to say.
		return false, nil
	}
	if under, err := b.Git.IsAncestor(ctx, repo, head.commit, w.Base); err != nil {
		return false, errors.New("git could not compare its delivery with its base: " + err.Error())
	} else if under {
		// An empty delivery: nothing_to_land or abandoned, and not ours.
		return false, nil
	}

	target := r.Landing.Target
	if target == "" {
		named, err := b.onlyTarget(ctx, repo, head.commit)
		if err != nil {
			return false, err
		}
		if named == "" {
			return false, nil
		}
		target = named
	} else if on, err := b.Git.IsAncestor(ctx, repo, head.commit, "refs/heads/"+target); err != nil {
		return false, errors.New("git could not compare its delivery with " + target + ": " + err.Error())
	} else if !on {
		return false, nil
	}
	tip, err := b.Git.ResolveCommit(ctx, repo, "refs/heads/"+target)
	if err != nil {
		return false, errors.New("its target " + target + " could not be resolved")
	}

	// The route's own gate, with the orchestrator's credential, which is what
	// the broker is. Land (the exported one) is not called: a landing the
	// root recorded closes the root's completion notice as read, and the
	// broker noticing a merge is not the root reading anything.
	_, err = b.land(ctx, r.ID, LandingRequest{State: string(LandingLanded), Target: target, Commit: tip,
		Note: landingDetectNote, Machine: true})
	if err != nil {
		var ref Refusal
		if errors.As(err, &ref) && ref.Code == "stale_write" {
			// Somebody wrote this landing while git was asked; the next look
			// reads what they wrote.
			return false, nil
		}
		return false, errors.New("the landing gate refused a merged delivery: " + err.Error())
	}
	return true, nil
}

// onlyTarget names the one branch a delivery can have landed on, or "" when
// git shows none or more than one.
func (b *Broker) onlyTarget(ctx context.Context, repo, head string) (string, error) {
	branches, err := b.Git.BranchesContaining(ctx, repo, head)
	if err != nil {
		return "", errors.New("git could not list the branches that hold its delivery: " + err.Error())
	}
	if len(branches) == 0 {
		return "", nil
	}
	checkouts, err := b.Git.Worktrees(ctx, repo)
	if err != nil {
		return "", errors.New("git could not list the repository's checkouts: " + err.Error())
	}
	// Every checkout after the first is linked, and a branch checked out in
	// one is somebody's work in progress — an integration not yet finished.
	busy := map[string]bool{}
	for i, c := range checkouts {
		if i > 0 && c.Branch != "" && filepath.Clean(c.Path) != filepath.Clean(repo) {
			busy[strings.TrimPrefix(c.Branch, "refs/heads/")] = true
		}
	}
	tasks := BranchName("")
	named := ""
	for _, name := range branches {
		if strings.HasPrefix(name, tasks) || busy[name] {
			continue
		}
		if named != "" {
			return "", nil
		}
		named = name
	}
	return named, nil
}

// sayOnce logs what went wrong with one task the first time it is said, and
// forgets it once the task looks fine again.
func (b *Broker) sayOnce(id string, err error) {
	s := &b.landingDetect
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		delete(s.logged, id)
		return
	}
	msg := err.Error()
	if s.logged[id] == msg {
		return
	}
	if s.logged == nil {
		s.logged = map[string]string{}
	}
	s.logged[id] = msg
	s.said++
	log.Printf("orchestrator: landing detection: task %s: %s", id, msg)
}
