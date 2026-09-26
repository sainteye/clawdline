package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/artifacts"
)

// Board and Session to-do reference images keep their bytes in files, and
// their rows in SQLite keep what names them.
//
// A picture is the one thing in the Board that is large and never changes, and
// while it was a BLOB in the daemon's one database every write transaction,
// WAL checkpoint and backup carried it, and deleting a picture did not shrink
// the file: on the machine this was measured on, 30 MB of a 64 MB database
// (2026-09-26) was reference images. Now each is `<id>.png` or `<id>.jpg` in
// ReferenceImagesDir, written through artifacts.WritePrivate (temporary name,
// fsync, rename), and the row keeps its sha256 so a read proves the file is
// the one the row names. A file that is gone, or that is not the bytes the row
// hashed, is ErrReferenceImageMissing or ErrReferenceImageMismatch: a named
// refusal, never an empty picture.
//
// The ordering that keeps the two in step:
//
//   - adding: the file is written and synced inside the write transaction,
//     before the row is inserted; the transaction commits after. If it does
//     not commit, the file is removed once the row is proved absent, and
//     whatever a crash leaves between is an unnamed file the sweep removes.
//   - deleting (the image, or the to-do its row cascades from): the file is
//     removed after the commit. A crash between leaves an unnamed file.
//   - the sweep removes files no row names, only under names this store writes
//     (referenceImageName, or its temporary spelling), and only once older than
//     ReferenceImageOrphanAgeLimit, so a picture another process has written
//     and not yet committed is never taken from under it.

// ReferenceImagesDir is where the files are, under the state directory.
func ReferenceImagesDir(stateDir string) string { return filepath.Join(stateDir, "reference-images") }

// referenceImageName is every name this store writes and the only names the
// sweep removes: a lowercase UUID, which is how every image id is minted, and
// the extension of the stored media type.
var referenceImageName = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.(?:png|jpg)$`)

// referenceImageTemp is a temporary name artifacts.WritePrivate made for one
// of those names and a crash left behind.
var referenceImageTemp = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.(?:png|jpg)` +
	regexp.QuoteMeta(artifacts.TempSuffix) + `[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

const (
	// ReferenceImageBatchLimit is how many bytes of pictures one step of the
	// move out of the database carries: read, written, verified and cleared
	// in one transaction. A picture larger than it still moves, alone.
	ReferenceImageBatchLimit = 32 << 20
	// ReferenceImageOrphanAgeLimit is how old a file no row names has to be
	// before the sweep removes it.
	ReferenceImageOrphanAgeLimit = time.Hour
	// ReferenceImageSweepIntervalLimit is how often the daemon sweeps, after
	// the sweep every Open runs.
	ReferenceImageSweepIntervalLimit = 6 * time.Hour
)

var (
	// ErrReferenceImageMissing is a row whose file is not there.
	ErrReferenceImageMissing = errors.New("reference image file is missing")
	// ErrReferenceImageMismatch is a row whose file is not the bytes it
	// recorded: another length, or another sha256.
	ErrReferenceImageMismatch = errors.New("reference image file does not match its row")
	// errReferenceImageID is an id that cannot be a file name here.
	errReferenceImageID = errors.New("reference image id cannot name a file")
)

// imageFiles is the directory, and what the sweep has removed since Open.
type imageFiles struct {
	dir string

	mu        sync.Mutex
	removed   int64
	refused   int64
	lastSweep time.Time
}

// ReferenceImageFileStats is what the files have cost since this handle
// opened: the sweep's removals and the reads refused for a missing or
// mismatched file.
type ReferenceImageFileStats struct {
	Removed   int64
	Refused   int64
	LastSweep time.Time
}

func (s *Store) ReferenceImageFileStats() ReferenceImageFileStats {
	s.images.mu.Lock()
	defer s.images.mu.Unlock()
	return ReferenceImageFileStats{Removed: s.images.removed, Refused: s.images.refused, LastSweep: s.images.lastSweep}
}

// imageRef names one picture's file.
type imageRef struct{ id, mediaType string }

func (f *imageFiles) path(r imageRef) (string, error) {
	name := r.id + artifacts.Extension(r.mediaType)
	if !referenceImageName.MatchString(name) {
		return "", fmt.Errorf("%w: %q", errReferenceImageID, r.id)
	}
	return filepath.Join(f.dir, name), nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// write puts data in r's file, synced, and answers its sha256.
func (f *imageFiles) write(r imageRef, data []byte) (string, error) {
	path, err := f.path(r)
	if err != nil {
		return "", err
	}
	if err := artifacts.EnsurePrivateDir(f.dir); err != nil {
		return "", err
	}
	if err := artifacts.WritePrivate(path, data); err != nil {
		return "", err
	}
	return digest(data), nil
}

// read answers r's bytes when they are the byteCount bytes hashing to sum.
func (f *imageFiles) read(r imageRef, byteCount int64, sum string) ([]byte, error) {
	data, err := f.readFile(r, byteCount, sum)
	if err != nil && (errors.Is(err, ErrReferenceImageMissing) || errors.Is(err, ErrReferenceImageMismatch)) {
		f.mu.Lock()
		f.refused++
		f.mu.Unlock()
		log.Printf("store: reference image %s refused: %v", r.id, err)
	}
	return data, err
}

func (f *imageFiles) readFile(r imageRef, byteCount int64, sum string) ([]byte, error) {
	path, err := f.path(r)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrReferenceImageMissing, filepath.Base(path))
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	// One byte past the row's count is enough to know the file is longer.
	data, err := io.ReadAll(io.LimitReader(file, byteCount+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != byteCount {
		return nil, fmt.Errorf("%w: %s holds %s bytes, the row says %d", ErrReferenceImageMismatch,
			filepath.Base(path), lengthWords(len(data), byteCount), byteCount)
	}
	if digest(data) != sum {
		return nil, fmt.Errorf("%w: %s is not the sha256 the row recorded", ErrReferenceImageMismatch, filepath.Base(path))
	}
	return data, nil
}

func lengthWords(n int, want int64) string {
	if int64(n) > want {
		return "more than " + fmt.Sprint(want)
	}
	return fmt.Sprint(n)
}

// remove deletes r's file; one already gone is the outcome wanted.
func (f *imageFiles) remove(r imageRef) {
	path, err := f.path(r)
	if err != nil {
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("store: reference image %s: the file stays for the sweep: %v", r.id, err)
	}
}

// settleImageFiles runs after a WorkV2 write. A commit removes the files of
// the rows it deleted; a write that did not commit removes the files it wrote,
// each once its row is proved absent. A file whose fate cannot be proved is
// left to the sweep, which is the safe way round: a stray file costs bytes, a
// removed one costs a picture.
func (s *Store) settleImageFiles(ctx context.Context, t *WorkV2Tx, err error) {
	if t == nil {
		return
	}
	if err == nil {
		for _, r := range t.dropped {
			s.images.remove(r)
		}
		return
	}
	for _, r := range t.added {
		var n int
		if qerr := s.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM work_v2_images WHERE id=?) +
      (SELECT COUNT(*) FROM session_direct_todo_images WHERE id=?)`, r.id, r.id).Scan(&n); qerr != nil || n != 0 {
			continue
		}
		s.images.remove(r)
	}
}

