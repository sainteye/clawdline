package projectassistantstore

import (
	"context"
	"sort"
	"strings"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// questionOwner is the visible chat whose question cards a turn may see and
// withdraw: the Bot itself, or for a hidden peer-request worker the Bot that
// owns it, where its ask_person cards landed.
func questionOwnerKey(a aggregate) pa.BotKey {
	if a.BackgroundOwner != nil {
		return *a.BackgroundOwner
	}
	return a.Bot.Key
}

// PendingQuestions lists the open ask_person questions in the caller's
// visible chat, oldest first, from its active turn.
func (s *Store) PendingQuestions(ctx context.Context, fence pa.Fence, turnID string) ([]pa.PendingQuestion, error) {
	a, err := load(ctx, s.db, fence.Key)
	if err != nil {
		return nil, err
	}
	if _, err = exchangeActive(a, fence, turnID); err != nil {
		return nil, err
	}
	owner := a
	if key := questionOwnerKey(a); key != a.Bot.Key {
		if owner, err = load(ctx, s.db, key); err != nil {
			return nil, err
		}
	}
	var offers []pa.PermissionOffer
	for _, p := range owner.Permissions {
		if pa.QuestionWithdrawable(p) {
			offers = append(offers, p)
		}
	}
	sort.Slice(offers, func(i, j int) bool {
		if offers[i].AnchorMessageSeq != offers[j].AnchorMessageSeq {
			return offers[i].AnchorMessageSeq < offers[j].AnchorMessageSeq
		}
		return offers[i].ID < offers[j].ID
	})
	out := make([]pa.PendingQuestion, 0, len(offers))
	for _, p := range offers {
		out = append(out, pa.PendingQuestionOf(p))
	}
	return out, nil
}

// WithdrawQuestion closes one of the caller's own open questions before the
// person answers it, keeping the Bot's reason on the card. When a requesting
// Bot was waiting on that question for the person, the exchange completes in
// the same transaction and the requester receives the withdrawal as its
// reply. It never answers a question and never touches a memory card.
func (s *Store) WithdrawQuestion(ctx context.Context, fence pa.Fence, turnID string, q pa.WithdrawQuestionRequest, at time.Time) (pa.PermissionOffer, pa.Exchange, error) {
	if err := pa.CheckWithdrawQuestion(q); err != nil {
		return pa.PermissionOffer{}, pa.Exchange{}, err
	}
	reason := strings.TrimSpace(q.Reason)
	caller, err := load(ctx, s.db, fence.Key)
	if err != nil {
		return pa.PermissionOffer{}, pa.Exchange{}, err
	}
	ownerKey := questionOwnerKey(caller)
	keys := []pa.BotKey{fence.Key, ownerKey}
	owner := caller
	if ownerKey != caller.Bot.Key {
		if owner, err = load(ctx, s.db, ownerKey); err != nil {
			return pa.PermissionOffer{}, pa.Exchange{}, err
		}
	}
	if p, ok := owner.Permissions[q.QuestionID]; ok && p.Decision != nil && p.Decision.ExchangeID != "" {
		x, e := exchangeRead(ctx, s.db, ownerKey.MachineID, p.Decision.ExchangeID)
		if e != nil {
			return pa.PermissionOffer{}, pa.Exchange{}, e
		}
		keys = append(keys, x.Source.Key, x.Target.Key)
	}
	unlock := s.lockExchange(keys...)
	defer unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return pa.PermissionOffer{}, pa.Exchange{}, err
	}
	defer tx.Rollback()
	if caller, err = load(ctx, tx, fence.Key); err != nil {
		return pa.PermissionOffer{}, pa.Exchange{}, err
	}
	if _, err = exchangeActive(caller, fence, turnID); err != nil {
		return pa.PermissionOffer{}, pa.Exchange{}, err
	}
	if questionOwnerKey(caller) != ownerKey {
		return pa.PermissionOffer{}, pa.Exchange{}, failure(pa.StaleVersion, "withdraw_question_owner")
	}
	owner = caller
	if ownerKey != caller.Bot.Key {
		if owner, err = load(ctx, tx, ownerKey); err != nil {
			return pa.PermissionOffer{}, pa.Exchange{}, err
		}
	}
	offer, ok := owner.Permissions[q.QuestionID]
	if !ok || offer.Kind != pa.DecisionOfferKind || offer.Decision == nil || offer.Decision.Memory != "" {
		return pa.PermissionOffer{}, pa.Exchange{}, failure(pa.NotFound, "withdraw_question")
	}
	if offer.State != pa.PermissionPending {
		return pa.PermissionOffer{}, pa.Exchange{}, failure(pa.StaleVersion, "withdraw_question_closed")
	}
	if !offer.ExpiresAt.IsZero() && !at.Before(offer.ExpiresAt) {
		return pa.PermissionOffer{}, pa.Exchange{}, failure(pa.Expired, "withdraw_question_expired")
	}
	d := *offer.Decision
	d.WithdrawnReason = reason
	offer.Decision = &d
	m := exchangeMutation(owner, offer.ID+"-withdraw", at, nil, nil)
	withdrawDecision(&m, offer, pa.PermissionCancelled, at)
	if err = exchangeApply(ctx, tx, &owner, m); err != nil {
		return pa.PermissionOffer{}, pa.Exchange{}, err
	}
	offer.State = pa.PermissionCancelled
	var x pa.Exchange
	if d.ExchangeID != "" {
		if x, err = exchangeRead(ctx, tx, ownerKey.MachineID, d.ExchangeID); err != nil {
			return pa.PermissionOffer{}, pa.Exchange{}, err
		}
		// An exchange still running has its recipient's turn to answer the
		// requester; only one already waiting for the person completes here.
		if x.Outcome == pa.ExchangeNeedsPerson {
			if x, err = finishPersonExchange(ctx, tx, x, pa.ExchangeCompleted, pa.WithdrawnQuestionReply(d.Question, reason), "-withdrawn-source", at); err != nil {
				return pa.PermissionOffer{}, pa.Exchange{}, err
			}
		}
	}
	return offer, x, s.commitRoster(ctx, tx, fence.Key.MachineID)
}

