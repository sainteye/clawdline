package antigravitysession

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/antigravityprocess"
)

// NativePast is an explicit older-history read, not a startup probe. Saved
// local history remains available without it. One native page is bounded by
// the existing ACP frame budget and carries the provider's own continuation.
func (h *Host) NativePast(ctx context.Context, cwd, cursor string) ([]Past, string, error) {
	if !filepath.IsAbs(cwd) {
		return nil, "", refusal("invalid_cwd")
	}
	cwd = filepath.Clean(cwd)
	h.mu.Lock()
	if h.closed || h.ctx.Err() != nil || h.opening {
		h.mu.Unlock()
		return nil, "", refusal("unavailable")
	}
	h.opening = true
	h.jobs.Add(1)
	h.mu.Unlock()
	defer h.jobs.Done()
	defer func() { h.mu.Lock(); h.opening = false; h.mu.Unlock() }()
	ctx, cancel := context.WithTimeout(ctx, MaxCallTime)
	defer cancel()
	client, err := antigravityprocess.Start(h.ctx, antigravityprocess.Options{Executable: h.opts.Executable, Args: h.opts.Args, AccountDir: h.opts.AccountDir, WorkingDir: cwd, MaxFrameBytes: MaxFrameBytes, CallTimeout: MaxCallTime})
	if err != nil {
		return nil, "", err
	}
	defer client.Close()
	if _, err = client.Initialize(ctx); err != nil {
		return nil, "", err
	}
	if _, err = client.AuthenticatePersonal(ctx); err != nil {
		return nil, "", err
	}
	raw, err := client.ListSessions(ctx, cursor)
	if err != nil {
		return nil, "", err
	}
	var reply struct {
		Sessions []struct {
			SessionID string `json:"sessionId"`
			CWD       string `json:"cwd"`
			Title     string `json:"title"`
			UpdatedAt string `json:"updatedAt"`
		} `json:"sessions"`
		NextCursor string `json:"nextCursor"`
	}
	if json.Unmarshal(raw, &reply) != nil || reply.Sessions == nil || (reply.NextCursor != "" && reply.NextCursor == cursor) {
		return nil, "", refusal("native_history_incomplete")
	}
	out := []Past{}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, item := range reply.Sessions {
		if item.SessionID == "" || item.CWD != cwd {
			continue
		}
		at, _ := time.Parse(time.RFC3339, item.UpdatedAt)
		row := Past{ID: item.SessionID, Title: item.Title, At: at}
		if row.Title == "" {
			row.Title = item.SessionID
		}
		for _, c := range h.rows {
			c.mu.Lock()
			if c.row.CWD == cwd && c.row.ConversationID == row.ID {
				row.At = c.row.Activity.At
				row.Live = !c.hidden && c.client != nil && c.client.Err() == nil
				if c.row.Label != "" {
					row.Title = c.row.Label
				}
			}
			c.mu.Unlock()
		}
		out = append(out, row)
	}
	return out, reply.NextCursor, nil
}
