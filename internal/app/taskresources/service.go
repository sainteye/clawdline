// Package taskresources owns the shared inspection and cleanup contract.
// CLI, scheduled sweeps and a future Bot use this service; callers never submit
// arbitrary filesystem paths or their own claims that an owner has finished.
package taskresources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/adapters/resourcefs"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

// Scope is constructed by an authenticated adapter, never decoded from a body.
// A Bot can have Inspect/Reclaim for one Project without Publish or Unpin.
type Scope struct {
	Actor                                         string
	Project                                       string
	AllProjects, Inspect, Reclaim, Publish, Unpin bool
}

func (s Scope) allows(r taskresource.Resource) bool {
	return s.Actor != "" && (s.AllProjects || (s.Project != "" && filepath.Clean(s.Project) == filepath.Clean(r.Owner.Project)))
}

type OwnerState struct {
	Owner          taskresource.Owner
	Complete       bool
	CompletedAt    int64
	Presence       string
	UsePresence    string
	OutputsSaved   bool
	OutputRoot     string
	EvidenceDigest string
}

type Service struct {
	Store *store.Store
	FS    *resourcefs.Root
	Owner func(context.Context, string, string) (OwnerState, error)
	Uses  func(context.Context, taskresource.Resource) string
	// Policy replaces the compiled policy in tests that change it between a
	// plan and its apply. No caller sets it; there is no per-machine setting.
	Policy    func() taskresource.Policy
	FreeSpace func() (int64, error)
	// OtherUsage accounts source-owned legacy roots without adopting or deleting them.
	OtherUsage func(context.Context) (taskresource.Usage, error)
	Clock      func() time.Time
	// Fault is a test-only failure injection point, never configured by a caller.
	Fault             func(string, string) error
	mu                sync.Mutex
	admitOnce         sync.Once
	admitGate         chan struct{}
	statsMu           sync.Mutex
	stats             taskresource.Usage
	admissionInstance string
	admissionRevision uint64
	usageChange       chan struct{} // protected by statsMu; closed after a managed filesystem change
	admitAfter        string        // protected by statsMu; cleanup uses the existing resource page bound.
}

func (s *Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock()
	}
	return time.Now()
}
func (s *Service) policy() taskresource.Policy {
	if s.Policy != nil {
		return s.Policy()
	}
	return taskresource.DefaultPolicy()
}
func (s *Service) ready() bool { return s != nil && s.Store != nil && s.FS != nil && s.Owner != nil }

type Allocation struct {
	Resource  taskresource.Resource `json:"resource"`
	Path      string                `json:"path"`
	Artifacts string                `json:"artifacts"`
}

