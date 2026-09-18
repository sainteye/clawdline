package board

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Settings is this daemon's old board document, `project-board.json`, read
// and never written (design-decisions D37).
//
// The two board-level facts it held — whether the board is on, and which
// assistant may read it for narratives — live in clawdline.sqlite3 now, with
// their revision, and a command's receipt is the store's one receipt table
// (D03). The receipts this document kept were evicted oldest first by count
// and replayed with whatever revision the board had reached since; both are
// gone with it. What is left here is the reading: once, when the store has no
// row yet, so that a person's setting is carried over rather than reset — and
// the rules a command is checked by, which do not depend on where the answer
// is kept.
type Settings struct {
	path string
	mu   sync.Mutex
}

type settingsFile struct {
	SchemaVersion     int             `json:"schemaVersion"`
	Revision          int64           `json:"revision"`
	Enabled           bool            `json:"enabled"`
	NarrativeConsent  *string         `json:"narrativeConsent"`
	PresentationEpoch int             `json:"presentationEpoch"`
	UpdatedAt         float64         `json:"updatedAt"`
	Receipts          []StoredReceipt `json:"receipts"`
	ReceiptEvictions  int             `json:"receiptEvictions"`
}

// SettingsState is one reading of the board-level facts.
type SettingsState struct {
	Revision         int64
	Enabled          bool
	NarrativeConsent *string
	UpdatedAt        float64
}

// OpenSettings names this daemon's board document inside its own directory.
// Nothing is created until the first write.
func OpenSettings(dir string) *Settings {
	return &Settings{path: filepath.Join(dir, "project-board.json")}
}

func (s *Settings) load() (settingsFile, error) {
	body, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		// A fresh board is enabled, as the Swift app's `StoredState.empty` is.
		return settingsFile{SchemaVersion: StorageSchemaVersion, Enabled: true}, nil
	}
	if err != nil {
		return settingsFile{}, refuse(503, "board_store_unreadable", err.Error())
	}
	if len(body) > MaximumStoreBytes {
		return settingsFile{}, refuse(503, "board_store_too_large",
			"This daemon's board document is larger than it will read.")
	}
	var f settingsFile
	if err := json.Unmarshal(body, &f); err != nil {
		return settingsFile{}, refuse(503, "board_store_corrupt",
			"This daemon's board document is not valid JSON and was left untouched.")
	}
	if f.SchemaVersion < 1 || f.SchemaVersion > StorageSchemaVersion {
		return settingsFile{}, refuse(503, "board_store_version_unsupported",
			"This daemon's board document is a version it does not read.")
	}
	return f, nil
}

// Command is one board write, in the Swift app's envelope.
type Command struct {
	Operation        string `json:"operation"`
	RequestID        string `json:"requestId"`
	ExpectedRevision *int64 `json:"expectedRevision"`
	// Operation-specific fields, read by name.
	Enabled  *bool   `json:"enabled,omitempty"`
	Provider *string `json:"provider,omitempty"`
	Policy   *string `json:"policy,omitempty"`
	ItemID   *string `json:"itemId,omitempty"`
}

// Outcome is what a command did.
type Outcome struct {
	Revision int64
	Replayed bool
}

// boardOperations is every operation the Swift app's board accepts. Anything
// outside it is a typo and answers `unknown_operation`, as that app does; the
// ones inside it that this daemon does not perform yet answer a different,
// named refusal so that "not yet" is never mistaken for "no such thing".
var boardOperations = map[string]bool{
	"set_ai_consent": true, "set_enabled": true, "create": true, "update": true,
	"transition": true, "checklist": true, "milestone": true, "record_session_delivery": true,
	"record_root_landing": true, "artifact": true, "record_output": true,
	"plan_structure": true, "approve_program_gate": true, "program_binding": true,
	"reconcile_historical_task_binding": true, "reconcile_catalog": true,
	"document_reference": true, "link": true, "obligation": true, "resolve_obligation": true,
	"span": true, "end_span": true, "assign_session": true, "decide_session_assignment": true,
	"cancel_session_assignment": true, "handoff": true, "accept_handoff": true,
	"accept_artifact": true, "record_evidence": true, "record_report": true,
}

// ManageOperations need `viewer.canManage`, not only `canWrite`.
var ManageOperations = map[string]bool{"set_enabled": true, "set_ai_consent": true, "accept_artifact": true}

