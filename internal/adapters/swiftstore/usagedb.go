package swiftstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

var scheduleID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// The Swift app's usage ledger (Sources/UsageLedger.swift), read from a copy.
//
// The ledger is a SQLite database in WAL mode, and the Swift app writes it
// while this reads. Opening it in place is not reading it: even a `mode=ro`
// connection takes locks on the `-shm` index and may create it, and a lock this
// process holds is a lock the Swift app has to wait for. So the database is
// never opened where it lives. The main file and its `-wal` are copied with
// O_RDONLY into a private directory, the copy is checked against both files'
// stamps taken before and after, and only the copy is opened. The `-shm` is
// not copied: it is an index SQLite rebuilds from the log, and copying it
// would copy somebody else's lock state.
//
// The rules of package swiftstore hold here too: nothing beside the ledger is
// written, the copy is removed as soon as it has been read, and a ledger that
// could not be read is "unknown" — the last good reading is carried and marked
// stale, and with none the caller is told it does not know rather than handed
// an empty ledger.

// ObservabilityDir is where the Swift app keeps usage.sqlite3
// (`UsageLedger.storeURL`). CLAWDLINE_OBSERVABILITY_DIR moves it, as it does
// for the Swift app. Only macOS has a default: there is no Swift app anywhere
// else, and "" means there is no ledger to read.
func ObservabilityDir() string {
	home, _ := os.UserHomeDir()
	return ObservabilityDirIn(home)
}

// ObservabilityDirIn is ObservabilityDir for the person whose home is home.
func ObservabilityDirIn(home string) string {
	if v := os.Getenv("CLAWDLINE_OBSERVABILITY_DIR"); v != "" {
		if (v == "~" || strings.HasPrefix(v, "~/")) && home != "" {
			v = home + v[1:]
		}
		return v
	}
	if runtime.GOOS != "darwin" || home == "" {
		return ""
	}
	return filepath.Join(home, "Library", "Application Support", "Clawdline", "Observability")
}

// LedgerInterval is one `usage_intervals` row: the columns
// `UsageLedger.row(from:)` reads that the Usage page publishes. The raw usage
// object, the working directory's neighbours and the source byte count stay in
// the ledger.
type LedgerInterval struct {
	IntervalKey     string
	Assistant       string
	SessionID       string
	BoundaryKind    string
	BoundaryID      string
	SegmentNo       int64
	Origin          string
	TaskID          *string
	ScheduleID      *string
	ProjectKey      *string
	WorkingDir      *string
	Depth           *int64
	Model           *string
	InputNew        *int64
	Output          *int64
	CacheRead       *int64
	CacheWrite      *int64
	SourceTotal     *int64
	Reconciliation  *string
	CostValue       *float64
	CostUnit        *string
	CostBasis       string
	PriceSnapshotID *string
	MissingReason   *string
	Coverage        string
	// CoverageReasons is the stored set, split on spaces as
	// `UsageLedger.coverageReasons(stored:)` does. Unknown words are kept.
	CoverageReasons []string
	StartedAt       time.Time
	EndedAt         *time.Time
	UpdatedAt       time.Time
	InputBasis      *string
	GraphID         *string
	ParentTaskID    *string
	RetryOf         *string
	Attempt         *int64
	LandingState    *string
	LandingVerified *bool
	Disposition     *string
}

// LedgerReading is one reading of the ledger and what is known about it.
type LedgerReading struct {
	// Intervals are every row, started newest first with the key as the
	// tie-break (`ORDER BY started_at DESC, interval_key DESC`).
	Intervals []LedgerInterval
	// Corrections counts `usage_corrections` rows by interval key.
	Corrections map[string]int
	// Latest is `MAX(updated_at)` over the whole ledger: the Swift page's
	// freshness, which is not bounded by the range being asked about.
	Latest time.Time
	// StoreVersion is the ledger's `PRAGMA user_version`.
	StoreVersion int64
	// Known is false when no reading has ever succeeded.
	Known bool
	// Stale is true when the newest attempt failed and an older reading is
	// being carried.
	Stale bool
	// Missing is true when there is no ledger at all: no Swift app, or one
	// that never recorded usage. The caller may use another source then.
	Missing bool
	// Disabled is true, together with Missing, when the legacy switch is off
	// (legacy.go): the ledger may exist and was not looked at.
	Disabled bool
	// ReadAt is when the reading in Intervals was copied.
	ReadAt time.Time
	// Err is why the newest attempt failed, if it did.
	Err error
}

