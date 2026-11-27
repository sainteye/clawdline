package projectassistantstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/botreadtrace"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// ViewStore borrows the host's query-only pool. It owns only an incarnation and
// a private signing key: no database lifecycle, writer, schema or cursor cache.
type ViewStore struct {
	borrowedDB  *sql.DB
	signingKey  [sha256.Size]byte
	sourceEpoch string
}

var _ app.ReadViews = (*ViewStore)(nil)

func NewReadViews(db *sql.DB) (*ViewStore, error) {
	if db == nil {
		return nil, failure(pa.Invalid, "read_view_database")
	}
	s := &ViewStore{borrowedDB: db}
	if _, err := rand.Read(s.signingKey[:]); err != nil {
		return nil, err
	}
	var epoch [sha256.Size]byte
	if _, err := rand.Read(epoch[:]); err != nil {
		return nil, err
	}
	s.sourceEpoch = base64.RawURLEncoding.EncodeToString(epoch[:])
	return s, nil
}

func (s *ViewStore) Read(ctx context.Context, q app.ReadViewQuery) (app.ReadView, error) {
	out := app.ReadView{Mode: q.Request.Mode, SourceEpoch: s.sourceEpoch, Items: []app.ReadViewItem{}}
	if err := pa.CheckReadViewRequest(q.Request); err != nil {
		return out, err
	}
	if strings.TrimSpace(q.AuthorizationBinding) == "" || !utf8.ValidString(q.AuthorizationBinding) {
		return out, failure(pa.ScopeDenied, "read_view_authorization")
	}
	if q.ByteLimit <= 0 || q.ByteLimit > cloudops.ReadPlaintextBytesLimit || q.CursorByteLimit <= 0 || q.CursorByteLimit > pa.MaxReadViewCursorBytes {
		return out, failure(pa.Invalid, "read_view_budget")
	}
	if len(q.Request.Cursor) > q.CursorByteLimit {
		return out, failure(pa.CapacityFull, "read_view_cursor_bytes")
	}
	if q.Request.Mode != pa.ReadViewSnapshot && q.Request.SourceEpoch != s.sourceEpoch {
		return out, failure(pa.CursorExpired, "source_epoch")
	}
	acquire := botreadtrace.Measure(ctx, "sqlite_acquire")
	tx, err := s.borrowedDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	acquire()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	defer botreadtrace.Measure(ctx, "sqlite_read")()
	rawBytes := 0
	h, bot, err := viewHead(ctx, tx, q, &rawBytes)
	if err != nil {
		return out, err
	}
	out.Head = h
	// Every persisted write passes validateAggregate, which checks each
	// message, turn, permission, work link and binding with its relations.
	// A read checks the rows it returns, not the whole history again.
	chatHead := botreadtrace.Measure(ctx, "sqlite_chat_head")
	err = viewOwnChatHead(ctx, tx, &out.Head)
	chatHead()
	if err != nil {
		return out, err
	}
	if q.Request.Mode == pa.ReadViewSnapshot || q.Request.Mode == pa.ReadViewEvents {
		background := botreadtrace.Measure(ctx, "sqlite_background_tasks")
		out.Head.BackgroundTasks, err = backgroundTaskSummaries(ctx, tx, h.Key)
		background()
		if err != nil {
			return out, err
		}
	}
	if err = viewCounts(ctx, tx, q, &out.Head); err != nil {
		return out, err
	}
	var position viewPosition
	if q.Request.Cursor != "" {
		position, err = s.position(q, out.Head)
		if err != nil {
			return out, err
		}
	}
	// The SQL-to-Go raw budget is shared by metadata, mandatory controls and
	// candidate payloads. Transport still measures the exact encoded envelope;
	// raw bytes are not a guessed substitute for that measurement.
	remaining := q
	remaining.ByteLimit -= rawBytes
	switch q.Request.Mode {
	case pa.ReadViewSnapshot:
		mandatoryBytes := 0
		out.Snapshot, err = viewSnapshot(ctx, tx, remaining, out.Head, bot, &mandatoryBytes)
		remaining.ByteLimit -= mandatoryBytes
		if err == nil {
			err = s.timeline(ctx, tx, remaining, &out, viewPosition{})
		}
	case pa.ReadViewHistory:
		err = s.timeline(ctx, tx, remaining, &out, position)
	case pa.ReadViewEvents:
		err = viewEvents(ctx, tx, remaining, &out)
	case pa.ReadViewPermissions:
		err = s.permissions(ctx, tx, remaining, &out, position)
	case pa.ReadViewProgress:
		err = s.progress(ctx, tx, remaining, &out, position)
	case pa.ReadViewTraces:
		err = s.traces(ctx, tx, remaining, &out, position)
	case pa.ReadViewDetail:
		err = s.detail(ctx, tx, remaining, &out, position)
	}
	if err != nil {
		return app.ReadView{}, err
	}
	reactions := botreadtrace.Measure(ctx, "sqlite_reactions")
	if out.Snapshot != nil {
		for i := range out.Snapshot.QueuedMessages {
			if err = messageReactions(ctx, tx, &out.Snapshot.QueuedMessages[i]); err != nil {
				reactions()
				return app.ReadView{}, err
			}
		}
	}
	for i := range out.Items {
		if m := out.Items[i].Message; m != nil {
			if err = messageReactions(ctx, tx, m); err != nil {
				reactions()
				return app.ReadView{}, err
			}
		}
	}
	reactions()
	commit := botreadtrace.Measure(ctx, "sqlite_commit")
	err = tx.Commit()
	commit()
	if err != nil {
		return app.ReadView{}, err
	}
	return out, nil
}

