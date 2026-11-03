package antigravitysession

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func (c *conversation) toolEntryReference(ordinal int64, e contract.TranscriptEntry) string {
	raw, _ := json.Marshal(e)
	sum := sha256.Sum256(append([]byte("acp-entry-v1\x00"+c.row.ID+"\x00"+c.row.ConversationID+"\x00"+c.profile+"\x00"+c.generation+"\x00"+strconv.FormatInt(ordinal, 10)+"\x00"), raw...))
	return strconv.FormatInt(ordinal, 10) + ":" + hex.EncodeToString(sum[:])
}

// Use the same inline/preview budgets as the other transcript readers. Full
// output remains in SQLite; a small page never becomes a retention policy.
func (c *conversation) compactToolEntry(ordinal int64, e contract.TranscriptEntry) contract.TranscriptEntry {
	if e.Role != "tool" || e.Activity == nil {
		return e
	}
	size := len(e.Activity.Result)
	for _, change := range e.FileChanges {
		size += len(change.Content) + len(change.UnifiedDiff)
	}
	if size <= int(capacity.Default("session.inline_tool_bytes")) {
		return e
	}
	e.Detail = &contract.TranscriptEntryDetail{Reference: c.toolEntryReference(ordinal, e), TextBytes: int64(size)}
	a := *e.Activity
	text := []rune(a.Result)
	if limit := int(capacity.Default("session.tool_preview_runes")); len(text) > limit {
		a.Result = string(text[:limit])
	}
	e.Activity = &a
	e.FileChanges = nil
	return e
}

// ToolEntry reads an exact locally retained body. Its digest binds the Session,
// profile, history generation and content, including updates to an old ordinal.
// It never launches a provider, reads a path supplied by a tool, or repairs a
// stale reference by choosing another row.
func (h *Host) ToolEntry(ctx context.Context, row session.Session, reference string) (contract.TranscriptEntry, error) {
	c, err := h.lookup(row)
	if err != nil {
		return contract.TranscriptEntry{}, err
	}
	parts := strings.Split(reference, ":")
	if len(parts) != 2 || len(parts[1]) != sha256.Size*2 {
		return contract.TranscriptEntry{}, refusal("cursor_stale")
	}
	ordinal, err := strconv.ParseInt(parts[0], 10, 64)
	if _, bad := hex.DecodeString(parts[1]); err != nil || ordinal < 1 || bad != nil {
		return contract.TranscriptEntry{}, refusal("cursor_stale")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return contract.TranscriptEntry{}, err
	}
	if c.store == nil || c.storeErr != nil {
		return contract.TranscriptEntry{}, refusal("history_unavailable")
	}
	_, e, err := c.readEntry(ordinal, "")
	if err != nil || e.Role != "tool" || c.toolEntryReference(ordinal, e) != reference {
		return contract.TranscriptEntry{}, refusal("cursor_stale")
	}
	return e, nil
}
