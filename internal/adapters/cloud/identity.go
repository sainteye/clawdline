package cloud

// Where this machine's cloud identity lives on disk.
//
// The keys are `internal/adapters/cloudkeys`'s job. This file keeps the other
// half — which account and machine the keys belong to, and the machine
// credential that buys device tokens — beside them, under the same manners:
// 0700 directory, 0600 file, written to a temporary file in the same directory
// and renamed over, never through a symlink, and never under
// `~/.config/clawdline`, which is the Swift app's.
//
// The machine credential is a bearer secret. It is not a key and it is not
// rotated by this daemon; losing it means asking the person to approve the
// machine again, which is exactly what should happen.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// IdentityFile is the file inside the cloud key directory. The name says what
// version of the record it is so that a future shape can sit beside it rather
// than be guessed at.
const IdentityFile = "machine-identity-v1.json"

// Identity is who this machine is on the wire.
type Identity struct {
	// AccountID and MachineID are the control plane's ids. MachineID is also
	// this machine's `sender` on every envelope and the `mid` in its token.
	AccountID string `json:"account_id"`
	MachineID string `json:"machine_id"`
	// MachineCredential buys device tokens. It is the one long-lived secret
	// here and it never leaves this file except as a bearer header.
	MachineCredential string `json:"machine_credential"`
	// APIBase is the control plane this identity belongs to. It is stored
	// because an identity minted against a local api is not usable against
	// the production one, and finding that out as `unauthorized` at 3am is
	// worse than finding it out as a mismatch here.
	APIBase string `json:"api_base"`
	// Name is what the person will see in their machine list.
	Name string `json:"name,omitempty"`
}

// Valid reports whether this record can be used to connect.
func (i Identity) Valid() bool {
	return i.AccountID != "" && i.MachineID != "" && i.MachineCredential != ""
}

// IdentityStore reads and writes the record.
type IdentityStore struct {
	dir string
	mu  sync.Mutex
}

// NewIdentityStore keeps the record in dir, which is normally the cloudkeys
// directory so that everything cloud-shaped is in one place to empty.
func NewIdentityStore(dir string) *IdentityStore { return &IdentityStore{dir: dir} }

// Path is where the record is, whether or not it exists.
func (s *IdentityStore) Path() string { return filepath.Join(s.dir, IdentityFile) }

// Load answers the record, and whether there is one.
//
// A file that exists and cannot be read is an **error**, never an absence.
// This is the same rule the key store keeps and for the same reason: treating
// an unreadable identity as "not registered yet" would quietly register a
// second machine, and the person would find out when their first one stopped
// receiving anything.
func (s *IdentityStore) Load() (Identity, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.Path())
	if errors.Is(err, os.ErrNotExist) {
		return Identity{}, false, nil
	}
	if err != nil {
		return Identity{}, false, fmt.Errorf("the cloud identity file cannot be read: %w", err)
	}
	var identity Identity
	if err := json.Unmarshal(data, &identity); err != nil {
		return Identity{}, false, fmt.Errorf("the cloud identity file is not readable JSON: %w", err)
	}
	if !identity.Valid() {
		return Identity{}, false, errors.New("the cloud identity file names no account, machine or credential")
	}
	return identity, true, nil
}

// Save writes the record.
func (s *IdentityStore) Save(identity Identity) error {
	if !identity.Valid() {
		return errors.New("refusing to save an identity with no account, machine or credential")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(identity, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return writeFileAtomically(s.dir, s.Path(), body)
}

// Forget removes the record. Signing out must not leave a credential behind.
func (s *IdentityStore) Forget() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.Path()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// writeFileAtomically writes body to path through a temporary file in dir.
// A reader sees the old contents or the new ones, never half of either.
func writeFileAtomically(dir, path string, body []byte) (err error) {
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(name)
		}
	}()
	// The mode is set rather than trusted from creation: a umask that allows
	// group read would otherwise decide how private a secret is.
	if err = tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err = tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	if d, derr := os.Open(dir); derr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