func viewCheckFence(f pa.Fence, h app.ReadViewHead, current bool) error {
	if f.Key != h.Key || f.Project != h.Project || f.ConversationID != h.Conversation.ID {
		return failure(pa.ScopeDenied, "read_view_fact")
	}
	if f.Generation < 0 || f.Generation > h.CurrentFence.Generation || (f.Generation == 0) != (f.BindingID == "") {
		return failure(pa.Invalid, "read_view_fence")
	}
	if current && f != h.CurrentFence {
		return failure(pa.CursorExpired, "read_view_fence")
	}
	return nil
}

func viewTurnProjection(value string) string {
	// Keep mandatory snapshots bounded even if an old or imported failure has
	// an arbitrary field. Only host diagnostic categories are public summaries.
	field := "json_extract(" + value + ",'$.failure.field')"
	detail := "CASE WHEN " + field + " IN ('prompt_timeout','prompt_stopped','acp_eof','prompt_result','prompt_max_tokens','prompt_max_turn_requests','owned_cleanup','owned_cleanup_timeout','restart','restart_confirmed','restart_resumed','acknowledged','provider_usage_limit','provider_out_of_credits','provider_rate_limited','provider_overloaded','provider_signed_out','provider_model_missing') THEN " + field + " ELSE '' END"
	fallback := viewSteerFallbackProjection(value)
	return `json_object('id',json_extract(` + value + `,'$.id'),'fence',json_extract(` + value + `,'$.fence'),
 'message_id',json_extract(` + value + `,'$.message_id'),'state',json_extract(` + value + `,'$.state'),
 'attempts',json_extract(` + value + `,'$.attempts'),'failure',json_object('code',json_extract(` + value + `,'$.failure.code'),'field',` + detail + `),'started_at',json_extract(` + value + `,'$.started_at'),'queue_edit_id',json_extract(` + value + `,'$.queue_edit_id'),
 'origin',json_extract(` + value + `,'$.origin'),'idempotency_key',json_extract(` + value + `,'$.idempotency_key'),
 'steering',json(CASE WHEN json_extract(` + value + `,'$.steering') THEN 'true' ELSE 'false' END),
 'steer_fallback',` + fallback + `,
 'released_restart',json(CASE WHEN json_extract(` + value + `,'$.released_restart') THEN 'true' ELSE 'false' END),
 'room',CASE WHEN json_extract(` + value + `,'$.room') IS NOT NULL THEN json_object() END,
 'network',CASE WHEN json_extract(` + value + `,'$.network') IS NOT NULL THEN json_object() END)`
}

func viewSteerFallbackProjection(value string) string {
	reason := "json_extract(" + value + ",'$.steer_fallback')"
	known := make([]string, len(pa.SteerFallbacks))
	for i, code := range pa.SteerFallbacks {
		known[i] = "'" + strings.ReplaceAll(string(code), "'", "''") + "'"
	}
	// Project future reasons to the generic queue label without adding
	// arbitrary legacy data to the bounded snapshot.
	return "CASE WHEN json_extract(" + value + ",'$.state') = 'queued' AND " + reason + " <> '' THEN CASE WHEN " +
		reason + " IN (" + strings.Join(known, ",") + ") THEN " + reason + " ELSE 'unknown' END END"
}

func viewTurn(raw string, h app.ReadViewHead) (app.ReadTurnSummary, error) {
	var turn pa.Turn
	if json.Unmarshal([]byte(raw), &turn) != nil || turn.ID == "" || turn.MessageID == "" {
		return app.ReadTurnSummary{}, failure(pa.Invalid, "read_view_turn")
	}
	if err := viewCheckFence(turn.Fence, h, false); err != nil {
		return app.ReadTurnSummary{}, err
	}
	summary := app.ReadTurnSummary{ID: turn.ID, Fence: turn.Fence, MessageID: turn.MessageID, State: turn.State, Attempts: turn.Attempts, QueueEditID: turn.QueueEditID, Origin: turn.Origin, ReleasedRestart: turn.ReleasedRestart, SteerFallback: turn.SteerFallback}
	if turn.StartedAt != nil {
		summary.StartedAt = turn.StartedAt.UTC().Format(time.RFC3339)
	}
	if turn.Failure != nil {
		summary.FailureCode = turn.Failure.Code
		summary.FailureField = turn.Failure.Field
	}
	return summary, nil
}

