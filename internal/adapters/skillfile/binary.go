package skillfile

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// The stable path.
//
// The skill stub has to name something it can run, and the binary itself
// moves: it is inside an app bundle that can be anywhere, or wherever
// `go build` put it. So the daemon keeps a copy of itself at one path under
// its own state directory, `<state dir>/bin/clawdline`, and the stub names
// that. The copy is rewritten only when its bytes differ from the running
// binary's (compared by SHA-256), which is the same rule the dispatch policy's
// projection follows (design-decisions D23 ③): a daemon that starts a hundred
// times with one build writes the file once.
//
// A copy rather than a symbolic link: a link into an app bundle breaks when
// the bundle is moved or replaced, and Windows does not let an ordinary user
// make one.

// BinDir is the directory under the state directory that holds the copy.
const BinDir = "bin"

// BinaryPath is the stable path under stateDir.
func BinaryPath(stateDir string) string {
	name := "clawdline"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(stateDir, BinDir, name)
}

// Projection is what ProjectBinary did.
type Projection struct {
	Path string
	// Changed is whether the file was written. False means it already held
	// these exact bytes.
	Changed bool
	SHA256  string
}

// ProjectBinary makes BinaryPath(stateDir) hold the bytes of the binary at
// self, writing only when they differ.
func ProjectBinary(stateDir, self string, foreign ...string) (Projection, error) {
	out := Projection{Path: BinaryPath(stateDir)}
	if err := RefuseForeign(stateDir, foreign...); err != nil {
		return out, err
	}
	want, err := fileDigest(self)
	if err != nil {
		return out, fmt.Errorf("the running binary at %s cannot be read: %w", self, err)
	}
	out.SHA256 = want
	info, err := os.Lstat(out.Path)
	switch {
	case err == nil && info.Mode().IsRegular():
		have, err := fileDigest(out.Path)
		if err == nil && have == want {
			return out, nil
		}
	case err == nil && info.IsDir():
		return out, fmt.Errorf("%w: %s is a directory", ErrNotPlain, out.Path)
	case err != nil && !errors.Is(err, os.ErrNotExist):
		return out, err
	}
	// Anything else at the path — an older build, a link, a file that cannot
	// be read — is this daemon's own and is replaced by a rename, which never
	// writes through a link.
	if err := os.MkdirAll(filepath.Dir(out.Path), 0o755); err != nil {
		return out, err
	}
	if err := copyOver(self, out.Path); err != nil {
		return out, err
	}
	out.Changed = true
	return out, nil
}

func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// copyOver copies src beside dst and renames it over dst.
func copyOver(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(name)
		}
	}()
	if _, err = io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, dst)
}
