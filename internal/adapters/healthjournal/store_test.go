package healthjournal

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
)

func TestHistorySurvivesARestartIntoAnotherBoot(t *testing.T) {
	dir := t.TempDir()
	clock := newClock(t0)
	a := open(t, Options{Dir: dir, BootID: "boot-a", Build: "build1", Clock: clock})
	pa := newProducer(t, clock, "boot-a", "build1")
	offerAll(t, a, pa.next(t, 30))
	flush(t, a)
	first, err := a.Read(Query{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	cursor := first.Coverage.NextCursor
	closeNow(t, a)

	clock.advance(time.Hour)
	b := open(t, Options{Dir: dir, BootID: "boot-b", Build: "build2", Clock: clock})
	if r := b.Status().Recovery; r.Sealed != 1 || r.Unsealed != 0 || r.Boots != 1 || r.TornBytes != 0 {
		t.Fatalf("recovery after a clean close: %+v", r)
	}
	pb := newProducer(t, clock, "boot-b", "build2")
	offerAll(t, b, pb.next(t, 5))
	flush(t, b)

	// The cursor issued by boot A continues in boot B, without a gap.
	rest, pages := readAll(t, b, cursor, 7)
	if got, want := len(rest), 31-10+6; got != want {
		t.Fatalf("entries after the boot-a cursor: %d, want %d", got, want)
	}
	if rest[0].Record.Seq != 11 || rest[0].BootID != "boot-a" {
		t.Fatalf("first entry after cursor: %+v", rest[0].Record)
	}
	last := rest[len(rest)-1]
	if last.BootID != "boot-b" || last.Record.Seq != 6 {
		t.Fatalf("last entry: %+v", last)
	}
	boots := map[string]string{}
	for _, p := range pages {
		if p.Coverage.Gap != nil || p.Coverage.Quality != connectionhealth.QualityComplete {
			t.Fatalf("a page of an intact store says %+v", p.Coverage)
		}
		for _, bt := range p.Boots {
			boots[bt.BootID] = bt.Build
		}
	}
	if boots["boot-a"] != "build1" || boots["boot-b"] != "build2" {
		t.Fatalf("boots: %v", boots)
	}
	// Boot records carry their build; nothing else does.
	if rest[21].Record.Event != connectionhealth.EventBoot || rest[21].Record.Build != "build2" {
		t.Fatalf("boot-b's first entry: %+v", rest[21].Record)
	}
}

func TestPaginationIsStableAndComplete(t *testing.T) {
	dir := t.TempDir()
	clock := newClock(t0)
	// Small segments so pages cross many segment boundaries.
	s := open(t, Options{Dir: dir, BootID: "boot-a", Build: "b", Clock: clock, segmentBytes: 4096})
	p := newProducer(t, clock, "boot-a", "b")
	offerAll(t, s, p.next(t, 199))
	flush(t, s)
	all, _ := readAll(t, s, "", 200)
	paged, _ := readAll(t, s, "", 7)
	if len(all) != 200 || len(paged) != 200 {
		t.Fatalf("one page %d, paged %d, want 200", len(all), len(paged))
	}
	for i := range all {
		if all[i].Record.Seq != uint64(i+1) || paged[i].Record.Seq != all[i].Record.Seq || paged[i].Cursor != all[i].Cursor {
			t.Fatalf("entry %d differs: %+v vs %+v", i, all[i], paged[i])
		}
	}
	if st := s.Status(); st.Segments < 10 {
		t.Fatalf("expected many segments, got %d", st.Segments)
	}
	// Every entry's cursor resumes right after it.
	again, err := s.Read(Query{Cursor: all[99].Cursor, Limit: 1})
	if err != nil || len(again.Entries) != 1 || again.Entries[0].Record.Seq != 101 {
		t.Fatalf("resume after entry 100: %+v %v", again.Entries, err)
	}
}

func TestReceiptsNeverClaimDurabilityBeforeDisk(t *testing.T) {
	clock := newClock(t0)
	f := &faults{block: make(chan struct{}), entered: make(chan struct{}, 1)}
	s := open(t, Options{Dir: t.TempDir(), BootID: "boot-a", Build: "b", Clock: clock, openSegment: f.opener()})
	p := newProducer(t, clock, "boot-a", "b")
	rc := offerAll(t, s, p.next(t, 1))
	if rc[0].Durability != DurabilityAdmittedMemory {
		t.Fatalf("receipt: %+v", rc[0])
	}
	<-f.entered // the worker is inside its first write
	if got := s.Commitment(rc[1].Ticket); got != CommitQueued {
		t.Fatalf("while the write is blocked: %s", got)
	}
	f.release()
	if n := flush(t, s); n < rc[1].Ticket {
		t.Fatalf("flush synced through %d, want %d", n, rc[1].Ticket)
	}
	if got := s.Commitment(rc[1].Ticket); got != CommitSynced {
		t.Fatalf("after flush: %s", got)
	}
	if got := s.Commitment(999); got != CommitUnknown {
		t.Fatalf("a ticket never issued: %s", got)
	}
}

func TestOfferNeverWaitsOnABlockedDisk(t *testing.T) {
	clock := newClock(t0)
	f := &faults{block: make(chan struct{}), entered: make(chan struct{}, 1)}
	s, err := Open(Options{Dir: t.TempDir(), BootID: "boot-a", Build: "b", Clock: clock, openSegment: f.opener()})
	if err != nil {
		t.Fatal(err)
	}
	p := newProducer(t, clock, "boot-a", "b")
	recs := p.next(t, 1)
	offerAll(t, s, recs)
	<-f.entered
	// The worker is stuck in a write. Offer must keep answering at once,
	// first admitting up to the bound, then refusing.
	recs = p.next(t, MaxQueueRecords+50)
	room := MaxQueueRecords - int(s.Status().QueueRows)
	var refused int
	start := time.Now()
	for _, r := range recs {
		if _, err := s.Offer(r); CodeOf(err) == CodeQueueFull {
			refused++
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("offers took %v with a blocked disk", took)
	}
	want := len(recs) - room
	if refused != want {
		t.Fatalf("refused %d, want %d", refused, want)
	}
	st := s.Status()
	if st.QueueRows != MaxQueueRecords || st.Refused != uint64(want) {
		t.Fatalf("status under pressure: %+v", st)
	}
	clock.advance(StallAfter + time.Second)
	if got := s.Status().State; got != StateStalled {
		t.Fatalf("state with a write stuck past StallAfter: %s", got)
	}
	// Status and Read keep answering while the worker is stuck.
	if _, err := s.Read(Query{}); err != nil {
		t.Fatalf("read during a stalled write: %v", err)
	}
	f.release()
	flush(t, s)
	// The refusals are on disk as one loss marker, in order.
	entries, _ := readAll(t, s, "", 200)
	var losses []Loss
	for _, e := range entries {
		if e.Kind == EntryLoss {
			losses = append(losses, *e.Loss)
		}
	}
	if len(losses) != 1 || losses[0].Records != uint64(want) || losses[0].Reason != LossQueueFull {
		t.Fatalf("loss markers: %+v", losses)
	}
	closeNow(t, s)
}

func TestADiskFailureIsTypedCountedAndRecovers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		state string
		// A full disk sheds the oldest sealed segment before giving up —
		// here the one just abandoned, so nothing is left to read.
		kept int
		shed uint64
	}{
		{"read only", errReadOnly, StateReadOnly, 4, 0},
		{"disk full", errDiskFull, StateDiskFull, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := newClock(t0)
			f := &faults{}
			s := open(t, Options{Dir: t.TempDir(), BootID: "boot-a", Build: "b", Clock: clock, openSegment: f.opener()})
			p := newProducer(t, clock, "boot-a", "b")
			offerAll(t, s, p.next(t, 3))
			flush(t, s)
			f.set(func(f *faults) { f.writeErr = tc.err })
			lost := offerAll(t, s, p.next(t, 4))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err := s.Flush(ctx)
			cancel()
			if want := map[string]Code{StateReadOnly: CodeReadOnly, StateDiskFull: CodeDiskFull}[tc.state]; CodeOf(err) != want {
				t.Fatalf("flush error %v, want %s", err, want)
			}
			st := s.Status()
			if st.State != tc.state || st.Lost != 4 || st.Shed != tc.shed {
				t.Fatalf("status: state %s lost %d shed %d", st.State, st.Lost, st.Shed)
			}
			for _, rc := range lost {
				if got := s.Commitment(rc.Ticket); got != CommitLost {
					t.Fatalf("ticket %d: %s", rc.Ticket, got)
				}
			}
			// Nothing torn is readable: the half-written frame was cut back.
			entries, _ := readAll(t, s, "", 200)
			if len(entries) != tc.kept {
				t.Fatalf("entries after the failure: %d, want %d", len(entries), tc.kept)
			}
			f.set(func(f *faults) { f.writeErr = nil })
			clock.advance(backoffLast)
			offerAll(t, s, p.next(t, 1))
			flush(t, s)
			if st := s.Status(); st.State != StateOK {
				t.Fatalf("after the disk recovered: %s %s", st.State, st.LastError)
			}
			entries, pages := readAll(t, s, "", 200)
			var loss *Loss
			for _, e := range entries {
				if e.Kind == EntryLoss {
					loss = e.Loss
				}
			}
			if loss == nil || loss.Records != 4 || loss.Reason != LossWriteFailed {
				t.Fatalf("loss marker: %+v", loss)
			}
			if pages[0].Coverage.Quality != connectionhealth.QualityPartial || pages[0].Coverage.LostRecords != 4 {
				t.Fatalf("coverage over a loss: %+v", pages[0].Coverage)
			}
		})
	}
}

