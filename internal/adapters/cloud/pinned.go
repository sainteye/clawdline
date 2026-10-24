package cloud

// Who this machine pinned, and who it threw out.
//
// `roster.go` reads the account's device list from the control plane, which is
// what let the fourth wave admit a viewer at all. A roster key alone does not
// prove pairing: the viewer must also produce an authenticated envelope under
// this machine's content key. A local pin is stronger identity evidence and
// takes precedence over a substituted roster key.
//
// This file is that local root. It holds one row per browser this machine
// itself handed the machine content key to, written at the moment of pairing, with the
// Ed25519 public key that came inside the sealed offer — bytes the cloud never
// saw in the clear. `PublicKeyFor` prefers it; the roster is the fallback for
// a device paired before this file existed, and the *refused* list beats both.
//
// Three rules worth naming:
//
//   - **Operate pins follow delivery.** A readonly pairing first reserves its
//     ceiling before key delivery, so a failed final write cannot admit the
//     viewer through the legacy roster fallback as an operator. A failed
//     readonly handover can leave a readonly reservation behind, but a
//     reservation never lifts a refusal: only the pin written after a
//     completed handover does.
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

const ScopeReadOnly = "readonly"
const ScopeOperate = "operate"

var ErrViewerScope = errors.New("viewer scope must be readonly or operate")
var ErrViewerNotPinned = errors.New("the viewer has no active local pin")

func ValidViewerScope(scope string) bool { return scope == ScopeReadOnly || scope == ScopeOperate }

// PinnedDevice is one browser this machine paired.
//
// `PublicKey` is standard padded base64 of 32 bytes — the same spelling the
// sealed offer carried, kept rather than re-encoded so that the file and the
// wire agree byte for byte. No private material is ever in this file: it holds
// public keys, ids and times, which is why it is JSON a person can read.
type PinnedDevice struct {
	// Scope is machine-local authority. Empty preserves pre-scope grants.
	Scope string `json:"scope,omitempty"`
	// PairingPending distinguishes a readonly ceiling reservation from a
	// completed handover; raw key exchange never treats it as a pairing pin.
	PairingPending bool   `json:"pairing_pending,omitempty"`
	DeviceID       string `json:"device_id"`
	PublicKey      string `json:"public_key"`
	Fingerprint    string `json:"fingerprint"`
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

func (d PinnedDevice) EffectiveScope() string {
	if d.Scope == "" {
		return ScopeOperate
	}
	return d.Scope
}

// ViewerScope returns the local ceiling. Missing pins retain legacy admission;
// malformed, revoked or future scopes fail closed rather than becoming operate.
func (s *PinnedStore) ViewerScope(sender string) (string, error) {
	rows, err := s.Devices()
	if err != nil {
		return "", err
	}
	for _, row := range rows {
		if row.DeviceID == sender {
			if !row.Active() {
				return "", ErrViewerNotPinned
			}
			if !ValidViewerScope(row.EffectiveScope()) {
				return "", ErrViewerScope
			}
			return row.EffectiveScope(), nil
		}
	}
	return ScopeOperate, nil
}

// SetScope changes only an existing active local pin, atomically and idempotently.
func (s *PinnedStore) SetScope(sender, scope string) error {
	if !ValidViewerScope(scope) {
		return ErrViewerScope
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	for i := range s.record.Devices {
		if s.record.Devices[i].DeviceID == sender && s.record.Devices[i].Active() {
			before := s.record.Devices[i].Scope
			s.record.Devices[i].Scope = scope
			if err := s.saveLocked(); err != nil {
				s.record.Devices[i].Scope = before
				return err
			}
			return nil
		}
	}
	return ErrViewerNotPinned
}

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

// ActiveRekeyPin excludes legacy roster fallback and pre-delivery reservations.
// All pin facts are read together, including the owning account.
func (s *PinnedStore) ActiveRekeyPin(account, viewer string) (ed25519.PublicKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil, err
	}
	if s.record.AccountID != account || account == "" {
		return nil, ErrViewerNotPinned
	}
	for _, row := range s.record.Devices {
		if row.DeviceID == viewer && row.Active() && !row.PairingPending && ValidViewerScope(row.EffectiveScope()) {
			if key, ok := row.Key(); ok {
				return key, nil
			}
		}
	}
	return nil, ErrViewerNotPinned
}

// Pin records a browser this machine has just handed the account key to.
//
// Re-pinning an existing device replaces its key and clears any refusal: that
// is what "pair this browser again" means, and it is the browser's own
// repair path when its keys drift.
func (s *PinnedStore) Pin(accountID string, device PinnedDevice) error {
	_, err := s.pin(accountID, device, false)
	return err
}

// RepairScope is the ceiling a re-pairing keeps when nobody on this machine
// chose one. A device this machine already holds a row for keeps that row's
// scope, so pairing a readonly browser again is not a silent promotion; only a
// device it has never pinned takes the legacy operate default. An unreadable
// file or a scope this build does not know refuses instead of guessing.
func (s *PinnedStore) RepairScope(sender string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return "", err
	}
	return s.repairScopeLocked(sender)
}