func viewSnapshot(ctx context.Context, tx *sql.Tx, q app.ReadViewQuery, h app.ReadViewHead, bot pa.Bot, rawBytes *int) (*app.ReadSnapshot, error) {
	out := &app.ReadSnapshot{Bot: bot, PendingTurns: []app.ReadTurnSummary{}, QueuedMessages: []pa.Message{}}
	statement := strings.TrimSuffix(viewAggregateSQL, " ") + `, pending AS MATERIALIZED (
 SELECT ` + viewTurnProjection("t.value") + ` j,json_extract(t.value,'$.message_id') message_id,t.key id
 FROM a,fact_turns t WHERE t.type='object'
 AND ` + ownChatTurnSQL("t.key") + `
 AND json_extract(t.value,'$.state') IN ('accepted','queued','running','waiting_permission')
), matched AS MATERIALIZED (
 SELECT p.j,p.id,m.value m,m.ord anchor FROM pending p
 LEFT JOIN fact_messages m ON m.member=p.message_id
), budget AS (
 SELECT *,sum(length(CAST(j AS BLOB))+coalesce(length(CAST(m AS BLOB)),0)) OVER(ORDER BY anchor,id COLLATE BINARY) spent FROM matched
)
SELECT CASE WHEN spent<=? THEN j END,CASE WHEN spent<=? THEN m END
 FROM budget ORDER BY anchor,id COLLATE BINARY`
	// Leave room for navigation, recent transcript and result metadata even
	// when queued work occupies most of the persisted conversation.
	queueBudget := q.ByteLimit / 2
	var order []pa.Turn
	pending := botreadtrace.Measure(ctx, "sqlite_pending_turns")
	rows, err := tx.QueryContext(ctx, statement, h.Key.MachineID, h.Key.BotID, queueBudget, queueBudget)
	if err != nil {
		pending()
		return nil, err
	}
	for rows.Next() {
		var raw, message sql.NullString
		if err = rows.Scan(&raw, &message); err != nil {
			rows.Close()
			pending()
			return nil, err
		}
		if !raw.Valid {
			// Past the byte budget: counted, never silently sliced.
			out.PendingOmitted++
			continue
		}
		if !message.Valid {
			rows.Close()
			pending()
			return nil, failure(pa.Invalid, "read_view_turn_message")
		}
		summary, e := viewTurn(raw.String, h)
		var m pa.Message
		if e == nil {
			e = json.Unmarshal([]byte(message.String), &m)
		}
		if e == nil {
			e = viewCheckFence(m.Fence, h, false)
		}
		if e != nil || m.ID != summary.MessageID || m.Kind != pa.UserMessage && m.Kind != pa.NoticeMessage || m.TurnID != summary.ID {
			rows.Close()
			pending()
			return nil, failure(pa.Invalid, "read_view_turn_message")
		}
		var fact pa.Turn
		_ = json.Unmarshal([]byte(raw.String), &fact) // viewTurn already decoded it
		order = append(order, fact)
		out.PendingTurns = append(out.PendingTurns, summary)
		out.QueuedMessages = append(out.QueuedMessages, m)
		*rawBytes += len(raw.String) + len(message.String)
	}
	err = rows.Err()
	rows.Close()
	pending()
	if err != nil {
		return nil, err
	}
	// Show the queue in the order the worker runs it (Store.PendingTurns).
	position := map[string]int{}
	for i, t := range out.PendingTurns {
		position[t.ID] = i
	}
	turns, messages := out.PendingTurns, out.QueuedMessages
	out.PendingTurns, out.QueuedMessages = make([]app.ReadTurnSummary, 0, len(turns)), make([]pa.Message, 0, len(messages))
	for _, t := range pa.OrderQueue(order) {
		i := position[t.ID]
		out.PendingTurns = append(out.PendingTurns, turns[i])
		out.QueuedMessages = append(out.QueuedMessages, messages[i])
	}
	// Join on scalar message positions so SQLite can look up each turn's
	// anchor without walking the complete message array for each turn. An
	// interrupted restart can still block the queue behind newer messages.
	statement = strings.TrimSuffix(viewAggregateSQL, " ") + `, message_positions AS NOT MATERIALIZED (
 SELECT m.member id,m.ord anchor FROM fact_messages m
), terminal AS MATERIALIZED (
 SELECT ` + viewTurnProjection("t.value") + ` j,m.anchor,t.key id
 FROM a,fact_turns t
 LEFT JOIN message_positions m ON m.id=json_extract(t.value,'$.message_id')
 WHERE t.type='object' AND ` + ownChatTurnSQL("t.key") + `
 AND ` + viewBoundFenceSQL("t.value") + ` ORDER BY CASE WHEN json_extract(t.value,'$.state')='unknown'
 AND json_extract(t.value,'$.failure.field') IN ('restart','restart_confirmed') THEN 0 ELSE 1 END,
 anchor DESC,id COLLATE BINARY DESC LIMIT 1
)
SELECT CASE WHEN length(CAST(j AS BLOB))<=? THEN j END FROM terminal WHERE json_extract(j,'$.state') IN ('succeeded','failed','cancelled','unknown')`
	var last sql.NullString
	lastTurn := botreadtrace.Measure(ctx, "sqlite_last_turn")
	err = tx.QueryRowContext(ctx, statement, h.Key.MachineID, h.Key.BotID, q.ByteLimit-*rawBytes).Scan(&last)
	lastTurn()
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if !last.Valid {
		return nil, failure(pa.CapacityFull, "read_view_snapshot_bytes")
	}
	*rawBytes += len(last.String)
	summary, err := viewTurn(last.String, h)
	if err == nil {
		out.LastTurn = &summary
	}
	return out, err
}

