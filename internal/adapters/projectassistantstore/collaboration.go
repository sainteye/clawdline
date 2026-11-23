package projectassistantstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

var _ app.CollaborationRepository = (*Store)(nil)

func readExchanges(ctx context.Context, tx *sql.Tx, key pa.BotKey) ([]pa.Exchange, error) {
	rows, err := tx.QueryContext(ctx, `SELECT data FROM project_assistant_exchanges WHERE machine=? AND (source=? OR target=?) ORDER BY id`, key.MachineID, key.BotID, key.BotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pa.Exchange
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var x pa.Exchange
		if err = json.Unmarshal(raw, &x); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// CollaborationUsage reads the logical outstanding counts, independent of
// process cleanup slots and historical exchange rows.
func (s *Store) CollaborationUsage(ctx context.Context, machine string) (int64, int64, error) {
	var total, largest int64
	err := s.db.QueryRowContext(ctx, `SELECT coalesce(sum(n),0),coalesce(max(n),0) FROM (SELECT count(*) n FROM project_assistant_exchanges WHERE machine=? AND outstanding=1 GROUP BY source)`, machine).Scan(&total, &largest)
	return total, largest, err
}

func (s *Store) lockExchange(keys ...pa.BotKey) func() {
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].MachineID == keys[j].MachineID {
			return keys[i].BotID < keys[j].BotID
		}
		return keys[i].MachineID < keys[j].MachineID
	})
	var unlock []func()
	for i, key := range keys {
		if i == 0 || key != keys[i-1] {
			unlock = append(unlock, s.lock(key))
		}
	}
	return func() {
		for i := len(unlock) - 1; i >= 0; i-- {
			unlock[i]()
		}
	}
}

func exchangeRead(ctx context.Context, r reader, machine, id string) (pa.Exchange, error) {
	var raw []byte
	err := r.QueryRowContext(ctx, `SELECT data FROM project_assistant_exchanges WHERE machine=? AND id=?`, machine, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return pa.Exchange{}, failure(pa.NotFound, "exchange")
	}
	var x pa.Exchange
	if err == nil {
		err = json.Unmarshal(raw, &x)
	}
	return x, err
}
func (s *Store) Exchange(ctx context.Context, key pa.BotKey, id string) (pa.Exchange, error) {
	x, err := exchangeRead(ctx, s.db, key.MachineID, id)
	if err == nil && key != x.Source.Key && key != x.Target.Key && key != x.Recipient().Key {
		err = failure(pa.ScopeDenied, "exchange")
	}
	return x, err
}

func exchangeWrite(ctx context.Context, tx *sql.Tx, x pa.Exchange, prior int64) error {
	raw, err := json.Marshal(x)
	if err != nil {
		return err
	}
	if prior == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_exchanges(machine,id,source,target,outstanding,revision,data) VALUES(?,?,?,?,?,?,?)`, x.Project.MachineID, x.ID, x.Source.Key.BotID, x.Target.Key.BotID, x.Outstanding(), x.Revision, raw)
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE project_assistant_exchanges SET outstanding=?,revision=?,data=? WHERE machine=? AND id=? AND revision=?`, x.Outstanding(), x.Revision, raw, x.Project.MachineID, x.ID, prior)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n != 1 {
		return failure(pa.StaleVersion, "exchange")
	}
	return err
}

func exchangeFence(a aggregate) pa.Fence {
	return pa.Fence{Key: a.Bot.Key, Project: a.Bot.Project, ConversationID: a.Conversation.ID, BindingID: a.Conversation.BindingID, Generation: a.Conversation.Generation}
}
func exchangeActive(a aggregate, fence pa.Fence, turn string) (pa.Turn, error) {
	t, ok := a.Turns[turn]
	if !ok || a.Conversation.ActiveTurnID != turn || exchangeFence(a) != fence || t.Fence != fence || t.State != pa.TurnRunning && t.State != pa.TurnWaitingPermission {
		return t, failure(pa.StaleGeneration, "collaboration_turn")
	}
	b := a.Bindings[fence.BindingID]
	if b.Revoked {
		return t, failure(pa.Revoked, "collaboration_binding")
	}
	return t, nil
}

