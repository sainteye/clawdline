// Package analytics answers the Usage page: the Swift app's
// `UsageQueryService`, over the Swift app's own ledger when this machine has
// one (ledger.go), and over the assistants' own records when it does not.
//
// The rules — what a run is, how a Project is named, when two ranges compare,
// which insight is worth showing — are that service's, ported rule for rule
// (Sources/UsageLedger.swift). Read from the ledger, the rows are that
// service's too. Read from the transcripts they are not: the Swift app records
// an interval while it watches a session, and this daemon was not watching, so
// a row there is one conversation (per model, for Claude) as its transcript
// accounts for it, and the numbers differ from the Swift page's; the shape and
// the arithmetic over them do not.
package analytics

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline-go/internal/adapters/transcript"
)

// PriceSnapshotID names the list-price table a `list_price_estimate` came
// from. transcript.Price is the Swift app's `Orchestrator.price(forModel:)`,
// which is the table the Swift ledger records under this id.
const PriceSnapshotID = "clawdline-prices-2026-08-28"

// openWindow is how recently a record must have been written for its
// conversation to count as still open. It is the Swift ledger's staleness
// line (twice its 300-second checkpoint), so "open" here and "current" there
// are the same length of quiet.
const openWindow = 600 * time.Second

// Token parts, in the Swift ledger's order.
const (
	partInputNew = iota
	partOutput
	partCacheRead
	partCacheWrite
)

var partNames = [4]string{"inputNew", "output", "cacheRead", "cacheWrite"}

// Row is one usage interval, in the Swift ledger's vocabulary.
type Row struct {
	IntervalKey  string
	TaskID       string
	ScheduleID   string
	StartedAt    time.Time
	EndedAt      time.Time // zero while the conversation is open
	UpdatedAt    time.Time
	Assistant    string
	Model        string
	Origin       string // manual, dispatch, schedule
	ProjectKey   string
	WorkingDir   string
	SessionID    string
	BoundaryKind string // session or task
	BoundaryID   string
	Depth        int64
	RootLabel    string
	// Tokens are inputNew, output, cacheRead and cacheWrite; nil is unknown,
	// which is never turned into zero.
	Tokens          [4]*int64
	SourceTotal     *int64
	CostValue       *float64
	CostUnit        string
	CostBasis       string
	PriceSnapshotID string
	MissingCost     string
	Coverage        string // complete, partial, source_missing
	CoverageReasons []string
	InputBasis      string
	ParentTaskID    string
	LandingState    string
	LandingVerified *bool
	// Legacy is a row read from the Swift ledger: history for a conversation
	// whose own records are gone (ledger.go).
	Legacy bool
	// The rest is only known from the Swift ledger; a transcript row leaves
	// them empty, which the wire spells null.
	Reconciliation string
	GraphID        string
	RetryOf        string
	Attempt        *int64
	Disposition    string
	// Corrections is how many `usage_corrections` name this row.
	Corrections int
}

// ErrBusy means the scan the answer needs is still running. The scan carries
// on; a later read gets its result.
var ErrBusy = errors.New("usage analytics is still reading the assistants' records")

// Collector reads the rows, once per file per change. A transcript only
// grows, so a Claude record is read on from where the last read stopped; a
// Codex record is read at both ends and nowhere else.
type Collector struct {
	home   string
	swift  *swiftstore.Store
	ledger *swiftstore.UsageLedger
	// open opens one record; os.Open outside tests. A test holds it shut to
	// stand for a disk that has stopped answering, which no file can do on
	// every platform: a FIFO blocks an open on Unix, and Windows has none.
	open func(name string) (*os.File, error)

	mu      sync.Mutex
	claude  map[string]*claudeFile
	codex   map[string]*codexFile
	keys    map[string]string
	scan    *scan
	fresh   time.Time // when the last completed scan started
	covered time.Time // the earliest start that scan was asked to cover

	ledgerRead    *ledgerCache
	source        string
	sourceFailing bool
}

type scan struct {
	from time.Time
	done chan struct{}
}

// NewCollector reads under home: the Swift app's usage ledger when home has
// one, and the assistants' transcripts otherwise. swift may be nil.
func NewCollector(home string, swift *swiftstore.Store) *Collector {
	return &Collector{
		home:   home,
		swift:  swift,
		ledger: swiftstore.OpenUsageLedger(swiftstore.ObservabilityDirIn(home)),
		open:   os.Open,
		claude: map[string]*claudeFile{},
		codex:  map[string]*codexFile{},
		keys:   map[string]string{},
	}
}

