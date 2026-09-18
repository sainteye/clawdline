package orchestrator

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
)

// What a running task says about itself, and what settles it afterwards.
//
// Each route below is authenticated by a different credential, and the split is
// the design rather than an accident of history:
//
//   - a **child** proves it is itself with its own task secret, and may say
//     what it is doing, that it has finished, and that it wants somebody woken;
//   - the **machine** proves it is this Mac's broker with the orchestrator
//     token, and may acknowledge a notice and settle a landing;
//   - `landed` and `nothing_to_land` are machine-only even though the child
//     holds a secret, because a delivery may not certify its own arrival.

// Progress records one material boundary change.
func (b *Broker) Progress(ctx context.Context, id, secret, note string) (Record, error) {
	if _, _, err := b.Authenticate(ctx, id, secret); err != nil {
		return Record{}, err
	}
	trimmed := strings.TrimSpace(note)
	if trimmed == "" || utf8.RuneCountInString(trimmed) > progressLimit {
		return Record{}, refuse(http.StatusBadRequest, "bad_request",
			"note must be a non-empty sentence of at most "+strconv.Itoa(progressLimit)+" characters.")
	}
	var (
		stored store.BrokerNote
		added  bool
	)
	record, err := b.mutate(ctx, id, "task.briefed", func(r *Record) error {
		if r.State.Terminal() {
			return refuse(http.StatusConflict, "not_live",
				"This task is over; what it did belongs in its summary.")
		}
		// A repeated sentence is accepted and ignored rather than refused: a
		// child retrying a note it is unsure landed has done nothing wrong, and
		// the same sentence twice on a person's screen is noise.
		var err error
		if stored, added, err = b.Store.AppendBrokerNote(ctx, id, trimmed); err != nil {
			return err
		}
		// A note proves the child read its briefing, which is the one thing a
		// spawn cannot prove by itself: bytes reaching a tty are not evidence
		// anybody read them, and an authenticated sentence is.
		if r.State != StateSpawning && r.State != StateQueued {
			return errUnchanged
		}
		r.State = StateBriefed
		return nil
	})
	if err == nil && added {
		// After the note is durable, never before: a stream that shows a note
		// the store does not hold is a stream a reconnect contradicts.
		b.progress.publish(stored)
	}
	return record, err
}

// Notes reads what a task has said.
func (b *Broker) Notes(ctx context.Context, id string) ([]store.BrokerNote, error) {
	return b.Store.BrokerNotes(ctx, id, progressKept)
}

// Complete is the child announcing its own outcome.
//
// `status` is not validated. Anything that is not `success` is a failure —
// including a missing field and a word nobody can read — because the safe
// reading of an unreadable outcome is that the work did not land.
func (b *Broker) Complete(ctx context.Context, id, secret, status, summary string) error {
	if _, _, err := b.Authenticate(ctx, id, secret); err != nil {
		return err
	}
	state := StateFailure
	if status == "success" {
		state = StateSuccess
	}
	_, err := b.Settle(ctx, id, state, summary, nil)
	if errors.Is(err, errAlreadyTerminal) {
		return refuse(http.StatusConflict, "already_done", "That task already finished.")
	}
	return err
}

