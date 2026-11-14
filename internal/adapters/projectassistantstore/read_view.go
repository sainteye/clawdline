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
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
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
	tx, err := s.borrowedDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	rawBytes := 0
	h, bot, err := viewHead(ctx, tx, q, &rawBytes)
	if err != nil {
		return out, err
	}
	out.Head = h
	// Events need only source identity/shape, not a materialized aggregate or a
	// complete pass over message/work relationships.
	if q.Request.Mode != pa.ReadViewEvents {
		if err = viewValidateFacts(ctx, tx, q); err != nil {
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
	case pa.ReadViewTraces:
		err = s.traces(ctx, tx, remaining, &out, position)
	case pa.ReadViewDetail:
		err = s.detail(ctx, tx, remaining, &out, position)
	}
	if err != nil {
		return app.ReadView{}, err
	}
	if err = tx.Commit(); err != nil {
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
	return `json_object('id',json_extract(` + value + `,'$.id'),'fence',json_extract(` + value + `,'$.fence'),
 'message_id',json_extract(` + value + `,'$.message_id'),'state',json_extract(` + value + `,'$.state'),
 'failure',json_object('code',json_extract(` + value + `,'$.failure.code')))`
}

func viewTurn(raw string, h app.ReadViewHead) (app.ReadTurnSummary, error) {
	var turn pa.Turn
	if json.Unmarshal([]byte(raw), &turn) != nil || turn.ID == "" || turn.MessageID == "" {
		return app.ReadTurnSummary{}, failure(pa.Invalid, "read_view_turn")
	}
	if err := viewCheckFence(turn.Fence, h, false); err != nil {
		return app.ReadTurnSummary{}, err
	}
	summary := app.ReadTurnSummary{ID: turn.ID, Fence: turn.Fence, MessageID: turn.MessageID, State: turn.State}
	if turn.Failure != nil {
		summary.FailureCode = turn.Failure.Code
	}
	return summary, nil
}

func viewSnapshot(ctx context.Context, tx *sql.Tx, q app.ReadViewQuery, h app.ReadViewHead, bot pa.Bot, rawBytes *int) (*app.ReadSnapshot, error) {
	out := &app.ReadSnapshot{Bot: bot, PendingTurns: []app.ReadTurnSummary{}, QueuedMessages: []pa.Message{}}
	statement := strings.TrimSuffix(viewAggregateSQL, " ") + `, pending AS MATERIALIZED (
 SELECT ` + viewTurnProjection("t.value") + ` j,json_extract(t.value,'$.message_id') message_id,t.key id
 FROM a,json_each(a.j,'$.Turns') t WHERE t.type='object'
 AND json_extract(t.value,'$.state') IN ('accepted','queued','running','waiting_permission')
), matched AS MATERIALIZED (
 SELECT p.j,p.id,m.value m,json_extract(m.value,'$.seq') anchor FROM pending p
 LEFT JOIN (a JOIN json_each(a.j,'$.Messages') m) ON m.type='object' AND json_extract(m.value,'$.id')=p.message_id
), budget AS (
 SELECT *,sum(length(CAST(j AS BLOB))+coalesce(length(CAST(m AS BLOB)),0)) OVER(ORDER BY anchor,id COLLATE BINARY) spent FROM matched
)
SELECT CASE WHEN spent<=? THEN j END,CASE WHEN spent<=? THEN m END
 FROM budget ORDER BY anchor,id COLLATE BINARY LIMIT ?`
	rows, err := tx.QueryContext(ctx, statement, h.Key.MachineID, h.Key.BotID, q.ByteLimit, q.ByteLimit, pa.MaxPendingTurns+1)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		if len(out.PendingTurns) == pa.MaxPendingTurns {
			rows.Close()
			return nil, failure(pa.CapacityFull, "pending_turns")
		}
		var raw, message sql.NullString
		if err = rows.Scan(&raw, &message); err != nil {
			rows.Close()
			return nil, err
		}
		if !raw.Valid {
			rows.Close()
			return nil, failure(pa.CapacityFull, "read_view_snapshot_bytes")
		}
		if !message.Valid {
			rows.Close()
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
			return nil, failure(pa.Invalid, "read_view_turn_message")
		}
		out.PendingTurns = append(out.PendingTurns, summary)
		out.QueuedMessages = append(out.QueuedMessages, m)
		*rawBytes += len(raw.String) + len(message.String)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	statement = strings.TrimSuffix(viewAggregateSQL, " ") + `, terminal AS MATERIALIZED (
 SELECT ` + viewTurnProjection("t.value") + ` j,
 (SELECT json_extract(m.value,'$.seq') FROM json_each(a.j,'$.Messages') m WHERE m.type='object' AND json_extract(m.value,'$.id')=json_extract(t.value,'$.message_id')) anchor,t.key id
 FROM a,json_each(a.j,'$.Turns') t WHERE t.type='object'
 AND json_extract(t.value,'$.state') IN ('succeeded','failed','cancelled','unknown')
 AND ` + viewFenceSQL("t.value", true) + ` ORDER BY anchor DESC,id COLLATE BINARY DESC LIMIT 1
)
SELECT CASE WHEN length(CAST(j AS BLOB))<=? THEN j END FROM terminal`
	var last sql.NullString
	err = tx.QueryRowContext(ctx, statement, h.Key.MachineID, h.Key.BotID, q.ByteLimit-*rawBytes).Scan(&last)
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
	rows, err := tx.QueryContext(ctx, viewTimelineSQL(), out.Head.Key.MachineID, out.Head.Key.BotID,
		p.ID != "", p.Anchor, p.Kind, p.ID, q.Request.PageSize+1, q.ByteLimit, q.ByteLimit, q.ByteLimit, q.ByteLimit)
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
		if kind == 0 {
			item.Kind, item.Message = app.ReadItemMessage, &pa.Message{}
			if json.Unmarshal([]byte(raw.String), item.Message) != nil || item.Message.ID != id || item.Message.Seq != anchor {
				return failure(pa.Invalid, "read_view_message")
			}
			if err = viewCheckFence(item.Message.Fence, out.Head, false); err != nil {
				return err
			}
		} else {
			item.Kind, item.Work = app.ReadItemWork, &pa.WorkLink{}
			if json.Unmarshal([]byte(raw.String), item.Work) != nil || item.Work.ID != id || item.Work.TaskID == "" {
				return failure(pa.Invalid, "read_view_work")
			}
			if err = viewCheckFence(item.Work.Fence, out.Head, false); err != nil {
				return err
			}
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
	statement := `WITH candidates AS MATERIALIZED (
 SELECT seq,event_id,CAST(data AS TEXT) j,length(data)+length(CAST(event_id AS BLOB)) n FROM project_assistant_events
 WHERE machine=? AND bot=? AND seq>? ORDER BY seq LIMIT ?
), budget AS (SELECT *,sum(n) OVER(ORDER BY seq) spent FROM candidates)
SELECT seq,CASE WHEN n<=? AND spent<=? THEN event_id ELSE '' END,
 CASE WHEN n<=? AND spent<=? THEN j END FROM budget ORDER BY seq`
	rows, err := tx.QueryContext(ctx, statement, out.Head.Key.MachineID, out.Head.Key.BotID, q.Request.AfterSeq, q.Request.PageSize+1,
		q.ByteLimit, q.ByteLimit, q.ByteLimit, q.ByteLimit)
	if err != nil {
		return err
	}
	defer rows.Close()
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
		if err = viewEventFacts(ctx, tx, e, out.Head); err != nil {
			return err
		}
		out.Items = append(out.Items, app.ReadViewItem{ID: id, Kind: app.ReadItemEvent, ObservedEventSeq: seq, Event: &e})
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

func viewEventFacts(ctx context.Context, tx *sql.Tx, e pa.Event, h app.ReadViewHead) error {
	// Scalar existence checks borrow the same transaction. In particular,
	// checking Trace against its stored Turn does not deserialize that Turn's
	// potentially large captured instructions or the enclosing aggregate.
	if e.Fence.BindingID != "" {
		statement := viewAggregateSQL + `SELECT EXISTS(SELECT 1 FROM a,json_each(a.j,'$.Bindings') b
 WHERE b.type='object' AND b.key=? AND b.key=json_extract(b.value,'$.fence.binding_id')
 AND json_extract(b.value,'$.fence.generation')=? AND ` + viewFenceSQL("b.value", false) + `)`
		var valid bool
		if err := tx.QueryRowContext(ctx, statement, h.Key.MachineID, h.Key.BotID, e.Fence.BindingID, e.Fence.Generation).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return failure(pa.ScopeDenied, "read_view_event_binding")
		}
	}
	if e.Trace != nil {
		statement := viewAggregateSQL + `SELECT EXISTS(SELECT 1 FROM a,json_each(a.j,'$.Turns') t
 WHERE t.type='object' AND t.key=? AND t.key=json_extract(t.value,'$.id') AND json_extract(t.value,'$.fence.generation')=?
 AND json_extract(t.value,'$.fence.binding_id')=? AND ` + viewFenceSQL("t.value", false) + `)`
		var valid bool
		if err := tx.QueryRowContext(ctx, statement, h.Key.MachineID, h.Key.BotID, e.Trace.TurnID, e.Fence.Generation, e.Fence.BindingID).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return failure(pa.ScopeDenied, "read_view_event_trace")
		}
	}
	return nil
}

func (s *ViewStore) permissions(ctx context.Context, tx *sql.Tx, q app.ReadViewQuery, out *app.ReadView, p viewPosition) error {
	statement := strings.TrimSuffix(viewAggregateSQL, " ") + `, candidates AS MATERIALIZED (
 SELECT p.key id,json_object('id',json_extract(p.value,'$.id'),'fence',json_extract(p.value,'$.fence'),
 'turn_id',json_extract(p.value,'$.turn_id'),'state',json_extract(p.value,'$.state'),
 'expires_at',json_extract(p.value,'$.expires_at')) j
 FROM a,json_each(a.j,'$.Permissions') p WHERE p.type='object' AND json_extract(p.value,'$.state')='pending'
 AND ` + viewFenceSQL("p.value", true) + ` AND (?='' OR p.key COLLATE BINARY>? COLLATE BINARY)
 ORDER BY p.key COLLATE BINARY LIMIT ?
), budget AS (SELECT *,sum(length(CAST(j AS BLOB))) OVER(ORDER BY id COLLATE BINARY) spent FROM candidates)
SELECT CASE WHEN spent<=? THEN id ELSE '' END,CASE WHEN spent<=? THEN j END FROM budget ORDER BY id COLLATE BINARY`
	rows, err := tx.QueryContext(ctx, statement, out.Head.Key.MachineID, out.Head.Key.BotID, p.ID, p.ID, q.Request.PageSize+1, q.ByteLimit, q.ByteLimit)
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
		var raw sql.NullString
		if err = rows.Scan(&id, &raw); err != nil {
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
		if err = viewCheckFence(offer.Fence, out.Head, true); err != nil {
			return err
		}
		detail, f := s.cursor(q, out.Head, viewPosition{Collection: "permission", ID: id})
		item := app.ReadViewItem{ID: id, Kind: app.ReadItemPermissionSummary, ObservedEventSeq: out.Head.EventFrontier, Failure: f}
		item.PermissionSummary = &app.ReadPermissionSummary{ID: id, Fence: offer.Fence, TurnID: offer.TurnID, State: offer.State, ExpiresAt: offer.ExpiresAt, DetailCursor: detail}
		if offer.Action != nil {
			item.PermissionSummary.Title, item.PermissionSummary.ToolKind = offer.Action.Title, offer.Action.ToolKind
		}
		item.AfterCursor, item.CursorFailure = s.cursor(q, out.Head, viewPosition{Collection: "permissions", ID: id})
		out.Items = append(out.Items, item)
	}
	return rows.Err()
}

func viewTraceID(turn, category string) string {
	// JSON pair encoding is unambiguous, including delimiters and UTF-8.
	raw, _ := json.Marshal([2]string{turn, category})
	return string(raw)
}

func (s *ViewStore) traces(ctx context.Context, tx *sql.Tx, q app.ReadViewQuery, out *app.ReadView, p viewPosition) error {
	// Event sequence is a stable position for this live latest-category view.
	statement := viewTraceCTE() + `, candidates AS MATERIALIZED (
 SELECT seq,json_object('turn_id',json_extract(j,'$.turn_id'),'category',json_extract(j,'$.category'),
 'complete',json_extract(j,'$.complete')) j FROM latest WHERE ?=0 OR seq<? ORDER BY seq DESC LIMIT ?
), budget AS (SELECT *,sum(length(CAST(j AS BLOB))) OVER(ORDER BY seq DESC) spent FROM candidates)
SELECT seq,CASE WHEN spent<=? THEN j END FROM budget ORDER BY seq DESC`
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
		var seq int64
		var raw sql.NullString
		if err = rows.Scan(&seq, &raw); err != nil {
			return err
		}
		if !raw.Valid {
			out.Items = append(out.Items, viewRowFailure())
			out.HasMore = true
			break
		}
		// SQLite JSON extraction represents booleans as integers unless wrapped.
		var trace struct {
			TurnID   string `json:"turn_id"`
			Category string `json:"category"`
			Complete int    `json:"complete"`
		}
		if json.Unmarshal([]byte(raw.String), &trace) != nil || trace.TurnID == "" || trace.Category == "" {
			return failure(pa.Invalid, "read_view_trace")
		}
		id := viewTraceID(trace.TurnID, trace.Category)
		detail, f := s.cursor(q, out.Head, viewPosition{Collection: "trace", Anchor: seq, ID: id})
		item := app.ReadViewItem{ID: id, Kind: app.ReadItemTraceSummary, ObservedEventSeq: out.Head.EventFrontier, Failure: f}
		item.TraceSummary = &app.ReadTraceSummary{ID: id, TurnID: trace.TurnID, Category: trace.Category, Complete: trace.Complete != 0, DetailCursor: detail}
		item.AfterCursor, item.CursorFailure = s.cursor(q, out.Head, viewPosition{Collection: "traces", Anchor: seq, ID: id})
		out.Items = append(out.Items, item)
	}
	return rows.Err()
}

