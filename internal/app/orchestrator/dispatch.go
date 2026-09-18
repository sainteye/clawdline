package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/projects"
	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/taskdir"
	"github.com/sainteye/clawdline-go/internal/adapters/terminal"
)

// DispatchRequest is the whole HTTP body: three fields.
//
// The brief is not in it. A caller writes `<task root>/<id>/task.json` and
// then posts this, which is why `secret` can travel one way only — the broker
// never returns it, and the child receives it in the one line typed into its
// composer.
type DispatchRequest struct {
	TaskID     string
	Secret     string
	Generation string
	// Offered is whether the caller sent `inventory_generation` at all. Absent
	// and wrong are two different sentences in the refusal.
	Offered bool
	// Respawn is set only by Respawn: the spawn_failed task this dispatch
	// retries. Such a dispatch carries no inventory receipt of its own — the
	// caller asked to retry a task, not to start new work, and the task.json
	// it runs is the one the original dispatch was already admitted with.
	Respawn *RespawnOrigin
	// Schedule is set only by DispatchScheduled: the schedule whose
	// occurrence this is. Such a dispatch has no root and carries no
	// inventory receipt — nobody read an inventory, a clock did — and its
	// task.json is the one the broker wrote from the stored template.
	// Everything else is the same door: the same arbitration, the same
	// record, briefing, clock and collection (D07).
	Schedule *ScheduleOrigin
}

// ScheduleOrigin is the schedule a scheduled dispatch belongs to.
type ScheduleOrigin struct {
	ID    string
	Title string
}

// RespawnOrigin is where a respawned task came from.
type RespawnOrigin struct {
	TaskID     string
	Generation int
}

// Warning is a non-blocking thing the dispatcher should know. It is not a
// refusal: the work started.
type Warning struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Task    string   `json:"task,omitempty"`
	Paths   []string `json:"paths,omitempty"`
	Age     int      `json:"age_seconds,omitempty"`
	RootKey string   `json:"root_key,omitempty"`
}

// Dispatched is what a successful dispatch answers with.
type Dispatched struct {
	Record   Record
	Warnings []Warning
	Replayed bool
}