// Acknowledge stops the completion notice.
//
// It is the last rung of the receipt chain the whole product is built on:
// accepted, executed, delivered, observed, acknowledged. A transport success
// proves the fourth at best, so this is the only thing that ends the resend.
func (b *Broker) Acknowledge(ctx context.Context, id, noticeID string) (changed bool, err error) {
	if _, _, err := b.Record(ctx, id); err != nil {
		if ref, ok := err.(Refusal); ok && ref.Code == "orchestrator_store_unavailable" {
			return false, err
		}
		// The one route whose 404 names the id, as the Swift app's does.
		return false, refuse(http.StatusNotFound, "not_found", "No task named "+id+".")
	}
	// A compare-and-set against the transition this read saw, retried a few
	// times because the pump may move the notice between the read and the
	// write — and an ACK must not be lost to a race it would win on the next
	// read. Whatever the pump learned in the meantime, the ACK is what ends
	// the sequence.
	for tries := 0; tries < 4; tries++ {
		r, _, err := b.Record(ctx, id)
		if err != nil {
			return false, err
		}
		if r.Notice == nil {
			return false, refuse(http.StatusConflict, "completion_not_reconciled",
				"This terminal task has no durable completion envelope; reconcile it or poll result.json.")
		}
		if !constantEqual(r.Notice.ID, strings.ToLower(noticeID)) {
			return false, refuse(http.StatusConflict, "completion_notice_mismatch",
				"The notice id does not identify this task's completion envelope.")
		}
		if r.Notice.State == NoticeAcknowledged {
			return false, nil
		}
		now := b.now()
		if b.moveNotice(ctx, id, *r.Notice, "task.completion.acknowledged", func(n *Notice) {
			if n.ObservedAt.IsZero() {
				n.ObservedAt = now
			}
			n.AcknowledgedAt = now
			n.State = NoticeAcknowledged
			n.NextRetryAt = zeroTime
			n.LastError = nil
		}) {
			b.observed.forgetNotice(r.Notice.ID)
			return true, nil
		}
	}
	return false, refuse(http.StatusInternalServerError, "completion_store_failed",
		"The acknowledgement could not be persisted; retry it.")
}

// LandingRequest is the body of the landing route.
type LandingRequest struct {
	State    string
	Target   string
	Delivery string
	Commit   string
	Note     string
	// Machine is whether the orchestrator token was presented.
	Machine bool
	// Secret is the task secret, header only on this route.
	Secret string
}

// Land settles the obligation a delivery leaves behind.
//
// `landed` is proved, never asserted. The proof is ancestry, asked of git, in
// the repository the work was done in — a branch existing, a diff being empty
// and a delivery being marked done are all compatible with the work never
// having reached the target.
func (b *Broker) Land(ctx context.Context, id string, req LandingRequest) (Record, error) {
	r, hash, err := b.Record(ctx, id)
	if err != nil {
		return Record{}, err
	}
	// What the checks below were decided against. The proof asks git, which
	// is slow, so the write re-reads the record and refuses if either of these
	// moved in the meantime — the Swift app's `stale_write`.
	readState := r.State
	readLanding := landingKey(r.Landing)
	machineOnly := req.State == string(LandingLanded) || req.State == string(LandingNothingToLand)
	if machineOnly {
		if !req.Machine {
			return Record{}, refuse(http.StatusForbidden, "forbidden",
				"Only the orchestrator token may settle a landing on a repository's behalf.")
		}
	} else if !req.Machine && !SecretMatches(hash, req.Secret) {
		return Record{}, refuse(http.StatusForbidden, "forbidden",
			"Use this task's secret or the orchestrator token.")
	}

	switch LandingState(req.State) {
	case LandingPending, LandingLanded, LandingAbandoned, LandingNothingToLand:
	default:
		return Record{}, refuse(http.StatusBadRequest, "bad_request",
			"state must be pending, landed, abandoned, or nothing_to_land.")
	}
	if req.Commit != "" && req.State != string(LandingLanded) {
		return Record{}, refuse(http.StatusBadRequest, "bad_request",
			"commit is valid only when state is landed.")
	}
	if req.Target != "" && req.State == string(LandingNothingToLand) {
		return Record{}, refuse(http.StatusBadRequest, "bad_request",
			"target is not valid when state is nothing_to_land.")
	}
	if req.State != string(LandingPending) && !r.State.Terminal() {
		return Record{}, refuse(http.StatusConflict, "not_terminal",
			"Only a terminal task can settle its landing obligation.")
	}

	next := &Landing{State: LandingState(req.State), Target: req.Target, Note: req.Note}
	if r.Landing != nil && next.Target == "" {
		next.Target = r.Landing.Target
	}
	if r.Landing != nil && r.Landing.State != LandingPending && r.Landing.State != "" {
		if r.Landing.State != next.State {
			return Record{}, refuse(http.StatusConflict, "invalid_transition",
				"A settled obligation cannot move to another state; open a new task.")
		}
		if next.State == LandingLanded && next.Target != r.Landing.Target {
			return Record{}, refuseWith(http.StatusConflict, "landing_conflict",
				"This obligation is already settled with a different target. A landing aimed somewhere "+
					"else is another claim rather than a correction of this one; record it against its own task.",
				map[string]any{"field": "target", "stored": r.Landing.Target, "requested": next.Target})
		}
	}

	switch next.State {
	case LandingLanded:
		if req.Commit == "" {
			return Record{}, refuse(http.StatusBadRequest, "bad_request",
				"commit is required when state is landed.")
		}
		if next.Target == "" {
			return Record{}, refuse(http.StatusBadRequest, "bad_request",
				"target is required when state is landed.")
		}
		repo := r.Repository
		if r.Worktree != nil && r.Worktree.Repository != "" {
			repo = r.Worktree.Repository
		}
		commit, target, err := b.proveLanding(ctx, repo, req.Commit, next.Target)
		if err != nil {
			return Record{}, err
		}
		// The ids git resolved replace the caller's text. A record that keeps
		// what somebody typed is a record that cannot be compared with the
		// repository later.
		next.Commit = commit
		next.Repo = repo
		next.At = b.now()
		_ = target
	case LandingNothingToLand:
		if why := b.nothingToLandRefusal(ctx, r); why != "" {
			return Record{}, refuse(http.StatusConflict, "wrote_to_repository",
				"nothing_to_land says this task wrote nothing to land, and "+why+".")
		}
		next.At = b.now()
	case LandingAbandoned:
		next.At = b.now()
	}

	return b.mutate(ctx, id, "landing."+req.State, func(now *Record) error {
		if now.State != readState || landingKey(now.Landing) != readLanding {
			return refuse(http.StatusConflict, "stale_write",
				"The landing changed while its target was being verified; retry.")
		}
		now.Landing = next
		return nil
	})
}

