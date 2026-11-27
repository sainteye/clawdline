package projectassistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

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

func (r *Engine) questions() (QuestionRepository, error) {
	p, ok := r.o.Repository.(QuestionRepository)
	if !ok {
		return nil, runtimeFailure(pa.Unsupported, "questions")
	}
	return p, nil
}

// ListQuestions returns the open questions in the Bot's visible chat, from
// its active turn.
func (r *Engine) ListQuestions(ctx context.Context, fence pa.Fence, turnID string) ([]pa.PendingQuestion, error) {
	p, err := r.questions()
	if err != nil {
		return nil, err
	}
	return p.PendingQuestions(ctx, fence, turnID)
}

// WithdrawQuestion closes one of the Bot's own open questions from its
// active turn. A requesting Bot that waited on it is woken with the reason.
func (r *Engine) WithdrawQuestion(ctx context.Context, fence pa.Fence, turnID string, q pa.WithdrawQuestionRequest) (pa.PermissionOffer, error) {
	p, err := r.questions()
	if err != nil {
		return pa.PermissionOffer{}, err
	}
	if err = pa.CheckWithdrawQuestion(q); err != nil {
		return pa.PermissionOffer{}, err
	}
	a, err := r.acquire(fence.Key)
	if err != nil {
		return pa.PermissionOffer{}, err
	}
	defer r.release(a)
	a.mu.Lock()
	if a.stopped.Load() || a.turnID != turnID {
		a.mu.Unlock()
		return pa.PermissionOffer{}, runtimeFailure(pa.StaleGeneration, "decision_turn")
	}
	offer, x, err := p.WithdrawQuestion(ctx, fence, turnID, q, r.o.Clock())
	a.mu.Unlock()
	if err != nil {
		return pa.PermissionOffer{}, err
	}
	if x.Delivery == "queued" || x.Delivery == "waiting" {
		r.wakeBot(x.Source.Key)
	}
	return offer, nil
}

// ExpireWaitingDecisions closes questions for the person that expired while
// a requesting Bot waited on them, and wakes each requester with that reply.
func (r *Engine) ExpireWaitingDecisions(ctx context.Context, machine string, at time.Time) error {
	p, ok := r.o.Repository.(DecisionExpiry)
	if !ok {
		return nil
	}
	expired, err := p.ExpireWaitingDecisions(ctx, machine, at)
	for _, x := range expired {
		if x.Delivery == "queued" || x.Delivery == "waiting" {
			r.wakeBot(x.Source.Key)
		}
	}
	return err
}

