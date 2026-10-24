package antigravitysession

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

// identityPath is stable across restarts and independent of native ID spelling.
// Records live beside owned history and remain for its lifetime, including hidden
// conversations. They are addresses, not authority. No foreign history is removed.
func (h *Host) identityPath(ownedID string) string {
	if h.opts.StateDir == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(ownedID))
	return filepath.Join(h.opts.StateDir, "identity-"+hex.EncodeToString(digest[:])+".json")
}

// publishIdentity replaces complete private JSON atomically. Retrying publishes
// the same identity; a failed write never exposes partial JSON to native tools.
func publishIdentity(path, nativeID string) error {
	if path == "" {
		return nil
	} // In-memory SDK hosts retain their no-file behavior.
	if nativeID == "" {
		return refusal("identity_unavailable")
	}
	raw, err := json.Marshal(struct {
		Assistant      string `json:"assistant"`
		ConversationID string `json:"conversation_id"`
	}{"antigravity", nativeID})
	if err != nil {
		return refusal("identity_unavailable")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".identity-*")
	if err != nil {
		return refusal("identity_unavailable")
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return refusal("identity_unavailable")
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return refusal("identity_unavailable")
	}
	return nil
}