func viewRowFailure() app.ReadViewItem {
	return app.ReadViewItem{Failure: &pa.Failure{Code: pa.CapacityFull, Field: "read_view_item_bytes"}}
}

func (s *ViewStore) timeline(ctx context.Context, tx *sql.Tx, q app.ReadViewQuery, out *app.ReadView, p viewPosition) error {
	defer botreadtrace.Measure(ctx, "sqlite_timeline")()
	rows, err := tx.QueryContext(ctx, viewTimelineSQL(), out.Head.Key.MachineID, out.Head.Key.BotID,
		q.Request.TeamCards, p.ID != "", p.Anchor, p.Kind, p.ID, q.Request.PageSize+1,
		out.Head.Key.MachineID, out.Head.Key.BotID, out.Head.Key.BotID, q.ByteLimit, q.ByteLimit, q.ByteLimit, q.ByteLimit)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if len(out.Items) == q.Request.PageSize {
			out.HasMore = true
			break
		}
		var anchor int64
		var kind int
		var id string
		var raw sql.NullString
		if err = rows.Scan(&anchor, &kind, &id, &raw); err != nil {
			return err
		}
		if !raw.Valid {
			out.Items = append(out.Items, viewRowFailure())
			out.HasMore = true
			break
		}
		item := app.ReadViewItem{ID: id, AnchorMessageSeq: anchor, ObservedEventSeq: out.Head.EventFrontier}
		switch kind {
		case 0:
			var message struct {
				pa.Message
				JoinedTurnID string `json:"joined_turn_id"`
			}
			if json.Unmarshal([]byte(raw.String), &message) != nil || message.ID != id || message.Seq != anchor {
				return failure(pa.Invalid, "read_view_message")
			}
			item.Kind, item.Message, item.JoinedTurnID = app.ReadItemMessage, &message.Message, message.JoinedTurnID
			if err = viewCheckFence(item.Message.Fence, out.Head, false); err != nil {
				return err
			}
			if q.Request.ExchangeExcerpts {
				pa.ExcerptExchange(item.Message)
			}
		case 1:
			item.Kind, item.Work = app.ReadItemWork, &pa.WorkLink{}
			if json.Unmarshal([]byte(raw.String), item.Work) != nil || item.Work.ID != id || item.Work.TaskID == "" {
				return failure(pa.Invalid, "read_view_work")
			}
			if err = viewCheckFence(item.Work.Fence, out.Head, false); err != nil {
				return err
			}
		case 2:
			item.Kind, item.Permission = app.ReadItemPermission, &pa.PermissionOffer{}
			if json.Unmarshal([]byte(raw.String), item.Permission) != nil || item.Permission.ID != id || anchor < 1 || anchor > out.Head.Conversation.LastMessageSeq || item.Permission.Action == nil || !pa.TeamPermissionKind(item.Permission.Action.ToolKind) {
				return failure(pa.Invalid, "read_view_team_card")
			}
			if err = viewCheckFence(item.Permission.Fence, out.Head, false); err != nil {
				return err
			}
		default:
			return failure(pa.Invalid, "read_view_item")
		}
		item.AfterCursor, item.CursorFailure = s.cursor(q, out.Head, viewPosition{Collection: "history", Anchor: anchor, Kind: kind, ID: id})
		out.Items = append(out.Items, item)
	}
	return rows.Err()
}

