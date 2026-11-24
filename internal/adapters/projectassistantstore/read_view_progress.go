package projectassistantstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func (s *ViewStore) progress(ctx context.Context, tx *sql.Tx, q app.ReadViewQuery, out *app.ReadView, p viewPosition) error {
	// Only narration belongs in this projection. A turn's delegated answer
	// belongs to its requesting Bot, even when its trace is readable on demand.
	statement := strings.TrimSuffix(viewAggregateSQL, " ") + `, turns AS MATERIALIZED (
 SELECT t.key id,json_extract(t.value,'$.fence.binding_id') binding,json_extract(t.value,'$.fence.generation') generation
 FROM a,json_each(a.j,'$.Turns') t
 WHERE t.type='object' AND coalesce(json_extract(t.value,'$.origin'),'') NOT IN ('bot_request','network_request')
 AND ` + viewBoundFenceSQL("t.value") + `
), chunks AS MATERIALIZED (
 SELECT e.seq,coalesce(json_extract(CAST(e.data AS TEXT),'$.trace.id'),'') trace_id,
 json_extract(CAST(e.data AS TEXT),'$.trace.turn_id') turn_id,` + viewTraceStepSQL + ` step
 ` + viewTraceSourceSQL() + `
 AND json_extract(CAST(e.data AS TEXT),'$.trace.category')='agent_message_chunk'
 AND (json_extract(CAST(e.data AS TEXT),'$.trace.turn_id'),
 json_extract(CAST(e.data AS TEXT),'$.fence.binding_id'),json_extract(CAST(e.data AS TEXT),'$.fence.generation'))
 IN (SELECT id,binding,generation FROM turns)
), ranked AS (
 SELECT *,row_number() OVER(PARTITION BY turn_id,step ORDER BY seq DESC) rn,
 min(seq) OVER(PARTITION BY turn_id,step) first_seq FROM chunks
), picked AS MATERIALIZED (
 SELECT *,CASE WHEN trace_id='' THEN seq ELSE first_seq END anchor
 FROM ranked WHERE rn=1 AND (?=0 OR anchor<?) ORDER BY anchor DESC LIMIT ?
), assembled AS MATERIALIZED (
 SELECT p.anchor,p.seq,json_extract(CAST(first.data AS TEXT),'$.at') at,
 CASE WHEN p.anchor=p.seq THEN json_extract(CAST(last.data AS TEXT),'$.trace') ELSE
 json_set(json_extract(CAST(last.data AS TEXT),'$.trace'),'$.text',
 (SELECT group_concat(json_extract(CAST(e.data AS TEXT),'$.trace.text'),'' ORDER BY c.seq)
 FROM chunks c JOIN project_assistant_events e ON e.seq=c.seq
 WHERE c.turn_id=p.turn_id AND c.step=p.step)) END j
 FROM picked p JOIN project_assistant_events first ON first.seq=p.anchor
 JOIN project_assistant_events last ON last.seq=p.seq
), budget AS (
 SELECT *,sum(length(CAST(j AS BLOB))) OVER(ORDER BY anchor DESC) spent FROM assembled
)
SELECT anchor,seq,at,CASE WHEN spent<=? THEN j END FROM budget ORDER BY anchor DESC`
	rows, err := tx.QueryContext(ctx, statement, out.Head.Key.MachineID, out.Head.Key.BotID,
		out.Head.Key.MachineID, out.Head.Key.BotID, p.Anchor, p.Anchor, q.Request.PageSize+1, q.ByteLimit)
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
		var raw, at sql.NullString
		if err = rows.Scan(&anchor, &seq, &at, &raw); err != nil {
			return err
		}
		if !raw.Valid {
			out.Items = append(out.Items, viewRowFailure())
			out.HasMore = true
			break
		}
		when, err := time.Parse(time.RFC3339Nano, at.String)
		if err != nil || when.IsZero() {
			return failure(pa.Invalid, "read_view_progress_time")
		}
		var trace pa.Trace
		if json.Unmarshal([]byte(raw.String), &trace) != nil || trace.Category != "agent_message_chunk" || trace.TurnID == "" {
			return failure(pa.Invalid, "read_view_progress")
		}
		id := viewTraceID(trace.TurnID, traceStepID(trace))
		item := app.ReadViewItem{Kind: app.ReadItemTrace, ID: id, OccurredAt: when, ObservedEventSeq: seq, Trace: &trace}
		item.AfterCursor, item.CursorFailure = s.cursor(q, out.Head, viewPosition{Collection: "progress", Anchor: anchor, ID: id})
		out.Items = append(out.Items, item)
	}
	return rows.Err()
}
