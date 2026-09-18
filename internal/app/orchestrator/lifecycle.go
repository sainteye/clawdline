package orchestrator

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/taskdir"
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
	at := b.now()
	record, _, err := b.mutateTx(ctx, id, "task.briefed", func(tx *store.Tx, r *Record) ([]store.Effect, error) {
		if r.State.Terminal() {
			return nil, refuse(http.StatusConflict, "not_live",
				"This task is over; what it did belongs in its summary.")
		}
		// A repeated sentence is accepted and ignored rather than refused: a
		// child retrying a note it is unsure landed has done nothing wrong, and
		// the same sentence twice on a person's screen is noise. The note and
		// what it proves are one transaction.
		var err error
		if stored, added, err = tx.AppendNote(id, trimmed, at); err != nil {
			return nil, err
		}
		// A note proves the child read its briefing, which is the one thing a
		// spawn cannot prove by itself: bytes reaching a tty are not evidence
		// anybody read them, and an authenticated sentence is.
		if r.State != StateSpawning && r.State != StateQueued {
			// The record stays as it is; the note, if new, still commits.
			return nil, errUnchanged
		}
		r.State = StateBriefed
		return nil, nil
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

// Accept is the child signing for its briefing (docs/design-decisions.md D10).
//
// It is the first rung of the receipt chain — accepted, executed, delivered,
// observed, acknowledged — and until it existed that rung was empty. What
// stood in for it was weaker on both sides: bytes reaching a tty prove
// nothing was read (the shell once answered a briefing with
// `command not found: Your`), and a tab that starts a turn proves only that
// something is running — it may be a dialog, or a child that was already busy.
// Both of those are kept as evidence of **life** (observe.go), and neither
// moves a task to `briefed` any more. A progress note still does: it is signed
// with the same secret, so it says the same thing and more.
//
// Idempotent. A child retrying a receipt it is unsure landed has done nothing
// wrong, and the second one changes nothing.
func (b *Broker) Accept(ctx context.Context, id, secret string) (Record, error) {
	if _, _, err := b.Authenticate(ctx, id, secret); err != nil {
		return Record{}, err
	}
	now := b.now()
	return b.mutate(ctx, id, "task.accepted", func(r *Record) error {
		if r.State.Terminal() {
			return refuse(http.StatusConflict, "not_live",
				"This task is over; a receipt for its briefing changes nothing now.")
		}
		changed := false
		if r.AcceptedAt.IsZero() {
			r.AcceptedAt = now
			changed = true
		}
		if r.State == StateSpawning || r.State == StateQueued {
			r.State = StateBriefed
			changed = true
		}
		if !changed {
			return errUnchanged
		}
		return nil
	})
}

// Complete is the child asking for its result to be collected now.
//
// It carries nothing and settles nothing by itself (D15). `result.json` is the
// one completion signal: the first version of this route took `status` and
// `summary` from the request body and settled on them, and because it usually
// arrived before the beat's five-second look at the file, the beat then found
// the task finished and skipped it — so the child's `symbols`, `artifacts`,
// `verification` and a review node's typed verdict never reached the record.
// Now this does exactly what the beat would do a few seconds later, and says
// plainly when there is nothing to collect.
func (b *Broker) Complete(ctx context.Context, id, secret string) error {
	r, _, err := b.Authenticate(ctx, id, secret)
	if err != nil {
		return err
	}
	if r.State.Terminal() {
		return refuse(http.StatusConflict, "already_done", "That task already finished.")
	}
	settled, err := b.collect(ctx, r)
	switch {
	case settled, errors.Is(err, errAlreadyTerminal):
		// The beat may have collected the same file a moment ago; either way
		// the file this caller wrote is what the record now says.
		return nil
	case errors.Is(err, taskdir.ErrNoResult):
		return refuse(http.StatusConflict, "result_not_written",
			"There is no result.json to collect. It is the completion signal: validate it and rename it "+
				"into place first; this route only asks for it to be collected now rather than on the next beat.")
	case errors.Is(err, errResultNotAuthentic):
		return refuse(http.StatusConflict, "result_rejected",
			"result.json is there but is not this task's: its protocol, task_id, status or task_secret "+
				"does not match. Nothing was settled.")
	case errors.Is(err, errResultUnreadable):
		return refuse(http.StatusConflict, "result_unreadable",
			"result.json is there but is not readable JSON. Nothing was settled.")
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
		if IsUnreadable(err) {
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
// `landed` is proved, never asserted, and what it proves is that **this
// task's** work reached the target (D17, landing.go) — not merely that some
// commit is on it, which the task's own base always is.
//
// A settled landing can be corrected and cannot be overwritten in silence
// (D18). A resend that says what the record says is a replay: nothing is
// written, nothing is re-proved, and the answer is the record. A resend that
// differs is a write, and a write has two honest answers — applied or refused
// — so it passes the gate the first one passed, and what it replaced is kept
// as `corrected_from` and in a `landing.corrected` event. The Swift app once
// answered `ok` to a correction it had not applied, and a record on that
// machine came to name another task's commit for good.
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

	var prev *Landing
	if r.Landing != nil && r.Landing.State != "" {
		held := *r.Landing
		prev = &held
	}
	// Settled is every state but pending: a claim about the repository that
	// stands until it is corrected through its own gate.
	settled := prev != nil && prev.State != LandingPending

	next := &Landing{State: LandingState(req.State), Target: req.Target, Note: req.Note}
	// The target is the record's once the root has named one (D19): a
	// request that leaves it out means the one on the record, and one that
	// names another on a settled landing is a different claim.
	if prev != nil && next.Target == "" {
		next.Target = prev.Target
	}
	// A note left out of a resend of the same state is the note on the
	// record. Moving to another state starts that state's own sentence.
	if prev != nil && prev.State == next.State && next.Note == "" {
		next.Note = prev.Note
	}
	if settled {
		if prev.State != next.State {
			return Record{}, refuse(http.StatusConflict, "invalid_transition",
				"A settled obligation cannot move to another state; open a new task.")
		}
		if next.State == LandingLanded && next.Target != prev.Target {
			return Record{}, refuseWith(http.StatusConflict, "landing_conflict",
				"This obligation is already settled with a different target. A landing aimed somewhere "+
					"else is another claim rather than a correction of this one; record it against its own task.",
				map[string]any{"field": "target", "stored": prev.Target, "requested": next.Target})
		}
	}

	var head branchHead
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
		// The replay is recognised by what git says the commit is, not by
		// its spelling: a short id of the recorded commit is the same claim.
		// It is not proved again — the record stands on the proof it was
		// written with, and a target that has moved on since does not make
		// a resend of it a lie.
		if settled {
			if c, err := b.Git.ResolveCommit(ctx, repo, req.Commit); err == nil && c == prev.Commit &&
				next.Note == prev.Note {
				return r, nil
			}
		}
		var proof landingProof
		proof, head, err = b.proveDelivery(ctx, r, req.Commit, next.Target)
		if err != nil {
			return Record{}, err
		}
		// The ids git resolved replace the caller's text. A record that keeps
		// what somebody typed is a record that cannot be compared with the
		// repository later.
		next.Commit = proof.commit
		next.Repo = repo
		next.TargetCommit = proof.targetCommit
		next.DeliveryHead = proof.deliveryHead
		next.Base = proof.base
		next.At = b.now()
	case LandingNothingToLand:
		if settled && next.sameAs(*prev) {
			return r, nil
		}
		// A correction of it is held to the same gate as the claim itself.
		if why := b.nothingToLandRefusal(ctx, r); why != "" {
			return Record{}, refuse(http.StatusConflict, "wrote_to_repository",
				"nothing_to_land says this task wrote nothing to land, and "+why+".")
		}
		next.At = b.now()
	case LandingAbandoned:
		if settled && next.sameAs(*prev) {
			return r, nil
		}
		next.At = b.now()
	case LandingPending:
		if prev != nil && next.sameAs(*prev) {
			return r, nil
		}
	}

	kind := "landing." + req.State
	extra := map[string]any{"landing": string(next.State), "target": next.Target}
	if next.Commit != "" {
		extra["commit"] = next.Commit
	}
	if settled {
		// A correction keeps what it replaced; and when only the words
		// changed, the work landed when it landed, not when it was annotated.
		next.CorrectedFrom = prev.replaced()
		if prev.Commit == next.Commit && !prev.At.IsZero() {
			next.At = prev.At
		}
		kind = "landing.corrected"
		// The commit pair is the fact; the words it replaced are on the
		// record's corrected_from, and prose is not copied into events.
		extra["previous_commit"] = prev.Commit
	}

	record, _, err := b.mutateEvent(ctx, id, kind, extra, func(_ *store.Tx, now *Record) ([]store.Effect, error) {
		if now.State != readState || landingKey(now.Landing) != readLanding {
			// Another caller wrote this landing while the proof ran. If what
			// it wrote is what this one says, this is a replay of it;
			// anything else was decided against a record that is gone.
			if now.Landing != nil && now.Landing.sameAs(*next) {
				return nil, errUnchanged
			}
			return nil, refuse(http.StatusConflict, "stale_write",
				"The landing changed while its target was being verified; retry.")
		}
		now.Landing = next
		// What the proof read of the branch is the delivery now (G17).
		if head.known && head.commit != "" && now.Worktree != nil && now.Worktree.Branch == head.branch {
			now.Worktree.Head = head.commit
		}
		return nil, nil
	})
	return record, err
}

// landingKey is a landing's identity, for noticing that it moved.
func landingKey(l *Landing) string {
	if l == nil {
		return ""
	}
	return string(l.State) + "\x00" + l.Target + "\x00" + l.Commit + "\x00" + l.Note
}

// nothingToLandRefusal is the sentence that says a task did write something.
// Empty means the claim is admitted.
func (b *Broker) nothingToLandRefusal(ctx context.Context, r Record) string {
	// The lease, as in landingAdvice: an isolated task's branch answers this.
	if n := len(r.Lease()); n > 0 {
		return "this task declared " + strconv.Itoa(n) + " path(s) to write"
	}
	if r.Landing != nil && r.Landing.Target != "" {
		return "its landing obligation already names the target " + r.Landing.Target
	}
	if r.Worktree == nil {
		return ""
	}
	commits, commitsKnown := b.Git.Commits(ctx, r.Worktree.Repository, r.Worktree.Base, r.Worktree.Branch)
	var dirty, dirtyKnown bool
	kept := ""
	if dirExists(r.Worktree.Path) {
		dirty, dirtyKnown = b.Git.Dirty(ctx, r.Worktree.Path)
	} else {
		// Taken by the sweep, which recorded what it held (reclaim.go).
		dirty, kept, dirtyKnown = b.reclaimedDirty(ctx, r.ID)
	}
	if !commitsKnown || !dirtyKnown {
		return "this Mac has no commit count for its checkout, and an unknown count is not permission"
	}
	if commits > 0 {
		return "its branch carries " + strconv.Itoa(commits) + " commit(s)"
	}
	if dirty {
		if kept != "" {
			return "its checkout had uncommitted changes, kept on the branch " + kept + " when it was reclaimed"
		}
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
	if b.Push == nil {
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
	if err := outside(); err != nil {
		return NotifyResult{}, err
	}
	// Tapping it opens the root the task reports to, when that root is a
	// session this machine is watching (D24).
	sent, failed, err := b.Push(ctx, label+": "+title, body, r.RootTerminalID, "agent-task-"+id)
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
		case VisibilityLive, VisibilityUnreadable:
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