func viewEvents(ctx context.Context, tx *sql.Tx, q app.ReadViewQuery, out *app.ReadView) error {
	out.NextSeq = q.Request.AfterSeq
	if q.Request.AfterSeq > out.Head.EventFrontier {
		return failure(pa.CursorExpired, "event_cursor")
	}
	statement := strings.TrimSuffix(viewAggregateSQL, " ") + `, candidates AS MATERIALIZED (
 SELECT e.seq,e.event_id,CAST(e.data AS TEXT) j,length(e.data)+length(CAST(e.event_id AS BLOB)) n FROM a,project_assistant_events e
 WHERE e.machine=? AND e.bot=? AND e.seq>? AND ` + ownChatEventSQL("CAST(e.data AS TEXT)") + ` ORDER BY e.seq LIMIT ?
), budget AS (SELECT *,sum(n) OVER(ORDER BY seq) spent FROM candidates)
SELECT seq,CASE WHEN n<=? AND spent<=? THEN event_id ELSE '' END,
 CASE WHEN n<=? AND spent<=? THEN j END FROM budget ORDER BY seq`
	rows, err := tx.QueryContext(ctx, statement, out.Head.Key.MachineID, out.Head.Key.BotID, out.Head.Key.MachineID, out.Head.Key.BotID, q.Request.AfterSeq, q.Request.PageSize+1,
		q.ByteLimit, q.ByteLimit, q.ByteLimit, q.ByteLimit)
	if err != nil {
		return err
	}
	defer rows.Close()
	var checks viewEventFactChecks
	for rows.Next() {
		if len(out.Items) == q.Request.PageSize {
			out.HasMore = true
			break
		}
		var seq int64
		var id string
		var raw sql.NullString
		if err = rows.Scan(&seq, &id, &raw); err != nil {
			return err
		}
		if !raw.Valid {
			out.Items = append(out.Items, viewRowFailure())
			out.HasMore = true
			break
		}
		var e pa.Event
		if json.Unmarshal([]byte(raw.String), &e) != nil || e.Seq != seq || e.ID != id {
			return failure(pa.Invalid, "read_view_event")
		}
		if err = pa.ValidateEvent(e); err != nil {
			return err
		}
		if err = viewCheckFence(e.Fence, out.Head, false); err != nil {
			return err
		}
		if err = viewEventPayload(e, out.Head); err != nil {
			return err
		}
		if err = checks.verify(ctx, tx, e, out.Head); err != nil {
			return err
		}
		// Replay the stored metadata patch. Current own-chat activity belongs
		// to the roster; substituting it here leaks future facts into history.
		var anchor int64
		if e.Work != nil {
			anchor = e.WorkAnchorMessageSeq
			if anchor > out.Head.Conversation.LastMessageSeq {
				return failure(pa.Invalid, "work_anchor_message_seq")
			}
		} else if q.Request.TeamCards && e.Permission != nil && e.Permission.Action != nil && pa.TeamPermissionKind(e.Permission.Action.ToolKind) {
			anchor = e.Permission.AnchorMessageSeq
		}
		out.Items = append(out.Items, app.ReadViewItem{ID: id, Kind: app.ReadItemEvent, AnchorMessageSeq: anchor, ObservedEventSeq: seq, Event: &e})
		out.NextSeq = seq
	}
	return rows.Err()
}

func viewEventPayload(e pa.Event, h app.ReadViewHead) error {
	// Events do not load the aggregate, but nested facts must still name the
	// same stored source identity. An outer fence never launders a foreign row.
	if e.Message != nil && e.Message.Fence != e.Fence || e.Turn != nil && e.Turn.Fence != e.Fence ||
		e.Permission != nil && e.Permission.Fence != e.Fence || e.Work != nil && e.Work.Fence != e.Fence {
		return failure(pa.ScopeDenied, "read_view_event_payload")
	}
	if e.Memory != nil && (e.Memory.Key != h.Key || e.Memory.Project != h.Project || e.Memory.Revision > h.MemoryRevision) {
		return failure(pa.ScopeDenied, "read_view_event_payload")
	}
	if e.Read != nil && (e.Read.Key != h.Key || e.Read.ConversationID != h.Conversation.ID) {
		return failure(pa.ScopeDenied, "read_view_event_payload")
	}
	var source *pa.SourceStatus
	if e.Bot != nil {
		b := e.Bot.Bot
		if b.Key != h.Key || b.Project != h.Project || b.ConversationID != h.Conversation.ID ||
			e.Bot.ReadCursor.Key != (pa.BotKey{}) && e.Bot.ReadCursor.Key != h.Key ||
			e.Bot.ReadCursor.ConversationID != "" && e.Bot.ReadCursor.ConversationID != h.Conversation.ID {
			return failure(pa.ScopeDenied, "read_view_event_payload")
		}
		source = &e.Bot.Source
	}
	if e.Source != nil {
		if e.Source.MachineID != h.Key.MachineID {
			return failure(pa.ScopeDenied, "read_view_event_payload")
		}
		source = e.Source
	}
	if source != nil {
		if source.MachineID != "" && source.MachineID != h.Key.MachineID {
			return failure(pa.ScopeDenied, "read_view_event_payload")
		}
		for _, cap := range source.Capabilities {
			if cap.Key != h.Key || cap.Project != h.Project {
				return failure(pa.ScopeDenied, "read_view_event_payload")
			}
		}
	}
	return nil
}

