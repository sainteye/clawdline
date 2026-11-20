package projectassistantstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

const backgroundTaskSchema = `CREATE TABLE IF NOT EXISTS project_assistant_background_tasks (
	order_id INTEGER PRIMARY KEY AUTOINCREMENT,
	machine TEXT NOT NULL, bot TEXT NOT NULL, task TEXT NOT NULL,
	worker TEXT NOT NULL DEFAULT '', state TEXT NOT NULL DEFAULT 'waiting',
	started_at DATETIME, ended_at DATETIME,
	data BLOB NOT NULL, UNIQUE(machine,bot,task)
)`

// AdmitBackgroundTask puts a brief in the Bot's queue and commits its receipt.
// It performs no provider work. Only a still-current chat turn can add to it.
func (s *Store) AdmitBackgroundTask(ctx context.Context, command pa.Command, task pa.BackgroundTask) (pa.BackgroundTask, error) {
	if command.Operation != pa.DispatchOperation || command.Key != task.Fence.Key ||
		task.ID == "" || task.OriginTurnID == "" || task.ExchangeID != "" || task.CreatedAt.IsZero() {
		return pa.BackgroundTask{}, failure(pa.Invalid, "background_admission")
	}
	if err := pa.ValidateBackgroundBrief(task.Title, task.Brief); err != nil {
		return pa.BackgroundTask{}, err
	}
	unlock := s.lock(command.Key)
	defer unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return pa.BackgroundTask{}, err
	}
	defer tx.Rollback()
	prior, err := receipt(ctx, tx, command)
	if err != nil {
		return pa.BackgroundTask{}, err
	}
	if prior != nil {
		return readBackgroundTask(ctx, tx, command.Key, prior.ResourceID)
	}
	a, err := load(ctx, tx, command.Key)
	if err != nil {
		return pa.BackgroundTask{}, err
	}
	if err = pa.CheckFence(task.Fence, aggregateFence(a)); err != nil {
		return pa.BackgroundTask{}, err
	}
	turn := a.Turns[task.OriginTurnID]
	if a.Conversation.ActiveTurnID != turn.ID || turn.Fence != task.Fence || a.Bindings[task.Fence.BindingID].Revoked {
		return pa.BackgroundTask{}, failure(pa.StaleGeneration, "background_origin")
	}
	if a.BackgroundOwner != nil || !pa.CanDelegateBackground(turn) {
		return pa.BackgroundTask{}, failure(pa.ScopeDenied, "background_origin")
	}
	task.OriginMessageID = turn.MessageID
	if err = queueBackgroundTask(ctx, tx, task); err != nil {
		return pa.BackgroundTask{}, err
	}
	if err = writeReceipt(ctx, tx, pa.Receipt{ID: task.ID, Command: command, State: pa.EffectAccepted, ResourceID: task.ID}); err != nil {
		return pa.BackgroundTask{}, err
	}
	if err = noteBackgroundOwner(ctx, tx, command.Key); err != nil {
		return pa.BackgroundTask{}, err
	}
	if err = s.commitRoster(ctx, tx, command.Key.MachineID); err != nil {
		return pa.BackgroundTask{}, err
	}
	return task, nil
}

func queueBackgroundTask(ctx context.Context, tx *sql.Tx, task pa.BackgroundTask) error {
	key := task.Fence.Key
	var waiting int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_background_tasks
		WHERE machine=? AND bot=? AND state='waiting'`, key.MachineID, key.BotID).Scan(&waiting)
	if err != nil {
		return err
	}
	if waiting >= pa.MaxBackgroundPending {
		return failure(pa.CapacityFull, "background_waiting")
	}
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_background_tasks(machine,bot,task,data) VALUES(?,?,?,?)`,
		key.MachineID, key.BotID, task.ID, data)
	return err
}

func aggregateFence(a aggregate) pa.Fence {
	return pa.Fence{
		Key: a.Bot.Key, Project: a.Bot.Project, ConversationID: a.Conversation.ID,
		BindingID: a.Conversation.BindingID, Generation: a.Conversation.Generation,
	}
}

