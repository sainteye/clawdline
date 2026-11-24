package projectassistanthost

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	br "github.com/sainteye/clawdline/internal/domain/botrequest"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// BotRequestToolNames are the Bot request card tools (docs/bot-request-cards.md §4.1, §8.2).
// Split and merge stay reserved names without a tool until they are built.
var BotRequestToolNames = []string{
	"bot_request_read", "bot_request_create", "bot_request_handoff", "bot_request_check",
	"bot_request_verdict", "bot_request_wait_person", "bot_request_done",
}

// botRequestStore is the part of the daemon store the card tools write.
type botRequestStore interface {
	CreateBotRequest(context.Context, br.Card) (br.Card, bool, error)
	UpdateBotRequest(context.Context, store.BotRequestWrite) (store.BotRequestResult, error)
	CheckBotRequests(context.Context, br.Actor, string, []store.BotRequestCheckTarget, []br.Mark, time.Time) ([]store.BotRequestCheckResult, error)
	BotRequestCard(context.Context, string) (br.Card, error)
	JoinBotRequestChain(ctx context.Context, cardID, by, bot string, now time.Time) (br.Card, error)
}

// botRequestCards answers the daemon store as the card tools use it, or nil
// (an untyped nil, so a missing store is never a non-nil interface).
func (h *Host) botRequestCards() botRequestStore {
	if h.o.Broker == nil || h.o.Broker.Store == nil {
		return nil
	}
	return h.o.Broker.Store
}

// botTurnLookup reads the turns and exchanges a creation walks back through.
type botTurnLookup interface {
	Turn(context.Context, pa.BotKey, string) (pa.Turn, error)
	Exchange(context.Context, pa.BotKey, string) (pa.Exchange, error)
}

// botRequestTurn is what a card tool knows about the turn that called it.
type botRequestTurn struct {
	Bot          pa.Bot
	Conversation string
	Turn         pa.Turn
}

// botRequestAuthority answers the person's message a turn roots in (§7.1):
// the turn itself when the person sent it, or, for a Bot-to-Bot request, the
// user turn its exchange chain reaches within MaxCollaborationHops. Routines,
// host notices, webhooks, compaction and network turns root in no person.
func botRequestAuthority(ctx context.Context, look botTurnLookup, t botRequestTurn) (br.Authority, error) {
	if t.Turn.Origin == pa.UserOrigin || t.Turn.Origin == "" {
		if t.Turn.Room != nil || t.Turn.Network != nil || t.Turn.RoutineRunID != "" || t.Turn.MessageID == "" {
			return br.Authority{}, &br.Refusal{Code: br.CodeNoAuthority, Field: "origin"}
		}
		return br.Authority{Bot: t.Bot.Key.BotID, ConversationID: t.Conversation, TurnID: t.Turn.ID, MessageID: t.Turn.MessageID}, nil
	}
	if t.Turn.Origin != pa.BotRequestOrigin {
		return br.Authority{}, &br.Refusal{Code: br.CodeNoAuthority, Field: "origin"}
	}
	key, cur := t.Bot.Key, t.Turn
	for range pa.MaxCollaborationHops {
		if cur.ExchangeID == "" {
			break
		}
		x, err := look.Exchange(ctx, key, cur.ExchangeID)
		if err != nil {
			return br.Authority{}, err
		}
		src, err := look.Turn(ctx, x.Source.Key, x.SourceTurnID)
		if err != nil {
			return br.Authority{}, err
		}
		switch src.Origin {
		case pa.UserOrigin, "":
			if src.Room != nil || src.Network != nil || src.RoutineRunID != "" || src.MessageID == "" {
				return br.Authority{}, &br.Refusal{Code: br.CodeNoAuthority, Field: "origin"}
			}
			return br.Authority{Bot: x.Source.Key.BotID, ConversationID: x.Source.ConversationID, TurnID: src.ID, MessageID: src.MessageID}, nil
		case pa.BotRequestOrigin:
			key, cur = x.Source.Key, src
		default:
			return br.Authority{}, &br.Refusal{Code: br.CodeNoAuthority, Field: "origin"}
		}
	}
	return br.Authority{}, &br.Refusal{Code: br.CodeNoAuthority, Field: "origin"}
}

