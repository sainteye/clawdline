// Package cloudkeys keeps this daemon's two long-lived cloud secrets on disk:
// the device signing key and the account master secret.
//
// **Files, not a keychain, and that is this version's deliberate choice.** The
// Swift app calls the macOS Keychain from 63 places in CloudKeys.swift; this
// daemon runs on macOS, Windows and Linux, and an owner-only file is the one
// store all three have. The seam for the platform keystores is
// cloud.KeyStore — a Keychain, a Credential Manager and a Secret Service
// implementation each satisfy it, and a Linux without a desktop session keeps
// this file store as its honest fallback, said out loud on the settings page
// rather than pretended away.
//
// The manners are internal/adapters/devices's, because they are the ones this
// repository already argued about:
//
//   - The directory is 0700 and every file 0600, set on every write rather
//     than trusted from creation.
//   - A write goes to a temporary file in the same directory, is synced, and is
//     renamed over the old one. A reader sees the old secret or the new one.
//   - A file is opened as the file it is, never through a symlink.
//   - A file that exists and cannot be read is a refusal, never an absence.
//     This is the rule that matters most here: minting a key over an
//     unreadable one would orphan every device the user has paired, silently,
//     and the next thing they would see is that nothing decrypts any more.
//   - Never the Swift app's directory. This package does not open, read or
//     accept anything under ~/.config/clawdline, and it does not reuse the
//     Swift app's device identity: two daemons sharing one machine identity
//     would fight over the same sequence numbers.
package cloudkeys

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sainteye/clawdline/internal/domain/cloud"
)

const (
	// DirName is the subdirectory of this app's state directory that holds
	// cloud key material, so that a future keystore migration has one place to
	// empty.
	DirName = "cloud"
	// DeviceKeyFile holds the 32-byte Ed25519 seed, base64, one line.
	// The name is the Swift app's Keychain account, kept so the two stores can
	// be reasoned about together.
	DeviceKeyFile = "device-ed25519-v1"
	// MasterSecretFile holds the 32-byte account content key, base64, one line.
	MasterSecretFile = "account-master-secret-v1"

	// secretLimit is far past the 45 bytes a base64 32-byte key needs and far
	// short of what a mistake could make of the file.
	secretLimit = 4 << 10
)

// ErrForeignDir is the answer for a directory that is, or resolves into, the
// Swift app's.
var ErrForeignDir = errors.New("refusing the Swift app's directory")

// ErrNotRegular is the answer for a key file that is a symlink, a directory, or
// anything else but a plain file.
var ErrNotRegular = errors.New("not a plain file")

// ErrUnreadable is the answer for a key file that exists and is not one. It is
// deliberately distinct from "not there": a caller that confuses them mints a
// new identity over a real one.
var ErrUnreadable = errors.New("the key file cannot be read")

// Files is the on-disk key store. It satisfies cloud.KeyStore.
type Files struct {
	dir string
	mu  sync.Mutex
}

var _ cloud.KeyStore = (*Files)(nil)

