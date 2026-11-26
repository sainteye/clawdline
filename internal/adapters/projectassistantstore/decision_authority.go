package projectassistantstore

import (
	"context"
	"log"
	"strings"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// DecisionAuthority finds the question card offerID in this machine's Bot
// chats and checks it as the person's answer behind a write made from the
// native conversation `conversation` (pa.CheckDecisionAuthority). Only the
// Bot chat's current binding counts as the conversation: a session the
// person replaced with New session no longer speaks for that chat.
func (s *Store) DecisionAuthority(ctx context.Context, machine, conversation, offerID string, now time.Time, window time.Duration) (pa.DecisionAuthority, error) {
	offerID = strings.TrimSpace(offerID)
	if offerID == "" {
		return pa.DecisionAuthority{}, &pa.DecisionRefusal{Code: pa.DecisionUnknown}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT bot FROM project_assistant_bot_facts WHERE machine=? AND collection='Permissions' AND member=? ORDER BY bot`, machine, offerID)
	if err != nil {
		return pa.DecisionAuthority{}, err
	}
	var owners []string
	for rows.Next() {
		var bot string
		if err := rows.Scan(&bot); err != nil {
			rows.Close()
			return pa.DecisionAuthority{}, err
		}
		owners = append(owners, bot)
	}
	if err := rows.Close(); err != nil {
		return pa.DecisionAuthority{}, err
	}
	for _, bot := range owners {
		a, err := load(ctx, s.db, pa.BotKey{MachineID: machine, BotID: bot})
		if err != nil {
			// One Bot's undecodable chat must not make every other Bot's
			// cards unreadable; its own cards are simply not found here.
			log.Printf("bots: question card lookup skipped an unreadable Bot %s: %v", bot, err)
			continue
		}
		offer, ok := a.Permissions[offerID]
		if !ok {
			continue
		}
		var answer *pa.Message
		for i := len(a.Messages) - 1; i >= 0; i-- {
			if m := a.Messages[i]; m.DecisionReply != nil && m.DecisionReply.OfferID == offerID {
				answer = &a.Messages[i]
				break
			}
		}
		current := map[string]pa.ExecutionBinding{}
		if b, ok := a.Bindings[a.Conversation.BindingID]; ok {
			current[a.Conversation.BindingID] = b
		}
		return pa.CheckDecisionAuthority(offer, true, current, answer, conversation, now, window)
	}
	return pa.CheckDecisionAuthority(pa.PermissionOffer{}, false, nil, nil, conversation, now, window)
}