func TestAFullDiskShedsTheOldestSealedSegment(t *testing.T) {
	dir := t.TempDir()
	clock := newClock(t0)
	a := open(t, Options{Dir: dir, BootID: "boot-a", Build: "b", Clock: clock})
	offerAll(t, a, newProducer(t, clock, "boot-a", "b").next(t, 3))
	closeNow(t, a)

	f := &faults{}
	f.failOpen = errDiskFull
	var once sync.Once
	removed := 0
	b := open(t, Options{Dir: dir, BootID: "boot-b", Build: "b", Clock: clock, openSegment: f.opener(),
		remove: func(p string) error {
			removed++
			once.Do(func() { f.set(func(f *faults) { f.failOpen = nil }) }) // the shed made room
			return os.Remove(p)
		}})
	offerAll(t, b, newProducer(t, clock, "boot-b", "b").next(t, 2))
	flush(t, b)
	st := b.Status()
	if removed != 1 || st.Shed != 1 || st.PrunedBytes != 1 || st.PrunedRecords != 4 || st.State != StateOK || st.Lost != 0 {
		t.Fatalf("after shedding: removed %d, %+v", removed, st)
	}
	p, _ := b.Read(Query{Cursor: "v1.1.0"})
	if p.Coverage.Gap == nil || p.Coverage.Gap.Reason != GapPruned || p.Coverage.PrunedEarly != 1 {
		t.Fatalf("reading the shed segment: %+v", p.Coverage)
	}
}

