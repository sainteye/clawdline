package cloudkeys

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/cloud"
)

func syntheticContent(n byte) io.Reader {
	return bytes.NewReader(bytes.Repeat([]byte{n}, cloud.ContentKeyBytes))
}

func TestContentEpochMigrationRotationAndRetry(t *testing.T) {
	s := openStore(t)
	legacy, err := cloud.LoadOrCreateMasterSecret(s, syntheticContent(1))
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.LoadOrCreateContentKey(nil)
	if err != nil || before.Epoch != 1 || before.KeyID != "ms-1" || !bytes.Equal(before.Key.Bytes(), legacy.Bytes()) {
		t.Fatalf("migration: epoch=%d err=%v", before.Epoch, err)
	}
	after, err := s.RotateContentKey("revoke-one", 1, syntheticContent(2))
	if err != nil || after.Epoch != 2 || after.KeyID == before.KeyID || bytes.Equal(after.Key.Bytes(), before.Key.Bytes()) {
		t.Fatalf("rotation: epoch=%d err=%v", after.Epoch, err)
	}
	raw, found, err := s.readSecret(MasterSecretFile, cloud.ContentKeyBytes)
	if err != nil || !found || !bytes.Equal(raw, after.Key.Bytes()) {
		t.Fatal("legacy binary would still load the revoked key")
	}
	if err := s.SaveMasterSecret(before.Key); !errors.Is(err, cloud.ErrContentKeyConflict) {
		t.Fatalf("legacy rollback: %v", err)
	}
	if err := s.SaveMasterSecret(after.Key); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(filepath.Dir(s.Dir()))
	if err != nil {
		t.Fatal(err)
	}
	retry, err := reopened.RotateContentKey("revoke-one", 1, bytes.NewReader(nil))
	if err != nil || retry.Epoch != 2 || !bytes.Equal(retry.Key.Bytes(), after.Key.Bytes()) {
		t.Fatalf("retry minted or failed: %v", err)
	}
	third, err := reopened.RotateContentKey("revoke-two", 2, syntheticContent(3))
	if err != nil || third.Epoch != 3 {
		t.Fatalf("second rotation: %v", err)
	}
	if _, err := reopened.RotateContentKey("revoke-one", 1, nil); !errors.Is(err, cloud.ErrContentKeyConflict) {
		t.Fatalf("stale operation: %v", err)
	}
	for _, name := range []string{contentStateFile, contentFloorFile, MasterSecretFile} {
		info, err := os.Stat(filepath.Join(s.Dir(), name))
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
			t.Fatalf("%s permissions %v", name, info.Mode().Perm())
		}
	}
	if err := reopened.Forget(); err != nil {
		t.Fatal(err)
	}
	if _, found, err := reopened.MasterSecret(); err != nil || found {
		t.Fatalf("forget left content material: %v", err)
	}
}

