package antigravitysession

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// Only the original submitted user input can create a receipt. Replay or an
// assistant quoting the same frame does not acknowledge a native prompt.
func nativeNoticeID(text string) string {
	text, ok := strings.CutPrefix(strings.TrimSpace(text), "<clawdline-notice>")
	if !ok {
		return ""
	}
	text, ok = strings.CutSuffix(text, "</clawdline-notice>")
	if !ok {
		return ""
	}
	var n struct {
		Protocol string `json:"protocol"`
		Kind     string `json:"kind"`
		NoticeID string `json:"notice_id"`
	}
	if json.Unmarshal([]byte(text), &n) != nil || n.Protocol != "clawdline.notice" || (n.Kind != "task_finished" && n.Kind != "task_reminder") {
		return ""
	}
	return n.NoticeID
}

// Called under the conversation lock only after this exact input returned
// end_turn and its history/state writes succeeded. Receipts survive replay's
// projection replacement; an unrelated later success cannot create one.
func (c *conversation) recordNoticeRead(text string) error {
	id := nativeNoticeID(text)
	if id == "" {
		return c.saveState()
	}
	if c.store != nil {
		// Clearing uncertainty and recording the exact notice read are one
		// commit. A failed receipt must not leave an idle row that can resend.
		tx, err := c.store.db.Begin()
		if err != nil {
			return refusal("history_unavailable")
		}
		defer tx.Rollback()
		if err = c.saveRow(tx); err == nil {
			_, err = tx.Exec(`INSERT INTO notice_reads(id,notice_id) VALUES(?,?) ON CONFLICT DO NOTHING`, c.row.ID, id)
		}
		if err == nil {
			err = tx.Commit()
		}
		if err != nil {
			return refusal("history_unavailable")
		}
		return nil
	}
	// Memory-only Hosts retain evidence only inside their existing view budget.
	for _, e := range c.entries {
		if e.Role == "user" && nativeNoticeID(e.Text) == id {
			if c.noticeReads == nil {
				c.noticeReads = map[string]bool{}
			}
			c.noticeReads[id] = true
			break
		}
	}
	return nil
}

// NoticeRead is local history evidence after an acknowledged native turn. A
// user echo alone is only a pipe-write receipt; an uncertain turn proves no read.
// This method never starts, authenticates, resumes or prompts a process.
func (h *Host) NoticeRead(ctx context.Context, row session.Session, noticeID string) (bool, error) {
	if noticeID == "" {
		return false, nil
	}
	c, err := h.lookup(row)
	if err != nil {
		return false, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hidden || c.busy || c.uncertain || c.row.State != session.StateIdle {
		return false, nil
	}
	if c.store == nil {
		return c.noticeReads[noticeID], nil
	}
	var found bool
	if err := c.store.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM notice_reads WHERE id=? AND notice_id=?)`, c.row.ID, noticeID).Scan(&found); err != nil {
		return false, refusal("history_unavailable")
	}
	return found, nil
}