// ErrLedgerChanging means every copy of the ledger was overtaken by a write.
// It is transient: the Swift app writes in bursts.
var ErrLedgerChanging = errors.New("the Swift usage ledger kept changing while it was being copied")

// UsageLedger reads usage.sqlite3 through private copies.
type UsageLedger struct {
	path     string
	disabled bool
	// tempRoot is where private copies are made. It belongs to this daemon.
	tempRoot string

	mu     sync.Mutex
	have   ledgerStamp
	good   LedgerReading
	copied time.Time // when the last successful copy started
}

// ledgerStamp is both files' stamps. The log is part of the database: a
// write that has not been checkpointed changes only the `-wal`.
type ledgerStamp struct {
	main   stamp
	wal    stamp
	hasWAL bool
}

const (
	// ledgerCopies is how many times a copy overtaken by a write is tried.
	ledgerCopies = 4
	// ledgerPause is the wait between two of those tries.
	ledgerPause = 40 * time.Millisecond
	// ledgerReuse is how long one copy answers every request even though the
	// Swift app has written since. A busy session writes the log every few
	// seconds; copying megabytes for each Usage request would make this
	// daemon's cost follow the Swift app's write rate.
	ledgerReuse = 5 * time.Second
	// ledgerMaxBytes bounds one copy. The ledger on the machine this was
	// written on is 8 MB after seven weeks.
	ledgerMaxBytes = 1 << 30
	// ledgerCopyPrefix names this reader's private copies.
	ledgerCopyPrefix = "usage-ledger-"
	// ledgerLeftover is how old a private copy must be before a later reader
	// removes it. A copy lives for one read; one this old belongs to a
	// process that stopped in the middle of that read.
	ledgerLeftover = 10 * time.Minute
)

// OpenUsageLedger prepares a reader of dir/usage.sqlite3. It touches nothing
// yet; dir "" is a reader that always answers Missing.
func OpenUsageLedger(dir string) *UsageLedger {
	if Disabled() {
		return &UsageLedger{disabled: true}
	}
	l := &UsageLedger{tempRoot: filepath.Join(os.TempDir(), "clawdline-next")}
	if dir != "" {
		l.path = filepath.Join(dir, "usage.sqlite3")
	}
	return l
}

// Path is the ledger this reader reads, or "".
func (l *UsageLedger) Path() string { return l.path }

// Read returns the newest whole reading of the ledger. The database is copied
// only when either file changed, and not more than once per ledgerReuse.
func (l *UsageLedger) Read(ctx context.Context) LedgerReading {
	if l == nil || l.path == "" {
		return LedgerReading{Known: true, Missing: true, Disabled: l != nil && l.disabled}
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	var lastErr error
	for attempt := 0; attempt < ledgerCopies; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return l.failed(ctx.Err())
			case <-time.After(ledgerPause):
			}
		}
		before, err := l.stamps()
		if errors.Is(err, os.ErrNotExist) {
			l.have = ledgerStamp{}
			l.good = LedgerReading{Known: true, Missing: true}
			return l.good
		}
		if err != nil {
			lastErr = err
			continue
		}
		if l.good.Known && !l.good.Missing &&
			(before == l.have || time.Since(l.copied) < ledgerReuse) {
			return l.good
		}
		started := time.Now()
		reading, err := l.copyAndRead(ctx, before)
		if err != nil {
			lastErr = err
			if !errors.Is(err, ErrLedgerChanging) && !errors.Is(err, os.ErrNotExist) {
				// A copy that did not race a write failed for a reason
				// another try will not change.
				break
			}
			continue
		}
		reading.ReadAt = started
		l.have = before
		l.good = reading
		l.copied = started
		return l.good
	}
	return l.failed(lastErr)
}

func (l *UsageLedger) failed(err error) LedgerReading {
	if l.good.Known && !l.good.Missing {
		out := l.good
		out.Stale = true
		out.Err = err
		return out
	}
	return LedgerReading{Err: err}
}

func (l *UsageLedger) stamps() (ledgerStamp, error) {
	var out ledgerStamp
	main, err := statFile(l.path)
	if err != nil {
		return out, err
	}
	out.main = main
	wal, err := statFile(l.path + "-wal")
	switch {
	case err == nil:
		out.wal, out.hasWAL = wal, true
	case errors.Is(err, os.ErrNotExist):
		// A ledger whose log was checkpointed away is whole on its own.
	default:
		return out, err
	}
	return out, nil
}

