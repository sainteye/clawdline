package projectassistantacp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

var extensionFilesMu sync.Mutex

// A durable reservation precedes every owned extension process. Unknown cleanup
// keeps the reservation, including across restart; no PID is killed on reopen.
func reserveExtension(state string, fence pa.Fence) (string, error) {
	extensionFilesMu.Lock()
	defer extensionFilesMu.Unlock()
	resolved, err := filepath.EvalSymlinks(state)
	if err != nil || resolved == "" {
		return "", refusal(pa.ScopeDenied, "extension_state")
	}
	root := filepath.Join(resolved, "project-assistants", "extension-executions")
	if err = privateDirectory(resolved, root); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", refusal(pa.OutcomeUnknown, "extension_reservations")
	}
	if len(entries) >= pa.MaxConnectorProcesses {
		return "", refusal(pa.CapacityFull, "extension_processes")
	}
	dir, err := os.MkdirTemp(root, "execution-")
	if err != nil {
		return "", refusal(pa.OutcomeUnknown, "extension_reservation")
	}
	raw, _ := json.Marshal(fence)
	file, err := os.OpenFile(filepath.Join(dir, "owner.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", refusal(pa.OutcomeUnknown, "extension_reservation")
	}
	if _, err = file.Write(raw); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return "", refusal(pa.OutcomeUnknown, "extension_reservation")
	}
	// The record and directory entries must survive a source restart before
	// any native effect can begin. An uncertain sync preserves the reservation.
	for _, path := range []string{dir, root, filepath.Dir(root), resolved} {
		directory, err := os.Open(path)
		if err != nil {
			return "", refusal(pa.OutcomeUnknown, "extension_reservation")
		}
		err = directory.Sync()
		closeErr := directory.Close()
		if err != nil || closeErr != nil {
			return "", refusal(pa.OutcomeUnknown, "extension_reservation")
		}
	}
	return dir, nil
}

func prepareExtensionFiles(q *app.OpenRequest) (string, error) {
	if len(q.Extensions.Folders) == 0 {
		return "", nil
	}
	return reserveExtension(q.Extensions.State, q.Binding.Fence)
}
