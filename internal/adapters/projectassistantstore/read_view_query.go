package projectassistantstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/botreadtrace"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Materialize header scalars once per statement. Extracting them from the
// complete JSON in each member's fence predicate repeatedly compares the large
// aggregate in SQLite's JSON parse cache. Keep j as TEXT so returned objects
// and raw-byte budgets preserve their existing JSON semantics.
// The inner lookup only names that TEXT expression. Inline it so SQLite does
// not write a second temporary copy of the full Bot aggregate.
// The Bot's collections come from member rows (facts.go); ?1 and ?2 are the
// machine and Bot, so later plain parameters keep their positions.
var viewAggregateSQL = `WITH aggregate AS NOT MATERIALIZED (
 SELECT CAST(data AS TEXT) j FROM project_assistant_bots WHERE machine=?1 AND bot=?2
)` + factCTEsSQL("?1", "?2") + `, a AS MATERIALIZED (
 SELECT j,
 json_extract(j,'$.Bot.key.machine_id') machine_id,
 json_extract(j,'$.Bot.key.bot_id') bot_id,
 json_extract(j,'$.Bot.project.machine_id') project_machine_id,
 json_extract(j,'$.Bot.project.project_id') project_id,
 json_extract(j,'$.Bot.project.canonical_id') canonical_id,
 json_extract(j,'$.Bot.project.kind') project_kind,
 json_extract(j,'$.Conversation.id') conversation_id,
 json_extract(j,'$.Conversation.binding_id') binding_id,
 json_extract(j,'$.Conversation.generation') generation
 FROM aggregate
) ` + roomTurnsCTE

// All expressions passed to these builders are adapter-owned SQL, never wire
// identifiers or JSON paths from a token. Values remain bound parameters.
func viewFenceSQL(value string, current bool) string {
	fields := [][2]string{
		{"key.machine_id", "machine_id"}, {"key.bot_id", "bot_id"},
		{"project.machine_id", "project_machine_id"}, {"project.project_id", "project_id"},
		{"project.canonical_id", "canonical_id"}, {"project.kind", "project_kind"},
		{"conversation_id", "conversation_id"},
	}
	if current {
		fields = append(fields, [2]string{"binding_id", "binding_id"}, [2]string{"generation", "generation"})
	}
	conditions := []string{`json_type(` + value + `,'$.fence')='object'`}
	for _, field := range fields {
		conditions = append(conditions, fmt.Sprintf("json_extract(%s,'$.fence.%s')=a.%s", value, field[0], field[1]))
	}
	if !current {
		conditions = append(conditions, `json_type(`+value+`,'$.fence.generation')='integer'`,
			`json_extract(`+value+`,'$.fence.generation') BETWEEN 0 AND a.generation`,
			`json_type(`+value+`,'$.fence.binding_id')='text'`)
	}
	return "coalesce((" + strings.Join(conditions, " AND ") + "),0)"
}

func viewPermissionFenceSQL(value string) string {
	return "(" + viewFenceSQL(value, true) + " OR (json_extract(" + value + ",'$.kind')='decision' AND " + viewFenceSQL(value, false) + "))"
}

// Historical display is not execution authority. It still requires the exact
// persisted binding, including a turn's matching generation. Generation zero
// is the valid pre-binding admission state.
func viewBoundFenceSQL(value string) string {
	binding := `json_extract(` + value + `,'$.fence.binding_id')`
	generation := `json_extract(` + value + `,'$.fence.generation')`
	// This membership list is independent of the candidate turn. Evaluate
	// the persisted binding scope once per statement.
	return viewFenceSQL(value, false) + ` AND ((` + generation + `=0 AND ` + binding + `='') OR (` + binding + `,` + generation + `) IN (
 SELECT b.key,json_extract(b.value,'$.fence.generation') FROM a,fact_bindings b
 WHERE CASE WHEN b.type='object' THEN b.key=json_extract(b.value,'$.fence.binding_id')
 AND ` + viewFenceSQL("b.value", false) + ` ELSE 0 END))`
}

func viewOwnChatHead(ctx context.Context, tx *sql.Tx, h *app.ReadViewHead) error {
	// The newest own-chat message is found from the newest row down.
	statement := viewAggregateSQL + `SELECT coalesce((SELECT m.ord FROM a,fact_messages m
 WHERE ` + ownChatTurnSQL("json_extract(m.value,'$.turn_id')") + ` ORDER BY m.ord DESC LIMIT 1),0)`
	return tx.QueryRowContext(ctx, statement, h.Key.MachineID, h.Key.BotID).Scan(&h.Conversation.LastMessageSeq)
}

