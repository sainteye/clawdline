package orchestrator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/resourcefs"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app/taskresources"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

// EnableTaskResources refuses to adopt an existing unmarked directory. Failure
// leaves the service unavailable and new heavy work fenced, not silently unmanaged.
func (b *Broker) EnableTaskResources() error {
	b.ResourceServiceRequired = true
	b.managedCleanup.pending = true
	root, err := resourcefs.Open(filepath.Join(b.Dir, "task-resources-v1"))
	if err != nil {
		return err
	}
	b.Resources = &taskresources.Service{Store: b.Store, FS: root, Clock: b.now, Owner: b.resourceOwner, OtherUsage: b.legacyResourceUsage,
		Uses: func(ctx context.Context, r taskresource.Resource) string {
			return b.resourceUsePresence(ctx, filepath.Join(root.Path(), r.ID))
		}, FreeSpace: func() (int64, error) {
			r := b.Store.Reading(b.Dir)
			if !r.HasDiskFree {
				return 0, errors.New("resource_disk_space_unknown")
			}
			return r.DiskFree, nil
		}}
	return nil
}

func (b *Broker) resourceUsePresence(ctx context.Context, path string) string {
	if b.ProcessUses != nil {
		return b.ProcessUses(ctx, path)
	}
	return resourcefs.UsePresence(ctx, path)
}

func ResourceOwnerScope() taskresources.Scope {
	return taskresources.Scope{Actor: "daemon", AllProjects: true, Inspect: true, Reclaim: true, Publish: true}
}