// Source is where one answer's rows came from (cutover B2): the assistants'
// own records, read by this daemon, always; and the Swift ledger as history,
// as Ledger says it was read.
type Source struct {
	Ledger swiftstore.Source
}

// Rows returns every row whose record was written at or after `since` (zero
// means all of them), waiting for a scan no longer than ctx allows. A scan is
// shared: two requests at once start one.
func (c *Collector) Rows(ctx context.Context, since time.Time) ([]Row, error) {
	rows, _, err := c.RowsFrom(ctx, since)
	return rows, err
}

// RowsFrom is Rows and the sources behind it: this daemon's own rows, with the
// Swift ledger's for the conversations they do not hold (ledger.go).
func (c *Collector) RowsFrom(ctx context.Context, since time.Time) ([]Row, Source, error) {
	legacy, status, err := c.ledgerRows(ctx, since)
	src := Source{Ledger: status}
	if err != nil {
		return nil, src, err
	}
	own, err := c.ownRows(ctx, since)
	if err != nil {
		return nil, src, err
	}
	return withHistory(own, legacy), src, nil
}

// ownRows is the assistants' records, read by this daemon.
func (c *Collector) ownRows(ctx context.Context, since time.Time) ([]Row, error) {
	for {
		c.mu.Lock()
		reusable := !c.fresh.IsZero() && time.Since(c.fresh) < 15*time.Second &&
			(c.covered.IsZero() || (!since.IsZero() && !since.Before(c.covered)))
		if reusable && c.scan == nil {
			rows := c.assemble(since)
			c.mu.Unlock()
			return rows, nil
		}
		s := c.scan
		if s == nil {
			s = &scan{from: since, done: make(chan struct{})}
			c.scan = s
			go c.run(s)
		}
		c.mu.Unlock()
		select {
		case <-s.done:
			// The scan that just finished may have been asked for less than
			// this request needs; the loop decides.
		case <-ctx.Done():
			return nil, ErrBusy
		}
	}
}

func (c *Collector) run(s *scan) {
	started := time.Now()
	claude := c.claudePaths(s.from)
	codex := c.codexPaths(s.from)

	work := make(chan func(), 64)
	var wg sync.WaitGroup
	// Bounded: this is a background read on a machine somebody is working on.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range work {
				f()
			}
		}()
	}
	for _, p := range claude {
		p := p
		work <- func() { c.readClaude(p) }
	}
	for _, p := range codex {
		p := p
		work <- func() { c.readCodex(p) }
	}
	close(work)
	wg.Wait()

	c.mu.Lock()
	c.fresh = started
	c.covered = s.from
	c.scan = nil
	c.mu.Unlock()
	close(s.done)
}

type candidate struct {
	path string
	info os.FileInfo
}

func (c *Collector) claudePaths(since time.Time) []candidate {
	matches, _ := filepath.Glob(filepath.Join(c.home, ".claude", "projects", "*", "*.jsonl"))
	out := make([]candidate, 0, len(matches))
	for _, p := range matches {
		st, err := os.Stat(p)
		if err != nil || (!since.IsZero() && st.ModTime().Before(since)) {
			continue
		}
		out = append(out, candidate{p, st})
	}
	return out
}

// codexPaths walks only the day directories that can hold a thread written
// since `since`: Codex files a thread under the day it started, and a thread
// is written after it starts.
func (c *Collector) codexPaths(since time.Time) []candidate {
	root := filepath.Join(c.home, ".codex", "sessions")
	days, _ := filepath.Glob(filepath.Join(root, "*", "*", "*"))
	out := []candidate{}
	for _, d := range days {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			st, err := e.Info()
			if err != nil || (!since.IsZero() && st.ModTime().Before(since)) {
				continue
			}
			out = append(out, candidate{filepath.Join(d, e.Name()), st})
		}
	}
	return out
}

// ---------- Claude ----------

type claudeFile struct {
	size, offset int64
	mtime        time.Time
	sessionID    string
	cwd          string
	entrypoint   string
	first, last  time.Time
	// sums are every assistant line's usage added up, and model the last
	// model a line named — `Orchestrator.claudeUsage`, which does not merge
	// the lines a reply is split across, so neither does this.
	sums  [4]int64
	model string
	found bool
	err   error
}

