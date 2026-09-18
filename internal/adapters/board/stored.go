package board

// The durable record, key for key as the Swift app writes it. The JSON names
// are that app's Swift property names verbatim — it has no CodingKeys — so the
// tags here are a transcription, not a naming choice.
//
// Everything is a pointer or an omitempty where the Swift side is optional,
// because reading a field that is absent and reading one that is zero are
// different answers and the projection below depends on telling them apart.

// StoredState is the whole board document.
type StoredState struct {
	SchemaVersion      int               `json:"schemaVersion"`
	Revision           int64             `json:"revision"`
	Enabled            bool              `json:"enabled"`
	UpdatedAt          float64           `json:"updatedAt"`
	Projects           []StoredProject   `json:"projects"`
	Items              []StoredItem      `json:"items"`
	Receipts           []StoredReceipt   `json:"receipts"`
	GraphItems         map[string]string `json:"graphItems,omitempty"`
	ReceiptEvictions   *int              `json:"receiptEvictions,omitempty"`
	IngestionCoverage  []StoredCoverage  `json:"ingestionCoverage,omitempty"`
	ExplicitGraphItems map[string]string `json:"explicitGraphItems,omitempty"`
	GraphNodeItems     map[string]string `json:"graphNodeItems,omitempty"`
	TaskItems          map[string]string `json:"taskItems,omitempty"`
	EventSequence      *int64            `json:"eventSequence,omitempty"`
	PresentationEpoch  *int              `json:"presentationEpoch,omitempty"`
	NarrativeConsent   *string           `json:"narrativeConsent,omitempty"`
}

// StoredProject is a place work happens.
type StoredProject struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	ItemKeyHighWater *int   `json:"itemKeyHighWater,omitempty"`
}

// StoredReceipt is one applied command, kept so a retry is free.
type StoredReceipt struct {
	Actor     string `json:"actor"`
	Digest    string `json:"digest"`
	ItemID    string `json:"itemId"`
	RequestID string `json:"requestId"`
	Revision  int64  `json:"revision"`
	Status    int    `json:"status"`
}

// StoredCoverage records that a source could not be read completely. It is
// current projection coverage, not an eternal failure ledger: success
// elsewhere cannot erase a dropped source.
type StoredCoverage struct {
	Kind            string   `json:"kind"`
	Reason          string   `json:"reason"`
	SourceDigests   []string `json:"sourceDigests"`
	Saturated       bool     `json:"saturated"`
	FirstObservedAt float64  `json:"firstObservedAt"`
}

// StoredItem is one work item.
type StoredItem struct {
	ID        string  `json:"id"`
	Key       string  `json:"key"`
	ProjectID string  `json:"projectId"`
	Title     string  `json:"title"`
	Type      string  `json:"type"`
	State     string  `json:"state"`
	Summary   string  `json:"summary"`
	Owner     string  `json:"owner"`
	ParentID  *string `json:"parentId,omitempty"`
	CreatedAt float64 `json:"createdAt"`
	UpdatedAt float64 `json:"updatedAt"`

	Checklist   []StoredChecklistRow `json:"checklist"`
	Milestones  []StoredMilestone    `json:"milestones"`
	Artifacts   []StoredArtifact     `json:"artifacts"`
	Links       []StoredLink         `json:"links"`
	Obligations []StoredObligation   `json:"obligations"`
	Spans       []StoredSpan         `json:"spans"`
	Evidence    []StoredEvidence     `json:"evidence"`

	CurrentVerificationEvidenceID *string `json:"currentVerificationEvidenceId,omitempty"`
	CurrentVerificationSubject    *string `json:"currentVerificationSubject,omitempty"`
	CurrentLandingEvidenceID      *string `json:"currentLandingEvidenceId,omitempty"`
	CurrentArtifactAcceptanceID   *string `json:"currentArtifactAcceptanceId,omitempty"`

	ScopeRevision      *int64  `json:"scopeRevision,omitempty"`
	ScopeEventAt       *float64 `json:"scopeEventAt,omitempty"`
	ScopeEventOrdinal  *int64  `json:"scopeEventOrdinal,omitempty"`
	InferredSourceKey  *string `json:"inferredSourceKey,omitempty"`
	HistoryDroppedCount *int   `json:"historyDroppedCount,omitempty"`

	PendingHandoff     *StoredHandoff            `json:"pendingHandoff,omitempty"`
	SessionDeliveries  []StoredSessionDelivery   `json:"sessionDeliveries,omitempty"`
	CatalogDisposition *StoredCatalogDisposition `json:"catalogDisposition,omitempty"`
	Presentations      []StoredPresentation      `json:"presentations,omitempty"`
}

// StoredChecklistRow is one acceptance row.
type StoredChecklistRow struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Status     string  `json:"status"`
	Required   bool    `json:"required"`
	EvidenceID *string `json:"evidenceId,omitempty"`
}

// StoredMilestone is one named checkpoint.
type StoredMilestone struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// StoredArtifact is something the work produced.
type StoredArtifact struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	URL           string `json:"url"`
	Kind          string `json:"kind"`
	ReferenceOnly *bool  `json:"referenceOnly,omitempty"`
}