// Dispatch is the whole route.
//
// The order below is the design, not an implementation detail:
//
//  1. everything that can refuse, refuses before anything exists;
//  2. the record is written, durably, while the task is still `queued`;
//  3. only then is a terminal opened and typed into.
//
// A crash between 2 and 3 leaves a task that is known and unstarted. A crash
// the other way round — the Swift app's first shape — leaves a session running
// that no broker knows about, and the only way to find it is a person reading
// tabs.
func (b *Broker) Dispatch(ctx context.Context, req DispatchRequest) (Dispatched, error) {
	if !IsTaskID(req.TaskID) {
		return Dispatched{}, refuse(http.StatusUnprocessableEntity, "bad_task",
			"task_id must be a lowercase UUID.")
	}
	// An id this broker already holds is a resend, and a resend starts nothing
	// twice. This is checked before the rate window and before the inventory
	// receipt, because a caller retrying a dispatch it is unsure landed must
	// not be punished for asking again.
	//
	// Only "never heard of it" goes on. A row that is there and cannot be
	// read is not an absent one (409 task_unreadable), and a store that did
	// not answer has said nothing about the id at all (503): the first version
	// of this check let both through, and the save at the end upserted a new
	// task over the row it had failed to read.
	held, _, err := b.Record(ctx, req.TaskID)
	switch {
	case err == nil:
		return Dispatched{Record: held, Replayed: true}, nil
	case !isNotFound(err):
		return Dispatched{}, err
	}
	if !IsTaskSecret(req.Secret) {
		return Dispatched{}, refuse(http.StatusUnprocessableEntity, "bad_task",
			"secret must be 64 hex characters.")
	}

	record, err := b.readDraft(req.TaskID, req.Schedule != nil)
	if err != nil {
		return Dispatched{}, err
	}
	if req.Schedule != nil {
		record.ScheduleID = req.Schedule.ID
		record.ScheduleTitle = truncate(req.Schedule.Title, rootLabelLimit)
	}
	// Fixed before anything is arbitrated, so the arbitration below asks the
	// same question of this task that it asks of every live one (D21).
	record.LeaseScope = LeaseShared
	if record.Isolation == IsolationWorktree {
		record.LeaseScope = LeaseWorktree
	}

	// The inventory receipt is checked once the brief is readable, because the
	// refusal has to carry the inventory for *this task's* repository and the
	// brief is where that repository is named.
	switch {
	case req.Respawn == nil && req.Schedule == nil:
		if err := b.checkGeneration(ctx, record.ProjectDir, req); err != nil {
			return Dispatched{}, err
		}
	case req.Respawn != nil:
		record.RespawnOf = req.Respawn.TaskID
		record.RespawnGeneration = req.Respawn.Generation
	}
	if err := b.admitDispatch(); err != nil {
		return Dispatched{}, err
	}
	refund := true
	defer func() {
		if refund {
			b.refundDispatch()
		}
	}()

	// The owner has to be a session that is actually here. A child grouped
	// under a root nobody can find finishes into silence: the completion notice
	// has no recipient, the close cascade has nothing to close, and the person
	// who asked for the work never hears that it is done. Resolving it now
	// turns that into a refusal the dispatcher can act on, rather than a
	// discovery somebody makes an hour later.
	rootTerminal, err := b.resolveRoot(ctx, record.Root)
	if err != nil {
		return Dispatched{}, err
	}
	record.RootTerminalID = rootTerminal

	live, err := b.liveTasks(ctx)
	if err != nil {
		return Dispatched{}, err
	}
	warnings := []Warning{}

	// Capacity, per root and per machine. Both are 429 with a retry_after,
	// because "busy" without a number sends a caller into a loop.
	if record.Root != nil {
		mine := 0
		for _, other := range live {
			if other.Root != nil && other.Root.SessionID == record.Root.SessionID {
				mine++
			}
		}
		if mine >= b.maxChildren() {
			return Dispatched{}, refuseWith(http.StatusTooManyRequests, "over_capacity",
				fmt.Sprintf("All %d child slots for this session are busy; retry when one finishes.", b.maxChildren()),
				map[string]any{"retry_after": 60})
		}
	}
	if len(live) >= b.machineChildren() {
		return Dispatched{}, refuseWith(http.StatusTooManyRequests, "over_capacity",
			fmt.Sprintf("All %d child sessions on this Mac are busy; retry when one finishes.", b.machineChildren()),
			map[string]any{"retry_after": 60})
	}

	// Claims arbitration, lease against lease. An overlap with a task
	// belonging to a **different** root blocks; one within the same root is a
	// warning, because a root splitting its own work across two tabs already
	// knows. An isolated task has no lease here in either direction: it writes
	// its own checkout, which is the Swift app's order too — it drops the
	// lease before it compares (Orchestrator.swift, retainLandingPaths then
	// claimsOverlaps). The first version of this broker compared first and
	// dropped after, so an isolated dispatch was blocked by a lease it could
	// never have collided with.
	for _, other := range live {
		shared := overlapWith(other.Lease(), record.Lease())
		if len(shared) == 0 {
			continue
		}
		sameRoot := record.Root != nil && other.Root != nil &&
			other.Root.SessionID == record.Root.SessionID
		message := fmt.Sprintf("Task %s shares claimed paths with task %s: %s.",
			record.ID, other.ID, strings.Join(sortedUnique(shared), ", "))
		if sameRoot {
			warnings = append(warnings, Warning{
				Code: "claims_overlap", Task: other.ID, Paths: sortedUnique(shared),
				Message: message, Age: other.Age(b.now()), RootKey: rootKey(other.Root),
			})
			continue
		}
		return Dispatched{}, refuseWith(http.StatusConflict, "workspace_busy",
			"Another dispatch tree has reserved a path this task claims.",
			map[string]any{
				"blocking_task":  other.ID,
				"title":          other.Title,
				"root_label":     labelOf(other),
				"created":        other.CreatedAt.Unix(),
				"conflict_paths": sortedUnique(shared),
				"retry_after":    60,
				"age_seconds":    other.Age(b.now()),
				"root_key":       rootKey(other.Root),
			})
	}
	if len(record.Claims) == 0 {
		warnings = append(warnings, Warning{Code: "claims_missing", Message: claimsMissingMessage})
	}

	// The repository this task belongs to, recorded now: an inventory read a
	// week later must not depend on the directory still existing.
	if repo, err := b.Git.Toplevel(ctx, record.ProjectDir); err == nil {
		record.Repository = repo
		// And where that repository stood, for a task that will write it in
		// place (D17). A commit this task lands has to be one it could have
		// made, and everything already under this line was there before it
		// was admitted. Unreadable stays empty: an unknown line proves
		// nothing, and the landing says so rather than guessing one.
		if record.Isolation != IsolationWorktree {
			if head, err := b.Git.ResolveCommit(ctx, repo, "HEAD"); err == nil {
				record.DispatchBase = head
			}
		}
	}

	cwd := record.ProjectDir
	var effects []store.Effect
	if record.Isolation == IsolationWorktree {
		w, more, err := b.planWorktree(ctx, record)
		if err != nil {
			return Dispatched{}, err
		}
		// Made after the task is recorded, never before (G14): the checkout is
		// an effect the task's creation owes, recorded with it as intent.
		payload, _ := json.Marshal(worktreeEffect{Repository: w.Repository, Path: w.Path, Branch: w.Branch, Base: w.Base})
		effects = append(effects, store.Effect{Kind: EffectWorktree, Subject: record.ID, Payload: payload})
		warnings = append(warnings, more...)
		record.Worktree = w
		record.Repository = w.Repository
		// The claims hold no lease in the shared tree — the work went
		// somewhere else — and they are **kept**, unchanged, as the declared
		// write set this branch lands (D21). The first version of this broker
		// emptied them here and stored nothing in their place, while its own
		// comment and the schema both said they were kept.
		if len(record.Claims) > 0 {
			warnings = append(warnings, Warning{
				Code:  "claims_ignored_for_worktree",
				Paths: sortedUnique(record.Claims),
				Message: "Claims inside project_dir reserve nothing in the shared tree because this task uses an " +
					"isolated worktree; they are kept as its declared_writes, the set its branch lands.",
			})
		}
		rel, relErr := filepath.Rel(w.Repository, record.ProjectDir)
		if relErr != nil || strings.HasPrefix(rel, "..") {
			rel = "."
		}
		cwd = filepath.Clean(filepath.Join(w.Path, rel))
	}

	// Opening a tab is a terminal write and takes a turn like every other one
	// (D22). It is taken before the record is written, so a full machine is a
	// 429 with nothing recorded, and given back the moment the tab is open —
	// the briefing that follows takes the child's own lane.
	opening := func() {}
	if b.Lanes != nil {
		release, err := b.Lanes.Acquire(ctx, "open:child:"+record.ID)
		if err != nil {
			return Dispatched{}, refuseWith(http.StatusTooManyRequests, "terminal_busy",
				"This Mac already has as many terminal writes in hand as it admits; nothing was recorded or opened.",
				map[string]any{"retry_after": 5})
		}
		opening = release
	}
	defer opening()

	record.CreatedAt = b.now()
	record.Dir = b.Tasks.Path(record.ID)
	record.State = StateQueued
	record.Dispatcher = b.Store.Owner()

	// Written before anything is opened or made. From here on the task exists
	// whatever happens to this process. A create, not a save: see create.
	//
	// The secret is held before the record exists, so there is no moment at
	// which this process has a queued task it could brief and does not know
	// it: the beat settles a queued task whose dispatcher is gone (watch.go),
	// and it must never mistake this one for such a task.
	hash := HashSecret(req.Secret)
	b.rememberSecret(record.ID, req.Secret)
	ids, err := b.create(ctx, record, hash, effects...)
	if err != nil {
		b.forgetSecret(record.ID)
		return Dispatched{}, err
	}
	refund = false

	// The checkout, now that the task that owes it is durable.
	for _, res := range b.runRecorded(ctx, ids) {
		if res.state != store.EffectDone {
			settled, _ := b.Settle(ctx, record.ID, StateSpawnFailed, "Could not make the isolated checkout: "+res.outcome, nil)
			return Dispatched{Record: settled, Warnings: warnings}, nil
		}
	}

	// The brief the child reads, beside the task.json the caller wrote.
	if _, err := b.Tasks.Write(record.Brief(), b.ChildBrief(record, cwd)); err != nil {
		// The caller's task.json is already there; failing to add CHILD.md is
		// still a failure to brief, and a child told to read a file that is not
		// there is worse than one that never started.
		settled, _ := b.Settle(ctx, record.ID, StateSpawnFailed, "Could not write CHILD.md: "+err.Error(), nil)
		return Dispatched{Record: settled, Warnings: warnings}, nil
	}

	spawned := b.spawn(ctx, record, cwd, req.Secret, opening)
	if spawned.State == StateSpawnFailed {
		settled, _ := b.Settle(ctx, record.ID, StateSpawnFailed, spawned.SpawnError, nil)
		return Dispatched{Record: settled, Warnings: warnings}, nil
	}
	// What the spawn learned is applied to the record as it is **now**. The
	// beat may already have moved it on — a progress note proves `briefed`
	// while this request was still typing — and writing this copy back whole
	// would put `spawning` over that proof.
	record, err = b.mutate(ctx, record.ID, "task.spawned", func(r *Record) error {
		r.ChildTerminalID = spawned.ChildTerminalID
		r.ChildBackend = spawned.ChildBackend
		r.SpawnedAt = spawned.SpawnedAt
		r.SpawnError = spawned.SpawnError
		if r.State == StateQueued {
			r.State = spawned.State
		}
		return nil
	})
	if err != nil {
		return Dispatched{}, err
	}
	b.forgetSecret(record.ID)
	return Dispatched{Record: record, Warnings: warnings}, nil
}

