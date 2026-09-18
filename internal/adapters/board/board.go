// Package board is the Project Board: the durable work items behind
// `GET/POST /v1/board`, and the read-time projection that turns them into the
// envelope the console was written against.
//
// # Two stores, one envelope
//
// This daemon owns its own board file. It also reads the Swift app's board,
// which on this machine holds 776 cards nobody has any other copy of, under the
// rules in docs/plan.md §4: O_RDONLY, never a rename, never a lock file, and a
// half-read file keeps the previous good reading rather than becoming an empty
// board. The two are merged for reading and kept apart for writing — a card
// that came from the other app is answered, never edited, because this process
// has no right to write that file and pretending otherwise would lose somebody
// else's work the first time both apps ran at once.
//
// # Why the shape is copied rather than designed
//
// `Sources/BoardCardContract.swift` states the wire card as an allow-list and
// says why: a field added to `progress` reached every card because `progress`
// was copied whole, and 62,801 bytes per read had no reader anywhere in the
// console. That file is the contract, so this package reproduces its key set
// rather than inventing a tidier one — the console is the same console.
package board

// The constants the envelope publishes about itself. Each is a hardcoded value
// in the Swift app too; they are named here so a reader can see that they were
// copied deliberately rather than guessed.
const (
	// WireSchemaVersion is what `schemaVersion` says on the wire. It is not the
	// storage version: `Sources/ProjectBoardStore.swift` keeps
	// `wireSchemaVersion = 1` and `storageSchemaVersion = 2` apart on purpose,
	// and the console refuses an answer whose wire version is not 1.
	WireSchemaVersion = 1
	// StorageSchemaVersion is what this daemon writes into its own file, and
	// the highest version it will read.
	StorageSchemaVersion = 2
	// MaximumSnapshotBytes is the budget the items list is cut to, and the
	// number published as `snapshotBudgetBytes`.
	MaximumSnapshotBytes = 1_000_000
	// MaximumSnapshotItems caps one page of items regardless of size, so a
	// project of small cards cannot make one response unbounded.
	MaximumSnapshotItems = 500
	// MaximumResponseBytes is the final guard on the serialized body. The item
	// cut happens before `responsibility`, `viewer` and `readState` are added,
	// so the body legitimately exceeds the snapshot budget and this is the real
	// ceiling.
	MaximumResponseBytes = 2 * 1024 * 1024
	// MaximumStoreBytes refuses a board file larger than this rather than
	// reading it. 32 MiB is the Swift app's number.
	MaximumStoreBytes = 32 * 1024 * 1024
	// MaximumItems and MaximumProjects bound what this daemon's own store may
	// grow to.
	MaximumItems    = 2_000
	MaximumProjects = 200
	// MaximumReceipts bounds the request-id ledger that makes a retry free.
	MaximumReceipts = 4_096
	// DefaultPageLimit and MaximumPageLimit bound `?audience=&limit=`.
	DefaultPageLimit = 30
	MaximumPageLimit = 200
	// MaximumCommandBytes is the admission boundary for one command body.
	MaximumCommandBytes = 64 * 1024
)

// Entitlement is what `board.entitlement` carries.
//
// It is a constant, and that is the whole answer to "how does the board tell a
// free machine from a Cloud one". `Sources/ProjectBoardStore.swift:4432` is a
// `private static let` with one value; `ProjectBoardIntegration.swift:617` and
// `net/board-mock.js:86` repeat the same literal; nothing reads a subscription,
// a CloudAccount or a Config to produce it, and nothing in the console reads
// the field back. The board is free for everyone. The paid distinction lives on
// `/v1/entitlements`, which the plan page reads, and never reached this
// envelope.
type Entitlement struct {
	State string `json:"state"`
	Label string `json:"label"`
}

// FreePreview is the only entitlement the board has ever published.
func FreePreview() Entitlement {
	return Entitlement{State: "free_preview", Label: "Currently free"}
}

// Mode is the word the console switches its whole vocabulary on. It is a pure
// function of `enabled` and has exactly two values.
func Mode(enabled bool) string {
	if enabled {
		return "board"
	}
	return "standard"
}

// Viewer is who is asking, and what they may do about it.
type Viewer struct {
	ID                string `json:"id"`
	CanWrite          bool   `json:"canWrite"`
	CanManage         bool   `json:"canManage"`
	NarrativeProvider string `json:"narrativeProvider"`
}

// Refusal is a typed reason a read or a write did not happen. It carries the
// status the route should answer with, so a caller never has to map a sentence
// back onto a number.
type Refusal struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (r Refusal) Error() string { return r.Code + ": " + r.Message }

// refuse is the short spelling used throughout this package.
func refuse(status int, code, message string) Refusal {
	return Refusal{Status: status, Code: code, Message: message}
}
