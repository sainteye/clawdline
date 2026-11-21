package projectassistant

import (
	"context"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type QueueEditRequest struct {
	Command pa.Command
	Fence   pa.Fence
	TurnID  string
	EditID  string
	Action  string
	Text    string
}

type queueEditor interface {
	EditQueue(context.Context, QueueEditRequest, func(context.Context) error) (pa.Receipt, error)
}

func (s *Service) EditQueue(ctx context.Context, q QueueEditRequest) (pa.Receipt, error) {
	// Queue editing is scoped to the human's own chat, not a hidden worker.
	if _, background, err := s.backgroundChannel(ctx, q.Command.Key); err != nil {
		return pa.Receipt{}, err
	} else if background {
		return pa.Receipt{}, serviceFailure(pa.ScopeDenied, "queue_edit")
	}
	a, err := s.authorize(ctx, q.Command.Key, true)
	if err != nil {
		return pa.Receipt{}, err
	}
	if q.Command.Key != q.Fence.Key || q.TurnID == "" {
		return pa.Receipt{}, serviceFailure(pa.ScopeDenied, "queue_edit")
	}
	switch q.Action {
	case "begin":
		if q.EditID != "" || q.Text != "" {
			return pa.Receipt{}, serviceFailure(pa.Invalid, "queue_edit")
		}
	case "save", "cancel":
		if q.EditID == "" || q.Action == "cancel" && q.Text != "" {
			return pa.Receipt{}, serviceFailure(pa.Invalid, "queue_edit")
		}
	default:
		return pa.Receipt{}, serviceFailure(pa.Invalid, "queue_edit")
	}
	q.Command, err = canonicalCommand(q.Command, pa.QueueEditOperation, a.PrincipalID, struct {
		Fence                        pa.Fence
		TurnID, EditID, Action, Text string
	}{q.Fence, q.TurnID, q.EditID, q.Action, q.Text})
	if err != nil {
		return pa.Receipt{}, err
	}
	editor, ok := s.opts.Runtime.(queueEditor)
	if !ok {
		return pa.Receipt{}, serviceFailure(pa.Unsupported, "queue_edit")
	}
	check := func(ctx context.Context) error {
		_, err := s.authorizeEffect(ctx, q.Command.Key, true, a)
		return err
	}
	return editor.EditQueue(ctx, q, check)
}

func (r *Engine) EditQueue(ctx context.Context, q QueueEditRequest, check func(context.Context) error) (pa.Receipt, error) {
	a, err := r.acquire(q.Command.Key)
	if err != nil {
		return pa.Receipt{}, err
	}
	defer r.release(a)
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := check(ctx); err != nil {
		return pa.Receipt{}, err
	}
	if receipt, err := r.replay(ctx, q.Command, pa.QueueEditOperation); err != nil {
		return pa.Receipt{}, err
	} else if receipt != nil {
		return *receipt, nil
	}
	bot, err := r.o.Repository.Bot(ctx, a.key)
	if err != nil {
		return pa.Receipt{}, err
	}
	r.setProject(a, bot.Project)
	if err := r.authorized(ctx, a.key, bot.Project); err != nil {
		return pa.Receipt{}, err
	}
	conversation, err := r.o.Repository.Conversation(ctx, a.key, bot.ConversationID)
	if err != nil {
		return pa.Receipt{}, err
	}
	if err := pa.CheckSendFence(q.Fence, currentFence(bot, conversation)); err != nil {
		return pa.Receipt{}, err
	}
	turn, err := r.o.Repository.Turn(ctx, a.key, q.TurnID)
	if err != nil {
		return pa.Receipt{}, err
	}
	if turn.State != pa.TurnQueued || turn.StartedAt != nil || turn.Room != nil ||
		a.turnID == turn.ID || a.steering[turn.ID] || turn.QueueEditID != q.EditID {
		return pa.Receipt{}, runtimeFailure(pa.StaleVersion, "queue_edit")
	}
	editID := q.EditID
	var edited *pa.Message
	switch q.Action {
	case "begin", "save":
		m, err := r.queueEditMessage(ctx, turn)
		if err != nil {
			return pa.Receipt{}, err
		}
		if q.Action == "begin" {
			editID = r.o.ID()
			turn.QueueEditID = editID
		} else {
			m.Text = q.Text
			ids := make([]string, 0, len(m.Files))
			for _, file := range m.Files {
				ids = append(ids, file.ID)
			}
			if err := pa.ValidateInputSend(m.Text, m.Attachments, ids); err != nil {
				return pa.Receipt{}, err
			}
			edited = &m
			turn.QueueEditID = ""
		}
	case "cancel":
		turn.QueueEditID = ""
	default:
		return pa.Receipt{}, runtimeFailure(pa.Invalid, "queue_edit")
	}
	if err := check(ctx); err != nil {
		return pa.Receipt{}, err
	}
	events := []pa.Event{{Fence: turn.Fence, Kind: pa.TurnUpdated, Turn: &turn}}
	if edited != nil {
		events = append(events, pa.Event{Fence: edited.Fence, Kind: pa.QueuedMessageEdited, Message: edited})
	}
	receipt, err := r.mutate(ctx, bot, Mutation{Command: q.Command, Turn: &turn, QueuedMessageEdit: edited,
		Receipt: pa.Receipt{State: pa.EffectDelivered, ResourceID: editID}, Events: events})
	if err == nil && q.Action != "begin" {
		r.kickLocked(a)
	}
	return receipt, err
}

func (r *Engine) queueEditMessage(ctx context.Context, turn pa.Turn) (pa.Message, error) {
	export, err := r.o.Repository.Export(ctx, turn.Fence.Key)
	if err != nil {
		return pa.Message{}, err
	}
	for _, m := range export.Messages {
		if m.ID == turn.MessageID && m.TurnID == turn.ID && m.Kind == pa.UserMessage && m.Exchange == nil {
			return m, nil
		}
	}
	return pa.Message{}, runtimeFailure(pa.ScopeDenied, "queue_edit_message")
}
