package cloud

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func freshPin(t *testing.T, id string) PinnedDevice {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("drawing a key: %v", err)
	}
	return PinnedDevice{
		DeviceID:    id,
		PublicKey:   base64.StdEncoding.EncodeToString(public),
		Fingerprint: "AAAA-BBBB-CCCC-DDDD",
		PairedAt:    time.Now().Unix(),
	}
}

func TestAPinnedKeyIsTheOneAnEnvelopeIsVerifiedAgainst(t *testing.T) {
	store := NewPinnedStore(t.TempDir())
	device := freshPin(t, "dev_one")
	if err := store.Pin("usr_one", device); err != nil {
		t.Fatalf("pinning: %v", err)
	}
	key, ok, err := store.PublicKeyFor("dev_one")
	if err != nil || !ok {
		t.Fatalf("the pin was not readable back: ok=%v err=%v", ok, err)
	}
	want, _ := device.Key()
	if !key.Equal(want) {
		t.Fatal("the pin answered a different key than it was given")
	}
	if _, ok, err := store.PublicKeyFor("dev_other"); ok || err != nil {
		t.Fatalf("a sender this store never saw answered: ok=%v err=%v", ok, err)
	}
}

func TestARevokedDeviceStaysRefusedAndStaysVisible(t *testing.T) {
	store := NewPinnedStore(t.TempDir())
	if err := store.Pin("usr_one", freshPin(t, "dev_one")); err != nil {
		t.Fatalf("pinning: %v", err)
	}
	changed, err := store.Revoke("dev_one", time.Now())
	if err != nil || !changed {
		t.Fatalf("revoking: changed=%v err=%v", changed, err)
	}
	if _, ok, err := store.PublicKeyFor("dev_one"); ok || err != nil {
		t.Fatalf("a revoked device still answered a key: ok=%v err=%v", ok, err)
	}
	refused, err := store.Refused("dev_one")
	if err != nil || !refused {
		t.Fatalf("a revoked device did not read as refused: %v %v", refused, err)
	}
	// The row stays, because the row *is* the refusal: deleting it would let
	// the account's roster quietly re-admit the device.
	devices, err := store.Devices()
	if err != nil || len(devices) != 1 || devices[0].Active() {
		t.Fatalf("the revoked row is not on the list: %#v %v", devices, err)
	}
	// Revoking twice is not an error and is not a second change.
	if changed, err := store.Revoke("dev_one", time.Now()); err != nil || changed {
		t.Fatalf("a second revoke reported a change: %v %v", changed, err)
	}
}

func TestPairingAgainReplacesAKeyAndClearsARefusal(t *testing.T) {
	store := NewPinnedStore(t.TempDir())
	first := freshPin(t, "dev_one")
	if err := store.Pin("usr_one", first); err != nil {
		t.Fatalf("pinning: %v", err)
	}
	if _, err := store.Revoke("dev_one", time.Now()); err != nil {
		t.Fatalf("revoking: %v", err)
	}
	second := freshPin(t, "dev_one")
	if err := store.Pin("usr_one", second); err != nil {
		t.Fatalf("re-pinning: %v", err)
	}
	refused, err := store.Refused("dev_one")
	if err != nil || refused {
		t.Fatalf("re-pairing did not clear the refusal: %v %v", refused, err)
	}
	key, ok, err := store.PublicKeyFor("dev_one")
	if err != nil || !ok {
		t.Fatalf("re-pairing left no key: %v %v", ok, err)
	}
	want, _ := second.Key()
	if !key.Equal(want) {
		t.Fatal("re-pairing kept the old key")
	}
	if devices, err := store.Devices(); err != nil || len(devices) != 1 {
		t.Fatalf("re-pairing added a second row: %#v %v", devices, err)
	}
}

func TestAnUnreadablePinFileIsAnErrorRatherThanAnEmptyOne(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, PinnedFile), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("writing the broken file: %v", err)
	}
	store := NewPinnedStore(dir)
	if _, _, err := store.PublicKeyFor("dev_one"); !errors.Is(err, ErrPinnedUnreadable) {
		t.Fatalf("a broken pin file read as an empty one: %v", err)
	}
	if _, err := store.Devices(); !errors.Is(err, ErrPinnedUnreadable) {
		t.Fatalf("a broken pin file listed as an empty one: %v", err)
	}
	if _, err := store.Refused("dev_one"); !errors.Is(err, ErrPinnedUnreadable) {
		t.Fatalf("a broken pin file answered a refusal question: %v", err)
	}
}