type viewEventBinding struct {
	id         string
	generation int64
}

type viewEventTrace struct {
	binding viewEventBinding
	turnID  string
}

// These positive results belong to one page and its SQLite read transaction.
// Repeated trace chunks and events often name the same binding and turn.
type viewEventFactChecks struct {
	bindings map[viewEventBinding]bool
	traces   map[viewEventTrace]bool
}

func (c *viewEventFactChecks) verify(ctx context.Context, tx *sql.Tx, e pa.Event, h app.ReadViewHead) error {
	// Scalar existence checks borrow the same transaction. In particular,
	// checking Trace against its stored Turn does not deserialize that Turn's
	// potentially large captured instructions or the enclosing aggregate.
	binding := viewEventBinding{id: e.Fence.BindingID, generation: e.Fence.Generation}
	if binding.id != "" && !c.bindings[binding] {
		statement := viewAggregateSQL + `SELECT EXISTS(SELECT 1 FROM a,fact_bindings b
 WHERE b.type='object' AND b.key=? AND b.key=json_extract(b.value,'$.fence.binding_id')
 AND json_extract(b.value,'$.fence.generation')=? AND ` + viewFenceSQL("b.value", false) + `)`
		var valid bool
		if err := tx.QueryRowContext(ctx, statement, h.Key.MachineID, h.Key.BotID, e.Fence.BindingID, e.Fence.Generation).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return failure(pa.ScopeDenied, "read_view_event_binding")
		}
		if c.bindings == nil {
			c.bindings = make(map[viewEventBinding]bool)
		}
		c.bindings[binding] = true
	}
	if e.Trace != nil {
		trace := viewEventTrace{binding: binding, turnID: e.Trace.TurnID}
		if c.traces[trace] {
			return nil
		}
		statement := viewAggregateSQL + `SELECT EXISTS(SELECT 1 FROM a,fact_turns t
 WHERE t.type='object' AND t.key=? AND t.key=json_extract(t.value,'$.id') AND json_extract(t.value,'$.fence.generation')=?
 AND json_extract(t.value,'$.fence.binding_id')=? AND ` + viewFenceSQL("t.value", false) + `)`
		var valid bool
		if err := tx.QueryRowContext(ctx, statement, h.Key.MachineID, h.Key.BotID, e.Trace.TurnID, e.Fence.Generation, e.Fence.BindingID).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return failure(pa.ScopeDenied, "read_view_event_trace")
		}
		if c.traces == nil {
			c.traces = make(map[viewEventTrace]bool)
		}
		c.traces[trace] = true
	}
	return nil
}

