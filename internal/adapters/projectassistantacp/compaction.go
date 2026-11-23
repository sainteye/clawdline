package projectassistantacp

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
