package artifacts

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// Drops is `Drop.store` and `Drop.prune`: pictures written out so a terminal
// program can read them.
//
// **What is handed to an assistant is a file.** Claude Code turns the picture
// on the pasteboard into `[Image #1]`; Codex, and Claude Code where there is no
// pasteboard, is given the path. Either way the file has to outlive the send:
// a terminal accepting a prompt is not the program on the other end having
// opened it, and a session may read the path again after a compaction or a
// resume.
//
// The Swift app kept the newest forty by count, reasoning that how often
// somebody sends a picture is not something a clock knows. That is true, and
// it is not what the clock is asked here: a young file is one a session is
// still likely to read, whatever the send rate, and a count of forty removes a
// file typed into a prompt minutes ago as soon as an afternoon's screenshots
// pass it. So a file goes once it is older than DropsAgeLimit, except the
// newest Keep, which age alone never removes (somebody who sends one picture a
// fortnight still has the last forty); and whatever its age, the oldest go
// while the directory holds more than MaxBytes, since forty pictures at the
// normalize limit are nearly half a gigabyte. A file the byte cap removes
// before it is DropsYoungLimit old is the one removal worth a person's phone:
// the cache is too small for how it is used (limits N16).
type Drops struct {
	Dir string
	// Keep is how many of the newest files age alone never removes.
	Keep int
	// MaxBytes is the most the directory holds; the oldest go past it.
	MaxBytes int64
	// MaxAge is when a file past the newest Keep is removed.
	MaxAge time.Duration
	// Young is how young a file the byte cap removes has to be for the
	// removal to be told as the cache being too small.
	Young time.Duration
	mu    sync.Mutex

	// What pruning let go since this process began, and who hears that a
	// picture was written (limits N16). Guarded by mu.
	evicted   int64
	expired   int64
	removedAt time.Time
	// youngAt is when the byte cap removed a file younger than Young, for
	// the removals within the last Young; youngTotal counts every one.
	youngAt    []time.Time
	youngTotal int64
	stored     func()
}

// OnStored hands f every picture written, after the cache has let go of its
// lock, so the capacity register measures it before the next write can
// remove the oldest. f must not block.
func (d *Drops) OnStored(f func()) {
	d.mu.Lock()
	d.stored = f
	d.mu.Unlock()
}

// Reading is the `artifacts.drops` row: the bytes the pictures in the cache
// hold against MaxBytes, each already typed into somebody's prompt as a path,
// and how many pruning let go by age and by bytes. Files past their age are
// removed first, so a cache nobody has sent to for a week does not read as
// holding what it would let go at the next picture. One directory listing.
func (d *Drops) Reading() capacity.Reading { return d.ReadingAt(time.Now()) }

// ReadingAt is Reading at now.
func (d *Drops) ReadingAt(now time.Time) capacity.Reading {
	d.mu.Lock()
	defer d.mu.Unlock()
	files, err := d.listLocked()
	if errors.Is(err, os.ErrNotExist) {
		r := capacity.Reading{Known: true, WindowSeconds: int64(d.maxAge() / time.Second),
			Note: "no picture has been sent from a page on this machine"}
		r.Counters = d.countersLocked()
		return r
	}
	if err != nil {
		return capacity.Unmeasured(err.Error())
	}
	files = d.pruneLocked(files, now)
	r := capacity.Reading{Known: true, WindowSeconds: int64(d.maxAge() / time.Second), Counters: d.countersLocked()}
	for _, f := range files {
		r.Used += f.size
	}
	if len(files) > 0 {
		r.OldestAt = files[0].at
	}
	r.Note = fmt.Sprintf("%d picture(s); the newest %d are kept whatever their age", len(files), d.keep())
	return r
}

// YoungReading is the `artifacts.drops_young` row: how many files the byte
// cap removed within the last Young while they were younger than Young. Each
// was typed into a prompt recently enough to be read again, so one is a cache
// too small for how it is used — the only thing about this cache a person is
// pushed.
func (d *Drops) YoungReading() capacity.Reading { return d.YoungReadingAt(time.Now()) }