func (s *ViewStore) permissions(ctx context.Context, tx *sql.Tx, q app.ReadViewQuery, out *app.ReadView, p viewPosition) error {
	// Keep the live proposal count on the first page even with an extensive
	// history. The summary only exposes the receipt choice, never the fact.
	statement := strings.TrimSuffix(viewAggregateSQL, " ") + `, offers AS NOT MATERIALIZED (
 SELECT p.key id,p.value,
 CASE WHEN json_extract(p.value,'$.state')='answered' THEN 2 WHEN ` + viewMemoryProposalSQL + ` THEN 0 ELSE 1 END priority
 FROM a,fact_permissions p WHERE p.type='object' AND
 ((json_extract(p.value,'$.state')='pending' AND (NOT (` + viewMemoryProposalSQL + `) OR ` + rosterOpenDecisionSQL("p.value") + `))
 OR (json_extract(p.value,'$.state')='answered' AND json_extract(p.value,'$.kind')='decision' AND ` + viewMemoryProposalSQL + `))
 AND ` + viewPermissionFenceSQL("p.value") + `
), candidates AS MATERIALIZED (
 SELECT id,priority,json_object('id',id,'fence',json_extract(value,'$.fence'),
 'turn_id',json_extract(value,'$.turn_id'),'state',json_extract(value,'$.state'),
 'kind',json_extract(value,'$.kind'),'decision',CASE WHEN json_extract(value,'$.kind')='decision'
 THEN json_object('question',json_extract(value,'$.decision.question'),'memory_exchange_id',json_extract(value,'$.decision.memory_exchange_id')) END,
 'selected_option_id',CASE WHEN priority=2 THEN json_extract(value,'$.selected_option_id') END,
 'expires_at',json_extract(value,'$.expires_at')) j
 FROM offers WHERE (?='' OR (priority,id COLLATE BINARY)>(?,? COLLATE BINARY))
 ORDER BY priority,id COLLATE BINARY LIMIT ?
), budget AS (SELECT *,sum(length(CAST(j AS BLOB))) OVER(ORDER BY priority,id COLLATE BINARY) spent FROM candidates)
SELECT priority,CASE WHEN spent<=? THEN id ELSE '' END,CASE WHEN spent<=? THEN j END FROM budget ORDER BY priority,id COLLATE BINARY`
	rows, err := tx.QueryContext(ctx, statement, out.Head.Key.MachineID, out.Head.Key.BotID, p.ID, p.Kind, p.ID, q.Request.PageSize+1, q.ByteLimit, q.ByteLimit)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if len(out.Items) == q.Request.PageSize {
			out.HasMore = true
			break
		}
		var id string
		var priority int
		var raw sql.NullString
		if err = rows.Scan(&priority, &id, &raw); err != nil {
			return err
		}
		if !raw.Valid {
			out.Items = append(out.Items, viewRowFailure())
			out.HasMore = true
			break
		}
		var offer pa.PermissionOffer
		if json.Unmarshal([]byte(raw.String), &offer) != nil || offer.ID != id {
			return failure(pa.Invalid, "read_view_permission")
		}
		if err = viewPermissionFence(&offer, out.Head); err != nil {
			return err
		}
		detail, f := s.cursor(q, out.Head, viewPosition{Collection: "permission", ID: id})
		item := app.ReadViewItem{ID: id, Kind: app.ReadItemPermissionSummary, ObservedEventSeq: out.Head.EventFrontier, Failure: f}
		item.PermissionSummary = &app.ReadPermissionSummary{ID: id, Fence: offer.Fence, TurnID: offer.TurnID, State: offer.State, ExpiresAt: offer.ExpiresAt, DetailCursor: detail}
		if offer.Action != nil {
			item.PermissionSummary.Title, item.PermissionSummary.ToolKind = offer.Action.Title, offer.Action.ToolKind
		}
		if offer.Kind == pa.DecisionOfferKind && offer.Decision != nil {
			item.PermissionSummary.Title, item.PermissionSummary.ToolKind = offer.Decision.Question, pa.DecisionOfferKind
			item.PermissionSummary.DecisionKind = pa.RosterDecisionQuestion
			if priority != 1 {
				item.PermissionSummary.DecisionKind = pa.RosterDecisionMemorySave
				item.PermissionSummary.MemoryExchangeID = offer.Decision.MemoryExchangeID
				item.PermissionSummary.SelectedOptionID = offer.SelectedOptionID
			}
		}
		item.AfterCursor, item.CursorFailure = s.cursor(q, out.Head, viewPosition{Collection: "permissions", Kind: priority, ID: id})
		out.Items = append(out.Items, item)
	}
	return rows.Err()
}

// A decision is answered by sending a new message to this conversation.
// Give that interaction the current fence while retaining the stored offer's
// original fence for its history. Native approvals still belong to their turn.
func viewPermissionFence(offer *pa.PermissionOffer, head app.ReadViewHead) error {
	decision := offer.Kind == pa.DecisionOfferKind
	if err := viewCheckFence(offer.Fence, head, !decision); err != nil {
		return err
	}
	if decision {
		offer.Fence = head.CurrentFence
	}
	return nil
}

func traceStepID(t pa.Trace) string {
	if t.ID != "" {
		return t.ID
	}
	return t.Category
}

func viewTraceID(turn, category string) string {
	// JSON pair encoding is unambiguous, including delimiters and UTF-8.
	raw, _ := json.Marshal([2]string{turn, category})
	return string(raw)
}

