package projectassistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

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

// ProposeMemory turns a short fact a Bot wants to remember while answering
// another Bot into a memory card in its visible main chat. Text from a peer
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
	if offer.Decision.Memory != "" {
		return s.answerMemoryProposal(ctx, c, answer, offer, bot, a)
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

// answerMemoryProposal writes the card's literal fact as the person's own
// memory entry when they press Save, then resolves the card. Don't save only
// resolves it. The entry is written before the card resolves, and a retried
// press finds the entry already there, so a failed resolve never loses it.
func (s *Service) answerMemoryProposal(ctx context.Context, c pa.Command, answer pa.PermissionAnswer, offer pa.PermissionOffer, bot pa.Bot, a Authority) (pa.Receipt, error) {
	if answer.Text != "" || answer.OptionID != pa.MemorySaveOptionID && answer.OptionID != pa.MemorySkipOptionID {
		return pa.Receipt{}, serviceFailure(pa.Invalid, "memory_answer")
	}
	if answer.OptionID == pa.MemorySaveOptionID {
		if err := s.saveProposedMemory(ctx, c, offer, bot, a); err != nil {
			return pa.Receipt{}, err
		}
		// Saving advanced the Bot's version; resolve against the current one.
		var err error
		if bot, err = s.opts.Repository.Bot(ctx, c.Key); err != nil {
			return pa.Receipt{}, err
		}
	}
	resolved := offer
	resolved.State, resolved.SelectedOptionID = pa.PermissionResolved, answer.OptionID
	cmd := c
	cmd.Operation, cmd.IdempotencyKey, cmd.ClientAttemptID = pa.AnswerOperation, "memory-card-"+offer.ID, ""
	cmd, err := canonicalCommand(cmd, pa.AnswerOperation, a.PrincipalID, answer)
	if err != nil {
		return pa.Receipt{}, err
	}
	if old, e := s.opts.Repository.Receipt(ctx, cmd); e != nil {
		return pa.Receipt{}, e
	} else if old != nil {
		return *old, nil
	}
	if _, err = s.authorizeEffect(ctx, c.Key, true, a); err != nil {
		return pa.Receipt{}, err
	}
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