// resolveRoot proves the declared owner is one live session on this machine.
func (b *Broker) resolveRoot(ctx context.Context, root *RootRef) (string, error) {
	if root == nil {
		return "", nil
	}
	s, err := b.terminalFor(ctx, root.SessionID, root.Assistant)
	if err == nil {
		return s.ID, nil
	}
	ref, ok := err.(Refusal)
	if !ok {
		return "", err
	}
	const tail = " Resolve the interactive Root with GET /v1/orchestrator/whoami and resend with the " +
		"current process-bound conversation id; do not downgrade owned work to detached polling."
	if ref.Code == "conversation_ambiguous" {
		return "", refuse(http.StatusConflict, "conversation_ambiguous",
			"More than one live process of the declared assistant proves root.session_id; no owner was selected."+tail)
	}
	return "", refuse(http.StatusUnprocessableEntity, "root_unresolved",
		"root.session_id did not resolve to one live process-bound session; completion notification, "+
			"grouping and close cascade are not guaranteed."+tail)
}

const claimsMissingMessage = "This task declared no claims, so nothing reserves the paths it is about to write " +
	"and no other root can be told to stay off them. Add \"claims\" to task.json — the relative paths this task " +
	"may write — or \"claims\": [] to say it writes nothing."

// labelOf is the name a person knows a task's owner by: its root's label, or
// — for a task a schedule started, which has no root — the schedule's title,
// as the Swift app labels one (`rootLabel: schedule?.title`).
func labelOf(r Record) any {
	switch {
	case r.Root != nil && r.Root.Label != "":
		return r.Root.Label
	case r.Root == nil && r.ScheduleTitle != "":
		return r.ScheduleTitle
	}
	return nil
}

