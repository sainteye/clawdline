package cloud

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

// TestDeviceKeyMatchesThePublishedSeed — CryptoKit's rawRepresentation for an
// Ed25519 private key is the 32-byte seed, and Go's NewKeyFromSeed takes the
// same 32 bytes. This asserts it rather than assuming it: if the two ever
// disagreed, every signature this daemon made would be rejected by a relay that
// had the Swift app's public key on file.
func TestDeviceKeyMatchesThePublishedSeed(t *testing.T) {
	t.Parallel()
	vectors := loadVectors(t)
	key, err := DeviceKeyFromSeed(mustBase64(t, vectors.Ed25519Seed))
	if err != nil {
		t.Fatalf("DeviceKeyFromSeed: %v", err)
	}
	got := base64.StdEncoding.EncodeToString(key.PublicKey())
	if got != vectors.Ed25519PublicKey {
		t.Errorf("public key\n got %s\nwant %s", got, vectors.Ed25519PublicKey)
	}
	if !bytes.Equal(key.Seed(), mustBase64(t, vectors.Ed25519Seed)) {
		t.Error("the seed did not survive being stored and read back")
	}
	if _, err := DeviceKeyFromSeed(make([]byte, 31)); !errors.Is(err, ErrSeedLength) {
		t.Errorf("a short seed: want ErrSeedLength, got %v", err)
	}
	if (DeviceKey{}).Valid() {
		t.Error("the zero device key must not claim to be usable")
	}
}

// TestFingerprintMatchesThePublishedPairingValues — the fingerprint is what two
// people read to each other while pairing, so it has two independent oracles
// here: the machine's and the viewer's, both taken from the pairing vector.
func TestFingerprintMatchesThePublishedPairingValues(t *testing.T) {
	t.Parallel()
	vectors := loadVectors(t)

	var handover struct {
		MachineSigningKey  string `json:"machine_signing_key"`
		MachineFingerprint string `json:"machine_fingerprint"`
	}
	if err := json.Unmarshal([]byte(vectors.PairingHandover.Handover), &handover); err != nil {
		t.Fatalf("reading the handover body: %v", err)
	}
	var offer struct {
		ViewerSigningKey  string `json:"viewer_signing_key"`
		ViewerFingerprint string `json:"viewer_fingerprint"`
	}
	if err := json.Unmarshal([]byte(vectors.PairingHandover.Offer), &offer); err != nil {
		t.Fatalf("reading the offer body: %v", err)
	}

	cases := []struct{ name, key, want string }{
		{"the machine's", handover.MachineSigningKey, handover.MachineFingerprint},
		{"the viewer's", offer.ViewerSigningKey, offer.ViewerFingerprint},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			public, err := PublicKeyFromBytes(mustBase64(t, c.key))
			if err != nil {
				t.Fatalf("PublicKeyFromBytes: %v", err)
			}
			if got := Fingerprint(public); got != c.want {
				t.Errorf("fingerprint\n got %s\nwant %s", got, c.want)
			}
		})
	}
	if Fingerprint(ed25519.PublicKey(make([]byte, 31))) != "" {
		t.Error("a key of the wrong length has no fingerprint, not a different one")
	}
}

// TestRecoveryCode. There is no published vector for this one — the Swift app
// never exported it — so the expected string below was produced by an
// independent Python implementation of the same rule written from
// CloudKeys.swift, and the test states that limitation rather than implying a
// cross-runtime proof it has not got.
func TestRecoveryCode(t *testing.T) {
	t.Parallel()
	vectors := loadVectors(t)
	secret, err := ContentKeyFromBytes(mustBase64(t, vectors.MasterSecret))
	if err != nil {
		t.Fatalf("ContentKeyFromBytes: %v", err)
	}
	const want = "CLAWD1-UCQ2F-I5EUW-TKPKF-JVKV2-ZLNOV-6YLDM-VTWS2-3NN5Y-XG5LX-PF5X2-7R24R-UTI"
	if got := secret.RecoveryCode(); got != want {
		t.Errorf("recovery code\n got %s\nwant %s", got, want)
	}

	back, err := ContentKeyFromRecoveryCode(want)
	if err != nil {
		t.Fatalf("ContentKeyFromRecoveryCode: %v", err)
	}
	if !bytes.Equal(back.Bytes(), secret.Bytes()) {
		t.Error("the secret did not survive the round trip")
	}

	t.Run("a person's typing is forgiven", func(t *testing.T) {
		t.Parallel()
		messy := strings.ToLower(strings.ReplaceAll(want, "-", " "))
		back, err := ContentKeyFromRecoveryCode(messy)
		if err != nil {
			t.Fatalf("lowercase and spaces must still read: %v", err)
		}
		if !bytes.Equal(back.Bytes(), secret.Bytes()) {
			t.Error("the secret changed")
		}
	})
	t.Run("one wrong character is not", func(t *testing.T) {
		t.Parallel()
		broken := strings.Replace(want, "UCQ2F", "UCQ2G", 1)
		if _, err := ContentKeyFromRecoveryCode(broken); !errors.Is(err, ErrRecoveryChecksum) {
			t.Errorf("want ErrRecoveryChecksum, got %v", err)
		}
	})
	t.Run("something else entirely", func(t *testing.T) {
		t.Parallel()
		if _, err := ContentKeyFromRecoveryCode("hello"); !errors.Is(err, ErrRecoveryCode) {
			t.Errorf("want ErrRecoveryCode, got %v", err)
		}
	})
	t.Run("non-zero unused bits are not a second spelling", func(t *testing.T) {
		t.Parallel()
		// The last base32 character of this code carries two unused bits. Any
		// value that sets them decodes to the same 36 bytes for a sloppy
		// decoder and must be refused here.
		broken := strings.TrimSuffix(want, "UTI") + "UTJ"
		if _, err := ContentKeyFromRecoveryCode(broken); err == nil {
			t.Error("a non-canonical base32 tail was accepted")
		}
	})
}