// referenceImageRow is the part of a row that names and proves its file.
func (s *Store) referenceImage(ctx context.Context, id string) (imageRef, int64, string, bool, error) {
	var r imageRef
	var byteCount int64
	var sum string
	err := s.db.QueryRowContext(ctx, `SELECT id,media_type,byte_count,sha256 FROM work_v2_images WHERE id=?
    UNION ALL SELECT id,media_type,byte_count,sha256 FROM session_direct_todo_images WHERE id=? LIMIT 1`, id, id).
		Scan(&r.id, &r.mediaType, &byteCount, &sum)
	if err == sql.ErrNoRows {
		return r, 0, "", false, nil
	}
	return r, byteCount, sum, err == nil, err
}

// SweepReferenceImages removes the files no row names, and the temporaries a
// crash left, once each is older than ReferenceImageOrphanAgeLimit. Any other
// name in the directory is somebody else's and is left alone. It answers how
// many it removed.
//
// The directory is listed before the rows are read. A picture being added in
// this process is in a transaction on the store's one connection, so the read
// waits for its commit and sees the row; one being added by another process is
// younger than the age limit.
func (s *Store) SweepReferenceImages(ctx context.Context, now time.Time) (int, error) {
	if err := reading(); err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(s.images.dir)
	if errors.Is(err, os.ErrNotExist) {
		s.noteSweep(now, 0)
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	named := map[string]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT id,media_type FROM work_v2_images
    UNION ALL SELECT id,media_type FROM session_direct_todo_images`)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var r imageRef
		if err := rows.Scan(&r.id, &r.mediaType); err != nil {
			rows.Close()
			return 0, err
		}
		named[r.id+artifacts.Extension(r.mediaType)] = true
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	removed := 0
	for _, e := range entries {
		name := e.Name()
		ours := referenceImageName.MatchString(name) && !named[name]
		if !ours && !referenceImageTemp.MatchString(name) {
			continue
		}
		if !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil || now.Sub(info.ModTime()) < ReferenceImageOrphanAgeLimit {
			continue
		}
		if err := os.Remove(filepath.Join(s.images.dir, name)); err == nil {
			removed++
		}
	}
	s.noteSweep(now, removed)
	if removed > 0 {
		log.Printf("store: the reference-image sweep removed %d file(s) no row names", removed)
	}
	return removed, nil
}

func (s *Store) noteSweep(now time.Time, removed int) {
	s.images.mu.Lock()
	s.images.removed += int64(removed)
	s.images.lastSweep = now
	s.images.mu.Unlock()
}

// SweepReferenceImagesEvery sweeps on a clock until ctx ends.
func (s *Store) SweepReferenceImagesEvery(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if _, err := s.SweepReferenceImages(ctx, now); err != nil && ctx.Err() == nil {
				log.Printf("store: the reference-image sweep failed: %v", err)
			}
		}
	}
}

// referenceImageTables are the two tables whose pictures moved to files.
var referenceImageTables = []struct{ table, index, owner string }{
	{"work_v2_images", "work_v2_images_item", "work_id"},
	{"session_direct_todo_images", "session_direct_todo_images_todo", "todo_id"},
}

// addReferenceImageHashColumns gives a table still carrying its bytes the
// column the move fills. Rows get ” until moved, which is how the move
// finds them.
func addReferenceImageHashColumns(db *sql.DB) error {
	for _, t := range referenceImageTables {
		has, err := hasColumn(db, t.table, "sha256")
		if err != nil {
			return err
		}
		if has {
			continue
		}
		if _, err := db.Exec(`ALTER TABLE ` + t.table + ` ADD COLUMN sha256 TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("%s.sha256: %w", t.table, err)
		}
	}
	return nil
}