func viewHead(ctx context.Context, tx *sql.Tx, q app.ReadViewQuery, rawBytes *int) (app.ReadViewHead, pa.Bot, error) {
	var h app.ReadViewHead
	var b pa.Bot
	// This first SELECT establishes all later queries' SQLite snapshot, including
	// a machine frontier that may belong to an entirely different Bot.
	const statement = `WITH raw AS MATERIALIZED (
 SELECT version,data,json_valid(CAST(data AS TEXT)) valid FROM project_assistant_bots WHERE machine=? AND bot=?
), a AS MATERIALIZED (
 SELECT version,valid,CASE WHEN valid THEN CAST(data AS TEXT) ELSE '{}' END j FROM raw
), meta AS MATERIALIZED (
 SELECT version,valid,j,
 CASE WHEN ? THEN json_extract(j,'$.Bot') ELSE json_object(
 'key',json_extract(j,'$.Bot.key'),'project',json_extract(j,'$.Bot.project'),
 'conversation_id',json_extract(j,'$.Bot.conversation_id'),'version',json_extract(j,'$.Bot.version')) END b,
 json_extract(j,'$.Conversation') c,
 json_object('key',json_extract(j,'$.Memory.key'),'project',json_extract(j,'$.Memory.project'),
 'revision',json_extract(j,'$.Memory.revision')) m FROM a
)
SELECT version,valid AND json_type(j)='object'
 AND coalesce(json_type(j,'$.Bot'),'missing')='object'
 AND coalesce(json_type(j,'$.Conversation'),'missing')='object'
 AND coalesce(json_type(j,'$.Memory'),'missing')='object'
 AND coalesce(json_type(j,'$.Messages'),'null')='null'
 AND coalesce(json_type(j,'$.Turns'),'null')='null'
 AND coalesce(json_type(j,'$.Bindings'),'null')='null'
 AND coalesce(json_type(j,'$.Permissions'),'null')='null'
 AND coalesce(json_type(j,'$.WorkLinks'),'null')='null',
 CASE WHEN length(CAST(b AS BLOB))+length(CAST(c AS BLOB))+length(CAST(m AS BLOB))<=? THEN b END,
 CASE WHEN length(CAST(b AS BLOB))+length(CAST(c AS BLOB))+length(CAST(m AS BLOB))<=? THEN c END,
 CASE WHEN length(CAST(b AS BLOB))+length(CAST(c AS BLOB))+length(CAST(m AS BLOB))<=? THEN m END,
 (` + eventFrontierSQL + `) FROM meta`
	var version int64
	var valid bool
	var bot, conversation, memory sql.NullString
	meta := botreadtrace.Measure(ctx, "sqlite_metadata")
	err := tx.QueryRowContext(ctx, statement, q.Request.Key.MachineID, q.Request.Key.BotID,
		q.Request.Mode == pa.ReadViewSnapshot, q.ByteLimit, q.ByteLimit, q.ByteLimit, q.Request.Key.MachineID).
		Scan(&version, &valid, &bot, &conversation, &memory, &h.EventFrontier)
	meta()
	if errors.Is(err, sql.ErrNoRows) {
		return h, b, failure(pa.NotFound, "bot")
	}
	if err != nil {
		return h, b, err
	}
	if !valid {
		return h, b, failure(pa.Invalid, "read_view_shape")
	}
	if !bot.Valid || !conversation.Valid || !memory.Valid {
		return h, b, failure(pa.CapacityFull, "read_view_metadata_bytes")
	}
	*rawBytes += len(bot.String) + len(conversation.String) + len(memory.String)
	var m pa.MemoryRevision
	if json.Unmarshal([]byte(bot.String), &b) != nil || json.Unmarshal([]byte(conversation.String), &h.Conversation) != nil || json.Unmarshal([]byte(memory.String), &m) != nil {
		return h, b, failure(pa.Invalid, "read_view_metadata")
	}
	if b.Key != q.Request.Key || b.Project.MachineID != b.Key.MachineID || h.Conversation.Key != b.Key || m.Key != b.Key || m.Project != b.Project {
		return h, b, failure(pa.ScopeDenied, "read_view_identity")
	}
	if err = pa.ValidateScope(b.Project); err != nil {
		return h, b, err
	}
	if b.ConversationID == "" || h.Conversation.ID != b.ConversationID {
		return h, b, failure(pa.Invalid, "read_view_conversation")
	}
	if q.Request.ConversationID != h.Conversation.ID {
		return h, b, failure(pa.NotFound, "conversation")
	}
	c := h.Conversation
	if sample := c.ContextUsage; c.ActiveTurnID == "" && sample.TurnID != "" &&
		sample.BindingID == c.BindingID && sample.Generation == c.Generation && sample.Generation > c.SessionResetGeneration && sample.MemoryRevision == m.Revision &&
		sample.UsedTokens >= 0 && sample.WindowTokens > 0 && (q.Request.Mode == pa.ReadViewSnapshot || q.Request.Mode == pa.ReadViewEvents) {
		h.ContextUsage = &app.ReadContextUsage{TurnID: sample.TurnID, UsedTokens: sample.UsedTokens, WindowTokens: sample.WindowTokens}
	}
	if version < 1 || b.Version != version || c.Version < 1 || c.Generation < 0 || (c.Generation == 0) != (c.BindingID == "") || c.LastMessageSeq < 0 || m.Revision < 1 || c.MemoryRevision < 1 || c.MemoryRevision > m.Revision {
		return h, b, failure(pa.Invalid, "read_view_metadata")
	}
	h.Key, h.Project, h.BotVersion, h.MemoryRevision = b.Key, b.Project, b.Version, m.Revision
	h.CurrentFence = pa.Fence{Key: b.Key, Project: b.Project, ConversationID: c.ID, BindingID: c.BindingID, Generation: c.Generation}
	if c.BindingID != "" {
		statement := viewAggregateSQL + `SELECT EXISTS(SELECT 1 FROM a,fact_bindings b
 WHERE CASE WHEN b.type!='object' THEN 0 ELSE b.key=a.binding_id AND ` + viewFenceSQL("b.value", true) + ` END)`
		var current bool
		binding := botreadtrace.Measure(ctx, "sqlite_binding")
		err = tx.QueryRowContext(ctx, statement, b.Key.MachineID, b.Key.BotID).Scan(&current)
		binding()
		if err != nil {
			return h, b, err
		}
		if !current {
			return h, b, failure(pa.Invalid, "read_view_current_binding")
		}
	}
	if q.Request.Mode == pa.ReadViewSnapshot {
		if err = pa.ValidateCreate(pa.CreateBot{Target: pa.ProjectSelection{MachineID: b.Key.MachineID, ProjectID: b.Project.ProjectID}, Name: b.Name, Appearance: b.Appearance, Agent: b.Agent, PermissionPolicy: b.PermissionPolicy, Mode: b.Mode, Instructions: b.Instructions.Text}, pa.ProjectResolution{Scope: b.Project, Directory: "host-resolved", Available: true, Current: true}); err != nil {
			return h, b, err
		}
		if b.Instructions.Revision < 1 {
			return h, b, failure(pa.Invalid, "instructions_revision")
		}
	}
	return h, b, nil
}

