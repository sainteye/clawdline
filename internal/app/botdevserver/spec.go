package botdevserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Spec is source-resolved data, not an approval or a filesystem sandbox.
// The authority callback must resolve the current Project and command grant.
// A native tool cannot supply Key/Project or the process environment itself.
type Spec struct {
	Key       pa.BotKey       `json:"key"`
	Project   pa.ProjectScope `json:"project"`
	Name      string          `json:"name"`
	Command   string          `json:"command"`
	Directory string          `json:"directory"`
	// The Forget boundary is independent of ordinary memory edits and turns.
	ForgottenThrough int64 `json:"forgotten_through,omitempty"`
}

func (s Spec) Validate() error {
	if err := pa.ValidateScope(s.Project); err != nil {
		return err
	}
	if s.ForgottenThrough < 0 {
		return failure(pa.Invalid, "server_provenance")
	}
	if s.Key.BotID == "" || s.Key.MachineID != s.Project.MachineID {
		return failure(pa.ScopeDenied, "server_spec")
	}
	for _, text := range []string{s.Key.MachineID, s.Key.BotID, s.Project.ProjectID, s.Project.CanonicalID, s.Name, s.Command, s.Directory} {
		if !utf8.ValidString(text) || strings.ContainsRune(text, 0) || strings.TrimSpace(text) == "" {
			return failure(pa.Invalid, "server_spec")
		}
	}
	if !filepath.IsAbs(s.Directory) || filepath.Clean(s.Directory) != s.Directory {
		return failure(pa.Invalid, "server_directory")
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if len(b) > capacity.MaximumBotServerSpecBytes {
		return failure(pa.CapacityFull, "server_spec_bytes")
	}
	return nil
}

// The label does not change command authority or create a duplicate process.
// Complete Project identity, command bytes and resolved directory do.
func (s Spec) Fingerprint() (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	s.Name = ""
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(b)
	return hex.EncodeToString(digest[:]), nil
}

// CheckMemory prevents old command/output from becoming input to a new context.
// Human cleanup uses its separate, authenticated control path.
func (s Spec) CheckMemory(m pa.MemoryRevision) error {
	if m.Key != s.Key || m.Project != s.Project || m.Revision < 1 || m.ForgottenThrough < 0 || m.ForgottenThrough > m.Revision {
		return failure(pa.ScopeDenied, "server_provenance")
	}
	if m.ForgottenThrough != s.ForgottenThrough {
		return failure(pa.ContextStale, "server_provenance")
	}
	return nil
}
