package transcript

import (
	"bytes"
	"io"
	"os"
)

type claudePageFilter struct {
	entries       []Entry
	deliveredIDs  map[string]bool
	deliveredKeys map[string]bool
	queuedKeys    map[string]bool
}

func newClaudePageFilter() *claudePageFilter {
	return &claudePageFilter{deliveredIDs: map[string]bool{}, deliveredKeys: map[string]bool{}, queuedKeys: map[string]bool{}}
}

func (f *claudePageFilter) add(e Entry) {
	if e.Kind == KindPeer {
		key := e.Source + "\x00" + e.Text
		if e.peerDelivery {
			if e.peerMessageID != "" {
				receipt := e.Source + "\x00" + e.peerMessageID
				if f.deliveredIDs[receipt] {
					return
				}
				f.deliveredIDs[receipt] = true
			} else if f.deliveredKeys[key] {
				return
			}
			f.deliveredKeys[key] = true
			kept := f.entries[:0]
			for _, queued := range f.entries {
				drop := queued.Kind == KindPeer && !queued.peerDelivery &&
					queued.Source+"\x00"+queued.Text == key &&
					(queued.At == 0 || e.At == 0 || queued.At <= e.At)
				if !drop {
					kept = append(kept, queued)
				}
			}
			f.entries = kept
			delete(f.queuedKeys, key)
		} else {
			if f.deliveredKeys[key] || f.queuedKeys[key] {
				return
			}
			f.queuedKeys[key] = true
		}
	}
	f.entries = append(f.entries, e)
}

// ReadClaudeBefore reads a bounded page ending at before. Zero starts at the
// current end of the record. A returned NextBefore is the exclusive byte end
// for the next, older page. Pages end on JSONL row boundaries, so a row that
// yields several visible entries is never split between requests.
func ReadClaudeBefore(path string, limit int, before int64) (Page, error) {
	return readBefore(path, limit, before, func(line []byte) []Entry { return claudeEntries(line, false) }, true)
}

func ReadClaudeAgentBefore(path string, limit int, before int64) (Page, error) {
	return readBefore(path, limit, before, func(line []byte) []Entry { return claudeEntries(line, true) }, true)
}

func ReadCodexBefore(path string, limit int, before int64) (Page, error) {
	return readBefore(path, limit, before, func(line []byte) []Entry {
		if !bytes.Contains(line, []byte("item_completed")) &&
			!bytes.Contains(line, []byte("task_complete")) &&
			!bytes.Contains(line, []byte("tools.update_plan(")) {
			return nil
		}
		return codexEntries(line)
	}, false)
}

func readBefore(path string, limit int, before int64, parse func([]byte) []Entry, claude bool) (Page, error) {
	var last Page
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.Open(path)
		if err != nil {
			return Page{}, recordError(err)
		}
		start, err := f.Stat()
		if err != nil {
			f.Close()
			return Page{}, recordError(err)
		}
		end := start.Size()
		if before > 0 && before < end {
			end = before
		}
		filter := newClaudePageFilter()
		var newest []Entry
		var next int64
		unread := linesBefore(f, end, func(line []byte, lineStart int64) bool {
			entries := parse(line)
			for i := len(entries) - 1; i >= 0; i-- {
				entries[i].Before = lineStart
				if claude {
					filter.add(entries[i])
				} else {
					newest = append(newest, entries[i])
				}
			}
			count := len(newest)
			if claude {
				count = len(filter.entries)
			}
			if count >= limit && len(entries) > 0 {
				next = lineStart
				return false
			}
			return true
		})
		if claude {
			newest = filter.entries
		}
		if next == 0 && unread > 0 {
			next = unread
		}
		page := Page{Entries: oldestFirst(newest, len(newest)), Signature: signature(start), Unread: unread, NextBefore: next,
			NextAfter: completeEnd(f, start.Size())}
		last = page
		f.Close()
		current, err := os.Stat(path)
		if err != nil || signature(current) == page.Signature {
			return page, nil
		}
	}
	return last, nil
}

// linesBefore walks complete JSONL rows backward from an exclusive byte end.
// A window boundary resumes at the end of the row it cut, so the next page
// reads that row whole. The scan never accumulates more than ReadBudget bytes.
func linesBefore(r io.ReaderAt, end int64, body func([]byte, int64) bool) (unread int64) {
	floor := end - ReadBudget
	if floor < 0 {
		floor = 0
	}
	pos := end
	var carry []byte
	for pos > floor {
		step := int64(chunk)
		if pos-floor < step {
			step = pos - floor
		}
		pos -= step
		buf := make([]byte, step, step+int64(len(carry)))
		if _, err := r.ReadAt(buf, pos); err != nil && err != io.EOF {
			return pos + step
		}
		data := append(buf, carry...)
		first := bytes.IndexByte(data, '\n')
		if first < 0 {
			carry = data
			continue
		}
		rest := data[first+1:]
		base := pos + int64(first) + 1
		for len(rest) > 0 {
			cut := bytes.LastIndexByte(rest, '\n')
			line := rest[cut+1:]
			if len(line) > 0 && !body(line, base+int64(cut)+1) {
				return 0
			}
			if cut < 0 {
				break
			}
			rest = rest[:cut]
		}
		carry = data[:first]
	}
	if floor == 0 {
		if len(carry) > 0 {
			body(carry, 0)
		}
		return 0
	}
	// The last row is incomplete at the floor. It is read again in the next
	// window, unless one row alone exceeds the budget; then advance anyway.
	if int64(len(carry)) >= ReadBudget {
		return floor
	}
	return floor + int64(len(carry))
}
