package transcript

import (
	"os"
	"sync"
)

// Ledger keeps what has already been counted, so a transcript is read once
// rather than once per request.
//
// A transcript only ever grows, so the next read starts where the last one
// stopped and adds to the totals already held. Without this, showing usage for
// eight sessions on a five-second poll would re-read hundreds of megabytes a
// minute to learn almost nothing.
//
// The file being shorter than the offset means it is not the file that was read
// before — truncated, replaced, or a different session reusing the name — so
// the totals are thrown away and it is counted again from the start. Continuing
// would add a new file's numbers to an old file's.
type Ledger struct {
	mu   sync.Mutex
	seen map[string]*counted
}

type counted struct {
	offset int64
	usage  Usage
}

func NewLedger() *Ledger { return &Ledger{seen: map[string]*counted{}} }

// Claude returns this conversation's usage, reading only what is new.
func (l *Ledger) Claude(path string) (Usage, error) {
	st, err := os.Stat(path)
	if err != nil {
		return Usage{}, err
	}

	l.mu.Lock()
	prev := l.seen[path]
	l.mu.Unlock()

	from := int64(0)
	base := Usage{Assistant: "claude", Path: path}
	if prev != nil && st.Size() >= prev.offset {
		if st.Size() == prev.offset {
			return prev.usage.clone(), nil
		}
		from = prev.offset
		base = prev.usage.clone()
	}

	grown, read, err := readClaudeFrom(path, from, base)
	if err != nil {
		return base, err
	}
	l.mu.Lock()
	l.seen[path] = &counted{offset: read, usage: grown}
	l.mu.Unlock()
	// The caller gets its own copy. Sharing the slice would let a later read
	// change a number somebody is already holding, so two readings taken at
	// different moments would compare equal — a difference that disappears
	// after the fact is the hardest kind to notice.
	return grown.clone(), nil
}

// Codex needs no ledger: its cumulative total is one line at the end of the
// file, and reading backwards finds it without touching the rest.
func (l *Ledger) Codex(path string) (Usage, error) { return ReadCodexUsage(path) }