// copyAndRead copies both files, proves the copy is of one moment, and reads
// the copy. The private directory is removed before it returns.
func (l *UsageLedger) copyAndRead(ctx context.Context, before ledgerStamp) (LedgerReading, error) {
	dir, err := l.privateDir()
	if err != nil {
		return LedgerReading{}, err
	}
	defer os.RemoveAll(dir)

	copyPath := filepath.Join(dir, "usage.sqlite3")
	if err := copyReadOnly(l.path, copyPath, before.main); err != nil {
		return LedgerReading{}, err
	}
	if before.hasWAL {
		if err := copyReadOnly(l.path+"-wal", copyPath+"-wal", before.wal); err != nil {
			return LedgerReading{}, err
		}
	}
	after, err := l.stamps()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return LedgerReading{}, fmt.Errorf("%w: a file went away during the copy", ErrLedgerChanging)
		}
		return LedgerReading{}, err
	}
	if after != before {
		return LedgerReading{}, ErrLedgerChanging
	}
	return readLedgerCopy(ctx, copyPath)
}

// privateDir makes a directory only this process can read, under this
// daemon's own temporary root, and clears copies an interrupted read left.
func (l *UsageLedger) privateDir() (string, error) {
	if err := os.MkdirAll(l.tempRoot, 0o700); err != nil {
		return "", err
	}
	info, err := os.Lstat(l.tempRoot)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("%s is not a private directory", l.tempRoot)
	}
	if entries, err := os.ReadDir(l.tempRoot); err == nil {
		for _, e := range entries {
			if !e.IsDir() || !strings.HasPrefix(e.Name(), ledgerCopyPrefix) {
				continue
			}
			if fi, err := e.Info(); err == nil && time.Since(fi.ModTime()) > ledgerLeftover {
				_ = os.RemoveAll(filepath.Join(l.tempRoot, e.Name()))
			}
		}
	}
	return os.MkdirTemp(l.tempRoot, ledgerCopyPrefix) // 0700
}