func (c *Collector) readClaude(cand candidate) {
	c.mu.Lock()
	prev := c.claude[cand.path]
	c.mu.Unlock()
	size, mtime := cand.info.Size(), cand.info.ModTime()
	if prev != nil && prev.size == size && prev.mtime.Equal(mtime) {
		return
	}
	var f *claudeFile
	if prev != nil && prev.err == nil && size >= prev.offset {
		cp := *prev
		f = &cp
	} else {
		f = &claudeFile{sessionID: strings.TrimSuffix(filepath.Base(cand.path), ".jsonl")}
	}
	f.size, f.mtime, f.err = size, mtime, nil
	if err := f.readFrom(c.open, cand.path); err != nil {
		f.err = err
	}
	c.mu.Lock()
	c.claude[cand.path] = f
	c.mu.Unlock()
}

var assistantNeedle = []byte(`"type":"assistant"`)

func (f *claudeFile) readFrom(open func(string) (*os.File, error), path string) error {
	fh, err := open(path)
	if err != nil {
		return err
	}
	defer fh.Close()
	if f.offset > 0 {
		if _, err := fh.Seek(f.offset, io.SeekStart); err != nil {
			return err
		}
	}
	r := bufio.NewReaderSize(fh, 1<<20)
	at := f.offset
	for {
		line, err := r.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			// A line longer than the buffer: gather it whole.
			full := append([]byte(nil), line...)
			for err == bufio.ErrBufferFull {
				line, err = r.ReadSlice('\n')
				full = append(full, line...)
			}
			line = full
		}
		if err != nil {
			// Only whole lines count; a partial last line is read next time.
			if err == io.EOF {
				break
			}
			return err
		}
		at += int64(len(line))
		f.offset = at
		f.line(line)
	}
	return nil
}