func TestRetentionByAgeBytesAndCountKeepsAnHonestFloor(t *testing.T) {
	dir := t.TempDir()
	clock := newClock(t0)
	a := open(t, Options{Dir: dir, BootID: "boot-a", Build: "b", Clock: clock})
	offerAll(t, a, newProducer(t, clock, "boot-a", "b").next(t, 5))
	closeNow(t, a)
	clock.advance(24 * time.Hour)
	b := open(t, Options{Dir: dir, BootID: "boot-b", Build: "b", Clock: clock})
	offerAll(t, b, newProducer(t, clock, "boot-b", "b").next(t, 5))
	closeNow(t, b)

	// Just under eight days after boot-a: its segment is past the target;
	// boot-b's, a day younger, is not.
	clock.advance(7*24*time.Hour - time.Hour)
	c := open(t, Options{Dir: dir, BootID: "boot-c", Build: "b", Clock: clock})
	st := c.Status()
	if st.PrunedAge != 1 || st.Segments != 1 || st.PrunedRecords != 6 {
		t.Fatalf("age retention: %+v", st)
	}
	if st.Floor == nil || !st.Floor.Equal(t0.Add(24*time.Hour)) {
		t.Fatalf("floor %v, want boot-b's first record", st.Floor)
	}
	page, _ := c.Read(Query{})
	if page.Coverage.RetentionFloor == nil || !page.Coverage.RetentionFloor.Equal(*st.Floor) || page.Coverage.TargetSeconds != MaxStoreAgeSeconds {
		t.Fatalf("page floor: %+v", page.Coverage)
	}
	closeNow(t, c)

	// Bytes: a store bound far below what is held deletes oldest first and
	// counts it as early, never the active segment.
	d := open(t, Options{Dir: dir, BootID: "boot-d", Build: "b", Clock: clock, storeBytes: 6000, segmentBytes: 2048})
	offerAll(t, d, newProducer(t, clock, "boot-d", "b").next(t, 60))
	flush(t, d)
	closeNow(t, d)
	e := open(t, Options{Dir: dir, BootID: "boot-e", Build: "b", Clock: clock, storeBytes: 6000, segmentBytes: 2048})
	st = e.Status()
	if st.Bytes > 6000 || st.PrunedBytes == 0 {
		t.Fatalf("byte retention: %d bytes held, %d pruned", st.Bytes, st.PrunedBytes)
	}
	page, _ = e.Read(Query{})
	if page.Coverage.PrunedEarly == 0 || page.Coverage.RetentionFloor == nil {
		t.Fatalf("page after early pruning: %+v", page.Coverage)
	}
}

