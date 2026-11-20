package healthjournal

import (
	"encoding/json"
	"errors"
	"os"
	"sync/atomic"
	"time"
)

// run is the worker: the store's only goroutine. It exits when Close has been
// called and the queue is written (or Close's context ended), and releases
// the directory lock on the way out.
func (s *Store) run() {
	defer close(s.done)
	defer s.lock.Close()
	tick := time.NewTicker(workerTick)
	defer tick.Stop()
	closing := false
	for {
		var reply chan flushResult
		select {
		case <-s.wake:
		case <-tick.C:
		case reply = <-s.flushReq:
		case <-s.quit:
			closing = true
		}
		if s.abort.Load() {
			s.dropQueued()
			s.closeActive(false)
			s.setHealth(StateClosed, "")
			return
		}
		now := s.clock.Now().Mono
		if closing || reply != nil || now >= s.retryAt {
			s.writeQueued()
		}
		if s.unsynced && (closing || reply != nil || now-s.lastSync >= SyncEvery) {
			s.sync()
		}
		if reply != nil {
			var err error
			if h := s.health.Load(); h.state != StateOK {
				err = fail(h.code)
			}
			reply <- flushResult{synced: s.st.syncedThrough.Load(), err: err}
		}
		if now-s.lastPrune >= PruneEvery {
			s.lastPrune = now
			s.prune(false)
		}
		if closing {
			s.closeActive(true)
			s.dropQueued() // anything admitted after the last write attempt
			s.setHealth(StateClosed, "")
			return
		}
	}
}

// dropQueued counts and marks lost whatever is still queued.
func (s *Store) dropQueued() {
	q, _ := s.take()
	if len(q) == 0 {
		return
	}
	s.st.droppedAtClose.Add(uint64(len(q)))
	s.st.lost.Add(uint64(len(q)))
	s.markLost(q[0].ticket, q[len(q)-1].ticket)
}

type pendingFrame struct {
	b      []byte
	ticket uint64 // 0 for a loss marker
	wall   int64
}

func (s *Store) writeQueued() {
	q, refused := s.take()
	if len(q) == 0 && refused == 0 {
		return
	}
	var frames []pendingFrame
	now := s.clock.Now().Wall
	if refused > 0 {
		frames = append(frames, s.lossFrame(refused, LossQueueFull, now))
	}
	if s.lostGap > 0 {
		frames = append(frames, s.lossFrame(s.lostGap, LossWriteFailed, now))
	}
	for _, r := range q {
		frames = append(frames, pendingFrame{b: appendFrame(nil, frameRecord, r.payload), ticket: r.ticket, wall: r.wall})
	}
	if len(q) == 0 {
		// Only refusals to report: the marker waits for the next record
		// rather than writing, or creating a segment, for itself.
		s.qmu.Lock()
		s.refusedGap += refused
		s.qmu.Unlock()
		return
	}
	s.writeFrames(frames, refused)
}

func (s *Store) lossFrame(n uint64, reason string, now time.Time) pendingFrame {
	p, _ := json.Marshal(Loss{Records: n, Reason: reason, Wall: now})
	return pendingFrame{b: appendFrame(nil, frameLoss, p)}
}