func evidenceDigest(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (b *Broker) resourceOwner(ctx context.Context, kind, id string) (taskresources.OwnerState, error) {
	out := taskresources.OwnerState{Presence: "unknown", UsePresence: "unknown"}
	switch kind {
	case "task":
		r, _, err := b.Record(ctx, id)
		if err != nil {
			return out, err
		}
		project := r.Repository
		if project == "" {
			project = r.ProjectDir
		}
		out.Owner = taskresource.Owner{Kind: kind, ID: id, Project: project}
		out.Complete = r.State.Terminal() && !r.FinishedAt.IsZero()
		out.CompletedAt = r.FinishedAt.Unix()
		out.OutputRoot = filepath.Join(b.Tasks.Path(id), "artifacts")
		// Store results survive scratch and raw-log expiry. On-disk child results
		// must still agree with the result accepted by the broker.
		if out.Complete && ownedPath(b.Tasks.Dir, r.Dir, id) {
			if r.Callback != nil {
				out.OutputsSaved = r.Callback.Exit != nil || r.Callback.Signal != "" || r.Verdict != ""
				out.EvidenceDigest = evidenceDigest(struct {
					State    State
					Verdict  string
					Callback *Callback
				}{r.State, r.Verdict, r.Callback})
			} else if r.Gate != nil && r.Result != nil && r.Result.GateVerdict != nil {
				// Gate collectors persist a typed result outside ordinary result.json.
				out.OutputRoot, out.EvidenceDigest, out.OutputsSaved = b.savedGateOutput(r)
			} else if r.Result != nil {
				disk, raw, e := b.Tasks.ReadResult(id)
				stored := *r.Result
				disk.Secret = ""
				stored.Secret = ""
				out.OutputsSaved = e == nil && sameSavedResult(disk, stored)
				out.EvidenceDigest = evidenceDigest(struct {
					Accepted any
					Raw      string
				}{stored, digestGate(raw)})
				// Expired artifacts are a recorded removal, not unsaved output.
				_, expired := b.ArtifactsExpired(ctx, id)
				for _, name := range stored.Artifacts {
					if expired {
						break
					}
					path := name
					if !filepath.IsAbs(path) {
						path = filepath.Join(r.Dir, path)
					}
					rel, e := filepath.Rel(out.OutputRoot, path)
					if e != nil || rel == ".." || filepath.IsAbs(rel) || len(rel) > 3 && rel[:3] == ".."+string(filepath.Separator) {
						out.OutputsSaved = false
						break
					}
					if _, e := os.Lstat(path); e != nil {
						out.OutputsSaved = false
						break
					}
				}
			}
		}
		if !out.Complete {
			return out, nil
		}
		if r.Callback != nil {
			out.Presence = b.callbackResourcePresence(r)
		} else {
			gone, why, _ := b.ownerGone(ctx, b.read(ctx), r, r.Dir)
			if gone {
				out.Presence = "absent"
			} else if why == WhyOwnerPresent {
				out.Presence = "present"
			}
		}
	case "root":
		a, err := b.RootAssignmentByID(ctx, id)
		if err != nil {
			return out, err
		}
		out.Owner = taskresource.Owner{Kind: kind, ID: id, Project: a.ProjectDir}
		out.OutputRoot = filepath.Join(b.AssignmentRoot(), id, "artifacts")
		p := b.rootCleanup(ctx, b.read(ctx), a)
		out.Complete = p.Completion
		out.Presence = p.Owner
		if a.Closure != nil {
			out.CompletedAt = a.Closure.ClosedAt
		}
		// A safe close is completion, not proof that outcomes were saved. Require
		// a durable delivery from that exact conversation during this assignment.
		if out.Complete && a.Closure.ConversationID != "" {
			events, e := b.Store.LatestEvents(ctx, "session.delivered", []string{a.Closure.TerminalID})
			if e == nil {
				if event, ok := events[a.Closure.TerminalID]; ok {
					var receipt struct {
						Conversation string `json:"conversation"`
						At           int64  `json:"at"`
						Summary      string `json:"summary"`
					}
					if json.Unmarshal(event.Payload, &receipt) == nil && receipt.Conversation == a.Closure.ConversationID && receipt.At >= a.Executor.OpenedAt && receipt.At <= a.Closure.ClosedAt && receipt.Summary != "" {
						out.OutputsSaved = true
						out.EvidenceDigest = evidenceDigest(json.RawMessage(event.Payload))
					}
				}
			}
		}
	case "shared_go":
		return b.sharedGoOwner(ctx, id)
	default:
		return out, errors.New("resource_owner_unknown")
	}
	if out.Presence == "absent" {
		out.UsePresence = "absent"
	}
	return out, nil
}

func (b *Broker) callbackResourcePresence(r Record) string {
	if r.Callback == nil {
		return "unknown"
	}
	if r.Callback.PGID <= 0 {
		if r.State.Terminal() && r.Callback.NotStarted {
			return "absent"
		}
		return "unknown"
	}

	path := (callbackFiles{r.Dir}).lock()
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() {
		return "unknown"
	}
	held, err := lockHeld(path)
	if err != nil {
		return "unknown"
	}
	if held {
		return "present"
	}
	return callbackGroupPresence(r.Callback.PGID)
}

// Seal only from source-owned publication facts. Retries after a restart never
// replace an existing manifest; changed output requires explicit owner review.
func (b *Broker) sealResourcePage(ctx context.Context, after string) error {
	rows, _, err := b.Store.TaskResourcePage(ctx, after, taskresource.PageRowsLimit, true)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.Manifest == "" && r.LastOutcome != "removing" {
			_, _ = b.Resources.Seal(ctx, ResourceOwnerScope(), r.ID)
		}
	}
	return nil
}

func (b *Broker) SweepTaskResources(ctx context.Context, after string) (taskresources.Result, string, error) {
	if b.Resources == nil {
		return taskresources.Result{}, after, errors.New("resource_service_unavailable")
	}
	if err := b.sealResourcePage(ctx, after); err != nil {
		return taskresources.Result{}, after, err
	}
	return b.Resources.Sweep(ctx, after)
}

