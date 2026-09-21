package board

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
)

// LegacyPath is where the Swift app keeps its board. CLAWDLINE_BOARD_STORE is
// that app's own override and is honoured for the same reason: a test or a
// second installation moves both apps' idea of the file together.
func LegacyPath() string {
	if v := os.Getenv("CLAWDLINE_BOARD_STORE"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if runtime.GOOS != "darwin" {
		// The Swift app only runs on macOS. Elsewhere there is nothing to read,
		// and an empty path is the honest answer rather than a guessed one.
		return ""
	}
	return filepath.Join(home, "Library", "Application Support", "Clawdline", "project-board.json")
}

// Legacy reads the Swift app's board and only reads it.
//
// The rules are docs/plan.md §4: one O_RDONLY open, never a rename, never a
// lock file, never a write. The other app rewrites this file about 580 times a
// day, so a half-read or a newer-than-understood document keeps the previous
// good reading; with no previous reading the answer is "unknown", never an
// empty board — an empty board is a claim that the 776 cards are gone.
type Legacy struct {
	path string
	// disabled is the legacy switch (swiftstore/legacy.go), taken when the
	// reader was opened: the file is never opened then.
	disabled bool

	mu      sync.Mutex
	stamp   fileStamp
	state   *StoredState
	readAt  time.Time
	lastErr error
}

type fileStamp struct {
	size  int64
	mtime time.Time
}

// OpenLegacy prepares a reader. It opens nothing yet, and with the legacy
// switch off (CLAWDLINE_NEXT_LEGACY_STORE=off) it never will.
func OpenLegacy(path string) *Legacy {
	return &Legacy{path: path, disabled: swiftstore.Disabled()}
}

// ErrLegacyAbsent means there is no Swift board on this machine. That is the
// ordinary state on Linux, Windows and a Mac that never ran the old app.
var ErrLegacyAbsent = errors.New("the Swift app has no board on this machine")

// ErrLegacyDisabled means this daemon was told not to read the Swift board
// (cutover B1). Like absent it is known and holds no cards; unlike absent the
// cards may be there, and the answer says which it is.
var ErrLegacyDisabled = errors.New("reading the Swift app's board is switched off")

// Read returns the latest good reading, re-reading only when the file's size
// or time has moved. The returned state is shared and must not be modified.
func (l *Legacy) Read() (*StoredState, time.Time, error) {
	if l != nil && l.disabled {
		return nil, time.Time{}, ErrLegacyDisabled
	}
	if l == nil || l.path == "" {
		return nil, time.Time{}, ErrLegacyAbsent
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	info, err := os.Stat(l.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, time.Time{}, ErrLegacyAbsent
	}
	if err != nil {
		return l.carry(err)
	}
	stamp := fileStamp{size: info.Size(), mtime: info.ModTime()}
	if l.state != nil && stamp == l.stamp {
		return l.state, l.readAt, nil
	}
	if info.Size() > MaximumStoreBytes {
		return l.carry(refuse(503, "board_store_too_large",
			"The Swift app's board is larger than this daemon will read."))
	}

	file, err := os.OpenFile(l.path, os.O_RDONLY, 0)
	if err != nil {
		return l.carry(err)
	}
	body, err := io.ReadAll(io.LimitReader(file, MaximumStoreBytes+1))
	file.Close()
	if err != nil {
		return l.carry(err)
	}
	var state StoredState
	if err := json.Unmarshal(body, &state); err != nil {
		// The other app writes atomically, so a torn read is unlikely; a
		// document that does not parse is still not evidence the board is empty.
		return l.carry(refuse(503, "board_store_corrupt",
			"The Swift app's board is not valid JSON right now."))
	}
	if state.SchemaVersion < 1 || state.SchemaVersion > StorageSchemaVersion {
		return l.carry(refuse(503, "board_store_version_unsupported",
			"The Swift app's board is a version this daemon does not read."))
	}
	l.state, l.stamp, l.readAt, l.lastErr = &state, stamp, time.Now(), nil
	return l.state, l.readAt, nil
}

// carry answers with the previous good reading when there is one. The error is
// kept so the envelope can say the reading is stale rather than pretend.
func (l *Legacy) carry(err error) (*StoredState, time.Time, error) {
	l.lastErr = err
	if l.state != nil {
		return l.state, l.readAt, errStale{err}
	}
	return nil, time.Time{}, err
}

// errStale wraps a failure that was answered with an older good reading.
type errStale struct{ cause error }

func (e errStale) Error() string { return "stale: " + e.cause.Error() }
func (e errStale) Unwrap() error { return e.cause }

// IsStale reports whether a Read answered with an older reading.
func IsStale(err error) bool {
	var stale errStale
	return errors.As(err, &stale)
}

// declaredSpanID is the Swift app's `declaredSpanID(actor:requestID:)`: the
// identity a session's declared interval must carry to be attributable.
func declaredSpanID(actor, requestID string) string {
	sum := sha256.Sum256([]byte(actor + "\x00" + requestID))
	return "span-" + hex.EncodeToString(sum[:])
}
