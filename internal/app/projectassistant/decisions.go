package projectassistant

import (
	"context"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func (r *Engine) decisions() (DecisionRepository, error) {
	p, ok := r.o.Repository.(DecisionRepository)
	if !ok {
		return nil, runtimeFailure(pa.Unsupported, "decisions")
	}
	return p, nil
}

// AskPerson records a question for the person from the Bot's active turn.
// In a peer request the question lands in the visible recipient's main chat.
func (r *Engine) AskPerson(ctx context.Context, fence pa.Fence, turnID string, q pa.AskPersonRequest) (pa.PermissionOffer, error) {
	p, err := r.decisions()
	if err != nil {
		return pa.PermissionOffer{}, err
	}
	if err = pa.CheckAskPerson(q); err != nil {
		return pa.PermissionOffer{}, err
	}
	a, err := r.acquire(fence.Key)
	if err != nil {
		return pa.PermissionOffer{}, err
	}
	defer r.release(a)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped.Load() || a.turnID != turnID {
		return pa.PermissionOffer{}, runtimeFailure(pa.StaleGeneration, "decision_turn")
	}
	bot, err := r.o.Repository.Bot(ctx, fence.Key)
	if err != nil {
		return pa.PermissionOffer{}, err
	}
	if err = r.authorized(ctx, bot.Key, bot.Project); err != nil {
		return pa.PermissionOffer{}, err
	}
	return p.OfferDecision(ctx, fence, turnID, q, r.o.Clock())
}

// forwardDecision sends the result of the person's answer turn back to the
// Bot that was waiting for it.
func (r *Engine) forwardDecision(ctx context.Context, key pa.BotKey, t pa.Turn, text string) error {
	p, err := r.decisions()
	if err != nil {
		return err
	}
	x, err := p.ForwardDecision(ctx, key, t, text, r.o.Clock())
	if err != nil {
		return err
	}
	if x.Delivery == "queued" || x.Delivery == "waiting" {
		r.wakeBot(x.Source.Key)
	}
	return nil
}

// answerDecision commits the person's press as their own message in the chat
// that asked. The offer resolves in the same commit, so it is answered once.
func (s *Service) answerDecision(ctx context.Context, c pa.Command, answer pa.PermissionAnswer, offer pa.PermissionOffer, a Authority) (pa.Receipt, error) {
	bot, err := s.opts.Repository.Bot(ctx, c.Key)
	if err != nil {
		return pa.Receipt{}, err
	}
	conv, err := s.opts.Repository.Conversation(ctx, c.Key, bot.ConversationID)
	if err != nil {
		return pa.Receipt{}, err
	}
	b, err := s.opts.Repository.Binding(ctx, c.Key, conv.BindingID)
	if err != nil && pa.Code(err) != pa.NotFound {
		return pa.Receipt{}, err
	}
	if err = pa.CheckDecision(offer, answer, bot, b.Revoked, s.opts.Clock()); err != nil {
		return pa.Receipt{}, err
	}
	text, err := pa.DecisionAnswerText(offer, answer)
	if err != nil {
		return pa.Receipt{}, err
	}
	resolved := offer
	resolved.State, resolved.SelectedOptionID = pa.PermissionResolved, answer.OptionID
	d := *offer.Decision
	d.AnswerText = text
	resolved.Decision = &d
	message := pa.SendMessage{Fence: currentFence(bot, conv), Text: pa.DecisionPrompt(offer, text)}
	if err = pa.ValidateInputSend(message.Text, nil, nil); err != nil {
		return pa.Receipt{}, err
	}
	// One key per offer: a second, different answer is a conflicting
	// command, and the same answer replays its receipt.
	cmd := c
	cmd.Operation, cmd.IdempotencyKey, cmd.ClientAttemptID = pa.SendOperation, "decision-"+offer.ID, ""
	cmd, err = canonicalCommand(cmd, pa.SendOperation, a.PrincipalID, message)
	if err != nil {
		return pa.Receipt{}, err
	}
	authorityCheck := func(ctx context.Context) error {
		_, e := s.authorizeEffect(ctx, c.Key, true, a)
		return e
	}
	return s.opts.Runtime.Submit(ctx, Submission{Command: cmd, Message: message, Decision: &resolved, InputOwner: InputOwner{Authority: readViewBinding(a)}, AuthorityCheck: authorityCheck, Feature: a.Feature, Source: a.Source})
}