// ProposeMemory turns a short fact a Bot wants to remember while answering
// another Bot into a proposal in its Memory panel. Text from a peer
// never reaches memory on the Bot's own say: only the person's press on the
// card writes it.
func (r *Engine) ProposeMemory(ctx context.Context, fence pa.Fence, turnID, text string) (pa.PermissionOffer, error) {
	text = strings.TrimSpace(text)
	if err := pa.CheckMemoryProposal(text); err != nil {
		return pa.PermissionOffer{}, err
	}
	return r.AskPerson(ctx, fence, turnID, pa.AskPersonRequest{Question: pa.MemoryProposalQuestion, SuggestedReplies: []string{"Save", "Don't save"}, Memory: text})
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
	if offer.Kind == pa.DecisionOfferKind && offer.Decision != nil && offer.Decision.Memory != "" {
		return s.answerMemoryProposal(ctx, c, answer, offer, a)
	}
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

// answerMemoryProposal writes the card's literal fact as the person's own
// memory entry when they press Save, then resolves the card. Don't save only
// resolves it. A memory card sends no message, so it never reaches Submit.
// As for any decision, a repeated press replays its receipt before any state
// check; the entry is written before the card resolves, and a retried press
// finds the entry already there, so a failed resolve never loses it.
func (s *Service) answerMemoryProposal(ctx context.Context, c pa.Command, answer pa.PermissionAnswer, offer pa.PermissionOffer, a Authority) (pa.Receipt, error) {
	if answer.Text != "" || answer.OptionID != pa.MemorySaveOptionID && answer.OptionID != pa.MemorySkipOptionID {
		return pa.Receipt{}, serviceFailure(pa.Invalid, "memory_answer")
	}
	cmd := c
	cmd.Operation, cmd.IdempotencyKey, cmd.ClientAttemptID = pa.AnswerOperation, "memory-card-"+offer.ID, ""
	cmd, err := canonicalCommand(cmd, pa.AnswerOperation, a.PrincipalID, answer)
	if err != nil {
		return pa.Receipt{}, err
	}
	prior, err := s.replay(ctx, cmd)
	if pa.Code(err) == pa.IdempotencyConflict {
		return pa.Receipt{}, serviceFailure(pa.StaleVersion, "decision_answered")
	}
	if err != nil {
		return pa.Receipt{}, err
	}
	if prior != nil {
		return *prior, nil
	}
	bot, err := s.opts.Repository.Bot(ctx, c.Key)
	if err != nil {
		return pa.Receipt{}, err
	}
	if err = pa.CheckDecision(offer, answer, bot, s.opts.Clock()); err != nil {
		return pa.Receipt{}, err
	}
	if answer.OptionID == pa.MemorySaveOptionID {
		if err = s.saveProposedMemory(ctx, c, offer, bot, a); err != nil {
			return pa.Receipt{}, err
		}
		// Saving advanced the Bot's version; resolve against the current one.
		if bot, err = s.opts.Repository.Bot(ctx, c.Key); err != nil {
			return pa.Receipt{}, err
		}
	}
	if _, err = s.authorizeEffect(ctx, c.Key, true, a); err != nil {
		return pa.Receipt{}, err
	}
	resolved := offer
	resolved.State, resolved.SelectedOptionID = pa.PermissionResolved, answer.OptionID
	now := s.opts.Clock()
	next := bot
	next.Version++
	next.UpdatedAt = now
	copy := resolved
	m := Mutation{Command: cmd, ExpectedVersion: bot.Version, Bot: &next, Permissions: []pa.PermissionOffer{resolved},
		Receipt: pa.Receipt{ID: s.opts.ID(), Command: cmd, State: pa.EffectDelivered, ResourceID: offer.ID},
		Events:  []pa.Event{{Version: pa.ContractVersion, ID: s.opts.ID(), At: now, Fence: offer.Fence, Kind: pa.PermissionAnswered, Permission: &copy}}}
	return s.opts.Repository.Commit(ctx, m)
}

func (s *Service) saveProposedMemory(ctx context.Context, c pa.Command, offer pa.PermissionOffer, bot pa.Bot, a Authority) error {
	sum := sha256.Sum256([]byte(offer.ID))
	id := "card-" + hex.EncodeToString(sum[:8])
	x, err := s.opts.Repository.Export(ctx, bot.Key)
	if err != nil {
		return err
	}
	for _, e := range x.MemoryEntries {
		if e.ID == id && !e.Forgotten {
			return nil
		}
	}
	current, err := s.opts.Memory.Current(ctx, bot.Key)
	if err != nil {
		return err
	}
	// The person chose this entry on their own card, so it carries their
	// authorship, as an entry they typed in memory settings would.
	entry := pa.MemoryEntry{ID: id, Key: bot.Key, Project: bot.Project, Text: offer.Decision.Memory, Revision: current.Revision,
		Sources: []pa.MemorySource{{ID: "manual:" + id, Key: bot.Key, Project: bot.Project, Kind: "human", Revision: current.Revision}}}
	cmd := c
	cmd.Operation, cmd.IdempotencyKey, cmd.ClientAttemptID = pa.MemorySaveOperation, "memory-card-save-"+offer.ID, ""
	cmd, err = canonicalCommand(cmd, pa.MemorySaveOperation, a.PrincipalID, struct {
		Entry            pa.MemoryEntry
		ExpectedRevision int64
	}{entry, current.Revision})
	if err != nil {
		return err
	}
	if _, err = s.authorizeEffect(ctx, c.Key, true, a); err != nil {
		return err
	}
	_, err = s.opts.Memory.Save(ctx, MemoryWrite{Command: cmd, Entry: entry, ExpectedRevision: current.Revision})
	return err
}