// Callback resources are allocated before a process can start. Their paths are
// daemon-owned record fields, not environment variables accepted from a client.
func (b *Broker) prepareCallbackResources(ctx context.Context, r Record) (Record, error) {
	ctx = taskresources.WithCaller(ctx, "callback.start", r.ID)
	if b.Resources == nil {
		if b.ResourceServiceRequired && r.Callback.Intent == CallbackIntentHeavy {
			return r, b.AdmitHeavyResources(ctx)
		}
		return r, nil // read/wait paths remain available; isolated brokers opt in.
	}
	if r.Callback.Intent == CallbackIntentHeavy {
		if err := b.AdmitHeavyResources(ctx); err != nil {
			return r, err
		}
	}
	if err := os.MkdirAll(filepath.Join(r.Dir, "artifacts"), 0o700); err != nil {
		return r, err
	}
	rows, _, err := b.Store.TaskResourcesByOwnerPage(ctx, "task", r.ID, "")
	if err != nil {
		return r, err
	}
	paths := map[taskresource.Class]string{}
	for _, res := range rows {
		if err := b.Resources.FS.Verify(res); err == nil && res.LastOutcome != "removed" {
			paths[res.Class] = filepath.Join(b.Resources.FS.Path(), res.ID)
		}
	}
	for _, class := range []taskresource.Class{taskresource.Scratch, taskresource.Cache} {
		if paths[class] == "" {
			a, err := b.Resources.Allocate(ctx, ResourceOwnerScope(), "task", r.ID, class)
			if err != nil {
				return r, err
			}
			paths[class] = a.Path
		}
	}
	next, err := b.mutate(ctx, r.ID, "task.resources_allocated", func(x *Record) error {
		if x.State.Terminal() || !x.Callback.StartedAt.IsZero() {
			return errors.New("resource_owner_already_started")
		}
		x.Callback.ScratchPath = paths[taskresource.Scratch]
		x.Callback.CachePath = paths[taskresource.Cache]
		return nil
	})
	if err != nil {
		return next, err
	}
	if r.Callback.Intent != CallbackIntentHeavy {
		return next, nil
	}
	return b.prepareSharedGoCache(ctx, next)
}

// New compile admission is distinct from lease renewal: pressure cannot revoke
// a running command's lease or grant permission to delete its inputs.
func (b *Broker) AdmitHeavyResources(ctx context.Context) error {
	if b.Resources == nil {
		return refuse(507, "resource_service_unavailable", "Managed resource service is unavailable.")
	}
	err := b.Resources.Admit(ctx)
	if err == nil {
		return nil
	}
	var failure taskresources.Failure
	pressure := errors.As(err, &failure) && (failure.Code == "disk_reserve_unavailable" || failure.Code == "cache_total_over_budget" || failure.Code == "scratch_over_budget")
	if pressure {
		// Only measured aggregate/reserve pressure puts cleanup on the compile
		// path. Optional cache maintenance belongs to the periodic sweeper.
		b.reclaim.mu.Lock()
		after := b.reclaim.resourcesAfter
		b.reclaim.mu.Unlock()
		if _, next, e := b.SweepTaskResources(ctx, after); e == nil {
			b.reclaim.mu.Lock()
			b.reclaim.resourcesAfter = next
			b.reclaim.mu.Unlock()
		}
		_, _ = b.Reclaim(ctx, false)
		err = b.Resources.Admit(ctx)
		if err == nil {
			return nil
		}
	}
	failure = taskresources.Explain(ctx, err, "admission")
	diagnostic := failure.Diagnostic()
	extra := diagnostic.Fields()
	extra["policy"] = taskresource.DefaultPolicy()
	// Client cancellation does not erase the server's refusal. This records
	// a decision, not proof that its response reached the caller.
	rec, cancel := recordContext(ctx)
	defer cancel()
	raw, _ := json.Marshal(diagnostic)
	subject := diagnostic.CallerID
	if diagnostic.CallbackTaskID != "" {
		subject = diagnostic.CallbackTaskID
	}
	if subject == "" {
		subject = diagnostic.Caller
	}
	recordErr := b.Store.Append(rec, store.Event{Kind: "resource.admission_refused", Subject: subject, Payload: raw})
	extra["resource_diagnostic_recorded"] = recordErr == nil
	if recordErr != nil {
		log.Printf("resource.admission_refused diagnostic not recorded: code=%s phase=%s", diagnostic.Code, diagnostic.Phase)
	}
	message := "Managed storage could not be verified. Inspect resource diagnostics before retrying."
	switch failure.Code {
	case "disk_reserve_unavailable", "cache_total_over_budget", "scratch_over_budget":
		message = "Storage limits prevent a new build. Finish or preserve work, then reclaim eligible temporary data and retry."
	}
	return refuseWith(507, failure.Code, message, extra)
}