// imageMoveBatchBytes is ReferenceImageBatchLimit, which a test lowers to see
// more than one step.
var imageMoveBatchBytes int64 = ReferenceImageBatchLimit

// imageMoveFault, when set by a test, runs after each step's files are
// written and before that step's rows are cleared: a crash at the worst point.
var imageMoveFault func(table string, step int) error

// ReferenceImageMove is what one Open's move out of the database did.
type ReferenceImageMove struct {
	Moved   int
	Bytes   int64
	Rebuilt bool
	// Vacuumed is whether the space was given back; VacuumErr why not.
	Vacuumed  bool
	VacuumErr error
}

// moveReferenceImages takes every picture still in the database out to its
// file, in steps of at most ReferenceImageBatchLimit bytes: each file is
// written, read back and hashed against the blob before the row's bytes are
// cleared and its hash recorded, in one transaction per step. A crash anywhere
// loses nothing — a row keeps its bytes until its file is proved — and the next
// Open carries on from the rows still holding bytes.
//
// When no row holds bytes, each table is rebuilt without the `data` column (the
// way migrateWorkV2DocumentRoles rebuilds one; this also widens a PNG-only
// media-type CHECK, which is what migrateImageMediaTypes did), and the space is
// given back with VACUUM: the database has auto_vacuum off, so nothing smaller
// would shrink the file. VACUUM holds the database's write lock while it runs,
// and Open has not returned, so nothing in this process is waiting on it; a CLI
// that opens the same file meanwhile waits busy_timeout (1 s) and is told the
// store is busy. It runs only in the Open that rebuilt the tables.
func moveReferenceImages(db *sql.DB, files *imageFiles) (ReferenceImageMove, error) {
	var out ReferenceImageMove
	carrying := false
	for _, t := range referenceImageTables {
		has, err := hasColumn(db, t.table, "data")
		if err != nil {
			return out, err
		}
		if !has {
			continue
		}
		carrying = true
		for step := 0; ; step++ {
			moved, n, err := moveReferenceImageStep(db, files, t.table, step)
			if err != nil {
				return out, fmt.Errorf("moving %s to files: %w", t.table, err)
			}
			out.Moved += moved
			out.Bytes += n
			if moved == 0 {
				break
			}
		}
	}
	if !carrying {
		return out, nil
	}
	rebuilt, err := rebuildReferenceImageTables(db)
	if err != nil {
		return out, err
	}
	out.Rebuilt = rebuilt
	if rebuilt {
		if _, err := db.Exec(`VACUUM`); err != nil {
			out.VacuumErr = err
		} else {
			out.Vacuumed = true
			_, _ = db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
		}
	}
	return out, nil
}