type botRequestArgs struct {
	IdempotencyKey  string                        `json:"idempotency_key"`
	CardID          string                        `json:"card_id"`
	ExpectedVersion int64                         `json:"expected_version"`
	Title           string                        `json:"title"`
	Description     string                        `json:"description"`
	Kind            string                        `json:"kind"`
	Checklist       []string                      `json:"checklist"`
	ToBot           string                        `json:"to_bot"`
	Role            string                        `json:"role"`
	Note            string                        `json:"note"`
	Entries         []br.Mark                     `json:"entries"`
	Cards           []store.BotRequestCheckTarget `json:"cards"`
	Commit          string                        `json:"commit"`
	Verdict         string                        `json:"verdict"`
	Text            string                        `json:"text"`
	QuestionID      string                        `json:"question_id"`
}

// botRequestTool runs one card tool for the Bot of turn t. Each write is one
// store transaction keyed by the Bot's idempotency key, so a retried call
// answers the first result and changes nothing.
func botRequestTool(ctx context.Context, s botRequestStore, look botTurnLookup, t botRequestTurn, machine, id string, now time.Time, name string, raw json.RawMessage) (any, error) {
	if s == nil {
		return nil, failure(pa.Unsupported, "bot_request_store")
	}
	var q botRequestArgs
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if err := json.Unmarshal(raw, &q); err != nil {
		return nil, &br.Refusal{Code: br.CodeInvalid, Field: "arguments"}
	}
	actor := br.Actor{Bot: t.Bot.Key.BotID}
	if name == "bot_request_read" {
		if q.CardID == "" {
			return nil, &br.Refusal{Code: br.CodeInvalid, Field: "card_id"}
		}
		c, err := s.BotRequestCard(ctx, q.CardID)
		if err != nil {
			return nil, botRequestStoreError(err)
		}
		return botRequestAnswer(c, false, false), nil
	}
	if strings.TrimSpace(q.IdempotencyKey) == "" || len(q.IdempotencyKey) > 200 {
		return nil, &br.Refusal{Code: br.CodeInvalid, Field: "idempotency_key"}
	}
	key := "bot/" + q.IdempotencyKey
	switch name {
	case "bot_request_create":
		auth, err := botRequestAuthority(ctx, look, t)
		if err != nil {
			return nil, err
		}
		card, err := br.NewCard(br.CardID(machine, auth.Bot, auth.ConversationID, auth.MessageID), t.Bot.Project.ProjectID, "", t.Bot.Key.BotID, auth,
			br.CreateInput{Title: q.Title, Description: q.Description, Kind: q.Kind, Checklist: q.Checklist}, now)
		if err != nil {
			return nil, err
		}
		out, created, err := s.CreateBotRequest(ctx, card)
		if err != nil {
			return nil, botRequestStoreError(err)
		}
		return struct {
			botRequestReply
			Created bool `json:"created"`
		}{botRequestAnswer(out, created, false), created}, nil
	case "bot_request_check":
		if len(q.Cards) > 0 {
			if q.CardID != "" {
				return nil, &br.Refusal{Code: br.CodeInvalid, Field: "card_id"}
			}
			results, err := s.CheckBotRequests(ctx, actor, key, q.Cards, q.Entries, now)
			if err != nil {
				return nil, botRequestStoreError(err)
			}
			return map[string]any{"results": results}, nil
		}
	}
	if q.CardID == "" {
		return nil, &br.Refusal{Code: br.CodeInvalid, Field: "card_id"}
	}
	if q.ExpectedVersion <= 0 {
		return nil, &br.Refusal{Code: br.CodeInvalid, Field: "expected_version"}
	}
	var apply func(*br.Card) ([]br.Event, error)
	switch name {
	case "bot_request_handoff":
		apply = func(c *br.Card) ([]br.Event, error) {
			if err := c.Handoff(actor, q.ToBot, q.Role, q.Note); err != nil {
				return nil, err
			}
			return []br.Event{{Kind: br.EventHanded, Detail: q.Note}}, nil
		}
	case "bot_request_check":
		apply = func(c *br.Card) ([]br.Event, error) {
			changed, err := c.Check(actor, q.Entries, now)
			if err != nil || !changed {
				return nil, err
			}
			return []br.Event{{Kind: br.EventChecklistMark}}, nil
		}
	case "bot_request_verdict":
		verdictID := id
		apply = func(c *br.Card) ([]br.Event, error) {
			if err := c.RecordVerdict(actor, verdictID, q.Commit, q.Verdict, q.Text, now); err != nil {
				return nil, err
			}
			return []br.Event{{Kind: br.EventVerdict, Detail: q.Verdict}}, nil
		}
	case "bot_request_wait_person":
		apply = func(c *br.Card) ([]br.Event, error) {
			changed, err := c.SetWaiting(actor, q.QuestionID)
			if err != nil || !changed {
				return nil, err
			}
			kind := br.EventWaitingSet
			if q.QuestionID == "" {
				kind = br.EventWaitingCleared
			}
			return []br.Event{{Kind: kind, Detail: q.QuestionID}}, nil
		}
	case "bot_request_done":
		apply = func(c *br.Card) ([]br.Event, error) {
			changed, err := c.Done(actor, now)
			if err != nil || !changed {
				return nil, err
			}
			return []br.Event{{Kind: br.EventDone}}, nil
		}
	default:
		return nil, failure(pa.Unsupported, "bot_request_tool")
	}
	res, err := s.UpdateBotRequest(ctx, store.BotRequestWrite{CardID: q.CardID, Expected: q.ExpectedVersion, Actor: actor, Key: key, Now: now, Apply: apply})
	if err != nil {
		return nil, botRequestStoreError(err)
	}
	return botRequestAnswer(res.Card, res.Changed, res.Replayed), nil
}