func callbackResourceFailure(ctx context.Context, taskID string, err error) string {
	ctx = taskresources.WithCaller(ctx, "callback.start", taskID)
	var diagnostic taskresources.Diagnostic
	code := "resource_preparation_failed"
	if ref, ok := err.(Refusal); ok {
		code = ref.Code
		diagnostic, _ = ref.Extra["resource_diagnostic"].(taskresources.Diagnostic)
		if diagnostic.Code == "" {
			diagnostic = (taskresources.Failure{Code: ref.Code}).WithContext(ctx, "callback.prepare", "").Diagnostic()
		}
	}
	if diagnostic.Code == "" {
		failure := taskresources.Explain(ctx, err, "callback.prepare")
		diagnostic, code = failure.Diagnostic(), failure.Code
	}
	raw, _ := json.Marshal(diagnostic)
	return "resource_admission_refused: " + code + "; resource_diagnostic=" + string(raw)
}

// Keep these facts when callback resources still depend on process-group and
// lock evidence. Raw output alone can expire independently.
func (b *Broker) callbackResourceMetadataNeeded(ctx context.Context, id string) bool {
	rows, _, err := b.Store.TaskResourcesByOwnerPage(ctx, "task", id, "")
	if err != nil || len(rows) > 0 {
		return true
	}
	r, _, err := b.Record(ctx, id)
	if err != nil {
		return true
	}
	if r.Callback != nil && r.Callback.GoCacheID != "" {
		cache, err := b.Store.TaskResource(ctx, r.Callback.GoCacheID)
		return err != nil || cache.LastOutcome != "removed"
	}
	return false
}

// Use the gate collector's actual durable protocol. A malformed ordinary
// result.json has no gate authority and must not strand all completed caches.
func (b *Broker) savedGateOutput(r Record) (string, string, bool) {
	root := filepath.Join(filepath.Dir(b.Tasks.Dir), "gate-submissions", r.ID)
	receipt, body, err := b.Tasks.ReadGateResult(r.ID)
	if err != nil {
		return root, "", false
	}
	result, err := contract.DecodeWorkGateResult(body)
	if err != nil || receipt.TaskID != r.ID || receipt.Sha256 != digestGate(body) || receipt.Verdict != result.Verdict {
		return root, "", false
	}
	if validateGateResultIdentity(r, result) != nil || !reflect.DeepEqual(result, *r.Result.GateVerdict) || b.validateGateArtifacts(r.ID, result) != nil {
		return root, "", false
	}
	return root, evidenceDigest(struct {
		Accepted any
		Raw      string
	}{r.Result, digestGate(body)}), true
}

// sameSavedResult compares a result as the store would encode it. A child's
// `"symbols": []` decodes as an empty slice while the stored copy, written
// with omitempty, decodes as nil; both are the same accepted result.
func sameSavedResult(disk, stored taskdir.Result) bool {
	a, ea := json.Marshal(disk)
	b, eb := json.Marshal(stored)
	return ea == nil && eb == nil && bytes.Equal(a, b)
}

// Task artifacts expire. The person decided on 2026-11-23 that a finished
// task keeps artifacts/ for taskresource.ArtifactRetentionSecondsLimit (30
// days) after it ended and its landing settled. Only artifacts/ goes: the
// brief, result.json and the landing stay, so `task show` still has the
// summary, and the standing reclaim row says the files expired rather than
// that there never were any.
const (
	// ReclaimTaskArtifacts is a finished task's artifacts/ directory.
	ReclaimTaskArtifacts = "task_artifacts"
	// WhyArtifactRetention: removed because the retention elapsed.
	WhyArtifactRetention = "artifact_retention_expired"
	// WhyLandingUnsettled: the task still owes a landing, or has a branch
	// and no landing record at all.
	WhyLandingUnsettled = "landing_unsettled"
	// WhyPinned: a registered resource of this task is pinned.
	WhyPinned = "pinned"
	// WhyEndUnknown: the task's end time cannot be read.
	WhyEndUnknown = "ended_at_unknown"
)

