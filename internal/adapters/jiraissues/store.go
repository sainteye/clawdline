package jiraissues

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// The credential is kept exactly as githubissues keeps its token: an
// owner-only file (0700 directory, 0600 file, set on every write), written
// through a synced temporary file and a rename, never read through a
// symlink, and unreadable is a failure rather than "not connected". One
// machine connects one Jira site.

const credentialFile = "credential-v1"

// MaxCredentialFileBytes bounds the stored credential.
const MaxCredentialFileBytes = 16 << 10

var ErrNotConnected = errors.New("Jira is not connected on this machine")

type Store struct{ Dir string }

func (s Store) path() string { return filepath.Join(s.Dir, credentialFile) }

func (s Store) Load() (Credential, error) {
	info, err := os.Lstat(s.path())
	if errors.Is(err, fs.ErrNotExist) {
		return Credential{}, ErrNotConnected
	}
	if err != nil {
		return Credential{}, fmt.Errorf("the Jira credential could not be read: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > MaxCredentialFileBytes {
		return Credential{}, errors.New("the Jira credential file is not what Clawdline wrote")
	}
	f, err := os.Open(s.path())
	if err != nil {
		return Credential{}, fmt.Errorf("the Jira credential could not be read: %w", err)
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, MaxCredentialFileBytes+1))
	if err != nil || len(raw) > MaxCredentialFileBytes {
		return Credential{}, errors.New("the Jira credential could not be read")
	}
	var c Credential
	if json.Unmarshal(raw, &c) != nil || c.Site == "" || c.Email == "" || c.Token == "" {
		return Credential{}, errors.New("the Jira credential file is damaged")
	}
	return c, nil
}

func (s Store) Save(c Credential) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(s.Dir, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if len(raw) > MaxCredentialFileBytes {
		return errors.New("the Jira credential is larger than expected")
	}
	tmp, err := os.CreateTemp(s.Dir, ".credential-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.path())
}

func (s Store) Delete() error {
	err := os.Remove(s.path())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
