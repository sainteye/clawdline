package antigravitysession

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
	_ "modernc.org/sqlite"
)

// historyStore holds only daemon-owned identities. Native session discovery is
// not ownership evidence and cannot import an unrelated conversation here.
type historyStore struct{ db *sql.DB }

// Domain Session intentionally omits Activity from its JSON wire shape. Keep
// the activity clock explicitly in the storage record, independent of that wire.
type savedConversation struct {
	Row      session.Session  `json:"row"`
	Activity session.Activity `json:"activity"`
}

func openHistory(dir string) (*historyStore, error) {
	if !filepath.IsAbs(dir) {
		return nil, refusal("invalid_state_directory")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, refusal("history_unavailable")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, refusal("history_unavailable")
	}
	path := filepath.Join(dir, "conversations.sqlite")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, refusal("history_unavailable")
	}
	err = f.Chmod(0600)
	f.Close()
	if err != nil {
		return nil, refusal("history_unavailable")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, refusal("history_unavailable")
	}
	// Reads and short write transactions share one connection; no unbounded pool.
	db.SetMaxOpenConns(MaxHistoryConnections)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS conversations (
	 id TEXT PRIMARY KEY, native_id TEXT NOT NULL, cwd TEXT NOT NULL, profile TEXT NOT NULL,
	 row_json BLOB NOT NULL, generation TEXT NOT NULL, revision INTEGER NOT NULL,
	 ordinal INTEGER NOT NULL, uncertain INTEGER NOT NULL, hidden INTEGER NOT NULL,
	 UNIQUE(profile,cwd,native_id));
	 CREATE TABLE IF NOT EXISTS entries (
	 id TEXT NOT NULL, generation TEXT NOT NULL, ordinal INTEGER NOT NULL,
	 tool_id TEXT NOT NULL DEFAULT '', body BLOB NOT NULL,
	 PRIMARY KEY(id,generation,ordinal));
	 CREATE INDEX IF NOT EXISTS entry_tools ON entries(id,generation,tool_id);
	 DELETE FROM entries WHERE NOT EXISTS
	 (SELECT 1 FROM conversations c WHERE c.id=entries.id AND c.generation=entries.generation);`)
	if err != nil {
		db.Close()
		return nil, refusal("history_unavailable")
	}
	return &historyStore{db: db}, nil
}

func (s *historyStore) restore(profile string) (map[string]*conversation, error) {
	rows, err := s.db.Query(`SELECT row_json,generation,revision,ordinal,uncertain,hidden FROM conversations WHERE profile=?`, profile)
	if err != nil {
		return nil, refusal("history_unavailable")
	}
	defer rows.Close()
	out := map[string]*conversation{}
	for rows.Next() {
		c := &conversation{store: s, profile: profile, tools: map[string]int{}, textIndex: -1}
		var raw []byte
		if err := rows.Scan(&raw, &c.generation, &c.revision, &c.ordinal, &c.uncertain, &c.hidden); err != nil {
			return nil, refusal("history_unavailable")
		}
		var saved savedConversation
		if json.Unmarshal(raw, &saved) != nil {
			return nil, refusal("history_unavailable")
		}
		if saved.Row.ID == "" { // Read the initial flat record shape as well.
			if json.Unmarshal(raw, &c.row) != nil || c.row.ID == "" {
				return nil, refusal("history_unavailable")
			}
		} else {
			c.row = saved.Row
			c.row.Activity = saved.Activity
		}
		c.row.PID = 0
		if c.row.State == session.StateWorking {
			c.uncertain = true
		}
		if c.uncertain {
			c.row.State = session.StateUnknown
		} else {
			c.row.State = session.StateIdle
		}
		out[c.row.ID] = c
	}
	if rows.Err() != nil {
		return nil, refusal("history_unavailable")
	}
	return out, nil
}

type historyWriter interface {
	Exec(string, ...any) (sql.Result, error)
}

func (c *conversation) saveRow(w historyWriter) error {
	if c.staging {
		return nil
	}
	raw, err := json.Marshal(savedConversation{Row: c.row, Activity: c.row.Activity})
	if err == nil {
		_, err = w.Exec(`INSERT INTO conversations(id,native_id,cwd,profile,row_json,generation,revision,ordinal,uncertain,hidden)
		 VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET row_json=excluded.row_json,generation=excluded.generation,
		 revision=excluded.revision,ordinal=excluded.ordinal,uncertain=excluded.uncertain,hidden=excluded.hidden`,
			c.row.ID, c.row.ConversationID, c.row.CWD, c.profile, raw, c.generation, c.revision, c.ordinal, c.uncertain, c.hidden)
	}
	if err != nil {
		return refusal("history_unavailable")
	}
	return nil
}

func (c *conversation) saveState() error {
	if c.store == nil {
		return nil
	}
	if err := c.saveRow(c.store.db); err != nil {
		c.storeErr = err
		return err
	}
	return nil
}

// Each streaming update upserts one ordinal, never the entire conversation.
func (c *conversation) saveEntry(ordinal int64, tool string, e contract.TranscriptEntry) error {
	e.ID = c.entryID(ordinal)
	raw, err := json.Marshal(e)
	if err != nil {
		return refusal("history_unavailable")
	}
	tx, err := c.store.db.Begin()
	if err != nil {
		return refusal("history_unavailable")
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO entries(id,generation,ordinal,tool_id,body) VALUES(?,?,?,?,?)
	 ON CONFLICT(id,generation,ordinal) DO UPDATE SET body=excluded.body,tool_id=excluded.tool_id`, c.row.ID, c.generation, ordinal, tool, raw)
	if err == nil {
		err = c.saveRow(tx)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		c.storeErr = refusal("history_unavailable")
		return c.storeErr
	}
	return nil
}

