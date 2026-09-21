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
// opened it. So these are kept, and pruned by count, oldest first by name — how
// often somebody sends a picture is not something a clock knows.
type Drops struct {
	Dir  string
	Keep int
	mu   sync.Mutex

	// What pruning let go since this process began, and who hears that a
	// picture was written (limits N16). Guarded by mu.
	evicted   int64
	evictedAt time.Time
	stored    func()
}

// OnStored hands f every picture written, after the cache has let go of its
// lock, so the capacity register measures it before the next write can
// remove the oldest. f must not block.
func (d *Drops) OnStored(f func()) {
	d.mu.Lock()
	d.stored = f
	d.mu.Unlock()
}

// Reading is the `artifacts.drops` row: the pictures in the cache, each
// already typed into somebody's prompt as a path, and how many pruning let go.
// One directory listing.
func (d *Drops) Reading() capacity.Reading {
	d.mu.Lock()
	defer d.mu.Unlock()
	r := capacity.Reading{Known: true, Counters: capacity.Counters{Evicted: d.evicted, LastActionAt: d.evictedAt}}
	entries, err := os.ReadDir(d.Dir)
	if errors.Is(err, os.ErrNotExist) {
		r.Note = "no picture has been sent from a page on this machine"
		return r
	}
	if err != nil {
		return capacity.Unmeasured(err.Error())
	}
	for _, e := range entries {
		if e.Type().IsRegular() && dropName.MatchString(e.Name()) {
			r.Used++
		}
	}
	return r
}

// DropsKeep is the Swift app's `prune(keeping: 40)`.
const DropsKeep = 40

// NewDrops is the drop cache under this daemon's own directory.
func NewDrops(stateDir string) *Drops {
	return &Drops{Dir: DropsDir(stateDir), Keep: DropsKeep}
}

// DropsDir is where NewDrops writes. The transcript reader asks the same
// function, so a path written here is recognised there as a picture rather
// than shown as forty characters of directory.
func DropsDir(stateDir string) string { return filepath.Join(stateDir, "drops") }

// dropName is every name this cache writes and the only names it removes:
// `clawdline-<yyyyMMdd-HHmmss-SSS>-<random>.png`, the Swift app's spelling.
var dropName = regexp.MustCompile(`^clawdline-\d{8}-\d{6}-\d{3}-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.png$`)

// Store writes one PNG and returns its absolute path.
func (d *Drops) Store(data []byte, now time.Time) (string, error) {
	d.mu.Lock()
	if err := ensurePrivateDir(d.Dir); err != nil {
		d.mu.Unlock()
		return "", err
	}
	name := "clawdline-" + now.Format("20060102-150405.000") + "-" + newUUID() + ".png"
	name = strings.Replace(name, ".", "-", 1)
	path := filepath.Join(d.Dir, name)
	if err := writePrivate(path, data); err != nil {
		d.mu.Unlock()
		return "", err
	}
	d.pruneLocked(now)
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

func (d *Drops) pruneLocked(now time.Time) {
	entries, err := os.ReadDir(d.Dir)
	if err != nil {
		return
	}
	names := []string{}
	for _, e := range entries {
		if e.Type().IsRegular() && dropName.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	keep := d.Keep
	if keep <= 0 {
		keep = DropsKeep
	}
	if len(names) <= keep {
		return
	}
	// The name starts with the time, so this is oldest first. Each one
	// removed was typed into a prompt as a path, so it is counted, and the
	// register — which measured this cache as it filled — has it on the row.
	sort.Strings(names)
	extra := len(names) - keep
	removed := 0
	for _, n := range names[:extra] {
		if os.Remove(filepath.Join(d.Dir, n)) == nil {
			removed++
		}
	}
	d.evicted += int64(removed)
	d.evictedAt = now
	log.Printf("drops: pruned %d, kept %d", removed, keep)
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
