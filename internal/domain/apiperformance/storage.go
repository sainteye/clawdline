package apiperformance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type loadResult struct {
	history history
	err     error
}
type storedSnapshot struct {
	bytes []byte
	at    int64
}
type storageWorker struct {
	loaded  chan loadResult
	pending chan storedSnapshot
	done    chan struct{}
}

// A separate database/connection/transaction from business state. SQLite's
// process lock and revision compare-and-swap protect concurrent daemon boots;
// a losing writer is disabled rather than overwriting another boot's history.
// DELETE journaling caps disk at the database cap plus one rollback journal.
func openStorage(dir string, caps ...map[string]int64) (*sql.DB, history, int64, error) {
	limits := HardLimits()
	if len(caps) > 0 {
		limits = caps[0]
	}
	h := emptyHistory()
	dir = filepath.Join(dir, "api-performance")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, h, 0, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, h, 0, err
	}
	path := filepath.Join(dir, "history.sqlite")
	// Pre-create with owner-only access, before SQLite can create its journal.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, h, 0, err
	}
	_ = f.Close()
	if err = os.Chmod(path, 0600); err != nil {
		return nil, h, 0, err
	}
	if info, e := os.Stat(path); e != nil || info.Size() > limits["store"] {
		return nil, h, 0, ErrUnavailable
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, h, 0, err
	}
	db.SetMaxOpenConns(1)
	fail := func(e error) (*sql.DB, history, int64, error) { _ = db.Close(); return nil, h, 0, e }
	ctx, cancel := context.WithTimeout(context.Background(), MaxQuerySeconds*time.Second)
	defer cancel()
	for _, stmt := range []string{"PRAGMA busy_timeout=0", "PRAGMA journal_mode=DELETE", "PRAGMA synchronous=FULL", "PRAGMA page_size=4096"} {
		if _, err = db.ExecContext(ctx, stmt); err != nil {
			return fail(err)
		}
	}
	var pageSize int64
	if err = db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil || pageSize <= 0 || limits["store"] < pageSize {
		return fail(ErrUnavailable)
	}
	for _, stmt := range []string{fmt.Sprintf("PRAGMA max_page_count=%d", limits["store"]/pageSize), "CREATE TABLE IF NOT EXISTS snapshot (id INTEGER PRIMARY KEY CHECK(id=1), revision INTEGER NOT NULL, payload BLOB NOT NULL)", "INSERT OR IGNORE INTO snapshot VALUES (1,0,'')"} {
		if _, err = db.ExecContext(ctx, stmt); err != nil {
			return fail(err)
		}
	}
	var revision int64
	var size int
	var payload []byte
	if err = db.QueryRowContext(ctx, "SELECT revision,length(payload),CASE WHEN length(payload)<=? THEN payload ELSE NULL END FROM snapshot WHERE id=1", limits["snapshot"]).Scan(&revision, &size, &payload); err != nil {
		return fail(err)
	}
	if int64(size) > limits["snapshot"] {
		return fail(ErrUnavailable)
	}
	if size > 0 {
		if err = json.Unmarshal(payload, &h); err != nil {
			return fail(ErrUnavailable)
		}
		if err = validateHistory(h); err != nil {
			return fail(err)
		}
		h.prune(time.Now().UnixMilli())
	}
	return db, h, revision, nil
}

