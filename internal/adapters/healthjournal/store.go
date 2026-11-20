// Package healthjournal is the durable, segmented on-disk journal behind the
// connection-health evidence foundation (internal/domain/connectionhealth;
// design docs/admin-connection-health.md "Storage, retention and budgets";
// this package's contract docs/admin-connection-health-storage.md).
//
// The foundation's in-memory ring is lost on restart and says so. This store
// keeps copies of the records that ring already stamped, in its own directory,
// so a later boot can still page through what an earlier one saw. It is built
// so that the store can never slow the thing it observes:
//
//   - Offer never waits. It validates the record, takes the store's own short
//     admission lock and either queues the record or refuses it with a typed
//     code. A refused record is counted and later written as a loss marker.
//   - One worker goroutine, started by Open and joined by Close, owns every
//     write, fsync and deletion. Nothing else touches the active segment.
//   - Readers copy the segment list and open their own file handles; they hold
//     no lock the worker or a producer needs, and only MaxConcurrentReads of
//     them run at once — the next is refused, not queued.
//   - Admission is not durability. Offer's receipt says "admitted to memory";
//     Commitment and Flush say when a ticket was written and when it was
//     fsynced, and a ticket lost to a disk failure says lost.
package healthjournal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
)

// The store's bounds. Each is a capacity row (cloud.health_store_*): the
// plan's proposed starting values (design "Local detail 7 days / 100 MiB;
// 8 MiB segments"), not measured budgets.
const (
	// MaxStoreBytes is every byte of this store's own segments. At the bound
	// the oldest sealed segment is deleted and its records counted as pruned.
	MaxStoreBytes = 100 << 20
	// MaxStoreAgeSeconds is the retention target. A sealed segment whose
	// newest record is older is deleted. Bytes can cut retention shorter;
	// Status and every page carry the actual floor, never the target.
	MaxStoreAgeSeconds = 7 * 24 * 60 * 60
	// MaxSegmentBytes is one segment file; the active segment is sealed and a
	// new one opened before a frame would cross it.
	MaxSegmentBytes = 8 << 20
	// MaxSegments is the number of segment files kept. Every boot opens a new
	// segment, so a daemon restarting in a loop would otherwise leave
	// thousands of tiny files under the byte bound.
	MaxSegments = 256
	// MaxQueueRecords and MaxQueueBytes bound the records admitted to memory
	// and not yet written. A record that would cross either is refused.
	MaxQueueRecords = 1024
	MaxQueueBytes   = 2 << 20
	// MaxFrameBytes is one frame on disk, header included. A record whose
	// encoding would not fit is refused; a frame claiming more is corrupt.
	MaxFrameBytes = 4096
	// MaxConcurrentReads is how many Read calls may run at once.
	MaxConcurrentReads = 4
	// MaxDirEntries is how many directory entries Open looks at. Further
	// entries are not opened, not deleted, and the listing says truncated.
	MaxDirEntries = 4096
	// MaxLostRanges is how many lost ticket ranges Commitment can name. Past
	// it the oldest range is forgotten and tickets in it answer unknown.
	MaxLostRanges = 64
)

// Cadences of the worker. They bound how stale a reading can be, not how
// much is held.
const (
	// SyncEvery is the longest a written record waits for an fsync: the
	// operating-system half of the crash-loss window.
	SyncEvery = 5 * time.Second
	// PruneEvery is how often the worker enforces retention by age.
	PruneEvery = time.Minute
	// StallAfter is how long one write or fsync may run before Status says
	// the store is stalled.
	StallAfter = 5 * time.Second
	workerTick = time.Second
	// A failed write is retried only with the next batch, after a delay that
	// doubles from backoffFirst to backoffLast and stops growing there.
	backoffFirst = time.Second
	backoffLast  = time.Minute
)

// Schemas this package writes. A reader that sees one it does not know
// reports it instead of guessing.
const (
	SegmentSchema = "clawdline.connection-health.segment/1"
	PageSchema    = "clawdline.connection-health.durable-page/1"
	StatusSchema  = "clawdline.connection-health.store-status/1"
)

// Durability statements. Each answer says which of them applies; nothing in
// this package says "durable" about a record that has not reached disk.
const (
	// DurabilityAdmittedMemory is an Offer receipt: the record is in this
	// process's queue. A crash now loses it.
	DurabilityAdmittedMemory = "admitted_memory_lost_on_crash"
	// DurabilityDisk is a page: every entry in it was read back from disk.
	// Records newer than the last fsync survive a process crash but not an
	// operating-system crash or power loss.
	DurabilityDisk = "disk_unsynced_tail_lost_on_os_crash"
)

