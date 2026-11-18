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
}

func (s Spec) Validate() error {
	if err := pa.ValidateScope(s.Project); err != nil {
		return err
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
