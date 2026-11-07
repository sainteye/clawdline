package http

import (
	"context"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// RotationRecordBytesLimit is the reminder threshold for a provider-owned
// conversation file. It never limits writes or removes provider history.
const RotationRecordBytesLimit = 50 << 20

// RotationIdleSecondsLimit is the quiet interval after a delivery receipt.
// The Console requires both a current delivery and known record activity.
const RotationIdleSecondsLimit = 24 * 60 * 60

// A durable handoff already in progress or delivered replaces the suggestion.
// Unknown broker history offers no new launch; definite failures before typing
// can still be prepared again. The provider record remains readable either way.
func (s *Server) rotationInfo(ctx context.Context, item session.Session) *contract.SessionRotation {
	if item.Backend == session.BackendClaudeRemote || item.ConversationID == "" ||
		(item.Assistant != session.AssistantCodex && item.Assistant != session.AssistantClaude) || s.broker == nil {
		return nil
	}
	handoffs, err := s.broker.Handoffs(ctx)
	if err != nil {
		return nil
	}
	for _, h := range handoffs {
		if h.Milestone && h.FromSession == item.ConversationID && h.FromTerm == item.ID &&
			(h.State != orchestrator.HandoffSpawnFailed || h.TypeAttemptedAt != 0) {
			return nil
		}
	}
	return &contract.SessionRotation{RecordBytesLimit: RotationRecordBytesLimit, IdleSecondsLimit: RotationIdleSecondsLimit}
}
