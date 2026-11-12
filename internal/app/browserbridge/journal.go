package browserbridge

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// MaxAuditTextRunes bounds the typed text one audit row keeps; longer text is
// cut and marked, so the journal stays bounded in bytes as well as rows.
const MaxAuditTextRunes = 2000

// JournalFile is the audit's file in the daemon's directory.
const JournalFile = "browser-audit.jsonl"

// journal keeps the audit across restarts: one JSON row per line, appended,
// rewritten to the newest MaxAuditRows rows when it reaches twice that. The
// first line of a rewritten file records how many rows were pruned before it.
type journal struct {
	mu    sync.Mutex
	path  string
	lines int
	err   error
}

type prunedHeader struct {
	Pruned int `json:"pruned"`
}

// load reads the journal, newest last, and how many rows were pruned. A
// missing file is an empty audit; a damaged line is skipped, not fatal.
func (j *journal) load() ([]AuditRow, int, error) {
	f, err := os.Open(j.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	var rows []AuditRow
	pruned := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	first := true
	for sc.Scan() {
		j.lines++
		line := sc.Bytes()
		if first {
			first = false
			var h prunedHeader
			if json.Unmarshal(line, &h) == nil && h.Pruned > 0 {
				pruned = h.Pruned
				continue
			}
		}
		var row AuditRow
		if json.Unmarshal(line, &row) == nil && row.At != 0 {
			rows = append(rows, row)
		}
	}
	if len(rows) > MaxAuditRows {
		pruned += len(rows) - MaxAuditRows
		rows = rows[len(rows)-MaxAuditRows:]
	}
	return rows, pruned, sc.Err()
}

// append writes one row, and rewrites the file when it has grown to twice
// the kept rows. keep is the in-memory audit after the row was added.
func (j *journal) append(row AuditRow, keep []AuditRow, pruned int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	data, err := json.Marshal(row)
	if err != nil {
		return
	}
	if j.lines+1 >= 2*MaxAuditRows {
		j.err = j.rewrite(keep, pruned)
		return
	}
	f, err := os.OpenFile(j.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		j.err = err
		return
	}
	defer f.Close()
	if _, err = f.Write(append(data, '\n')); err != nil {
		j.err = err
		return
	}
	j.lines++
	j.err = nil
}

func (j *journal) rewrite(rows []AuditRow, pruned int) error {
	tmp, err := os.CreateTemp(filepath.Dir(j.path), ".browser-audit-*")
	if err != nil {
		return err
	}
	w := bufio.NewWriter(tmp)
	lines := 0
	if pruned > 0 {
		h, _ := json.Marshal(prunedHeader{Pruned: pruned})
		w.Write(append(h, '\n'))
		lines++
	}
	for _, r := range rows {
		data, _ := json.Marshal(r)
		w.Write(append(data, '\n'))
		lines++
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), j.path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	j.lines = lines
	return nil
}

// Err is the last write failure, or nil; the audit route reports it.
func (j *journal) Err() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.err
}

// Open makes a bridge whose audit is kept in dir, loading what is there.
func Open(dir string, onSite func(NewSite)) (*Bridge, error) {
	b := New(onSite)
	j := &journal{path: filepath.Join(dir, JournalFile)}
	rows, pruned, err := j.load()
	b.audit, b.pruned, b.journal = rows, pruned, j
	if terr := b.loadTabs(filepath.Join(dir, TabsFile)); err == nil {
		err = terr
	}
	return b, err
}

// JournalErr is the audit file's last write failure, or nil when the audit
// is kept only in memory or every write succeeded.
func (b *Bridge) JournalErr() error {
	if b.journal == nil {
		return nil
	}
	return b.journal.Err()
}

func cutText(s string) (string, bool) {
	r := []rune(s)
	if len(r) <= MaxAuditTextRunes {
		return s, false
	}
	return string(r[:MaxAuditTextRunes]), true
}