// TestContentKeyGeneration — the generator must consume exactly the bytes it
// was handed, so a caller with a seeded reader gets a reproducible key and a
// short reader gets an error instead of a weak key.
func TestContentKeyGeneration(t *testing.T) {
	t.Parallel()
	source := bytes.Repeat([]byte{7}, ContentKeyBytes)
	key, err := NewContentKey(bytes.NewReader(source))
	if err != nil {
		t.Fatalf("NewContentKey: %v", err)
	}
	if !bytes.Equal(key.Bytes(), source) {
		t.Error("the key is not the bytes it was given")
	}
	if _, err := NewContentKey(bytes.NewReader(source[:31])); err == nil {
		t.Error("a short reader must fail, not produce a short key")
	}
	if _, err := ContentKeyFromBytes(make([]byte, 16)); !errors.Is(err, ErrKeyLength) {
		t.Errorf("want ErrKeyLength, got %v", err)
	}
	if (ContentKey{}).Valid() {
		t.Error("the zero content key must not claim to be usable")
	}
}

// fakeStore is a KeyStore that can be made to fail, which is the only
// interesting thing about a store from the domain's side.
type fakeStore struct {
	device  *DeviceKey
	secret  *ContentKey
	readErr error
	saves   int
}

func (s *fakeStore) DeviceKey() (DeviceKey, bool, error) {
	if s.readErr != nil {
		return DeviceKey{}, false, s.readErr
	}
	if s.device == nil {
		return DeviceKey{}, false, nil
	}
	return *s.device, true, nil
}

func (s *fakeStore) SaveDeviceKey(k DeviceKey) error {
	s.saves++
	s.device = &k
	return nil
}

func (s *fakeStore) MasterSecret() (ContentKey, bool, error) {
	if s.readErr != nil {
		return ContentKey{}, false, s.readErr
	}
	if s.secret == nil {
		return ContentKey{}, false, nil
	}
	return *s.secret, true, nil
}

func (s *fakeStore) SaveMasterSecret(k ContentKey) error {
	s.saves++
	s.secret = &k
	return nil
}

func (s *fakeStore) Forget() error {
	s.device, s.secret = nil, nil
	return nil
}

// TestLoadOrCreateMintsOnceAndNeverOverAFailedRead is the rule that protects a
// paired account: an unreadable store is an error, never an absence, because
// minting over one would orphan every device the user paired.
func TestLoadOrCreateMintsOnceAndNeverOverAFailedRead(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	first, err := LoadOrCreateDeviceKey(store, nil)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := LoadOrCreateDeviceKey(store, nil)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if !bytes.Equal(first.Seed(), second.Seed()) {
		t.Error("the second call minted a new identity")
	}
	if store.saves != 1 {
		t.Errorf("saved %d times, want 1", store.saves)
	}

	broken := &fakeStore{readErr: os.ErrPermission}
	if _, err := LoadOrCreateDeviceKey(broken, nil); !errors.Is(err, os.ErrPermission) {
		t.Errorf("an unreadable store: want the read error, got %v", err)
	}
	if _, err := LoadOrCreateMasterSecret(broken, nil); !errors.Is(err, os.ErrPermission) {
		t.Errorf("an unreadable store: want the read error, got %v", err)
	}
	if broken.saves != 0 {
		t.Error("a failed read must not mint anything")
	}
}