func (f *claudeFile) line(line []byte) {
	if f.first.IsZero() || f.cwd == "" || f.entrypoint == "" {
		var head struct {
			Timestamp  string `json:"timestamp"`
			CWD        string `json:"cwd"`
			Entrypoint string `json:"entrypoint"`
		}
		if json.Unmarshal(line, &head) == nil {
			if f.entrypoint == "" {
				f.entrypoint = head.Entrypoint
			}
			if t, ok := parseTime(head.Timestamp); ok && f.first.IsZero() {
				f.first = t
			}
			if f.cwd == "" && head.CWD != "" {
				f.cwd = head.CWD
			}
		}
	}
	if !bytes.Contains(line, assistantNeedle) {
		return
	}
	var rec struct {
		Type      string `json:"type"`
		Timestamp string `json:"timestamp"`
		CWD       string `json:"cwd"`
		Message   struct {
			Model string `json:"model"`
			Usage *struct {
				Input      int64 `json:"input_tokens"`
				Output     int64 `json:"output_tokens"`
				CacheRead  int64 `json:"cache_read_input_tokens"`
				CacheWrite int64 `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &rec) != nil || rec.Type != "assistant" {
		return
	}
	if t, ok := parseTime(rec.Timestamp); ok {
		if f.first.IsZero() {
			f.first = t
		}
		f.last = t
	}
	u := rec.Message.Usage
	if u == nil {
		return
	}
	f.found = true
	f.sums[partInputNew] += u.Input
	f.sums[partOutput] += u.Output
	f.sums[partCacheRead] += u.CacheRead
	f.sums[partCacheWrite] += u.CacheWrite
	if rec.Message.Model != "" {
		f.model = rec.Message.Model
	}
}

// ---------- Codex ----------

type codexFile struct {
	size      int64
	mtime     time.Time
	sessionID string
	cwd       string
	// interactive is a thread a person started in a Codex front end, as
	// opposed to an `exec` run or a thread Codex opened for itself (a
	// subagent, a guardian review).
	interactive bool
	started     time.Time
	updated     time.Time
	model       string
	tokens      *[4]int64
	total       int64
	err         error
}

func (c *Collector) readCodex(cand candidate) {
	c.mu.Lock()
	prev := c.codex[cand.path]
	c.mu.Unlock()
	size, mtime := cand.info.Size(), cand.info.ModTime()
	if prev != nil && prev.size == size && prev.mtime.Equal(mtime) {
		return
	}
	f := &codexFile{size: size, mtime: mtime}
	f.err = f.read(c.open, cand.path)
	c.mu.Lock()
	c.codex[cand.path] = f
	c.mu.Unlock()
}

func (f *codexFile) read(open func(string) (*os.File, error), path string) error {
	fh, err := open(path)
	if err != nil {
		return err
	}
	head, err := bufio.NewReaderSize(io.LimitReader(fh, 8<<20), 256<<10).ReadBytes('\n')
	fh.Close()
	if err != nil && len(head) == 0 {
		return err
	}
	var meta struct {
		Timestamp string `json:"timestamp"`
		Type      string `json:"type"`
		Payload   struct {
			ID           string          `json:"id"`
			Timestamp    string          `json:"timestamp"`
			CWD          string          `json:"cwd"`
			Source       json.RawMessage `json:"source"`
			ThreadSource string          `json:"thread_source"`
		} `json:"payload"`
	}
	if json.Unmarshal(head, &meta) != nil || meta.Type != "session_meta" {
		return errors.New("no session_meta at the head of the record")
	}
	var source string
	_ = json.Unmarshal(meta.Payload.Source, &source)
	f.interactive = meta.Payload.ThreadSource == "user" && source != "exec"
	f.sessionID = meta.Payload.ID
	f.cwd = meta.Payload.CWD
	if t, ok := parseTime(meta.Payload.Timestamp); ok {
		f.started = t
	} else if t, ok := parseTime(meta.Timestamp); ok {
		f.started = t
	}
	f.updated = f.mtime

	if line, err := transcript.LastLineWith(path, []byte(`"type":"token_count"`), 16<<20); err == nil {
		var rec struct {
			Timestamp string `json:"timestamp"`
			Payload   struct {
				Info *struct {
					Total struct {
						Input      int64 `json:"input_tokens"`
						Cached     int64 `json:"cached_input_tokens"`
						CacheWrite int64 `json:"cache_write_input_tokens"`
						Output     int64 `json:"output_tokens"`
						Total      int64 `json:"total_tokens"`
					} `json:"total_token_usage"`
				} `json:"info"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &rec) == nil && rec.Payload.Info != nil {
			t := rec.Payload.Info.Total
			// Codex counts cached input inside input; the ledger's inputNew
			// is the part that was not cached.
			f.tokens = &[4]int64{max(t.Input-t.Cached, 0), t.Output, t.Cached, t.CacheWrite}
			f.total = t.Total
			if ts, ok := parseTime(rec.Timestamp); ok {
				f.updated = ts
			}
		}
	}
	if line, err := transcript.LastLineWith(path, []byte(`"type":"turn_context"`), 16<<20); err == nil {
		var rec struct {
			Payload struct {
				Model string `json:"model"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &rec) == nil {
			f.model = rec.Payload.Model
		}
	}
	return nil
}

// ---------- assembly ----------

// assemble turns what has been read into rows. Called with c.mu held.
func (c *Collector) assemble(since time.Time) []Row {
	tasks := c.tasksBySession()
	now := time.Now()
	out := []Row{}
	for path, f := range c.claude {
		if !since.IsZero() && f.mtime.Before(since) {
			continue
		}
		_, linked := tasks[f.sessionID]
		// What the Swift ledger can hold is a conversation it saw: one a
		// person or a dispatch opened in a terminal. A headless `claude -p`
		// (a naming call, a test harness) is never on its screen.
		if !linked && f.entrypoint != "cli" {
			continue
		}
		if !linked && f.err == nil && !f.found {
			// Nothing was ever measured here, and nothing says it was work.
			continue
		}
		row := Row{
			Assistant:   "claude",
			SessionID:   f.sessionID,
			WorkingDir:  f.cwd,
			ProjectKey:  c.projectKey(f.cwd),
			StartedAt:   f.first,
			UpdatedAt:   f.last,
			Model:       f.model,
			InputBasis:  "excludes_cache",
			IntervalKey: intervalKey("claude", f.sessionID, path),
		}
		if row.StartedAt.IsZero() {
			row.StartedAt = f.mtime
		}
		if row.UpdatedAt.IsZero() {
			row.UpdatedAt = row.StartedAt
		}
		open := now.Sub(f.mtime) < openWindow
		if !open {
			row.EndedAt = row.UpdatedAt
		}
		c.link(&row, tasks)
		switch {
		case f.err != nil:
			row.Coverage = "source_missing"
			row.CoverageReasons = []string{"source_unreadable"}
			row.MissingCost = "no_cost_recorded"
			row.InputBasis = ""
		case !f.found:
			row.Coverage = coverageFor(open)
			row.CoverageReasons = []string{"no_usage_recorded"}
			row.MissingCost = "no_cost_recorded"
		default:
			row.Coverage = coverageFor(open)
			row.CoverageReasons = []string{}
			t := f.sums
			for i := range t {
				v := t[i]
				row.Tokens[i] = &v
			}
			sum := t[0] + t[1] + t[2] + t[3]
			row.SourceTotal = &sum
			usage := transcript.Summary{Model: f.model, Input: t[partInputNew], Output: t[partOutput],
				CacheRead: t[partCacheRead], CacheWrite: t[partCacheWrite]}
			if cost, ok := transcript.Cost(usage); ok {
				row.CostValue = &cost
				row.CostUnit = "USD"
				row.CostBasis = "list_price_estimate"
				row.PriceSnapshotID = PriceSnapshotID
			} else if f.model == "" {
				row.MissingCost = "unknown_model"
			} else {
				row.MissingCost = "no_price_for_model"
			}
		}
		out = append(out, row)
	}
	for path, f := range c.codex {
		if !since.IsZero() && f.mtime.Before(since) {
			continue
		}
		_, linked := tasks[f.sessionID]
		if !linked && (f.err != nil || !f.interactive || f.tokens == nil) {
			continue
		}
		row := Row{
			Assistant:   "codex",
			SessionID:   f.sessionID,
			WorkingDir:  f.cwd,
			ProjectKey:  c.projectKey(f.cwd),
			StartedAt:   f.started,
			UpdatedAt:   f.updated,
			Model:       f.model,
			InputBasis:  "includes_cache",
			MissingCost: "plan_billed",
			IntervalKey: intervalKey("codex", f.sessionID, path),
		}
		if row.StartedAt.IsZero() {
			row.StartedAt = f.mtime
		}
		open := now.Sub(f.mtime) < openWindow
		if !open {
			row.EndedAt = row.UpdatedAt
		}
		c.link(&row, tasks)
		switch {
		case f.err != nil:
			row.Coverage = "source_missing"
			row.CoverageReasons = []string{"source_unreadable"}
			row.MissingCost = "no_cost_recorded"
			row.InputBasis = ""
		case f.tokens == nil:
			row.Coverage = coverageFor(open)
			row.CoverageReasons = []string{"no_usage_recorded"}
		default:
			row.Coverage = coverageFor(open)
			row.CoverageReasons = []string{}
			for i := range f.tokens {
				v := f.tokens[i]
				row.Tokens[i] = &v
			}
			total := f.total
			row.SourceTotal = &total
		}
		if row.Model == "" {
			row.MissingCost = "unknown_model"
		}
		out = append(out, row)
	}
	return out
}

// coverageFor is the Swift ledger's reading of an interval: sealed is
// complete, and one still being written is partial.
func coverageFor(open bool) string {
	if open {
		return "partial"
	}
	return "complete"
}

func intervalKey(assistant, session, part string) string {
	sum := sha256.Sum256([]byte(assistant + "\x00" + session + "\x00" + part))
	return hex.EncodeToString(sum[:])
}

type taskFacts struct {
	id, schedule, parent, rootLabel, landing string
	projectDir, commonDir                    string
	depth                                    int64
	verified                                 *bool
}

// tasksBySession maps each dispatched conversation to the Swift task that
// opened it. The Swift app's store is read, never written; a store that
// cannot be read leaves every row manual, which is what it would be without
// the Swift app.
func (c *Collector) tasksBySession() map[string]taskFacts {
	out := map[string]taskFacts{}
	if c.swift == nil {
		return out
	}
	snap := c.swift.Read()
	if !snap.Known {
		return out
	}
	for _, t := range snap.Tasks {
		if t.ChildSession == nil || *t.ChildSession == "" {
			continue
		}
		f := taskFacts{id: t.ID, depth: t.Depth}
		if t.ScheduleID != nil {
			f.schedule = strings.TrimSpace(*t.ScheduleID)
		}
		if t.ParentTask != nil {
			f.parent = *t.ParentTask
		}
		if t.RootLabel != nil {
			f.rootLabel = *t.RootLabel
		}
		f.projectDir = t.ProjectDir
		if t.Landing != nil {
			if t.Landing.RepositoryCommonDir != nil {
				f.commonDir = *t.Landing.RepositoryCommonDir
			}
			f.landing = t.Landing.State
			v := t.Landing.VerifiedCommit != nil && *t.Landing.VerifiedCommit != ""
			f.verified = &v
		}
		out[*t.ChildSession] = f
	}
	return out
}

func (c *Collector) link(row *Row, tasks map[string]taskFacts) {
	row.Origin = "manual"
	row.BoundaryKind = "session"
	row.BoundaryID = row.SessionID
	t, ok := tasks[row.SessionID]
	if !ok || row.SessionID == "" {
		return
	}
	row.TaskID = t.id
	// The Swift collector keys a task's interval on the task's project, which
	// outlives the worktree the task ran in.
	if key := c.taskProjectKey(t); key != "" {
		row.ProjectKey = key
	}
	row.BoundaryKind = "task"
	row.BoundaryID = t.id
	row.Depth = t.depth
	row.ParentTaskID = t.parent
	row.RootLabel = t.rootLabel
	row.LandingState = t.landing
	row.LandingVerified = t.verified
	row.Origin = "dispatch"
	if t.schedule != "" {
		row.Origin = "schedule"
		row.ScheduleID = t.schedule
	}
}

func (c *Collector) taskProjectKey(t taskFacts) string {
	if common := strings.TrimSpace(t.commonDir); common != "" {
		clean := filepath.Clean(common)
		if filepath.Base(clean) == ".git" {
			return filepath.Dir(clean)
		}
		if before, _, ok := strings.Cut(clean, "/.git/"); ok {
			return before
		}
	}
	return c.projectKey(t.projectDir)
}

// projectKey is `UsageLedger.canonicalProjectKey`, remembered per directory.
// Called with c.mu held.
func (c *Collector) projectKey(dir string) string {
	if dir == "" {
		return ""
	}
	if k, ok := c.keys[dir]; ok {
		return k
	}
	k := CanonicalProjectKey(dir)
	c.keys[dir] = k
	return k
}

// CanonicalProjectKey is the repository a directory belongs to: the directory
// holding `.git`, or for a linked worktree the main checkout its `commondir`
// names. A directory in no repository is its own key.
func CanonicalProjectKey(dir string) string {
	if !filepath.IsAbs(dir) {
		return ""
	}
	dir = filepath.Clean(dir)
	for d := dir; ; d = filepath.Dir(d) {
		dotgit := filepath.Join(d, ".git")
		if st, err := os.Stat(dotgit); err == nil {
			if st.IsDir() {
				return d
			}
			if common := worktreeCommon(d, dotgit); common != "" {
				return common
			}
			return d
		}
		if d == filepath.Dir(d) {
			break
		}
	}
	return dir
}

func worktreeCommon(dir, dotgit string) string {
	data, err := os.ReadFile(dotgit)
	if err != nil {
		return ""
	}
	line, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
	if !ok {
		return ""
	}
	gitdir := strings.TrimSpace(line)
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(dir, gitdir)
	}
	raw, err := os.ReadFile(filepath.Join(gitdir, "commondir"))
	if err != nil {
		return ""
	}
	common := strings.TrimSpace(string(raw))
	if !filepath.IsAbs(common) {
		common = filepath.Join(gitdir, common)
	}
	common = filepath.Clean(common)
	if st, err := os.Stat(common); err != nil || !st.IsDir() || filepath.Base(common) != ".git" {
		return ""
	}
	return filepath.Dir(common)
}

var legacyWorktree = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// LegacyWorktreeKey is `legacyManagedWorktreeTaskID` as a test: a Project key
// that is a managed worktree rather than a repository — under
// `/Clawdline/worktrees/`, ending in a task's UUID.
func LegacyWorktreeKey(path string) bool {
	clean := filepath.Clean(path)
	return strings.Contains(clean, "/Clawdline/worktrees/") && legacyWorktree.MatchString(filepath.Base(clean))
}

// LegacyWorktreeID is the task id in a managed worktree's working directory
// (`…/Clawdline/worktrees/<project>/<uuid>/…`), or "".
func LegacyWorktreeID(path string) string {
	clean := filepath.Clean(path)
	i := strings.Index(clean, "/Clawdline/worktrees/")
	if i < 0 {
		return ""
	}
	for _, part := range strings.Split(clean[i+len("/Clawdline/worktrees/"):], "/") {
		if legacyWorktree.MatchString(part) {
			return strings.ToLower(part)
		}
	}
	return ""
}

func parseTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// sortNewestFirst is the ledger's order: started, newest first, then the key.
func sortNewestFirst(rows []Row) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if !a.StartedAt.Equal(b.StartedAt) {
			return a.StartedAt.After(b.StartedAt)
		}
		return a.IntervalKey > b.IntervalKey
	})
}

// epoch is `timeIntervalSince1970`. A ledger row's start is a double in the
// ledger, and the whole seconds and the fraction are added separately so that
// double comes back exactly: a cursor carries it, and the Swift route's
// cursor carries it unrounded.
func epoch(t time.Time) float64 {
	return float64(t.Unix()) + float64(t.Nanosecond())/1e9
}
