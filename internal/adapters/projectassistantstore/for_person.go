package projectassistantstore

import (
	"context"
	"database/sql"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// stampForPerson records, as the messages are written, which of them are
// addressed to the person (pa.CountsAsUnread). Every message this mutation
// adds gets an explicit answer: false unless its writer marked it, as
// notify_person does. When the mutation ends a turn the person started, that
// turn's last visible Bot message becomes the reply that counts. It returns
// the messages that became addressed to the person in this commit, once each.
func stampForPerson(a *aggregate, m app.Mutation, added int, previous pa.TurnState) []pa.Message {
	var out []pa.Message
	for i := len(a.Messages) - added; i < len(a.Messages); i++ {
		msg := &a.Messages[i]
		if msg.ForPerson == nil || msg.Kind != pa.AssistantMessage || !ownChatTurn(*a, msg.TurnID) {
			msg.ForPerson = pa.ForPersonFlag(false)
		}
		if *msg.ForPerson {
			out = append(out, *msg)
		}
	}
	t := m.Turn
	if t == nil || m.QuietCompletion || a.Bot.Hidden || t.State != pa.TurnSucceeded || previous == pa.TurnSucceeded ||
		!pa.DirectUserTurn(*t) || t.JoinedTurnID != "" {
		return out
	}
	for i := len(a.Messages) - 1; i >= 0; i-- {
		msg := &a.Messages[i]
		if msg.TurnID != t.ID || msg.Kind != pa.AssistantMessage || msg.Exchange != nil || quietMessage(*msg) {
			continue
		}
		if msg.ForPerson == nil || !*msg.ForPerson {
			msg.ForPerson = pa.ForPersonFlag(true)
			out = append(out, *msg)
		}
		break
	}
	return out
}

// syncMessageEvents gives each MessageCreated event the message as stored,
// so its payload still matches the aggregate after stampForPerson.
func syncMessageEvents(a aggregate, events []pa.Event) {
	for i := range events {
		e := &events[i]
		if e.Kind != pa.MessageCreated || e.Message == nil {
			continue
		}
		for _, stored := range a.Messages {
			if stored.ID == e.Message.ID {
				copy := *e.Message
				copy.ForPerson = stored.ForPerson
				e.Message = &copy
				break
			}
		}
	}
}

// tellPerson hands each message addressed to the person in this commit to
// PersonIntent, inside the same transaction, so a replayed receipt cannot
// tell the person twice.
func (s *Store) tellPerson(ctx context.Context, tx *sql.Tx, a aggregate, addressed []pa.Message) error {
	if s.PersonIntent == nil {
		return nil
	}
	for _, msg := range addressed {
		if err := s.PersonIntent(ctx, tx, a.Bot, msg); err != nil {
			return err
		}
	}
	return nil
}
