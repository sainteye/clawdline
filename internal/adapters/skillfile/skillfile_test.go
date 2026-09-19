package skillfile

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

var (
	stubV1 = []byte("---\nname: clawdline\n---\n\nversion one\n")
	stubV2 = []byte("---\nname: clawdline\n---\n\nversion two\n")
	when   = time.Date(2026, 9, 19, 5, 0, 0, 0, time.UTC)
)

func testPaths(t *testing.T) Paths {
	t.Helper()
	root := t.TempDir()
	return Paths{
		Home:     filepath.Join(root, "home"),
		StateDir: filepath.Join(root, "home", ".config", "clawdline-next"),
		Foreign:  []string{filepath.Join(root, "home", ".config", "clawdline")},
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// The stub this replaces is a link into another checkout. The install must
// replace the link and leave the file it pointed at exactly as it was; a
// write through the link would have overwritten that checkout's SKILL.md.
// Uninstall puts the same link back.
func TestAnInstallReplacesALinkAndNeverWritesThroughIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("making a symbolic link needs a privilege on Windows")
	}
	p := testPaths(t)
	other := filepath.Join(filepath.Dir(p.Home), "old-checkout", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(other), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("the old app's stub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(StubPath(p.Home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, StubPath(p.Home)); err != nil {
		t.Fatal(err)
	}

	done, err := Install(p, stubV1, when)
	if err != nil {
		t.Fatal(err)
	}
	if done.Outcome != Installed || done.Previous.Kind != KindSymlink || done.Previous.LinkTarget != other {
		t.Fatalf("install = %+v", done)
	}
	if got := mustRead(t, other); got != "the old app's stub\n" {
		t.Fatalf("the linked file was written through: %q", got)
	}
	info, err := os.Lstat(StubPath(p.Home))
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("the stub is not a plain file now: %v %v", info, err)
	}
	if got := mustRead(t, StubPath(p.Home)); got != string(stubV1) {
		t.Fatalf("stub = %q", got)
	}

	// Again: nothing to do, and nothing written.
	before, _ := os.Stat(StubPath(p.Home))
	again, err := Install(p, stubV1, when.Add(time.Hour))
	if err != nil || again.Outcome != AlreadyCurrent {
		t.Fatalf("second install = %+v %v", again, err)
	}
	after, _ := os.Stat(StubPath(p.Home))
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("an install with nothing to do replaced the file")
	}

	gone, err := Uninstall(p)
	if err != nil || !gone.Recorded {
		t.Fatalf("uninstall = %+v %v", gone, err)
	}
	target, err := os.Readlink(StubPath(p.Home))
	if err != nil || target != other {
		t.Fatalf("the link was not put back: %q %v", target, err)
	}
	if _, err := os.Stat(filepath.Join(p.StateDir, RecordDir, RecordFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the record is still there: %v", err)
	}
	if got := mustRead(t, other); got != "the old app's stub\n" {
		t.Fatalf("the linked file changed: %q", got)
	}
}

// A file is kept as a backup with its digest, and put back byte for byte.
func TestAFileIsBackedUpAndPutBack(t *testing.T) {
	p := testPaths(t)
	if err := os.MkdirAll(filepath.Dir(StubPath(p.Home)), 0o755); err != nil {
		t.Fatal(err)
	}
	hand := "a stub somebody wrote by hand\n"
	if err := os.WriteFile(StubPath(p.Home), []byte(hand), 0o644); err != nil {
		t.Fatal(err)
	}
	done, err := Install(p, stubV1, when)
	if err != nil || done.Previous.Kind != KindFile || done.Previous.SHA256 != digest([]byte(hand)) {
		t.Fatalf("install = %+v %v", done, err)
	}
	if _, err := Uninstall(p); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, StubPath(p.Home)); got != hand {
		t.Fatalf("put back %q", got)
	}
}

// Nothing there: the install makes the directory, and uninstall takes both
// the stub and the directory it made away again.
func TestNothingThereIsPutBackAsNothing(t *testing.T) {
	p := testPaths(t)
	done, err := Install(p, stubV1, when)
	if err != nil || done.Previous.Kind != KindAbsent || !done.Previous.CreatedDir {
		t.Fatalf("install = %+v %v", done, err)
	}
	if _, err := Uninstall(p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Dir(StubPath(p.Home))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the skill directory the install made is still there: %v", err)
	}
	// And a second uninstall has nothing to undo.
	again, err := Uninstall(p)
	if err != nil || again.Recorded {
		t.Fatalf("second uninstall = %+v %v", again, err)
	}
}

