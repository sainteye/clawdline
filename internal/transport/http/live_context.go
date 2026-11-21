package http

import (
	"math"
	"os"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// LiveContextRefreshSecondsLimit is how often one Session's window reading is
// renewed for the list. The window fills over minutes, and a working record
// changes every second, so without it each list pass would reread every
// working conversation's tail.
const LiveContextRefreshSecondsLimit = 15

// MaxLiveContextRows is how many Sessions' readings are kept. A Session that
// leaves the list is forgotten at the next renewal past the bound.
const MaxLiveContextRows = 256

// liveContext answers how full each Session's context window is, for the list
// row, from a short tail of its record at most every LiveContextRefreshSecondsLimit.
type liveContext struct {
	mu   sync.Mutex
	rows map[string]liveContextRow
	now  func() time.Time
}

type liveContextRow struct {
	at      time.Time
	size    int64
	mod     int64
	percent int64
	known   bool
}

func newLiveContext() *liveContext {
	return &liveContext{rows: map[string]liveContextRow{}, now: time.Now}
}

// percent is the used share of the window, 0…100, or false when it cannot be
// stated: no record, an assistant whose record names no usage, or no window.
func (l *liveContext) percent(s *Server, item session.Session) (int64, bool) {
	if l == nil || (item.Assistant != session.AssistantClaude && item.Assistant != session.AssistantCodex) {
		return 0, false
	}
	path := s.recordPath(item)
	var size, mod int64
	if path != "" {
		if st, err := os.Stat(path); err == nil {
			size, mod = st.Size(), st.ModTime().UnixNano()
		} else {
			path = ""
		}
	}
	now := l.now()
	l.mu.Lock()
	held, ok := l.rows[item.ID]
	l.mu.Unlock()
	if ok && (held.size == size && held.mod == mod || now.Sub(held.at) < LiveContextRefreshSecondsLimit*time.Second) {
		return held.percent, held.known
	}
	var status transcript.ClaudeStatusLine
	if item.Assistant == session.AssistantClaude {
		if home, err := os.UserHomeDir(); err == nil {
			status = transcript.ReadClaudeStatusLine(home, item.ConversationID)
		}
	}
	row := liveContextRow{at: now, size: size, mod: mod}
	if at, ok := transcript.LiveContext(path, string(item.Assistant), status); ok && !math.IsNaN(at.UsedPercent) {
		row.percent, row.known = int64(math.Ceil(min(100, max(0, at.UsedPercent)))), true
	}
	l.mu.Lock()
	if _, present := l.rows[item.ID]; !present && len(l.rows) >= MaxLiveContextRows {
		oldest, first := "", time.Time{}
		for id, r := range l.rows {
			if oldest == "" || r.at.Before(first) {
				oldest, first = id, r.at
			}
		}
		delete(l.rows, oldest)
	}
	l.rows[item.ID] = row
	l.mu.Unlock()
	return row.percent, row.known
}

// count is how many Sessions' readings are held, for the capacity report.
func (l *liveContext) count() int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.rows)
}