// CrashLossWindow is the documented loss on a crash, in Status.
const CrashLossWindow = "process crash: records still queued (at most 1024 records / 2 MiB); " +
	"operating-system crash or power loss: additionally records written in the last 5 s before fsync"

// Code is a stable failure a caller can branch on.
type Code string

const (
	CodeInvalidRecord    Code = "invalid_record"
	CodeRecordTooLarge   Code = "record_too_large"
	CodeQueueFull        Code = "queue_full"
	CodeClosed           Code = "closed"
	CodeLocked           Code = "locked"
	CodeUnsafeDirectory  Code = "unsafe_directory"
	CodeInvalidOptions   Code = "invalid_options"
	CodeDiskFull         Code = "disk_full"
	CodeReadOnly         Code = "read_only"
	CodePermissionDenied Code = "permission_denied"
	CodeIOError          Code = "io_error"
	CodeInvalidCursor    Code = "invalid_cursor"
	CodeReadersBusy      Code = "readers_busy"
	CodeQueryBound       Code = "query_bound_exceeded"
	CodeShutdownCanceled Code = "shutdown_canceled"
)

// Error is every failure this package returns. It never carries a path, a
// raw operating-system message or a record's content.
type Error struct {
	Code  Code
	Field string
}

func (e *Error) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("healthjournal: %s: %s", e.Code, e.Field)
	}
	return "healthjournal: " + string(e.Code)
}

// CodeOf is err's code, and "" for an error this package did not make.
func CodeOf(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func fail(c Code) error { return &Error{Code: c} }

// Options opens a store.
type Options struct {
	// Dir is the store's own directory: absolute, clean, not a symlink. It is
	// created 0700 when absent. Nothing but this package writes in it.
	Dir string
	// BootID and Build are the producing boot, as the foundation journal was
	// opened with; every offered record must carry this boot.
	BootID string
	Build  string
	// Clock is where wall time (retention, timestamps) and monotonic time
	// (stall detection) come from; nil is the system clock.
	Clock connectionhealth.Clock

	// Test seams: how a segment is created and a file removed, and lower
	// byte bounds so retention can be driven without writing 100 MiB.
	openSegment  func(path string) (segmentFile, error)
	remove       func(path string) error
	storeBytes   int64
	segmentBytes int64
}

// segmentFile is the active segment as the worker writes it.
type segmentFile interface {
	io.Writer
	Sync() error
	Truncate(size int64) error
	Close() error
}

func createSegment(path string) (segmentFile, error) {
	// O_EXCL: a name that already exists — a leftover, a planted symlink —
	// is never opened for writing.
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_APPEND, 0o600)
}

// Receipt is Offer's answer. It never says durable.
type Receipt struct {
	Ticket     uint64 `json:"ticket"`
	Durability string `json:"durability"`
}

// Commit states of a ticket.
const (
	CommitQueued  = "queued"
	CommitWritten = "written_unsynced"
	CommitSynced  = "synced"
	CommitLost    = "lost"
	CommitUnknown = "unknown"
)

type queued struct {
	ticket  uint64
	payload []byte
	wall    int64
}

type ticketRange struct{ first, last uint64 }

type health struct {
	state string
	code  Code
	at    time.Time
}

// Store states.
const (
	StateOK               = "ok"
	StateDiskFull         = "disk_full"
	StateReadOnly         = "read_only"
	StatePermissionDenied = "permission_denied"
	StateIOError          = "io_error"
	StateStalled          = "stalled"
	StateClosed           = "closed"
)

// Store is one open journal directory.
type Store struct {
	dir, bootID, build string
	clock              connectionhealth.Clock
	openSeg            func(string) (segmentFile, error)
	remove             func(string) error
	lock               io.Closer
	recovery           Recovery
	storeBytes         int64
	segmentBytes       int64

	// Admission: the only lock a producer takes.
	qmu        sync.Mutex
	queue      []queued
	queueBytes int
	admitted   uint64
	refusedGap uint64 // refused since the last loss marker
	closed     bool

	wake     chan struct{}
	quit     chan struct{}
	flushReq chan chan flushResult
	done     chan struct{}
	abort    atomic.Bool

	readers chan struct{}

	segMu sync.RWMutex
	segs  []*segment

	// Owned by the worker goroutine alone.
	nextIndex  uint64
	active     *segment
	activeFile segmentFile
	unsynced   bool
	lastSync   time.Duration
	lastPrune  time.Duration
	backoff    time.Duration
	retryAt    time.Duration
	lostGap    uint64 // lost to write failures since the last loss marker
	lostReason string

	lostMu       sync.Mutex
	lost         []ticketRange
	lostForgot   uint64 // last ticket of the newest forgotten range
	health       atomic.Pointer[health]
	writeStarted atomic.Int64
	st           counters
}

