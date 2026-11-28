// Package taskresource defines ownership and the evidence a cleanup may rest on.
// An inspection Bot supplies observations, never completion or deletion authority.
package taskresource

import (
	"math"
	"time"
)

const (
	ScratchRetentionSecondsLimit = 24 * 60 * 60
	LogRetentionSecondsLimit     = 7 * 24 * 60 * 60
	// ArtifactRetentionSecondsLimit is how long a finished task's artifacts/
	// are kept after the task ended and its landing settled (the person's
	// decision of 2026-11-23: 30 days). Its result.json, task.json and landing
	// record stay; only the artifact files go.
	ArtifactRetentionSecondsLimit       = 30 * 24 * 60 * 60
	CacheBytesLimit               int64 = 4 << 30
	CacheTotalBytesLimit          int64 = 16 << 30
	ScratchTotalBytesLimit        int64 = 16 << 30
	DiskReserveBytesLimit         int64 = 2 << 30
	PageRowsLimit                       = 64
	FilesLimit                          = 262144
	PreparationReadySecondsLimit        = 60
	// LowDiskAlertBytesLimit is the free space below which the person is told
	// once (capacity notice cooldown) what is using the disk.
	LowDiskAlertBytesLimit int64 = 20 << 30
	// DiskSourceScanFilesLimit bounds each largest-source measurement that
	// accompanies a low-disk notice; a larger tree is reported as unmeasured.
	DiskSourceScanFilesLimit = 2_000_000
)

// AdmissionMeasurementAttemptsLimit includes the original scan and one retry
// when a managed change overlaps accounting.
const AdmissionMeasurementAttemptsLimit = 2

type Class string

const (
	Cache    Class = "cache"
	Scratch  Class = "scratch"
	Log      Class = "execution_log"
	Artifact Class = "artifact"
	Evidence Class = "evidence"
)

// Owner is immutable. Project is the verified canonical Project directory,
// not a caller's proposed permission scope.
type Owner struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Project string `json:"project"`
}

// Resource names a daemon-created directory by an opaque identity. RelativePath
// is composed by the daemon under its managed root; registration cannot adopt an
// arbitrary directory. Retained rows explain removals after a restart.
type Resource struct {
	ID            string `json:"id"`
	Generation    string `json:"generation"`
	Owner         Owner  `json:"owner"`
	Class         Class  `json:"class"`
	RelativePath  string `json:"relative_path"`
	Identity      string `json:"filesystem_identity"`
	CreatedAt     int64  `json:"created_at"`
	Pinned        bool   `json:"pinned"`
	Manifest      string `json:"manifest_digest"`
	LastOutcome   string `json:"last_outcome"`
	LastReason    string `json:"last_reason"`
	LastExecution string `json:"last_execution"`
	LastActor     string `json:"last_actor"`
	LastPlan      string `json:"last_plan"`
	LastTargets   string `json:"last_targets"`
	LastAt        int64  `json:"last_at"`
}

type Policy struct {
	ScratchSeconds int64 `json:"scratch_seconds"`
	LogSeconds     int64 `json:"log_seconds"`
	CacheBytes     int64 `json:"cache_bytes"`
	CacheTotal     int64 `json:"cache_total_bytes"`
	ScratchTotal   int64 `json:"scratch_total_bytes"`
	DiskReserve    int64 `json:"disk_reserve_bytes"`
}

func DefaultPolicy() Policy {
	return Policy{ScratchRetentionSecondsLimit, LogRetentionSecondsLimit,
		CacheBytesLimit, CacheTotalBytesLimit, ScratchTotalBytesLimit, DiskReserveBytesLimit}
}

func (p Policy) Valid() bool {
	return p.ScratchSeconds > 0 && p.LogSeconds > 0 && p.CacheBytes > 0 &&
		p.CacheTotal >= p.CacheBytes && p.ScratchTotal > 0 && p.DiskReserve > 0
}

// Facts are read by the daemon from the source that owns each fact. Unknown is
// represented independently of false; missing process inventory is never idle.
type Facts struct {
	ScopeAllowed    bool
	PathOwned       bool
	IdentityMatches bool
	OwnerComplete   bool
	CompletedAt     int64
	OwnerPresence   string // absent, present, unknown
	UsePresence     string // absent, present, unknown
	OutputsSaved    bool
	ManifestMatches bool
}