// checkGeneration is the stale-inventory door.
//
// The refusal carries the whole inventory, so recovering from it is one round
// trip rather than two. That is not a courtesy: a caller that has to make a
// second request to learn the new receipt will race the next dispatch, and the
// receipt it reads will already be the wrong one.
func (b *Broker) checkGeneration(ctx context.Context, project string, req DispatchRequest) error {
	inv, err := b.ReadInventory(ctx, project, nil)
	if err != nil {
		// A project in no repository cannot have an inventory, and the brief's
		// own validation has a better sentence for that. Skipping here is the
		// Swift app's behaviour and it keeps one fault from wearing two names.
		return nil
	}
	if req.Offered && req.Generation == inv.Generation {
		return nil
	}
	clause := "This dispatch carried an inventory_generation this repository has moved past."
	if !req.Offered || req.Generation == "" {
		clause = "This dispatch carried no inventory_generation."
	}
	return refuseWith(http.StatusConflict, "stale_inventory",
		clause+" GET /v1/orchestrator/inventory?project="+inv.Repository+" answers "+inv.Generation+
			"; the whole of it is in this error, so read it and resend with that value.",
		map[string]any{
			"inventory_generation": inv.Generation,
			"inventory":            InventoryPayload(inv),
		})
}

// planWorktree decides the isolated checkout — repository, base, path, branch
// — and refuses what can be refused before anything exists. It makes nothing:
// the checkout itself is the `worktree.add` effect, run after the task that
// owes it is recorded.
//
// The base commit is resolved and recorded here and never recomputed. A reader
// asking later whether this branch produced anything compares its head against
// this base; recomputing it would answer that question against a tree that has
// moved.
func (b *Broker) planWorktree(ctx context.Context, r Record) (*Worktree, []Warning, error) {
	bad := func(msg string) (*Worktree, []Warning, error) {
		return nil, nil, refuse(http.StatusUnprocessableEntity, "bad_task", msg)
	}
	repo, err := b.Git.Toplevel(ctx, r.ProjectDir)
	if err != nil || repo == "" {
		return bad(`isolation:"worktree" needs project_dir to be inside a Git repository.`)
	}
	base, err := b.Git.ResolveCommit(ctx, repo, "HEAD")
	if err != nil || base == "" {
		return nil, nil, refuse(http.StatusConflict, "worktree_unavailable",
			"This repository has no commit to use as a worktree base.")
	}
	path := b.worktreePath(repo, r.ID)
	if dirExists(path) {
		return bad("task_id cannot name a worktree branch or path.")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, err
	}
	branch := BranchName(r.ID)
	if exists, known := b.Git.BranchExists(ctx, repo, branch); !known || exists {
		return nil, nil, refuse(http.StatusConflict, "worktree_unavailable",
			"The delivery branch "+branch+" already exists, or this Mac could not tell whether it does.")
	}
	warnings := []Warning{}
	if dirty, known := b.Git.Dirty(ctx, repo); known && dirty {
		warnings = append(warnings, Warning{
			Code: "dirty_worktree_base",
			Message: "The base tree has uncommitted files; the worktree starts from commit " +
				shortCommit(base) + " and does not contain them.",
		})
	}
	return &Worktree{Repository: repo, Path: path, Branch: branch, Base: base, Head: base}, warnings, nil
}