func validateHistory(h history) error {
	if h.Schema != 1 || len(h.Minutes) > MaxMinutes || len(h.Hours) > MaxHours || len(h.Routes) > MaxRoutes || len(h.LossHours) > MaxHours || h.Minutes == nil || h.Hours == nil || h.Routes == nil {
		return ErrUnavailable
	}
	for i, m := range []map[int64]*bucket{h.Minutes, h.Hours} {
		width := int64(60000)
		if i == 1 {
			width = 3600000
		}
		for key, b := range m {
			if b == nil || b.Start != key || key < 0 || key%width != 0 || len(b.Routes) > MaxRoutes+1 || b.Routes == nil || len(b.Source.Build) > MaxRouteBytes || len(b.Source.BootID) > 32 {
				return ErrUnavailable
			}
			for word, v := range b.Coverage {
				if v>>60 != 0 || (i == 0 && word > 0 && v != 0) {
					return ErrUnavailable
				}
			}
			for route := range b.Routes {
				if len(route) > MaxRouteBytes {
					return ErrUnavailable
				}
			}
		}
	}
	for route := range h.Routes {
		if len(route) > MaxRouteBytes {
			return ErrUnavailable
		}
	}
	for _, hour := range h.LossHours {
		if hour <= 0 || hour%3600000 != 0 {
			return ErrUnavailable
		}
	}
	return nil
}

func writeSnapshot(db *sql.DB, revision int64, payload []byte, caps ...int64) error {
	bytes := int64(MaxSnapshotBytes)
	if len(caps) > 0 {
		bytes = caps[0]
	}
	if int64(len(payload)) > bytes {
		return ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), MaxQuerySeconds*time.Second)
	defer cancel()
	result, err := db.ExecContext(ctx, "UPDATE snapshot SET revision=revision+1,payload=? WHERE id=1 AND revision=?", payload, revision)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrUnavailable
	}
	return nil
}

func (r *Runtime) startStorage(dir string) *storageWorker {
	w := &storageWorker{loaded: make(chan loadResult, 1), pending: make(chan storedSnapshot, 1), done: make(chan struct{})}
	go func() {
		defer close(w.done)
		db, h, revision, err := openStorage(dir, r.limits)
		if err != nil {
			r.storeFailures.Add(1)
			r.setStoreState("unavailable")
		} else {
			r.setStoreState("ok")
			r.storeAt.Store(h.PersistedAt)
			if info, e := os.Stat(filepath.Join(dir, "api-performance", "history.sqlite")); e == nil {
				r.storeBytes.Store(info.Size())
			}
		}
		w.loaded <- loadResult{h, err}
		if db != nil {
			defer db.Close()
		}
		disabled := err != nil
		for snapshot := range w.pending {
			if disabled {
				continue
			}
			if err = writeSnapshot(db, revision, snapshot.bytes, r.Limit("snapshot")); err != nil {
				r.storeFailures.Add(1)
				r.setStoreState("unavailable")
				// Zero automatic retries. Restart is the explicit recovery boundary.
				disabled = true
				continue
			}
			revision++
			r.storeAt.Store(snapshot.at)
			if info, e := os.Stat(filepath.Join(dir, "api-performance", "history.sqlite")); e == nil {
				r.storeBytes.Store(info.Size())
			}
		}
	}()
	return w
}

func (r *Runtime) persist(w *storageWorker, h *history, now time.Time) {
	if *r.storeState.Load() != "ok" {
		return
	}
	h.prune(now.UnixMilli())
	h.PersistedAt = now.UnixMilli()
	h.Dropped = r.dropped.Load()
	h.LossHours = r.lossHoursAt(now.UnixMilli())
	h.UnlocatedLoss = r.unlocatedLoss.Load()
	payload, err := json.Marshal(h)
	r.snapshotUsed.Store(int64(len(payload)))
	if err != nil || int64(len(payload)) > r.Limit("snapshot") {
		r.storeFailures.Add(1)
		r.setStoreState("snapshot_too_large")
		return
	}
	s := storedSnapshot{payload, h.PersistedAt}
	select {
	case w.pending <- s:
	default:
		// One pending snapshot, coalesced to the latest cumulative state.
		select {
		case <-w.pending:
		default:
		}
		select {
		case w.pending <- s:
		default:
		}
	}
}

// IsUnavailable is intentionally independent of SQLite's error strings, which
// may include filesystem paths and must never be sent to a reader.
func IsUnavailable(err error) bool { return errors.Is(err, ErrUnavailable) }
