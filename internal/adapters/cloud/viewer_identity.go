package cloud

// ViewerIdentity is deliberately separate from the machine identity. A machine
// credential only authorizes that machine; it never makes the local CLI a viewer.
import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const ViewerIdentityFile = "viewer-identity-v1.json"

type ViewerIdentity struct {
	AccountID        string `json:"account_id"`
	DeviceID         string `json:"device_id"`
	ViewerCredential string `json:"viewer_credential"`
	APIBase          string `json:"api_base"`
}

func (v ViewerIdentity) Valid() bool {
	return v.AccountID != "" && v.DeviceID != "" && v.ViewerCredential != "" && v.APIBase != ""
}

type ViewerIdentityStore struct{ dir string }

func NewViewerIdentityStore(dir string) *ViewerIdentityStore { return &ViewerIdentityStore{dir: dir} }
func (s *ViewerIdentityStore) Path() string                  { return filepath.Join(s.dir, ViewerIdentityFile) }

func (s *ViewerIdentityStore) Load() (ViewerIdentity, bool, error) {
	info, err := os.Lstat(s.Path())
	if errors.Is(err, os.ErrNotExist) {
		return ViewerIdentity{}, false, nil
	}
	if err != nil {
		return ViewerIdentity{}, false, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return ViewerIdentity{}, false, errors.New("the viewer identity is not an owner-only regular file")
	}
	data, err := os.ReadFile(s.Path())
	if err != nil {
		return ViewerIdentity{}, false, err
	}
	var v ViewerIdentity
	if err := json.Unmarshal(data, &v); err != nil || !v.Valid() {
		return ViewerIdentity{}, false, errors.New("the viewer identity is unusable")
	}
	return v, true, nil
}

func (s *ViewerIdentityStore) Save(v ViewerIdentity) error {
	if !v.Valid() {
		return errors.New("refusing to save an incomplete viewer identity")
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(s.dir, 0o700); err != nil {
		return err
	}
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode viewer identity: %w", err)
	}
	return writeFileAtomically(s.dir, s.Path(), append(body, '\n'))
}
