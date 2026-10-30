package antigravitysession

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"math"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func (c *conversation) imageRecord() string {
	sum := sha256.Sum256([]byte("owned-image-v1\x00" + c.row.ID + "\x00" + c.row.ConversationID + "\x00" + c.row.CWD + "\x00" + c.profile + "\x00" + c.generation))
	return "owned-image-v1:" + hex.EncodeToString(sum[:]) + ":" + c.generation
}

// ImageAddress is an opaque history address and next native ordinal, never a
// filesystem path or a wall-clock guess. It does not launch or resume a process.
func (h *Host) ImageAddress(ctx context.Context, row session.Session) (string, int64, error) {
	c, err := h.lookup(row)
	if err != nil {
		return "", 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	if c.store == nil || c.storeErr != nil || c.ordinal == math.MaxInt64 {
		return "", 0, refusal("history_unavailable")
	}
	return c.imageRecord(), c.ordinal + 1, nil
}

// ImageReplyOrdinal binds a shown picture to its first later assistant row.
// Reloaded history with another generation cannot borrow an old ordinal.
func (h *Host) ImageReplyOrdinal(ctx context.Context, row session.Session, record string, after int64) (int64, error) {
	c, err := h.lookup(row)
	if err != nil {
		return -1, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.store == nil || c.storeErr != nil || after < 1 || record != c.imageRecord() {
		return -1, refusal("identity_changed")
	}
	var ordinal int64
	err = c.store.db.QueryRowContext(ctx, `SELECT ordinal FROM entries WHERE id=? AND generation=? AND ordinal>=? AND json_extract(body,'$.role')='assistant' ORDER BY ordinal LIMIT 1`, c.row.ID, c.generation, after).Scan(&ordinal)
	if err == sql.ErrNoRows {
		return -1, nil
	}
	if err != nil {
		return -1, refusal("history_unavailable")
	}
	return ordinal, nil
}