func shortCommit(v string) string {
	if len(v) > 7 {
		return v[:7]
	}
	return v
}

// spawn opens the child's tab and types the one line that briefs it.
//
// The answer is always a record: a tab that would not open is `spawn_failed`
// with the terminal's own sentence, not an error thrown back at the dispatcher.
// The dispatch succeeded — the task exists and is recorded — and what failed is
// the machine's ability to put a session in front of it, which is a fact about
// the task rather than about the request.
func (b *Broker) spawn(ctx context.Context, r Record, cwd, secret string, opened func()) Record {
	defer opened()
	launch, err := projects.Admit(projects.LaunchRequest{
		ProjectRoot: cwd,
		Assistant:   r.Assistant,
		Model:       r.Model,
	})
	if err != nil {
		r.State = StateSpawnFailed
		r.SpawnError = err.Error()
		r.FinishedAt = b.now()
		return r
	}
	line := "cd " + projects.ShellQuoted(cwd) + " && " + shellCommand(launch, r, b.Tasks.Dir)

	itermOpen := false
	if b.Launcher != nil {
		itermOpen, _ = b.Launcher.ITermRunning(ctx)
	}
	reach := projects.TmuxAbsent
	if b.Launcher != nil {
		reach = projects.TmuxReach(b.Launcher.TmuxReach(ctx))
	}
	choice := projects.TerminalAuto
	if b.Terminal != nil {
		choice = b.Terminal()
	}
	var (
		terminalID string
		backend    string
		openErr    error
	)
	if err := outside(); err != nil {
		opened()
		r.State = StateSpawnFailed
		r.SpawnError = err.Error()
		r.FinishedAt = b.now()
		return r
	}
	switch projects.ChoosePlan(choice, itermOpen, reach) {
	case projects.PlanITerm:
		terminalID, openErr = b.Launcher.NewITermTab(ctx, line)
		backend = "iterm"
	case projects.PlanTmux, projects.PlanTmuxDetached:
		// A **new detached session per child**, never a window on whichever
		// session tmux happens to have used last. `new-window` with no target
		// lands in the most recently used session, which on this machine is one
		// somebody is working in — a broker that can put a child in front of
		// your keyboard is a broker that can take it. This is the tmux
		// equivalent of the iTerm tab above: its own place, named after the
		// task, easy to find and easy to close.
		terminalID, openErr = b.Launcher.NewTmuxSession(ctx, cwd, ChildSessionName(r.ID),
			shellCommand(launch, r, b.Tasks.Dir))
		backend = "tmux"
	case projects.PlanNotRunning:
		openErr = terminal.Failure{Message: "iTerm2 is not running, and this will not launch it for you."}
	default:
		openErr = terminal.Failure{Message: "tmux is the terminal for new sessions in Settings, and there is no tmux on this Mac."}
	}
	// The tab is open, or will not be: the opening's turn ends here.
	opened()
	if openErr != nil {
		r.State = StateSpawnFailed
		r.SpawnError = openErr.Error()
		r.FinishedAt = b.now()
		return r
	}

	r.ChildTerminalID = terminalID
	r.ChildBackend = backend
	r.SpawnedAt = b.now()
	r.State = StateSpawning

	// The composer needs the assistant to have drawn one before it can be
	// typed into. Waiting here rather than in the watch beat keeps the whole
	// spawn in one place; the beat's own 4-minute clock is the backstop.
	if err := b.brief(ctx, r, secret); err != nil {
		// Not `spawn_failed`: the tab is open and the assistant may still be
		// starting. The beat decides, and it decides with evidence.
		r.SpawnError = err.Error()
	}
	return r
}

