package transcript

import (
	"bytes"
	"errors"
	"io"
	"os"
)

// ErrCursorStale is an `after` cursor this reader will not answer from: it is
// past the record's end, not on a row boundary, or what was appended since is
// more than one incremental page may carry. The caller reads the newest page
// whole instead, which is what it would have done without a cursor.
var ErrCursorStale = errors.New("the transcript cursor no longer meets the record; read the newest page")

// ReadClaudeAfter reads the rows appended at or after the byte cursor `after`,
// oldest first, up to the last complete row. NextAfter is the cursor for the
// next such read.
//
// A full read filters peer messages across the whole page (a delivered message
// removes its queued copy), which an appended slice cannot do against rows it
// did not read. So any peer entry among the new rows refuses the cursor, and
// the page that merges these rows never differs from one read whole.
func ReadClaudeAfter(path string, limit int, after int64) (Page, error) {
	return readAfter(path, limit, after, func(line []byte) []Entry { return claudeEntries(line, false) }, true)
}

// ReadCodexAfter is ReadClaudeAfter for a Codex record.
func ReadCodexAfter(path string, limit int, after int64) (Page, error) {
	return readAfter(path, limit, after, func(line []byte) []Entry {
		if !bytes.Contains(line, []byte("item_completed")) &&
			!bytes.Contains(line, []byte("task_complete")) &&
			!bytes.Contains(line, []byte("tools.update_plan(")) {
			return nil
		}
		return codexEntries(line)
	}, false)
}

func readAfter(path string, limit int, after int64, parse func([]byte) []Entry, claude bool) (Page, error) {
	if after < 0 {
		return Page{}, ErrCursorStale
	}
	f, err := os.Open(path)
	if err != nil {
		return Page{}, recordError(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Page{}, recordError(err)
	}
	size := info.Size()
	if after > size || size-after > ReadBudget {
		return Page{}, ErrCursorStale
	}
	if after > 0 {
		var prev [1]byte
		if _, err := f.ReadAt(prev[:], after-1); err != nil {
			return Page{}, recordError(err)
		}
		if prev[0] != '\n' {
			return Page{}, ErrCursorStale
		}
	}
	buf := make([]byte, size-after)
	if _, err := f.ReadAt(buf, after); err != nil && err != io.EOF {
		return Page{}, recordError(err)
	}
	// Only complete rows: a row still being written is read next time, whole.
	cut := bytes.LastIndexByte(buf, '\n')
	buf = buf[:cut+1]
	entries := []Entry{}
	pos := after
	for len(buf) > 0 {
		n := bytes.IndexByte(buf, '\n')
		line := buf[:n]
		if len(line) > 0 {
			for _, e := range parse(line) {
				if claude && e.Kind == KindPeer {
					return Page{}, ErrCursorStale
				}
				e.Before = pos
				entries = append(entries, e)
			}
			if len(entries) > limit {
				return Page{}, ErrCursorStale
			}
		}
		pos += int64(n) + 1
		buf = buf[n+1:]
	}
	return Page{Entries: entries, Signature: signature(info), NextAfter: pos}, nil
}

// completeEnd is the byte offset just past the last newline at or before
// size: where an `after` read of what is appended next begins. A trailing row
// still being written is not counted. It looks back at most ReadBudget bytes;
// a partial row longer than that answers 0, whose `after` read the budget
// then refuses, so the reader falls back to a whole page.
func completeEnd(r io.ReaderAt, size int64) int64 {
	pos := size
	floor := max(size-ReadBudget, 0)
	for pos > floor {
		step := min(int64(chunk), pos-floor)
		buf := make([]byte, step)
		if _, err := r.ReadAt(buf, pos-step); err != nil && err != io.EOF {
			return 0
		}
		if i := bytes.LastIndexByte(buf, '\n'); i >= 0 {
			return pos - step + int64(i) + 1
		}
		pos -= step
	}
	return 0
}
