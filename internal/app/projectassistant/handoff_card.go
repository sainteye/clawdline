package projectassistant

import (
	"context"
	"strings"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type HandoffAction string

const (
	PrepareHandoff HandoffAction = "prepare"
	EditHandoff    HandoffAction = "edit"
	KeepHandoff    HandoffAction = "keep"
)

type HandoffRequest struct {
	Command pa.Command
	Fence   pa.Fence
	Action  HandoffAction
	Ref     *pa.HandoffRef
	Text    string
}

type HandoffWrite struct {
	Request HandoffRequest
	Preview *pa.HandoffCard
}

type HandoffView struct {
	Fence      pa.Fence
	CanPrepare bool
	Card       *pa.HandoffCard
}

// HandoffRepository owns the source-local draft. CommitHandoffInput admits the
// input and adopts its exact card in one transaction. A nil ref requires there
// to be no current draft. ClaimHandoffText runs inside Memory.WithCurrent.
type HandoffRepository interface {
	ReadHandoff(context.Context, pa.BotKey, string) (HandoffView, error)
	HandoffReplay(context.Context, pa.Command) (bool, error)
	WriteHandoff(context.Context, HandoffWrite) (HandoffView, error)
	CommitHandoffInput(context.Context, Mutation, pa.Fence, *pa.HandoffRef) (pa.Receipt, error)
	HandoffForTurn(context.Context, pa.BotKey, string) (*pa.HandoffCard, error)
	ClaimHandoffText(context.Context, pa.HandoffCard) (string, error)
}

func (s *Service) ReadHandoff(ctx context.Context, key pa.BotKey, conversation string) (HandoffView, error) {
	authority, err := s.authorize(ctx, key, true)
	if err != nil {
		return HandoffView{}, err
	}
	repository, ok := s.opts.Repository.(HandoffRepository)
	if !ok {
		return HandoffView{}, serviceFailure(pa.Unsupported, "handoff_card")
	}
	view, err := repository.ReadHandoff(ctx, key, conversation)
	if err != nil {
		return HandoffView{}, err
	}
	if _, err = s.authorizeEffect(ctx, key, true, authority); err != nil {
		return HandoffView{}, err
	}
	return view, nil
}

func (s *Service) WriteHandoff(ctx context.Context, request HandoffRequest) (HandoffView, error) {
	key := request.Command.Key
	authority, err := s.authorize(ctx, key, true)
	if err != nil {
		return HandoffView{}, err
	}
	if key != request.Fence.Key {
		return HandoffView{}, serviceFailure(pa.ScopeDenied, "handoff_key")
	}
	switch request.Action {
	case PrepareHandoff:
		if request.Ref != nil || request.Text != "" {
			return HandoffView{}, serviceFailure(pa.Invalid, "handoff_prepare")
		}
	case EditHandoff, KeepHandoff:
		if request.Ref == nil || request.Action == KeepHandoff && request.Text != "" {
			return HandoffView{}, serviceFailure(pa.Invalid, "handoff_ref")
		}
		if err = pa.ValidateHandoffRef(*request.Ref); err != nil {
			return HandoffView{}, err
		}
		if err = pa.ValidateHandoffText(request.Text); err != nil {
			return HandoffView{}, err
		}
	default:
		return HandoffView{}, serviceFailure(pa.Invalid, "handoff_action")
	}
	command, err := canonicalCommand(request.Command, pa.EditOperation, authority.PrincipalID, struct {
		Fence  pa.Fence
		Action HandoffAction
		Ref    *pa.HandoffRef
		Text   string
	}{request.Fence, request.Action, request.Ref, request.Text})
	if err != nil {
		return HandoffView{}, err
	}
	request.Command = command
	repository, ok := s.opts.Repository.(HandoffRepository)
	if !ok {
		return HandoffView{}, serviceFailure(pa.Unsupported, "handoff_card")
	}
	if replay, e := repository.HandoffReplay(ctx, command); e != nil {
		return HandoffView{}, e
	} else if replay {
		return s.ReadHandoff(ctx, key, request.Fence.ConversationID)
	}
	write := HandoffWrite{Request: request}
	if request.Action == PrepareHandoff {
		export, e := s.opts.Repository.Export(ctx, key)
		if e != nil {
			return HandoffView{}, e
		}
		write.Preview, err = prepareHandoffCard(export, request.Fence, s.opts.ID())
		if err != nil {
			return HandoffView{}, err
		}
	}
	if _, err = s.authorizeEffect(ctx, key, true, authority); err != nil {
		return HandoffView{}, err
	}
	view, err := repository.WriteHandoff(ctx, write)
	if err != nil {
		return HandoffView{}, err
	}
	if _, err = s.authorizeEffect(ctx, key, true, authority); err != nil {
		return HandoffView{}, err
	}
	return view, nil
}

func prepareHandoffCard(x Export, fence pa.Fence, id string) (*pa.HandoffCard, error) {
	if err := pa.CheckFence(fence, currentFence(x.Bot, x.Conversation)); err != nil {
		return nil, err
	}
	// Preview uses the same selection as the actual fresh native opening.
	// The cutoff is the persisted end of this conversation at Prepare.
	turn := pa.Turn{Fence: fence, Origin: pa.UserOrigin}
	quotes, _, err := sessionContinuationBefore(x, turn, x.Memory, x.Conversation.LastMessageSeq+1)
	if err != nil {
		return nil, err
	}
	card := &pa.HandoffCard{Ref: pa.HandoffRef{ID: id, Revision: 1}, State: pa.HandoffDraft, Fence: fence,
		MemoryRevision: x.Memory.Revision, InstructionRevision: x.Bot.Instructions.Revision}
	var excerpts []string
	for _, quote := range quotes {
		label := "User / 人的訊息"
		switch {
		case quote.Kind == pa.AssistantMessage:
			label = "Bot"
		case quote.State == pa.TurnUnknown:
			label = "User, outcome unknown / 人的訊息，結果不明"
		}
		excerpts = append(excerpts, label+"\n"+quote.Text)
		card.BookmarkMessageID = quote.MessageID
	}
	card.Text = strings.Join(excerpts, "\n\n")
	return card, pa.ValidateHandoffText(card.Text)
}

type handoffInputAuthority struct {
	TurnID string
	Check  func(context.Context) error
}

// Retain the request's verified gate and viewer identity while using the
// actor's lifetime. The original HTTP request ending cannot revoke its grant;
// actual revocation is checked again before opening and writing to the Agent.
func afterRequest(request context.Context, check func(context.Context) error) func(context.Context) error {
	values := context.WithoutCancel(request)
	return func(work context.Context) error {
		if err := work.Err(); err != nil {
			return err
		}
		ctx, cancel := context.WithCancel(values)
		defer cancel()
		stop := context.AfterFunc(work, cancel)
		defer stop()
		if err := check(ctx); err != nil {
			return err
		}
		return work.Err()
	}
}

func (r *Engine) commitHandoffAwareInput(ctx context.Context, bot pa.Bot, submission Submission, m Mutation) (pa.Receipt, error) {
	repository, ok := r.o.Repository.(HandoffRepository)
	if !ok && submission.Message.Handoff != nil {
		return pa.Receipt{}, runtimeFailure(pa.Unsupported, "handoff_card")
	}
	if !ok || submission.Notice {
		return r.mutate(ctx, bot, m)
	}
	prepared, err := r.prepareMutation(ctx, bot, m)
	if err != nil {
		return pa.Receipt{}, err
	}
	return repository.CommitHandoffInput(ctx, prepared, submission.Message.Fence, submission.Message.Handoff)
}

func (r *Engine) handoffForTurn(ctx context.Context, turn pa.Turn) (*pa.HandoffCard, error) {
	repository, ok := r.o.Repository.(HandoffRepository)
	if !ok || !mainChatTurn(turn) || turn.Room != nil {
		return nil, nil
	}
	return repository.HandoffForTurn(ctx, turn.Fence.Key, turn.ID)
}

func checkHandoffExecution(card pa.HandoffCard, turn pa.Turn, bot pa.Bot, current pa.Fence, memory pa.MemoryRevision, firstAttempt bool) error {
	if !sameSessionOwner(card.Fence, turn.Fence) || card.TurnID != turn.ID || firstAttempt && (card.Fence != turn.Fence || card.Fence != current) {
		return runtimeFailure(pa.ScopeDenied, "handoff_turn")
	}
	if card.State != pa.HandoffAdopted || card.Claimed || card.MemoryRevision != memory.Revision || card.InstructionRevision != bot.Instructions.Revision {
		return runtimeFailure(pa.ContextStale, "handoff_snapshot")
	}
	return nil
}