// artifactRetention is the bound as a duration.
func artifactRetention() time.Duration {
	return time.Duration(taskresource.ArtifactRetentionSecondsLimit) * time.Second
}

// artifactRetentionState is whether a terminal state is one the person named:
// success, failure, cancelled or spawn_failed. A timeout's child may still be
// working, so its files stay.
func artifactRetentionState(s State) bool {
	switch s {
	case StateSuccess, StateFailure, StateCancelled, StateSpawnFailed:
		return true
	}
	return false
}

// landingSettled is whether nothing is still owed: landed, incorporated,
// nothing to land or abandoned. A task with no landing record is settled only
// when it never had a branch to land.
func landingSettled(r Record) bool {
	if r.Landing == nil {
		return r.Worktree == nil
	}
	switch r.Landing.State {
	case LandingLanded, LandingIncorporated, LandingNothingToLand, LandingAbandoned:
		return true
	}
	return false
}

// taskPinned answers whether any registered resource of the task is pinned.
// A page that cannot be read, or more than one page, is not a "no".
func (b *Broker) taskPinned(ctx context.Context, id string) (pinned bool, known bool) {
	rows, next, err := b.Store.TaskResourcesByOwnerPage(ctx, "task", id, "")
	if err != nil || next != "" {
		return false, false
	}
	for _, r := range rows {
		if r.Pinned {
			return true, true
		}
	}
	return false, true
}

// reclaimTaskArtifacts decides about one task's artifacts/. Within the
// retention it says nothing at all: a young directory is not a decision worth
// a row. Past it, a removal rests on the same positive answers reclaimTaskDir
// needs — owned path, owner gone, nothing reading the files, a stable tree —
// plus a settled landing and no pin, all asked again after the intent is
// recorded.
func (b *Broker) reclaimTaskArtifacts(ctx context.Context, rd reading, r Record, now time.Time, dryRun bool) ReclaimDecision {
	dir := filepath.Join(r.Dir, "artifacts")
	if !r.State.Terminal() || !dirExists(dir) {
		return ReclaimDecision{}
	}
	if !artifactRetentionState(r.State) {
		return kept(r, ReclaimTaskArtifacts, dir, WhyLive, nil)
	}
	if r.FinishedAt.IsZero() {
		return kept(r, ReclaimTaskArtifacts, dir, WhyEndUnknown, nil)
	}
	retention := artifactRetention()
	at := ended(r)
	if now.Sub(at) < retention {
		return ReclaimDecision{}
	}
	evidence := map[string]any{"ended_at": at.Unix(), "retention_seconds": int(retention.Seconds())}
	if !landingSettled(r) {
		if r.Landing != nil {
			evidence["landing"] = string(r.Landing.State)
		}
		return kept(r, ReclaimTaskArtifacts, dir, WhyLandingUnsettled, evidence)
	}
	if !ownedPath(b.Tasks.Dir, r.Dir, r.ID) || !ownedPath(r.Dir, dir, "artifacts") {
		evidence["root"] = b.Tasks.Dir
		return kept(r, ReclaimTaskArtifacts, dir, WhyPathNotOwned, evidence)
	}
	if pinned, known := b.taskPinned(ctx, r.ID); !known {
		return kept(r, ReclaimTaskArtifacts, dir, WhyUnreadable, evidence)
	} else if pinned {
		return kept(r, ReclaimTaskArtifacts, dir, WhyPinned, evidence)
	}
	checkout := ""
	if r.Worktree != nil {
		checkout = r.Worktree.Path
	}
	gone, why, owner := b.artifactOwnerGone(ctx, rd, r, checkout)
	if !gone {
		evidence["owner"] = owner
		return kept(r, ReclaimTaskArtifacts, dir, why, evidence)
	}
	evidence["owner"] = owner
	snapshot, err := resourcefs.InspectLegacyScratch(ctx, dir)
	if err != nil {
		evidence["tree"] = "unverified"
		return kept(r, ReclaimTaskArtifacts, dir, WhyUnreadable, evidence)
	}
	if presence := b.resourceUsePresence(ctx, dir); presence != "absent" {
		why := WhyOwnerUnknown
		if presence == "present" {
			why = WhyOwnerPresent
		}
		evidence["open_files"] = presence
		return kept(r, ReclaimTaskArtifacts, dir, why, evidence)
	}
	bytes := snapshot.Logical
	if snapshot.AllocationKnown {
		bytes = snapshot.Allocated
	}
	d := ReclaimDecision{Task: r.ID, Subject: ReclaimTaskArtifacts, Path: dir, Reason: WhyArtifactRetention,
		Bytes: bytes, Evidence: evidence}
	if dryRun {
		d.Outcome = ReclaimWouldRemove
		return d
	}
	intent := d
	intent.Outcome = reclaimRemoving
	if err := b.markReclaim(ctx, intent, b.now()); err != nil {
		evidence["intent"] = err.Error()
		return kept(r, ReclaimTaskArtifacts, dir, WhyUnrecorded, evidence)
	}
	// Every proof again, after the intent: the record, the landing, the
	// pins, the owner, the tree and the readers.
	current, _, err := b.Record(ctx, r.ID)
	if err != nil || !current.State.Terminal() || !landingSettled(current) || ended(current) != at {
		return kept(r, ReclaimTaskArtifacts, dir, WhyChanged, evidence)
	}
	if pinned, known := b.taskPinned(ctx, r.ID); !known || pinned {
		return kept(r, ReclaimTaskArtifacts, dir, WhyChanged, evidence)
	}
	if gone, why, _ := b.artifactOwnerGone(ctx, b.read(ctx), current, checkout); !gone {
		return kept(r, ReclaimTaskArtifacts, dir, why, evidence)
	}
	tree, err := resourcefs.InspectLegacyScratch(ctx, dir)
	if err != nil || tree.Digest != snapshot.Digest {
		return kept(r, ReclaimTaskArtifacts, dir, WhyChanged, evidence)
	}
	if b.resourceUsePresence(ctx, dir) != "absent" {
		return kept(r, ReclaimTaskArtifacts, dir, WhyOwnerUnknown, evidence)
	}
	remove := b.removeTaskScratch
	if remove == nil {
		remove = removeOwned
	}
	if err := remove(r.Dir, dir, "artifacts"); err != nil {
		evidence["remove"] = err.Error()
		return kept(r, ReclaimTaskArtifacts, dir, WhyRemoveFailed, evidence)
	}
	d.Outcome = ReclaimRemoved
	return d
}

