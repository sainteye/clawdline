// Package skillfile puts this app's skill stub where an assistant finds it,
// takes it out again, and keeps a copy of this binary at a path that does not
// move, so that the stub always has something to run.
//
// The problem it solves: the `clawdline` skill every session loads is a stub
// that points at a reader, and until now that reader lived in the Swift app's
// bundle. When that app stops, the reader goes with it and every session is
// left with a skill that cannot load its guide. The new reader is this binary
// (`clawdline guide`), and the guide is compiled into it, so the two cannot
// drift apart.
//
// The rules, each of which is about a file that belongs to a person rather
// than to this app:
//
//   - **Nothing here runs by itself.** Installing replaces a file in the
//     person's own assistant configuration, so it happens only when they run
//     `clawdline skill install` (design-guidelines DG-10).
//   - **What was there is written down before it is replaced**: a symbolic
//     link by its target, a file by a copy of its bytes, nothing by saying so.
//     `Uninstall` puts exactly that back.
//   - **A link is replaced, never written through.** The stub being replaced is
//     usually a symbolic link into another checkout; writing to its path would
//     overwrite that checkout's file. Every write here is a new file renamed
//     over the path, and a rename replaces the link itself.
//   - **Unknown never authorises a change** (DG-7). A stub somebody edited
//     after it was installed, a record that cannot be read, a backup whose
//     bytes are not the ones recorded: each is refused and left as it is.
package skillfile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// RecordDir is the directory under this app's state directory that holds
	// the install record and the backup of a replaced file.
	RecordDir = "skill"
	// RecordFile is the install record.
	RecordFile = "install.json"
	// BackupFile is the replaced SKILL.md, when it was a file.
	BackupFile = "previous-SKILL.md"

	recordSchema = 1
	// stubLimit is the most of any SKILL.md this package reads. A skill stub
	// is a few kilobytes; a file past this is not one, and is refused rather
	// than copied.
	stubLimit = 1 << 20
	// recordLimit bounds the record read, which is a few hundred bytes.
	recordLimit = 64 << 10
)

// The previous kinds, as the record spells them.
const (
	KindSymlink = "symlink"
	KindFile    = "file"
	KindAbsent  = "absent"
)

var (
	// ErrChanged is an installed stub that is no longer the bytes this
	// package wrote. Somebody edited or replaced it; neither install nor
	// uninstall decides for them.
	ErrChanged = errors.New("SKILL.md is not the stub clawdline installed")
	// ErrNotPlain is a SKILL.md, or a skill directory, that is something
	// other than what this package knows how to put back.
	ErrNotPlain = errors.New("not a plain file, symbolic link or directory")
	// ErrRecordUnreadable is an install record that exists and cannot be
	// used. It is left where it is.
	ErrRecordUnreadable = errors.New("the install record cannot be read")
	// ErrBackupChanged is a backup whose bytes are not the ones recorded.
	ErrBackupChanged = errors.New("the backup of the previous SKILL.md is not the file that was recorded")
	// ErrForeignDir is a state directory that is, or resolves into, the Swift
	// app's.
	ErrForeignDir = errors.New("refusing the Swift app's directory")
)

// StubPath is where Claude Code looks for the clawdline skill under home.
func StubPath(home string) string {
	return filepath.Join(home, ".claude", "skills", "clawdline", "SKILL.md")
}

// Previous is what the install found at the stub's path.
type Previous struct {
	Kind string `json:"kind"`
	// LinkTarget is the symbolic link's target, exactly as the link spelled it.
	LinkTarget string `json:"link_target,omitempty"`
	// SHA256 is the replaced file's bytes, which the backup must still match.
	SHA256 string `json:"sha256,omitempty"`
	// CreatedDir is whether the install made the skill directory, which
	// uninstall then removes when it is empty again.
	CreatedDir bool `json:"created_dir,omitempty"`
}

// Record is the install record.
type Record struct {
	Schema          int      `json:"schema"`
	Path            string   `json:"path"`
	InstalledAt     string   `json:"installed_at"`
	InstalledSHA256 string   `json:"installed_sha256"`
	Previous        Previous `json:"previous"`
}

// Outcome is what an install did.
type Outcome string

const (
	// Installed replaced what was there, after recording it.
	Installed Outcome = "installed"
	// Updated replaced an earlier stub this package installed; the record of
	// what was there before that is kept.
	Updated Outcome = "updated"
	// AlreadyCurrent found these exact bytes in place and wrote nothing.
	AlreadyCurrent Outcome = "already_current"
)

// Installation is the answer to Install.
type Installation struct {
	Outcome    Outcome
	Path       string
	RecordPath string
	Previous   Previous
}

// Removal is the answer to Uninstall.
type Removal struct {
	// Recorded is false when there was no install record: nothing was done.
	Recorded   bool
	Path       string
	RecordPath string
	Restored   Previous
}

// Paths is where one installation lives.
type Paths struct {
	// Home is the person's home directory, under which the stub goes.
	Home string
	// StateDir is this app's state directory, which keeps the record.
	StateDir string
	// Foreign are directories the record must never be written into: the
	// Swift app's settings directory.
	Foreign []string
}