// writeFrames writes frames across as many segments as they need. On a
// failure the active segment is abandoned at its last whole frame, a disk
// that is full first sheds the oldest sealed segment and retries once, and
// what still could not be written is counted and marked lost.
func (s *Store) writeFrames(frames []pendingFrame, refused uint64) {
	shed := false
	markersWritten := false
	for len(frames) > 0 {
		if s.active == nil {
			if err := s.openActive(); err != nil {
				if s.retryFull(err, &shed) {
					continue
				}
				s.failFrames(frames, err, refused, markersWritten)
				return
			}
		}
		room := s.segmentBytes - s.active.valid.Load()
		var chunk []byte
		k := 0
		for k < len(frames) && int64(len(chunk)+len(frames[k].b)+sealReserve) <= room {
			chunk = append(chunk, frames[k].b...)
			k++
		}
		if k == 0 {
			s.closeActive(true)
			s.prune(false) // a sealed segment may put the store over its bytes
			continue
		}
		before := s.active.valid.Load()
		err := s.timed(func() error {
			_, err := s.activeFile.Write(chunk)
			return err
		})
		if err != nil {
			s.abandonActive(before)
			if s.retryFull(err, &shed) {
				continue
			}
			s.failFrames(frames, err, refused, markersWritten)
			return
		}
		seg := s.active
		var last uint64
		for _, f := range frames[:k] {
			if f.ticket == 0 {
				continue
			}
			last = f.ticket
			seg.records.Add(1)
			if seg.firstWall.Load() == 0 {
				seg.firstWall.Store(f.wall)
			}
			if f.wall > seg.lastWall.Load() {
				seg.lastWall.Store(f.wall)
			}
			s.st.written.Add(1)
		}
		end := before + int64(len(chunk))
		seg.size.Store(end)
		seg.valid.Store(end) // published after the bytes are written
		if last != 0 {
			s.st.writtenThrough.Store(last)
		}
		s.st.writes.Add(1)
		s.unsynced = true
		markersWritten = true
		s.lostGap = 0
		frames = frames[k:]
	}
	s.backoff, s.retryAt = 0, 0
	if s.health.Load().state != StateOK {
		s.setHealth(StateOK, "")
	}
}

// sealReserve keeps room in every segment for its seal frame.
const sealReserve = frameHeader + 64

// retryFull sheds one sealed segment when err is a full disk and nothing has
// been shed yet in this write; it reports whether to try again.
func (s *Store) retryFull(err error, shed *bool) bool {
	if *shed || classify(err) != CodeDiskFull {
		return false
	}
	*shed = true
	if s.prune(true) {
		s.st.shed.Add(1)
		return true
	}
	return false
}

func (s *Store) failFrames(frames []pendingFrame, err error, refused uint64, markersWritten bool) {
	code := classify(err)
	var first, last, n uint64
	for _, f := range frames {
		if f.ticket == 0 {
			continue
		}
		if first == 0 {
			first = f.ticket
		}
		last = f.ticket
		n++
	}
	s.st.lost.Add(n)
	s.markLost(first, last)
	if !markersWritten {
		// The refusal marker did not reach disk either; carry it to the
		// next try. lostGap was not reset, so it keeps its earlier count.
		s.qmu.Lock()
		s.refusedGap += refused
		s.qmu.Unlock()
	}
	s.lostGap += n
	s.setHealth(stateFor(code), code)
	if s.backoff == 0 {
		s.backoff = backoffFirst
	} else if s.backoff < backoffLast {
		s.backoff *= 2
		if s.backoff > backoffLast {
			s.backoff = backoffLast
		}
	}
	s.retryAt = s.clock.Now().Mono + s.backoff
}

func stateFor(c Code) string {
	switch c {
	case CodeDiskFull:
		return StateDiskFull
	case CodeReadOnly:
		return StateReadOnly
	case CodePermissionDenied:
		return StatePermissionDenied
	}
	return StateIOError
}

// timed runs one system call with the stall clock running.
func (s *Store) timed(f func() error) error {
	s.writeStarted.Store(int64(s.clock.Now().Mono) | 1)
	err := f()
	s.writeStarted.Store(0)
	return err
}

func (s *Store) openActive() error {
	idx := s.nextIndex
	s.nextIndex++ // a name tried once is never reused, even when the try failed
	path := s.dir + string(os.PathSeparator) + nameOf(idx)
	var f segmentFile
	err := s.timed(func() error {
		var err error
		f, err = s.openSeg(path)
		return err
	})
	if err != nil {
		return err
	}
	now := s.clock.Now().Wall
	h, _ := json.Marshal(segmentHeader{Schema: SegmentSchema, Index: idx, BootID: s.bootID, Build: s.build, Created: now})
	hb := appendFrame(nil, frameSegment, h)
	if err := s.timed(func() error { _, err := f.Write(hb); return err }); err != nil {
		f.Close()
		// We created it with O_EXCL a moment ago; it holds no record.
		s.remove(path)
		return err
	}
	info, _ := os.Lstat(path)
	seg := &segment{index: idx, path: path, boot: s.bootID, build: s.build, created: now, info: info}
	seg.valid.Store(int64(len(hb)))
	seg.size.Store(int64(len(hb)))
	s.segMu.Lock()
	s.segs = append(s.segs, seg)
	s.segMu.Unlock()
	s.active, s.activeFile = seg, f
	return nil
}

