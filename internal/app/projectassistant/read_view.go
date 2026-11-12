package projectassistant

import (
	"context"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// ReadViews is optional and additive to Repository/EventFeed. Its adapter
// borrows a host-owned read pool and ends its short SQLite snapshot before
// returning. It does not load/Export the aggregate into Go or retain SQL rows.
type ReadViews interface {
	Read(context.Context, ReadViewQuery) (ReadView, error)
}

type ReadViewQuery struct {
	Request pa.ReadViewRequest
	// Both fields come from trusted composition/service authority, never the
	// wire. ByteLimit cannot exceed the existing Cloud plaintext ceiling.
	AuthorizationBinding string
	ByteLimit            int
	// A trusted lowered cursor budget, never above pa.MaxReadViewCursorBytes.
	CursorByteLimit int
}

// Head and every returned projection belong to one SQLite snapshot. Frontier
// is the machine log high-water mark, never a message sequence. History pages
// are live reads, not cross-request frozen snapshots: only snapshot mode may
// replace the client's event cursor; later pages reconcile through events.
type ReadViewHead struct {
	Key                    pa.BotKey
	Project                pa.ProjectScope
	Conversation           pa.Conversation
	CurrentFence           pa.Fence
	BotVersion             int64
	MemoryRevision         int64
	EventFrontier          int64
	UnknownTurnCount       int64
	PendingPermissionCount int64
	TraceCategoryCount     int64
}

type ReadTurnSummary struct {
	ID          string
	Fence       pa.Fence
	MessageID   string
	State       pa.TurnState
	FailureCode pa.ErrorCode
}

type ReadSnapshot struct {
	Bot            pa.Bot
	PendingTurns   []ReadTurnSummary
	QueuedMessages []pa.Message
	LastTurn       *ReadTurnSummary
}

// A summary is not enough to approve a native operation. DetailCursor fetches
// the complete current-fence offer/trace under fresh source authority.
type ReadPermissionSummary struct {
	ID           string
	Fence        pa.Fence
	TurnID       string
	State        pa.PermissionState
	ExpiresAt    time.Time
	DetailCursor string
}

type ReadTraceSummary struct {
	ID           string
	TurnID       string
	Category     string
	Complete     bool
	DetailCursor string
}

type ReadItemKind string

const (
	ReadItemMessage           ReadItemKind = "message"
	ReadItemWork              ReadItemKind = "work"
	ReadItemPermissionSummary ReadItemKind = "permission_summary"
	ReadItemTraceSummary      ReadItemKind = "trace_summary"
	ReadItemPermission        ReadItemKind = "permission"
	ReadItemTrace             ReadItemKind = "trace"
	ReadItemEvent             ReadItemKind = "event"
)

// Exactly one payload matches Kind. ObservedEventSeq prevents an older page
// from downgrading a newer work fact. AfterCursor/CursorFailure/Failure are
// internal packing metadata and never appear in the generated wire item.
type ReadViewItem struct {
	ID                string
	Kind              ReadItemKind
	AnchorMessageSeq  int64
	ObservedEventSeq  int64
	Message           *pa.Message
	Work              *pa.WorkLink
	PermissionSummary *ReadPermissionSummary
	TraceSummary      *ReadTraceSummary
	Permission        *pa.PermissionOffer
	Trace             *pa.Trace
	Event             *pa.Event
	AfterCursor       string
	CursorFailure     *pa.Failure
	Failure           *pa.Failure
}

type ReadView struct {
	Mode        pa.ReadViewMode
	SourceEpoch string
	Head        ReadViewHead
	Snapshot    *ReadSnapshot
	Items       []ReadViewItem
	HasMore     bool
	// Events uses the last included source event. Non-event modes leave this
	// zero; their head frontier must not advance an existing replay cursor.
	NextSeq int64
}