// landingKey is a landing's identity, for noticing that it moved.
func landingKey(l *Landing) string {
	if l == nil {
		return ""
	}
	return string(l.State) + "\x00" + l.Target + "\x00" + l.Commit
}

// proveLanding asks git the only question that means "landed".
func (b *Broker) proveLanding(ctx context.Context, repo, commit, target string) (string, string, error) {
	unverified := refuse(http.StatusConflict, "unverified_landing",
		"The commit must resolve in the task repository and be contained by the named local target branch.")
	if repo == "" || !b.Git.ValidBranchName(ctx, target) {
		return "", "", unverified
	}
	commitID, err := b.Git.ResolveCommit(ctx, repo, commit)
	if err != nil {
		return "", "", unverified
	}
	targetID, err := b.Git.ResolveCommit(ctx, repo, "refs/heads/"+target)
	if err != nil {
		return "", "", unverified
	}
	ok, err := b.Git.IsAncestor(ctx, repo, commitID, targetID)
	if err != nil || !ok {
		return "", "", unverified
	}
	return commitID, targetID, nil
}

// nothingToLandRefusal is the sentence that says a task did write something.
// Empty means the claim is admitted.
func (b *Broker) nothingToLandRefusal(ctx context.Context, r Record) string {
	if n := len(r.Claims); n > 0 {
		return "this task declared " + strconv.Itoa(n) + " path(s) to write"
	}
	if r.Landing != nil && r.Landing.Target != "" {
		return "its landing obligation already names the target " + r.Landing.Target
	}
	if r.Worktree == nil {
		return ""
	}
	commits, commitsKnown := b.Git.Commits(ctx, r.Worktree.Repository, r.Worktree.Base, r.Worktree.Branch)
	dirty, dirtyKnown := b.Git.Dirty(ctx, r.Worktree.Path)
	if !commitsKnown || !dirtyKnown {
		return "this Mac has no commit count for its checkout, and an unknown count is not permission"
	}
	if commits > 0 {
		return "its branch carries " + strconv.Itoa(commits) + " commit(s)"
	}
	if dirty {
		return "its checkout has uncommitted changes"
	}
	return ""
}

// NotifyResult is what a push answered.
type NotifyResult struct {
	Sent   int
	Failed int
}

