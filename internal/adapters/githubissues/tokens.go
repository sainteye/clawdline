package githubissues

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// The token is kept the way internal/adapters/cloudkeys keeps this daemon's
// own secrets, for the same reason: an owner-only file is the one store
// macOS, Linux and Windows all have. The directory is 0700 and the file 0600,
// set on every write; a write is a synced temporary file renamed over the old
// one; a symlink is refused; and a file that exists and cannot be read is a
// failure, never "not connected". No route answers the token itself.

const tokenFile = "token-v1"

// MaxTokenFileBytes bounds the stored token file. One token pair with its
// account name is well under a kilobyte.
const MaxTokenFileBytes = 16 << 10

// ErrNoToken is a machine that has not connected GitHub.
var ErrNoToken = errors.New("GitHub is not connected on this machine")

type TokenStore struct{ Dir string }

func (s TokenStore) path() string { return filepath.Join(s.Dir, tokenFile) }

func (s TokenStore) Load() (Token, error) {
	info, err := os.Lstat(s.path())
	if errors.Is(err, fs.ErrNotExist) {
		return Token{}, ErrNoToken
	}
	if err != nil {
		return Token{}, fmt.Errorf("the GitHub token file could not be read: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Token{}, errors.New("the GitHub token file is not a regular file")
	}
	if info.Size() > MaxTokenFileBytes {
		return Token{}, errors.New("the GitHub token file is larger than expected")
	}
	f, err := os.Open(s.path())
	if err != nil {
		return Token{}, fmt.Errorf("the GitHub token file could not be read: %w", err)
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, MaxTokenFileBytes+1))
	if err != nil || len(raw) > MaxTokenFileBytes {
		return Token{}, errors.New("the GitHub token file could not be read")
	}
	var t Token
	if json.Unmarshal(raw, &t) != nil || t.AccessToken == "" {
		return Token{}, errors.New("the GitHub token file is damaged")
	}
	return t, nil
}

func (s TokenStore) Save(t Token) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(s.Dir, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(t)
	if err != nil {
		return err
	}
	if len(raw) > MaxTokenFileBytes {
		return errors.New("the GitHub token is larger than expected")
	}
	tmp, err := os.CreateTemp(s.Dir, ".token-*")
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

// Delete forgets the token. A token that was never saved is already gone.
func (s TokenStore) Delete() error {
	err := os.Remove(s.path())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