// YoungReadingAt is YoungReading at now.
func (d *Drops) YoungReadingAt(now time.Time) capacity.Reading {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.forgetYoungLocked(now)
	r := capacity.Reading{Known: true, Used: int64(len(d.youngAt)), WindowSeconds: int64(d.young() / time.Second),
		Counters: capacity.Counters{Evicted: d.youngTotal}}
	if n := len(d.youngAt); n > 0 {
		r.Counters.LastActionAt = d.youngAt[n-1]
		r.Note = fmt.Sprintf("the byte cap removed %d picture(s) sent less than %s before", n, d.young())
	}
	return r
}

func (d *Drops) countersLocked() capacity.Counters {
	return capacity.Counters{Evicted: d.evicted, Expired: d.expired, LastActionAt: d.removedAt}
}

// The drop cache's bounds. DropsKeep is the Swift app's `prune(keeping: 40)`,
// now a floor under the age rule rather than the whole rule.
const (
	DropsKeep       = 40
	MaxDropsBytes   = 256 << 20
	DropsAgeLimit   = 7 * 24 * time.Hour
	DropsYoungLimit = 24 * time.Hour
)

// NewDrops is the drop cache under this daemon's own directory.
func NewDrops(stateDir string) *Drops {
	return &Drops{Dir: DropsDir(stateDir), Keep: DropsKeep, MaxBytes: MaxDropsBytes,
		MaxAge: DropsAgeLimit, Young: DropsYoungLimit}
}

// DropsDir is where NewDrops writes. The transcript reader asks the same
// function, so a path written here is recognised there as a picture rather
// than shown as forty characters of directory.
func DropsDir(stateDir string) string { return filepath.Join(stateDir, "drops") }

// dropName is every name this cache writes and the only names it removes:
// `clawdline-<yyyyMMdd-HHmmss-SSS>-<random>.png`, the Swift app's spelling, or
// the same ending `.jpg` for a photograph (Normalize).
var dropName = regexp.MustCompile(`^clawdline-\d{8}-\d{6}-\d{3}-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.(?:png|jpg)$`)

// Store writes one normalized picture and returns its absolute path. The name
// ends as the bytes are: `.jpg` for a JPEG, `.png` for anything else.
func (d *Drops) Store(data []byte, now time.Time) (string, error) {
	d.mu.Lock()
	if err := ensurePrivateDir(d.Dir); err != nil {
		d.mu.Unlock()
		return "", err
	}
	name := "clawdline-" + now.Format("20060102-150405.000") + "-" + newUUID() + Extension(sniffMediaType(data))
	name = strings.Replace(name, ".", "-", 1)
	path := filepath.Join(d.Dir, name)
	if err := writePrivate(path, data); err != nil {
		d.mu.Unlock()
		return "", err
	}
	if files, err := d.listLocked(); err == nil {
		d.pruneLocked(files, now)
	}
	stored := d.stored
	d.mu.Unlock()
	if stored != nil {
		stored()
	}
	return path, nil
}

// Discard removes files a send wrote and never handed over. Only names this
// cache would have written, and only inside its own directory, are ever
// removed: a path is a string that came back from somewhere.
func (d *Drops) Discard(paths []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, p := range paths {
		if filepath.Dir(p) != filepath.Clean(d.Dir) || !dropName.MatchString(filepath.Base(p)) {
			continue
		}
		_ = os.Remove(p)
	}
}

func (d *Drops) keep() int {
	if d.Keep <= 0 {
		return DropsKeep
	}
	return d.Keep
}

func (d *Drops) maxBytes() int64 {
	if d.MaxBytes <= 0 {
		return MaxDropsBytes
	}
	return d.MaxBytes
}

func (d *Drops) maxAge() time.Duration {
	if d.MaxAge <= 0 {
		return DropsAgeLimit
	}
	return d.MaxAge
}

