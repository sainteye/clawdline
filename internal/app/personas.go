package app

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sainteye/clawdline/internal/domain/persona"
)

// WritePersonaFiles writes each built-in persona's injected text to
// <nextDir>/personas/<id>.md, which is what a launch names (docs/personas.md).
//
// A file already holding the same bytes is left alone, so a restart rewrites
// nothing; one that differs — a daemon upgraded with a new text — is replaced
// through a temporary file and a rename, so a session starting at that
// moment reads the old text or the new one, never half of either. A session
// already running keeps what it was launched with. The directory is 0700 and
// each file 0600, as everything else under the state directory is.
//
// Files there that are not in the catalog are not removed: nothing reads
// them, and a directory a person may have looked into is not swept.
func WritePersonaFiles(nextDir string) error {
	dir := persona.Dir(nextDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	var errs []error
	for _, p := range persona.All() {
		if err := writeIfChanged(dir, filepath.Join(dir, persona.FileName(p.ID)), []byte(p.Text())); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p.ID, err))
		}
	}
	// The texts are adaptations of MIT-licensed files, and these are copies
	// of them: the notice travels with them.
	if err := writeIfChanged(dir, filepath.Join(dir, persona.LicenseFileName), []byte(persona.License())); err != nil {
		errs = append(errs, fmt.Errorf("licence: %w", err))
	}
	return errors.Join(errs...)
}

func writeIfChanged(dir, path string, body []byte) (err error) {
	if old, rerr := os.ReadFile(path); rerr == nil && bytes.Equal(old, body) {
		if info, serr := os.Stat(path); serr == nil && info.Mode().Perm() == 0o600 {
			return nil
		}
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(name)
		}
	}()
	if err = tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err = tmp.Write(body); err != nil {
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
	return os.Rename(name, path)
}