// The file has more than one writer in practice, so a store that cached the
// first reading would keep admitting a device somebody else revoked.
func TestAStoreSeesWhatAnotherProcessWrote(t *testing.T) {
	dir := t.TempDir()
	writer := NewPinnedStore(dir)
	reader := NewPinnedStore(dir)
	if err := writer.Pin("usr_one", freshPin(t, "dev_one")); err != nil {
		t.Fatalf("pinning: %v", err)
	}
	if _, ok, err := reader.PublicKeyFor("dev_one"); !ok || err != nil {
		t.Fatalf("the second store did not see the pin: %v %v", ok, err)
	}
	// A same-second write has to be visible too, so the stamp cannot be the
	// modification time alone on a filesystem with one-second granularity.
	if _, err := writer.Revoke("dev_one", time.Now()); err != nil {
		t.Fatalf("revoking: %v", err)
	}
	refused, err := reader.Refused("dev_one")
	if err != nil {
		t.Fatalf("reading the refusal: %v", err)
	}
	if !refused {
		t.Fatal("the second store kept admitting a device the first one revoked")
	}
}

func TestAPinFileBelongingToAnotherAccountIsNotMergedInto(t *testing.T) {
	store := NewPinnedStore(t.TempDir())
	if err := store.Pin("usr_one", freshPin(t, "dev_one")); err != nil {
		t.Fatalf("pinning: %v", err)
	}
	if err := store.Pin("usr_two", freshPin(t, "dev_two")); err == nil {
		t.Fatal("a second account's pin was written into the first account's file")
	}
}

func TestAPinWithNoUsableKeyIsRefused(t *testing.T) {
	store := NewPinnedStore(t.TempDir())
	if err := store.Pin("usr_one", PinnedDevice{DeviceID: "dev_one", PublicKey: "not base64"}); err == nil {
		t.Fatal("a device with no usable key was pinned")
	}
	if err := store.Pin("usr_one", PinnedDevice{PublicKey: "AA=="}); err == nil {
		t.Fatal("a device with no id was pinned")
	}
}

func TestForgettingEmptiesTheFile(t *testing.T) {
	dir := t.TempDir()
	store := NewPinnedStore(dir)
	if err := store.Pin("usr_one", freshPin(t, "dev_one")); err != nil {
		t.Fatalf("pinning: %v", err)
	}
	if err := store.Forget(); err != nil {
		t.Fatalf("forgetting: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, PinnedFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the pin file survived signing out: %v", err)
	}
	devices, err := store.Devices()
	if err != nil || len(devices) != 0 {
		t.Fatalf("a forgotten store still lists devices: %#v %v", devices, err)
	}
}

// 0600, because the file names every browser that can drive this Mac. It holds
// no private key, but a list of ids and public keys is still a map of the
// account for anything that can read this directory.
func TestThePinFileIsOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	store := NewPinnedStore(dir)
	if err := store.Pin("usr_one", freshPin(t, "dev_one")); err != nil {
		t.Fatalf("pinning: %v", err)
	}
	info, err := os.Stat(store.Path())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("the pin file is %o, wanted 600", mode)
	}
}