type counters struct {
	admittedThrough, writtenThrough, syncedThrough atomic.Uint64
	queueRows, queueBytes                          atomic.Int64
	refused, invalid, tooLarge                     atomic.Uint64
	written, lost, droppedAtClose                  atomic.Uint64
	prunedAge, prunedBytes, prunedCount            atomic.Uint64
	prunedRecords, pruneFailures, shed             atomic.Uint64
	pruneBlocked                                   atomic.Bool
	syncs, writes                                  atomic.Uint64
}

// Open validates the directory, takes its exclusive lock, recovers every
// segment earlier boots left, and starts the worker. A store whose disk later
// fails keeps running and says so in Status; Open fails only when the
// directory cannot be trusted or is held by another process.
func Open(o Options) (*Store, error) {
	if o.Dir == "" || !filepath.IsAbs(o.Dir) || filepath.Clean(o.Dir) != o.Dir {
		return nil, &Error{Code: CodeInvalidOptions, Field: "dir"}
	}
	if !connectionhealth.ValidID(o.BootID) {
		return nil, &Error{Code: CodeInvalidOptions, Field: "boot_id"}
	}
	if !connectionhealth.ValidID(o.Build) {
		return nil, &Error{Code: CodeInvalidOptions, Field: "build"}
	}
	if o.Clock == nil {
		o.Clock = connectionhealth.NewSystemClock()
	}
	if o.openSegment == nil {
		o.openSegment = createSegment
	}
	if o.remove == nil {
		o.remove = os.Remove
	}
	if o.storeBytes <= 0 || o.storeBytes > MaxStoreBytes {
		o.storeBytes = MaxStoreBytes
	}
	if o.segmentBytes <= 0 || o.segmentBytes > MaxSegmentBytes {
		o.segmentBytes = MaxSegmentBytes
	}
	if err := ensureDir(o.Dir); err != nil {
		return nil, err
	}
	lock, err := lockDir(o.Dir)
	if err != nil {
		return nil, err
	}
	segs, next, rec, err := recoverDir(o.Dir)
	if err != nil {
		lock.Close()
		return nil, err
	}
	now := o.Clock.Now()
	s := &Store{
		dir: o.Dir, bootID: o.BootID, build: o.Build, clock: o.Clock,
		openSeg: o.openSegment, remove: o.remove, lock: lock, recovery: rec,
		storeBytes: o.storeBytes, segmentBytes: o.segmentBytes,
		wake: make(chan struct{}, 1), quit: make(chan struct{}),
		flushReq: make(chan chan flushResult), done: make(chan struct{}),
		readers: make(chan struct{}, MaxConcurrentReads),
		segs:    segs, nextIndex: next,
		lastSync: now.Mono, lastPrune: now.Mono,
	}
	s.health.Store(&health{state: StateOK, at: now.Wall})
	// Retention is enforced before Open returns, so a boot after a long
	// absence never serves, or reports, what it is about to delete. The
	// worker is not running yet; nothing else touches the segments.
	s.prune(false)
	go s.run()
	return s, nil
}

