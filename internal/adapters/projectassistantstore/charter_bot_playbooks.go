package projectassistantstore

import (
	"context"
	"slices"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// A private worker proposes changes in its owner's name only while answering
// a request from the same Project.
func charterProposalActor(ctx context.Context, r reader, fence pa.Fence, turnID string) (pa.Bot, error) {
	a, err := load(ctx, r, fence.Key)
	if err != nil {
		return pa.Bot{}, err
	}
	turn, err := exchangeActive(a, fence, turnID)
	if err != nil {
		return pa.Bot{}, err
	}
	editor := a.Bot
	switch {
	case turn.Origin == pa.BotRequestOrigin:
		x, err := exchangeRead(ctx, r, fence.Key.MachineID, turn.ExchangeID)
		if err != nil {
			return pa.Bot{}, err
		}
		if x.Recipient() != (pa.ExchangeEndpoint{Key: fence.Key, ConversationID: fence.ConversationID}) ||
			x.RequestTurnID != turnID || x.Project != fence.Project || x.DestinationProject() != fence.Project {
			return pa.Bot{}, failure(pa.ScopeDenied, "charter_request")
		}
		if a.BackgroundOwner != nil {
			if *a.BackgroundOwner != x.Target.Key {
				return pa.Bot{}, failure(pa.ScopeDenied, "charter_request")
			}
			owner, err := load(ctx, r, *a.BackgroundOwner)
			if err != nil {
				return pa.Bot{}, err
			}
			if owner.Conversation.ID != x.Target.ConversationID {
				return pa.Bot{}, failure(pa.StaleGeneration, "charter_request")
			}
			editor = owner.Bot
		}
	case a.BackgroundOwner == nil && pa.DirectUserTurn(turn):
	default:
		return pa.Bot{}, failure(pa.ScopeDenied, "charter_turn")
	}
	if editor.Project != fence.Project {
		return pa.Bot{}, failure(pa.ScopeDenied, "charter_project")
	}
	return editor, nil
}

func (s *Store) CharterProposalActor(ctx context.Context, fence pa.Fence, turnID string) (pa.Bot, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return pa.Bot{}, err
	}
	defer tx.Rollback()
	return charterProposalActor(ctx, tx, fence, turnID)
}

func (s *Store) ReadBotCharter(ctx context.Context, fence pa.Fence, turnID string) (pa.CharterVersion, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return pa.CharterVersion{}, err
	}
	defer tx.Rollback()
	if _, err = charterProposalActor(ctx, tx, fence, turnID); err != nil {
		return pa.CharterVersion{}, err
	}
	return currentCharter(ctx, tx, fence.Project.MachineID, fence.Project.ProjectID)
}

func withProposedPlaybook(current pa.CharterVersion, proposed pa.CharterPlaybook) (charterBody, error) {
	body := bodyOf(current)
	for i, existing := range body.Playbooks {
		if existing.ID == proposed.ID {
			body.Playbooks = slices.Clone(body.Playbooks)
			proposed.BotIDs = existing.BotIDs
			body.Playbooks[i] = proposed
			return body, nil
		}
	}
	return charterBody{}, failure(pa.NotFound, "charter_playbook")
}