func TestTooManySegmentsArePrunedByCount(t *testing.T) {
	dir := t.TempDir()
	clock := newClock(t0)
	s := open(t, Options{Dir: dir, BootID: "boot-a", Build: "b", Clock: clock})
	s.segMu.Lock()
	for i := 0; i < MaxSegments+3; i++ {
		g := &segment{index: uint64(1000 + i), path: filepath.Join(dir, nameOf(uint64(1000+i))), created: t0}
		os.WriteFile(g.path, []byte("x"), 0o600)
		g.info, _ = os.Lstat(g.path)
		g.sealed.Store(true)
		g.size.Store(1)
		s.segs = append(s.segs, g)
	}
	s.segMu.Unlock()
	closeNow(t, s)
	s.prune(false) // the worker has exited; nothing else touches segs
	if got := len(s.segs); got != MaxSegments || s.st.prunedCount.Load() != 3 {
		t.Fatalf("segments %d, pruned by count %d", got, s.st.prunedCount.Load())
	}
}

func TestTornAndCorruptFramesNeverReachAPage(t *testing.T) {
	dir := t.TempDir()
	clock := newClock(t0)
	a := open(t, Options{Dir: dir, BootID: "boot-a", Build: "b", Clock: clock})
	offerAll(t, a, newProducer(t, clock, "boot-a", "b").next(t, 9))
	closeNow(t, a)
	b := open(t, Options{Dir: dir, BootID: "boot-b", Build: "b", Clock: clock})
	offerAll(t, b, newProducer(t, clock, "boot-b", "b").next(t, 9))
	flush(t, b)
	// Simulate boot-b dying mid-write: no seal, half a frame at the tail.
	segB := filepath.Join(dir, nameOf(2))
	b.abort.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b.Close(ctx)
	<-b.Done()
	raw, _ := os.ReadFile(segB)
	if i := strings.LastIndex(string(raw), `{"records":`); i > 0 {
		raw = raw[:i-frameHeader] // remove the seal frame, if one got written
	}
	torn := appendFrame(nil, frameRecord, []byte(`{"schema":"x"}`))
	raw = append(raw, torn[:len(torn)-4]...)
	os.WriteFile(segB, raw, 0o600)
	// And flip one byte inside boot-a's fifth record.
	segA := filepath.Join(dir, nameOf(1))
	rawA, _ := os.ReadFile(segA)
	k := indexNth(string(rawA), `"producer_seq":5`, 1)
	rawA[k+3] ^= 0x20
	os.WriteFile(segA, rawA, 0o600)

	c := open(t, Options{Dir: dir, BootID: "boot-c", Build: "b", Clock: clock})
	r := c.Status().Recovery
	// Both read as unsealed prefixes: boot-b died before sealing, and
	// boot-a's seal sits after the corrupt frame where no reader goes.
	if r.Unsealed != 2 || r.TornBytes == 0 || r.Segments != 2 {
		t.Fatalf("recovery: %+v", r)
	}
	entries, pages := readAll(t, c, "", 200)
	var seqA, seqB []uint64
	for _, e := range entries {
		if e.BootID == "boot-a" {
			seqA = append(seqA, e.Record.Seq)
		} else {
			seqB = append(seqB, e.Record.Seq)
		}
	}
	if len(seqA) != 4 || len(seqB) != 10 {
		t.Fatalf("boot-a %v, boot-b %v", seqA, seqB)
	}
	if pages[0].Coverage.Gap == nil || pages[0].Coverage.Gap.Reason != GapTorn || pages[0].Coverage.Quality != connectionhealth.QualityPartial {
		t.Fatalf("first page over the corrupt frame: %+v", pages[0].Coverage)
	}
}

