// Package logs keeps the daemon's own log: CLAWDLINE_NEXT_DIR/logs/daemon.log,
// rotated by size, with a bounded number of closed segments kept beside it
// (docs/limits.md N29, the capacity register's `log.daemon` row).
//
// It exists because the log had no home and no end. The daemon wrote to
// stderr, and stderr went wherever whoever started it pointed it — a file in
// /tmp that nothing ever shortened. The Swift app's own log is the warning:
// hundreds of megabytes, readable by anybody, never rotated.
//
// The manners:
//
//   - The directory is 0700 and every file 0600, set on every open rather
//     than trusted from creation.
//   - A file is opened as the file it is, never through a symlink.
//   - At the segment size the file is renamed to a segment, SegmentPrefix + a
//     UTC time + SegmentSuffix, and a new one is begun. A segment runs past the
//     size by at most the one write that reached it; a write is never split.
//   - The daemon keeps maxSegments closed segments and deletes older ones.
//     Only a plain file in this directory whose name is one this package
//     makes is ever deleted: the log is the daemon's, and nothing else here is.
//   - A line that cannot be written to the file goes to the fallback writer
//     (stderr) instead, and the failure is counted. Losing the log's own
//     account of why it could not write would be the worst place to go quiet.
//   - Never the Swift app's directory.
package logs

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/capacity"
)

const (
	// DirName is the subdirectory of the state directory that holds the log.
	DirName = "logs"
	// File is the log being written.
	File = "daemon.log"
	// SegmentPrefix and SegmentSuffix frame a closed segment's name, so that
	// `ls logs/daemon.*` is the whole log, current file included.
	SegmentPrefix = "daemon."
	SegmentSuffix = ".log"

	// maxSegments is how many closed segments are kept beside the current
	// file: the register's 10 MiB × (1 + 4) is limits §4.2's "10 MiB × 5".
	maxSegments = 4
	// segmentTime is the UTC time in a segment's name. Fixed width, so the
	// names sort in the order they were made.
	segmentTime = "20060102T150405.000000000Z"
)

// segmentName is exactly the names rotate makes. Nothing else is deleted.
var segmentName = regexp.MustCompile(`^daemon\.[0-9]{8}T[0-9]{6}\.[0-9]{9}Z\.log$`)

// ErrForeignDir is the answer for a directory that is the Swift app's.
var ErrForeignDir = errors.New("refusing the Swift app's directory")

// ErrNotRegular is the answer for a log that is a symlink, a directory or
// anything but a plain file.
var ErrNotRegular = errors.New("not a plain file")

// Writer is the daemon's log file. One per process per directory.
type Writer struct {
	dir string

	mu       sync.Mutex
	file     *os.File
	size     int64
	limit    int64
	mirror   io.Writer
	fallback io.Writer

	// What the writer has done since this process opened the directory.
	rotations  int64
	deleted    int64
	failures   int64
	lastAction time.Time
	// lastErr is the last failure to write, rotate or delete, nil once the
	// next write succeeds.
	lastErr error
}

// Open prepares root/logs and opens the log in it, refusing root when it is,
// or resolves into, any of foreign.
func Open(root string, foreign ...string) (*Writer, error) {
	if root == "" {
		return nil, errors.New("no state directory")
	}
	for _, f := range foreign {
		if f != "" && (within(root, f) || within(resolved(root), resolved(f))) {
			return nil, fmt.Errorf("%w: %s", ErrForeignDir, root)
		}
	}
	dir := filepath.Join(root, DirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	w := &Writer{dir: dir, fallback: os.Stderr}
	if err := w.reopen(); err != nil {
		return nil, err
	}
	return w, nil
}

// Dir is where the log and its segments are.
func (w *Writer) Dir() string { return w.dir }

// Path is the log being written.
func (w *Writer) Path() string { return filepath.Join(w.dir, File) }

// SetLimit is the capacity override for `log.daemon`: the size at which the
// file becomes a segment. Zero or less is the register's default.
func (w *Writer) SetLimit(n int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.limit = n
}

// SetMirror also writes every line to m, as well as to the file. The daemon
// passes stderr when stderr is a terminal: somebody running it by hand is
// watching, and a redirected stderr would be the unbounded file again.
func (w *Writer) SetMirror(m io.Writer) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.mirror = m
}

// SetFallback is where a write that could not reach the file goes instead.
// The default is stderr.
func (w *Writer) SetFallback(f io.Writer) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.fallback = f
}

func (w *Writer) segmentLimit() int64 {
	if w.limit > 0 {
		return w.limit
	}
	return capacity.Default(capacity.LogDaemon)
}

