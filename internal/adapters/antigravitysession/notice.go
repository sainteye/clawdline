package antigravitysession

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func hasNativeNotice(text, noticeID string) bool {
	text, ok := strings.CutPrefix(strings.TrimSpace(text), "<clawdline-notice>")
	if !ok {
		return false
	}
	text, ok = strings.CutSuffix(text, "</clawdline-notice>")
	if !ok {
		return false
	}
	var notice struct {
		Protocol string `json:"protocol"`
		Kind     string `json:"kind"`
		NoticeID string `json:"notice_id"`
	}
	return json.Unmarshal([]byte(text), &notice) == nil && notice.Protocol == "clawdline.notice" &&
		(notice.Kind == "task_finished" || notice.Kind == "task_reminder") && notice.NoticeID == noticeID
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
		for _, entry := range c.entries {
			if entry.Role == "user" && hasNativeNotice(entry.Text, noticeID) {
				return true, nil
			}
		}
		return false, nil
	}
	rows, err := c.store.db.QueryContext(ctx, `SELECT body FROM entries
	 WHERE id=? AND generation=? AND json_extract(body,'$.role')='user'
	 AND instr(json_extract(body,'$.text'),?)>0`, c.row.ID, c.generation, noticeID)
	if err != nil {
		return false, refusal("history_unavailable")
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var entry contract.TranscriptEntry
		if rows.Scan(&raw) != nil || json.Unmarshal(raw, &entry) != nil {
			return false, refusal("history_unavailable")
		}
		if hasNativeNotice(entry.Text, noticeID) {
			return true, nil
		}
	}
	if rows.Err() != nil {
		return false, refusal("history_unavailable")
	}
	return false, nil
}
