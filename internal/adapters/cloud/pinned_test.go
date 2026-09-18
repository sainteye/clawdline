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
