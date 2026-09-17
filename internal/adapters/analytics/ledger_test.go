package analytics

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ledgerSchema is the part of the Swift ledger's store version 7 the reader
// needs, written the way the Swift app writes it.
const ledgerSchema = `
PRAGMA journal_mode=WAL;
CREATE TABLE usage_intervals (
  interval_key TEXT PRIMARY KEY, schema_version INTEGER NOT NULL, assistant TEXT NOT NULL,
  session_id TEXT NOT NULL, boundary_kind TEXT NOT NULL, boundary_id TEXT NOT NULL,
  segment_no INTEGER NOT NULL, segment_reason TEXT NOT NULL, origin TEXT NOT NULL,
  task_id TEXT, schedule_id TEXT, project_key TEXT, working_dir TEXT, kind_raw TEXT,
  isolation TEXT, depth INTEGER, claim_count INTEGER, timeout_seconds INTEGER, task_state TEXT,
  model TEXT, reasoning_effort TEXT, billing_mode TEXT NOT NULL, usage_raw TEXT,
  input_new INTEGER, output INTEGER, cache_read INTEGER, cache_write INTEGER, total INTEGER,
  source_total INTEGER, reconciliation TEXT, cost_value REAL, cost_unit TEXT,
  cost_basis TEXT NOT NULL, price_snapshot_id TEXT, missing_reason TEXT, coverage TEXT NOT NULL,
  coverage_reasons TEXT, sealed INTEGER NOT NULL DEFAULT 0, source_bytes INTEGER,
  started_at REAL NOT NULL, ended_at REAL, local_day TEXT NOT NULL, observed_at REAL NOT NULL,
  updated_at REAL NOT NULL, graph_id TEXT, parent_task_id TEXT, retry_of TEXT, attempt INTEGER,
  landing_state TEXT, disposition TEXT, input_basis TEXT, landing_verified INTEGER);
CREATE TABLE usage_corrections (id INTEGER PRIMARY KEY AUTOINCREMENT, interval_key TEXT NOT NULL,
  reason TEXT NOT NULL, was TEXT, proposed TEXT, written_at REAL NOT NULL);
PRAGMA user_version=7;
`

type fileStamp struct {
	size  int64
	mtime time.Time
}

func listing(t *testing.T, dir string) map[string]fileStamp {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]fileStamp{}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = fileStamp{info.Size(), info.ModTime()}
	}
	return out
}

// The Swift app is writing while this reads, so a row it has committed may be
// only in the log. The reader must see that row, and must leave the ledger's
// directory exactly as it found it: no `-shm` touched, nothing created, and no
// copy left behind in its own temporary directory.
func TestTheLedgerIsReadFromACopyThatIncludesTheLog(t *testing.T) {
	dir := t.TempDir()
	tmp := t.TempDir()
	t.Setenv("CLAWDLINE_OBSERVABILITY_DIR", dir)
	t.Setenv("TMPDIR", tmp)

	writer, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "usage.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	writer.SetMaxOpenConns(1)
	if _, err := writer.Exec(ledgerSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec("PRAGMA wal_autocheckpoint=0"); err != nil {
		t.Fatal(err)
	}
	now := float64(time.Now().Unix())
	if _, err := writer.Exec(`INSERT INTO usage_intervals (interval_key, schema_version, assistant,
		session_id, boundary_kind, boundary_id, segment_no, segment_reason, origin, task_id,
		project_key, depth, model, billing_mode, input_new, output, cache_read, cache_write,
		cost_basis, coverage, coverage_reasons, started_at, local_day, observed_at, updated_at,
		graph_id, attempt, landing_verified)
		VALUES ('k1', 1, 'claude', 'unresolved-session:x', 'task', 't1', 0, 'start', 'dispatch',
		't1', '/work/repo', 1, 'claude-opus-5', 'subscription', 10, 20, NULL, 5,
		'list_price_estimate', 'partial', 'source_regressed no_usage_recorded', ?, '2026-09-17',
		?, ?, 'g1', 2, 1)`, now-60, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec(`INSERT INTO usage_corrections (interval_key, reason, written_at)
		VALUES ('k1', 'a', ?), ('k1', 'b', ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "usage.sqlite3-wal")); err != nil {
		t.Fatalf("the fixture must keep its rows in the log: %v", err)
	}
	before := listing(t, dir)

	c := NewCollector(t.TempDir(), nil)
	rows, err := c.Rows(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("want the one row that is only in the log, got %d", len(rows))
	}
	r := rows[0]
	if r.IntervalKey != "k1" || r.Origin != "dispatch" || r.ProjectKey != "/work/repo" || r.Depth != 1 ||
		r.GraphID != "g1" || r.Attempt == nil || *r.Attempt != 2 || r.Corrections != 2 ||
		r.LandingVerified == nil || !*r.LandingVerified {
		t.Fatalf("row not carried as stored: %+v", r)
	}
	if r.Tokens[partCacheRead] != nil || r.Tokens[partOutput] == nil || *r.Tokens[partOutput] != 20 {
		t.Fatalf("an unknown part must stay unknown and a known one known: %v", r.Tokens)
	}
	if strings.Join(r.CoverageReasons, ",") != "source_regressed,no_usage_recorded" {
		t.Fatalf("coverage reasons: %v", r.CoverageReasons)
	}
	if m := measureRow(r); !contains(m.reasons, "session_unresolved") || !m.incomplete {
		t.Fatalf("measurement lost a mark: %+v", m)
	}

	after := listing(t, dir)
	if len(after) != len(before) {
		t.Fatalf("the ledger directory changed: %v -> %v", before, after)
	}
	for name, st := range before {
		if got, ok := after[name]; !ok || got.size != st.size || !got.mtime.Equal(st.mtime) {
			t.Fatalf("%s changed while it was read: %+v -> %+v", name, st, got)
		}
	}
	copies, _ := filepath.Glob(filepath.Join(os.TempDir(), "clawdline-next", "usage-ledger-*"))
	if len(copies) != 0 {
		t.Fatalf("a private copy was left behind: %v", copies)
	}
}

// A ledger that is there and cannot be read is not an idle month: the answer
// is an error, never an empty list and never the transcripts' other numbers.
func TestAnUnreadableLedgerIsUnknownNotEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAWDLINE_OBSERVABILITY_DIR", dir)
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.WriteFile(filepath.Join(dir, "usage.sqlite3"), []byte("not a database, not at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := NewCollector(t.TempDir(), nil)
	rows, err := c.Rows(context.Background(), time.Time{})
	if !errors.Is(err, ErrLedgerUnreadable) {
		t.Fatalf("want ErrLedgerUnreadable, got %d rows, err %v", len(rows), err)
	}
}