func (s *ViewStore) traces(ctx context.Context, tx *sql.Tx, q app.ReadViewQuery, out *app.ReadView, p viewPosition) error {
	// A step stays at its first event while its detail cursor follows the latest snapshot.
	statement := viewTraceCTE() + `, candidates AS MATERIALIZED (
 SELECT anchor,seq,json_object('turn_id',json_extract(j,'$.turn_id'),'category',json_extract(j,'$.category'),
 'complete',json_extract(j,'$.complete'),'id',coalesce(json_extract(j,'$.id'),'')) j FROM latest WHERE ?=0 OR anchor<? ORDER BY anchor DESC LIMIT ?
), budget AS (SELECT *,sum(length(CAST(j AS BLOB))) OVER(ORDER BY anchor DESC) spent FROM candidates)
SELECT anchor,seq,CASE WHEN spent<=? THEN j END FROM budget ORDER BY anchor DESC`
	rows, err := tx.QueryContext(ctx, statement, out.Head.Key.MachineID, out.Head.Key.BotID, out.Head.Key.MachineID, out.Head.Key.BotID,
		p.Anchor, p.Anchor, q.Request.PageSize+1, q.ByteLimit)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if len(out.Items) == q.Request.PageSize {
			out.HasMore = true
			break
		}
		var anchor, seq int64
		var raw sql.NullString
		if err = rows.Scan(&anchor, &seq, &raw); err != nil {
			return err
		}
		if !raw.Valid {
			out.Items = append(out.Items, viewRowFailure())
			out.HasMore = true
			break
		}
		// SQLite JSON extraction represents booleans as integers unless wrapped.
		var trace struct {
			ID       string `json:"id"`
			TurnID   string `json:"turn_id"`
			Category string `json:"category"`
			Complete int    `json:"complete"`
		}
		if json.Unmarshal([]byte(raw.String), &trace) != nil || trace.TurnID == "" || trace.Category == "" {
			return failure(pa.Invalid, "read_view_trace")
		}
		step := trace.ID
		if step == "" {
			step = trace.Category
		}
		id := viewTraceID(trace.TurnID, step)
		detail, f := s.cursor(q, out.Head, viewPosition{Collection: "trace", Anchor: seq, ID: id})
		item := app.ReadViewItem{ID: id, Kind: app.ReadItemTraceSummary, ObservedEventSeq: out.Head.EventFrontier, Failure: f}
		item.TraceSummary = &app.ReadTraceSummary{ID: id, TurnID: trace.TurnID, Category: trace.Category, Complete: trace.Complete != 0, DetailCursor: detail}
		item.AfterCursor, item.CursorFailure = s.cursor(q, out.Head, viewPosition{Collection: "traces", Anchor: anchor, ID: id})
		out.Items = append(out.Items, item)
	}
	return rows.Err()
}

func (s *ViewStore) detail(ctx context.Context, tx *sql.Tx, q app.ReadViewQuery, out *app.ReadView, p viewPosition) error {
	var statement string
	args := []any{out.Head.Key.MachineID, out.Head.Key.BotID}
	if p.Collection == "permission" {
		statement = viewAggregateSQL + `SELECT CASE WHEN length(CAST(p.value AS BLOB))<=? THEN p.value END
 FROM a,fact_permissions p WHERE p.type='object' AND p.key=? AND ` + viewPermissionFenceSQL("p.value")
		args = append(args, q.ByteLimit, p.ID)
	} else {
		// Seek the signed event sequence. Only accumulated text needs earlier
		// chunks of that step, stopping at the revision the cursor pins.
		statement = strings.TrimSuffix(viewAggregateSQL, " ") + `, target AS MATERIALIZED (
 SELECT e.seq,json_extract(CAST(e.data AS TEXT),'$.trace') j
 ` + viewTraceSourceSQL() + ` AND e.seq=?
), detail AS MATERIALIZED (
 SELECT CASE WHEN coalesce(json_extract(t.j,'$.id'),'')!=''
 AND json_extract(t.j,'$.category') IN ('agent_message_chunk','agent_thought_chunk')
 THEN json_set(t.j,'$.text',(SELECT group_concat(json_extract(CAST(e.data AS TEXT),'$.trace.text'),'' ORDER BY e.seq)
 ` + viewTraceSourceSQL() + `
 AND json_extract(CAST(e.data AS TEXT),'$.trace.turn_id')=json_extract(t.j,'$.turn_id')
 AND json_extract(CAST(e.data AS TEXT),'$.trace.id')=json_extract(t.j,'$.id') AND e.seq<=t.seq))
 ELSE t.j END j FROM target t
) SELECT CASE WHEN length(CAST(j AS BLOB))<=? THEN j END FROM detail`
		args = append(args,
			out.Head.Key.MachineID, out.Head.Key.BotID, p.Anchor,
			out.Head.Key.MachineID, out.Head.Key.BotID, q.ByteLimit)
	}
	var raw sql.NullString
	err := tx.QueryRowContext(ctx, statement, args...).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return failure(pa.CursorExpired, "read_view_detail")
	}
	if err != nil {
		return err
	}
	if !raw.Valid {
		return failure(pa.CapacityFull, "read_view_item_bytes")
	}
	item := app.ReadViewItem{ID: p.ID, ObservedEventSeq: out.Head.EventFrontier}
	if p.Collection == "permission" {
		item.Kind, item.Permission = app.ReadItemPermission, &pa.PermissionOffer{}
		if json.Unmarshal([]byte(raw.String), item.Permission) != nil || item.Permission.ID != p.ID {
			return failure(pa.Invalid, "read_view_permission")
		}
		if item.Permission.Options == nil {
			item.Permission.Options = []pa.PermissionOption{}
		}
		if err = viewPermissionFence(item.Permission, out.Head); err != nil {
			return err
		}
	} else {
		item.Kind, item.Trace = app.ReadItemTrace, &pa.Trace{}
		if json.Unmarshal([]byte(raw.String), item.Trace) != nil || viewTraceID(item.Trace.TurnID, traceStepID(*item.Trace)) != p.ID {
			return failure(pa.Invalid, "read_view_trace")
		}
	}
	out.Items = append(out.Items, item)
	return nil
}