func TestViewerScopePersistsAndCannotBeExpandedByLegacyPinReads(t *testing.T) {
	dir := t.TempDir()
	s := NewPinnedStore(dir)
	for _, id := range []string{"reader", "operator", "legacy"} {
		d := freshPin(t, id)
		if id == "reader" {
			d.Scope = ScopeReadOnly
		}
		if id == "operator" {
			d.Scope = ScopeOperate
		}
		if err := s.Pin("account", d); err != nil {
			t.Fatal(err)
		}
	}
	for _, store := range []*PinnedStore{s, NewPinnedStore(dir)} {
		for id, want := range map[string]string{"reader": ScopeReadOnly, "operator": ScopeOperate, "legacy": ScopeOperate} {
			if got, err := store.ViewerScope(id); err != nil || got != want {
				t.Fatalf("%s: %q %v", id, got, err)
			}
		}
	}
	if err := s.SetScope("reader", "admin"); !errors.Is(err, ErrViewerScope) {
		t.Fatal(err)
	}
	if err := s.SetScope("missing", ScopeOperate); !errors.Is(err, ErrViewerNotPinned) {
		t.Fatal(err)
	}
	if err := s.SetScope("operator", ScopeReadOnly); err != nil {
		t.Fatal(err)
	}
	if err := s.SetScope("operator", ScopeReadOnly); err != nil {
		t.Fatal(err)
	}
	if got, err := NewPinnedStore(dir).ViewerScope("operator"); err != nil || got != ScopeReadOnly {
		t.Fatalf("restart: %q %v", got, err)
	}
	if _, err := s.Revoke("reader", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.SetScope("reader", ScopeOperate); !errors.Is(err, ErrViewerNotPinned) {
		t.Fatal(err)
	}
}

func TestFailedScopeWriteDoesNotChangeLiveOrRestartedAuthority(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	dir := t.TempDir()
	s := NewPinnedStore(dir)
	d := freshPin(t, "reader")
	d.Scope = ScopeReadOnly
	if err := s.Pin("account", d); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	if err := s.SetScope("reader", ScopeOperate); err == nil {
		t.Fatal("write unexpectedly succeeded")
	}
	for _, store := range []*PinnedStore{s, NewPinnedStore(dir)} {
		if got, err := store.ViewerScope("reader"); err != nil || got != ScopeReadOnly {
			t.Fatalf("failed write elevated: %q %v", got, err)
		}
	}
	d.Scope = ScopeOperate
	if err := s.Pin("account", d); err == nil {
		t.Fatal("re-pin unexpectedly succeeded")
	}
	if got, err := s.ViewerScope("reader"); err != nil || got != ScopeReadOnly {
		t.Fatalf("failed re-pin elevated: %q %v", got, err)
	}
}

func TestAReadonlyReservationKeepsARefusalAndOnlyLowersAnExistingRow(t *testing.T) {
	dir := t.TempDir()
	s := NewPinnedStore(dir)
	revoked, active := freshPin(t, "revoked"), freshPin(t, "active")
	for _, d := range []PinnedDevice{revoked, active} {
		if err := s.Pin("account", d); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Revoke("revoked", time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"revoked", "active", "new"} {
		if err := s.ReserveReadOnly("account", freshPin(t, id)); err != nil {
			t.Fatalf("reserve %s: %v", id, err)
		}
	}
	restarted := NewPinnedStore(dir)
	if refused, err := restarted.Refused("revoked"); err != nil || !refused {
		t.Fatalf("a reservation lifted a refusal: %v %v", refused, err)
	}
	if key, ok, err := restarted.PublicKeyFor("active"); err != nil || !ok || base64.StdEncoding.EncodeToString(key) != active.PublicKey {
		t.Fatalf("a reservation replaced an active pin's key: %v %v", ok, err)
	}
	for _, id := range []string{"active", "new"} {
		if got, err := restarted.ViewerScope(id); err != nil || got != ScopeReadOnly {
			t.Fatalf("%s ceiling: %q %v", id, got, err)
		}
	}
	if err := restarted.Pin("account", freshPin(t, "revoked")); err != nil {
		t.Fatal(err)
	}
	if refused, err := restarted.Refused("revoked"); err != nil || refused {
		t.Fatalf("the completed pairing's pin did not restore the device: %v %v", refused, err)
	}
	if err := s.ReserveReadOnly("other", freshPin(t, "x")); err == nil {
		t.Fatal("a reservation merged into another account's file")
	}
}

func TestAFailedReservationWriteLeavesTheRecordAsItWas(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	dir := t.TempDir()
	s := NewPinnedStore(dir)
	if err := s.Pin("account", freshPin(t, "operator")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	if err := s.ReserveReadOnly("account", freshPin(t, "operator")); err == nil {
		t.Fatal("write unexpectedly succeeded")
	}
	if err := s.ReserveReadOnly("account", freshPin(t, "newcomer")); err == nil {
		t.Fatal("write unexpectedly succeeded")
	}
	for _, store := range []*PinnedStore{s, NewPinnedStore(dir)} {
		if got, err := store.ViewerScope("operator"); err != nil || got != ScopeOperate {
			t.Fatalf("failed reservation changed live or stored scope: %q %v", got, err)
		}
		if _, ok, err := store.PublicKeyFor("newcomer"); err != nil || ok {
			t.Fatalf("failed reservation left a row: %v %v", ok, err)
		}
	}
}