func (s *Store) Directory(ctx context.Context, fence pa.Fence, turn string, q pa.BotDirectoryQuery) (pa.BotDirectoryPage, error) {
	if q.PageSize == 0 {
		q.PageSize = pa.MaxPageRows
	}
	if err := pa.CheckPage(q.PageSize, false); err != nil {
		return pa.BotDirectoryPage{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return pa.BotDirectoryPage{}, err
	}
	defer tx.Rollback()
	a, err := load(ctx, tx, fence.Key)
	if err != nil {
		return pa.BotDirectoryPage{}, err
	}
	if _, err = exchangeActive(a, fence, turn); err != nil {
		return pa.BotDirectoryPage{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT data FROM project_assistant_bots WHERE machine=? AND bot>? AND json_extract(CAST(data AS TEXT),'$.BackgroundOwner') IS NULL ORDER BY bot`, fence.Key.MachineID, q.Cursor)
	if err != nil {
		return pa.BotDirectoryPage{}, err
	}
	defer rows.Close()
	out := pa.BotDirectoryPage{Bots: []pa.BotDirectoryEntry{}}
	words := strings.Fields(strings.ToLower(q.Query))
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return out, err
		}
		var row aggregate
		if err = json.Unmarshal(raw, &row); err != nil {
			return out, err
		}
		b := row.Bot
		if a.BackgroundOwner != nil || row.BackgroundOwner != nil || !pa.MayContactBot(a.Bot, b) {
			continue
		}
		match := true
		for _, word := range words {
			if !strings.Contains(strings.ToLower(b.Name), word) {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		if len(out.Bots) == q.PageSize {
			out.NextCursor = out.Bots[len(out.Bots)-1].ID
			break
		}
		out.Bots = append(out.Bots, pa.BotDirectoryEntry{ID: b.Key.BotID, Name: b.Name, Agent: b.Agent})
	}
	return out, rows.Err()
}

func exchangeMutation(a aggregate, id string, at time.Time, turn *pa.Turn, messages []pa.Message) app.Mutation {
	cmd := commandForExchange(a.Bot.Key, id)
	m := app.Mutation{Command: cmd, ExpectedVersion: a.Bot.Version, Turn: turn, Messages: messages, Receipt: pa.Receipt{ID: id, Command: cmd, State: pa.EffectDelivered, ResourceID: id}}
	bot := a.Bot
	bot.Version++
	bot.UpdatedAt = at
	m.Bot = &bot
	if turn != nil {
		m.Events = append(m.Events, pa.Event{Version: pa.ContractVersion, ID: id + "-turn", At: at, Fence: turn.Fence, Kind: pa.TurnUpdated, Turn: turn})
	}
	seq := a.Conversation.LastMessageSeq
	for i := range m.Messages {
		seq++
		m.Messages[i].Seq = seq
		copy := m.Messages[i]
		m.Events = append(m.Events, pa.Event{Version: pa.ContractVersion, ID: copy.ID, At: at, Fence: copy.Fence, Kind: pa.MessageCreated, Message: &copy})
	}
	return m
}

func commandForExchange(key pa.BotKey, id string) pa.Command {
	// This command carries no model text; the admission command separately
	// hashes its exact payload and is checked before any effect.
	return pa.Command{Key: key, Operation: pa.SendOperation, IdempotencyKey: id, ContentHash: strings.Repeat("0", 64)}
}
func exchangeNotice(a aggregate, x pa.Exchange, id, text, turn string, at time.Time) pa.Message {
	return pa.Message{ID: id, Fence: exchangeFence(a), TurnID: turn, Kind: pa.NoticeMessage, Text: text, At: at, Exchange: &x}
}

func exchangeApply(ctx context.Context, tx *sql.Tx, a *aggregate, m app.Mutation) error {
	if a.Bot.Key != m.Command.Key || a.Bot.Version != m.ExpectedVersion {
		return failure(pa.StaleVersion, "exchange_bot")
	}
	if err := applyMutation(a, m); err != nil {
		return err
	}
	if err := applyAgentRest(ctx, tx, *a, m); err != nil {
		return err
	}
	if err := validateAggregate(*a); err != nil {
		return err
	}
	for _, e := range m.Events {
		seq, err := appendEvent(ctx, tx, *a, e)
		if err != nil {
			return err
		}
		m.Receipt.EventSeq = seq
		rememberRosterTrace(a, e)
	}
	if err := persist(ctx, tx, *a, m.ExpectedVersion); err != nil {
		return err
	}
	return writeReceipt(ctx, tx, m.Receipt)
}

func (s *Store) AdmitExchange(ctx context.Context, q app.ExchangeAdmission) (pa.Exchange, error) {
	x := q.Exchange
	if x.Source.Key != q.SourceFence.Key || x.Source.Key == x.Target.Key || x.Project != q.SourceFence.Project || x.Target.Key.MachineID != x.Source.Key.MachineID || x.ID == "" || x.Revision != 1 || x.Outcome != pa.ExchangeQueued || x.SourceTurnID != q.SourceTurnID || x.RequestTurnID == "" || x.RequestWorkerID != "" {
		return x, failure(pa.ScopeDenied, "exchange_admission")
	}
	u := s.lockExchange(x.Source.Key, x.Target.Key)
	defer u()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return x, err
	}
	defer tx.Rollback()
	prior, err := receipt(ctx, tx, q.Command)
	if err != nil {
		return x, err
	}
	if prior != nil {
		return exchangeRead(ctx, tx, x.Project.MachineID, prior.ResourceID)
	}
	a, err := load(ctx, tx, x.Source.Key)
	if err != nil {
		return x, err
	}
	b, err := load(ctx, tx, x.Target.Key)
	if err != nil {
		return x, err
	}
	t, err := exchangeActive(a, q.SourceFence, q.SourceTurnID)
	if err != nil {
		return x, err
	}
	if err = pa.CheckCollaborationRequest(pa.BotMessageRequest{IdempotencyKey: q.Command.IdempotencyKey, TargetBotID: x.Target.Key.BotID, Text: x.Request}, t.Origin, t.Hops); err != nil {
		return x, err
	}
	if a.Bot.Version != q.SourceVersion || b.Bot.Version != q.TargetVersion {
		return x, failure(pa.StaleVersion, "exchange_bot")
	}
	if a.Bot.Project != x.Project || b.Bot.Project != x.DestinationProject() || b.BackgroundOwner != nil || !pa.MayContactBot(a.Bot, b.Bot) || a.Conversation.ID != x.Source.ConversationID || b.Conversation.ID != x.Target.ConversationID || x.Hops != t.Hops+1 {
		return x, failure(pa.ScopeDenied, "exchange_project")
	}
	var total, sender int
	if err = tx.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(CASE WHEN source=? THEN 1 ELSE 0 END),0) FROM project_assistant_exchanges WHERE machine=? AND outstanding=1`, x.Source.Key.BotID, x.Project.MachineID).Scan(&total, &sender); err != nil {
		return x, err
	}
	if total >= pa.MaxCollaborationOutstanding {
		return x, failure(pa.CapacityFull, "bot_outstanding")
	}
	if sender >= pa.MaxCollaborationPerSender {
		return x, failure(pa.CapacityFull, "bot_sender_outstanding")
	}
	var mb app.Mutation
	if b.Bot.ManageTeam {
		for _, turn := range b.Turns {
			if turn.State == pa.TurnUnknown {
				return x, failure(pa.OutcomeUnknown, "recipient_unknown")
			}
		}
		if b.Conversation.BindingID == "" {
			b.Conversation.BindingID = x.ID + "-initial"
			b.Conversation.Generation = 1
			f := exchangeFence(b)
			b.Bindings[f.BindingID] = pa.ExecutionBinding{Fence: f, Agent: b.Bot.Agent, MemoryRevision: b.Memory.Revision, InstructionRevision: b.Bot.Instructions.Revision}
		}
		request := pa.Turn{ID: x.RequestTurnID, Fence: exchangeFence(b), MessageID: x.ID + "-request", State: pa.TurnQueued, Origin: pa.BotRequestOrigin, ExchangeID: x.ID, Hops: x.Hops, IdempotencyKey: x.ID}
		mb = exchangeMutation(b, x.ID+"-admit-target", q.At, &request, []pa.Message{exchangeNotice(b, x, request.MessageID, x.Request, request.ID, q.At)})
	} else {
		x.RequestWorkerID = backgroundWorkerID(x.ID)
		notice := exchangeNotice(b, x, x.ID+"-request", x.Request, "", q.At)
		task := pa.BackgroundTask{ID: x.ID, Fence: aggregateFence(b), ExchangeID: x.ID,
			OriginMessageID: notice.ID, Title: a.Bot.Name, Brief: x.Request, CreatedAt: q.At}
		if err = queueBackgroundTask(ctx, tx, task); err != nil {
			return x, err
		}
		mb = exchangeMutation(b, x.ID+"-admit-target", q.At, nil, []pa.Message{notice})
	}
	ma := exchangeMutation(a, x.ID+"-admit-source", q.At, nil, []pa.Message{exchangeNotice(a, x, x.ID+"-queued", x.Request, "", q.At)})
	ma.Command = q.Command
	ma.Receipt.Command = q.Command
	ma.Receipt.ID = x.ID + "-receipt"
	ma.Receipt.State = pa.EffectAccepted
	ma.Receipt.ResourceID = x.ID
	if err = exchangeApply(ctx, tx, &a, ma); err != nil {
		return x, err
	}
	if err = exchangeApply(ctx, tx, &b, mb); err != nil {
		return x, err
	}
	if err = exchangeWrite(ctx, tx, x, 0); err != nil {
		return x, err
	}
	return x, s.commitRoster(ctx, tx, x.Project.MachineID)
}

func (s *Store) StartExchange(ctx context.Context, fence pa.Fence, turn, id string) error {
	x, err := s.Exchange(ctx, fence.Key, id)
	if err != nil {
		return err
	}
	u := s.lockExchange(x.Source.Key, x.Target.Key, fence.Key)
	defer u()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	x, err = exchangeRead(ctx, tx, fence.Key.MachineID, id)
	if err != nil {
		return err
	}
	b, err := load(ctx, tx, fence.Key)
	if err != nil {
		return err
	}
	if _, err = exchangeActive(b, fence, turn); err != nil {
		return err
	}
	if x.Recipient() != (pa.ExchangeEndpoint{Key: fence.Key, ConversationID: fence.ConversationID}) ||
		x.DestinationProject() != fence.Project || x.RequestTurnID != turn || !x.Outstanding() {
		return failure(pa.ScopeDenied, "exchange_start")
	}
	if x.RequestWorkerID == "" {
		if b.Bot.Hidden || b.BackgroundOwner != nil {
			return failure(pa.ScopeDenied, "exchange_recipient")
		}
	} else {
		owner, err := load(ctx, tx, x.Target.Key)
		if err != nil {
			return err
		}
		if b.BackgroundOwner == nil || *b.BackgroundOwner != x.Target.Key || owner.Bot.Hidden ||
			owner.Bot.Project != x.DestinationProject() || owner.Conversation.ID != x.Target.ConversationID {
			return failure(pa.ScopeDenied, "exchange_recipient")
		}
	}
	if x.Outcome == pa.ExchangeSent {
		return nil
	}
	a, err := load(ctx, tx, x.Source.Key)
	if err != nil && pa.Code(err) != pa.NotFound {
		return err
	}
	if err == nil && (a.Bot.Project != x.Project || a.Conversation.ID != x.Source.ConversationID) {
		return failure(pa.ScopeDenied, "exchange_source")
	}
	prior := x.Revision
	x.Revision++
	x.Outcome = pa.ExchangeSent
	if err = exchangeWrite(ctx, tx, x, prior); err != nil {
		return err
	}
	return tx.Commit()
}

// FinishExchange commits terminal target facts, surviving projections, the
// source reply queue and delivery CAS together. It never waits on an actor.
func (s *Store) FinishExchange(ctx context.Context, m app.Mutation, text string) (pa.Exchange, error) {
	if m.Turn == nil || m.Turn.ExchangeID == "" {
		return pa.Exchange{}, failure(pa.Invalid, "exchange_finish")
	}
	x, err := s.Exchange(ctx, m.Command.Key, m.Turn.ExchangeID)
	if err != nil {
		return x, err
	}
	u := s.lockExchange(x.Source.Key, x.Target.Key, m.Command.Key)
	defer u()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return x, err
	}
	defer tx.Rollback()
	x, err = exchangeRead(ctx, tx, m.Command.Key.MachineID, x.ID)
	if err != nil {
		return x, err
	}
	if p, e := receipt(ctx, tx, m.Command); e != nil {
		return x, e
	} else if p != nil {
		return x, nil
	}
	a, err := load(ctx, tx, m.Command.Key)
	if err != nil {
		return x, err
	}
	t := *m.Turn
	prior := x.Revision
	x.Revision++
	if t.Origin == pa.BotReplyOrigin {
		if x.Source.Key != a.Bot.Key || x.ReplyTurnID != t.ID || x.Delivery != "queued" {
			return x, failure(pa.ScopeDenied, "reply_delivery")
		}
		x.Delivery = "delivered"
		if t.State != pa.TurnSucceeded {
			x.Delivery = "dropped"
		}
		if err = exchangeApply(ctx, tx, &a, m); err != nil {
			return x, err
		}
	} else {
		if t.Origin != pa.BotRequestOrigin || x.Recipient() != (pa.ExchangeEndpoint{Key: a.Bot.Key, ConversationID: a.Conversation.ID}) ||
			x.DestinationProject() != a.Bot.Project || x.RequestTurnID != t.ID || !x.Outstanding() ||
			x.RequestWorkerID != "" && (a.BackgroundOwner == nil || *a.BackgroundOwner != x.Target.Key) {
			return x, failure(pa.StaleVersion, "request_completion")
		}
		switch t.State {
		case pa.TurnSucceeded:
			x.Outcome = pa.ExchangeCompleted
			x.Reply = pa.CollaborationReply(text)
		case pa.TurnUnknown:
			x.Outcome = pa.ExchangeUnknown
		case pa.TurnCancelled:
			x.Outcome = pa.ExchangeCancelled
		case pa.TurnFailed:
			x.Outcome = pa.ExchangeFailed
		default:
			return x, failure(pa.Invalid, "exchange_completion")
		}
		if t.Failure != nil {
			x.Detail = string(t.Failure.Code) + ":" + t.Failure.Field
		}
		at := m.Bot.UpdatedAt
		b, e := load(ctx, tx, x.Source.Key)
		if e != nil && pa.Code(e) != pa.NotFound {
			return x, e
		}
		if e == nil && (b.Bot.Project != x.Project || b.Conversation.ID != x.Source.ConversationID) {
			return x, failure(pa.ScopeDenied, "reply_source")
		}
		x.Delivery = "dropped"
		var reply *pa.Turn
		if e == nil && repliesToSource(x.Outcome) {
			x.Delivery = "queued"
			x.ReplyTurnID = x.ID + "-reply-turn"
			if collaborationQueueFull(b) {
				// Completion has already happened. Retain the reply durably
				// without occupying a native slot or exceeding the chat queue.
				x.Delivery = "waiting"
			} else {
				reply = exchangeReplyTurn(b, x)
			}
		}
		if e == nil {
			body := x.Reply
			if body == "" {
				body = x.Request
			}
			msgID, turnID := x.ID+"-result-source", ""
			if reply != nil {
				msgID, turnID = reply.MessageID, reply.ID
			}
			mb := exchangeMutation(b, x.ID+"-complete-source", at, reply, []pa.Message{exchangeNotice(b, x, msgID, body, turnID, at)})
			if err = exchangeApply(ctx, tx, &b, mb); err != nil {
				return x, err
			}
		}
		// Completion is a trace and structured notice, not an ordinary answer
		// in the recipient's chat. Preserve pending permission closures.
		m.Messages = nil
		events := m.Events[:0]
		for _, e := range m.Events {
			if e.Kind != pa.MessageCreated {
				events = append(events, e)
			}
		}
		m.Events = events
		var message *pa.Message
		if x.RequestWorkerID == "" {
			msg := exchangeNotice(a, x, x.ID+"-result-target", x.Request, "", at)
			message = &msg
		} else if x.Reply != "" {
			message = &pa.Message{ID: x.ID + "-worker-reply", Fence: t.Fence, TurnID: t.ID, Kind: pa.AssistantMessage, Text: x.Reply, At: at}
		}
		if message != nil {
			message.Seq = a.Conversation.LastMessageSeq + 1
			m.Messages = []pa.Message{*message}
			if m.Conversation != nil {
				m.Conversation.LastMessageSeq = message.Seq
			}
			m.Events = append(m.Events, pa.Event{Version: pa.ContractVersion, ID: message.ID, At: at, Fence: message.Fence, Kind: pa.MessageCreated, Message: message})
		}
		m.Events = append(m.Events, pa.Event{Version: pa.ContractVersion, ID: x.ID + "-complete-trace", At: at, Fence: t.Fence, Kind: pa.TraceUpdated, Trace: &pa.Trace{TurnID: t.ID, Category: "bot_request", Text: x.Reply, Complete: true}})
		previousBackgroundTurn := a.backgroundTurnState()
		if err = exchangeApply(ctx, tx, &a, m); err != nil {
			return x, err
		}
		if x.RequestWorkerID != "" {
			owner, err := load(ctx, tx, x.Target.Key)
			if err != nil {
				return x, err
			}
			if owner.Bot.Project != x.DestinationProject() || owner.Conversation.ID != x.Target.ConversationID {
				return x, failure(pa.ScopeDenied, "exchange_recipient")
			}
			msg := exchangeNotice(owner, x, x.ID+"-result-target", x.Request, "", at)
			if err = exchangeApply(ctx, tx, &owner, exchangeMutation(owner, x.ID+"-finish-target", at, nil, []pa.Message{msg})); err != nil {
				return x, err
			}
			if err = projectBackgroundTurn(ctx, tx, a, m, previousBackgroundTurn); err != nil {
				return x, err
			}
		}
	}
	if err = exchangeWrite(ctx, tx, x, prior); err != nil {
		return x, err
	}
	return x, s.commitRoster(ctx, tx, x.Project.MachineID)
}

// Cancel queued peer work before deleting its recipient. Active workers must
// reach a terminal outcome through their own fence first.
func cancelWaitingBackgroundExchanges(ctx context.Context, tx *sql.Tx, target aggregate, now time.Time) error {
	key := target.Bot.Key
	rows, err := tx.QueryContext(ctx, `SELECT task FROM project_assistant_background_tasks
		WHERE machine=? AND bot=? AND state='waiting' AND json_extract(CAST(data AS TEXT),'$.exchange_id') IS NOT NULL`,
		key.MachineID, key.BotID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var IDs []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return err
		}
		IDs = append(IDs, id)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, id := range IDs {
		x, err := exchangeRead(ctx, tx, key.MachineID, id)
		if err != nil {
			return err
		}
		if x.Target != (pa.ExchangeEndpoint{Key: key, ConversationID: target.Conversation.ID}) ||
			x.DestinationProject() != target.Bot.Project || x.RequestWorkerID != backgroundWorkerID(id) || x.Outcome != pa.ExchangeQueued {
			return failure(pa.ScopeDenied, "background_exchange")
		}
		prior := x.Revision
		x.Revision++
		x.Outcome, x.Delivery = pa.ExchangeCancelled, "dropped"
		source, err := load(ctx, tx, x.Source.Key)
		if err != nil && pa.Code(err) != pa.NotFound {
			return err
		}
		if err == nil && source.Bot.Project == x.Project && source.Conversation.ID == x.Source.ConversationID {
			msg := exchangeNotice(source, x, x.ID+"-result-source", x.Request, "", now)
			if err = exchangeApply(ctx, tx, &source, exchangeMutation(source, x.ID+"-cancel-source", now, nil, []pa.Message{msg})); err != nil {
				return err
			}
		}
		if err = exchangeWrite(ctx, tx, x, prior); err != nil {
			return err
		}
	}
	return nil
}

func collaborationQueueFull(a aggregate) bool {
	pending := 0
	for _, t := range a.Turns {
		switch t.State {
		case pa.TurnQueued, pa.TurnAccepted, pa.TurnRunning, pa.TurnWaitingPermission, pa.TurnUnknown:
			pending++
		}
	}
	return pending >= pa.MaxAutomationBacklog
}
func exchangeReplyTurn(a aggregate, x pa.Exchange) *pa.Turn {
	return &pa.Turn{ID: x.ReplyTurnID, Fence: exchangeFence(a), MessageID: x.ID + "-reply-input", State: pa.TurnQueued, Origin: pa.BotReplyOrigin, ExchangeID: x.ID, Hops: x.Hops, IdempotencyKey: x.ID + "-reply"}
}

// HasPendingExchangeReply looks only at the sender's pending delivery index.
func (s *Store) HasPendingExchangeReply(ctx context.Context, key pa.BotKey) (bool, error) {
	var pending bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM project_assistant_exchanges
		WHERE machine=? AND source=? AND json_extract(CAST(data AS TEXT),'$.delivery') IN ('waiting','queued'))`, key.MachineID, key.BotID).Scan(&pending)
	return pending, err
}

// Promotion is only a local transfer from a durable completed result into
// available chat queue space. It cannot repeat the recipient invocation.
func (s *Store) PromoteExchangeReply(ctx context.Context, key pa.BotKey, at time.Time) (bool, error) {
	unlock := s.lock(key)
	defer unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	a, err := load(ctx, tx, key)
	if err != nil {
		return false, err
	}
	if collaborationQueueFull(a) {
		return false, nil
	}
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT data FROM project_assistant_exchanges WHERE machine=? AND source=? AND json_extract(CAST(data AS TEXT),'$.delivery')='waiting' ORDER BY rowid LIMIT 1`, key.MachineID, key.BotID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var x pa.Exchange
	if err = json.Unmarshal(raw, &x); err != nil {
		return false, err
	}
	if x.Source.Key != key || x.Source.ConversationID != a.Conversation.ID || x.Project != a.Bot.Project || !repliesToSource(x.Outcome) || x.ReplyTurnID == "" {
		return false, failure(pa.ScopeDenied, "reply_promotion")
	}
	prior := x.Revision
	x.Revision++
	x.Delivery = "queued"
	turn := exchangeReplyTurn(a, x)
	body := x.Reply
	if body == "" {
		body = x.Request
	}
	m := exchangeMutation(a, x.ID+"-promote-source", at, turn, []pa.Message{exchangeNotice(a, x, turn.MessageID, body, turn.ID, at)})
	if err = exchangeApply(ctx, tx, &a, m); err != nil {
		return false, err
	}
	if err = exchangeWrite(ctx, tx, x, prior); err != nil {
		return false, err
	}
	return true, s.commitRoster(ctx, tx, key.MachineID)
}

