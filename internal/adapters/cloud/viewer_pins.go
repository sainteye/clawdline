package cloud

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	domain "github.com/sainteye/clawdline/internal/domain/cloud"
)

const ViewerPinsFile = "viewer-machine-pins-v1.json"

// ViewerMachinePin keeps the authenticated handover for exactly one machine.
// The content secret is owner-only and never appears in a URL or terminal output.
type ViewerMachinePin struct {
	MachineID          string `json:"machine_id"`
	MachineSigningKey  string `json:"machine_signing_key"`
	MachineFingerprint string `json:"machine_fingerprint"`
	KeyID              string `json:"key_id"`
	MasterSecret       string `json:"master_secret"`
}

type viewerPinsRecord struct {
	Version   int                         `json:"version"`
	AccountID string                      `json:"account_id"`
	DeviceID  string                      `json:"device_id"`
	Machines  map[string]ViewerMachinePin `json:"machines"`
}

type ViewerPinStore struct{ dir string }

func NewViewerPinStore(dir string) *ViewerPinStore { return &ViewerPinStore{dir: dir} }
func (s *ViewerPinStore) Path() string             { return filepath.Join(s.dir, ViewerPinsFile) }

func (s *ViewerPinStore) Load(identity ViewerIdentity) (map[string]ViewerMachinePin, error) {
	info, err := os.Lstat(s.Path())
	if errors.Is(err, os.ErrNotExist) {
		return map[string]ViewerMachinePin{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("viewer machine pins are not an owner-only regular file")
	}
	data, err := os.ReadFile(s.Path())
	if err != nil {
		return nil, err
	}
	var record viewerPinsRecord
	if json.Unmarshal(data, &record) != nil || record.Version != 1 || record.AccountID != identity.AccountID || record.DeviceID != identity.DeviceID || record.Machines == nil {
		return nil, errors.New("viewer machine pins do not belong to this viewer identity")
	}
	for id, pin := range record.Machines {
		if id != pin.MachineID || !validViewerPin(pin) {
			return nil, errors.New("viewer machine pins are invalid")
		}
	}
	return record.Machines, nil
}

func validViewerPin(pin ViewerMachinePin) bool {
	key, err := base64.StdEncoding.DecodeString(pin.MachineSigningKey)
	if err != nil || len(key) != 32 || domain.Fingerprint(key) != pin.MachineFingerprint {
		return false
	}
	secret, err := base64.StdEncoding.DecodeString(pin.MasterSecret)
	return err == nil && len(secret) == domain.ContentKeyBytes && pin.MachineID != "" && pin.KeyID != ""
}

// Save refuses a changed signing key under an existing machine ID. The person
// must remove that local pin explicitly before pairing a replacement identity.
func (s *ViewerPinStore) Save(identity ViewerIdentity, pin ViewerMachinePin) error {
	if !identity.Valid() || !validViewerPin(pin) {
		return errors.New("refusing an invalid viewer machine pin")
	}
	machines, err := s.Load(identity)
	if err != nil {
		return err
	}
	if previous, ok := machines[pin.MachineID]; ok && previous.MachineSigningKey != pin.MachineSigningKey {
		return errors.New("machine signing key changed; explicit re-pairing is required")
	}
	machines[pin.MachineID] = pin
	record := viewerPinsRecord{Version: 1, AccountID: identity.AccountID, DeviceID: identity.DeviceID, Machines: machines}
	body, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(s.dir, 0o700); err != nil {
		return err
	}
	return writeFileAtomically(s.dir, s.Path(), append(body, '\n'))
}
