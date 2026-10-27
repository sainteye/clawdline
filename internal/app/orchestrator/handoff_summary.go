package orchestrator

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
)

// writeHandoffSummary publishes a complete file without replacing an existing
// package. Retrying after a failed admission may reuse the identical summary.
func (b *Broker) writeHandoffSummary(id string, data []byte) error {
	unwritable := func(err error) error {
		return refuseWith(http.StatusInternalServerError, "handoff_package_unwritable",
			"The handoff package could not be written; nothing was opened.", map[string]any{"cause": err.Error()})
	}
	if err := os.MkdirAll(b.HandoffRoot(), 0o700); err != nil {
		return unwritable(err)
	}
	root, err := os.OpenRoot(b.HandoffRoot())
	if err != nil {
		return unwritable(err)
	}
	defer root.Close()
	if err := root.Mkdir(id, 0o700); err != nil && !os.IsExist(err) {
		return unwritable(err)
	}
	// Refuse a redirected package directory as well as a redirected summary.
	info, err := root.Lstat(id)
	if err != nil {
		return unwritable(err)
	}
	if !info.IsDir() {
		return unwritable(errors.New("handoff directory is not a directory"))
	}
	dir, err := root.OpenRoot(id)
	if err != nil {
		return unwritable(err)
	}
	defer dir.Close()
	name := ".summary-" + NewUUID()
	tmp, err := dir.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return unwritable(err)
	}
	defer dir.Remove(name)
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr != nil {
		return unwritable(writeErr)
	}
	if closeErr != nil {
		return unwritable(closeErr)
	}
	if err := dir.Link(name, "handoff.md"); err == nil {
		return nil
	} else if !os.IsExist(err) {
		return unwritable(err)
	}
	info, err = dir.Lstat("handoff.md")
	if err != nil {
		return unwritable(err)
	}
	if info.Mode().IsRegular() {
		f, err := dir.Open("handoff.md")
		if err != nil {
			return unwritable(err)
		}
		existing, readErr := io.ReadAll(io.LimitReader(f, MilestoneSummaryLimit+1))
		closeErr := f.Close()
		if readErr != nil {
			return unwritable(readErr)
		}
		if closeErr != nil {
			return unwritable(closeErr)
		}
		if bytes.Equal(existing, data) {
			return nil
		}
	}
	return refuse(http.StatusConflict, "handoff_package_conflict",
		"The handoff package already contains a different summary; nothing was opened.")
}