// Offer admits a copy of a record the foundation journal already stamped
// (connectionhealth.Journal.Read returns them). It validates every stamp and
// enum the way a replay does, never waits, and refuses with queue_full when
// the queue is at either bound. The receipt is memory admission only.
func (s *Store) Offer(r connectionhealth.Record) (Receipt, error) {
	if err := validateRecord(r, s.bootID, s.build); err != nil {
		s.st.invalid.Add(1)
		return Receipt{}, err
	}
	b, err := json.Marshal(r)
	if err != nil || len(b) > connectionhealth.MaxRecordBytes || len(b)+frameHeader > MaxFrameBytes {
		s.st.tooLarge.Add(1)
		return Receipt{}, fail(CodeRecordTooLarge)
	}
	s.qmu.Lock()
	if s.closed {
		s.qmu.Unlock()
		return Receipt{}, fail(CodeClosed)
	}
	if len(s.queue) >= MaxQueueRecords || s.queueBytes+len(b) > MaxQueueBytes {
		s.refusedGap++
		s.qmu.Unlock()
		s.st.refused.Add(1)
		return Receipt{}, fail(CodeQueueFull)
	}
	s.admitted++
	t := s.admitted
	s.queue = append(s.queue, queued{ticket: t, payload: b, wall: r.Wall.UnixNano()})
	s.queueBytes += len(b)
	s.st.queueRows.Store(int64(len(s.queue)))
	s.st.queueBytes.Store(int64(s.queueBytes))
	s.st.admittedThrough.Store(t)
	s.qmu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return Receipt{Ticket: t, Durability: DurabilityAdmittedMemory}, nil
}

// take hands the worker everything queued, with the refusals since the last
// marker, in O(1) under the admission lock.
func (s *Store) take() ([]queued, uint64) {
	s.qmu.Lock()
	q, refused := s.queue, s.refusedGap
	s.queue, s.queueBytes, s.refusedGap = nil, 0, 0
	s.st.queueRows.Store(0)
	s.st.queueBytes.Store(0)
	s.qmu.Unlock()
	return q, refused
}

// Commitment says how far a ticket got: queued, written (survives a process
// crash), synced (fsynced), lost, or unknown when the store cannot say.
func (s *Store) Commitment(t uint64) string {
	if t == 0 || t > s.st.admittedThrough.Load() {
		return CommitUnknown
	}
	s.lostMu.Lock()
	for _, r := range s.lost {
		if t >= r.first && t <= r.last {
			s.lostMu.Unlock()
			return CommitLost
		}
	}
	forgot := t <= s.lostForgot
	s.lostMu.Unlock()
	if forgot {
		return CommitUnknown
	}
	switch {
	case t <= s.st.syncedThrough.Load():
		return CommitSynced
	case t <= s.st.writtenThrough.Load():
		return CommitWritten
	}
	return CommitQueued
}

func (s *Store) markLost(first, last uint64) {
	if first == 0 || last < first {
		return
	}
	s.lostMu.Lock()
	defer s.lostMu.Unlock()
	if n := len(s.lost); n > 0 && s.lost[n-1].last+1 == first {
		s.lost[n-1].last = last
		return
	}
	if len(s.lost) == MaxLostRanges {
		s.lostForgot = s.lost[0].last
		s.lost = append(s.lost[:0], s.lost[1:]...)
	}
	s.lost = append(s.lost, ticketRange{first, last})
}

type flushResult struct {
	synced uint64
	err    error
}

// Flush asks the worker to write and fsync everything admitted so far and
// returns the ticket synced through: the explicit durability receipt. It
// waits for at most ctx; a canceled wait does not cancel the worker's write.
func (s *Store) Flush(ctx context.Context) (uint64, error) {
	reply := make(chan flushResult, 1)
	select {
	case s.flushReq <- reply:
	case <-s.done:
		return s.st.syncedThrough.Load(), fail(CodeClosed)
	case <-ctx.Done():
		return s.st.syncedThrough.Load(), ctx.Err()
	}
	select {
	case r := <-reply:
		return r.synced, r.err
	case <-ctx.Done():
		return s.st.syncedThrough.Load(), ctx.Err()
	}
}

// Close stops admission, lets the worker write what is queued, seal the
// active segment and release the directory lock. When ctx ends first, Close
// returns shutdown_canceled; the worker then drops what is still queued
// (counted, and marked lost) as soon as its current system call returns, and
// exits. No goroutine outlives that call.
func (s *Store) Close(ctx context.Context) error {
	s.qmu.Lock()
	if !s.closed {
		s.closed = true
		close(s.quit)
	}
	s.qmu.Unlock()
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		s.abort.Store(true)
		return fail(CodeShutdownCanceled)
	}
}

// Done is closed when the worker has exited and the lock is released.
func (s *Store) Done() <-chan struct{} { return s.done }

func (s *Store) setHealth(state string, code Code) {
	s.health.Store(&health{state: state, code: code, at: s.clock.Now().Wall})
}

func (s *Store) snapshot() []*segment {
	s.segMu.RLock()
	out := append([]*segment(nil), s.segs...)
	s.segMu.RUnlock()
	return out
}