func TestContentRotationPersistenceFailuresRecoverWithoutMinting(t *testing.T) {
	// Each stage is injected at the actual write/sync/rename boundary. Restart
	// uses disk bytes, not a cached state or an in-memory recovery promise.
	for _, name := range []string{contentStateFile, contentFloorFile, MasterSecretFile} {
		for _, stage := range []string{"write", "file-sync", "rename", "dir-sync"} {
			for _, occurrence := range []int{1, 2} {
				if occurrence == 2 && name != contentStateFile {
					continue
				}
				t.Run(fmt.Sprintf("%s/%s/%d", name, stage, occurrence), func(t *testing.T) {
					s := openStore(t)
					before, err := s.LoadOrCreateContentKey(syntheticContent(1))
					if err != nil {
						t.Fatal(err)
					}
					seen := 0
					injected := errors.New("synthetic disk failure")
					s.fault = func(at string) error {
						if at == name+":"+stage {
							seen++
							if seen == occurrence {
								return injected
							}
						}
						return nil
					}
					state, err := s.RotateContentKey("one", 1, syntheticContent(2))
					if !errors.Is(err, cloud.ErrContentKeyPending) || !errors.Is(err, injected) || state.Key.Valid() {
						t.Fatalf("partial write reported success: %v", err)
					}
					r, err := Open(filepath.Dir(s.Dir()))
					if err != nil {
						t.Fatal(err)
					}
					loaded, loadErr := r.LoadOrCreateContentKey(bytes.NewReader(nil))
					intentAbsent := name == contentStateFile && occurrence == 1 && stage != "dir-sync"
					if intentAbsent {
						if loadErr != nil || loaded.Epoch != before.Epoch || !bytes.Equal(loaded.Key.Bytes(), before.Key.Bytes()) {
							t.Fatalf("before intent old state lost: %v", loadErr)
						}
						_, err = r.RotateContentKey("one", 1, syntheticContent(2))
						if err != nil {
							t.Fatal(err)
						}
					} else {
						committed := name == contentStateFile && occurrence == 2 && stage == "dir-sync"
						if committed {
							if loadErr != nil || loaded.Epoch != 2 {
								t.Fatalf("committed restart: %v", loadErr)
							}
						} else {
							if !errors.Is(loadErr, cloud.ErrContentKeyPending) || loaded.Key.Valid() {
								t.Fatalf("pending fell back: %v", loadErr)
							}
							if _, _, err := r.MasterSecret(); !errors.Is(err, cloud.ErrContentKeyPending) {
								t.Fatalf("legacy reader bypassed pending: %v", err)
							}
							if err := r.SaveMasterSecret(before.Key); !errors.Is(err, cloud.ErrContentKeyPending) {
								t.Fatalf("legacy writer bypassed pending: %v", err)
							}
							if _, err := r.RotateContentKey("different", 1, nil); !errors.Is(err, cloud.ErrContentKeyPending) {
								t.Fatalf("different operation bypassed pending: %v", err)
							}
						}
						// Empty entropy source proves recovery reuses the durable key.
						recovered, err := r.RotateContentKey("one", 1, bytes.NewReader(nil))
						if err != nil || recovered.Epoch != 2 || !bytes.Equal(recovered.Key.Bytes(), bytes.Repeat([]byte{2}, 32)) {
							t.Fatalf("recovery: %v", err)
						}
					}
					current, err := r.RecoverContentKey()
					if err != nil || current.Epoch != 2 {
						t.Fatalf("recovery receipt: %v", err)
					}
					raw, _, err := r.readSecret(MasterSecretFile, 32)
					if err != nil || bytes.Equal(raw, before.Key.Bytes()) {
						t.Fatal("old legacy key survived recovery")
					}
				})
			}
		}
	}
}