// A newer build's stub replaces an older one of ours, and the record of what
// was there before the first install survives it.
func TestANewerStubKeepsTheFirstRecord(t *testing.T) {
	p := testPaths(t)
	if err := os.MkdirAll(filepath.Dir(StubPath(p.Home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(StubPath(p.Home), []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(p, stubV1, when); err != nil {
		t.Fatal(err)
	}
	done, err := Install(p, stubV2, when.Add(time.Hour))
	if err != nil || done.Outcome != Updated || done.Previous.Kind != KindFile {
		t.Fatalf("update = %+v %v", done, err)
	}
	if _, err := Uninstall(p); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, StubPath(p.Home)); got != "original\n" {
		t.Fatalf("put back %q", got)
	}
}

// A stub somebody edited after the install belongs to them: neither a new
// install nor an uninstall touches it.
func TestAnEditedStubIsNobodysToReplace(t *testing.T) {
	p := testPaths(t)
	if _, err := Install(p, stubV1, when); err != nil {
		t.Fatal(err)
	}
	edited := "edited after the install\n"
	if err := os.WriteFile(StubPath(p.Home), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(p, stubV2, when); !errors.Is(err, ErrChanged) {
		t.Fatalf("install over an edit = %v", err)
	}
	if _, err := Uninstall(p); !errors.Is(err, ErrChanged) {
		t.Fatalf("uninstall over an edit = %v", err)
	}
	if got := mustRead(t, StubPath(p.Home)); got != edited {
		t.Fatalf("the edit was replaced: %q", got)
	}
}

// A backup whose bytes are not the recorded ones is not put back.
func TestAChangedBackupIsNotPutBack(t *testing.T) {
	p := testPaths(t)
	if err := os.MkdirAll(filepath.Dir(StubPath(p.Home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(StubPath(p.Home), []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(p, stubV1, when); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.StateDir, RecordDir, BackupFile), []byte("not it\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(p); !errors.Is(err, ErrBackupChanged) {
		t.Fatalf("uninstall = %v", err)
	}
	if got := mustRead(t, StubPath(p.Home)); got != string(stubV1) {
		t.Fatalf("the stub changed: %q", got)
	}
}

// An unreadable record is unknown, and unknown is refused.
func TestAnUnreadableRecordRefusesBoth(t *testing.T) {
	p := testPaths(t)
	if err := os.MkdirAll(filepath.Join(p.StateDir, RecordDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.StateDir, RecordDir, RecordFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(p, stubV1, when); !errors.Is(err, ErrRecordUnreadable) {
		t.Fatalf("install = %v", err)
	}
	if _, err := Uninstall(p); !errors.Is(err, ErrRecordUnreadable) {
		t.Fatalf("uninstall = %v", err)
	}
	if _, err := os.Lstat(StubPath(p.Home)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("something was written: %v", err)
	}
}

// The Swift app's directory is never where the record goes, however it is
// spelled.
func TestTheSwiftAppsDirectoryIsRefused(t *testing.T) {
	p := testPaths(t)
	p.StateDir = filepath.Join(p.Foreign[0], "sub")
	if _, err := Install(p, stubV1, when); !errors.Is(err, ErrForeignDir) {
		t.Fatalf("install = %v", err)
	}
	if _, err := os.Lstat(StubPath(p.Home)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("something was written: %v", err)
	}
	if _, err := ProjectBinary(p.StateDir, os.Args[0], p.Foreign...); !errors.Is(err, ErrForeignDir) {
		t.Fatalf("projection = %v", err)
	}
}

// The stable binary is written when its bytes differ and not otherwise.
func TestTheStableBinaryIsWrittenOnlyWhenItDiffers(t *testing.T) {
	p := testPaths(t)
	self := filepath.Join(t.TempDir(), "clawdline-build")
	if err := os.WriteFile(self, []byte("build one"), 0o755); err != nil {
		t.Fatal(err)
	}
	first, err := ProjectBinary(p.StateDir, self)
	if err != nil || !first.Changed || first.Path != BinaryPath(p.StateDir) {
		t.Fatalf("first = %+v %v", first, err)
	}
	if got := mustRead(t, first.Path); got != "build one" {
		t.Fatalf("copy = %q", got)
	}
	again, err := ProjectBinary(p.StateDir, self)
	if err != nil || again.Changed {
		t.Fatalf("again = %+v %v", again, err)
	}
	if err := os.WriteFile(self, []byte("build two"), 0o755); err != nil {
		t.Fatal(err)
	}
	next, err := ProjectBinary(p.StateDir, self)
	if err != nil || !next.Changed || mustRead(t, next.Path) != "build two" {
		t.Fatalf("next = %+v %v", next, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(next.Path)
		if err != nil || info.Mode().Perm()&0o100 == 0 {
			t.Fatalf("the copy is not executable: %v %v", info, err)
		}
	}
}
