package projectassistantstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// OfferDecision stores a question a Bot asks the person. A peer request's
// question is placed in the visible recipient's main chat, never in the
// hidden worker that asked, so the person can answer where the authority
// lives. It never sends a chat message and never answers anything.
func (s *Store) OfferDecision(ctx context.Context, fence pa.Fence, turnID string, q pa.AskPersonRequest, at time.Time) (pa.PermissionOffer, error) {
	if err := pa.CheckAskPerson(q); err != nil {
		return pa.PermissionOffer{}, err
	}
	caller, err := s.Export(ctx, fence.Key)
	if err != nil {
		return pa.PermissionOffer{}, err
	}
	var turn pa.Turn
	for _, t := range caller.Turns {
		if t.ID == turnID {
			turn = t
		}
	}
	keys := []pa.BotKey{fence.Key}
	var x pa.Exchange
	if pa.IsPeerRequest(turn.Origin) && turn.ExchangeID != "" {
		x, err = exchangeRead(ctx, s.db, fence.Key.MachineID, turn.ExchangeID)
		if err != nil {
			return pa.PermissionOffer{}, err
		}
		keys = append(keys, x.Target.Key)
	}
	unlock := s.lockExchange(keys...)
	defer unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return pa.PermissionOffer{}, err
	}
	defer tx.Rollback()
	a, err := load(ctx, tx, fence.Key)
	if err != nil {
		return pa.PermissionOffer{}, err
	}
	t, err := exchangeActive(a, fence, turnID)
	if err != nil {
		return pa.PermissionOffer{}, err
	}
	sum := sha256.Sum256([]byte(turnID + "\x00" + q.Question))
	id := "decision-" + hex.EncodeToString(sum[:12])
	d := pa.DecisionRequest{Question: q.Question}
	owner := a
	if pa.IsPeerRequest(t.Origin) {
		if t.ExchangeID == "" || t.ExchangeID != x.ID {
			return pa.PermissionOffer{}, failure(pa.StaleVersion, "decision_exchange")
		}
		x, err = exchangeRead(ctx, tx, fence.Key.MachineID, x.ID)
		if err != nil {
			return pa.PermissionOffer{}, err
		}
		if !x.Outstanding() || x.RequestTurnID != t.ID || x.Recipient() != (pa.ExchangeEndpoint{Key: a.Bot.Key, ConversationID: a.Conversation.ID}) {
			return pa.PermissionOffer{}, failure(pa.StaleVersion, "decision_exchange")
		}
		if x.RequestWorkerID != "" {
			if a.BackgroundOwner == nil || *a.BackgroundOwner != x.Target.Key {
				return pa.PermissionOffer{}, failure(pa.ScopeDenied, "decision_owner")
			}
			owner, err = load(ctx, tx, x.Target.Key)
			if err != nil {
				return pa.PermissionOffer{}, err
			}
			if owner.Conversation.ID != x.Target.ConversationID || owner.Bot.Hidden {
				return pa.PermissionOffer{}, failure(pa.ScopeDenied, "decision_owner")
			}
		}
		d.ExchangeID, d.SourceBotID, d.RequestTurnID = x.ID, x.Source.Key.BotID, t.ID
		d.Request = pa.ClipUTF8(x.Request, pa.MaxDecisionRequestQuoteBytes)
	} else if a.Bot.Hidden {
		return pa.PermissionOffer{}, failure(pa.ScopeDenied, "decision_owner")
	}
	if old, ok := owner.Permissions[id]; ok {
		return old, nil
	}
	pending := 0
	for _, p := range owner.Permissions {
		if p.Kind == pa.DecisionOfferKind && p.State == pa.PermissionPending {
			if d.ExchangeID != "" && p.Decision != nil && p.Decision.ExchangeID == d.ExchangeID {
				return pa.PermissionOffer{}, failure(pa.StaleVersion, "decision_exchange_pending")
			}
			pending++
		}
	}
	if pending >= pa.MaxDecisionOffersPerBot {
		return pa.PermissionOffer{}, failure(pa.CapacityFull, "decision_offers")
	}
	offer := pa.PermissionOffer{
		ID: id, NativeRequestID: id, Kind: pa.DecisionOfferKind, Decision: &d,
		Fence: exchangeFence(owner), AnchorMessageSeq: owner.Conversation.LastMessageSeq,
		ExpiresAt: at.Add(pa.DecisionOfferTTL), State: pa.PermissionPending,
	}
	for i, r := range q.SuggestedReplies {
		offer.Options = append(offer.Options, pa.PermissionOption{ID: pa.DecisionOptionPrefix + strconv.Itoa(i+1), Label: r, Kind: pa.AllowOnce})
	}
	m := exchangeMutation(owner, id+"-offer", at, nil, nil)
	m.Permissions = []pa.PermissionOffer{offer}
	copy := offer
	m.Events = append(m.Events, pa.Event{Version: pa.ContractVersion, ID: id + "-offered", At: at, Fence: offer.Fence, Kind: pa.PermissionOffered, Permission: &copy})
	if err = exchangeApply(ctx, tx, &owner, m); err != nil {
		return pa.PermissionOffer{}, err
	}
	return offer, s.commitRoster(ctx, tx, fence.Key.MachineID)
}