// page_floor is the position of the page-size-plus-one-th own-chat message
// below the cursor. Those messages are candidates themselves, so every row of
// the page sits at or above it, and no older message needs to be read.
// Parameters: ?4 a cursor is set, ?5 its anchor, ?8 the page size plus one.
const viewTimelineCTE = `, page_floor AS MATERIALIZED (
 SELECT coalesce((SELECT m.ord FROM a,fact_messages m WHERE (?4=0 OR m.ord<?5)
 AND ` + "NOT EXISTS(SELECT 1 FROM room_turns WHERE room_turns.id=json_extract(m.value,'$.turn_id'))" + `
 AND NOT EXISTS(SELECT 1 FROM withdrawn_turns WHERE withdrawn_turns.id=json_extract(m.value,'$.turn_id'))
 ORDER BY m.ord DESC LIMIT 1 OFFSET ?8-1),0) floor
), messages AS MATERIALIZED (
 SELECT CASE WHEN json_extract(m.value,'$.output.artifact_id')!='' THEN
 json_set(m.value,'$.output.name',coalesce((SELECT json_extract(t.value,'$.title') FROM json_each(a.j,'$.ArtifactTitles') t WHERE t.key=json_extract(m.value,'$.output.artifact_id')),json_extract(m.value,'$.output.name')),
 '$.output.title_version',coalesce((SELECT json_extract(t.value,'$.version') FROM json_each(a.j,'$.ArtifactTitles') t WHERE t.key=json_extract(m.value,'$.output.artifact_id')),1)) ELSE m.value END j,
 m.member id,m.ord anchor
 FROM a,fact_messages m WHERE m.ord>=(SELECT floor FROM page_floor)
), turns AS NOT MATERIALIZED (
 SELECT key id,value j,json_extract(value,'$.message_id') message_id FROM fact_turns
), timeline AS (
 SELECT anchor,0 kind,id,j FROM messages UNION ALL
 SELECT m.anchor,1,w.key,w.value FROM a,fact_work_links w
 JOIN turns t ON t.id=json_extract(w.value,'$.turn_id') JOIN messages m ON m.id=t.message_id
) `