// AgentNotify wakes the person, at most five times per task.
//
// Every gate here is a refusal a child must be able to tell apart: a disabled
// setting is not its fault, an exhausted allowance is not a fault at all, and
// a machine with nothing subscribed is a fact about the machine. None of them
// is a reason to retry, and the briefing says so.
func (b *Broker) AgentNotify(ctx context.Context, id, secret, title, body string) (NotifyResult, error) {
	r, _, err := b.Authenticate(ctx, id, secret)
	if err != nil {
		return NotifyResult{}, err
	}
	if r.State.Terminal() {
		if r.FinishedAt.IsZero() || b.now().Sub(r.FinishedAt) > notifyGrace {
			return NotifyResult{}, refuse(http.StatusConflict, "notify_expired",
				"That task's notification window has expired.")
		}
	}
	if strings.TrimSpace(title) == "" || utf8.RuneCountInString(title) > notifyTitleLimit {
		return NotifyResult{}, refuse(http.StatusBadRequest, "bad_request",
			"title must be non-empty and at most 80 characters.")
	}
	if strings.TrimSpace(body) == "" || utf8.RuneCountInString(body) > notifyBodyLimit {
		return NotifyResult{}, refuse(http.StatusBadRequest, "bad_request",
			"body must be non-empty and at most 500 characters.")
	}
	if b.Notify == nil {
		return NotifyResult{}, refuse(http.StatusConflict, "not_subscribed",
			"No device has asked for notifications yet.")
	}
	perTask, perHour, err := b.Store.NotificationCounts(ctx, id)
	if err != nil {
		return NotifyResult{}, err
	}
	if perTask >= notifyTaskLimit {
		return NotifyResult{}, refuse(http.StatusTooManyRequests, "notify_limit",
			"That task has sent its five notifications.")
	}
	if perHour >= notifyHourLimit {
		return NotifyResult{}, refuse(http.StatusTooManyRequests, "rate_limited",
			"Too many agent notifications; wait for the hourly window.")
	}
	label := r.Title
	if label == "" && r.Root != nil {
		label = r.Root.Label
	}
	sent, failed, err := b.Notify(ctx, label+": "+title, body, "agent-task-"+id)
	if err != nil {
		return NotifyResult{}, refuse(http.StatusBadGateway, "push_failed",
			"One or more push services did not accept the notification.")
	}
	if sent == 0 && failed == 0 {
		return NotifyResult{}, refuse(http.StatusConflict, "not_subscribed",
			"No device has asked for notifications yet.")
	}
	if _, _, err := b.Store.RecordNotification(ctx, id, title, body); err != nil {
		return NotifyResult{}, err
	}
	if failed > 0 {
		return NotifyResult{Sent: sent, Failed: failed}, refuseWith(http.StatusBadGateway, "push_failed",
			"One or more push services did not accept the notification.",
			map[string]any{"sent": sent, "failed": failed})
	}
	return NotifyResult{Sent: sent, Failed: failed}, nil
}

// Inflight is every line of work outstanding in one repository.
//
// A task never sees itself in its own answer: it asked what else is going on,
// and a row naming the asker is the one row it cannot act on.
func (b *Broker) Inflight(ctx context.Context, repo, exclude string) ([]InventoryRow, error) {
	rows, err := b.rows(ctx, repo)
	if err != nil {
		return nil, err
	}
	out := []InventoryRow{}
	for _, row := range rows {
		if row.Task == exclude {
			continue
		}
		switch row.Section {
		case VisibilityLive:
			out = append(out, row)
		case VisibilityUnlanded:
			row.Section = VisibilityUnmerged
			out = append(out, row)
		}
	}
	return out, nil
}

// InflightFor is the per-task form: the repository comes from the task, so a
// child cannot ask about a repository it was not sent to.
func (b *Broker) InflightFor(ctx context.Context, id, secret string) (string, []InventoryRow, error) {
	r, _, err := b.Authenticate(ctx, id, secret)
	if err != nil {
		return "", nil, err
	}
	repo := r.Repository
	if repo == "" {
		resolved, err := b.Git.Toplevel(ctx, r.ProjectDir)
		if err != nil {
			return "", nil, refuse(http.StatusConflict, "not_a_repository",
				"This task's directory is not inside a Git repository.")
		}
		repo = resolved
	}
	rows, err := b.Inflight(ctx, repo, id)
	return repo, rows, err
}

func constantEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
