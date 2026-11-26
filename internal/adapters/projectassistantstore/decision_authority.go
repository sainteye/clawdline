package projectassistantstore

import (
	"context"
	"encoding/json"
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
	rows, err := s.db.QueryContext(ctx, `SELECT data FROM project_assistant_bots WHERE machine=?`, machine)
	if err != nil {
		return pa.DecisionAuthority{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return pa.DecisionAuthority{}, err
		}
		var a aggregate
		if err := json.Unmarshal(data, &a); err != nil {
			return pa.DecisionAuthority{}, err
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
	if err := rows.Err(); err != nil {
		return pa.DecisionAuthority{}, err
	}
	return pa.CheckDecisionAuthority(pa.PermissionOffer{}, false, nil, nil, conversation, now, window)
}