func (s *ViewStore) detail(ctx context.Context, tx *sql.Tx, q app.ReadViewQuery, out *app.ReadView, p viewPosition) error {
	var statement string
	args := []any{out.Head.Key.MachineID, out.Head.Key.BotID}
	if p.Collection == "permission" {
		statement = viewAggregateSQL + `SELECT CASE WHEN length(CAST(p.value AS BLOB))<=? THEN p.value END
 FROM a,json_each(a.j,'$.Permissions') p WHERE p.type='object' AND p.key=? AND ` + viewFenceSQL("p.value", true)
		args = append(args, q.ByteLimit, p.ID)
	} else {
		statement = viewTraceCTE() + `SELECT CASE WHEN length(CAST(j AS BLOB))<=? THEN j END FROM latest WHERE seq=?`
		args = append(args, out.Head.Key.MachineID, out.Head.Key.BotID, q.ByteLimit, p.Anchor)
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
		if err = viewCheckFence(item.Permission.Fence, out.Head, true); err != nil {
			return err
		}
	} else {
		item.Kind, item.Trace = app.ReadItemTrace, &pa.Trace{}
		if json.Unmarshal([]byte(raw.String), item.Trace) != nil || viewTraceID(item.Trace.TurnID, item.Trace.Category) != p.ID {
			return failure(pa.Invalid, "read_view_trace")
		}
	}
	out.Items = append(out.Items, item)
	return nil
}