// ValidateCommand is the part of a command that is about the command alone:
// its envelope, and whether this daemon performs that operation at all.
func ValidateCommand(actor string, c Command) error {
	if c.Operation == "" || len(c.Operation) > 64 || c.RequestID == "" ||
		len(c.RequestID) > 200 || c.ExpectedRevision == nil || *c.ExpectedRevision < 0 ||
		actor == "" || len(actor) > 300 {
		return refuse(400, "invalid_command",
			"A board command needs an operation, a requestId and an expectedRevision.")
	}
	if !boardOperations[c.Operation] {
		return refuse(400, "unknown_operation", "That is not a board operation.")
	}
	if c.Operation != "set_enabled" && c.Operation != "set_ai_consent" {
		return refuse(501, "board_item_writes_pending_design",
			"The old cards are shown read-only (design-decisions D32); the new board's items are written through /v1/work.")
	}
	return nil
}

// SettingsRow is the board-level facts with the epoch that revokes
// presentation work in flight.
type SettingsRow struct {
	Revision          int64
	Enabled           bool
	NarrativeConsent  *string
	PresentationEpoch int
	UpdatedAt         float64
}

// Transition is what a valid command does to the board-level facts, under
// compare-and-set on the revision the caller last read. The answer's
// revision is the next one.
func Transition(cur SettingsRow, c Command, now time.Time) (SettingsRow, error) {
	if *c.ExpectedRevision != cur.Revision {
		return SettingsRow{}, refuse(409, "revision_conflict",
			"The board has moved since you read it.")
	}
	next := cur
	switch c.Operation {
	case "set_enabled":
		if c.Enabled == nil {
			return SettingsRow{}, refuse(400, "invalid_command", "set_enabled needs enabled.")
		}
		next.Enabled = *c.Enabled
		if !next.Enabled {
			// Turning the board off revokes any in-flight presentation work.
			next.PresentationEpoch++
		}
	case "set_ai_consent":
		if c.Enabled == nil || c.Provider == nil || c.Policy == nil ||
			(*c.Provider != "codex" && *c.Provider != "claude") || *c.Policy != "board-reading-v1" {
			return SettingsRow{}, refuse(400, "invalid_command",
				"set_ai_consent needs enabled, a provider of codex or claude, and policy board-reading-v1.")
		}
		if *c.Enabled {
			provider := *c.Provider
			next.NarrativeConsent = &provider
		} else {
			next.NarrativeConsent = nil
		}
		// Revocation and grant both advance the epoch, so a result generated
		// under an older consent cannot commit after the answer changed.
		next.PresentationEpoch++
	}
	next.Revision = cur.Revision + 1
	next.UpdatedAt = float64(now.UnixNano()) / 1e9
	return next, nil
}

// Document reads the old document for the one import: its facts, and false
// when there is no document (nothing was ever written, and there is nothing to
// carry over). A document that cannot be read is a refusal, never a fresh
// board: carrying over "enabled" from a file nobody could read would reset a
// person's setting without telling them.
func (s *Settings) Document() (SettingsRow, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(s.path); errors.Is(err, os.ErrNotExist) {
		return SettingsRow{}, false, nil
	}
	f, err := s.load()
	if err != nil {
		return SettingsRow{}, false, err
	}
	return SettingsRow{Revision: f.Revision, Enabled: f.Enabled, NarrativeConsent: f.NarrativeConsent,
		PresentationEpoch: f.PresentationEpoch, UpdatedAt: f.UpdatedAt}, true, nil
}

// Path is where the old document is, for the record of where an import
// came from.
func (s *Settings) Path() string { return s.path }

// CommandDigest identifies a command by what it asks for, for its receipt.
func CommandDigest(raw []byte) string { return commandDigest(raw) }

// commandDigest identifies a command by what it asks for. Keys are sorted so
// the same object sent twice with its fields in another order is the same
// command, and a different value under a reused requestId is not.
func commandDigest(raw []byte) string {
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:])
	}
	keys := make([]string, 0, len(object))
	for k := range object {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		value, _ := json.Marshal(object[k])
		b.WriteString(k)
		b.WriteByte(0)
		b.Write(value)
		b.WriteByte(0)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}
