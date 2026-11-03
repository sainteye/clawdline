package healthjournal

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
)

// segmentName is the only file name this package creates besides lockName,
// and the only one it ever deletes — and then only after the file's own
// header proved it is one of this package's segments.
var segmentName = regexp.MustCompile(`^seg-([0-9]{16})\.chj$`)

const lockName = "LOCK"

func nameOf(index uint64) string {
	s := strconv.FormatUint(index, 10)
	for len(s) < 16 {
		s = "0" + s
	}
	return "seg-" + s + ".chj"
}

// segment is one file. The worker is its only writer; valid only grows while
// the segment is active and is what a reader may read up to.
type segment struct {
	index   uint64
	path    string
	boot    string
	build   string
	created time.Time
	// info is the file as it was found or created; deletion re-checks it so
	// a file swapped in under the same name is never removed.
	info os.FileInfo

	valid     atomic.Int64
	size      atomic.Int64
	records   atomic.Uint64
	firstWall atomic.Int64 // unix nanoseconds; 0 when it holds no record
	lastWall  atomic.Int64
	sealed    atomic.Bool

	recovered bool // left unsealed by an earlier process
	torn      int64
	invalid   uint64
}

// Recovery is what Open found from earlier boots.
type Recovery struct {
	Segments        int `json:"segments"`
	EntriesExamined int `json:"entries_examined"`
	// Sealed segments closed cleanly; Unsealed ones were active when their
	// process died and are read up to their last whole, valid frame.
	Sealed   int `json:"sealed"`
	Unsealed int `json:"unsealed"`
	// TornBytes are bytes after the last whole frame — the crash-loss
	// window made visible — and after any corrupt frame.
	TornBytes int64 `json:"torn_bytes"`
	// InvalidFrames are well-framed payloads that failed validation: never
	// exported, counted here.
	InvalidFrames uint64 `json:"invalid_frames"`
	// Foreign are files named like a segment whose header is not one of
	// ours; Unrecognized are other names. Both are kept untouched and never
	// counted as this store's bytes.
	Foreign      int `json:"foreign_segments"`
	Unrecognized int `json:"unrecognized_files"`
	// ListingTruncated says the directory held more than MaxDirEntries
	// entries and the rest were not looked at.
	ListingTruncated bool `json:"listing_truncated"`
	// Boots is how many distinct boots the recovered segments belong to.
	Boots int `json:"boots"`
}

var errForeign = errors.New("not a segment of this store")

// recoverDir scans dir and returns its segments in index order, the next
// free index, and what it found.
func recoverDir(dir string) ([]*segment, uint64, Recovery, error) {
	var rec Recovery
	d, err := os.Open(dir)
	if err != nil {
		return nil, 0, rec, &Error{Code: classify(err), Field: "dir"}
	}
	defer d.Close()
	var names []string
	seen := 0
	for {
		ents, err := d.ReadDir(256)
		for _, e := range ents {
			if seen == MaxDirEntries {
				rec.ListingTruncated = true
				break
			}
			seen++
			names = append(names, e.Name())
		}
		if rec.ListingTruncated || err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, rec, &Error{Code: classify(err), Field: "dir"}
		}
	}
	rec.EntriesExamined = seen
	var segs []*segment
	var next uint64
	boots := map[string]bool{}
	for _, name := range names {
		m := segmentName.FindStringSubmatch(name)
		if m == nil {
			if name != lockName {
				rec.Unrecognized++
			}
			continue
		}
		idx, err := strconv.ParseUint(m[1], 10, 64)
		if err != nil {
			rec.Foreign++
			continue
		}
		if idx >= next {
			// A foreign file holding a name still takes it out of use.
			next = idx + 1
		}
		seg, err := scanSegment(dir, name, idx)
		if err != nil {
			rec.Foreign++
			continue
		}
		seg.sealed.Store(true) // no process writes an earlier boot's segment
		if seg.recovered {
			rec.Unsealed++
		} else {
			rec.Sealed++
		}
		rec.TornBytes += seg.torn
		rec.InvalidFrames += seg.invalid
		boots[seg.boot] = true
		segs = append(segs, seg)
	}
	sort.Slice(segs, func(i, j int) bool { return segs[i].index < segs[j].index })
	rec.Segments = len(segs)
	rec.Boots = len(boots)
	if next == 0 {
		next = 1
	}
	return segs, next, rec, nil
}

// openOwned opens a segment for reading only if it is still the regular file
// that was found or created under that name.
func openOwned(path string, want os.FileInfo) (*os.File, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() || (want != nil && !os.SameFile(fi, want)) {
		return nil, errForeign
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil || !os.SameFile(fi, st) {
		f.Close()
		return nil, errForeign
	}
	return f, nil
}

func scanSegment(dir, name string, idx uint64) (*segment, error) {
	path := filepath.Join(dir, name)
	f, err := openOwned(path, nil)
	if err != nil {
		return nil, errForeign
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, errForeign
	}
	size := st.Size()
	br := bufio.NewReaderSize(io.LimitReader(f, size), 64<<10)
	typ, payload, n, err := readFrame(br)
	var h segmentHeader
	if err != nil || typ != frameSegment || !decodeStrict(payload, &h) || !h.valid(idx) {
		return nil, errForeign
	}
	seg := &segment{index: idx, path: path, boot: h.BootID, build: h.Build, created: h.Created, info: st}
	off := int64(n)
	var lastSeq uint64
	sealed := false
	for !sealed {
		typ, payload, n, err := readFrame(br)
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		switch typ {
		case frameRecord:
			var r connectionhealth.Record
			if decodeStrict(payload, &r) && validateRecord(r, h.BootID, h.Build) == nil && r.Seq > lastSeq {
				lastSeq = r.Seq
				seg.records.Add(1)
				w := r.Wall.UnixNano()
				if seg.firstWall.Load() == 0 {
					seg.firstWall.Store(w)
				}
				if w > seg.lastWall.Load() {
					seg.lastWall.Store(w)
				}
			} else {
				seg.invalid++
			}
		case frameLoss:
			var l Loss
			if !decodeStrict(payload, &l) || !l.valid() {
				seg.invalid++
			}
		case frameSeal:
			var sp sealPayload
			if !decodeStrict(payload, &sp) {
				seg.invalid++
			}
			sealed = true
		default:
			// A second header inside a segment is corruption: stop.
			seg.torn = size - off
			seg.valid.Store(off)
			seg.size.Store(size)
			seg.recovered = true
			return seg, nil
		}
		off += int64(n)
	}
	seg.valid.Store(off)
	seg.size.Store(size)
	seg.torn = size - off
	seg.recovered = !sealed
	return seg, nil
}
