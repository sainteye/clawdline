package logs

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/capacity"
)

func open(t *testing.T) (*Writer, string) {
	t.Helper()
	root := t.TempDir()
	w, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w, root
}

// The row's whole promise: the file becomes a segment at its size, the log
// never holds more than the current file and maxSegments segments, and each
// rotation and deletion is counted where the register reads it.
func TestTheLogRotatesAtItsSizeAndKeepsABoundedTail(t *testing.T) {
	w, root := open(t)
	w.SetLimit(100)
	line := []byte(strings.Repeat("x", 39) + "\n") // 40 bytes
	lines := 3 * (maxSegments + 3)
	for i := 0; i < lines; i++ {
		if _, err := w.Write(line); err != nil {
			t.Fatal(err)
		}
		// Segment names carry the nanosecond; two rotations in one would be
		// refused rather than overwrite, and this test is not about that.
		time.Sleep(time.Millisecond)
	}
	segments, err := w.Segments()
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != maxSegments {
		t.Fatalf("segments kept: %d, want %d: %v", len(segments), maxSegments, segments)
	}
	r := w.Reading()
	if !r.Known || r.Used <= 0 || r.Used > 100+int64(len(line)) {
		t.Fatalf("reading: %+v", r)
	}
	// Lines of 40 bytes at 100 bytes a segment: the file reaches 120 on the
	// third, so every third line from the fourth begins a new segment.
	rotations := int64((lines - 1) / 3)
	if r.Counters.Rotated != rotations || r.Counters.Evicted != rotations-maxSegments || r.Counters.WriteErrors != 0 {
		t.Fatalf("counters: %+v", r.Counters)
	}
	for _, name := range append(segments, File) {
		info, err := os.Stat(filepath.Join(root, DirName, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s is %v, want 0600", name, info.Mode().Perm())
		}
		if info.Size() > 100+int64(len(line)) {
			t.Errorf("%s is %d bytes: a segment runs past its size by more than one write", name, info.Size())
		}
	}
	dir, err := os.Stat(filepath.Join(root, DirName))
	if err != nil || dir.Mode().Perm() != 0o700 {
		t.Fatalf("the log directory: %v %v", dir, err)
	}
}

// Only what this package made is deleted. A file a person left in the log
// directory, however old, is theirs.
func TestPruningDeletesOnlyItsOwnSegments(t *testing.T) {
	w, root := open(t)
	dir := filepath.Join(root, DirName)
	theirs := []string{"daemon.old.log", "notes.txt", "daemon.20200101T000000.000000000Z.log.bak"}
	for _, name := range theirs {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A directory with a segment's name is not a segment either.
	if err := os.Mkdir(filepath.Join(dir, "daemon.20000101T000000.000000000Z.log"), 0o700); err != nil {
		t.Fatal(err)
	}
	w.SetLimit(10)
	for i := 0; i < 2*(maxSegments+2); i++ {
		_, _ = w.Write([]byte("0123456789\n"))
		time.Sleep(time.Millisecond)
	}
	for _, name := range append(theirs, "daemon.20000101T000000.000000000Z.log") {
		if _, err := os.Lstat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s was removed: %v", name, err)
		}
	}
	if segments, _ := w.Segments(); len(segments) != maxSegments {
		t.Fatalf("segments: %v", segments)
	}
}

// A log that is a symlink is refused, at open and at every reopen: the daemon
// writes its own file and nobody else's.
func TestTheLogIsNeverWrittenThroughALink(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, DirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.WriteFile(elsewhere, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(dir, File)); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root); err == nil {
		t.Fatal("a log that is a symlink was opened")
	}
}

// A line that could not reach the file goes to the fallback, and the failure
// is counted rather than lost.
func TestAWriteThatFailsGoesToTheFallbackAndIsCounted(t *testing.T) {
	w, root := open(t)
	var fallback bytes.Buffer
	w.SetFallback(&fallback)
	_ = w.Close()
	// The directory the log lives in is gone, so the reopen fails.
	if err := os.RemoveAll(filepath.Join(root, DirName)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, DirName), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("the line\n"))
	if fallback.String() != "the line\n" {
		t.Fatalf("fallback got %q", fallback.String())
	}
	r := w.Reading()
	if r.Counters.WriteErrors == 0 || r.Note == "" {
		t.Fatalf("the failure was not counted: %+v", r)
	}
}

func TestTheSwiftAppsDirectoryIsRefused(t *testing.T) {
	foreign := t.TempDir()
	if _, err := Open(filepath.Join(foreign, "sub"), foreign); err == nil {
		t.Fatal("a directory inside the Swift app's was opened")
	}
}

// The register row the writer reports against, filled on purpose through the
// writer itself: ok → warn → critical → full, then one rotation (limits §4.7
// layer 1, on the real writer rather than a table).
func TestTheRowWalksToFullAndRotatesOnce(t *testing.T) {
	w, _ := open(t)
	res, problems := capacity.Resolve(capacity.Register(), capacity.LogDaemon+"=200")
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	var row capacity.Resolved
	for _, r := range res {
		if r.Entry.Name == capacity.LogDaemon {
			row = r
		}
	}
	w.SetLimit(row.Limit)
	tracker := capacity.NewTracker()
	var states []capacity.State
	notices := 0
	for i := 0; i < 40; i++ {
		st, events := tracker.Observe(row, w.Reading(), time.Now())
		if n := len(states); n == 0 || states[n-1] != st.State {
			states = append(states, st.State)
		}
		for _, e := range events {
			if e.Kind == capacity.EventNotify {
				notices++
			}
		}
		if st.Reading.Counters.Rotated > 0 {
			break
		}
		_, _ = w.Write([]byte("0123456789\n"))
	}
	got := make([]string, 0, len(states))
	for _, s := range states {
		got = append(got, string(s))
	}
	if strings.Join(got[:4], ",") != "ok,warn,critical,full" || w.Reading().Counters.Rotated != 1 || notices != 1 {
		t.Fatalf("states %v, rotated %d, notices %d", got, w.Reading().Counters.Rotated, notices)
	}
}
