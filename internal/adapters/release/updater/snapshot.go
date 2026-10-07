package updater

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	// The store's own driver (internal/adapters/store).
	_ "modernc.org/sqlite"
)

// StoreFile is the daemon's store in the state directory
// (internal/adapters/store.DBFile).
const StoreFile = "clawdline.sqlite3"

// Snapshot writes a consistent copy of the store to
// <state>/backups/<from>-<to>.sqlite3 with VACUUM INTO, through a connection
// of its own: SQLite reads the last committed state, WAL pages not yet
// checkpointed included, while the daemon keeps writing. It keeps the newest
// BackupsKeptLimit snapshots. A machine with no store yet has nothing to
// snapshot and answers "".
func (e Env) Snapshot(from, to string) (string, error) {
	db := filepath.Join(e.StateDir, StoreFile)
	if _, err := os.Stat(db); errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err := os.MkdirAll(e.BackupsDir(), 0o700); err != nil {
		return "", fail(CodeSnapshotFailed, "%v", err)
	}
	out := filepath.Join(e.BackupsDir(), from+"-"+to+".sqlite3")
	tmp := out + ".tmp"
	os.Remove(tmp)
	conn, err := sql.Open("sqlite", db+"?_busy_timeout=10000")
	if err != nil {
		return "", fail(CodeSnapshotFailed, "%v", err)
	}
	defer conn.Close()
	if _, err := conn.Exec("VACUUM INTO ?", tmp); err != nil {
		os.Remove(tmp)
		return "", fail(CodeSnapshotFailed, "VACUUM INTO: %v", err)
	}
	if err := os.Rename(tmp, out); err != nil {
		os.Remove(tmp)
		return "", fail(CodeSnapshotFailed, "%v", err)
	}
	if err := e.pruneBackups(out); err != nil {
		return out, fail(CodeSnapshotFailed, "pruning old snapshots: %v", err)
	}
	return out, nil
}

// pruneBackups keeps the newest BackupsKeptLimit snapshots, and always keep.
func (e Env) pruneBackups(keep string) error {
	entries, err := os.ReadDir(e.BackupsDir())
	if err != nil {
		return err
	}
	type snap struct {
		path string
		mod  int64
	}
	var all []snap
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".sqlite3") {
			continue
		}
		info, err := ent.Info()
		if err != nil {
			continue
		}
		all = append(all, snap{filepath.Join(e.BackupsDir(), ent.Name()), info.ModTime().UnixNano()})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].mod > all[j].mod })
	kept := 0
	for _, s := range all {
		if s.path == keep || kept < BackupsKeptLimit-1 {
			if s.path != keep {
				kept++
			}
			continue
		}
		if err := os.Remove(s.path); err != nil {
			return err
		}
	}
	return nil
}

// restore puts snapshot back as the store. It runs only before the store is
// opened (the boot guard), so no connection holds the file; the WAL and
// shared-memory files of the store it replaces go with it.
func (e Env) restore(snapshot string) error {
	db := filepath.Join(e.StateDir, StoreFile)
	tmp := db + ".restore"
	if err := copyFile(snapshot, tmp); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(db + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			os.Remove(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, db); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("restoring %s: %w", filepath.Base(snapshot), err)
	}
	return nil
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(to)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(to)
		return err
	}
	return out.Close()
}
