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

// Settings is this daemon's own board document: the board-level facts it may
// change, and the receipts that make a retry of that change free.
//
// It is deliberately only board-level. Work items are not written here yet,
// because where an item lives is the open question in docs/board-design.md
// (C1, C2) and writing one now would lock that answer into the code before the
// person who owns the decision has read it. The two commands this accepts are
// exactly the two the settings page issues.
//
// State and receipts are one document written in one atomic rename. That is the
// Swift app's reason for keeping receipts in the aggregate, and it is kept here
// for the same reason: a receipt and the change it acknowledges that could
// land separately would make the idempotency guard lie in one direction or the
// other.
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

// Read returns the current board-level facts.
func (s *Settings) Read() (SettingsState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return SettingsState{}, err
	}
	return SettingsState{Revision: f.Revision, Enabled: f.Enabled,
		NarrativeConsent: f.NarrativeConsent, UpdatedAt: f.UpdatedAt}, nil
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

// Apply performs one command under compare-and-swap on the revision the caller
// last read, with `(actor, requestId)` replay.
func (s *Settings) Apply(actor string, c Command, raw []byte) (Outcome, error) {
	if c.Operation == "" || len(c.Operation) > 64 || c.RequestID == "" ||
		len(c.RequestID) > 200 || c.ExpectedRevision == nil || *c.ExpectedRevision < 0 ||
		actor == "" || len(actor) > 300 {
		return Outcome{}, refuse(400, "invalid_command",
			"A board command needs an operation, a requestId and an expectedRevision.")
	}
	if !boardOperations[c.Operation] {
		return Outcome{}, refuse(400, "unknown_operation", "That is not a board operation.")
	}
	if c.Operation != "set_enabled" && c.Operation != "set_ai_consent" {
		return Outcome{}, refuse(501, "board_item_writes_pending_design",
			"This daemon does not write board items yet: where an item is stored is an open design decision (docs/board-design.md C1, C2). Cards from the Swift app are shown read-only.")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return Outcome{}, err
	}

	digest := commandDigest(raw)
	for _, r := range f.Receipts {
		if r.Actor == actor && r.RequestID == c.RequestID {
			if r.Digest == digest {
				// The same command twice changes nothing twice.
				return Outcome{Revision: f.Revision, Replayed: true}, nil
			}
			return Outcome{}, refuse(409, "request_id_conflict",
				"That requestId was already used for a different command.")
		}
	}
	if *c.ExpectedRevision != f.Revision {
		return Outcome{}, refuse(409, "revision_conflict",
			"The board has moved since you read it.")
	}

	switch c.Operation {
	case "set_enabled":
		if c.Enabled == nil {
			return Outcome{}, refuse(400, "invalid_command", "set_enabled needs enabled.")
		}
		f.Enabled = *c.Enabled
		if !f.Enabled {
			// Turning the board off revokes any in-flight presentation work.
			f.PresentationEpoch++
		}
	case "set_ai_consent":
		if c.Enabled == nil || c.Provider == nil || c.Policy == nil ||
			(*c.Provider != "codex" && *c.Provider != "claude") || *c.Policy != "board-reading-v1" {
			return Outcome{}, refuse(400, "invalid_command",
				"set_ai_consent needs enabled, a provider of codex or claude, and policy board-reading-v1.")
		}
		if *c.Enabled {
			provider := *c.Provider
			f.NarrativeConsent = &provider
		} else {
			f.NarrativeConsent = nil
		}
		// Revocation and grant both advance the epoch, so a result generated
		// under an older consent cannot commit after the answer changed.
		f.PresentationEpoch++
	}

	f.Revision++
	f.UpdatedAt = float64(time.Now().UnixNano()) / 1e9
	f.Receipts = append(f.Receipts, StoredReceipt{Actor: actor, Digest: digest, ItemID: "",
		RequestID: c.RequestID, Revision: f.Revision, Status: 200})
	if over := len(f.Receipts) - MaximumReceipts; over > 0 {
		f.Receipts = f.Receipts[over:]
		f.ReceiptEvictions += over
	}
	f.SchemaVersion = StorageSchemaVersion
	if err := s.persist(f); err != nil {
		return Outcome{}, refuse(503, "board_persistence_failed", err.Error())
	}
	return Outcome{Revision: f.Revision}, nil
}

// persist writes the whole document to a temporary file beside it and renames
// it into place, 0600, then syncs — the Swift app's `persist` in shape.
func (s *Settings) persist(f settingsFile) error {
	body, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".project-board-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

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