// Allocate creates only a new daemon-owned direct child. Existing unregistered
// directories are never adopted. The owner must already exist durably.
func (s *Service) Allocate(ctx context.Context, scope Scope, kind, id string, class taskresource.Class) (Allocation, error) {
	if !s.ready() {
		return Allocation{}, fail("resource_service_unavailable")
	}
	if !scope.Publish {
		return Allocation{}, fail("resource_publish_not_allowed")
	}
	if kind != "task" && kind != "root" {
		return Allocation{}, fail("resource_owner_invalid")
	}
	if u, err := uuid.Parse(id); err != nil || u.String() != id {
		return Allocation{}, fail("resource_owner_invalid")
	}
	if class != taskresource.Cache && class != taskresource.Scratch && class != taskresource.Log {
		return Allocation{}, fail("resource_class_not_disposable")
	}
	owner, err := s.Owner(ctx, kind, id)
	if err != nil {
		return Allocation{}, err
	}
	r := taskresource.Resource{ID: uuid.NewString(), Generation: uuid.NewString(), Owner: owner.Owner, Class: class, CreatedAt: s.now().Unix()}
	r.RelativePath = r.ID
	if !scope.allows(r) || owner.Owner.Kind != kind || owner.Owner.ID != id || !filepath.IsAbs(owner.Owner.Project) || owner.Complete {
		return Allocation{}, fail("resource_owner_not_allocatable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	endChange := s.beginUsageChange()
	defer endChange()
	identity, err := s.FS.Allocate(r.ID, r.Generation)
	if err != nil {
		return Allocation{}, fail("resource_allocation_failed")
	}
	r.Identity = identity
	if err := s.Store.RegisterTaskResource(ctx, r); err != nil {
		return Allocation{}, fail("resource_registration_failed")
	}
	return Allocation{r, filepath.Join(s.FS.Path(), r.ID), owner.OutputRoot}, nil
}

func (s *Service) resource(ctx context.Context, scope Scope, id string) (taskresource.Resource, error) {
	if !s.ready() {
		return taskresource.Resource{}, fail("resource_service_unavailable")
	}
	r, err := s.Store.TaskResource(ctx, id)
	if err != nil {
		return taskresource.Resource{}, readFailure(ctx, err, "resource.lookup", id)
	}
	if !scope.allows(r) {
		return taskresource.Resource{}, fail("resource_scope_not_allowed")
	}
	return visibleResource(scope, r), nil
}

// An internal recovery batch can span Projects. Its target list stays in the
// store; a scoped inspector receives only the resource it is allowed to see.
func visibleResource(scope Scope, r taskresource.Resource) taskresource.Resource {
	if !scope.AllProjects {
		r.LastTargets = ""
	}
	return r
}

func manifest(ctx context.Context, o OwnerState) (string, error) {
	if o.EvidenceDigest == "" {
		return "", fail("resource_outputs_unavailable")
	}
	if o.Owner.Kind == "shared_go" {
		return digest(o.EvidenceDigest), nil
	}
	if o.OutputRoot == "" {
		return "", fail("resource_outputs_unavailable")
	}
	snapshot, err := resourcefs.Manifest(ctx, o.OutputRoot)
	if err != nil {
		return "", fail("resource_outputs_unverified")
	}
	return digest(struct{ Files, Evidence string }{snapshot.Digest, o.EvidenceDigest}), nil
}

// Seal only records an already-published output manifest. It does not complete
// the owner, accept the quality of a result or permit a Bot to reclassify data.
func (s *Service) Seal(ctx context.Context, scope Scope, id string) (taskresource.Resource, error) {
	if !s.ready() || !scope.Publish {
		return taskresource.Resource{}, fail("resource_publish_not_allowed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.resource(ctx, scope, id)
	if err != nil {
		return r, err
	}
	updated, err := s.sealResourceLocked(ctx, r)
	return visibleResource(scope, updated), err
}

func (s *Service) sealResourceLocked(ctx context.Context, r taskresource.Resource) (taskresource.Resource, error) {
	o, err := s.Owner(ctx, r.Owner.Kind, r.Owner.ID)
	if err != nil {
		return r, err
	}
	if o.Owner != r.Owner || !o.OutputsSaved {
		return r, fail("resource_outputs_unavailable")
	}
	hash, err := manifest(ctx, o)
	if err != nil {
		return r, err
	}
	return s.Store.UpdateTaskResource(ctx, r.ID, "resource.outputs_sealed", s.now(), func(next *taskresource.Resource) error {
		if next.LastOutcome == "removed" || next.LastOutcome == "removing" {
			return fail("resource_cleanup_in_progress")
		}
		next.Manifest = hash
		return nil
	})
}

func (s *Service) Pin(ctx context.Context, scope Scope, id string, pin bool) (taskresource.Resource, error) {
	if !s.ready() || (!pin && !scope.Unpin) || (pin && !scope.Inspect) {
		return taskresource.Resource{}, fail("resource_protection_not_allowed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.resource(ctx, scope, id)
	if err != nil {
		return r, err
	}
	updated, err := s.Store.UpdateTaskResource(ctx, id, "resource.protection_changed", s.now(), func(next *taskresource.Resource) error {
		if next.LastOutcome == "removed" || next.LastOutcome == "removing" {
			return fail("resource_cleanup_in_progress")
		}
		next.Pinned = pin
		return nil
	})
	return visibleResource(scope, updated), err
}

type Inspection struct {
	Resource    taskresource.Resource `json:"resource"`
	Decision    taskresource.Decision `json:"decision"`
	Snapshot    resourcefs.Snapshot   `json:"snapshot"`
	Manifest    string                `json:"current_manifest"`
	OwnerDigest string                `json:"owner_evidence_digest"`
}

func (s *Service) inspect(ctx context.Context, scope Scope, id string) (Inspection, error) {
	return s.inspectFor(ctx, scope, id, "")
}

func (s *Service) inspectFor(ctx context.Context, scope Scope, id, execution string) (Inspection, error) {
	r, err := s.resource(ctx, scope, id)
	if err != nil {
		return Inspection{}, err
	}
	i := Inspection{Resource: r}
	if r.LastOutcome == "removed" {
		i.Decision = taskresource.Decision{Resource: id, Outcome: "removed", Reason: r.LastReason, NextOwner: "none"}
		return i, nil
	}
	f := taskresource.Facts{ScopeAllowed: scope.allows(r)}
	o, ownerErr := s.Owner(ctx, r.Owner.Kind, r.Owner.ID)
	if ownerErr == nil && o.Owner == r.Owner {
		f.OwnerComplete, f.CompletedAt, f.OwnerPresence, f.UsePresence, f.OutputsSaved = o.Complete, o.CompletedAt, o.Presence, o.UsePresence, o.OutputsSaved
		if f.OwnerComplete && f.OwnerPresence == "absent" && f.UsePresence == "absent" && s.Uses != nil {
			f.UsePresence = s.Uses(ctx, r)
		}
		i.OwnerDigest = digest(struct {
			Complete   bool
			At         int64
			Owner, Use string
			Saved      bool
			Evidence   string
		}{o.Complete, o.CompletedAt, o.Presence, o.UsePresence, o.OutputsSaved, o.EvidenceDigest})
	}
	scanErr := s.FS.Verify(r)
	if scanErr == nil {
		f.PathOwned, f.IdentityMatches = true, true
	}
	evaluated := r
	sharedGo := r.Owner.Kind == "shared_go"
	if evaluated.LastOutcome == "removing" && execution != "" && evaluated.LastExecution == execution {
		evaluated.LastOutcome = ""
	}
	if scanErr == nil && (r.Pinned || !f.OwnerComplete || f.OwnerPresence != "absent" || f.UsePresence != "absent" || !f.OutputsSaved || (!sharedGo && r.Manifest == "") || evaluated.LastOutcome == "removing") {
		i.Decision = taskresource.Evaluate(evaluated, s.policy(), f, s.now())
		return i, nil
	}
	snapshot, scanErr := s.FS.Snapshot(ctx, r)
	if scanErr == nil {
		f.PathOwned, f.IdentityMatches = true, true
		i.Snapshot = snapshot
	}
	if ownerErr == nil && o.Owner == r.Owner {
		i.Manifest, err = manifest(ctx, o)
		if sharedGo {
			// Apply still compares this live proof with the reviewed plan and
			// checks for new readers under the consumer-attachment fence.
			evaluated.Manifest = i.Manifest
		}
		f.ManifestMatches = err == nil && i.Manifest == evaluated.Manifest
	}
	i.Decision = taskresource.Evaluate(evaluated, s.policy(), f, s.now())
	if scanErr != nil {
		i.Decision = taskresource.Decision{Resource: id, Outcome: "kept", Reason: "resource_tree_unverified", NextOwner: "broker"}
		if errors.Is(scanErr, resourcefs.ErrChanged) {
			i.Decision.Reason = "identity_changed"
		}
	}
	return i, nil
}

type Page struct {
	Items  []Inspection        `json:"items"`
	Next   string              `json:"next"`
	Policy taskresource.Policy `json:"policy"`
}

// Inventory is read-only, including its scans. A narrow scope never receives
// other Projects' rows; the cursor still advances over the bounded store page.
func (s *Service) Inventory(ctx context.Context, scope Scope, after string, limit int) (Page, error) {
	if !s.ready() || !scope.Inspect {
		return Page{}, fail("resource_inspection_not_allowed")
	}
	rows, next, err := s.Store.TaskResourcePage(ctx, after, limit, true)
	if err != nil {
		return Page{}, err
	}
	out := Page{Items: []Inspection{}, Next: next, Policy: s.policy()}
	for _, r := range rows {
		if !scope.allows(r) {
			continue
		}
		i, err := s.inspect(ctx, scope, r.ID)
		if err != nil {
			return Page{}, err
		}
		out.Items = append(out.Items, i)
	}
	return out, nil
}

type Plan struct {
	Digest string              `json:"digest"`
	Policy taskresource.Policy `json:"policy"`
	Items  []Inspection        `json:"items"`
}

func (s *Service) Plan(ctx context.Context, scope Scope, ids []string) (Plan, error) {
	if !s.ready() || !scope.Inspect {
		return Plan{}, fail("resource_inspection_not_allowed")
	}
	if len(ids) == 0 || len(ids) > taskresource.PageRowsLimit {
		return Plan{}, fail("resource_plan_size_invalid")
	}
	ids = append([]string(nil), ids...)
	sort.Strings(ids)
	out := Plan{Policy: s.policy(), Items: []Inspection{}}
	for n, id := range ids {
		if n > 0 && ids[n-1] == id {
			return Plan{}, fail("resource_plan_duplicate")
		}
		i, err := s.inspect(ctx, scope, id)
		if err != nil {
			return Plan{}, err
		}
		out.Items = append(out.Items, i)
	}
	out.Digest = digest(struct {
		Policy taskresource.Policy
		Items  []Inspection
	}{out.Policy, out.Items})
	return out, nil
}

type Receipt struct {
	Resource        string `json:"resource"`
	Outcome         string `json:"outcome"`
	Reason          string `json:"reason"`
	LogicalBytes    int64  `json:"logical_bytes"`
	AllocatedBytes  int64  `json:"allocated_bytes"`
	AllocationKnown bool   `json:"allocation_known"`
}
type Result struct {
	Execution      string    `json:"execution"`
	Plan           string    `json:"plan"`
	Items          []Receipt `json:"items"`
	OutcomeUnknown bool      `json:"outcome_unknown"`
	Replayed       bool      `json:"replayed"`
}

// Apply only removes exact resources from an unchanged reviewed plan. It shares
// the durable receipt mechanism used by the broker; a lost/orphaned request is
// reconciled, never blindly executed again.
func (s *Service) Apply(ctx context.Context, scope Scope, plan Plan, execution string) (Result, error) {
	if !scope.Reclaim {
		return Result{}, fail("resource_cleanup_not_allowed")
	}
	if u, err := uuid.Parse(execution); err != nil || u.String() != execution {
		return Result{}, fail("resource_execution_invalid")
	}
	ids := make([]string, 0, len(plan.Items))
	for _, i := range plan.Items {
		if _, err := s.resource(ctx, scope, i.Resource.ID); err != nil {
			return Result{}, err
		}
		ids = append(ids, i.Resource.ID)
	}
	if len(ids) == 0 || len(ids) > taskresource.PageRowsLimit {
		return Result{}, fail("resource_plan_size_invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := store.ReceiptKey{Scope: "resources.cleanup", Actor: scope.Actor, Key: execution}
	claim, err := s.Store.LookupReceipt(ctx, key)
	if err != nil {
		return Result{}, err
	}
	if claim.Outcome == store.ReceiptReplay {
		var out Result
		if json.Unmarshal(claim.Answer.Body, &out) != nil || out.Plan != plan.Digest {
			return Result{}, fail("resource_execution_conflict")
		}
		for _, row := range out.Items {
			if _, err := s.resource(ctx, scope, row.Resource); err != nil {
				return Result{}, err
			}
		}
		out.Replayed = true
		return out, nil
	}
	if claim.Outcome == store.ReceiptPending {
		return Result{}, fail("resource_execution_pending")
	}
	if claim.Outcome == store.ReceiptExpired {
		return Result{}, fail("resource_execution_expired")
	}
	if claim.Outcome != store.ReceiptOrphaned {
		fresh, err := s.Plan(ctx, scope, ids)
		if err != nil {
			return Result{}, err
		}
		if fresh.Digest != plan.Digest {
			return Result{}, fail("resource_plan_changed")
		}
		plan = fresh
	}
	claim, err = s.Store.ClaimReceipt(ctx, key, plan.Digest, store.ReceiptPolicy{}, s.now())
	if err != nil {
		return Result{}, err
	}
	if claim.Outcome != store.ReceiptNew && claim.Outcome != store.ReceiptOrphaned {
		return Result{}, fail("resource_execution_not_claimed")
	}
	out := Result{Execution: execution, Plan: plan.Digest, Items: []Receipt{}, OutcomeUnknown: claim.Outcome == store.ReceiptOrphaned}
	completed := false
	defer func() {
		if completed {
			return
		}
		unknown := Result{Execution: execution, Plan: plan.Digest, Items: []Receipt{}, OutcomeUnknown: true}
		for _, id := range ids {
			unknown.Items = append(unknown.Items, Receipt{Resource: id, Outcome: "unknown", Reason: "interrupted_execution"})
		}
		body, _ := json.Marshal(unknown)
		// If storage is still unavailable the pending receipt and per-resource
		// intent remain for recovery; never disguise an uncertain delete as no-op.
		_ = s.Store.CompleteReceipt(context.WithoutCancel(ctx), key, store.ReceiptAnswer{Status: 200, Body: body})
	}()
	for _, item := range plan.Items {
		r := item.Resource
		r.LastPlan, r.LastTargets = plan.Digest, strings.Join(ids, ",")
		if out.OutcomeUnknown {
			current, err := s.Store.TaskResource(ctx, r.ID)
			if err != nil {
				return Result{}, err
			}
			row := Receipt{Resource: r.ID, Outcome: "unknown", Reason: "interrupted_execution"}
			if current.LastExecution == execution && (current.LastOutcome == "removing" || current.LastOutcome == "removed") {
				absent, err := s.FS.Absent(current)
				if err == nil && absent {
					row.Outcome, row.Reason = "removed", "missing_after_recorded_intent"
					if current.LastOutcome != "removed" {
						if err := s.recordRecoveredRemoval(ctx, current, execution, scope.Actor, row.Reason); err != nil {
							return Result{}, err
						}
					}
				}
			}
			out.Items = append(out.Items, row)
			continue
		}
		fresh, err := s.inspect(ctx, scope, r.ID)
		if err != nil {
			return Result{}, err
		}
		row := Receipt{Resource: r.ID, Outcome: "kept", Reason: fresh.Decision.Reason}
		if fresh.Decision.Outcome != "eligible" || digest(fresh) != digest(item) {
			if fresh.Decision.Outcome == "eligible" {
				row.Reason = "resource_changed_after_plan"
			}
			out.Items = append(out.Items, row)
			continue
		}
		row, err = s.applyRemovalLocked(ctx, scope, item, r, execution)
		if err != nil {
			return Result{}, err
		}
		out.Items = append(out.Items, row)
	}
	body, _ := json.Marshal(out)
	if err := s.Store.CompleteReceipt(ctx, key, store.ReceiptAnswer{Status: 200, Body: body}); err != nil {
		return Result{}, fail("resource_receipt_unknown")
	}
	completed = true
	return out, nil
}

func (s *Service) applyRemovalLocked(ctx context.Context, scope Scope, item Inspection, r taskresource.Resource, execution string) (Receipt, error) {
	if r.Owner.Kind == "shared_go" {
		return s.trimSharedGoLocked(ctx, scope, item, r, execution)
	}
	row := Receipt{Resource: r.ID, Outcome: "kept", Reason: "resource_changed_before_remove"}
	if ready, err := s.prepareRemovalLocked(ctx, scope, item, r, execution); !ready || err != nil {
		return row, err
	}
	endChange := s.beginUsageChange()
	defer endChange()
	if err := s.FS.Remove(ctx, r, item.Snapshot.Digest); err != nil {
		row.Reason = "remove_failed"
	} else {
		row.Outcome, row.Reason = "removed", "completed_owned_scratch"
		row.LogicalBytes, row.AllocatedBytes, row.AllocationKnown = item.Snapshot.Logical, item.Snapshot.Allocated, item.Snapshot.AllocationKnown
	}
	return row, s.finishRemovalLocked(ctx, r, execution, scope.Actor, row)
}

func (s *Service) trimSharedGoLocked(ctx context.Context, scope Scope, item Inspection, r taskresource.Resource, execution string) (Receipt, error) {
	row := Receipt{Resource: r.ID, Outcome: "kept"}
	// This read informs a trim while mu fences managed writers. It leaves the
	// published measurement and admission ticket alone.
	u, err := s.measureUsage(ctx)
	if err != nil {
		row.Reason = Explain(ctx, err, "cleanup.measure").Code
		return row, nil
	}
	size := item.Snapshot.Logical
	if item.Snapshot.AllocationKnown {
		size = item.Snapshot.Allocated
	}
	policy := s.policy()
	target := policy.CacheBytes
	if u.CacheTotal > policy.CacheTotal {
		target = min(target, max(int64(0), size-(u.CacheTotal-policy.CacheTotal)))
	}
	if u.Available < policy.DiskReserve {
		target = min(target, max(int64(0), size-(policy.DiskReserve-u.Available)))
	}
	if size <= target {
		row.Reason = "shared_cache_within_budget"
		return row, nil
	}

	var endChange func()
	defer func() {
		if endChange != nil {
			endChange()
		}
	}()
	var intentErr error
	row.Reason = "shared_cache_no_trim_candidates"
	removed, trimErr := s.FS.TrimGoCache(ctx, r, item.Snapshot.Digest, target, func() (bool, error) {
		var ready bool
		ready, intentErr = s.prepareRemovalLocked(ctx, scope, item, r, execution)
		if ready {
			endChange = s.beginUsageChange()
		} else {
			row.Reason = "resource_changed_before_remove"
		}
		return ready, intentErr
	})
	if intentErr != nil {
		return Receipt{}, intentErr
	}
	if trimErr != nil {
		row.Reason = "shared_cache_trim_failed"
	} else if endChange != nil {
		row.Reason = "shared_cache_trimmed"
	}
	if endChange == nil {
		return row, nil
	}
	row.LogicalBytes, row.AllocatedBytes, row.AllocationKnown = removed.Logical, removed.Allocated, removed.AllocationKnown
	return row, s.finishRemovalLocked(ctx, r, execution, scope.Actor, row)
}

func (s *Service) prepareRemovalLocked(ctx context.Context, scope Scope, item Inspection, r taskresource.Resource, execution string) (bool, error) {
	if s.Fault != nil {
		if err := s.Fault("before_intent", r.ID); err != nil {
			return false, err
		}
	}
	if _, err := s.mark(ctx, r, execution, scope.Actor, "removing", "completed_owned_scratch"); err != nil {
		return false, err
	}
	if s.Fault != nil {
		if err := s.Fault("after_intent", r.ID); err != nil {
			return false, err
		}
	}
	// A fresh owner read after the intent catches readers or protection
	// changes between planning and the actual filesystem operation.
	check, err := s.inspectFor(ctx, scope, r.ID, execution)
	if err != nil || check.Decision.Outcome != "eligible" || check.Snapshot.Digest != item.Snapshot.Digest || check.Manifest != item.Manifest {
		_, err := s.mark(ctx, r, execution, scope.Actor, "kept", "resource_changed_before_remove")
		return false, err
	}
	return true, nil
}

func (s *Service) finishRemovalLocked(ctx context.Context, r taskresource.Resource, execution, actor string, row Receipt) error {
	if s.Fault != nil {
		if err := s.Fault("after_remove", r.ID); err != nil {
			return err
		}
	}
	_, err := s.mark(ctx, r, execution, actor, row.Outcome, row.Reason)
	return err
}

func (s *Service) recordRecoveredRemoval(ctx context.Context, r taskresource.Resource, execution, actor, reason string) error {
	endChange := s.beginUsageChange()
	defer endChange()
	_, err := s.mark(ctx, r, execution, actor, "removed", reason)
	return err
}

// The caller holds mu. Invalidate before touching the accounted tree and
// release readers only when both the change and its registry update settle.
func (s *Service) beginUsageChange() func() {
	s.statsMu.Lock()
	s.stats.Known = false
	s.admissionRevision++
	done := make(chan struct{})
	s.usageChange = done
	s.statsMu.Unlock()
	return func() {
		s.statsMu.Lock()
		s.usageChange = nil
		close(done)
		s.statsMu.Unlock()
	}
}

func (s *Service) mark(ctx context.Context, r taskresource.Resource, execution, actor, outcome, reason string) (taskresource.Resource, error) {
	return s.Store.UpdateTaskResource(ctx, r.ID, "resource."+outcome, s.now(), func(next *taskresource.Resource) error {
		if next.Generation != r.Generation || next.Identity != r.Identity {
			return fail("resource_identity_changed")
		}
		if outcome == "removing" && (next.Pinned || next.Manifest != r.Manifest || next.LastOutcome == "removed" || next.LastOutcome == "removing") {
			return fail("resource_protection_changed")
		}
		next.LastOutcome, next.LastReason, next.LastExecution, next.LastAt = outcome, reason, execution, s.now().Unix()
		next.LastActor = actor
		next.LastPlan, next.LastTargets = r.LastPlan, r.LastTargets
		return nil
	})
}

func digest(value any) string {
	body, _ := json.Marshal(value)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// Measure is explicit/background work. Dashboard reads only the cached result.
func (s *Service) Measure(ctx context.Context) (taskresource.Usage, error) {
	if !s.ready() {
		return taskresource.Usage{}, fail("resource_service_unavailable")
	}
	if err := s.lockAdmission(ctx); err != nil {
		return taskresource.Usage{}, measurementFailure("resource_measurement_unknown", err).WithContext(ctx, "measure.begin", "")
	}
	defer s.unlockAdmission()
	return s.measure(ctx)
}

func (s *Service) measure(ctx context.Context) (taskresource.Usage, error) {
	if s.FreeSpace == nil {
		return taskresource.Usage{}, fail("resource_service_unavailable")
	}
	for {
		s.statsMu.Lock()
		done := s.usageChange
		if done == nil {
			break
		}
		s.statsMu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
			return taskresource.Usage{}, measurementFailure("resource_measurement_unknown", ctx.Err()).WithContext(ctx, "measure.begin", "")
		}
	}
	if err := ctx.Err(); err != nil {
		s.statsMu.Unlock()
		return taskresource.Usage{}, measurementFailure("resource_measurement_unknown", err).WithContext(ctx, "measure.begin", "")
	}
	s.stats.Known = false
	s.admissionRevision++
	revision := s.admissionRevision
	s.statsMu.Unlock()
	u, err := s.measureUsage(ctx)
	s.statsMu.Lock()
	defer s.statsMu.Unlock()
	if revision != s.admissionRevision && (err == nil || errors.Is(err, fs.ErrNotExist) || errors.Is(err, resourcefs.ErrChanged)) {
		if ctx.Err() != nil {
			return taskresource.Usage{}, measurementFailure("resource_measurement_unknown", ctx.Err()).WithContext(ctx, "measure.complete", "")
		}
		return taskresource.Usage{}, measurementFailure("resource_measurement_changed", errMeasurementChanged).WithContext(ctx, "measure.revision", "")
	}
	if err != nil {
		return taskresource.Usage{}, err
	}
	s.stats = u
	return u, nil
}

func (s *Service) measureUsage(ctx context.Context) (taskresource.Usage, error) {
	if s.FreeSpace == nil {
		return taskresource.Usage{}, fail("resource_service_unavailable")
	}
	u := taskresource.Usage{Known: true}
	if s.OtherUsage != nil {
		var err error
		u, err = s.OtherUsage(ctx)
		if err != nil || !u.Known {
			return taskresource.Usage{}, measurementFailure("resource_legacy_measurement_unknown", err).WithContext(ctx, "measure.legacy", "")
		}
	}
	after := ""
	for {
		rows, next, err := s.Store.TaskResourcePage(ctx, after, taskresource.PageRowsLimit, true)
		if err != nil {
			return taskresource.Usage{}, measurementFailure("resource_measurement_unknown", err).WithContext(ctx, "measure.registry", "")
		}
		for _, r := range rows {
			if r.Class == taskresource.Artifact || r.Class == taskresource.Evidence {
				continue
			}
			snapshot, err := s.FS.Measure(ctx, r)
			if err != nil {
				return taskresource.Usage{}, measurementFailure("resource_measurement_unknown", err).WithContext(ctx, "measure.resource", r.ID)
			}
			size := snapshot.Logical
			if snapshot.AllocationKnown {
				size = snapshot.Allocated
			}
			if r.Class == taskresource.Cache {
				u.CacheTotal += size
				if size > u.LargestCache {
					u.LargestCache = size
				}
			} else if r.Class == taskresource.Scratch || r.Class == taskresource.Log {
				u.ScratchTotal += size
			}
		}
		if next == "" {
			break
		}
		after = next
	}
	free, err := s.FreeSpace()
	if err != nil {
		return taskresource.Usage{}, measurementFailure("resource_disk_space_unknown", err).WithContext(ctx, "measure.disk", "")
	}
	if err := ctx.Err(); err != nil {
		return taskresource.Usage{}, measurementFailure("resource_measurement_unknown", err).WithContext(ctx, "measure.complete", "")
	}
	u.Available = free
	return u, nil
}

func (s *Service) measureForAdmission(ctx context.Context) (taskresource.Usage, error) {
	var err error
	for range taskresource.AdmissionMeasurementAttemptsLimit {
		var u taskresource.Usage
		u, err = s.measure(ctx)
		if !errors.Is(err, errMeasurementChanged) {
			return u, err
		}
	}
	return taskresource.Usage{}, err
}

func (s *Service) Usage() taskresource.Usage {
	s.statsMu.Lock()
	defer s.statsMu.Unlock()
	return s.stats
}

// CacheOverTarget reports maintenance pressure without making an active or unsealed
// individual cache a reason to refuse otherwise affordable work.
func (s *Service) CacheOverTarget() bool {
	u := s.Usage()
	return u.Known && u.LargestCache > s.policy().CacheBytes
}

// Sweep uses the exact same plans and executor as an authorized Bot. It visits
// one advancing page per call, so a kept owner cannot starve later resources.
func (s *Service) Sweep(ctx context.Context, after string) (Result, string, error) {
	if err := s.recoverPage(ctx, after); err != nil {
		return Result{}, after, err
	}
	scope := Scope{Actor: "daemon", AllProjects: true, Inspect: true, Reclaim: true}
	page, err := s.Inventory(ctx, scope, after, taskresource.PageRowsLimit)
	if err != nil {
		return Result{}, "", err
	}
	ids := []string{}
	for _, i := range page.Items {
		if i.Decision.Outcome == "eligible" {
			ids = append(ids, i.Resource.ID)
		}
	}
	if len(ids) == 0 {
		return Result{Items: []Receipt{}}, page.Next, nil
	}
	plan, err := s.Plan(ctx, scope, ids)
	if err != nil {
		return Result{}, page.Next, err
	}
	result, err := s.Apply(ctx, scope, plan, uuid.NewString())
	return result, page.Next, err
}

// SweepResource limits automatic maintenance to one resource and its durable
// receipt. The caller owns admission serialization and the per-item deadline.
func (s *Service) SweepResource(ctx context.Context, id string) (Result, error) {
	scope := Scope{Actor: "daemon", AllProjects: true, Inspect: true, Reclaim: true}
	if err := s.prepareSweepResource(ctx, scope, id); err != nil {
		return Result{}, err
	}
	plan, err := s.Plan(ctx, scope, []string{id})
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if plan.Items[0].Decision.Outcome != "eligible" {
		return Result{}, nil
	}
	return s.Apply(ctx, scope, plan, uuid.NewString())
}

func (s *Service) prepareSweepResource(ctx context.Context, scope Scope, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.resource(ctx, scope, id)
	if err != nil {
		return err
	}
	if err := s.recoverResourceLocked(ctx, r); err != nil {
		return err
	}
	if r.Manifest == "" && r.LastOutcome != "removed" && r.LastOutcome != "removing" {
		// A failed first seal leaves the normal inspection to keep the resource.
		// Replacing an existing seal would hide evidence that outputs changed.
		_, _ = s.sealResourceLocked(ctx, r)
	}
	return ctx.Err()
}

// Admit first tries a bounded safe sweep, then refuses new managed heavy work if
// capacity is still unavailable. It never relaxes retention or kills a reader.
func (s *Service) Admit(ctx context.Context) error {
	ctx = withDefaultCaller(ctx, "resources.admit")
	if !s.ready() {
		return fail("resource_service_unavailable")
	}
	if err := s.lockAdmission(ctx); err != nil {
		return measurementFailure("resource_measurement_unknown", err).WithContext(ctx, "admission.wait", "")
	}
	defer s.unlockAdmission()
	u, err := s.measureForAdmission(ctx)
	if err != nil {
		return err
	}
	// A per-cache target is maintained by the periodic sweeper. Waiting for
	// optional owner/output inspection here can exhaust the compile lease
	// deadline even when measured aggregate capacity is available.
	if reason := taskresource.Admission(s.policy(), u); reason == "" {
		return nil
	}
	s.statsMu.Lock()
	after := s.admitAfter
	s.statsMu.Unlock()
	_, next, err := s.Sweep(ctx, after)
	if err != nil {
		return Explain(ctx, err, "admission.sweep")
	}
	s.statsMu.Lock()
	s.admitAfter = next
	s.statsMu.Unlock()
	u, err = s.measureForAdmission(ctx)
	if err != nil {
		return err
	}
	if reason := taskresource.Admission(s.policy(), u); reason != "" {
		return fail(reason)
	}
	return nil
}

// recoverPage only reconciles a prior intent; it never repeats its deletion.
// The service mutex fences every executor using this Store handle. Completing
// a pending receipt succeeds only for this handle; another live daemon's
// receipt cannot be taken over by this recovery path.
func (s *Service) recoverPage(ctx context.Context, after string) error {
	if !s.ready() {
		return fail("resource_service_unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, _, err := s.Store.TaskResourcePage(ctx, after, taskresource.PageRowsLimit, true)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if err := s.recoverResourceLocked(ctx, r); err != nil {
			return err
		}
	}
	return nil
}

// recoverResourceLocked reconciles only a previous execution. Its caller owns
// mu, so it cannot race this handle's live executor or repeat its deletion.
func (s *Service) recoverResourceLocked(ctx context.Context, r taskresource.Resource) error {
	if r.LastOutcome != "removing" || r.LastActor == "" || r.LastExecution == "" || r.LastPlan == "" {
		return nil
	}
	key := store.ReceiptKey{Scope: "resources.cleanup", Actor: r.LastActor, Key: r.LastExecution}
	claim, err := s.Store.LookupReceipt(ctx, key)
	if err != nil {
		return err
	}
	if claim.Outcome == store.ReceiptPending {
		unknown := Result{Execution: r.LastExecution, Plan: r.LastPlan, OutcomeUnknown: true, Items: []Receipt{}}
		ids := strings.Split(r.LastTargets, ",")
		if len(ids) > taskresource.PageRowsLimit {
			return nil
		}
		for _, id := range ids {
			unknown.Items = append(unknown.Items, Receipt{Resource: id, Outcome: "unknown", Reason: "interrupted_execution"})
		}
		body, _ := json.Marshal(unknown)
		if err := s.Store.CompleteReceipt(ctx, key, store.ReceiptAnswer{Status: 200, Body: body}); err != nil {
			return nil
		}
		claim, err = s.Store.LookupReceipt(ctx, key)
		if err != nil {
			return err
		}
	}
	permitted := claim.Outcome == store.ReceiptOrphaned
	if claim.Outcome == store.ReceiptReplay {
		var result Result
		permitted = json.Unmarshal(claim.Answer.Body, &result) == nil && result.Plan == r.LastPlan && result.OutcomeUnknown
	}
	if !permitted {
		return nil
	}
	absent, err := s.FS.Absent(r)
	if err != nil {
		return nil
	}
	if absent {
		return s.recordRecoveredRemoval(ctx, r, r.LastExecution, r.LastActor, "missing_after_recorded_intent")
	}
	if err := s.FS.Verify(r); err != nil {
		return nil
	}
	_, err = s.mark(ctx, r, r.LastExecution, r.LastActor, "kept", "interrupted_cleanup")
	return err
}