func readBackgroundTask(ctx context.Context, r reader, key pa.BotKey, id string) (pa.BackgroundTask, error) {
	var task pa.BackgroundTask
	var data []byte
	err := r.QueryRowContext(ctx, `SELECT data FROM project_assistant_background_tasks
		WHERE machine=? AND bot=? AND task=?`, key.MachineID, key.BotID, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return task, failure(pa.NotFound, "background_task")
	}
	if err != nil {
		return task, err
	}
	err = json.Unmarshal(data, &task)
	return task, err
}

// dropInterruptedBackgroundTasks discards work that was still in the queue
// when the originating chat turn stopped. It runs in the same commit as Stop.
func dropInterruptedBackgroundTasks(ctx context.Context, tx *sql.Tx, turn *pa.Turn) error {
	if turn == nil || (turn.State != pa.TurnCancelled && turn.State != pa.TurnUnknown) {
		return nil
	}
	key := turn.Fence.Key
	_, err := tx.ExecContext(ctx, `UPDATE project_assistant_background_tasks SET state='cancelled'
		WHERE machine=? AND bot=? AND state='waiting' AND json_extract(CAST(data AS TEXT),'$.origin_turn_id')=?`,
		key.MachineID, key.BotID, turn.ID)
	return err
}

// StartBackgroundTask creates the execution aggregate and its first queued turn
// in the same transaction as occupying a slot. Active workers keep their slot
// until execution and owned cleanup have reached a terminal state.
func (s *Store) StartBackgroundTask(ctx context.Context, key pa.BotKey, now time.Time) (pa.BotKey, error) {
	unlock := s.lock(key)
	defer unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return pa.BotKey{}, err
	}
	defer tx.Rollback()
	parent, err := load(ctx, tx, key)
	if err != nil {
		return pa.BotKey{}, err
	}
	if parent.BackgroundOwner != nil {
		return pa.BotKey{}, nil
	}
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_background_tasks
		WHERE machine=? AND bot=? AND state='active'`, key.MachineID, key.BotID).Scan(&active); err != nil {
		return pa.BotKey{}, err
	}
	if active >= pa.MaxBackgroundThreads {
		return pa.BotKey{}, nil
	}
	var id string
	err = tx.QueryRowContext(ctx, `SELECT task FROM project_assistant_background_tasks
		WHERE machine=? AND bot=? AND state='waiting'
		AND (?='' OR json_extract(CAST(data AS TEXT),'$.exchange_id') IS NOT NULL)
		ORDER BY order_id LIMIT 1`, key.MachineID, key.BotID, parent.Conversation.ActiveTurnID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return pa.BotKey{}, nil
	}
	if err != nil {
		return pa.BotKey{}, err
	}
	task, err := readBackgroundTask(ctx, tx, key, id)
	if err != nil {
		return pa.BotKey{}, err
	}
	if task.Fence.Key != parent.Bot.Key || task.Fence.Project != parent.Bot.Project ||
		task.Fence.ConversationID != parent.Conversation.ID {
		return pa.BotKey{}, failure(pa.ScopeDenied, "background_parent")
	}
	var exchange *pa.Exchange
	if task.ExchangeID != "" {
		x, err := exchangeRead(ctx, tx, key.MachineID, task.ExchangeID)
		if err != nil {
			return pa.BotKey{}, err
		}
		if x.Target != (pa.ExchangeEndpoint{Key: key, ConversationID: task.Fence.ConversationID}) ||
			x.DestinationProject() != task.Fence.Project || x.Outcome != pa.ExchangeQueued || x.ID != task.ID ||
			x.RequestWorkerID != backgroundWorkerID(task.ID) {
			return pa.BotKey{}, failure(pa.ScopeDenied, "background_exchange")
		}
		exchange = &x
	} else {
		origin := parent.Turns[task.OriginTurnID]
		if origin.State != pa.TurnSucceeded && origin.State != pa.TurnFailed {
			return pa.BotKey{}, nil
		}
	}
	worker := newBackgroundWorker(parent, task, now, exchange)
	if err = validateAggregate(worker); err != nil {
		return pa.BotKey{}, err
	}
	if err = persist(ctx, tx, worker, 0); err != nil {
		return pa.BotKey{}, err
	}
	message := worker.Messages[0]
	turn := worker.Turns[message.TurnID]
	for _, event := range []pa.Event{
		{Version: pa.ContractVersion, ID: message.ID, Fence: turn.Fence, At: now, Kind: pa.MessageCreated, Message: &message},
		{Version: pa.ContractVersion, ID: task.ID + "-worker-queued", Fence: turn.Fence, At: now, Kind: pa.TurnUpdated, Turn: &turn},
	} {
		if _, err = appendEvent(ctx, tx, worker, event); err != nil {
			return pa.BotKey{}, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE project_assistant_background_tasks SET state='active', worker=?, started_at=?
		WHERE machine=? AND bot=? AND task=?`, worker.Bot.Key.BotID, now, key.MachineID, key.BotID, task.ID); err != nil {
		return pa.BotKey{}, err
	}
	if err = noteBackgroundOwner(ctx, tx, key); err != nil {
		return pa.BotKey{}, err
	}
	return worker.Bot.Key, s.commitRoster(ctx, tx, key.MachineID)
}