func (c *conversation) entryID(ordinal int64) string {
	return "acp2:" + c.generation + ":" + strconv.FormatInt(ordinal, 10)
}

func (c *conversation) readEntry(ordinal int64, tool string) (int64, contract.TranscriptEntry, error) {
	var raw []byte
	var err error
	if ordinal > 0 {
		err = c.store.db.QueryRow(`SELECT ordinal,body FROM entries WHERE id=? AND generation=? AND ordinal=?`, c.row.ID, c.generation, ordinal).Scan(&ordinal, &raw)
	} else {
		err = c.store.db.QueryRow(`SELECT ordinal,body FROM entries WHERE id=? AND generation=? AND tool_id=? ORDER BY ordinal DESC LIMIT 1`, c.row.ID, c.generation, tool).Scan(&ordinal, &raw)
	}
	var e contract.TranscriptEntry
	if err == sql.ErrNoRows {
		return 0, e, nil
	}
	if err != nil || json.Unmarshal(raw, &e) != nil {
		return 0, e, refusal("history_unavailable")
	}
	return ordinal, e, nil
}

// Past is a locally verified conversation identity, not a provider listing.
type Past struct {
	ID    string
	Title string
	At    time.Time
	Live  bool
}

type pastCursor struct {
	At time.Time `json:"at"`
	ID string    `json:"id"`
}

func (h *Host) Past(ctx context.Context, cwd, cursor string, limit int) ([]Past, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if !filepath.IsAbs(cwd) {
		return nil, "", refusal("invalid_cwd")
	}
	var after pastCursor
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || json.Unmarshal(raw, &after) != nil || after.ID == "" {
			return nil, "", refusal("invalid_cursor")
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []Past{}
	for _, c := range h.rows {
		c.mu.Lock()
		if c.row.CWD == filepath.Clean(cwd) {
			title := c.row.Label
			if title == "" {
				title = c.row.ConversationID
			}
			out = append(out, Past{ID: c.row.ConversationID, Title: title, At: c.row.Activity.At, Live: !c.hidden && c.client != nil && c.client.Err() == nil})
		}
		c.mu.Unlock()
	}
	// Recent activity sorts first; the immutable native ID breaks equal-time ties.
	slices.SortFunc(out, func(a, b Past) int {
		if d := b.At.Compare(a.At); d != 0 {
			return d
		}
		return strings.Compare(b.ID, a.ID)
	})
	if cursor != "" {
		out = slices.DeleteFunc(out, func(p Past) bool { return p.At.After(after.At) || p.At.Equal(after.At) && p.ID >= after.ID })
	}
	if limit <= 0 {
		limit = MaxViewBytes
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		raw, _ := json.Marshal(pastCursor{At: last.At, ID: last.ID})
		next = base64.RawURLEncoding.EncodeToString(raw)
	}
	return out, next, nil
}

func (h *Host) TranscriptPage(ctx context.Context, s session.Session, limit int, before int64, revision string) (contract.TranscriptPage, error) {
	c, err := h.lookup(s)
	if err != nil {
		return contract.TranscriptPage{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.store == nil {
		return c.memoryTranscript(s, limit), nil
	}
	page := contract.TranscriptPage{ID: s.ID, Entries: []contract.TranscriptEntry{}, Evidence: contract.EvidenceStructured, Signature: "acp2:" + c.generation + ":" + strconv.FormatUint(c.revision, 10)}
	if before < 0 {
		return page, refusal("invalid_cursor")
	}
	if before > 0 {
		parts := strings.Split(revision, ":")
		if len(parts) != 3 || parts[0] != "acp2" || parts[1] != c.generation {
			return page, refusal("cursor_stale")
		}
		if _, err := strconv.ParseUint(parts[2], 10, 64); err != nil {
			return page, refusal("cursor_stale")
		}
	}
	if before == 0 {
		before = c.ordinal + 1
	}
	if limit <= 0 {
		limit = MaxViewBytes
	}
	page.ReadBefore = before
	rows, err := c.store.db.QueryContext(ctx, `SELECT ordinal,body FROM entries WHERE id=? AND generation=? AND ordinal<? ORDER BY ordinal DESC LIMIT ?`, c.row.ID, c.generation, before, limit)
	if err != nil {
		return page, refusal("history_unavailable")
	}
	defer rows.Close()
	bytes := 2
	var oldest int64
	for rows.Next() {
		var ordinal int64
		var raw []byte
		if rows.Scan(&ordinal, &raw) != nil {
			return page, refusal("history_unavailable")
		}
		var e contract.TranscriptEntry
		if json.Unmarshal(raw, &e) != nil {
			return page, refusal("history_unavailable")
		}
		e.ID = c.entryID(ordinal)
		raw, _ = json.Marshal(e)
		if bytes+len(raw)+1 > MaxViewBytes {
			if len(page.Entries) == 0 {
				oldest = ordinal
				page.Truncation = &contract.TranscriptTruncation{Reason: "transcript_byte_budget", EntriesOmittedCount: 1, BudgetBytes: MaxViewBytes}
			}
			break
		}
		page.Entries = append(page.Entries, e)
		bytes += len(raw) + 1
		oldest = ordinal
	}
	if rows.Err() != nil {
		return page, refusal("history_unavailable")
	}
	if oldest > 1 {
		page.NextBefore = oldest
	}
	slices.Reverse(page.Entries)
	return page, nil
}