func (p Paths) stub() string      { return StubPath(p.Home) }
func (p Paths) recordDir() string { return filepath.Join(p.StateDir, RecordDir) }
func (p Paths) record() string    { return filepath.Join(p.recordDir(), RecordFile) }
func (p Paths) backup() string    { return filepath.Join(p.recordDir(), BackupFile) }
func (p Paths) skillDir() string  { return filepath.Dir(p.stub()) }
func (p Paths) checkState() error { return RefuseForeign(p.StateDir, p.Foreign...) }

// found is one reading of the stub's path.
type found struct {
	kind   string
	target string
	data   []byte
	sum    string
}

// Install writes stub to the skill's path, recording what was there first.
func Install(p Paths, stub []byte, now time.Time) (Installation, error) {
	out := Installation{Path: p.stub(), RecordPath: p.record()}
	if err := p.checkState(); err != nil {
		return out, err
	}
	if _, err := skillDirReady(p.skillDir(), false); err != nil {
		return out, err
	}
	cur, err := look(p.stub())
	if err != nil {
		return out, err
	}
	want := digest(stub)
	if cur.kind == KindFile && cur.sum == want {
		out.Outcome = AlreadyCurrent
		if rec, ok, err := readRecord(p.record()); err == nil && ok {
			out.Previous = rec.Previous
		}
		return out, nil
	}
	rec, recorded, err := readRecord(p.record())
	if err != nil {
		return out, err
	}
	if recorded {
		// An earlier stub of ours is replaced by this one, and what was there
		// before the first install stays recorded. Anything else at the path
		// is somebody's since then, and is theirs to move.
		if cur.kind != KindFile || cur.sum != rec.InstalledSHA256 {
			return out, fmt.Errorf("%w (found %s at %s, and %s records a different stub); nothing was written. "+
				"Move that file aside, or run `clawdline skill uninstall` once it is the recorded stub again",
				ErrChanged, describe(cur), p.stub(), p.record())
		}
		rec.InstalledSHA256, rec.InstalledAt = want, now.UTC().Format(time.RFC3339)
		if err := writeRecord(p, rec); err != nil {
			return out, err
		}
		if err := replaceWithFile(p.stub(), stub, 0o644); err != nil {
			return out, err
		}
		out.Outcome, out.Previous = Updated, rec.Previous
		return out, nil
	}

	// The first install: make the skill directory if it is missing, record
	// what is there, then replace it.
	createdDir, err := skillDirReady(p.skillDir(), true)
	if err != nil {
		return out, err
	}
	prev := Previous{Kind: cur.kind, LinkTarget: cur.target, SHA256: cur.sum, CreatedDir: createdDir}
	if cur.kind == KindFile {
		if err := os.MkdirAll(p.recordDir(), 0o700); err != nil {
			return out, err
		}
		if err := replaceWithFile(p.backup(), cur.data, 0o600); err != nil {
			return out, err
		}
	}
	rec = Record{Schema: recordSchema, Path: p.stub(), InstalledAt: now.UTC().Format(time.RFC3339),
		InstalledSHA256: want, Previous: prev}
	if err := writeRecord(p, rec); err != nil {
		return out, err
	}
	if err := replaceWithFile(p.stub(), stub, 0o644); err != nil {
		// Nothing was replaced, so the record would describe an install that
		// did not happen, and the next install would refuse over it.
		_ = os.Remove(p.record())
		_ = os.Remove(p.backup())
		return out, err
	}
	out.Outcome, out.Previous = Installed, prev
	return out, nil
}

// Uninstall puts back what Install recorded, and removes the record.
func Uninstall(p Paths) (Removal, error) {
	out := Removal{Path: p.stub(), RecordPath: p.record()}
	if err := p.checkState(); err != nil {
		return out, err
	}
	rec, recorded, err := readRecord(p.record())
	if err != nil || !recorded {
		return out, err
	}
	out.Recorded, out.Restored = true, rec.Previous
	if filepath.Clean(rec.Path) != filepath.Clean(p.stub()) {
		return out, fmt.Errorf("%w: the record at %s is for %s, not %s; nothing was changed",
			ErrRecordUnreadable, p.record(), rec.Path, p.stub())
	}
	if _, err := skillDirReady(p.skillDir(), false); err != nil {
		return out, err
	}
	cur, err := look(p.stub())
	if err != nil {
		return out, err
	}
	// The recorded stub, or nothing at all, may be replaced. Anything else is
	// somebody's since the install.
	if !(cur.kind == KindFile && cur.sum == rec.InstalledSHA256) && cur.kind != KindAbsent {
		return out, fmt.Errorf("%w (found %s at %s); nothing was changed", ErrChanged, describe(cur), p.stub())
	}
	switch rec.Previous.Kind {
	case KindSymlink:
		if rec.Previous.LinkTarget == "" {
			return out, fmt.Errorf("%w: a symbolic link with no target", ErrRecordUnreadable)
		}
		if err := replaceWithLink(p.stub(), rec.Previous.LinkTarget); err != nil {
			return out, err
		}
	case KindFile:
		data, err := readLimited(p.backup(), stubLimit)
		if err != nil {
			return out, fmt.Errorf("the backup at %s cannot be read, so nothing was changed: %w", p.backup(), err)
		}
		if digest(data) != rec.Previous.SHA256 {
			return out, fmt.Errorf("%w (%s); nothing was changed", ErrBackupChanged, p.backup())
		}
		if err := replaceWithFile(p.stub(), data, 0o644); err != nil {
			return out, err
		}
	case KindAbsent:
		if err := os.Remove(p.stub()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return out, err
		}
		if rec.Previous.CreatedDir {
			// Only when it is empty again: whatever else is in it now was
			// put there by somebody else.
			_ = os.Remove(p.skillDir())
		}
	default:
		return out, fmt.Errorf("%w: unknown previous kind %q", ErrRecordUnreadable, rec.Previous.Kind)
	}
	_ = os.Remove(p.backup())
	if err := os.Remove(p.record()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return out, err
	}
	return out, nil
}