// moveReferenceImageStep moves the next rows of table still holding bytes, up
// to ReferenceImageBatchLimit of them.
func moveReferenceImageStep(db *sql.DB, files *imageFiles, table string, step int) (int, int64, error) {
	rows, err := db.Query(`SELECT id,media_type,length(data) FROM ` + table + `
    WHERE sha256='' ORDER BY rowid LIMIT 256`)
	if err != nil {
		return 0, 0, err
	}
	type pending struct {
		imageRef
		size int64
		sum  string
	}
	var batch []pending
	var total int64
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.mediaType, &p.size); err != nil {
			rows.Close()
			return 0, 0, err
		}
		if len(batch) > 0 && total+p.size > imageMoveBatchBytes {
			break
		}
		batch = append(batch, p)
		total += p.size
	}
	if err := rows.Close(); err != nil {
		return 0, 0, err
	}
	for i := range batch {
		p := &batch[i]
		var data []byte
		if err := db.QueryRow(`SELECT data FROM `+table+` WHERE id=?`, p.id).Scan(&data); err != nil {
			return 0, 0, err
		}
		if len(data) == 0 {
			return 0, 0, fmt.Errorf("%s %s has no bytes and no file", table, p.id)
		}
		sum, err := files.write(p.imageRef, data)
		if err != nil {
			return 0, 0, fmt.Errorf("%s %s: %w", table, p.id, err)
		}
		path, _ := files.path(p.imageRef)
		back, err := os.ReadFile(path)
		if err != nil {
			return 0, 0, fmt.Errorf("%s %s read back: %w", table, p.id, err)
		}
		if !bytes.Equal(back, data) || digest(back) != sum {
			return 0, 0, fmt.Errorf("%s %s: %w after writing it", table, p.id, ErrReferenceImageMismatch)
		}
		p.sum = sum
	}
	if len(batch) == 0 {
		return 0, 0, nil
	}
	if imageMoveFault != nil {
		if err := imageMoveFault(table, step); err != nil {
			return 0, 0, err
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	for _, p := range batch {
		if _, err := tx.Exec(`UPDATE `+table+` SET sha256=?, data=X'', byte_count=?
      WHERE id=? AND sha256=''`, p.sum, p.size, p.id); err != nil {
			return 0, 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return len(batch), total, nil
}

// rebuildReferenceImageTables drops the `data` column once no row holds bytes.
// The check is inside the rebuild's own transaction, so a second process that
// moved in between is seen.
func rebuildReferenceImageTables(db *sql.DB) (bool, error) {
	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	rebuilt := false
	for _, t := range referenceImageTables {
		var has int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name='data'`, t.table).Scan(&has); err != nil {
			return false, err
		}
		if has == 0 {
			continue
		}
		var left int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM ` + t.table + ` WHERE sha256=''`).Scan(&left); err != nil {
			return false, err
		}
		if left != 0 {
			return false, fmt.Errorf("%s still holds %d picture(s) in the database", t.table, left)
		}
		columns := "id," + t.owner + ",title,media_type,sha256,byte_count,width,height,position,created_by,created_at"
		before := t.table + "_before_files"
		for _, step := range []string{
			`ALTER TABLE ` + t.table + ` RENAME TO ` + before,
			`DROP INDEX IF EXISTS ` + t.index,
			workV2Schema,
			`INSERT INTO ` + t.table + ` (` + columns + `) SELECT ` + columns + ` FROM ` + before,
			`DROP TABLE ` + before,
		} {
			if _, err := tx.Exec(step); err != nil {
				return false, fmt.Errorf("%s without its bytes: %w", t.table, err)
			}
		}
		rebuilt = true
	}
	if !rebuilt {
		return false, nil
	}
	return true, tx.Commit()
}

// openReferenceImages moves what is left in the database out, then sweeps.
func openReferenceImages(db *sql.DB, files *imageFiles) error {
	move, err := moveReferenceImages(db, files)
	if err != nil {
		return err
	}
	if move.Moved > 0 || move.Rebuilt {
		log.Printf("store: moved %d reference image(s), %d bytes, from the database to %s; tables rebuilt=%v vacuumed=%v",
			move.Moved, move.Bytes, files.dir, move.Rebuilt, move.Vacuumed)
	}
	if move.VacuumErr != nil {
		log.Printf("store: the database keeps the space the pictures took: VACUUM: %v", move.VacuumErr)
	}
	return nil
}
