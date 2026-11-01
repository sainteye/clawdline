package antigravitysession

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/antigravityprocess"
)

const (
	MaxWireTraceBytes   = 16 << 20
	MaxWireTraceRecords = 8192
	MaxWireTraceAge     = 24 * time.Hour
	MaxWireQueueBytes   = 16 << 20
	MaxWireQueueRecords = 64
	MaxWireConnections  = 1
)

type wireRecord struct {
	owner, worker string
	at            int64
	raw           []byte
}
type WireTraceStats struct {
	Bytes, Records, OldestAt                       int64
	QueueBytes, QueueRecords, Dropped, WriteErrors int64
	Connections                                    int64
	Known                                          bool
}
type wireRecorder struct {
	db     *sql.DB
	mu     sync.Mutex
	jobs   chan wireRecord
	done   chan struct{}
	ctx    context.Context
	cancel context.CancelFunc
	closed bool
	stats  WireTraceStats
}

func openWireRecorder(dir string) (*wireRecorder, error) {
	path := filepath.Join(dir, "acp-wire.sqlite")
	if st, err := os.Lstat(path); err == nil && !st.Mode().IsRegular() {
		return nil, refusal("trace_unavailable")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, refusal("trace_unavailable")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, refusal("trace_unavailable")
	}
	info, statErr := f.Stat()
	named, nameErr := os.Lstat(path)
	if statErr != nil || nameErr != nil || !named.Mode().IsRegular() || !os.SameFile(info, named) || f.Chmod(0600) != nil {
		f.Close()
		return nil, refusal("trace_unavailable")
	}
	f.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, refusal("trace_unavailable")
	}
	db.SetMaxOpenConns(MaxWireConnections)
	ctx, cancel := context.WithTimeout(context.Background(), MaxCallTime)
	defer cancel()
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS acp_wire(seq INTEGER PRIMARY KEY AUTOINCREMENT,owned_id TEXT NOT NULL,worker TEXT NOT NULL,at_ns INTEGER NOT NULL,record BLOB NOT NULL,bytes INTEGER NOT NULL)`)
	if err == nil {
		_, err = db.ExecContext(ctx, `DELETE FROM acp_wire WHERE at_ns<?`, time.Now().Add(-MaxWireTraceAge).UnixNano())
	}
	if err != nil {
		db.Close()
		return nil, refusal("trace_unavailable")
	}
	life, stop := context.WithCancel(context.Background())
	r := &wireRecorder{db: db, jobs: make(chan wireRecord, MaxWireQueueRecords), done: make(chan struct{}), ctx: life, cancel: stop}
	r.stats.Connections = 1
	if err := r.readStats(ctx); err != nil {
		stop()
		db.Close()
		return nil, refusal("trace_unavailable")
	}
	go r.run()
	return r, nil
}

// Enqueue before interpretation without waiting for disk or the conversation's
// SQL connection. Overload drops diagnostic evidence, never a provider frame.
func (r *wireRecorder) enqueue(owner, worker string, event antigravityprocess.WireEvent) error {
	raw, err := json.Marshal(event)
	if err != nil {
		return refusal("trace_record_invalid")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.stats.QueueRecords >= MaxWireQueueRecords || int64(len(raw))+r.stats.QueueBytes > MaxWireQueueBytes {
		r.stats.Dropped++
		return refusal("trace_full")
	}
	r.stats.QueueBytes += int64(len(raw))
	r.stats.QueueRecords++
	r.jobs <- wireRecord{owner: owner, worker: worker, at: event.At, raw: raw}
	return nil
}
func (r *wireRecorder) run() {
	defer close(r.done)
	for record := range r.jobs {
		ctx, cancel := context.WithTimeout(r.ctx, MaxCallTime)
		err := r.write(ctx, record)
		cancel()
		r.mu.Lock()
		r.stats.QueueBytes -= int64(len(record.raw))
		r.stats.QueueRecords--
		if err != nil {
			r.stats.WriteErrors++
			r.stats.Known = false
		}
		r.mu.Unlock()
	}
}
func (r *wireRecorder) write(ctx context.Context, record wireRecord) error {
	if len(record.raw) > MaxWireTraceBytes {
		return refusal("trace_record_invalid")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO acp_wire(owned_id,worker,at_ns,record,bytes) VALUES(?,?,?,?,?)`, record.owner, record.worker, record.at, record.raw, len(record.raw))
	if err == nil {
		_, err = tx.ExecContext(ctx, `DELETE FROM acp_wire WHERE at_ns<?`, time.Now().Add(-MaxWireTraceAge).UnixNano())
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `DELETE FROM acp_wire WHERE seq IN (SELECT seq FROM (
 SELECT seq,SUM(bytes) OVER (ORDER BY seq DESC) AS retained_bytes,ROW_NUMBER() OVER (ORDER BY seq DESC) AS retained_rows FROM acp_wire)
 WHERE retained_bytes>? OR retained_rows>?)`, MaxWireTraceBytes, MaxWireTraceRecords)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err == nil {
		err = r.readStats(ctx)
	}
	return err
}
func (r *wireRecorder) readStats(ctx context.Context) error {
	var bytes, records, oldest int64
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(bytes),0),COUNT(*),COALESCE(MIN(at_ns),0) FROM acp_wire`).Scan(&bytes, &records, &oldest); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stats.Bytes, r.stats.Records, r.stats.OldestAt, r.stats.Known = bytes, records, oldest, true
	return nil
}
func (r *wireRecorder) snapshot() WireTraceStats { r.mu.Lock(); defer r.mu.Unlock(); return r.stats }
func (r *wireRecorder) close() error {
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		close(r.jobs)
	}
	r.mu.Unlock()
	timer := time.AfterFunc(MaxCallTime, r.cancel)
	<-r.done
	timer.Stop()
	r.cancel()
	return r.db.Close()
}
func (h *Host) wireObserver(ownedID string) func(antigravityprocess.WireEvent) error {
	if h.trace == nil {
		return nil
	}
	worker := rand.Text()
	return func(event antigravityprocess.WireEvent) error { return h.trace.enqueue(ownedID, worker, event) }
}
func (h *Host) WireTraceStats() WireTraceStats {
	if h.trace != nil {
		return h.trace.snapshot()
	}
	return WireTraceStats{Known: h.traceErr == ""}
}
func (h *Host) wireError() string {
	if h.traceErr != "" {
		return h.traceErr
	}
	if h.trace != nil {
		st := h.trace.snapshot()
		if st.Dropped > 0 || st.WriteErrors > 0 {
			return "trace_write_failed"
		}
	}
	return ""
}