func (d *Drops) young() time.Duration {
	if d.Young <= 0 {
		return DropsYoungLimit
	}
	return d.Young
}

// drop is one file of the cache: its name, when it was written and its size.
type drop struct {
	name string
	at   time.Time
	size int64
}

// listLocked is every file this cache wrote, oldest first. The name starts
// with the time Store was handed, so sorting by name is sorting by age; that
// time is the age too, and the file's own time only when the name cannot be
// read.
func (d *Drops) listLocked() ([]drop, error) {
	entries, err := os.ReadDir(d.Dir)
	if err != nil {
		return nil, err
	}
	var files []drop
	for _, e := range entries {
		if !e.Type().IsRegular() || !dropName.MatchString(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		at, ok := dropTime(e.Name())
		if !ok {
			at = info.ModTime()
		}
		files = append(files, drop{name: e.Name(), at: at, size: info.Size()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	return files, nil
}

// dropTime reads the time out of `clawdline-<yyyyMMdd-HHmmss-SSS>-…`, which
// Store wrote in the local zone.
func dropTime(name string) (time.Time, bool) {
	const stamp = "20060102-150405-000"
	rest := strings.TrimPrefix(name, "clawdline-")
	if len(rest) < len(stamp) {
		return time.Time{}, false
	}
	s := rest[:len(stamp)]
	at, err := time.ParseInLocation("20060102-150405.000", s[:15]+"."+s[16:], time.Local)
	return at, err == nil
}

// pruneLocked removes, oldest first, every file past MaxAge that is not one
// of the newest Keep, and then the oldest while the directory holds more than
// MaxBytes — never the newest, which was just typed into a prompt. It answers
// the files left. Each file removed was typed into a prompt as a path, so it
// is counted, and one the byte cap removed while young is remembered for the
// row that tells a person.
func (d *Drops) pruneLocked(files []drop, now time.Time) []drop {
	maxAge, young := d.maxAge(), d.young()
	floor := len(files) - d.keep()
	kept := files[:0:0]
	expired := 0
	for i, f := range files {
		if i < floor && now.Sub(f.at) > maxAge && os.Remove(filepath.Join(d.Dir, f.name)) == nil {
			expired++
			continue
		}
		kept = append(kept, f)
	}
	var total int64
	for _, f := range kept {
		total += f.size
	}
	evicted, youngs := 0, 0
	for len(kept) > 1 && total > d.maxBytes() {
		f := kept[0]
		if err := os.Remove(filepath.Join(d.Dir, f.name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			break
		}
		kept, total = kept[1:], total-f.size
		evicted++
		if now.Sub(f.at) < young {
			youngs++
			d.youngAt = append(d.youngAt, now)
		}
	}
	if expired+evicted == 0 {
		return kept
	}
	d.expired += int64(expired)
	d.evicted += int64(evicted)
	d.youngTotal += int64(youngs)
	d.removedAt = now
	d.forgetYoungLocked(now)
	log.Printf("drops: %d past %s, %d over %d bytes (%d of them under %s old); kept %d", expired, maxAge, evicted, d.maxBytes(), youngs, young, len(kept))
	return kept
}

// forgetYoungLocked lets go of young removals older than Young: the row
// reads the last Young's worth.
func (d *Drops) forgetYoungLocked(now time.Time) {
	cut := 0
	for cut < len(d.youngAt) && now.Sub(d.youngAt[cut]) >= d.young() {
		cut++
	}
	d.youngAt = d.youngAt[cut:]
}

// ensurePrivateDir makes dir 0700, and makes an existing one 0700 too: a cache
// somebody loosened is a cache other users can read pictures out of.
func ensurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	return os.Chmod(dir, 0o700)
}

// writePrivate writes a whole file 0600 through a temporary name, so a reader
// never sees half of it and a failure leaves nothing under the real name.
func writePrivate(path string, data []byte) error {
	tmp := path + ".tmp-" + newUUID()
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// newUUID is a lowercase random UUID, spelled as Foundation spells one.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
