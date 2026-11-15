package projectassistant

import (
	"context"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type OutputReader interface {
	Chunk(context.Context, pa.BotKey, pa.Output, int64) ([]byte, error)
}

type OutputRead struct {
	Output    pa.Output
	MessageID string
	Source    pa.SourceStatus
	Check     func(context.Context) error
}

// Output resolves only publications already committed to this conversation.
// Artifact reads follow the newest whole publication; files require their exact card.
func (s *Service) Output(ctx context.Context, key pa.BotKey, conversation, message, id, artifact string) (OutputRead, error) {
	a, err := s.readViewAuthority(ctx, key)
	if err != nil {
		return OutputRead{}, err
	}
	x, err := s.opts.Repository.Export(ctx, key)
	if err != nil {
		return OutputRead{}, err
	}
	if x.Bot.Key != key || x.Conversation.ID != conversation {
		return OutputRead{}, serviceFailure(pa.ScopeDenied, "output_conversation")
	}
	var found *pa.Message
	for i := len(x.Messages) - 1; i >= 0; i-- {
		m := &x.Messages[i]
		if m.Output == nil {
			continue
		}
		if artifact != "" && m.Output.ArtifactID == artifact || artifact == "" && m.ID == message && m.Output.ID == id {
			found = m
			break
		}
	}
	if found == nil {
		return OutputRead{}, serviceFailure(pa.NotFound, "output_message")
	}
	if found.Kind != pa.AssistantMessage || found.Fence.Key != key || found.Fence.ConversationID != conversation || found.Fence.Project != x.Bot.Project {
		return OutputRead{}, serviceFailure(pa.ScopeDenied, "output_message")
	}
	if err = pa.ValidateOutput(*found.Output); err != nil {
		return OutputRead{}, err
	}
	binding := readViewBinding(a)
	check := func(ctx context.Context) error {
		current, e := s.readViewAuthority(ctx, key)
		if e != nil {
			return e
		}
		if readViewBinding(current) != binding {
			return serviceFailure(pa.Revoked, "output_authority_changed")
		}
		b, e := s.opts.Repository.Bot(ctx, key)
		if e != nil {
			return e
		}
		if b.ConversationID != conversation {
			return serviceFailure(pa.ScopeDenied, "output_conversation")
		}
		return ctx.Err()
	}
	return OutputRead{Output: *found.Output, MessageID: found.ID, Source: a.Source, Check: check}, check(ctx)
}

// PublishedOutputs is shared by the execution-bound list tool and library projections.
func PublishedOutputs(messages []pa.Message) []pa.Output {
	items := []pa.Output{}
	seen := map[string]bool{}
	for i := len(messages) - 1; i >= 0; i-- {
		o := messages[i].Output
		if o == nil {
			continue
		}
		id := o.ID
		if o.ArtifactID != "" {
			id = o.ArtifactID
		}
		if !seen[id] {
			seen[id] = true
			items = append(items, *o)
		}
	}
	return items
}
