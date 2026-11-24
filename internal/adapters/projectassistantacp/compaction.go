package projectassistantacp

import (
	"context"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// An agent's own compaction, observed outside Clawdline's maintenance write.
//
// Claude Code's and Codex's ACP adapters compact a full context by
// themselves. Two wire facts report it: a `compaction_update` whose status is
// "completed", and, for an adapter that never sends one, a `usage_update`
// whose used tokens fell below half of a previous reading of at least
// compactionDropFloor (Codync host/src/agent/bot/compaction.rs#L379-L394).
// Nothing is inferred from elapsed time or message counts.
//
// The observation is reported through NativeContext. A Client lives for one
// turn, so the engine owns what follows: it writes one persistent
// "summarized" notice for that turn, and the next turn on the same binding
// sends PromptRequest.Compacted, which repeats memory once (see
// docs/bots.md). Two wire signals for one compaction count once.

const (
	// compactionDropFloor is the smallest previous reading a halved usage
	// report may be read as a compaction from (Codync's COMPACTED_FROM). It
	// is a detection threshold, not a capacity bound: nothing is limited by it.
	compactionDropFloor int64 = 20_000
)

type observedCompactions struct {
	// turnID is the prompt during which the last compaction was observed;
	// count, before and after describe that turn only.
	turnID        string
	count         int
	before, after int64
	// awaitUsage: a completed compaction_update was counted, and the next
	// usage reading belongs to it rather than to a second compaction.
	awaitUsage bool
	// reportsCompaction: this connection sends compaction_update, so a usage
	// drop is never read as a second signal (Codync updates.rs, track_usage).
	reportsCompaction bool
}

// observeCompactionLocked records one compaction. The caller holds c.mu.
func (c *Client) observeCompactionLocked(before, after int64) {
	o := &c.observed
	if o.turnID != c.turnID {
		o.turnID, o.count, o.before, o.after = c.turnID, 0, 0, 0
	}
	if o.count == 0 {
		o.before = before
	}
	o.count++
	o.after = after
}

// observeCompletedCompactionLocked handles a compaction_update outside
// Clawdline's own maintenance operation. The caller holds c.mu.
func (c *Client) observeCompletedCompactionLocked(status string) {
	c.observed.reportsCompaction = true
	if status != "completed" || c.failure != nil || c.cancelled {
		return
	}
	before := int64(0)
	if c.nativeContext.UsageKnown {
		before = c.nativeContext.UsedTokens
	}
	c.observeCompactionLocked(before, 0)
	c.observed.awaitUsage = true
}

// observeUsageLocked compares a new usage reading with the previous one of
// the same session. The caller holds c.mu and has not yet replaced it.
func (c *Client) observeUsageLocked(prevKnown bool, prev, used int64) {
	if c.observed.awaitUsage {
		c.observed.awaitUsage = false
		if c.observed.turnID == c.turnID {
			c.observed.after = used
		}
		return
	}
	if !c.observed.reportsCompaction && prevKnown && prev >= compactionDropFloor && used*2 < prev {
		c.observeCompactionLocked(prev, used)
	}
}

// compactionRedelivery is the context repeated on the first prompt after a
// compaction. Memory uses the one renderer a resumed session already uses
// when memory changed, over the same filtered snapshot a fresh session's
// opening is built from. Instructions need no copy here: Claude Code and
// Codex hold them in the system prompt, which their compaction keeps, and
// Gemini hears them on every open.
func compactionRedelivery(memory string) string {
	return "<context_restored>\nYour earlier context was summarized to make room. Your memory is repeated here so the summary does not lose it.\n</context_restored>\n" + memoryUpdate(memory)
}

// compactKeep is Codync's keep-list for a host-requested compaction
// (host/crates/memory/src/compaction.rs#L176-L178). Claude Code reads text
// after /compact as instructions; Codex ignores it, so Codex gets the bare
// command (Codync host/src/agent/bot/compaction.rs#L114-L121).
const compactKeep = "Keep verbatim: the latest request, and every instruction not to do something. Keep exact paths, URLs and ids, decisions with their reasons, and open tasks."

// sendCompact sends the person's "Compact now" as the agent's own command and
// nothing else: no instructions, memory, quote, files or reminders, so the
// agent recognizes it as a command. The adapter still reports the result
// through compaction_update, which is counted as the agent's own compaction.
func (c *Client) sendCompact(ctx context.Context, q app.PromptRequest) (app.PendingPrompt, error) {
	if q.Turn.Origin != pa.CompactOrigin || q.Handoff != nil || q.Quote != nil || len(q.Images) > 0 || len(q.Files) > 0 {
		return nil, refusal(pa.ScopeDenied, "compact_prompt")
	}
	text := "/compact"
	switch c.agent {
	case pa.ClaudeCode:
		text += " " + compactKeep
	case pa.Codex:
	default:
		return nil, refusal(pa.Unsupported, "compact_agent")
	}
	deadline, _ := ctx.Deadline()
	c.mu.Lock()
	if c.compaction != nil {
		c.mu.Unlock()
		return nil, refusal(pa.Revoked, "compaction_lifetime")
	}
	if c.permissionsSealed {
		c.mu.Unlock()
		return nil, refusal(pa.Revoked, "prompt_lifetime")
	}
	if c.turnID != "" {
		c.mu.Unlock()
		return nil, refusal(pa.CapacityFull, "active_prompt")
	}
	// An agent that listed its commands without compact would read the text
	// as an ordinary message; nothing is sent then.
	if c.nativeContext.CommandsKnown && !c.nativeContext.CompactAdvertised {
		c.mu.Unlock()
		return nil, refusal(pa.Unsupported, "native_compaction")
	}
	c.turnID = q.Turn.ID
	c.promptDeadline = deadline
	c.partial, c.segmentStart, c.segmentClosed, c.messageID = "", 0, false, ""
	c.answerStart, c.toolSinceText = 0, false
	c.traceSegment, c.traceTools = pa.Trace{}, nil
	sid := c.sessionID
	c.mu.Unlock()
	p, err := c.begin(ctx, "session/prompt", map[string]any{"sessionId": sid, "prompt": []any{map[string]string{"type": "text", "text": text}}})
	if err != nil {
		return nil, err
	}
	return &promptPending{rpc: p, deadline: deadline}, nil
}