type Decision struct {
	Resource   string `json:"resource"`
	Outcome    string `json:"outcome"`
	Reason     string `json:"reason"`
	NextOwner  string `json:"next_owner"`
	EligibleAt int64  `json:"eligible_at,omitempty"`
}

// Evaluate cannot be relaxed by a Bot verdict, a large byte count or disk pressure.
// The executor must obtain fresh Facts again immediately before unlinking.
func Evaluate(r Resource, p Policy, f Facts, now time.Time) Decision {
	d := Decision{Resource: r.ID, Outcome: "kept", NextOwner: "broker"}
	keep := func(reason string) Decision { d.Reason = reason; return d }
	switch r.LastOutcome {
	case "removing":
		return keep("cleanup_in_progress")
	case "removed":
		return keep("already_removed")
	case "", "kept":
	default:
		return keep("lifecycle_unknown")
	}
	if r.Class == Artifact || r.Class == Evidence {
		return keep("protected_class")
	}
	if r.Class != Cache && r.Class != Scratch && r.Class != Log {
		return keep("class_unknown")
	}
	if !p.Valid() {
		return keep("policy_unknown")
	}
	if !f.ScopeAllowed {
		return keep("scope_not_allowed")
	}
	if r.Pinned {
		d.NextOwner = "person"
		return keep("pinned")
	}
	if !f.PathOwned {
		return keep("path_not_owned")
	}
	if !f.IdentityMatches {
		return keep("identity_changed")
	}
	if !f.OwnerComplete || f.CompletedAt <= 0 {
		d.NextOwner = "owner"
		return keep("completion_missing")
	}
	if f.OwnerPresence != "absent" {
		return keep("owner_" + presence(f.OwnerPresence))
	}
	if f.UsePresence != "absent" {
		return keep("use_" + presence(f.UsePresence))
	}
	if !f.OutputsSaved || r.Manifest == "" {
		d.NextOwner = "owner"
		return keep("outputs_unsealed")
	}
	if !f.ManifestMatches {
		return keep("outputs_changed")
	}
	retention := p.ScratchSeconds
	if r.Class == Cache {
		retention = 0
	}
	if r.Class == Log {
		retention = p.LogSeconds
	}
	// Subtraction avoids overflowing a supplied historical timestamp plus retention.
	start := max(f.CompletedAt, r.CreatedAt)
	if now.Unix() < start || now.Unix()-start < retention {
		if start <= now.Unix() && start <= math.MaxInt64-retention {
			d.EligibleAt = start + retention
		}
		return keep("within_retention")
	}
	d.Outcome, d.Reason, d.NextOwner = "eligible", "completed_owned_scratch", "daemon"
	return d
}

func presence(p string) string {
	if p == "present" {
		return "present"
	}
	return "unknown"
}

// Usage excludes protected artifacts from eviction budgets. Callers still show
// their space separately. Roots must be disjoint so nested caches count once.
type Usage struct {
	Known        bool  `json:"known"`
	LargestCache int64 `json:"largest_cache_bytes"`
	CacheTotal   int64 `json:"cache_total_bytes"`
	ScratchTotal int64 `json:"scratch_total_bytes"`
	Available    int64 `json:"available_bytes"`
}

// Admission protects aggregate capacity and free space between commands. An
// individual cache target triggers safe eviction, not refusal while its owner
// is still running or publishing outputs. Refusal never authorizes killing a
// producer or removing protected data.
func Admission(p Policy, u Usage) string {
	if !p.Valid() || !u.Known || u.LargestCache < 0 || u.CacheTotal < 0 || u.ScratchTotal < 0 || u.Available < 0 {
		return "storage_unknown"
	}
	if u.Available < p.DiskReserve {
		return "disk_reserve_unavailable"
	}
	if u.CacheTotal > p.CacheTotal {
		return "cache_total_over_budget"
	}
	if u.ScratchTotal > p.ScratchTotal {
		return "scratch_over_budget"
	}
	return ""
}
