package cloud

// Who this machine pinned, and who it threw out.
//
// `roster.go` reads the account's device list from the control plane, which is
// what let the fourth wave admit a viewer at all. It is the wrong root of
// trust and always was: PROTOCOL.md §3 says a machine holds a paired device's
// key **locally** and does not trust what the cloud says about it. A control
// plane that answered with a substituted `public_key` would otherwise be able
// to speak as any viewer on the account.
//
// This file is that local root. It holds one row per browser this machine
// itself handed the account key to, written at the moment of pairing, with the
// Ed25519 public key that came inside the sealed offer — bytes the cloud never
// saw in the clear. `PublicKeyFor` prefers it; the roster is the fallback for
// a device paired before this file existed, and the *refused* list beats both.
//
// Three rules worth naming:
//
//   - **A pin is written after delivery, never before.** Pinning is what makes
//     a browser able to drive this Mac; a browser that never received the
//     account key cannot produce a command anyway, so pinning first would only
//     leave a pinned viewer behind every failed handover.
//   - **Revoking is local and it is final here.** The control plane's
//     `DELETE /v1/devices/:id` needs a browser session, which a daemon does not
//     have; and even when the account revokes a device, "the roster no longer
//     lists it" is a fact this machine learns a minute later. A refusal written
//     here stops that sender at the next envelope, and keeps stopping it even
//     if the roster comes back saying otherwise.
//   - **Unreadable is not empty.** A pin file that exists and will not parse is
//     an error. Reading it as "nobody is paired" would silently fall back to
//     the cloud's list, which is the exact substitution the pins exist against.

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// PinnedFile is the record's name inside the cloud key directory. It sits
// beside the identity record, under the same 0700 directory, so that one
// directory holds everything this daemon knows about its cloud self.
const PinnedFile = "paired-devices-v1.json"

// ErrPinnedUnreadable is a pin file that exists and is not readable as one.
var ErrPinnedUnreadable = errors.New("the paired-device file cannot be read")

// PinnedDevice is one browser this machine paired.
//
// `PublicKey` is standard padded base64 of 32 bytes — the same spelling the
// sealed offer carried, kept rather than re-encoded so that the file and the
// wire agree byte for byte. No private material is ever in this file: it holds
// public keys, ids and times, which is why it is JSON a person can read.
type PinnedDevice struct {
	DeviceID    string `json:"device_id"`
	PublicKey   string `json:"public_key"`
	Fingerprint string `json:"fingerprint"`
	// PairedAt is when this machine sealed the handover, in Unix seconds.
	PairedAt int64 `json:"paired_at"`
	// Name is what the person will recognise the browser by, as the offer's
	// account and device ids allow. It is cosmetic and may be empty.
	Name string `json:"name,omitempty"`
	// RevokedAt is when this machine threw the device out, in Unix seconds.
	// A revoked row is kept rather than deleted: the row is the refusal, and
	// deleting it would let the cloud's roster quietly re-admit the device.
	RevokedAt int64 `json:"revoked_at,omitempty"`
}

// Active reports whether this row still admits its device.
func (d PinnedDevice) Active() bool { return d.RevokedAt == 0 }

// Key answers the pinned public key, and whether the row holds a usable one.
func (d PinnedDevice) Key() (ed25519.PublicKey, bool) {
	raw, err := base64.StdEncoding.DecodeString(d.PublicKey)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, false
	}
	return ed25519.PublicKey(raw), true
}

// pinnedRecord is the file's shape. The account id is stored so that a record
// left behind by a previous account is refused rather than half-used.
type pinnedRecord struct {
	Version   int            `json:"version"`
	AccountID string         `json:"account_id"`
	Devices   []PinnedDevice `json:"devices"`
}

// PinnedStore reads and writes the record.
type PinnedStore struct {
	dir string
	mu  sync.Mutex

	loaded  bool
	record  pinnedRecord
	loadErr error
	// stamp is the file as it was when it was last read. The record is
	// re-read when it changes, because this file has more than one writer in
	// practice — a second `clawdline` process, or a person with an editor —
	// and a daemon that cached a revocation away would keep admitting a
	// device somebody threw out.
	stamp fileStamp
}

// fileStamp is the cheapest honest "is this the same file" this needs.
type fileStamp struct {
	size    int64
	modTime time.Time
	missing bool
}

func stampOf(path string) fileStamp {
	info, err := os.Stat(path)
	if err != nil {
		return fileStamp{missing: true}
	}
	return fileStamp{size: info.Size(), modTime: info.ModTime()}
}

// NewPinnedStore keeps the record in dir, which is the cloudkeys directory.
func NewPinnedStore(dir string) *PinnedStore { return &PinnedStore{dir: dir} }

// Path is where the record is, whether or not it exists.
func (s *PinnedStore) Path() string { return filepath.Join(s.dir, PinnedFile) }

