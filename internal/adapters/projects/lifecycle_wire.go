package projects

import "time"

// The lifecycle's wire shape, key for key and null for null as
// ProjectWorktreeLifecycleService.snapshotJSON writes it. It is declared here
// rather than generated from api/v1/projects.schema.json because the schema's
// nullable integers and booleans are not something tools/contract-gen can
// render yet: it would emit a plain int64, and a count that could not be read
// would reach the page as zero. The schema remains the contract; these types
// are held to it by name.

type Snapshot struct {
	SchemaVersion int             `json:"schemaVersion"`
	Project       SnapshotProject `json:"project"`
	Repository    SnapshotRepo    `json:"repository"`
	ObservedAt    *string         `json:"observedAt"`
	Complete      bool            `json:"complete"`
	Error         *Issue          `json:"error"`
	Rows          []SnapshotRow   `json:"rows"`
	Truncated     bool            `json:"truncated"`
	Counts        SnapshotCounts  `json:"counts"`
}

type SnapshotProject struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type SnapshotRepo struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	CanonicalPath string `json:"canonicalPath"`
}

type SnapshotCounts struct {
	Rows         *int64 `json:"rows"`
	Active       *int64 `json:"active"`
	Staged       *int64 `json:"staged"`
	Modified     *int64 `json:"modified"`
	Untracked    *int64 `json:"untracked"`
	StorageBytes *int64 `json:"storageBytes"`
	Unknown      *int64 `json:"unknown"`
}

type SnapshotRow struct {
	WorktreeID      string               `json:"worktreeId"`
	Path            string               `json:"path"`
	Branch          *string              `json:"branch"`
	Base            *string              `json:"base"`
	Head            *string              `json:"head"`
	Target          *string              `json:"target"`
	Owner           RowOwner             `json:"owner"`
	Active          *bool                `json:"active"`
	Status          RowStatus            `json:"status"`
	Classifications []string             `json:"classifications"`
	Local           LocalObservation     `json:"localObservation"`
	Canonical       CanonicalObservation `json:"canonicalTargetObservation"`
	Context         RowContext           `json:"context"`
	Storage         RowStorage           `json:"storage"`
	Cleanup         RowCleanup           `json:"cleanup"`
}

type RowOwner struct {
	TaskID     *string `json:"taskId"`
	SessionID  *string `json:"sessionId"`
	TerminalID *string `json:"terminalId"`
	Title      *string `json:"title"`
	Evidence   string  `json:"evidence"`
}

type RowStatus struct {
	Complete  bool   `json:"complete"`
	Staged    *int64 `json:"staged"`
	Modified  *int64 `json:"modified"`
	Untracked *int64 `json:"untracked"`
}

type LocalObservation struct {
	State      string  `json:"state"`
	ObservedAt *string `json:"observedAt"`
	Head       *string `json:"head"`
	Error      *Issue  `json:"error"`
}

type CanonicalObservation struct {
	State      string  `json:"state"`
	ObservedAt *string `json:"observedAt"`
	Ref        *string `json:"ref"`
	OID        *string `json:"oid"`
	Error      *Issue  `json:"error"`
}

type RowContext struct {
	Purpose       *string       `json:"purpose"`
	Note          *string       `json:"note"`
	CurrentStatus *string       `json:"currentStatus"`
	State         string        `json:"state"`
	CreatedAt     *string       `json:"createdAt"`
	StartedAt     *string       `json:"startedAt"`
	FinishedAt    *string       `json:"finishedAt"`
	OriginSession OriginSession `json:"originSession"`
	Evidence      string        `json:"evidence"`
}

type OriginSession struct {
	SessionID *string `json:"sessionId"`
	Title     *string `json:"title"`
}

type RowStorage struct {
	Complete   bool    `json:"complete"`
	Bytes      *int64  `json:"bytes"`
	ObservedAt *string `json:"observedAt"`
	Error      *Issue  `json:"error"`
}

type RowCleanup struct {
	Eligible  bool    `json:"eligible"`
	Blockers  []Issue `json:"blockers"`
	NextOwner *string `json:"nextOwner"`
}