func backgroundWorkerID(taskID string) string { return "thread-" + taskID }

func newBackgroundWorker(parent aggregate, task pa.BackgroundTask, now time.Time, exchange *pa.Exchange) aggregate {
	bot := parent.Bot
	bot.Key.BotID = backgroundWorkerID(task.ID)
	bot.ConversationID = task.ID
	bot.Version = 1
	bot.CreatedAt, bot.UpdatedAt = now, now
	bot.Hidden, bot.ManageTeam, bot.Lead = true, false, false
	bot.CreatedBy = nil
	fence := pa.Fence{Key: bot.Key, Project: bot.Project, ConversationID: bot.ConversationID, BindingID: task.ID + "-binding", Generation: 1}
	binding := pa.ExecutionBinding{Fence: fence, Agent: bot.Agent, LoginProfile: bot.LoginProfile, MemoryRevision: 1, InstructionRevision: bot.Instructions.Revision}
	turn := pa.Turn{
		ID: task.ID + "-initial", MessageID: task.ID + "-brief", Fence: fence,
		IdempotencyKey: task.ID, Origin: pa.UserOrigin, State: pa.TurnQueued,
	}
	if exchange != nil {
		turn.ID, turn.Origin, turn.ExchangeID, turn.Hops = exchange.RequestTurnID, pa.BotRequestOrigin, exchange.ID, exchange.Hops
	}
	message := pa.Message{ID: turn.MessageID, Seq: 1, TurnID: turn.ID, Fence: fence, Kind: pa.UserMessage, Text: task.Brief, At: now, Exchange: exchange}
	return aggregate{
		BackgroundOwner: &parent.Bot.Key,
		Bot:             bot,
		Conversation: pa.Conversation{
			Key: bot.Key, ID: bot.ConversationID, BindingID: fence.BindingID, Generation: fence.Generation,
			LastMessageSeq: 1, Version: 1, MemoryRevision: 1,
		},
		Turns: map[string]pa.Turn{turn.ID: turn}, Bindings: map[string]pa.ExecutionBinding{fence.BindingID: binding},
		Messages: []pa.Message{message}, Permissions: map[string]pa.PermissionOffer{}, WorkLinks: map[string]pa.WorkLink{},
		Memory:  pa.MemoryRevision{Key: bot.Key, Project: bot.Project, Revision: 1},
		Entries: map[string]pa.MemoryEntry{}, ReadCursors: map[string]pa.ReadCursor{},
	}
}

