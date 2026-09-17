package cloud

// The durable sequence fence.
//
// One number, kept on disk, that no restart may go below. It is the one piece
// of the outbound spool that cannot be allowed to live only in memory.
//
// Why: every viewer keeps a replay window keyed by (sender, seq)
// (docs/cloud-wire.md §6.3). If this machine restarts and starts again at
// sequence 0, every envelope it sends is a sequence the viewer has already
// claimed, and the viewer refuses all of them — silently, because refusing a
// replay is the correct behaviour and nothing about it looks like a fault.
// The symptom is a machine that connects, publishes happily, and appears
// frozen on every screen.
//
// The fence is advanced in **blocks**, as the Swift app's does
// (`CloudDurableStores.swift:588-658`, block size 64): writing the file on
// every single envelope would put an fsync in the path of every snapshot, and
// the cost of a crash inside a block is a handful of skipped sequence numbers,
// which nothing anywhere minds. The rule that matters is only that the number
// on disk is always **ahead** of the number handed out.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// SequenceFenceFile is the file inside the cloud key directory.
const SequenceFenceFile = "outbound-sequence-v1.json"

// fenceBlock is how far ahead of the handed-out sequence the file is kept.
const fenceBlock = 64

// fenceLimit bounds the file: it holds one small object, and anything bigger
// is not this file.
const fenceLimit = 1 << 20

type fenceRecord struct {
	V       int               `json:"v"`
	Senders map[string]uint64 `json:"senders"`
}

// FileFence keeps one ceiling per sender in a 0600 file.
type FileFence struct {
	dir    string
	sender string

	mu      sync.Mutex
	ceiling uint64
	loaded  bool
}

var _ SequenceFence = (*FileFence)(nil)

// NewFileFence returns the fence for one sender — this machine's id. Keying by
// sender rather than by file means a machine that is re-registered under a new
// id starts its own count, which is right: a different sender has a different
// replay window on every viewer.
func NewFileFence(dir, sender string) *FileFence {
	return &FileFence{dir: dir, sender: sender}
}

// Path is where the file is.
func (f *FileFence) Path() string { return filepath.Join(f.dir, SequenceFenceFile) }

// Ceiling is the lowest sequence this fence allows.
func (f *FileFence) Ceiling() (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.loadLocked(); err != nil {
		return 0, err
	}
	return f.ceiling, nil
}

// Reserve promises that no sequence at or below this one is ever handed out
// again. It writes only when the sequence has caught up with the block on
// disk.
func (f *FileFence) Reserve(sequence uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.loadLocked(); err != nil {
		return err
	}
	if sequence < f.ceiling {
		// Already covered by a block written earlier.
		return nil
	}
	next := sequence + fenceBlock
	if err := f.writeLocked(next); err != nil {
		return err
	}
	f.ceiling = next
	return nil
}

func (f *FileFence) loadLocked() error {
	if f.loaded {
		return nil
	}
	data, err := os.ReadFile(f.Path())
	switch {
	case errors.Is(err, os.ErrNotExist):
		f.ceiling, f.loaded = 0, true
		return nil
	case err != nil:
		// An unreadable fence is not an absent one. Starting from zero here
		// is the exact failure the fence exists to prevent.
		return fmt.Errorf("the outbound sequence fence cannot be read: %w", err)
	}
	if len(data) > fenceLimit {
		return errors.New("the outbound sequence fence is implausibly large")
	}
	var record fenceRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return fmt.Errorf("the outbound sequence fence is not readable JSON: %w", err)
	}
	f.ceiling = record.Senders[f.sender]
	f.loaded = true
	return nil
}

func (f *FileFence) writeLocked(ceiling uint64) error {
	if err := os.MkdirAll(f.dir, 0o700); err != nil {
		return err
	}
	record := fenceRecord{V: 1, Senders: map[string]uint64{}}
	if data, err := os.ReadFile(f.Path()); err == nil && len(data) <= fenceLimit {
		// Other senders' ceilings are carried over untouched: this file may
		// hold a previous identity's high-water mark, and overwriting it
		// would un-fence that sender if it ever came back.
		var current fenceRecord
		if json.Unmarshal(data, &current) == nil && current.Senders != nil {
			record.Senders = current.Senders
		}
	}
	record.Senders[f.sender] = ceiling
	body, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return writeFileAtomically(f.dir, f.Path(), body)
}

// MemoryFence is a fence that forgets. It is for tests, and naming it says so
// where a nil would not.
type MemoryFence struct {
	mu      sync.Mutex
	ceiling uint64
}

var _ SequenceFence = (*MemoryFence)(nil)

// Ceiling answers the current ceiling.
func (f *MemoryFence) Ceiling() (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ceiling, nil
}

// Reserve advances the ceiling.
func (f *MemoryFence) Reserve(sequence uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if sequence >= f.ceiling {
		f.ceiling = sequence + fenceBlock
	}
	return nil
}
