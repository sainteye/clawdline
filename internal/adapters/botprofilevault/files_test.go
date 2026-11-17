package botprofilevault

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncryptedProfileVaultSurvivesReopenAndRefusesLostAuthority(t *testing.T) {
	root := t.TempDir()
	if e := os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	f := New(root)
	id := strings.Repeat("a", 32)
	value := []byte("SYNTHETIC_PROFILE_KEY_ONLY")
	aad := []byte("synthetic-source-agent-profile")
	if e := f.Put(id, aad, value, 16384); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(f.dir, id+".sealed")
	sealed, e := os.ReadFile(path)
	if e != nil || bytes.Contains(sealed, value) {
		t.Fatal("encrypted file exposed plaintext", e)
	}
	again := New(root)
	got, e := again.Get(id, aad, 16384)
	if e != nil || !bytes.Equal(got, value) {
		t.Fatal("reopen could not decrypt", e)
	}
	Wipe(got)
	if _, e = again.Get(id, []byte("wrong-source"), 16384); e != ErrUnavailable {
		t.Fatal("foreign AAD accepted", e)
	}
	sealed[len(sealed)-1] ^= 1
	if os.WriteFile(path, sealed, 0600) != nil {
		t.Fatal("fixture write")
	}
	if _, e = again.Get(id, aad, 16384); e != ErrUnavailable {
		t.Fatal("tampered ciphertext accepted", e)
	}
	key := filepath.Join(f.dir, "profile-aes256-gcm.key")
	if os.Remove(key) != nil {
		t.Fatal("fixture key removal")
	}
	if e = again.Put(id, aad, []byte("REPLACEMENT_SYNTHETIC"), 16384); e != ErrUnavailable {
		t.Fatal("lost key regenerated", e)
	}
	if _, e = os.Stat(key); !os.IsNotExist(e) {
		t.Fatal("lost authority was replaced")
	}
	if e = os.Remove(path); e != nil {
		t.Fatal(e)
	}
	outside := filepath.Join(root, "outside")
	if e = os.WriteFile(outside, []byte("untouched"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(outside, path); e != nil {
		t.Fatal(e)
	}
	if e = again.Remove(id); e != ErrUnavailable {
		t.Fatal("symlink deletion accepted", e)
	}
	raw, _ := os.ReadFile(outside)
	if string(raw) != "untouched" {
		t.Fatal("outside bytes changed")
	}
}
