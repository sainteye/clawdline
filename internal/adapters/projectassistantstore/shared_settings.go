package projectassistantstore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// SharedSettings stores the machine's dispatch rules and shared Bot memory
// as one versioned document. Writes replace the whole document and must name
// the version they read, the same pattern as a Bot PATCH.
type SharedSettings struct {
	Dir string
}

var sharedSettingsMu sync.Mutex

func (s SharedSettings) path() string { return filepath.Join(s.Dir, "shared-settings.json") }

// Load returns version 0 with empty lists when nothing was saved yet.
func (s SharedSettings) Load() (pa.SharedSettings, error) {
	sharedSettingsMu.Lock()
	defer sharedSettingsMu.Unlock()
	return s.load()
}

func (s SharedSettings) load() (pa.SharedSettings, error) {
	out := pa.SharedSettings{Rules: []pa.DispatchRule{}, Memory: []pa.SharedMemoryEntry{}}
	raw, err := os.ReadFile(s.path())
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	if out.Rules == nil {
		out.Rules = []pa.DispatchRule{}
	}
	if out.Memory == nil {
		out.Memory = []pa.SharedMemoryEntry{}
	}
	return out, nil
}

// Save writes next when expected is the stored version and returns the new
// document with its incremented version.
func (s SharedSettings) Save(expected int64, next pa.SharedSettings) (pa.SharedSettings, error) {
	if err := pa.ValidateSharedSettings(next); err != nil {
		return pa.SharedSettings{}, err
	}
	sharedSettingsMu.Lock()
	defer sharedSettingsMu.Unlock()
	current, err := s.load()
	if err != nil {
		return pa.SharedSettings{}, err
	}
	if current.Version != expected {
		return current, &pa.Failure{Code: pa.StaleVersion, Field: "shared_settings"}
	}
	next.Version = expected + 1
	raw, err := json.Marshal(next)
	if err != nil {
		return pa.SharedSettings{}, err
	}
	if err = os.MkdirAll(s.Dir, 0o700); err != nil {
		return pa.SharedSettings{}, err
	}
	tmp, err := os.CreateTemp(s.Dir, ".shared-settings-*")
	if err != nil {
		return pa.SharedSettings{}, err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(raw); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), s.path())
	}
	if err != nil {
		return pa.SharedSettings{}, err
	}
	return next, nil
}