// StoredLink joins this item to a task, a session, a worktree or another item.
// The broker's task links are the main input to the progress projection.
type StoredLink struct {
	ID                  string   `json:"id"`
	Kind                string   `json:"kind"`
	TargetID            string   `json:"targetId"`
	Label               string   `json:"label"`
	Source              *string  `json:"source,omitempty"`
	Phase               *string  `json:"phase,omitempty"`
	Head                *string  `json:"head,omitempty"`
	AttemptState        *string  `json:"attemptState,omitempty"`
	StartedAt           *float64 `json:"startedAt,omitempty"`
	FinishedAt          *float64 `json:"finishedAt,omitempty"`
	SourceTaskID        *string  `json:"sourceTaskId,omitempty"`
	GraphID             *string  `json:"graphId,omitempty"`
	GraphNodeID         *string  `json:"graphNodeId,omitempty"`
	EventAt             *float64 `json:"eventAt,omitempty"`
	EventOrdinal        *int64   `json:"eventOrdinal,omitempty"`
	LandingDisposition  *string  `json:"landingDisposition,omitempty"`
	StatusObservedAt    *float64 `json:"statusObservedAt,omitempty"`
}

// StoredObligation is something still owed.
type StoredObligation struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Owner     string  `json:"owner"`
	Blocking  bool    `json:"blocking"`
	Resolved  bool    `json:"resolved"`
	ActorKind *string `json:"actorKind,omitempty"`
}

// StoredSpan is a declared interval of work. A span whose id is not the
// derived `span-<sha256>` form cannot be attributed, and the projection treats
// it as unresolved rather than as proof of activity.
type StoredSpan struct {
	ID        string   `json:"id"`
	SessionID string   `json:"sessionId"`
	Phase     string   `json:"phase"`
	StartedAt float64  `json:"startedAt"`
	EndedAt   *float64 `json:"endedAt,omitempty"`
	Source    *string  `json:"source,omitempty"`
	SourceID  *string  `json:"sourceId,omitempty"`
}

// StoredEvidence is a recorded fact: a verification, a landing, a finding.
// Unlike history it is not append-only — `resolved`, `status`, `eventAt` and
// `eventOrdinal` are updated in place, which is why it stays in the aggregate.
type StoredEvidence struct {
	ID            string   `json:"id"`
	Kind          string   `json:"kind"`
	Status        string   `json:"status"`
	At            float64  `json:"at"`
	Actor         string   `json:"actor"`
	Source        *string  `json:"source,omitempty"`
	SourceID      string   `json:"sourceId"`
	Subject       *string  `json:"subject,omitempty"`
	Summary       string   `json:"summary"`
	Blocking      bool     `json:"blocking"`
	Resolved      bool     `json:"resolved"`
	ScopeRevision *int64   `json:"scopeRevision,omitempty"`
	EventAt       *float64 `json:"eventAt,omitempty"`
	EventOrdinal  *int64   `json:"eventOrdinal,omitempty"`
	ChecklistID   *string  `json:"checklistId,omitempty"`
}

// StoredHandoff is an offered transfer that nobody has accepted yet.
type StoredHandoff struct {
	Note          string  `json:"note"`
	ProposedOwner string  `json:"proposedOwner"`
	Provider      *string `json:"provider,omitempty"`
	Status        *string `json:"status,omitempty"`
}

// StoredSessionDelivery is a session's own report that it delivered. It is an
// attestation, never a verification.
type StoredSessionDelivery struct {
	At             float64 `json:"at"`
	EventID        string  `json:"eventId"`
	SessionID      string  `json:"sessionId"`
	Provider       string  `json:"provider"`
	StartRequestID string  `json:"startRequestId"`
	Phase          string  `json:"phase"`
	Disposition    string  `json:"disposition"`
	ScopeRevision  int64   `json:"scopeRevision"`
}

// StoredCatalogDisposition is where a reconciliation decided this row belongs:
// which audience reads it, what part it plays, and under whom.
type StoredCatalogDisposition struct {
	Actor    string  `json:"actor"`
	At       float64 `json:"at"`
	Audience string  `json:"audience"`
	AuditID  string  `json:"auditId"`
	Outcome  string  `json:"outcome"`
	ParentID *string `json:"parentId,omitempty"`
	Reason   string  `json:"reason"`
	Role     string  `json:"role"`
}

// StoredPresentation is AI-authored prose about an item. It is presentation
// only and never advances a lifecycle.
type StoredPresentation struct {
	Status  string  `json:"status"`
	Locale  string  `json:"locale"`
	Title   string  `json:"title"`
	Summary string  `json:"summary"`
	Outcome *string `json:"outcome,omitempty"`
}

// scopeRevision reads the item's scope revision with the Swift app's default.
func (i StoredItem) scopeRevision() int64 {
	if i.ScopeRevision == nil {
		return 0
	}
	return *i.ScopeRevision
}

func (l StoredLink) attemptState() string {
	if l.AttemptState == nil {
		return ""
	}
	return *l.AttemptState
}

func (l StoredLink) source() string {
	if l.Source == nil {
		return "unknown"
	}
	return *l.Source
}

func (l StoredLink) ordinal() int64 {
	if l.EventOrdinal == nil {
		return 0
	}
	return *l.EventOrdinal
}

func (e StoredEvidence) ordinal() int64 {
	if e.EventOrdinal == nil {
		return 0
	}
	return *e.EventOrdinal
}

func (e StoredEvidence) scopeRevision() int64 {
	if e.ScopeRevision == nil {
		return 0
	}
	return *e.ScopeRevision
}

func (s StoredSpan) source() string {
	if s.Source == nil {
		return "user"
	}
	return *s.Source
}