func indexNth(s, sub string, n int) int {
	at := -1
	for i := 0; i < n; i++ {
		j := strings.Index(s[at+1:], sub)
		if j < 0 {
			return -1
		}
		at += 1 + j
	}
	return at
}

// writeSegment writes a hand-built segment, the way a forger would.
func writeSegment(t *testing.T, dir string, idx uint64, boot string, payloads ...[]byte) {
	t.Helper()
	h, _ := json.Marshal(segmentHeader{Schema: SegmentSchema, Index: idx, BootID: boot, Build: "b", Created: t0})
	b := appendFrame(nil, frameSegment, h)
	for _, p := range payloads {
		b = appendFrame(b, frameRecord, p)
	}
	if err := os.WriteFile(filepath.Join(dir, nameOf(idx)), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestForgedRecordsAreSkippedNotExported(t *testing.T) {
	dir := t.TempDir()
	clock := newClock(t0)
	good := newProducer(t, clock, "boot-f", "b").next(t, 1)
	enc := func(r connectionhealth.Record) []byte { b, _ := json.Marshal(r); return b }
	pathLoc := good[1]
	pathLoc.Seq, pathLoc.EventID, pathLoc.Location = 3, "boot-f-3", "/Users/someone/secret"
	otherBoot := good[1]
	otherBoot.Seq, otherBoot.EventID, otherBoot.BootID = 4, "boot-x-4", "boot-x"
	forgedBuild := good[1]
	forgedBuild.Seq, forgedBuild.EventID, forgedBuild.Build = 5, "boot-f-5", "evil"
	extra := []byte(strings.Replace(string(enc(good[1])), `"producer_seq":2`, `"producer_seq":6,"token":"Bearer x"`, 1))
	extra = []byte(strings.Replace(string(extra), `"boot-f-2"`, `"boot-f-6"`, 1))
	badEvent := good[1]
	badEvent.Seq, badEvent.EventID, badEvent.Event = 7, "boot-f-7", "rm -rf"
	writeSegment(t, dir, 1, "boot-f", enc(good[0]), enc(good[1]), enc(pathLoc), enc(otherBoot), enc(forgedBuild), extra, enc(badEvent))
	writeSegment(t, dir, 2, "../../etc")                                        // header with a path for a boot: foreign
	os.WriteFile(filepath.Join(dir, nameOf(3)), []byte("not a segment"), 0o600) // foreign
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("keep me"), 0o600)

	s := open(t, Options{Dir: dir, BootID: "boot-g", Build: "b", Clock: clock})
	r := s.Status().Recovery
	if r.InvalidFrames != 5 || r.Foreign != 2 || r.Unrecognized != 1 || r.Segments != 1 {
		t.Fatalf("recovery: %+v", r)
	}
	p, err := s.Read(Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Entries) != 2 || p.Coverage.SkippedInvalid != 5 || p.Coverage.Quality != connectionhealth.QualityPartial {
		t.Fatalf("page: %d entries, %+v", len(p.Entries), p.Coverage)
	}
	out, _ := json.Marshal(p)
	for _, bad := range []string{"/Users", "Bearer", "evil", "rm -rf", "boot-x", dir} {
		if strings.Contains(string(out), bad) {
			t.Fatalf("export contains %q", bad)
		}
	}
	// New segments never reuse a name a foreign file holds, and foreign or
	// unrecognized files survive pruning everything of ours.
	offerAll(t, s, newProducer(t, clock, "boot-g", "b").next(t, 1))
	flush(t, s)
	closeNow(t, s)
	if _, err := os.Stat(filepath.Join(dir, nameOf(4))); err != nil {
		t.Fatalf("the new segment is not seg 4: %v", err)
	}
	clock.advance(30 * 24 * time.Hour)
	s2 := open(t, Options{Dir: dir, BootID: "boot-h", Build: "b", Clock: clock})
	if st := s2.Status(); st.PrunedAge != 2 || st.Segments != 0 {
		t.Fatalf("after a month: %+v", st)
	}
	for _, keep := range []string{nameOf(2), nameOf(3), "notes.txt"} {
		if _, err := os.Stat(filepath.Join(dir, keep)); err != nil {
			t.Fatalf("%s was not preserved: %v", keep, err)
		}
	}
	// With nothing retained the floor is unknown, not now.
	if p, _ := s2.Read(Query{}); p.Coverage.RetentionFloor != nil || s2.Status().Floor != nil {
		t.Fatalf("floor of an empty store: %v", p.Coverage.RetentionFloor)
	}
}

func TestOfferRefusesWhatAReplayWouldRefuse(t *testing.T) {
	clock := newClock(t0)
	s := open(t, Options{Dir: t.TempDir(), BootID: "boot-a", Build: "b", Clock: clock})
	other := newProducer(t, clock, "boot-z", "b").next(t, 1)
	if _, err := s.Offer(other[1]); CodeOf(err) != CodeInvalidRecord || !strings.Contains(err.Error(), "boot_id") {
		t.Fatalf("another boot's record: %v", err)
	}
	if _, err := s.Offer(connectionhealth.Record{Event: connectionhealth.EventRecovered}); CodeOf(err) != CodeInvalidRecord {
		t.Fatalf("an unstamped record: %v", err)
	}
	mine := newProducer(t, clock, "boot-a", "b").next(t, 1)
	bad := mine[1]
	bad.Location = "C:\\Users\\x"
	if _, err := s.Offer(bad); CodeOf(err) != CodeInvalidRecord || strings.Contains(err.Error(), "Users") {
		t.Fatalf("a path in a location: %v", err)
	}
	if st := s.Status(); st.Invalid != 3 || st.Admitted != 0 {
		t.Fatalf("status: %+v", st)
	}
}

func TestReadsAreBoundedAndNeverBlockTheProducer(t *testing.T) {
	clock := newClock(t0)
	s := open(t, Options{Dir: t.TempDir(), BootID: "boot-a", Build: "b", Clock: clock, segmentBytes: 8192})
	p := newProducer(t, clock, "boot-a", "b")
	offerAll(t, s, p.next(t, 300))
	flush(t, s)
	// Every reader slot taken: the next Read is refused, not queued.
	for i := 0; i < MaxConcurrentReads; i++ {
		s.readers <- struct{}{}
	}
	if _, err := s.Read(Query{}); CodeOf(err) != CodeReadersBusy {
		t.Fatalf("a fifth reader: %v", err)
	}
	for i := 0; i < MaxConcurrentReads; i++ {
		<-s.readers
	}
	if _, err := s.Read(Query{Limit: connectionhealth.MaxQueryRecords + 1}); CodeOf(err) != CodeQueryBound {
		t.Fatalf("an oversize limit: %v", err)
	}
	for _, c := range []string{"v1.0.0", "v2.1.0", "v1.1.-1", "../x", "v1.99.0"} {
		if _, err := s.Read(Query{Cursor: c}); CodeOf(err) != CodeInvalidCursor {
			t.Fatalf("cursor %q: %v", c, err)
		}
	}
	// Readers hammer the store while a producer offers; offers keep
	// succeeding and every page is internally ordered.
	more := p.next(t, 600)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < MaxConcurrentReads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				pg, err := s.Read(Query{Limit: 50})
				if err != nil && CodeOf(err) != CodeReadersBusy {
					errs <- err
					return
				}
				for k := 1; k < len(pg.Entries); k++ {
					a, b := pg.Entries[k-1].Record, pg.Entries[k].Record
					if a.BootID == b.BootID && b.Seq <= a.Seq {
						errs <- os.ErrInvalid
						return
					}
				}
			}
		}()
	}
	for _, r := range more {
		if _, err := s.Offer(r); err != nil && CodeOf(err) != CodeQueueFull {
			t.Fatal(err)
		}
	}
	flush(t, s)
	close(stop)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestACanceledCloseLeavesNoGoroutine(t *testing.T) {
	before := runtime.NumGoroutine()
	clock := newClock(t0)
	f := &faults{block: make(chan struct{}), entered: make(chan struct{}, 1)}
	s, err := Open(Options{Dir: t.TempDir(), BootID: "boot-a", Build: "b", Clock: clock, openSegment: f.opener()})
	if err != nil {
		t.Fatal(err)
	}
	p := newProducer(t, clock, "boot-a", "b")
	offerAll(t, s, p.next(t, 1))
	<-f.entered
	offerAll(t, s, p.next(t, 5))
	queued := uint64(s.Status().QueueRows)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := s.Close(ctx); CodeOf(err) != CodeShutdownCanceled {
		t.Fatalf("close with a stuck write: %v", err)
	}
	if _, err := s.Offer(p.next(t, 1)[0]); CodeOf(err) != CodeClosed {
		t.Fatalf("offer after close: %v", err)
	}
	f.release()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the worker did not exit once its write returned")
	}
	st := s.Status()
	if st.State != StateClosed || st.DroppedOnClose != queued || queued < 5 {
		t.Fatalf("after a canceled close: %+v", st)
	}
	for i := 0; i < 100 && runtime.NumGoroutine() > before; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before {
		t.Fatalf("goroutines %d, before %d", n, before)
	}
	// The lock is released: the directory opens again.
	s2, err := Open(Options{Dir: s.dir, BootID: "boot-b", Build: "b", Clock: clock})
	if err != nil {
		t.Fatalf("reopen after a canceled close: %v", err)
	}
	closeNow(t, s2)
}

