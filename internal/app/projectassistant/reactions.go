package projectassistant

import (
	"context"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type ReactionRequest struct {
	Command          pa.Command
	Fence            pa.Fence
	MessageID, Emoji string
	Present          bool
	ExpectedRevision int64
}

// ReactionWrite is an internal persistence port. Native identity and target
// are captured by Host; human identity comes from the authenticated reader.
type ReactionWrite struct {
	ReactionRequest
	Actor              pa.ReactionActor
	NativeTurnID       string
	NativeBot          *pa.Bot
	ReceiptID, EventID string
}

type Reactions interface {
	React(context.Context, ReactionWrite) (pa.Receipt, error)
}

func (s *Service) React(ctx context.Context, q ReactionRequest) (pa.Receipt, func(context.Context) error, error) {
	a, err := s.authorize(ctx, q.Command.Key, true)
	if err != nil {
		return pa.Receipt{}, nil, err
	}
	if q.Fence.Key != q.Command.Key || q.MessageID == "" || q.ExpectedRevision < 0 {
		return pa.Receipt{}, nil, serviceFailure(pa.Invalid, "reaction_scope")
	}
	q.Emoji, err = pa.ReactionEmoji(q.Emoji)
	if err != nil {
		return pa.Receipt{}, nil, err
	}
	q.Command, err = canonicalCommand(q.Command, pa.ReactOperation, a.PrincipalID, struct {
		Fence                  pa.Fence
		Message, Emoji, Reader string
		Present                bool
		Revision               int64
	}{q.Fence, q.MessageID, q.Emoji, a.ReaderID, q.Present, q.ExpectedRevision})
	if err != nil {
		return pa.Receipt{}, nil, err
	}
	b, err := s.opts.Repository.Bot(ctx, q.Command.Key)
	if err != nil {
		return pa.Receipt{}, nil, err
	}
	if b.Project != q.Fence.Project || b.ConversationID != q.Fence.ConversationID {
		return pa.Receipt{}, nil, serviceFailure(pa.ScopeDenied, "reaction_scope")
	}
	if err = s.routineProject(ctx, b); err != nil {
		return pa.Receipt{}, nil, err
	}
	p, ok := s.opts.Repository.(Reactions)
	if !ok {
		return pa.Receipt{}, nil, serviceFailure(pa.Unsupported, "reactions")
	}
	check := func(ctx context.Context) error {
		_, e := s.authorizeEffect(ctx, b.Key, true, a)
		if e != nil {
			return e
		}
		now, e := s.opts.Repository.Bot(ctx, b.Key)
		if e != nil {
			return e
		}
		if now.Project != b.Project || now.ConversationID != b.ConversationID {
			return serviceFailure(pa.Revoked, "reaction_publication")
		}
		return nil
	}
	if err = check(ctx); err != nil {
		return pa.Receipt{}, nil, err
	}
	r, err := p.React(ctx, ReactionWrite{ReactionRequest: q, Actor: pa.ReactionActor{ID: pa.HumanReactionID(a.ReaderID), Kind: "human", Name: ""}, ReceiptID: s.opts.ID(), EventID: s.opts.ID()})
	return r, check, err
}

func personalizeReaction(m *pa.Message, reader string) {
	if m == nil {
		return
	}
	for i := range m.Reactions {
		m.Reactions[i].Own = m.Reactions[i].Kind == "human" && m.Reactions[i].ID == pa.HumanReactionID(reader)
	}
}
