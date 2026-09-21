package cloudkeys

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/cloud"
)

func openStore(t *testing.T) *Files {
	t.Helper()
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return store
}

// TestAnEmptyStoreIsEmptyAndNotBroken — the first start of a daemon that has
// never paired must be able to tell "nothing here yet" from "something is
// wrong", because only the first one may mint.
func TestAnEmptyStoreIsEmptyAndNotBroken(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	if _, found, err := store.DeviceKey(); err != nil || found {
		t.Errorf("device key: found=%v err=%v", found, err)
	}
	if _, found, err := store.MasterSecret(); err != nil || found {
		t.Errorf("master secret: found=%v err=%v", found, err)
	}
}

// TestSecretsSurviveARoundTrip, and the files they live in are owner-only.
func TestSecretsSurviveARoundTrip(t *testing.T) {
	t.Parallel()
	store := openStore(t)

	device, err := cloud.NewDeviceKey(bytes.NewReader(bytes.Repeat([]byte{3}, 32)))
	if err != nil {
		t.Fatalf("NewDeviceKey: %v", err)
	}
	if err := store.SaveDeviceKey(device); err != nil {
		t.Fatalf("SaveDeviceKey: %v", err)
	}
	secret, err := cloud.NewContentKey(bytes.NewReader(bytes.Repeat([]byte{9}, 32)))
	if err != nil {
		t.Fatalf("NewContentKey: %v", err)
	}
	if err := store.SaveMasterSecret(secret); err != nil {
		t.Fatalf("SaveMasterSecret: %v", err)
	}

	// A second store over the same directory, because what matters is what is
	// on disk and not what is in memory.
	reopened, err := Open(filepath.Dir(store.Dir()))
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	readDevice, found, err := reopened.DeviceKey()
	if err != nil || !found {
		t.Fatalf("device key: found=%v err=%v", found, err)
	}
	if !bytes.Equal(readDevice.Seed(), device.Seed()) {
		t.Error("the device key changed on the way to disk")
	}
	readSecret, found, err := reopened.MasterSecret()
	if err != nil || !found {
		t.Fatalf("master secret: found=%v err=%v", found, err)
	}
	if !bytes.Equal(readSecret.Bytes(), secret.Bytes()) {
		t.Error("the master secret changed on the way to disk")
	}

	if runtime.GOOS != "windows" {
		for _, name := range []string{DeviceKeyFile, MasterSecretFile} {
			info, err := os.Stat(filepath.Join(store.Dir(), name))
			if err != nil {
				t.Fatalf("stat %s: %v", name, err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Errorf("%s is %v, want 0600", name, info.Mode().Perm())
			}
		}
		info, err := os.Stat(store.Dir())
		if err != nil {
			t.Fatalf("stat dir: %v", err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Errorf("the directory is %v, want 0700", info.Mode().Perm())
		}
	}
}

// TestAnUnreadableSecretIsNeverAnAbsentOne is the rule that protects a paired
// account. Every one of these files exists; none of them may read as "there is
// no key yet", because the caller above answers that by minting a new identity.
func TestAnUnreadableSecretIsNeverAnAbsentOne(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"truncated":                  "",
		"not base64":                 "not a key at all\n",
		"base64 of the wrong length": "AAAA\n",
		"an unpadded spelling":       "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := openStore(t)
			path := filepath.Join(store.Dir(), DeviceKeyFile)
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatalf("writing the broken file: %v", err)
			}
			_, found, err := store.DeviceKey()
			if found {
				t.Fatal("a broken key file was read as a key")
			}
			if !errors.Is(err, ErrUnreadable) {
				t.Fatalf("want ErrUnreadable, got %v", err)
			}
		})
	}
}