// Devices answers every row, newest pairing first, and whether the file could
// be read at all. A caller that treats `false` as "no devices" has made the
// mistake this store exists to prevent.
func (s *PinnedStore) Devices() ([]PinnedDevice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil, err
	}
	out := append([]PinnedDevice(nil), s.record.Devices...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].PairedAt > out[j].PairedAt })
	return out, nil
}

// PublicKeyFor answers the pinned key for a sender.
//
// The three answers are distinct on purpose. `(key, true, nil)` is a live pin.
// `(nil, false, nil)` is "this store holds nothing about that sender", which
// lets a caller fall back to the roster. A refused device answers
// `(nil, false, nil)` too — but `Refused` says so, and the caller checks that
// first, because falling back to the roster for a device this machine threw
// out would undo the revocation.
func (s *PinnedStore) PublicKeyFor(sender string) (ed25519.PublicKey, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil, false, err
	}
	for _, device := range s.record.Devices {
		if device.DeviceID != sender || !device.Active() {
			continue
		}
		if key, ok := device.Key(); ok {
			return key, true, nil
		}
		// A row this build cannot read is not a pin. It is also not an
		// absence that should fall through to the cloud, so it is an error.
		return nil, false, fmt.Errorf("%w: %s holds an unusable public key", ErrPinnedUnreadable, sender)
	}
	return nil, false, nil
}

// Refused reports whether this machine threw that sender out.
func (s *PinnedStore) Refused(sender string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return false, err
	}
	for _, device := range s.record.Devices {
		if device.DeviceID == sender && !device.Active() {
			return true, nil
		}
	}
	return false, nil
}

// Pin records a browser this machine has just handed the account key to.
//
// Re-pinning an existing device replaces its key and clears any refusal: that
// is what "pair this browser again" means, and it is the browser's own
// repair path when its keys drift.
func (s *PinnedStore) Pin(accountID string, device PinnedDevice) error {
	if device.DeviceID == "" {
		return errors.New("refusing to pin a device with no id")
	}
	if _, ok := device.Key(); !ok {
		return errors.New("refusing to pin a device with no usable public key")
	}
	if device.PairedAt == 0 {
		device.PairedAt = time.Now().Unix()
	}
	device.RevokedAt = 0
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	if s.record.AccountID != "" && accountID != "" && s.record.AccountID != accountID {
		// A record from another account is not merged into. Signing out is
		// what empties it; quietly mixing two accounts' pins would make the
		// file say something no single account ever agreed to.
		return fmt.Errorf("the paired-device file belongs to account %s, not %s",
			s.record.AccountID, accountID)
	}
	s.record.Version = 1
	if accountID != "" {
		s.record.AccountID = accountID
	}
	replaced := false
	for i := range s.record.Devices {
		if s.record.Devices[i].DeviceID == device.DeviceID {
			s.record.Devices[i], replaced = device, true
			break
		}
	}
	if !replaced {
		s.record.Devices = append(s.record.Devices, device)
	}
	return s.saveLocked()
}

// Revoke marks a device refused. It answers whether a row changed, so that a
// caller can tell "that device is now out" from "there was no such device".
func (s *PinnedStore) Revoke(deviceID string, at time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return false, err
	}
	when := at.Unix()
	if when <= 0 {
		when = time.Now().Unix()
	}
	for i := range s.record.Devices {
		if s.record.Devices[i].DeviceID != deviceID || !s.record.Devices[i].Active() {
			continue
		}
		s.record.Devices[i].RevokedAt = when
		return true, s.saveLocked()
	}
	return false, nil
}

// Forget removes the whole record, which is what signing out means.
func (s *PinnedStore) Forget() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loaded, s.record, s.loadErr, s.stamp = false, pinnedRecord{}, nil, fileStamp{}
	if err := os.Remove(s.Path()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// loadLocked reads the file once and remembers the answer, including a
// failure: a store that retried a permission error on every envelope would
// turn one misconfigured file into a syscall per inbound message.
func (s *PinnedStore) loadLocked() error {
	stamp := stampOf(s.Path())
	if s.loaded && stamp == s.stamp {
		return s.loadErr
	}
	s.loaded, s.stamp, s.loadErr = true, stamp, nil
	data, err := os.ReadFile(s.Path())
	if errors.Is(err, os.ErrNotExist) {
		s.record = pinnedRecord{Version: 1}
		return nil
	}
	if err != nil {
		s.loadErr = fmt.Errorf("%w: %v", ErrPinnedUnreadable, err)
		return s.loadErr
	}
	var record pinnedRecord
	if err := json.Unmarshal(data, &record); err != nil {
		s.loadErr = fmt.Errorf("%w: %v", ErrPinnedUnreadable, err)
		return s.loadErr
	}
	if record.Version != 1 {
		s.loadErr = fmt.Errorf("%w: version %d is not this build's", ErrPinnedUnreadable, record.Version)
		return s.loadErr
	}
	s.record = record
	return nil
}

func (s *PinnedStore) saveLocked() error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(s.record, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomically(s.dir, s.Path(), append(body, '\n')); err != nil {
		return err
	}
	s.stamp = stampOf(s.Path())
	return nil
}
