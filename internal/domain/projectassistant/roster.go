package projectassistant

// Roster subscriptions retain only content-free wake-ups. Durable changes are
// replayed in MaxEventPageRows batches; snapshots use MaxPageRows.
const (
	MaxRosterSubscribers         = 64
	MaxRosterWakeups             = 1
	MaxRosterLifetimeSeconds     = 3600
	MaxRosterRecheckMilliseconds = 250
)

type RosterRequest struct {
	MachineID string `json:"machine_id"`
	ProjectID string `json:"project_id,omitempty"`
	Cursor    string `json:"cursor,omitempty"`
}

// Revision is the durable roster journal sequence, independent of message and
// Bot versions. Clients replace by key/revision, including removal tombstones.
type RosterChange struct {
	Revision int64       `json:"revision"`
	Key      BotKey      `json:"key"`
	Removed  bool        `json:"removed"`
	Summary  *BotSummary `json:"summary,omitempty"`
}

// A cursor is resumable only after the entire frame has been applied. During
// snapshot pagination it retains the initial replay frontier, never a later one.
type RosterFrameKind string

const (
	RosterHello            RosterFrameKind = "hello"
	RosterSnapshot         RosterFrameKind = "snapshot"
	RosterSnapshotComplete RosterFrameKind = "snapshot-complete"
	RosterChanges          RosterFrameKind = "changes"
	RosterResync           RosterFrameKind = "resync"
	RosterClosure          RosterFrameKind = "closure"
)

type RosterFrame struct {
	Type        RosterFrameKind `json:"type"`
	SourceEpoch string          `json:"source_epoch,omitempty"`
	Cursor      string          `json:"cursor,omitempty"`
	Changes     []RosterChange  `json:"changes,omitempty"`
	Failure     *Failure        `json:"failure,omitempty"`
}