// Hydrate exchange notices from the matched durable record inside the timeline
// budget. The selected message page bounds the exchange lookups.
func viewTimelineSQL() string {
	return strings.TrimSuffix(viewAggregateSQL, " ") + withdrawnTurnsCTE + viewTimelineCTE + `, candidates AS NOT MATERIALIZED (
 SELECT anchor,kind,id,j FROM timeline
 UNION ALL
 SELECT json_extract(p.value,'$.anchor_message_seq'),2,p.key,p.value FROM a,fact_permissions p
 WHERE ?3 AND json_extract(p.value,'$.anchor_message_seq')>0
 AND json_extract(p.value,'$.action.tool_kind') IN ('team_create','team_update')
), bounded AS MATERIALIZED (
 SELECT anchor,kind,id,candidates.j FROM a,candidates
 WHERE ` + ownChatMessageSQL("json_extract(candidates.j,'$.turn_id')") + ` AND (?4=0 OR (anchor,kind,id COLLATE BINARY)<(?5,?6,?7 COLLATE BINARY))
 ORDER BY anchor DESC,kind DESC,id COLLATE BINARY DESC LIMIT ?8
), exchange_state AS MATERIALIZED (
 SELECT c.anchor,c.kind,c.id,CASE
 WHEN json_extract(c.j,'$.kind')='user' AND json_extract(t.j,'$.joined_turn_id') IS NOT NULL
 THEN json_set(c.j,'$.joined_turn_id',json_extract(t.j,'$.joined_turn_id'))
 WHEN json_valid(CAST(e.data AS TEXT))=1 THEN CASE
 WHEN e.revision>=json_extract(c.j,'$.exchange.revision')
 AND json_extract(CAST(e.data AS TEXT),'$.source')=json_extract(c.j,'$.exchange.source')
 AND json_extract(CAST(e.data AS TEXT),'$.target')=json_extract(c.j,'$.exchange.target')
 AND json_extract(CAST(e.data AS TEXT),'$.project')=json_extract(c.j,'$.exchange.project')
 AND json_extract(CAST(e.data AS TEXT),'$.target_project') IS json_extract(c.j,'$.exchange.target_project')
 AND json_extract(CAST(e.data AS TEXT),'$.source_turn_id')=json_extract(c.j,'$.exchange.source_turn_id')
 AND json_extract(CAST(e.data AS TEXT),'$.request_turn_id')=json_extract(c.j,'$.exchange.request_turn_id')
 THEN json_set(c.j,'$.exchange',json(CAST(e.data AS TEXT))) ELSE c.j END
 ELSE c.j END j
 FROM bounded c LEFT JOIN project_assistant_exchanges e ON c.kind=0 AND e.machine=?9
 AND e.id=json_extract(c.j,'$.exchange.id') AND (e.source=?10 OR e.target=?11)
 LEFT JOIN turns t ON c.kind=0 AND t.id=json_extract(c.j,'$.turn_id') AND t.message_id=c.id
), sized AS (
 SELECT *,length(CAST(j AS BLOB)) n FROM exchange_state
), budget AS (
 SELECT *,sum(n) OVER(ORDER BY anchor DESC,kind DESC,id COLLATE BINARY DESC) spent FROM sized
)
SELECT anchor,kind,CASE WHEN n<=?12 AND spent<=?13 THEN id ELSE '' END,
 CASE WHEN n<=?14 AND spent<=?15 THEN j END FROM budget
ORDER BY anchor DESC,kind DESC,id COLLATE BINARY DESC`
}

const viewTraceStepSQL = `coalesce(nullif(json_extract(CAST(e.data AS TEXT),'$.trace.id'),''),json_extract(CAST(e.data AS TEXT),'$.trace.category'))`

func viewTraceSourceSQL() string {
	return ` FROM a,project_assistant_events e
 WHERE e.machine=? AND e.bot=? AND json_extract(CAST(e.data AS TEXT),'$.kind')='trace_updated'
 AND ` + ownChatEventSQL("CAST(e.data AS TEXT)") + `
 AND ` + viewFenceSQL("CAST(e.data AS TEXT)", false)
}