func TestContentAuthorityNeverFallsBackAfterLossOrTampering(t *testing.T) {
	for _, damage := range []string{"missing-state", "missing-floor", "rollback", "key", "epoch", "legacy", "symlink", "oversize"} {
		t.Run(damage, func(t *testing.T) {
			s := openStore(t)
			_, err := s.LoadOrCreateContentKey(syntheticContent(1))
			if err != nil {
				t.Fatal(err)
			}
			old, err := os.ReadFile(filepath.Join(s.Dir(), contentStateFile))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.RotateContentKey("one", 1, syntheticContent(2)); err != nil {
				t.Fatal(err)
			}
			switch damage {
			case "missing-state":
				err = os.Remove(filepath.Join(s.Dir(), contentStateFile))
			case "missing-floor":
				err = os.Remove(filepath.Join(s.Dir(), contentFloorFile))
			case "rollback":
				err = os.WriteFile(filepath.Join(s.Dir(), contentStateFile), old, 0o600)
			case "key", "epoch":
				r, _, e := s.readContentRecord()
				if e != nil {
					t.Fatal(e)
				}
				if damage == "key" {
					r.Secret = encodeBase64(bytes.Repeat([]byte{9}, 32))
				} else {
					r.Epoch++
				}
				err = s.writeJSON(contentStateFile, r)
			case "legacy":
				err = s.writeSecret(MasterSecretFile, bytes.Repeat([]byte{1}, 32))
			case "oversize":
				err = os.WriteFile(filepath.Join(s.Dir(), contentStateFile), bytes.Repeat([]byte{'x'}, secretLimit+1), 0o600)
			case "symlink":
				if runtime.GOOS == "windows" {
					t.Skip("symlink privilege")
				}
				if err = os.Remove(filepath.Join(s.Dir(), contentStateFile)); err == nil {
					err = os.Symlink(filepath.Join(s.Dir(), MasterSecretFile), filepath.Join(s.Dir(), contentStateFile))
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if state, err := s.LoadOrCreateContentKey(syntheticContent(8)); !errors.Is(err, cloud.ErrContentKeyState) || state.Key.Valid() {
				t.Fatalf("damaged authority minted or fell back: %v", err)
			}
			if key, err := cloud.LoadOrCreateMasterSecret(s, syntheticContent(8)); !errors.Is(err, cloud.ErrContentKeyState) || key.Valid() {
				t.Fatalf("legacy caller bypassed authority: %v", err)
			}
		})
	}
}

func TestContentMigrationPendingAndEntropyFailure(t *testing.T) {
	s := openStore(t)
	if _, err := s.LoadOrCreateContentKey(bytes.NewReader(nil)); err == nil {
		t.Fatal("entropy failure ignored")
	}
	if _, found, err := s.MasterSecret(); err != nil || found {
		t.Fatalf("entropy failure modified store: %v", err)
	}
	s.fault = func(stage string) error {
		if stage == MasterSecretFile+":rename" {
			return errors.New("disk full")
		}
		return nil
	}
	if _, err := s.LoadOrCreateContentKey(syntheticContent(4)); !errors.Is(err, cloud.ErrContentKeyPending) {
		t.Fatalf("migration failure: %v", err)
	}
	s.fault = nil
	r, err := Open(filepath.Dir(s.Dir()))
	if err != nil {
		t.Fatal(err)
	}
	state, err := r.RecoverContentKey()
	if err != nil || state.Epoch != 1 || !bytes.Equal(state.Key.Bytes(), bytes.Repeat([]byte{4}, 32)) {
		t.Fatalf("migration recovery: %v", err)
	}
	if _, err := r.RotateContentKey("one", 1, bytes.NewReader(nil)); err == nil {
		t.Fatal("rotation entropy failure ignored")
	}
	still, err := r.LoadOrCreateContentKey(nil)
	if err != nil || still.Epoch != 1 {
		t.Fatalf("entropy failure changed epoch: %v", err)
	}
}

func TestContentCommitSyncFailureRemainsPendingUntilDurable(t *testing.T) {
	s := openStore(t)
	if _, err := s.LoadOrCreateContentKey(syntheticContent(1)); err != nil {
		t.Fatal(err)
	}
	seen := 0
	s.fault = func(stage string) error {
		if stage == contentStateFile+":dir-sync" {
			seen++
			if seen == 2 {
				return errors.New("directory sync failure")
			}
		}
		if stage == contentStateFile+":load-sync" && seen == 2 {
			return errors.New("still cannot sync")
		}
		return nil
	}
	if _, err := s.RotateContentKey("one", 1, syntheticContent(2)); !errors.Is(err, cloud.ErrContentKeyPending) {
		t.Fatalf("commit sync: %v", err)
	}
	if _, err := s.RecoverContentKey(); !errors.Is(err, cloud.ErrContentKeyPending) {
		t.Fatalf("unsynced commit acknowledged: %v", err)
	}
	s.fault = nil
	state, err := s.RecoverContentKey()
	if err != nil || state.Epoch != 2 {
		t.Fatalf("local recovery: %v", err)
	}
}

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