// artifactOwnerGone is ownerGone for a child, and for a callback — which has
// no tab — its own lock and process-group answer followed by the same kernel
// reading of who works inside the directory.
func (b *Broker) artifactOwnerGone(ctx context.Context, rd reading, r Record, checkout string) (bool, string, map[string]any) {
	if r.Callback == nil {
		return b.ownerGone(ctx, rd, r, r.Dir, checkout)
	}
	evidence := map[string]any{"callback": b.callbackResourcePresence(r)}
	switch evidence["callback"] {
	case "absent":
	case "present":
		return false, WhyOwnerPresent, evidence
	default:
		return false, WhyOwnerUnknown, evidence
	}
	inside, err := b.occupied(ctx, r.Dir)
	if err != nil {
		evidence["process_table"] = err.Error()
		return false, WhyOwnerUnknown, evidence
	}
	if inside != "" {
		evidence["working_inside"] = inside
		return false, WhyOwnerPresent, evidence
	}
	return true, "", evidence
}

// ArtifactsExpired is the standing record that a task's artifacts/ was
// removed because its retention elapsed, and whether there is one. A read
// that fails is "no record", never a claim that the files expired.
func (b *Broker) ArtifactsExpired(ctx context.Context, id string) (store.Reclaim, bool) {
	row, err := b.Store.ReclaimOf(ctx, id, ReclaimTaskArtifacts)
	if err != nil || row.Outcome != ReclaimRemoved || row.Reason != WhyArtifactRetention {
		return store.Reclaim{}, false
	}
	return row, true
}
