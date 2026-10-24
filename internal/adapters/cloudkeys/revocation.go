package cloudkeys

import (
	"crypto/rand"
	"encoding/hex"
	"errors"

	"github.com/sainteye/clawdline/internal/domain/cloud"
)

const revocationFile = "content-key-revocation-v1"

// RevocationIntent is a fixed-size single-operation journal, not an audit log.
// A new intent may replace only a completed one. Files has one daemon owner.
type RevocationIntent struct {
	Viewer        string `json:"viewer"`
	Operation     string `json:"operation"`
	ExpectedEpoch uint64 `json:"expected_epoch"`
	Complete      bool   `json:"complete"`
}

func (f *Files) PendingRevocation() (RevocationIntent, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.revocationLocked()
}

func (f *Files) revocationLocked() (RevocationIntent, bool, error) {
	var r RevocationIntent
	found, err := f.readJSON(revocationFile, &r)
	if err != nil || !found {
		return r, false, err
	}
	if r.Viewer == "" || len(r.Operation) != 64 || r.ExpectedEpoch == 0 {
		return r, false, cloud.ErrContentKeyState
	}
	if _, err := hex.DecodeString(r.Operation); err != nil {
		return r, false, cloud.ErrContentKeyState
	}
	return r, !r.Complete, nil
}

func (f *Files) BeginRevocation(viewer string, epoch uint64) (RevocationIntent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, pending, err := f.revocationLocked()
	if err != nil {
		return r, err
	}
	if pending {
		if r.Viewer != viewer {
			return r, cloud.ErrContentKeyPending
		}
		return r, nil
	}
	if viewer == "" || epoch == 0 {
		return r, cloud.ErrContentKeyState
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return r, err
	}
	r = RevocationIntent{Viewer: viewer, Operation: hex.EncodeToString(nonce), ExpectedEpoch: epoch}
	return r, f.writeJSON(revocationFile, r)
}

func (f *Files) CompleteRevocation(r RevocationIntent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	current, pending, err := f.revocationLocked()
	if err != nil {
		return err
	}
	if !pending || current != r {
		return errors.New("revocation journal conflict")
	}
	r.Complete = true
	return f.writeJSON(revocationFile, r)
}