func viewTraceCTE() string {
	return strings.TrimSuffix(viewAggregateSQL, " ") + `, traces AS MATERIALIZED (
 SELECT e.seq,json_extract(CAST(e.data AS TEXT),'$.trace') j,
 json_extract(CAST(e.data AS TEXT),'$.fence.binding_id') binding_id,json_extract(CAST(e.data AS TEXT),'$.fence.generation') generation,
 row_number() OVER(PARTITION BY json_extract(CAST(e.data AS TEXT),'$.trace.turn_id'),` + viewTraceStepSQL + ` ORDER BY e.seq DESC) rn,
 min(e.seq) OVER(PARTITION BY json_extract(CAST(e.data AS TEXT),'$.trace.turn_id'),` + viewTraceStepSQL + `) first_seq
 ` + viewTraceSourceSQL() + `
), latest AS MATERIALIZED (
 SELECT seq,CASE WHEN coalesce(json_extract(j,'$.id'),'')!='' THEN first_seq ELSE seq END anchor,j FROM traces WHERE rn=1
) `
}

func viewCounts(ctx context.Context, tx *sql.Tx, q app.ReadViewQuery, h *app.ReadViewHead) error {
	statement := viewAggregateSQL + `SELECT
 (SELECT count(*) FROM a,fact_turns WHERE type='object' AND json_extract(value,'$.state')='unknown'),
 (SELECT count(*) FROM a,fact_turns t WHERE t.type='object'
 AND json_extract(t.value,'$.state') IN ('accepted','queued') AND ` + ownChatTurnSQL("t.key") + `),
 (SELECT count(*) FROM a,fact_permissions p WHERE p.type='object' AND json_extract(p.value,'$.state')='pending' AND ` + viewPermissionFenceSQL("p.value") + `)`
	counts := botreadtrace.Measure(ctx, "sqlite_turn_counts")
	err := tx.QueryRowContext(ctx, statement, h.Key.MachineID, h.Key.BotID).Scan(&h.UnknownTurnCount, &h.QueuedTurnCount, &h.PendingPermissionCount)
	counts()
	if err != nil {
		return err
	}
	if q.Request.Mode == pa.ReadViewProgress || q.Request.DeferTraceCount && q.Request.Mode != pa.ReadViewTraces {
		return nil
	}
	var bad int64
	// The head counts distinct steps. Only the traces page needs the newest
	// content or chronological anchors. Validate every chunk so
	// a later update cannot hide a malformed chunk or a mismatched binding.
	statement = strings.TrimSuffix(viewAggregateSQL, " ") + `, traces AS MATERIALIZED (
 SELECT json_extract(CAST(e.data AS TEXT),'$.trace.turn_id') turn_id,` + viewTraceStepSQL + ` step,
 json_extract(CAST(e.data AS TEXT),'$.fence.binding_id') binding_id,
 json_extract(CAST(e.data AS TEXT),'$.fence.generation') generation,
 json_type(CAST(e.data AS TEXT),'$.trace')='object'
 AND json_type(CAST(e.data AS TEXT),'$.trace.turn_id')='text' AND json_extract(CAST(e.data AS TEXT),'$.trace.turn_id')!=''
 AND json_type(CAST(e.data AS TEXT),'$.trace.category')='text' AND json_extract(CAST(e.data AS TEXT),'$.trace.category')!=''
 AND json_type(CAST(e.data AS TEXT),'$.trace.text')='text'
 AND json_type(CAST(e.data AS TEXT),'$.trace.complete') IN ('true','false') valid
 ` + viewTraceSourceSQL() + `
), trace_refs AS MATERIALIZED (
 SELECT DISTINCT turn_id,binding_id,generation FROM traces
), trace_turns AS MATERIALIZED (
 SELECT t.key id,json_extract(t.value,'$.fence.binding_id') binding_id,json_extract(t.value,'$.fence.generation') generation
 FROM a,fact_turns t
 WHERE CASE WHEN t.type!='object' THEN 0
 WHEN (t.key,json_extract(t.value,'$.fence.binding_id'),json_extract(t.value,'$.fence.generation')) IN (SELECT turn_id,binding_id,generation FROM trace_refs)
 THEN ` + viewBoundFenceSQL("t.value") + ` ELSE 0 END
), steps AS (
 SELECT 1 FROM traces GROUP BY turn_id,step
) SELECT (SELECT count(*) FROM steps),coalesce(sum(CASE WHEN valid
 AND (turn_id,binding_id,generation) IN (SELECT id,binding_id,generation FROM trace_turns)
 THEN 0 ELSE 1 END),0) FROM traces`
	traces := botreadtrace.Measure(ctx, "sqlite_trace_count")
	err = tx.QueryRowContext(ctx, statement, h.Key.MachineID, h.Key.BotID, h.Key.MachineID, h.Key.BotID).Scan(&h.TraceCategoryCount, &bad)
	traces()
	if err != nil {
		return err
	}
	if bad != 0 {
		return failure(pa.ScopeDenied, "read_view_trace_turn")
	}
	return nil
}