// copyReadOnly copies src, opened O_RDONLY, to a new 0600 file. The opened
// file must be the one that was stamped: a rename-into-place between the stamp
// and the open would otherwise copy a file nobody compared.
func copyReadOnly(src, dst string, want stamp) error {
	in, err := os.OpenFile(src, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if (stamp{size: info.Size(), mtime: info.ModTime().UnixNano(), ino: inode(info)}) != want {
		return ErrLedgerChanging
	}
	if info.Size() > ledgerMaxBytes {
		return fmt.Errorf("%s is %d bytes, more than this reader copies", src, info.Size())
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(in, ledgerMaxBytes+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n != info.Size() {
		// Grew or shrank while it was read; the stamp comparison after the
		// copy would say so too, but a short copy is never worth opening.
		return ErrLedgerChanging
	}
	return nil
}

// ledgerColumns are the columns read, in order. A column an older store
// version does not have yet is read as NULL, which is what every row written
// before it existed means by it.
var ledgerColumns = []string{
	"interval_key", "assistant", "session_id", "boundary_kind", "boundary_id", "segment_no",
	"origin", "task_id", "schedule_id", "project_key", "working_dir", "depth", "model",
	"input_new", "output", "cache_read", "cache_write", "source_total", "reconciliation",
	"cost_value", "cost_unit", "cost_basis", "price_snapshot_id", "missing_reason",
	"coverage", "coverage_reasons", "started_at", "ended_at", "updated_at", "input_basis",
	"graph_id", "parent_task_id", "retry_of", "attempt", "landing_state", "landing_verified",
	"disposition",
}

func readLedgerCopy(ctx context.Context, path string) (LedgerReading, error) {
	// The copy is this process's own file, so it is opened as an ordinary
	// database: SQLite replays the copied log into a fresh index beside it.
	// query_only keeps even that copy from being written by a statement.
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=query_only(1)")
	if err != nil {
		return LedgerReading{}, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	var out LedgerReading
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&out.StoreVersion); err != nil {
		return LedgerReading{}, fmt.Errorf("the usage ledger copy did not open: %w", err)
	}
	have, err := tableColumns(ctx, db, "usage_intervals")
	if err != nil {
		return LedgerReading{}, err
	}
	if len(have) == 0 {
		// A ledger file with no interval table has recorded nothing yet.
		return LedgerReading{Known: true, Corrections: map[string]int{}}, nil
	}
	selection := make([]string, len(ledgerColumns))
	for i, c := range ledgerColumns {
		switch {
		case have[c]:
			selection[i] = c
		case c == "coverage_reasons" && have["coverage_reason"]:
			// Store versions 1 and 2 named the one-mark column this; version 3
			// renamed it without rewriting a value.
			selection[i] = "coverage_reason"
		default:
			selection[i] = "NULL"
		}
	}
	rows, err := db.QueryContext(ctx, "SELECT "+strings.Join(selection, ", ")+
		" FROM usage_intervals ORDER BY started_at DESC, interval_key DESC")
	if err != nil {
		return LedgerReading{}, err
	}
	defer rows.Close()
	for rows.Next() {
		iv, err := scanInterval(rows)
		if err != nil {
			return LedgerReading{}, err
		}
		out.Intervals = append(out.Intervals, iv)
	}
	if err := rows.Err(); err != nil {
		return LedgerReading{}, err
	}

	out.Corrections = map[string]int{}
	if tables, err := tableColumns(ctx, db, "usage_corrections"); err == nil && len(tables) > 0 {
		crows, err := db.QueryContext(ctx,
			"SELECT interval_key, COUNT(*) FROM usage_corrections GROUP BY interval_key")
		if err != nil {
			return LedgerReading{}, err
		}
		defer crows.Close()
		for crows.Next() {
			var key string
			var n int
			if err := crows.Scan(&key, &n); err != nil {
				return LedgerReading{}, err
			}
			out.Corrections[key] = n
		}
		if err := crows.Err(); err != nil {
			return LedgerReading{}, err
		}
	}

	var latest sql.NullFloat64
	if err := db.QueryRowContext(ctx, "SELECT MAX(updated_at) FROM usage_intervals").Scan(&latest); err != nil {
		return LedgerReading{}, err
	}
	if latest.Valid {
		out.Latest = seconds(latest.Float64)
	}
	out.Known = true
	return out, nil
}

func tableColumns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, "SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

func scanInterval(rows *sql.Rows) (LedgerInterval, error) {
	var (
		key, assistant, session, bkind, bid, origin, coverage, costBasis sql.NullString
		segment                                                          sql.NullInt64
		task, schedule, project, workdir, model, recon, unit, snapshot   sql.NullString
		missing, reasons, basis, graph, parent, retry, landing, disp     sql.NullString
		depth, in, outTok, cacheR, cacheW, srcTotal, attempt, verified   sql.NullInt64
		cost, started, ended, updated                                    sql.NullFloat64
	)
	err := rows.Scan(&key, &assistant, &session, &bkind, &bid, &segment,
		&origin, &task, &schedule, &project, &workdir, &depth, &model,
		&in, &outTok, &cacheR, &cacheW, &srcTotal, &recon,
		&cost, &unit, &costBasis, &snapshot, &missing,
		&coverage, &reasons, &started, &ended, &updated, &basis,
		&graph, &parent, &retry, &attempt, &landing, &verified,
		&disp)
	if err != nil {
		return LedgerInterval{}, err
	}
	iv := LedgerInterval{
		IntervalKey:     key.String,
		Assistant:       assistant.String,
		SessionID:       session.String,
		BoundaryKind:    bkind.String,
		BoundaryID:      bid.String,
		SegmentNo:       segment.Int64,
		Origin:          origin.String,
		TaskID:          nullString(task),
		ScheduleID:      nullString(schedule),
		ProjectKey:      nullString(project),
		WorkingDir:      nullString(workdir),
		Depth:           nullInt(depth),
		Model:           nullString(model),
		InputNew:        nullInt(in),
		Output:          nullInt(outTok),
		CacheRead:       nullInt(cacheR),
		CacheWrite:      nullInt(cacheW),
		SourceTotal:     nullInt(srcTotal),
		Reconciliation:  nullString(recon),
		CostUnit:        nullString(unit),
		CostBasis:       costBasis.String,
		PriceSnapshotID: nullString(snapshot),
		MissingReason:   nullString(missing),
		Coverage:        coverage.String,
		CoverageReasons: strings.Fields(reasons.String),
		StartedAt:       seconds(started.Float64),
		UpdatedAt:       seconds(updated.Float64),
		InputBasis:      nullString(basis),
		GraphID:         nullString(graph),
		ParentTaskID:    nullString(parent),
		RetryOf:         nullString(retry),
		Attempt:         nullInt(attempt),
		LandingState:    nullString(landing),
		Disposition:     nullString(disp),
	}
	if iv.CoverageReasons == nil {
		iv.CoverageReasons = []string{}
	}
	if cost.Valid {
		v := cost.Float64
		iv.CostValue = &v
	}
	if ended.Valid {
		t := seconds(ended.Float64)
		iv.EndedAt = &t
	}
	if verified.Valid {
		v := verified.Int64 == 1
		iv.LandingVerified = &v
	}
	return iv, nil
}

func nullString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func nullInt(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}

// seconds is a ledger timestamp (`timeIntervalSince1970`) as a time.
func seconds(v float64) time.Time {
	whole, frac := math.Modf(v)
	return time.Unix(int64(whole), int64(math.Round(frac*1e9))).UTC()
}

// ScheduleTitles are the live schedules' names by id, from the Swift app's
// schedules directory: `Orchestrator.usageScheduleLabels`'s authoritative half.
// Only `schedule_id` and `title` are decoded; a schedule's task and delivery
// settings are not this daemon's to hold. A file that fails the identity and
// title checks of `Orchestrator.schedule(from:)` is skipped, as the Swift
// inventory lists it as invalid rather than as a schedule; the checks on the
// fields not decoded here are not repeated.
func (s *Store) ScheduleTitles() map[string]string {
	out := map[string]string{}
	if s == nil || s.dir == "" || s.disabled {
		return out
	}
	dir := filepath.Join(s.dir, "schedules")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || filepath.Ext(name) != ".json" {
			continue
		}
		var v struct {
			Version    int    `json:"clawdline_schedule"`
			ScheduleID string `json:"schedule_id"`
			Title      string `json:"title"`
		}
		if _, err := decodeFile(filepath.Join(dir, name), &v); err != nil {
			continue
		}
		if v.Version != 1 || v.ScheduleID != strings.TrimSuffix(name, ".json") ||
			!scheduleID.MatchString(v.ScheduleID) ||
			v.Title == "" || utf8.RuneCountInString(v.Title) > 120 {
			continue
		}
		out[v.ScheduleID] = v.Title
	}
	return out
}

// QuotaSettings are the Swift app's settings that decide how its plan windows
// are read (Config.swift): where the status line keeps its cache, where Codex
// lives, and when a window counts as nearly spent. Empty and zero are the
// Swift app's defaults.
type QuotaSettings struct {
	StatusDir    string
	CodexHome    string
	LowThreshold float64
}

// quotaConfigFile declares those three keys of config.json and nothing else.
type quotaConfigFile struct {
	StatusDir    any `json:"status_dir"`
	CodexHome    any `json:"codex_home"`
	LowThreshold any `json:"assistant_quota_low_threshold"`
}

// QuotaConfig reads QuotaSettings, decoding config.json only when it changed.
type QuotaConfig struct {
	config *file[quotaConfigFile]
}

// OpenQuotaConfig prepares a reader of dir/config.json. With the legacy switch
// off it reads nothing and answers the defaults, which is what the Swift app
// runs on without a config of its own.
func OpenQuotaConfig(dir string) *QuotaConfig {
	if Disabled() {
		return &QuotaConfig{}
	}
	return &QuotaConfig{config: newFile[quotaConfigFile](filepath.Join(dir, "config.json"))}
}

// Read returns the settings as `Config` parses them: a key of the wrong type,
// or a threshold outside (0, 100), is the default. A config that cannot be
// read is the last good reading, or the defaults when there was none — which
// is what the Swift app itself runs on without one.
func (q *QuotaConfig) Read() QuotaSettings {
	var out QuotaSettings
	if q == nil || q.config == nil {
		return out
	}
	r := q.config.read()
	if !r.Known || r.Missing {
		return out
	}
	if v, ok := r.Value.StatusDir.(string); ok {
		out.StatusDir = v
	}
	if v, ok := r.Value.CodexHome.(string); ok {
		out.CodexHome = v
	}
	if v, ok := r.Value.LowThreshold.(float64); ok && v > 0 && v < 100 {
		out.LowThreshold = v
	}
	return out
}
