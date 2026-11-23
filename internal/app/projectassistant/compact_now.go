package projectassistant

import (
	"context"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// "Compact now" is one ordinary, durably journaled turn whose Origin is
// pa.CompactOrigin and whose only prompt is the agent's own /compact command.
// Riding the turn path, rather than the native maintenance port, keeps the
// receipt, idempotency key, queue, Stop and restart recovery a chat message
// already has, and the Client still lives for that one turn: the next message
// opens a new one, so a compaction never reserves a client the person needs.
// The agent's compaction_update then produces the same "summarized" notice
// and memory redelivery as an automatic compaction (compaction_notice.go).
const (
	compactCommand = "/compact"
	// compactRequestText is the chat notice the request leaves in history.
	compactRequestText = "Compact now / 立即壓縮"

	compactBusy        = "compact_busy"
	compactNoSession   = "compact_no_session"
	compactAgent       = "compact_agent"
	compactInput       = "compact_input"
	compactNotReported = "compaction_not_reported"
)

// admitCompaction runs under the actor lock after receipt replay, so a
// repeated idempotency key has already returned the first receipt. Every
// refusal here is typed and writes nothing.
func (r *Engine) admitCompaction(ctx context.Context, a *runtimeActor, bot pa.Bot, c pa.Conversation, pending []pa.Turn, s Submission) error {
	m := s.Message
	if s.Notice || s.LeadCall || s.Network != nil || s.Room != nil || m.Handoff != nil || m.QuoteMessageID != "" ||
		len(m.Attachments) > 0 || len(m.UploadIDs) > 0 || m.Text != compactRequestText {
		return runtimeFailure(pa.ScopeDenied, compactInput)
	}
	// As in Codync, only Claude Code and Codex compact on request.
	if bot.Agent != pa.ClaudeCode && bot.Agent != pa.Codex {
		return runtimeFailure(pa.Unsupported, compactAgent)
	}
	if a.working || c.ActiveTurnID != "" || len(pending) > 0 {
		return runtimeFailure(pa.CapacityFull, compactBusy)
	}
	if c.BindingID == "" {
		return runtimeFailure(pa.Unsupported, compactNoSession)
	}
	binding, err := r.o.Repository.Binding(ctx, a.key, c.BindingID)
	if err != nil {
		return err
	}
	if binding.NativeSessionID == "" || binding.Revoked {
		return runtimeFailure(pa.Unsupported, compactNoSession)
	}
	return nil
}

// withPriorUsage fills a manual compaction's "before" from the last turn's
// stored reading when the agent reported only the reading after it.
func withPriorUsage(notice *pa.Message, session ProviderSession, prior pa.ContextSample) *pa.Message {
	if notice == nil {
		return nil
	}
	reader, ok := session.(NativeContextReader)
	if !ok {
		return notice
	}
	state := reader.NativeContext()
	if state.CompactedBefore > 0 || prior.UsedTokens <= 0 || state.CompactedAfter <= 0 || state.CompactedAfter >= prior.UsedTokens {
		return notice
	}
	state.CompactedBefore = prior.UsedTokens
	out := *notice
	out.Text, out.Notice = compactionNoticeContent(state)
	return &out
}
