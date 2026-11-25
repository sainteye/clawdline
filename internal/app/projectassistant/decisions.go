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
// A press whose reply was lost replays its receipt before any state check,
// so the same answer never turns into "already answered". Every refusal
// before Submit has written nothing and stays a typed, retryable one.
func (s *Service) answerDecision(ctx context.Context, c pa.Command, answer pa.PermissionAnswer, offer pa.PermissionOffer, a Authority) (pa.Receipt, error) {
	text, err := pa.DecisionAnswerText(offer, answer)
	if err != nil {
		return pa.Receipt{}, err
	}
	// One key per offer, hashed over the answer and not the chat's
	// current fence, which a restart or fallback may have moved: the same
	// answer replays its receipt, and a different one finds the offer
	// already answered.
	cmd := c
	cmd.Operation, cmd.IdempotencyKey, cmd.ClientAttemptID = pa.SendOperation, "decision-"+offer.ID, ""
	cmd, err = canonicalCommand(cmd, pa.SendOperation, a.PrincipalID, decisionAnswerPayload{OfferID: offer.ID, OptionID: answer.OptionID, Text: text})
	if err != nil {
		return pa.Receipt{}, err
	}
	prior, err := s.replay(ctx, cmd)
	if pa.Code(err) == pa.IdempotencyConflict {
		return pa.Receipt{}, serviceFailure(pa.StaleVersion, "decision_answered")
	}
	if err != nil {
		return pa.Receipt{}, pa.NotSent(err, "decision_receipt")
	}
	if prior != nil {
		return *prior, nil
	}
	bot, err := s.opts.Repository.Bot(ctx, c.Key)
	if err != nil {
		return pa.Receipt{}, pa.NotSent(err, "decision_bot")
	}
	conv, err := s.opts.Repository.Conversation(ctx, c.Key, bot.ConversationID)
	if err != nil {
		return pa.Receipt{}, pa.NotSent(err, "decision_conversation")
	}
	if err = pa.CheckDecision(offer, answer, bot, s.opts.Clock()); err != nil {
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
	authorityCheck := func(ctx context.Context) error {
		_, e := s.authorizeEffect(ctx, c.Key, true, a)
		return e
	}
	return s.opts.Runtime.Submit(ctx, Submission{Command: cmd, Message: message, Decision: &resolved, InputOwner: InputOwner{Authority: readViewBinding(a)}, AuthorityCheck: authorityCheck, Feature: a.Feature, Source: a.Source})
}

// decisionAnswerPayload is what a decision answer's idempotency hash covers.
type decisionAnswerPayload struct {
	OfferID  string `json:"offer_id"`
	OptionID string `json:"option_id,omitempty"`
	Text     string `json:"text"`
}
