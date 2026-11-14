package browserlaunch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/sainteye/clawdline/extension"
)

// FolderMarker is the file only the stable folder carries. The extension
// reads it and tells the daemon it was loaded from the stable folder, so the
// console knows whether Chrome will pick up new files at its next start or
// still needs the one-time move (docs/browser.md, "Updating the extension").
const FolderMarker = "clawdline-folder.json"

// StableExtensionDir is the one folder Chrome loads the extension from. Its
// path never changes, so neither does the unpacked extension's ID, and a new
// release only replaces its contents: Chrome reads them when it next starts.
func StableExtensionDir(dir string) string { return filepath.Join(dir, "chrome-extension") }

// previousDir keeps the release before the last replacement, for rollback.
func previousDir(dir string) string { return filepath.Join(dir, ".chrome-extension-previous") }

type folderMarker struct {
	Stable   bool   `json:"stable"`
	Revision string `json:"revision"`
	Version  string `json:"version"`
}

// EmbeddedVersion is the manifest version of the extension in this build.
var EmbeddedVersion = sync.OnceValue(func() string {
	data, err := extension.Chrome.ReadFile("chrome/manifest.json")
	if err != nil {
		return ""
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &manifest) != nil {
		return ""
	}
	return manifest.Version
})

var stableMu sync.Mutex

// WriteStableExtension makes the stable folder hold this build's extension.
// A folder that already holds it is left alone. Otherwise the complete new
// payload is written beside it and swapped in with renames, the previous
// release kept once, so Chrome never reads a half-written folder. It reports
// whether the contents changed.
func WriteStableExtension(dir string) (bool, error) {
	if dir == "" {
		return false, fmt.Errorf("the Clawdline directory is unavailable")
	}
	stableMu.Lock()
	defer stableMu.Unlock()
	target := StableExtensionDir(dir)
	if stableCurrent(target) {
		return false, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	staging, err := os.MkdirTemp(dir, ".chrome-stable-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(staging)
	payload := filepath.Join(staging, "payload")
	if err := WriteExtension(payload); err != nil {
		return false, err
	}
	marker, _ := json.Marshal(folderMarker{Stable: true, Revision: extensionRevision(), Version: EmbeddedVersion()})
	if err := os.WriteFile(filepath.Join(payload, FolderMarker), append(marker, '\n'), 0o644); err != nil {
		return false, err
	}
	if _, err := os.Lstat(target); err == nil {
		if err := os.RemoveAll(previousDir(dir)); err != nil {
			return false, err
		}
		if err := os.Rename(target, previousDir(dir)); err != nil {
			return false, err
		}
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if err := os.Rename(payload, target); err != nil {
		// Put the previous release back rather than leave no folder at all.
		_ = os.Rename(previousDir(dir), target)
		return false, err
	}
	return true, nil
}

// stableCurrent reports whether the stable folder already holds this build's
// files with a matching marker.
func stableCurrent(target string) bool {
	data, err := os.ReadFile(filepath.Join(target, FolderMarker))
	if err != nil {
		return false
	}
	var marker folderMarker
	if json.Unmarshal(data, &marker) != nil || !marker.Stable || marker.Revision != extensionRevision() {
		return false
	}
	return verifyPreparedExtension(target) == nil
}

// RunningPID is the process holding Clawdline's Chrome profile, read from the
// profile's SingletonLock link ("host-PID"), or 0 when none does.
func RunningPID(dir string) int {
	target, err := os.Readlink(filepath.Join(ProfileDir(dir), "SingletonLock"))
	if err != nil {
		return 0
	}
	i := len(target) - 1
	for i >= 0 && target[i] >= '0' && target[i] <= '9' {
		i--
	}
	if i < 0 || target[i] != '-' || i == len(target)-1 {
		return 0
	}
	pid := 0
	for _, c := range target[i+1:] {
		pid = pid*10 + int(c-'0')
		if pid > 1<<30 {
			return 0
		}
	}
	return pid
}
