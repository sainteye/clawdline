// Package botprofilevault encrypts Bot keys with a purpose-specific owner-only
// file key. This is protected-file storage, not an operating-system keychain.
package botprofilevault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

var ErrUnavailable = errors.New("bot_profile_vault_unavailable")
var opaque = regexp.MustCompile(`^[0-9a-f]{32}$`)

type Files struct {
	dir string
	mu  sync.Mutex
}

// New is inert. The parent directory is source-owned and already checked by
// the registry. No Cloud identity or preexisting credential is read.
func New(parent string) *Files { return &Files{dir: filepath.Join(parent, "key-vault-v1")} }

// NewConnector uses a separate protected key namespace for connector credentials.
// A provider login vault cannot decrypt or overwrite this namespace.
func NewConnector(parent string) *Files {
	return &Files{dir: filepath.Join(parent, "connector-key-vault-v1")}
}

func privateDir(path string) error {
	info, e := os.Lstat(path)
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return ErrUnavailable
	}
	return nil
}

func read(path string, limit int) ([]byte, error) {
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, ErrUnavailable
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, ErrUnavailable
	}
	defer f.Close()
	actual, e := f.Stat()
	if e != nil || !os.SameFile(info, actual) {
		return nil, ErrUnavailable
	}
	raw, e := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if e != nil || len(raw) > limit {
		return nil, ErrUnavailable
	}
	return raw, nil
}

func (f *Files) cipher(create bool) (cipher.AEAD, error) {
	if privateDir(filepath.Dir(f.dir)) != nil {
		return nil, ErrUnavailable
	}
	// Only a newly created namespace can mint its encryption key. If an
	// existing namespace loses its key, all operations refuse without replacing it.
	fresh := false
	if create {
		if e := os.Mkdir(f.dir, 0700); e == nil {
			fresh = true
		} else if !os.IsExist(e) {
			return nil, ErrUnavailable
		}
	}
	if e := privateDir(f.dir); e != nil {
		return nil, e
	}
	path := filepath.Join(f.dir, "profile-aes256-gcm.key")
	if fresh {
		if !create {
			return nil, ErrUnavailable
		}
		key := make([]byte, 32)
		defer Wipe(key)
		if _, e := rand.Read(key); e != nil {
			return nil, ErrUnavailable
		}
		file, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return nil, ErrUnavailable
		}
		_, e = file.Write(key)
		if e == nil {
			e = file.Sync()
		}
		closeErr := file.Close()
		if e != nil || closeErr != nil {
			return nil, ErrUnavailable
		}
		if e = syncDir(f.dir); e != nil {
			return nil, e
		}
	}
	key, e := read(path, 32)
	if e != nil || len(key) != 32 {
		return nil, ErrUnavailable
	}
	defer Wipe(key)
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, ErrUnavailable
	}
	return cipher.NewGCM(block)
}

func syncDir(path string) error {
	d, e := os.Open(path)
	if e != nil {
		return ErrUnavailable
	}
	defer d.Close()
	if d.Sync() != nil {
		return ErrUnavailable
	}
	return nil
}
func Wipe(raw []byte) {
	for i := range raw {
		raw[i] = 0
	}
}

func (f *Files) Put(id string, aad, value []byte, max int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !opaque.MatchString(id) || len(value) == 0 || len(value) > max {
		return ErrUnavailable
	}
	a, e := f.cipher(true)
	if e != nil {
		return e
	}
	nonce := make([]byte, a.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return ErrUnavailable
	}
	sealed := a.Seal(nonce, nonce, value, aad)
	path := filepath.Join(f.dir, id+".sealed")
	if info, e := os.Lstat(path); e == nil && (!info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0) {
		return ErrUnavailable
	} else if e != nil && !os.IsNotExist(e) {
		return ErrUnavailable
	}
	file, e := os.CreateTemp(f.dir, ".pending-")
	if e != nil {
		return ErrUnavailable
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	_, e = file.Write(sealed)
	if e == nil {
		e = file.Sync()
	}
	ce := file.Close()
	if e != nil || ce != nil {
		return ErrUnavailable
	}
	if os.Rename(tmp, path) != nil {
		return ErrUnavailable
	}
	return syncDir(f.dir)
}

func (f *Files) Get(id string, aad []byte, max int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !opaque.MatchString(id) {
		return nil, ErrUnavailable
	}
	a, e := f.cipher(false)
	if e != nil {
		return nil, e
	}
	raw, e := read(filepath.Join(f.dir, id+".sealed"), max+a.NonceSize()+a.Overhead())
	if e != nil || len(raw) < a.NonceSize()+a.Overhead() {
		return nil, ErrUnavailable
	}
	value, e := a.Open(nil, raw[:a.NonceSize()], raw[a.NonceSize():], aad)
	if e != nil {
		return nil, ErrUnavailable
	}
	return value, nil
}

func (f *Files) Remove(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, e := os.Lstat(f.dir); os.IsNotExist(e) {
		return nil
	}
	if !opaque.MatchString(id) || privateDir(f.dir) != nil {
		return ErrUnavailable
	}
	path := filepath.Join(f.dir, id+".sealed")
	info, e := os.Lstat(path)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return ErrUnavailable
	}
	if os.Remove(path) != nil {
		return ErrUnavailable
	}
	return syncDir(f.dir)
}