// Explicit Stop drops source replies which had no queue space yet.
// The completed payload survives in both the exchange and historical notices.
func (s *Store) DropExchangeReplies(ctx context.Context, key pa.BotKey, at time.Time) error {
	unlock := s.lock(key)
	defer unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT data FROM project_assistant_exchanges WHERE machine=? AND source=? AND json_extract(CAST(data AS TEXT),'$.delivery')='waiting' ORDER BY rowid`, key.MachineID, key.BotID)
	if err != nil {
		return err
	}
	var pending []pa.Exchange
	for rows.Next() {
		var raw []byte
		var x pa.Exchange
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal(raw, &x); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, x)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	a, err := load(ctx, tx, key)
	if err != nil {
		return err
	}
	for _, x := range pending {
		if x.Source.Key != key || x.Source.ConversationID != a.Conversation.ID || x.Project != a.Bot.Project {
			return failure(pa.ScopeDenied, "reply_drop")
		}
		prior := x.Revision
		x.Revision++
		x.Delivery = "dropped"
		m := exchangeMutation(a, x.ID+"-drop-source", at, nil, []pa.Message{exchangeNotice(a, x, x.ID+"-reply-dropped", x.Reply, "", at)})
		if err = exchangeApply(ctx, tx, &a, m); err != nil {
			return err
		}
		if err = exchangeWrite(ctx, tx, x, prior); err != nil {
			return err
		}
	}
	if len(pending) == 0 {
		return nil
	}
	return s.commitRoster(ctx, tx, key.MachineID)
}

// repliesToSource reports whether a finished request comes back to the
// requesting Bot as a turn of its own. As in Codync (message_bot: "its final
// reply (or why it couldn't finish) comes back to you"), that includes a
// request a restart cut off (unknown) or the recipient's Stop cancelled: the
// requester then knows it is over and can tell the person, instead of waiting
// for a reply that will never arrive. The request itself is never replayed.
func repliesToSource(outcome pa.ExchangeOutcome) bool {
	switch outcome {
	case pa.ExchangeCompleted, pa.ExchangeFailed, pa.ExchangeUnknown, pa.ExchangeCancelled:
		return true
	}
	return false
}