// Write appends p to the log, rotating first when the file has reached its
// size. It always answers len(p), nil: the standard logger has nobody to tell
// about a failure, so the failure is counted here, reported by Reading, and p
// goes to the fallback instead of nowhere.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil && w.size >= w.segmentLimit() {
		w.rotate()
	}
	if w.file == nil {
		if err := w.reopen(); err != nil {
			w.failed(err)
		}
	}
	written := false
	if w.file != nil {
		n, err := w.file.Write(p)
		w.size += int64(n)
		if err != nil {
			w.failed(err)
		} else {
			written = true
			w.lastErr = nil
		}
	}
	if w.mirror != nil {
		_, _ = w.mirror.Write(p)
	}
	if !written && w.fallback != nil && w.fallback != w.mirror {
		_, _ = w.fallback.Write(p)
	}
	return len(p), nil
}

// Close closes the file. A later Write opens it again.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

// reopen opens the current file for appending, as the file it is. Under mu.
func (w *Writer) reopen() error {
	path := w.Path()
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %s", ErrNotRegular, path)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE|noFollow, 0o600)
	if err != nil {
		if isLinkRefusal(err) {
			return fmt.Errorf("%w: %s became a symlink", ErrNotRegular, path)
		}
		return err
	}
	info, err := f.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("%w: %s", ErrNotRegular, path)
	}
	if err == nil {
		// Every time, not only at creation: a file somebody else made, or
		// made wider, is tightened before anything more is written to it.
		err = f.Chmod(0o600)
	}
	if err != nil {
		_ = f.Close()
		return err
	}
	w.file, w.size = f, info.Size()
	return nil
}

// rotate closes the file, renames it to a new segment, begins a new one and
// deletes the segments past maxSegments. Under mu. A rename that fails leaves
// the file where it was and writing goes on into it: a segment that is too
// long is better than a line that is lost.
func (w *Writer) rotate() {
	now := time.Now().UTC()
	segment := filepath.Join(w.dir, SegmentPrefix+now.Format(segmentTime)+SegmentSuffix)
	if _, err := os.Lstat(segment); !errors.Is(err, os.ErrNotExist) {
		w.failed(fmt.Errorf("segment %s already exists", segment))
		return
	}
	_ = w.file.Close()
	w.file = nil
	if err := os.Rename(w.Path(), segment); err != nil {
		w.failed(err)
		if err := w.reopen(); err != nil {
			w.failed(err)
		}
		return
	}
	w.rotations++
	w.lastAction = now
	if err := w.reopen(); err != nil {
		w.failed(err)
	}
	w.prune()
}

// prune deletes the oldest segments past maxSegments. Only plain files whose
// names are segment names are counted or touched. Under mu.
func (w *Writer) prune() {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		w.failed(err)
		return
	}
	var segments []string
	for _, e := range entries {
		if e.Type().IsRegular() && segmentName.MatchString(e.Name()) {
			segments = append(segments, e.Name())
		}
	}
	sort.Strings(segments)
	for len(segments) > maxSegments {
		if err := os.Remove(filepath.Join(w.dir, segments[0])); err != nil && !errors.Is(err, os.ErrNotExist) {
			w.failed(err)
			return
		}
		w.deleted++
		segments = segments[1:]
	}
}

func (w *Writer) failed(err error) {
	w.failures++
	w.lastErr = err
}

// Reading is the `log.daemon` row: the current file's size, and what the
// writer has done. A stat, nothing read.
func (w *Writer) Reading() capacity.Reading {
	w.mu.Lock()
	defer w.mu.Unlock()
	r := capacity.Reading{Counters: capacity.Counters{
		Rotated: w.rotations, Evicted: w.deleted, WriteErrors: w.failures, LastActionAt: w.lastAction,
	}}
	if w.lastErr != nil {
		r.Note = "the last write, rotation or deletion failed: " + w.lastErr.Error()
	}
	info, err := os.Lstat(w.Path())
	switch {
	case errors.Is(err, os.ErrNotExist):
		r.Known = true
	case err != nil:
		r.Err = err.Error()
	case !info.Mode().IsRegular():
		r.Err = fmt.Sprintf("%s is not a plain file", w.Path())
	default:
		r.Known, r.Used = true, info.Size()
	}
	return r
}

// Segments is the closed segments in the directory, oldest first.
func (w *Writer) Segments() ([]string, error) {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.Type().IsRegular() && segmentName.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
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

// resolved follows symlinks as far as the path exists.
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