func TestTheDirectoryIsOwnedAndExclusive(t *testing.T) {
	dir := t.TempDir()
	clock := newClock(t0)
	s := open(t, Options{Dir: dir, BootID: "boot-a", Build: "b", Clock: clock})
	if _, err := Open(Options{Dir: dir, BootID: "boot-b", Build: "b", Clock: clock}); CodeOf(err) != CodeLocked {
		t.Fatalf("a second open: %v", err)
	}
	_ = s
	for _, o := range []Options{
		{Dir: "relative/dir", BootID: "a", Build: "b"},
		{Dir: dir + "/../x", BootID: "a", Build: "b"},
		{Dir: dir, BootID: "a/b", Build: "b"},
		{Dir: dir, BootID: "a", Build: "b c"},
	} {
		if _, err := Open(o); CodeOf(err) != CodeInvalidOptions {
			t.Fatalf("%+v: %v", o, err)
		}
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Open(Options{Dir: link, BootID: "boot-c", Build: "b", Clock: clock}); CodeOf(err) != CodeUnsafeDirectory {
		t.Fatalf("a symlinked directory: %v", err)
	}
}

func TestAnIdleStoreDoesNoIO(t *testing.T) {
	clock := newClock(t0)
	s := open(t, Options{Dir: t.TempDir(), BootID: "boot-a", Build: "b", Clock: clock})
	offerAll(t, s, newProducer(t, clock, "boot-a", "b").next(t, 3))
	flush(t, s)
	before := s.Status()
	clock.advance(time.Hour)
	time.Sleep(2500 * time.Millisecond) // two worker ticks
	after := s.Status()
	if after.Writes != before.Writes || after.Syncs != before.Syncs {
		t.Fatalf("idle store wrote: writes %d→%d syncs %d→%d", before.Writes, after.Writes, before.Syncs, after.Syncs)
	}
}

func TestStatusMeasuresRecoverySegmentsAndLostRangeMemory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "unrecognized"), []byte("kept"), 0600); err != nil {
		t.Fatal(err)
	}
	clock := newClock(t0)
	s := open(t, Options{Dir: dir, BootID: "boot-a", Build: "build", Clock: clock})
	producer := newProducer(t, clock, "boot-a", "build")
	offerAll(t, s, producer.next(t, 2))
	flush(t, s)
	s.markLost(10, 11)
	s.markLost(20, 21)
	st := s.Status()
	if st.LostRanges != 2 || st.LargestSegmentBytes <= 0 || st.Recovery.EntriesExamined != 2 {
		t.Fatal(st)
	}
	// An opening scan is not misrepresented as a fresh directory scan.
	if err := os.WriteFile(filepath.Join(dir, "later"), []byte("kept"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := s.Status().Recovery.EntriesExamined; got != 2 {
		t.Fatal("opening observation changed", got)
	}
}
