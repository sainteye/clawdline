package projectassistantacp

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type nativeCompaction struct {
	result   app.NativeCompactionResult
	started  bool
	terminal bool
}

type pendingCompaction struct {
	c      *Client
	rpc    *pendingRPC
	once   sync.Once
	result app.NativeCompactionResult
	err    error
}

var _ app.NativeCompactor = (*Client)(nil)

func (c *Client) BeginCompaction(ctx context.Context, fence pa.Fence) (app.PendingNativeCompaction, error) {
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(refusal(pa.Revoked, "compaction_cancelled"), err)
	}
	c.mu.Lock()
	if err := pa.CheckFence(fence, c.fence); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	if c.failure != nil {
		err := c.failure
		c.mu.Unlock()
		return nil, err
	}
	if c.cancelled || c.permissionsSealed || c.compaction != nil {
		c.mu.Unlock()
		return nil, refusal(pa.Revoked, "compaction_lifetime")
	}
	if c.turnID != "" {
		c.mu.Unlock()
		return nil, refusal(pa.CapacityFull, "active_prompt")
	}
	sid := c.sessionID
	if sid == "" || c.nativeContext.NativeSessionID != sid || !c.nativeContext.CommandsKnown || !c.nativeContext.CompactAdvertised {
		c.mu.Unlock()
		return nil, refusal(pa.Unsupported, "native_compaction")
	}
	// Reserve the whole client lifetime before the external write. Even a
	// failed or uncertain attempt cannot become a second maintenance write.
	c.compaction = &nativeCompaction{result: app.NativeCompactionResult{Fence: fence, NativeSessionID: sid, State: app.NativeCompactionUnknown}}
	c.mu.Unlock()
	// The adapter recognizes only a bare command. No instructions, memory,
	// handoff, attachments or arbitrary caller text enter this maintenance RPC.
	rpc, err := c.begin(ctx, "session/prompt", map[string]any{"sessionId": sid, "prompt": []any{map[string]string{"type": "text", "text": "/compact"}}})
	if err != nil {
		return nil, err
	}
	return &pendingCompaction{c: c, rpc: rpc}, nil
}

func (c *Client) updateNativeCompaction(sessionID, id, status string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if sessionID == "" || c.sessionID != "" && sessionID != c.sessionID {
		return refusal(pa.StaleGeneration, "native_session")
	}
	op := c.compaction
	if op == nil {
		// Not Clawdline's maintenance write: the agent compacted by itself.
		c.observeCompletedCompactionLocked(status)
		return nil
	}
	if c.cancelled || c.failure != nil || op.terminal {
		return nil
	}
	if id == "" || op.started && id != op.result.ID {
		return refusal(pa.ExecutionEffectUnknown, "compaction_identity")
	}
	if status == "in_progress" {
		op.result.ID, op.started = id, true
		return nil
	}
	if !op.started {
		return refusal(pa.ExecutionEffectUnknown, "compaction_start_missing")
	}
	switch app.NativeCompactionState(status) {
	case app.NativeCompactionCompleted, app.NativeCompactionFailed, app.NativeCompactionCancelled:
		op.result.State = app.NativeCompactionState(status)
		op.terminal = true
		return nil
	default:
		return refusal(pa.ExecutionEffectUnknown, "compaction_status")
	}
}

func (p *pendingCompaction) Wait(ctx context.Context) (app.NativeCompactionResult, error) {
	p.once.Do(func() { p.result, p.err = p.wait(ctx) })
	return p.result, p.err
}

func (p *pendingCompaction) wait(ctx context.Context) (app.NativeCompactionResult, error) {
	b, err := p.rpc.waitResult(ctx)
	p.c.mu.Lock()
	op := *p.c.compaction
	cancelled := p.c.cancelled
	p.c.mu.Unlock()
	var reply struct {
		StopReason string `json:"stopReason"`
	}
	if err == nil && (json.Unmarshal(b, &reply) != nil || reply.StopReason != "end_turn") {
		err = refusal(pa.ExecutionEffectUnknown, "compaction_result")
	}
	if err == nil && (cancelled || !op.terminal) {
		err = refusal(pa.ExecutionEffectUnknown, "compaction_terminal")
	}
	if err != nil {
		op.result.State = app.NativeCompactionUnknown
		return op.result, errors.Join(refusal(pa.ExecutionEffectUnknown, "native_compaction"), err)
	}
	return op.result, nil
}
