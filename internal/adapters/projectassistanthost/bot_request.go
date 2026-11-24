package projectassistanthost

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
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