// ChildSessionName is the tmux session one child is given.
//
// Deliberately not "clawdline": that is the Swift app's name, and on this
// machine it is a session with twenty-five windows somebody is working in.
func ChildSessionName(taskID string) string {
	short := taskID
	if len(short) > 8 {
		short = short[:8]
	}
	return "clawdline-task-" + short
}

// shellCommand is the one line the child's shell runs.
func shellCommand(l projects.Launch, r Record, taskRoot string) string {
	args := append([]string{}, l.Arguments...)
	args = append(args, "--add-dir", projects.ShellQuoted(taskRoot))
	args = append(args, permissionArgs(r)...)
	prefix := ""
	if keys := projects.InheritedIdentityKeys(l.Assistant); len(keys) > 0 {
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = "-u " + k
		}
		prefix = "env " + strings.Join(parts, " ") + " "
	}
	return prefix + strings.Join(append([]string{l.Assistant}, args...), " ")
}

// permissionArgs is the ceiling the dispatcher asked for, spelled the way each
// CLI spells it.
func permissionArgs(r Record) []string {
	switch r.Assistant {
	case "claude":
		switch r.PermissionMode {
		case "edits":
			return []string{"--permission-mode", "acceptEdits"}
		case "full":
			return []string{"--permission-mode", "bypassPermissions"}
		}
	case "codex":
		switch r.PermissionMode {
		case "edits":
			return []string{"--ask-for-approval", "on-request", "--sandbox", "workspace-write"}
		case "full":
			return []string{"--ask-for-approval", "never", "--sandbox", "workspace-write"}
		}
	}
	return nil
}