// abandonActive closes the active segment after a failed write, cutting it
// back to its last whole frame when the file allows; readers never read past
// valid either way.
func (s *Store) abandonActive(valid int64) {
	if s.active == nil {
		return
	}
	if s.activeFile.Truncate(valid) == nil {
		s.active.size.Store(valid)
	}
	s.activeFile.Close()
	s.active.sealed.Store(true)
	s.active, s.activeFile = nil, nil
	s.unsynced = false
}

// closeActive seals the active segment (writes its seal frame and fsyncs)
// when seal is true, and closes it either way.
func (s *Store) closeActive(seal bool) {
	if s.active == nil {
		return
	}
	if seal {
		p, _ := json.Marshal(sealPayload{Records: s.active.records.Load()})
		b := appendFrame(nil, frameSeal, p)
		before := s.active.valid.Load()
		if err := s.timed(func() error { _, err := s.activeFile.Write(b); return err }); err == nil {
			s.active.size.Store(before + int64(len(b)))
			s.active.valid.Store(before + int64(len(b)))
			s.sync()
		} else {
			s.abandonActive(before)
			return
		}
	}
	if s.activeFile != nil {
		s.activeFile.Close()
	}
	if s.active != nil {
		s.active.sealed.Store(true)
	}
	s.active, s.activeFile = nil, nil
	s.unsynced = false
}

func (s *Store) sync() {
	s.lastSync = s.clock.Now().Mono
	if s.active == nil {
		s.unsynced = false
		return
	}
	through := s.st.writtenThrough.Load()
	if err := s.timed(s.activeFile.Sync); err != nil {
		code := classify(err)
		s.setHealth(stateFor(code), code)
		// After a failed fsync the page cache's state is unknown; a fresh
		// segment is the only thing worth appending to.
		s.abandonActive(s.active.valid.Load())
		return
	}
	s.unsynced = false
	s.st.syncs.Add(1)
	if through > s.st.syncedThrough.Load() {
		s.st.syncedThrough.Store(through)
	}
}

// prune deletes this store's own sealed segments, oldest first, while the
// store is over its byte bound, its segment-count bound or (unless shedding)
// past its age. shed deletes exactly one sealed segment to make room on a full
// disk. It reports whether anything was deleted.
func (s *Store) prune(shed bool) bool {
	blocked := false
	defer func() { s.st.pruneBlocked.Store(blocked) }()
	now := s.clock.Now().Wall.UnixNano()
	horizon := now - int64(MaxStoreAgeSeconds)*int64(time.Second)
	deleted := false
	for {
		segs := s.snapshot()
		if len(segs) == 0 {
			return deleted
		}
		var total int64
		for _, g := range segs {
			total += g.size.Load()
		}
		old := segs[0]
		if old == s.active || !old.sealed.Load() {
			return deleted
		}
		var reason *atomic.Uint64
		switch {
		case shed && !deleted:
			reason = &s.st.prunedBytes
		case total > s.storeBytes:
			reason = &s.st.prunedBytes
		case len(segs) > MaxSegments:
			reason = &s.st.prunedCount
		case !shed && old.lastWall.Load() != 0 && old.lastWall.Load() < horizon:
			reason = &s.st.prunedAge
		case !shed && old.lastWall.Load() == 0 && old.created.UnixNano() < horizon:
			reason = &s.st.prunedAge
		default:
			return deleted
		}
		if err := s.removeOwned(old); err != nil {
			s.st.pruneFailures.Add(1)
			blocked = true
			return deleted
		}
		reason.Add(1)
		s.st.prunedRecords.Add(old.records.Load())
		s.segMu.Lock()
		s.segs = s.segs[1:]
		s.segMu.Unlock()
		deleted = true
	}
}

// removeOwned deletes a segment only if the name still holds the very file
// this store scanned or created.
func (s *Store) removeOwned(g *segment) error {
	fi, err := os.Lstat(g.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() || g.info == nil || !os.SameFile(fi, g.info) {
		return errForeign
	}
	return s.remove(g.path)
}
