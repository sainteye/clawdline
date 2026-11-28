package projectassistanthost

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	gitadapter "github.com/sainteye/clawdline/internal/adapters/git"
	featurestore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
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
	ResolveBotRequestID(context.Context, string) (string, error)
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

// BotRequestStalls answers why each held card on this machine is stalled
// (featurestore.BotRequestStalls); nothing before the Bots store is open.
func (h *Host) BotRequestStalls(ctx context.Context, holds []featurestore.BotRequestHold) (map[string]string, error) {
	h.mu.Lock()
	st := h.store
	h.mu.Unlock()
	if st == nil {
		return nil, nil
	}
	return st.BotRequestStalls(ctx, h.o.MachineID, holds)
}

// botTurnLookup reads a creation's origin and a handoff's recipient.
type botTurnLookup interface {
	Turn(context.Context, pa.BotKey, string) (pa.Turn, error)
	Exchange(context.Context, pa.BotKey, string) (pa.Exchange, error)
	BotInTransaction(context.Context, *sql.Tx, pa.BotKey) (pa.Bot, error)
}

// botRequestTurn is what a card tool knows about the turn that called it.
type botRequestTurn struct {
	Bot          pa.Bot
	Conversation string
	Turn         pa.Turn
	// Directory is the Project's resolved directory for this turn, empty when
	// the Project has none. A card snapshots it as project_path, and a
	// verdict's commit is resolved in it.
	Directory string
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
		id, err := s.ResolveBotRequestID(ctx, q.CardID)
		if err != nil {
			return nil, botRequestStoreError(err)
		}
		c, err := s.BotRequestCard(ctx, id)
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
		card, err := br.NewCard(br.CardID(machine, auth.Bot, auth.ConversationID, auth.MessageID), t.Bot.Project.ProjectID, t.Directory, t.Bot.Key.BotID, auth,
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
	cardID, err := s.ResolveBotRequestID(ctx, q.CardID)
	if err != nil {
		return nil, botRequestStoreError(err)
	}
	write := store.BotRequestWrite{CardID: cardID, Expected: q.ExpectedVersion, Actor: actor, Key: key, Now: now}
	switch name {
	case "bot_request_handoff":
		write.Apply = func(c *br.Card) ([]br.Event, error) {
			if err := c.Handoff(actor, q.ToBot, q.Role, q.Note); err != nil {
				return nil, err
			}
			return []br.Event{{Kind: br.EventHanded, Detail: q.Note}}, nil
		}
		write.Validate = func(tx *sql.Tx, c br.Card) error {
			key := pa.BotKey{MachineID: t.Bot.Key.MachineID, BotID: c.Baton.Bot}
			target, err := look.BotInTransaction(ctx, tx, key)
			if err != nil && pa.Code(err) != pa.NotFound {
				return err
			}
			if err != nil || target.Hidden || target.Project != t.Bot.Project || target.Project.ProjectID != c.ProjectID {
				return &br.Refusal{Code: br.CodeForbidden, Field: "to_bot",
					Reason: "The recipient must be a visible Bot on this machine in the same Project as you and the card. Choose a recipient from that Project. 接棒者必須是同一台裝置上、與你及交辦卡同 Project 的可見 Bot。請改選該 Project 的 Bot。"}
			}
			return nil
		}
	case "bot_request_check":
		write.Apply = func(c *br.Card) ([]br.Event, error) {
			return c.CheckEvents(actor, q.Entries, now)
		}
	case "bot_request_verdict":
		verdictID := id
		commit, err := resolveVerdictCommit(ctx, t.Directory, q.Commit)
		if err != nil {
			return nil, err
		}
		write.Apply = func(c *br.Card) ([]br.Event, error) {
			if err := c.RecordVerdict(actor, verdictID, commit, q.Verdict, q.Text, now); err != nil {
				return nil, err
			}
			return []br.Event{{Kind: br.EventVerdict, Detail: q.Verdict}}, nil
		}
	case "bot_request_wait_person":
		write.Apply = func(c *br.Card) ([]br.Event, error) {
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
		write.Apply = func(c *br.Card) ([]br.Event, error) {
			changed, err := c.Done(actor, now)
			if err != nil || !changed {
				return nil, err
			}
			return []br.Event{{Kind: br.EventDone}}, nil
		}
	default:
		return nil, failure(pa.Unsupported, "bot_request_tool")
	}
	res, err := s.UpdateBotRequest(ctx, write)
	if err != nil {
		return nil, botRequestStoreError(err)
	}
	return botRequestAnswer(res.Card, res.Changed, res.Replayed), nil
}

// resolveVerdictCommit resolves a verdict's commit to a full object id in the
// Project, before the card's transaction (docs/bot-request-cards.md §4, §10).
// No commit is no commit; one that does not resolve in the Project, or a
// Project with no directory to resolve it in, is refused on the commit field.
func resolveVerdictCommit(ctx context.Context, dir, rev string) (string, error) {
	rev = strings.TrimSpace(rev)
	if rev == "" {
		return "", nil
	}
	if dir == "" {
		return "", &br.Refusal{Code: br.CodeInvalid, Field: "commit"}
	}
	full, err := gitadapter.New().Commit(ctx, dir, rev)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil || full == "" {
		return "", &br.Refusal{Code: br.CodeInvalid, Field: "commit"}
	}
	return full, nil
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

// resolveDispatchBotRequestID canonicalizes a card before the scope check or
// dispatch hash, so a full ID and its unique prefix name the same operation.
func resolveDispatchBotRequestID(ctx context.Context, s botRequestStore) (string, error) {
	named := pa.BotRequestFrom(ctx)
	if named == "" {
		return "", nil
	}
	if s == nil {
		return "", failure(pa.Unsupported, "bot_request_store")
	}
	id, err := s.ResolveBotRequestID(ctx, named)
	if errors.Is(err, store.ErrNoBotRequest) {
		return "", failure(pa.NotFound, "bot_request_id")
	}
	var ref *br.Refusal
	if errors.As(err, &ref) {
		dispatchRefusal := *ref
		dispatchRefusal.Field = "bot_request_id"
		return "", &dispatchRefusal
	}
	return id, err
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