// botRequestReply is a card as a Bot reads it: the card and its progress.
type botRequestReply struct {
	Card     br.Card `json:"card"`
	Done     int     `json:"progress_done"`
	Total    int     `json:"progress_total"`
	Changed  bool    `json:"changed"`
	Replayed bool    `json:"replayed,omitempty"`
}

func botRequestAnswer(c br.Card, changed, replayed bool) botRequestReply {
	done, total := c.Progress()
	return botRequestReply{Card: c, Done: done, Total: total, Changed: changed, Replayed: replayed}
}

func botRequestStoreError(err error) error {
	if errors.Is(err, store.ErrNoBotRequest) {
		return &br.Refusal{Code: br.CodeNotFound, Field: "card_id"}
	}
	return err
}

// botRequestNotice delivers one card notice from the broker's outbox as a
// host-notice turn in the Bot's main chat (docs/bot-request-cards.md §5, §7.2),
// through the same runtime and receipt rules a work completion uses.
func (h *Host) botRequestNotice(ctx context.Context, n orchestrator.BotRequestNotice) error {
	key := pa.BotKey{MachineID: h.o.MachineID, BotID: n.Bot}
	runtime, err := h.runtime()
	if err != nil {
		return err
	}
	feature, source, err := h.background(ctx, key, true)
	if err != nil {
		return err
	}
	return runtime.WakeBotRequest(ctx, key, app.BotRequestNotice{Key: n.Key, CardID: n.CardID, Version: n.Version, Kind: n.Kind,
		Title: n.Title, Role: n.Role, Note: n.Note, From: n.From}, feature, source)
}

// botRequestDispatchScope decides whether a dispatch_work or message_bot call
// may proceed in turn, and which card it attaches to (§7.1, §7.2).
//
//   - In a turn a handoff notice woke, the call must name that card, and the
//     card must be open, rooted in a person's message and held by this Bot.
//   - In a Bot-to-Bot request whose exchange names a card, the same holds for
//     that card; such a turn without a card keeps today's refusal (peer).
//   - In every other turn a named card only has to be one the broker accepts.
//
// peer is true when the turn is a Bot-to-Bot request with no card scope, so
// the caller keeps its existing refusal.
func botRequestDispatchScope(ctx context.Context, s botRequestStore, exchangeCard func(context.Context) (string, error), bot string, turn pa.Turn, named string) (scoped bool, err error) {
	notice, _ := pa.BotRequestHandoffCard(turn)
	if notice == "" && pa.IsPeerRequest(turn.Origin) && exchangeCard != nil {
		if notice, err = exchangeCard(ctx); err != nil {
			return false, err
		}
	}
	if notice == "" {
		return false, nil
	}
	if s == nil {
		return true, failure(pa.Unsupported, "bot_request_store")
	}
	if named == "" || named != notice {
		return true, &br.Refusal{Code: br.CodeHandoffScope, Field: "bot_request_id"}
	}
	c, err := s.BotRequestCard(ctx, notice)
	if err != nil {
		return true, botRequestStoreError(err)
	}
	return true, br.HandoffDispatch(c, notice, bot, named)
}
