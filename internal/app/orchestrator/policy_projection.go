package orchestrator

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// The house rules' own copy (docs/design-decisions.md D23 ③, U8).
//
// Until this wave the broker read the Swift app's two files under
// ~/.config/clawdline at every dispatch: the base, which says how work is
// handed out on this machine, and the person's local additions. Cutover B1
// says the new daemon must not need that directory at all, so the base now
// ships in this repository (dispatch-policy.md beside this file, the Swift
// base's text as it was on 2026-09-18, SHA-256 55b2c30f…) and is projected at
// start into this daemon's own directory, where a person can read exactly
// what children are being told. The projection compares SHA-256 and writes
// only when the bytes differ, so an unchanged start writes nothing.
//
// The local file is the person's, and this daemon never writes it (U8). It is
// read from this daemon's directory; while that file does not exist the
// retired Swift app's is read in its place, read-only, so a machine whose
// person has not copied it yet keeps their rules rather than silently losing
// them — and PolicySource says which one a briefing used.
//
// **That fallback outlived the app it was written for, and it is still the
// right answer.** The Swift app was stopped on 2026-09-19; its directory was
// not deleted, so ~/.config/clawdline/dispatch-policy.local.md is on this
// machine and is the only copy of what the person wrote there. Dropping the
// fallback would not migrate those rules, it would drop them out of every
// briefing without a word — the failure the fallback exists to prevent, at
// the moment nobody is left to notice. What retirement did change is that it
// is no longer a transitional read that will end by itself, so it must not be
// silent: ReadPolicy already answers which file it used, and the caller says
// so once in the log with the two paths, so the person can move the file and
// make the fallback stop mattering (transport/http/orchestrator_wiring.go).

//go:embed dispatch-policy.md
var basePolicy []byte

// PolicyBaseFile and PolicyLocalFile are the two names, in either directory.
const (
	PolicyBaseFile  = "dispatch-policy.md"
	PolicyLocalFile = "dispatch-policy.local.md"
)

// ProjectPolicy writes the shipped base into dir when what is there is not
// the same bytes. It answers whether it wrote. A file it cannot read is
// replaced — the base is this daemon's, not a person's — but a directory it
// cannot write is an error, said once by the caller.
func ProjectPolicy(dir string) (bool, error) {
	path := filepath.Join(dir, PolicyBaseFile)
	if held, err := os.ReadFile(path); err == nil && sha256.Sum256(held) == sha256.Sum256(basePolicy) {
		return false, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, basePolicy, 0o644); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	return true, nil
}

// PolicySource says where a composition's two halves came from.
type PolicySource struct {
	// Base is "next" (this daemon's projected copy) or "shipped" (the copy in
	// the binary, when the projected file could not be read).
	Base string
	// Local is "next", "legacy" (the retired Swift app's, read while this
	// daemon has none of its own) or "none".
	Local string
}

// ReadPolicy is the house rules for one dispatch: the projected base, and the
// person's local file from this daemon's directory, or else the retired Swift
// app's. legacyDir empty skips the fallback. The PolicySource it returns is
// not decoration: it is the only way anyone learns that a briefing is carrying
// rules out of a directory this daemon is retiring.
func ReadPolicy(dir, legacyDir string) (base, local string, src PolicySource) {
	if held, err := os.ReadFile(filepath.Join(dir, PolicyBaseFile)); err == nil {
		base, src.Base = string(held), "next"
	} else {
		base, src.Base = string(basePolicy), "shipped"
	}
	src.Local = "none"
	held, err := os.ReadFile(filepath.Join(dir, PolicyLocalFile))
	switch {
	case err == nil:
		return base, string(held), PolicySource{Base: src.Base, Local: "next"}
	case !errors.Is(err, fs.ErrNotExist) || legacyDir == "":
		return base, "", src
	}
	if held, err := os.ReadFile(filepath.Join(legacyDir, PolicyLocalFile)); err == nil {
		local, src.Local = string(held), "legacy"
	}
	return base, local, src
}

// ShippedPolicy is the base as it is compiled in.
func ShippedPolicy() []byte { return bytes.Clone(basePolicy) }
