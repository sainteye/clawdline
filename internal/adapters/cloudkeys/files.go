// Package cloudkeys keeps this daemon's two long-lived cloud secrets on disk:
// the device signing key and this machine's content key.
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
	"bytes"
	"crypto/sha256"
	"encoding/json"
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
	// ViewerKeyFile is a separate identity. A machine key must never become a viewer.
	ViewerKeyFile = "viewer-ed25519-v1"
	// MasterSecretFile holds the legacy machine content key, base64, one line.
	MasterSecretFile = "account-master-secret-v1"
	contentStateFile = "content-key-state-v1"
	contentFloorFile = "content-key-floor-v1"

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
	// fault is a deterministic test seam at actual persistence boundaries.
	fault func(string) error
}

var _ cloud.KeyStore = (*Files)(nil)
var _ cloud.ContentKeyStore = (*Files)(nil)

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
	for _, name := range []string{DeviceKeyFile, ViewerKeyFile, MasterSecretFile, contentStateFile, contentFloorFile} {
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

// ViewerKey reads the CLI viewer signing key, separate from the machine key.
func (f *Files) ViewerKey() (cloud.DeviceKey, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, found, err := f.readSecret(ViewerKeyFile, cloud.DeviceKeySeedBytes)
	if err != nil || !found {
		return cloud.DeviceKey{}, false, err
	}
	key, err := cloud.DeviceKeyFromSeed(raw)
	if err != nil {
		return cloud.DeviceKey{}, false, fmt.Errorf("%w: %s: %v", ErrUnreadable, ViewerKeyFile, err)
	}
	return key, true, nil
}

// SaveViewerKey writes the viewer signing key only after a viewer flow starts.
func (f *Files) SaveViewerKey(key cloud.DeviceKey) error {
	if !key.Valid() {
		return fmt.Errorf("%w: refusing to store an empty viewer key", cloud.ErrSeedLength)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writeSecret(ViewerKeyFile, key.Seed())
}

// MasterSecret reads the machine content key, refusing pending transactions.
func (f *Files) MasterSecret() (cloud.ContentKey, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, exists, err := f.readContentRecord()
	if err != nil {
		return cloud.ContentKey{}, false, err
	}
	if exists {
		state, err := f.committedContent(r)
		return state.Key, err == nil, err
	}
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

// SaveMasterSecret retains legacy replacement only before epoch migration.
// Afterwards only an identical committed key is accepted; this API cannot
// roll back an epoch or overwrite a pending rotation.
func (f *Files) SaveMasterSecret(secret cloud.ContentKey) error {
	if !secret.Valid() {
		return fmt.Errorf("%w: refusing to store an empty master secret", cloud.ErrKeyLength)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	r, exists, err := f.readContentRecord()
	if err != nil {
		return err
	}
	if exists {
		state, err := f.committedContent(r)
		if err != nil {
			return err
		}
		if !bytes.Equal(secret.Bytes(), state.Key.Bytes()) {
			return cloud.ErrContentKeyConflict
		}
		return nil
	}
	return f.writeSecret(MasterSecretFile, secret.Bytes())
}

// contentRecord is the sole key authority after migration. A pending record
// is an intent containing the exact next key, not permission to use it. The
// separate floor contains no key and detects missing or rolled-back authority.
// Neither file can defend against a privileged attacker restoring ALL state.
type contentRecord struct {
	Epoch     uint64 `json:"epoch"`
	KeyID     string `json:"key_id"`
	Secret    string `json:"secret"`
	Operation string `json:"operation"`
	Pending   bool   `json:"pending"`
}

type contentFloor struct {
	Epoch     uint64 `json:"epoch"`
	KeyID     string `json:"key_id"`
	Operation string `json:"operation"`
}

func (r contentRecord) floor() contentFloor { return contentFloor{r.Epoch, r.KeyID, r.Operation} }

func contentID(epoch uint64, key cloud.ContentKey) string {
	if epoch == 1 {
		return "ms-1"
	}
	digest := sha256.Sum256(key.Bytes())
	return fmt.Sprintf("mk-%d-%x", epoch, digest)
}

func (r contentRecord) state() (cloud.ContentKeyState, error) {
	raw, err := cloud.DecodeCanonicalBase64(r.Secret)
	if err != nil {
		return cloud.ContentKeyState{}, cloud.ErrContentKeyState
	}
	key, err := cloud.ContentKeyFromBytes(raw)
	if err != nil || r.Epoch == 0 || r.KeyID != contentID(r.Epoch, key) ||
		(r.Epoch == 1 && r.Operation != "") || (r.Epoch > 1 && len(r.Operation) != sha256.Size*2) {
		return cloud.ContentKeyState{}, cloud.ErrContentKeyState
	}
	return cloud.ContentKeyState{Epoch: r.Epoch, KeyID: r.KeyID, Key: key}, nil
}

// readJSON shares the registered secretLimit bound with legacy key reads.
// No parse error includes the input, which contains secret material.
func (f *Files) readJSON(name string, out any) (bool, error) {
	file, err := f.open(name, os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%w: %w", cloud.ErrContentKeyState, err)
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, secretLimit+1))
	if err != nil || len(body) > secretLimit {
		return false, cloud.ErrContentKeyState
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil || decoder.Decode(new(any)) != io.EOF {
		return false, cloud.ErrContentKeyState
	}
	return true, nil
}

func (f *Files) readContentRecord() (contentRecord, bool, error) {
	var r contentRecord
	found, err := f.readJSON(contentStateFile, &r)
	if err != nil {
		return r, false, err
	}
	var floor contentFloor
	hasFloor, err := f.readJSON(contentFloorFile, &floor)
	if err != nil {
		return r, false, err
	}
	if hasFloor && (floor.Epoch == 0 || floor.KeyID == "" ||
		(floor.Epoch == 1 && (floor.KeyID != "ms-1" || floor.Operation != "")) ||
		(floor.Epoch > 1 && len(floor.Operation) != sha256.Size*2)) {
		return r, false, cloud.ErrContentKeyState
	}
	if !found {
		if hasFloor {
			return r, false, cloud.ErrContentKeyState
		}
		return r, false, nil
	}
	if _, err := r.state(); err != nil {
		return r, false, err
	}
	if r.Pending {
		// A crash before advancing the floor is recoverable only from the
		// immediately preceding epoch, or from the initial migration.
		if (!hasFloor && r.Epoch != 1) || (hasFloor && floor != r.floor() && floor.Epoch != r.Epoch-1) {
			return r, false, cloud.ErrContentKeyState
		}
	} else if !hasFloor || floor != r.floor() {
		return r, false, cloud.ErrContentKeyState
	}
	return r, true, nil
}

func (f *Files) committedContent(r contentRecord) (cloud.ContentKeyState, error) {
	if r.Pending {
		return cloud.ContentKeyState{}, cloud.ErrContentKeyPending
	}
	state, err := r.state()
	if err != nil {
		return cloud.ContentKeyState{}, err
	}
	legacy, found, err := f.readSecret(MasterSecretFile, cloud.ContentKeyBytes)
	if err != nil || !found || !bytes.Equal(legacy, state.Key.Bytes()) {
		return cloud.ContentKeyState{}, cloud.ErrContentKeyState
	}
	// Re-establish durability when a prior rename succeeded but its directory
	// sync failed and the caller is recovering in the same process.
	if err := f.boundary(contentStateFile + ":load-sync"); err != nil {
		return cloud.ContentKeyState{}, fmt.Errorf("%w: %w", cloud.ErrContentKeyPending, err)
	}
	if err := syncDir(f.dir); err != nil {
		return cloud.ContentKeyState{}, fmt.Errorf("%w: %w", cloud.ErrContentKeyPending, err)
	}
	return state, nil
}

func (f *Files) writeJSON(name string, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return cloud.ErrContentKeyState
	}
	return f.writeFile(name, body, true)
}

func (f *Files) recoverContent(r contentRecord) (cloud.ContentKeyState, error) {
	if !r.Pending {
		return f.committedContent(r)
	}
	// No multi-file atomicity is claimed. Until the last durable write, every
	// new reader fails closed. An old binary may use the new legacy key early,
	// but after success it cannot accidentally publish under the revoked key.
	if err := f.writeJSON(contentFloorFile, r.floor()); err != nil {
		return cloud.ContentKeyState{}, fmt.Errorf("%w: %w", cloud.ErrContentKeyPending, err)
	}
	state, err := r.state()
	if err != nil {
		return cloud.ContentKeyState{}, err
	}
	if err := f.writeFile(MasterSecretFile, append([]byte(encodeBase64(state.Key.Bytes())), '\n'), true); err != nil {
		return cloud.ContentKeyState{}, fmt.Errorf("%w: %w", cloud.ErrContentKeyPending, err)
	}
	r.Pending = false
	if err := f.writeJSON(contentStateFile, r); err != nil {
		return cloud.ContentKeyState{}, fmt.Errorf("%w: %w", cloud.ErrContentKeyPending, err)
	}
	return state, nil
}

// RecoverContentKey completes a durable intent without drawing randomness.
// It also accepts a committed record when the previous success reply was lost.
func (f *Files) RecoverContentKey() (cloud.ContentKeyState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, found, err := f.readContentRecord()
	if err != nil {
		return cloud.ContentKeyState{}, err
	}
	if !found {
		return cloud.ContentKeyState{}, cloud.ErrContentKeyState
	}
	return f.recoverContent(r)
}

// LoadOrCreateContentKey explicitly migrates the legacy key without rotation.
// Existing MasterSecret callers do not automatically create epoch records.
func (f *Files) LoadOrCreateContentKey(source io.Reader) (cloud.ContentKeyState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loadContent(source)
}

func (f *Files) loadContent(source io.Reader) (cloud.ContentKeyState, error) {
	r, found, err := f.readContentRecord()
	if err != nil {
		return cloud.ContentKeyState{}, err
	}
	if found {
		return f.committedContent(r)
	}
	raw, found, err := f.readSecret(MasterSecretFile, cloud.ContentKeyBytes)
	if err != nil {
		return cloud.ContentKeyState{}, err
	}
	var key cloud.ContentKey
	if found {
		key, err = cloud.ContentKeyFromBytes(raw)
	} else {
		key, err = cloud.NewContentKey(source)
	}
	if err != nil {
		return cloud.ContentKeyState{}, err
	}
	r = contentRecord{Epoch: 1, KeyID: "ms-1", Secret: encodeBase64(key.Bytes()), Pending: true}
	if err := f.writeJSON(contentStateFile, r); err != nil {
		return cloud.ContentKeyState{}, fmt.Errorf("%w: %w", cloud.ErrContentKeyPending, err)
	}
	return f.recoverContent(r)
}

// RotateContentKey requires a stable nonempty operation ID and the previously
// loaded epoch. Only the latest operation is retained, so storage stays fixed
// size regardless of rotations. The same operation/epoch pair is idempotent.
// The daemon must exclusively own this Files instance and directory: this is
// not an interprocess transaction lock.
func (f *Files) RotateContentKey(operationID string, expectedEpoch uint64, source io.Reader) (cloud.ContentKeyState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if operationID == "" || expectedEpoch == 0 || expectedEpoch == ^uint64(0) {
		return cloud.ContentKeyState{}, cloud.ErrContentKeyConflict
	}
	digest := sha256.Sum256([]byte(operationID))
	op := fmt.Sprintf("%x", digest)
	r, found, err := f.readContentRecord()
	if err != nil {
		return cloud.ContentKeyState{}, err
	}
	if !found {
		return cloud.ContentKeyState{}, cloud.ErrContentKeyState
	}
	if r.Operation == op && r.Epoch == expectedEpoch+1 {
		return f.recoverContent(r)
	}
	if r.Pending {
		return cloud.ContentKeyState{}, cloud.ErrContentKeyPending
	}
	if r.Epoch != expectedEpoch || r.Operation == op {
		return cloud.ContentKeyState{}, cloud.ErrContentKeyConflict
	}
	current, err := f.committedContent(r)
	if err != nil {
		return cloud.ContentKeyState{}, err
	}
	key, err := cloud.NewContentKey(source)
	if err != nil {
		return cloud.ContentKeyState{}, err
	}
	if bytes.Equal(key.Bytes(), current.Key.Bytes()) {
		return cloud.ContentKeyState{}, cloud.ErrContentKeyConflict
	}
	r = contentRecord{Epoch: r.Epoch + 1, KeyID: contentID(r.Epoch+1, key), Secret: encodeBase64(key.Bytes()), Operation: op, Pending: true}
	if err := f.writeJSON(contentStateFile, r); err != nil {
		return cloud.ContentKeyState{}, fmt.Errorf("%w: %w", cloud.ErrContentKeyPending, err)
	}
	return f.recoverContent(r)
}

// Forget removes both secrets. Signing out must not leave key material behind.
// A file that was not there is not an error; one that could not be removed is.
func (f *Files) Forget() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, stateErr := os.Lstat(filepath.Join(f.dir, contentStateFile))
	_, floorErr := os.Lstat(filepath.Join(f.dir, contentFloorFile))
	migrated := !errors.Is(stateErr, os.ErrNotExist) || !errors.Is(floorErr, os.ErrNotExist)
	// Remove the legacy key before the authority, and the floor last. A
	// partial deletion must never make an old key look like an unmigrated one.
	for _, name := range []string{DeviceKeyFile, ViewerKeyFile, MasterSecretFile, contentStateFile, contentFloorFile} {
		if err := os.Remove(filepath.Join(f.dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("cloud key material remains: %s: %w", name, err)
		}
		if err := syncDir(f.dir); err != nil && migrated {
			return err
		}
	}
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
	// Preserve the pre-migration store's best-effort directory sync. Content
	// transactions explicitly require strict directory durability instead.
	return f.writeFile(name, body, false)
}

func (f *Files) writeFile(name string, body []byte, durable bool) (err error) {
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
	if err = f.boundary(name + ":write"); err == nil {
		_, err = temporary.Write(body)
	}
	if err != nil {
		_ = temporary.Close()
		return err
	}
	if err = f.boundary(name + ":file-sync"); err == nil {
		err = temporary.Sync()
	}
	if err != nil {
		_ = temporary.Close()
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	if err = f.boundary(name + ":rename"); err == nil {
		err = os.Rename(path, filepath.Join(f.dir, name))
	}
	if err != nil {
		return err
	}
	if err = f.boundary(name + ":dir-sync"); err != nil {
		return err
	}
	err = syncDir(f.dir)
	if !durable {
		return nil
	}
	return err
}

func (f *Files) boundary(stage string) error {
	if f.fault != nil {
		return f.fault(stage)
	}
	return nil
}

// syncDir makes the rename itself durable. Failure is uncertain durability,
// never success: key rotation must not acknowledge an unsynced commit.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	closeErr := d.Close()
	if err != nil {
		return err
	}
	return closeErr
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