// brief types the one line that carries the secret.
//
// It waits for a **composer**, not merely for an assistant process. A child
// whose first screen is a dialog — Claude Code's workspace-trust question, for
// one — would have the Return at the end of this line answer that dialog
// instead, and the option under the highlight is "No, exit". See composer.go.
//
// The wait is bounded here and again by the four-minute clock in the beat, so a
// child that never draws a prompt is reported rather than waited on for ever.
func (b *Broker) brief(ctx context.Context, r Record, secret string) error {
	if b.Type == nil {
		return errors.New("this daemon cannot type into a terminal")
	}
	deadline := b.now().Add(90 * time.Second)
	var last error
	for b.now().Before(deadline) {
		if s, ok := b.sessionByTerminal(ctx, r.ChildTerminalID); ok && s.IsAssistant() {
			ready, why := b.composerReady(ctx, r.ChildTerminalID)
			if ready {
				if err := b.typeLine(ctx, r.ChildTerminalID, FirstLine(r, secret, b.Language)); err != nil {
					last = err
				} else {
					return nil
				}
			} else if why != nil {
				last = why
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	if last == nil {
		last = errors.New("the child session did not reach a prompt")
	}
	return last
}

// composerReady asks the child's own screen whether it is ready to be typed at.
//
// With no screen reader at all the answer is yes: a daemon that cannot see the
// terminal it drives must still be able to brief a child, and the machines
// where that is true are the ones with no dialogs to hit.
func (b *Broker) composerReady(ctx context.Context, terminalID string) (bool, error) {
	if b.Screen == nil {
		return true, nil
	}
	screen, ok := b.Screen(ctx, terminalID)
	if !ok {
		return true, nil
	}
	if Choosing(screen) {
		return false, errors.New("the child is showing a dialog; the briefing would have answered it")
	}
	if !ComposerReady(screen) {
		return false, nil
	}
	return true, nil
}

// errAlreadyTerminal is Settle finding that somebody settled the task first.
var errAlreadyTerminal = errors.New("already terminal")

// Settle records a terminal outcome and opens the notice that tells the root.
//
// Exactly once. A `/complete` and the beat's collection of result.json can both
// arrive for one task, and before this was a precondition inside mutate they
// both settled it — and minted two notice ids, the second overwriting the one
// the root had already been told to acknowledge.
//
// A result is recorded only as the child wrote it. With none, `why` is the
// broker's own sentence and is kept as that (Record.Verdict); the first version
// of this function wrapped it in a Result, which is how a `/complete` body
// came to stand in for the file (D15).
func (b *Broker) Settle(ctx context.Context, id string, state State, why string, result *taskdir.Result) (Record, error) {
	r, _, err := b.settle(ctx, id, state, why, result)
	return r, err
}

// settle is Settle that also records the effects the settlement owes — a
// spawn_failed child's session to close — in the same transaction, and
// answers their ids for the caller to run after the commit.
func (b *Broker) settle(ctx context.Context, id string, state State, why string, result *taskdir.Result, effects ...store.Effect) (Record, []int64, error) {
	now := b.now()
	// What the delivery branch holds as the task ends, asked of git before the
	// write right is taken (D08) and applied inside it only to the branch it
	// was read from (D17, G17).
	head := b.settlementHead(ctx, id)
	return b.mutateTx(ctx, id, "task."+string(state), func(_ *store.Tx, r *Record) ([]store.Effect, error) {
		if r.State.Terminal() {
			return nil, errAlreadyTerminal
		}
		r.State = state
		r.FinishedAt = now
		if result != nil {
			r.Result = result
		} else if why != "" {
			r.Verdict = truncate(why, summaryLimit)
		}
		// Empty when the branch is gone or could not be read: the base it
		// held at creation is no longer a claim anybody can stand on.
		if head.asked && r.Worktree != nil && r.Worktree.Branch == head.branch {
			r.Worktree.Head = head.commit
		}
		// A task that reserved paths in the shared tree still owes a landing,
		// and so does an isolated one, whose declared paths became its landing
		// write set. Delivered is not landed, and the obligation is what keeps
		// the difference visible to the next root rather than to nobody.
		//
		// It opens with no target (D19): which branch the work belongs on is
		// the root's to say, the first time it records this landing, and
		// until then every reader treats the target as not decided — never as
		// whatever the repository's HEAD happens to be.
		if r.Landing == nil && (len(r.Claims) > 0 || r.Worktree != nil) {
			r.Landing = &Landing{State: LandingPending, Note: "not yet on its target"}
		}
		if r.Root != nil && r.Notice == nil {
			r.Notice = &Notice{
				ID:          b.newID(),
				State:       NoticePending,
				CreatedAt:   now,
				NextRetryAt: now,
			}
		}
		b.forgetSecret(r.ID)
		return effects, nil
	})
}

func (b *Broker) newID() string {
	if b.NewID != nil {
		return b.NewID()
	}
	return uuidLike()
}

// uuidLike mints the hyphenated lowercase-hex id a notice is addressed by. It
// is a v4 UUID in shape; nothing downstream parses it as one, but a person
// reading two logs should not have to learn a second id format.
func uuidLike() string {
	raw := NewSecret()[:32]
	return raw[0:8] + "-" + raw[8:12] + "-4" + raw[13:16] + "-a" + raw[17:20] + "-" + raw[20:32]
}
