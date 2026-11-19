package projectassistant

import (
	"context"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type GroupReactionRequest struct {
	Command                                    pa.Command
	ConversationID                             string
	GroupVersion, MessageSeq, ExpectedRevision int64
	Emoji                                      string
	Present                                    bool
}

type GroupReactionWriter interface {
	ReactToGroupLine(context.Context, GroupReactionRequest, string) (pa.Receipt, error)
}

func (s *Service) ReactToGroupLine(ctx context.Context, q GroupReactionRequest) (pa.Receipt, func(context.Context) error, error) {
	rooms, _, err := s.groups()
	if err != nil {
		return pa.Receipt{}, nil, err
	}
	a, err := s.authorize(ctx, q.Command.Key, true)
	if err != nil {
		return pa.Receipt{}, nil, err
	}
	if q.ConversationID == "" || q.GroupVersion < 1 || q.MessageSeq < 1 || q.ExpectedRevision < 0 {
		return pa.Receipt{}, nil, serviceFailure(pa.Invalid, "group_reaction_scope")
	}
	q.Emoji, err = pa.ReactionEmoji(q.Emoji)
	if err != nil {
		return pa.Receipt{}, nil, err
	}
	actor := pa.HumanReactionID(a.ReaderID)
	q.Command, err = canonicalCommand(q.Command, pa.ReactOperation, a.PrincipalID, struct {
		Conversation, Actor, Emoji         string
		GroupVersion, MessageSeq, Revision int64
		Present                            bool
	}{q.ConversationID, actor, q.Emoji, q.GroupVersion, q.MessageSeq, q.ExpectedRevision, q.Present})
	if err != nil {
		return pa.Receipt{}, nil, err
	}
	store, ok := rooms.store.(GroupReactionWriter)
	if !ok {
		return pa.Receipt{}, nil, serviceFailure(pa.Unsupported, "group_reactions")
	}
	check := func(ctx context.Context) error {
		_, err := s.authorizeEffect(ctx, q.Command.Key, true, a)
		return err
	}
	if err := check(ctx); err != nil {
		return pa.Receipt{}, nil, err
	}
	receipt, err := store.ReactToGroupLine(ctx, q, actor)
	return receipt, check, err
}