func (s *PinnedStore) repairScopeLocked(sender string) (string, error) {
	for _, row := range s.record.Devices {
		if row.DeviceID == sender {
			if !ValidViewerScope(row.EffectiveScope()) {
				return "", ErrViewerScope
			}
			return row.EffectiveScope(), nil
		}
	}
	return ScopeOperate, nil
}

// PinKeepingScope is Pin for a re-pairing with no scope chosen: the scope
// written is RepairScope's answer, read under the same lock as the write, so a
// grant that lands while the handover is in flight is not overwritten. It
// answers the scope it wrote.
func (s *PinnedStore) PinKeepingScope(accountID string, device PinnedDevice) (string, error) {
	return s.pin(accountID, device, true)
}

func (s *PinnedStore) pin(accountID string, device PinnedDevice, keepScope bool) (string, error) {
	if device.Scope != "" && !ValidViewerScope(device.Scope) {
		return "", ErrViewerScope
	}
	if device.DeviceID == "" {
		return "", errors.New("refusing to pin a device with no id")
	}
	if _, ok := device.Key(); !ok {
		return "", errors.New("refusing to pin a device with no usable public key")
	}
	if device.PairedAt == 0 {
		device.PairedAt = time.Now().Unix()
	}
	device.RevokedAt = 0
	device.PairingPending = false
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return "", err
	}
	if s.record.AccountID != "" && accountID != "" && s.record.AccountID != accountID {
		// A record from another account is not merged into. Signing out is
		// what empties it; quietly mixing two accounts' pins would make the
		// file say something no single account ever agreed to.
		return "", fmt.Errorf("the paired-device file belongs to account %s, not %s",
			s.record.AccountID, accountID)
	}
	if keepScope {
		scope, err := s.repairScopeLocked(device.DeviceID)
		if err != nil {
			return "", err
		}
		device.Scope = scope
	}
	before := s.record
	before.Devices = append([]PinnedDevice(nil), s.record.Devices...)
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
	if err := s.saveLocked(); err != nil {
		s.record = before
		return "", err
	}
	return device.EffectiveScope(), nil
}

// ReserveReadOnly lowers a device's ceiling to readonly before a handover
// leaves, without otherwise changing what this machine knows about it.
//
// It exists for the pairing's pre-delivery step, which is not yet a pairing:
// the handover may still fail or be cancelled. So, unlike Pin, it keeps a
// refusal — the revoked row stays revoked, read and written under the same
// lock so no concurrent writer slips between them — and it keeps an existing
// row's key and pairing time. Only a device this machine holds no row for gets
// a new active readonly row, which is what stops a failed final write from
// leaving it to the roster's legacy operate fallback. The pin written after a
// completed handover is what restores a revoked device.
func (s *PinnedStore) ReserveReadOnly(accountID string, device PinnedDevice) error {
	if device.DeviceID == "" {
		return errors.New("refusing to reserve a device with no id")
	}
	if _, ok := device.Key(); !ok {
		return errors.New("refusing to reserve a device with no usable public key")
	}
	if device.PairedAt == 0 {
		device.PairedAt = time.Now().Unix()
	}
	device.Scope, device.RevokedAt = ScopeReadOnly, 0
	device.PairingPending = true
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	if s.record.AccountID != "" && accountID != "" && s.record.AccountID != accountID {
		return fmt.Errorf("the paired-device file belongs to account %s, not %s",
			s.record.AccountID, accountID)
	}
	before := s.record
	before.Devices = append([]PinnedDevice(nil), s.record.Devices...)
	s.record.Version = 1
	if accountID != "" {
		s.record.AccountID = accountID
	}
	found := false
	for i := range s.record.Devices {
		if s.record.Devices[i].DeviceID == device.DeviceID {
			s.record.Devices[i].Scope, found = ScopeReadOnly, true
			break
		}
	}
	if !found {
		s.record.Devices = append(s.record.Devices, device)
	}
	if err := s.saveLocked(); err != nil {
		s.record = before
		return err
	}
	return nil
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
		if err := s.saveLocked(); err != nil {
			s.record.Devices[i].RevokedAt = 0
			s.loaded = false
			return false, err
		}
		return true, nil
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
	d, err := os.Open(s.dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	_ = d.Close()
	if err != nil {
		return err
	}
	s.stamp = stampOf(s.Path())
	return nil
}