// ReadRecord is the install record, and whether there is one.
func ReadRecord(p Paths) (Record, bool, error) { return readRecord(p.record()) }

// skillDirReady checks the skill directory is a real directory and not a link
// into somewhere else, making it when make is set. It answers whether it made
// it.
func skillDirReady(dir string, make bool) (bool, error) {
	info, err := os.Lstat(dir)
	switch {
	case err == nil && info.IsDir():
		return false, nil
	case err == nil:
		// A linked skill directory is somebody's arrangement, and a file
		// written into it lands wherever it points.
		return false, fmt.Errorf("%w: %s is a %s; nothing was written", ErrNotPlain, dir, kindName(info.Mode()))
	case errors.Is(err, os.ErrNotExist):
		if !make {
			return false, nil
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return false, err
		}
		return true, nil
	default:
		return false, err
	}
}

// look reads what is at path without following it.
func look(path string) (found, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return found{kind: KindAbsent}, nil
	}
	if err != nil {
		return found{}, err
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return found{}, err
		}
		return found{kind: KindSymlink, target: target}, nil
	case info.Mode().IsRegular():
		data, err := readLimited(path, stubLimit)
		if err != nil {
			return found{}, err
		}
		return found{kind: KindFile, data: data, sum: digest(data)}, nil
	}
	return found{}, fmt.Errorf("%w: %s is a %s", ErrNotPlain, path, kindName(info.Mode()))
}

func describe(f found) string {
	switch f.kind {
	case KindSymlink:
		return "a symbolic link to " + f.target
	case KindFile:
		return "a file with sha256 " + f.sum
	}
	return "nothing"
}

func kindName(m os.FileMode) string {
	switch {
	case m&os.ModeSymlink != 0:
		return "symbolic link"
	case m.IsDir():
		return "directory"
	case m.IsRegular():
		return "file"
	}
	return "special file"
}

func readRecord(path string) (Record, bool, error) {
	data, err := readLimited(path, recordLimit)
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("%w at %s: %v", ErrRecordUnreadable, path, err)
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil || rec.Schema != recordSchema || rec.Path == "" ||
		rec.InstalledSHA256 == "" {
		return Record{}, false, fmt.Errorf("%w at %s; it was left as it is", ErrRecordUnreadable, path)
	}
	return rec, true, nil
}

func writeRecord(p Paths, rec Record) error {
	if err := os.MkdirAll(p.recordDir(), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return replaceWithFile(p.record(), append(data, '\n'), 0o600)
}

// readLimited reads a plain file, refusing one past limit bytes.
func readLimited(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s is a %s", ErrNotPlain, path, kindName(info.Mode()))
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than %d bytes", path, limit)
	}
	return data, nil
}

// replaceWithFile writes data beside path and renames it over path, which
// replaces a symbolic link rather than writing through it.
func replaceWithFile(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)
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
	if err = tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err = tmp.Write(data); err != nil {
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

// replaceWithLink makes a symbolic link beside path and renames it over path.
func replaceWithLink(path, target string) error {
	name := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+"."+randomSuffix()+".link")
	if err := os.Symlink(target, name); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

func randomSuffix() string {
	return fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// RefuseForeign answers ErrForeignDir when dir is, or resolves into, any of
// foreign, compared as spelled and as resolved.
func RefuseForeign(dir string, foreign ...string) error {
	if dir == "" {
		return errors.New("no state directory")
	}
	for _, f := range foreign {
		if f != "" && (within(dir, f) || within(resolved(dir), resolved(f))) {
			return fmt.Errorf("%w: %s", ErrForeignDir, dir)
		}
	}
	return nil
}

// within reports whether path is dir or inside it, comparing cleaned absolute
// spellings.
func within(path, dir string) bool {
	p, err1 := filepath.Abs(path)
	d, err2 := filepath.Abs(dir)
	if err1 != nil || err2 != nil {
		// A path that cannot be made absolute cannot be shown to be elsewhere.
		return true
	}
	return p == d || strings.HasPrefix(p, d+string(filepath.Separator))
}

// resolved follows symbolic links as far as the path exists.
func resolved(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	rest := ""
	for cur := abs; ; {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}