// Open prepares the cloud key directory under root, refusing it when root is,
// or resolves into, any of foreign.
func Open(root string, foreign ...string) (*Files, error) {
	if root == "" {
		return nil, errors.New("no state directory")
	}
	for _, other := range foreign {
		if other != "" && (within(root, other) || within(resolved(root), resolved(other))) {
			return nil, fmt.Errorf("%w: %s", ErrForeignDir, root)
		}
	}
	dir := filepath.Join(root, DirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	// Tightened even when it already existed: a readable directory still lists
	// the names of the files in it.
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	f := &Files{dir: dir}
	for _, name := range []string{DeviceKeyFile, MasterSecretFile} {
		file, err := f.open(name, os.O_RDONLY, 0)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		err = file.Chmod(0o600)
		_ = file.Close()
		if err != nil {
			return nil, err
		}
	}
	return f, nil
}

// Dir is where the key files live.
func (f *Files) Dir() string { return f.dir }

// DeviceKey reads this machine's signing key. The middle result is whether
// there is one at all; an unreadable file is an error, never a false.
func (f *Files) DeviceKey() (cloud.DeviceKey, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, found, err := f.readSecret(DeviceKeyFile, cloud.DeviceKeySeedBytes)
	if err != nil || !found {
		return cloud.DeviceKey{}, false, err
	}
	key, err := cloud.DeviceKeyFromSeed(raw)
	if err != nil {
		return cloud.DeviceKey{}, false, fmt.Errorf("%w: %s: %v", ErrUnreadable, DeviceKeyFile, err)
	}
	return key, true, nil
}

// SaveDeviceKey writes the signing key, replacing whatever was there.
func (f *Files) SaveDeviceKey(key cloud.DeviceKey) error {
	if !key.Valid() {
		return fmt.Errorf("%w: refusing to store an empty device key", cloud.ErrSeedLength)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writeSecret(DeviceKeyFile, key.Seed())
}

// MasterSecret reads the account content key.
func (f *Files) MasterSecret() (cloud.ContentKey, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, found, err := f.readSecret(MasterSecretFile, cloud.ContentKeyBytes)
	if err != nil || !found {
		return cloud.ContentKey{}, false, err
	}
	secret, err := cloud.ContentKeyFromBytes(raw)
	if err != nil {
		return cloud.ContentKey{}, false, fmt.Errorf("%w: %s: %v", ErrUnreadable, MasterSecretFile, err)
	}
	return secret, true, nil
}

// SaveMasterSecret writes the account content key, replacing whatever was
// there. Pairing calls this with the secret it received from the paired Mac;
// replacing it with a freshly minted one would make everything the account
// already holds unreadable.
func (f *Files) SaveMasterSecret(secret cloud.ContentKey) error {
	if !secret.Valid() {
		return fmt.Errorf("%w: refusing to store an empty master secret", cloud.ErrKeyLength)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writeSecret(MasterSecretFile, secret.Bytes())
}

// Forget removes both secrets. Signing out must not leave key material behind.
// A file that was not there is not an error; one that could not be removed is.
func (f *Files) Forget() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var failures []string
	for _, name := range []string{DeviceKeyFile, MasterSecretFile} {
		if err := os.Remove(filepath.Join(f.dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, fmt.Sprintf("%s: %v", name, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("cloud key material remains: %s", strings.Join(failures, "; "))
	}
	syncDir(f.dir)
	return nil
}

// readSecret reads one base64 line and requires it to decode to exactly want
// bytes. Anything else — a truncated write, a file somebody edited, a stray
// newline in the middle — is ErrUnreadable rather than a short key.
func (f *Files) readSecret(name string, want int) ([]byte, bool, error) {
	file, err := f.open(name, os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, secretLimit+1))
	if err != nil {
		return nil, false, fmt.Errorf("%w: %s: %v", ErrUnreadable, name, err)
	}
	if len(data) > secretLimit {
		return nil, false, fmt.Errorf("%w: %s is larger than %d bytes", ErrUnreadable, name, secretLimit)
	}
	text := strings.TrimRight(string(data), "\n")
	raw, err := cloud.DecodeCanonicalBase64(text)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %s: %v", ErrUnreadable, name, err)
	}
	if len(raw) != want {
		return nil, false, fmt.Errorf("%w: %s holds %d bytes, want %d", ErrUnreadable, name, len(raw), want)
	}
	return raw, true, nil
}

func (f *Files) writeSecret(name string, raw []byte) error {
	return f.writeAtomically(name, append([]byte(encodeBase64(raw)), '\n'))
}

// open opens one of this directory's files as the file it is and only when it
// is a plain file. The Lstat before refuses a link that is there, O_NOFOLLOW
// refuses one that appears after it, and the same-file check after refuses a
// swap.
func (f *Files) open(name string, flag int, perm os.FileMode) (*os.File, error) {
	path := filepath.Join(f.dir, name)
	for range 2 {
		before, err := os.Lstat(path)
		exists := err == nil
		switch {
		case exists && !before.Mode().IsRegular():
			return nil, fmt.Errorf("%w: %s", ErrNotRegular, path)
		case !exists && !(errors.Is(err, os.ErrNotExist) && flag&os.O_CREATE != 0):
			return nil, err
		}
		mode := flag &^ (os.O_CREATE | os.O_EXCL)
		if !exists {
			mode = flag | os.O_EXCL
		}
		file, err := os.OpenFile(path, mode|noFollow, perm)
		if !exists && errors.Is(err, os.ErrExist) {
			// Made by somebody else between the two looks: look again.
			continue
		}
		if err != nil {
			if isLinkRefusal(err) {
				return nil, fmt.Errorf("%w: %s became a symlink", ErrNotRegular, path)
			}
			return nil, err
		}
		after, err := file.Stat()
		if err == nil && (!after.Mode().IsRegular() || exists && !os.SameFile(before, after)) {
			err = fmt.Errorf("%w: %s changed while it was being opened", ErrNotRegular, path)
		}
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		return file, nil
	}
	return nil, fmt.Errorf("%w: %s kept changing while it was being opened", ErrNotRegular, path)
}

// writeAtomically writes body to a temporary file in the same directory, syncs
// it, and renames it over name. A reader sees one whole secret or the other,
// never half of one.
func (f *Files) writeAtomically(name string, body []byte) (err error) {
	// The target must not be a symlink even though the rename replaces it:
	// refusing here is what stops a planted link from being the thing this
	// process believed it was managing.
	if existing, statErr := os.Lstat(filepath.Join(f.dir, name)); statErr == nil && !existing.Mode().IsRegular() {
		return fmt.Errorf("%w: %s", ErrNotRegular, filepath.Join(f.dir, name))
	}
	temporary, err := os.CreateTemp(f.dir, name+".*")
	if err != nil {
		return err
	}
	path := temporary.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	if err = temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err = temporary.Write(body); err != nil {
		_ = temporary.Close()
		return err
	}
	if err = temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	if err = os.Rename(path, filepath.Join(f.dir, name)); err != nil {
		return err
	}
	syncDir(f.dir)
	return nil
}

// syncDir makes the rename itself durable. A failure here is not worth failing
// a write over: the bytes are already on disk.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}

func within(path, dir string) bool {
	relative, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return relative == "." || !strings.HasPrefix(relative, "..")
}

func resolved(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	// A path that does not exist yet still has a parent that might.
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return path
	}
	return filepath.Join(parent, filepath.Base(path))
}