// ExpireWaitingDecisions completes every exchange on the machine that still
// waits for the person on a question whose offer has expired unanswered. The
// card closes as expired and the requester receives that as its reply, in
// one transaction per exchange. An answered question is left to the answer
// turn, which forwards it as before.
func (s *Store) ExpireWaitingDecisions(ctx context.Context, machine string, at time.Time) ([]pa.Exchange, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM project_assistant_exchanges WHERE machine=? AND json_extract(CAST(data AS TEXT),'$.outcome')=? ORDER BY rowid`, machine, string(pa.ExchangeNeedsPerson))
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	var expired []pa.Exchange
	for _, id := range ids {
		x, done, e := s.expireWaitingDecision(ctx, machine, id, at)
		if e != nil {
			return expired, e
		}
		if done {
			expired = append(expired, x)
		}
	}
	return expired, nil
}

func (s *Store) expireWaitingDecision(ctx context.Context, machine, id string, at time.Time) (pa.Exchange, bool, error) {
	x, err := exchangeRead(ctx, s.db, machine, id)
	if err != nil {
		return x, false, err
	}
	unlock := s.lockExchange(x.Source.Key, x.Target.Key)
	defer unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return x, false, err
	}
	defer tx.Rollback()
	if x, err = exchangeRead(ctx, tx, machine, id); err != nil || x.Outcome != pa.ExchangeNeedsPerson {
		return x, false, err
	}
	owner, err := load(ctx, tx, x.Target.Key)
	if err != nil {
		return x, false, err
	}
	offer, ok := pendingDecision(owner, x.ID)
	if !ok || offer.ExpiresAt.IsZero() || at.Before(offer.ExpiresAt) {
		return x, false, nil
	}
	m := exchangeMutation(owner, offer.ID+"-expire", at, nil, nil)
	withdrawDecision(&m, offer, pa.PermissionExpired, at)
	if err = exchangeApply(ctx, tx, &owner, m); err != nil {
		return x, false, err
	}
	if x, err = finishPersonExchange(ctx, tx, x, pa.ExchangeCompleted, pa.ExpiredQuestionReply(offer.Decision.Question), "-expired-source", at); err != nil {
		return x, false, err
	}
	return x, true, s.commitRoster(ctx, tx, machine)
}
