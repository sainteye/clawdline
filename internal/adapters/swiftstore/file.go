// Package swiftstore reads the Swift app's own store, and only reads it.
//
// The two apps keep separate stores by design, and the person decided on
// 2026-09-17 that this daemon may nevertheless *read* the Swift app's one
// (~/.config/clawdline), because the facts the session list draws — which
// session is Clawdfather, which task opened a tab, who is parked on whom, what
// a session delivered — are recorded only there.
//
// Reading somebody else's live files has three rules, and every function in
// this package keeps them:
//
//   - Nothing is ever written, renamed, created or locked. Files are opened
//     O_RDONLY and nothing else; a `.lock` beside a file is not looked at. The
//     Swift app is the only writer and it is running while this reads.
//   - Secrets are never read into memory. The token files and `secrets/` are
//     not opened, and the decoded records declare only the fields the session
//     list needs, so a task's `secret_hash` is skipped by the decoder rather
//     than carried and filtered.
//   - An unreadable store is "unknown", never "empty". The Swift app rewrites
//     these files while this reads them, so a half-written file is ordinary:
//     it is read again, and if it is still not whole the last good reading is
//     carried and marked stale. With no good reading at all the answer says it
//     does not know, and the caller must not draw that as "nothing is owed".
package swiftstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Reading is one file's decoded contents together with what is known about it.
type Reading[T any] struct {
	// Value is the decoded file. It is the zero value when Known is false.
	Value T
	// Known is false when no reading of this file has ever succeeded.
	Known bool
	// Stale is true when the newest attempt failed and Value is an older
	// reading being carried.
	Stale bool
	// Missing is true when the file does not exist. That is a real answer for
	// an optional file, and a failure for a required one; the caller decides.
	Missing bool
	// ModTime is the file's modification time for the reading in Value.
	ModTime time.Time
	// Err is why the newest attempt failed, if it did.
	Err error
}

// stamp is what decides whether a file changed: size, modification time and
// inode together, because a rename-into-place can keep the first two.
type stamp struct {
	size  int64
	mtime int64
	ino   uint64
}

// file caches one JSON file by its stamp.
//
// A 6.4 MB orchestrator.json takes tens of milliseconds to decode, and the
// session list is built every two seconds per connected page. Decoding only
// when the stamp moves makes that cost proportional to how often the Swift app
// writes, not to how often anybody looks.
type file[T any] struct {
	path string

	mu    sync.Mutex
	have  stamp
	good  Reading[T]
	tried bool
}

// retries is how many times a file that did not decode is read again before
// the last good reading is used instead. The Swift app writes atomically in
// most places, so a torn read is rare; two more reads, a moment apart, cover
// the writers that do not.
const retries = 2

const retryPause = 15 * time.Millisecond

// maxBytes bounds one read. The largest file here is a few megabytes; a file
// ten times that size is not one this package knows how to read quickly, and
// the answer is to say so rather than to allocate whatever it is.
const maxBytes = 64 << 20

func newFile[T any](path string) *file[T] { return &file[T]{path: path} }

// read returns the newest whole reading of the file.
func (f *file[T]) read() Reading[T] {
	f.mu.Lock()
	defer f.mu.Unlock()

	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			time.Sleep(retryPause)
		}
		st, err := statFile(f.path)
		if errors.Is(err, os.ErrNotExist) {
			// A file that is not there has no older reading worth carrying: the
			// Swift app removed it, or never wrote it.
			f.tried = true
			f.have = stamp{}
			f.good = Reading[T]{Known: true, Missing: true}
			return f.good
		}
		if err != nil {
			lastErr = err
			continue
		}
		if f.tried && f.good.Known && st == f.have && f.good.Err == nil {
			return f.good
		}
		var value T
		mod, err := decodeFile(f.path, &value)
		if err != nil {
			lastErr = err
			continue
		}
		// The stamp is taken again after the read: a file replaced while it was
		// being read decoded as whichever version was opened, and caching that
		// under the newer stamp would keep the older contents for good.
		after, err := statFile(f.path)
		if err != nil || after != st {
			lastErr = fmt.Errorf("%s changed while it was being read", f.path)
			continue
		}
		f.tried = true
		f.have = st
		f.good = Reading[T]{Value: value, Known: true, ModTime: mod}
		return f.good
	}

	f.tried = true
	out := f.good
	out.Err = lastErr
	if out.Known && !out.Missing {
		out.Stale = true
		return out
	}
	// Never read, or last seen missing and now unreadable: nothing is known.
	return Reading[T]{Err: lastErr}
}

func statFile(path string) (stamp, error) {
	info, err := os.Stat(path)
	if err != nil {
		return stamp{}, err
	}
	return stamp{size: info.Size(), mtime: info.ModTime().UnixNano(), ino: inode(info)}, nil
}

// decodeFile opens the file read-only and decodes it whole.
func decodeFile(path string, into any) (time.Time, error) {
	fh, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return time.Time{}, err
	}
	defer fh.Close()
	info, err := fh.Stat()
	if err != nil {
		return time.Time{}, err
	}
	if info.Size() > maxBytes {
		return time.Time{}, fmt.Errorf("%s is %d bytes, more than this reader accepts", path, info.Size())
	}
	body, err := io.ReadAll(io.LimitReader(fh, maxBytes+1))
	if err != nil {
		return time.Time{}, err
	}
	if len(body) == 0 {
		return time.Time{}, fmt.Errorf("%s is empty", path)
	}
	if err := json.Unmarshal(body, into); err != nil {
		return time.Time{}, fmt.Errorf("%s did not decode: %w", path, err)
	}
	return info.ModTime(), nil
}