// projectBackgroundTurn keeps queue admission and final publication in the
// worker's transaction. A terminal turn with queued input leaves work active.
func projectBackgroundTurn(ctx context.Context, tx *sql.Tx, a aggregate, m app.Mutation) error {
	if a.BackgroundOwner == nil || m.Turn == nil {
		return nil
	}
	owner := *a.BackgroundOwner
	id := a.Conversation.ID
	match := []any{owner.MachineID, owner.BotID, id, a.Bot.Key.BotID}
	switch m.Turn.State {
	case pa.TurnAccepted, pa.TurnQueued:
		result, err := tx.ExecContext(ctx, `UPDATE project_assistant_background_tasks
			SET state='active', started_at=?, ended_at=NULL
			WHERE machine=? AND bot=? AND task=? AND worker=? AND state<>'active'`, append([]any{a.Bot.UpdatedAt}, match...)...)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil || n == 0 {
			return err
		}
		return noteBackgroundOwner(ctx, tx, owner)
	case pa.TurnSucceeded, pa.TurnFailed, pa.TurnCancelled, pa.TurnUnknown:
	default:
		return nil
	}
	for _, turn := range a.Turns {
		switch turn.State {
		case pa.TurnQueued, pa.TurnAccepted, pa.TurnRunning, pa.TurnWaitingPermission:
			return nil
		}
	}
	state := m.Turn.State
	for _, turn := range a.Turns {
		if turn.State == pa.TurnUnknown {
			state = pa.TurnUnknown
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE project_assistant_background_tasks SET state=?, ended_at=?
		WHERE machine=? AND bot=? AND task=? AND worker=? AND state='active'`, append([]any{state, a.Bot.UpdatedAt}, match...)...)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil || n == 0 {
		return err
	}
	if err = noteBackgroundOwner(ctx, tx, owner); err != nil {
		return err
	}
	if state != pa.TurnSucceeded || m.Turn.Origin == pa.BotRequestOrigin {
		return nil
	}
	for i := len(a.Messages) - 1; i >= 0; i-- {
		message := a.Messages[i]
		if message.Kind == pa.AssistantMessage && message.TurnID == m.Turn.ID && message.Text != "" && message.Output == nil {
			return publishBackgroundReport(ctx, tx, owner, id, *m.Turn, message)
		}
	}
	return nil
}

func publishBackgroundReport(ctx context.Context, tx *sql.Tx, key pa.BotKey, taskID string, turn pa.Turn, reply pa.Message) error {
	task, err := readBackgroundTask(ctx, tx, key, taskID)
	if err != nil {
		return err
	}
	a, err := load(ctx, tx, key)
	if err != nil {
		return err
	}
	if task.Fence.Project != a.Bot.Project || task.Fence.ConversationID != a.Conversation.ID {
		return failure(pa.ScopeDenied, "background_report")
	}
	a.Conversation.LastMessageSeq++
	a.Conversation.Version++
	expected := a.Bot.Version
	a.Bot.Version++
	a.Bot.UpdatedAt = reply.At
	message := pa.Message{
		ID: "background-report-" + turn.ID, Fence: aggregateFence(a),
		Seq: a.Conversation.LastMessageSeq, Kind: pa.AssistantMessage,
		Text: reply.Text, QuoteMessageID: task.OriginMessageID, At: reply.At,
	}
	a.Messages = append(a.Messages, message)
	if err = validateAggregate(a); err != nil {
		return err
	}
	if _, err = appendEvent(ctx, tx, a, pa.Event{
		Version: pa.ContractVersion, ID: message.ID, Fence: message.Fence,
		At: message.At, Kind: pa.MessageCreated, Message: &message,
	}); err != nil {
		return err
	}
	return persist(ctx, tx, a, expected)
}

// BackgroundOwner resolves an internal worker by durable ownership, rather
// than treating a client-supplied worker ID as authority.
func (s *Store) BackgroundOwner(ctx context.Context, worker pa.BotKey) (pa.BotKey, error) {
	var owner pa.BotKey
	if !strings.HasPrefix(worker.BotID, "thread-") {
		return owner, nil
	}
	err := s.db.QueryRowContext(ctx, `SELECT bot FROM project_assistant_background_tasks WHERE machine=? AND worker=? AND worker<>''`,
		worker.MachineID, worker.BotID).Scan(&owner.BotID)
	if errors.Is(err, sql.ErrNoRows) {
		return owner, nil
	}
	if err != nil {
		return owner, err
	}
	owner.MachineID = worker.MachineID
	return owner, nil
}

type BackgroundQueue struct {
	Owner   pa.BotKey
	Workers []pa.BotKey
}

func (s *Store) BackgroundQueues(ctx context.Context, machine string) ([]BackgroundQueue, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT bot, worker FROM project_assistant_background_tasks
		WHERE machine=? AND state IN ('waiting','active') GROUP BY bot,worker ORDER BY bot,worker`, machine)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var queues []BackgroundQueue
	for rows.Next() {
		var bot, worker string
		if err = rows.Scan(&bot, &worker); err != nil {
			return nil, err
		}
		if len(queues) == 0 || queues[len(queues)-1].Owner.BotID != bot {
			queues = append(queues, BackgroundQueue{Owner: pa.BotKey{MachineID: machine, BotID: bot}})
		}
		if worker != "" {
			i := len(queues) - 1
			queues[i].Workers = append(queues[i].Workers, pa.BotKey{MachineID: machine, BotID: worker})
		}
	}
	return queues, rows.Err()
}

func backgroundTaskSummaries(ctx context.Context, tx *sql.Tx, key pa.BotKey) ([]pa.BackgroundTaskSummary, error) {
	rows, err := tx.QueryContext(ctx, `SELECT
		t.task, t.worker, t.state, t.started_at, t.ended_at,
		json_extract(CAST(t.data AS TEXT),'$.title'),
		json_extract(CAST(t.data AS TEXT),'$.origin_message_id'),
		json_extract(CAST(b.data AS TEXT),'$.Turns.'||json_quote(json_extract(CAST(b.data AS TEXT),'$.Conversation.active_turn_id'))||'.state'),
		CASE WHEN json_extract(CAST(b.data AS TEXT),'$.RosterTrace.trace.turn_id')=json_extract(CAST(b.data AS TEXT),'$.Conversation.active_turn_id')
		  AND json_extract(CAST(b.data AS TEXT),'$.RosterTrace.trace.complete')=0
		  THEN json_extract(CAST(b.data AS TEXT),'$.RosterTrace.trace') END
		FROM project_assistant_background_tasks t
		LEFT JOIN project_assistant_bots b ON t.worker=b.bot AND t.machine=b.machine
		WHERE t.machine=? AND t.bot=?
		ORDER BY CASE t.state WHEN 'active' THEN 0 WHEN 'waiting' THEN 1 ELSE 2 END,
		CASE WHEN t.state IN ('active','waiting') THEN t.order_id ELSE -t.order_id END LIMIT ?`,
		key.MachineID, key.BotID, pa.MaxPageRows)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []pa.BackgroundTaskSummary
	for rows.Next() {
		var task pa.BackgroundTaskSummary
		var worker string
		var active, traceJSON sql.NullString
		if err = rows.Scan(&task.ID, &worker, &task.State, &task.StartedAt, &task.EndedAt,
			&task.Title, &task.OriginMessageID, &active, &traceJSON); err != nil {
			return nil, err
		}
		if worker != "" {
			task.Worker = &pa.BotKey{MachineID: key.MachineID, BotID: worker}
		}
		if task.State == "active" && active.String == string(pa.TurnWaitingPermission) {
			task.State = "waiting_permission"
		}
		if task.State == "active" && traceJSON.Valid {
			var trace pa.Trace
			if err = json.Unmarshal([]byte(traceJSON.String), &trace); err != nil {
				return nil, err
			}
			task.Activity = traceActivity(trace)
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func backgroundDeleteKeys(ctx context.Context, tx *sql.Tx, owner pa.BotKey) ([]pa.BotKey, error) {
	rows, err := tx.QueryContext(ctx, `SELECT worker,state FROM project_assistant_background_tasks WHERE machine=? AND bot=?`, owner.MachineID, owner.BotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []pa.BotKey
	for rows.Next() {
		var worker, state string
		if err = rows.Scan(&worker, &state); err != nil {
			return nil, err
		}
		if state == "active" {
			return nil, failure(pa.StaleVersion, "delete_background_busy")
		}
		if worker != "" {
			keys = append(keys, pa.BotKey{MachineID: owner.MachineID, BotID: worker})
		}
	}
	return keys, rows.Err()
}