// pendingDecision finds the open question a peer request left for the person.
func pendingDecision(a aggregate, exchangeID string) (pa.PermissionOffer, bool) {
	for _, p := range a.Permissions {
		if p.Kind == pa.DecisionOfferKind && p.State == pa.PermissionPending && p.Decision != nil && p.Decision.ExchangeID == exchangeID {
			return p, true
		}
	}
	return pa.PermissionOffer{}, false
}

// ForwardDecision completes an exchange that waited for the person once the
// person's answer turn in the recipient's main chat ends, queueing the
// result to the requester as an ordinary correlated reply.
func (s *Store) ForwardDecision(ctx context.Context, key pa.BotKey, turn pa.Turn, text string, at time.Time) (pa.Exchange, error) {
	if turn.DecisionExchangeID == "" {
		return pa.Exchange{}, failure(pa.Invalid, "decision_forward")
	}
	x, err := exchangeRead(ctx, s.db, key.MachineID, turn.DecisionExchangeID)
	if err != nil {
		return x, err
	}
	unlock := s.lockExchange(x.Source.Key, x.Target.Key)
	defer unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return x, err
	}
	defer tx.Rollback()
	x, err = exchangeRead(ctx, tx, key.MachineID, x.ID)
	if err != nil {
		return x, err
	}
	if x.Target.Key != key {
		return x, failure(pa.ScopeDenied, "decision_forward")
	}
	if x.Outcome != pa.ExchangeNeedsPerson {
		return x, nil
	}
	prior := x.Revision
	x.Revision++
	switch turn.State {
	case pa.TurnSucceeded:
		x.Outcome = pa.ExchangeCompleted
		x.Reply = pa.CollaborationReply(text)
		x.ReplyAt = time.Time{}
		if x.Reply != "" {
			x.ReplyAt = at
		}
	case pa.TurnCancelled:
		x.Outcome = pa.ExchangeCancelled
	case pa.TurnUnknown:
		x.Outcome = pa.ExchangeUnknown
	default:
		x.Outcome = pa.ExchangeFailed
	}
	b, e := load(ctx, tx, x.Source.Key)
	if e != nil && pa.Code(e) != pa.NotFound {
		return x, e
	}
	x.Delivery = "dropped"
	if e == nil && b.Bot.Project == x.Project && b.Conversation.ID == x.Source.ConversationID {
		var reply *pa.Turn
		x.Delivery = "queued"
		x.ReplyTurnID = x.ID + "-reply-turn"
		if collaborationQueueFull(b) {
			x.Delivery = "waiting"
		} else {
			reply = exchangeReplyTurn(b, x)
		}
		body := x.Reply
		if body == "" {
			body = x.Request
		}
		msgID, turnID := x.ID+"-decided-source", ""
		if reply != nil {
			msgID, turnID = reply.MessageID, reply.ID
		}
		mb := exchangeMutation(b, x.ID+"-decided-source", at, reply, []pa.Message{exchangeNotice(b, x, msgID, body, turnID, at)})
		if err = exchangeApply(ctx, tx, &b, mb); err != nil {
			return x, err
		}
	}
	if err = exchangeWrite(ctx, tx, x, prior); err != nil {
		return x, err
	}
	return x, s.commitRoster(ctx, tx, key.MachineID)
}

// withdrawDecision closes a question its exchange no longer waits for.
func withdrawDecision(m *app.Mutation, offer pa.PermissionOffer, state pa.PermissionState, at time.Time) {
	offer.State = state
	m.Permissions = append(m.Permissions, offer)
	copy := offer
	m.Events = append(m.Events, pa.Event{Version: pa.ContractVersion, ID: offer.ID + "-withdrawn", At: at, Fence: offer.Fence, Kind: pa.PermissionAnswered, Permission: &copy})
}