func str(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func i64(v int64) *int64 { return &v }

// rfc3339 is `.withInternetDateTime`: UTC, whole seconds.
func rfc3339(t *time.Time) *string {
	if t == nil {
		return nil
	}
	v := t.UTC().Format("2006-01-02T15:04:05Z")
	return &v
}

func notObserved(p Identity) *Snapshot {
	return &Snapshot{
		SchemaVersion: schemaVersion,
		Project:       SnapshotProject{ID: p.ID, Label: p.Label},
		Repository:    SnapshotRepo{ID: RepositoryID(p.CanonicalPath), Label: p.Label, CanonicalPath: p.CanonicalPath},
		Error:         &Issue{"not_observed", "This Mac has not observed these worktrees yet; request a refresh."},
		Rows:          []SnapshotRow{},
	}
}

func encode(obs observation, now time.Time) *Snapshot {
	stale := now.Sub(obs.observedAt) > localFreshness
	sound := obs.err == nil && !obs.truncated
	complete := sound && !stale
	for _, r := range obs.rows {
		if !r.statusComplete || len(r.errors) > 0 || r.active == nil || r.storageBytes == nil || r.storageError != nil {
			complete = false
		}
	}
	sum := func(value func(row) *int64) *int64 {
		if !sound {
			return nil
		}
		var total int64
		for _, r := range obs.rows {
			v := value(r)
			if v == nil {
				return nil
			}
			total += *v
			if total > 9_007_199_254_740_991 {
				return nil
			}
		}
		return &total
	}
	counts := SnapshotCounts{
		Staged:       sum(func(r row) *int64 { return r.staged }),
		Modified:     sum(func(r row) *int64 { return r.modified }),
		Untracked:    sum(func(r row) *int64 { return r.untracked }),
		StorageBytes: sum(func(r row) *int64 { return r.storageBytes }),
	}
	if sound {
		counts.Rows = i64(int64(len(obs.rows)))
		var unknown, active int64
		allKnown := true
		for _, r := range obs.rows {
			for _, c := range r.classifications {
				if c == classUnknownEvidence {
					unknown++
				}
			}
			if r.active == nil {
				allKnown = false
			} else if *r.active {
				active++
			}
		}
		counts.Unknown = i64(unknown)
		if allKnown {
			counts.Active = i64(active)
		}
	}
	observedAt := obs.observedAt
	rows := make([]SnapshotRow, 0, len(obs.rows))
	for _, r := range obs.rows {
		rows = append(rows, encodeRow(r, obs, now))
	}
	return &Snapshot{
		SchemaVersion: schemaVersion,
		Project:       SnapshotProject{ID: obs.project.ID, Label: obs.project.Label},
		Repository: SnapshotRepo{ID: obs.repositoryID, Label: obs.project.Label,
			CanonicalPath: obs.project.CanonicalPath},
		ObservedAt: rfc3339(&observedAt),
		Complete:   complete,
		Error:      obs.err,
		Rows:       rows,
		Truncated:  obs.truncated,
		Counts:     counts,
	}
}

func encodeRow(r row, obs observation, now time.Time) SnapshotRow {
	age := now.Sub(obs.observedAt)
	observedAt := obs.observedAt
	local := LocalObservation{ObservedAt: rfc3339(&observedAt), Head: str(r.head)}
	var failure, missing *Issue
	for i := range r.errors {
		e := r.errors[i]
		if e.Code != "worktree_path_missing" && failure == nil {
			failure = &e
		}
		if missing == nil {
			missing = &e
		}
	}
	switch {
	case failure != nil:
		local.State, local.Error = "failed", failure
	case missing != nil:
		local.State, local.Error = "unknown", missing
	case obs.err != nil:
		local.State, local.Error = "stale", obs.err
	case age > localFreshness:
		local.State = "stale"
	default:
		local.State = "current"
	}

	t := obs.target
	var canonical CanonicalObservation
	switch {
	case t.err != nil:
		canonical = CanonicalObservation{State: "failed", Ref: str(t.localRef), OID: str(t.localOID), Error: t.err}
	case t.remoteRef != "":
		canonical = CanonicalObservation{ObservedAt: rfc3339(t.remoteObservedAt), Ref: str(t.remoteRef), OID: str(t.remoteOID)}
		switch {
		case t.remoteObservedAt == nil:
			canonical.State = "unknown"
			canonical.Error = &Issue{"canonical_remote_unobserved", "No fetch of the canonical remote has been recorded."}
		case now.Sub(*t.remoteObservedAt) <= canonicalFreshness:
			canonical.State = "current"
		default:
			canonical.State = "stale"
		}
	default:
		state := "current"
		if age > localFreshness {
			state = "stale"
		}
		canonical = CanonicalObservation{State: state, ObservedAt: rfc3339(&observedAt),
			Ref: str(t.localRef), OID: str(t.localOID)}
	}

	status := RowStatus{Complete: r.statusComplete}
	if r.statusComplete {
		status.Staged, status.Modified, status.Untracked = r.staged, r.modified, r.untracked
	}
	var purpose *string
	if r.isMain {
		purpose = str("Canonical checkout for " + obs.project.Label)
	} else {
		purpose = str(r.owner.purpose)
	}
	state := r.owner.taskState
	if state == "" {
		state = "unknown"
		if r.isMain {
			state = "repository"
		}
	}
	storage := RowStorage{Complete: r.storageBytes != nil && r.storageError == nil, Bytes: r.storageBytes,
		ObservedAt: rfc3339(r.storageObserved), Error: r.storageError}
	if r.storageObserved == nil && r.storageError == nil {
		storage.Error = &Issue{"storage_not_observed", "Disk usage has not been observed."}
	}
	cleanup := RowCleanup{Eligible: len(r.actions) > 0 && len(r.blockers) == 0, Blockers: r.blockers}
	if cleanup.Blockers == nil {
		cleanup.Blockers = []Issue{}
	}
	if len(r.blockers) > 0 {
		cleanup.NextOwner = str(r.owner.nextOwner())
	}
	classes := r.classifications
	if classes == nil {
		classes = []string{}
	}
	return SnapshotRow{
		WorktreeID: r.worktreeID, Path: r.path,
		Branch: str(r.branch), Base: str(r.base()), Head: str(r.head), Target: str(t.branch),
		Owner: RowOwner{TaskID: str(r.owner.taskID), SessionID: str(r.owner.sessionID),
			TerminalID: str(r.owner.terminalID), Title: str(r.owner.title), Evidence: r.owner.evidence},
		Active:          r.active,
		Status:          status,
		Classifications: classes,
		Local:           local,
		Canonical:       canonical,
		Context: RowContext{Purpose: purpose, Note: str(r.owner.note), CurrentStatus: str(r.owner.currentStatus),
			State: state, CreatedAt: rfc3339(r.owner.createdAt), StartedAt: rfc3339(r.owner.startedAt),
			FinishedAt:    rfc3339(r.owner.finishedAt),
			OriginSession: OriginSession{SessionID: str(r.owner.originSession), Title: str(r.owner.originTitle)},
			Evidence:      r.owner.evidence},
		Storage: storage,
		Cleanup: cleanup,
	}
}