// TestASymlinkIsNotAKeyFile — a link in place of a secret is somebody else's
// file, and tightening or writing through one is how a 0600 promise becomes a
// 0600 promise about the wrong inode.
func TestASymlinkIsNotAKeyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need a privilege on Windows that CI does not have")
	}
	t.Parallel()
	root := t.TempDir()
	store, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	elsewhere := filepath.Join(root, "elsewhere")
	if err := os.WriteFile(elsewhere, []byte("someone else's\n"), 0o644); err != nil {
		t.Fatalf("writing the other file: %v", err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(store.Dir(), DeviceKeyFile)); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if _, _, err := store.DeviceKey(); !errors.Is(err, ErrNotRegular) {
		t.Errorf("reading through a link: want ErrNotRegular, got %v", err)
	}
	key, err := cloud.NewDeviceKey(bytes.NewReader(bytes.Repeat([]byte{1}, 32)))
	if err != nil {
		t.Fatalf("NewDeviceKey: %v", err)
	}
	if err := store.SaveDeviceKey(key); !errors.Is(err, ErrNotRegular) {
		t.Errorf("writing over a link: want ErrNotRegular, got %v", err)
	}
	body, err := os.ReadFile(elsewhere)
	if err != nil || string(body) != "someone else's\n" {
		t.Errorf("the linked-to file was touched: %q %v", body, err)
	}
	// And Open itself stops rather than tightening whatever it points at.
	if _, err := Open(root); !errors.Is(err, ErrNotRegular) {
		t.Errorf("opening over a link: want ErrNotRegular, got %v", err)
	}
}

// TestRefusesTheSwiftAppsDirectory. The two apps keep separate state on
// purpose; sharing a device identity would make two daemons fight over one
// machine's sequence numbers.
func TestRefusesTheSwiftAppsDirectory(t *testing.T) {
	t.Parallel()
	swift := t.TempDir()
	if _, err := Open(swift, swift); !errors.Is(err, ErrForeignDir) {
		t.Errorf("want ErrForeignDir, got %v", err)
	}
	if _, err := Open(filepath.Join(swift, "inside"), swift); !errors.Is(err, ErrForeignDir) {
		t.Errorf("a directory inside it: want ErrForeignDir, got %v", err)
	}
	if _, err := Open(t.TempDir(), swift); err != nil {
		t.Errorf("an unrelated directory must be fine: %v", err)
	}
}

// TestForgetLeavesNothingBehind.
func TestForgetLeavesNothingBehind(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	key, err := cloud.NewDeviceKey(nil)
	if err != nil {
		t.Fatalf("NewDeviceKey: %v", err)
	}
	secret, err := cloud.NewContentKey(nil)
	if err != nil {
		t.Fatalf("NewContentKey: %v", err)
	}
	if err := store.SaveDeviceKey(key); err != nil {
		t.Fatalf("SaveDeviceKey: %v", err)
	}
	if err := store.SaveMasterSecret(secret); err != nil {
		t.Fatalf("SaveMasterSecret: %v", err)
	}
	if err := store.Forget(); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	for _, name := range []string{DeviceKeyFile, MasterSecretFile} {
		if _, err := os.Stat(filepath.Join(store.Dir(), name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s is still there: %v", name, err)
		}
	}
	// Forgetting twice is not an error; a store with nothing in it is the
	// state Forget was asked for.
	if err := store.Forget(); err != nil {
		t.Errorf("a second Forget: %v", err)
	}
}

// TestThroughTheDomainSeam — the point of the interface is that the domain's
// mint-once rule works over a real file store, not only over a fake.
func TestThroughTheDomainSeam(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	first, err := cloud.LoadOrCreateDeviceKey(store, nil)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := cloud.LoadOrCreateDeviceKey(store, nil)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if !bytes.Equal(first.Seed(), second.Seed()) {
		t.Error("the second call minted a new identity")
	}
	secret, err := cloud.LoadOrCreateMasterSecret(store, nil)
	if err != nil {
		t.Fatalf("master secret: %v", err)
	}
	if !secret.Valid() {
		t.Error("the minted master secret is empty")
	}
}
