package projectassistantstore

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type rosterPosition struct {
	Snapshot bool   `json:"s"`
	Frontier int64  `json:"f"`
	After    string `json:"a,omitempty"`
}

func (s *ViewStore) rosterMAC(q app.RosterQuery, raw []byte) []byte {
	mac := hmac.New(sha256.New, s.signingKey[:])
	binding, _ := json.Marshal([]string{"clawdline/roster/v1", s.sourceEpoch, q.AuthorizationBinding, q.ReaderID, q.Request.MachineID, q.Request.ProjectID})
	mac.Write(binding)
	mac.Write(raw)
	return mac.Sum(nil)
}

func (s *ViewStore) rosterCursor(q app.RosterQuery, p rosterPosition) (string, error) {
	raw, _ := json.Marshal(p)
	value := base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(s.rosterMAC(q, raw))
	if len(value) > pa.MaxReadViewCursorBytes {
		return "", failure(pa.CapacityFull, "roster_cursor_bytes")
	}
	return value, nil
}

func (s *ViewStore) rosterPosition(q app.RosterQuery) (rosterPosition, error) {
	p := rosterPosition{Snapshot: true}
	if q.Request.Cursor == "" {
		return p, nil
	}
	if len(q.Request.Cursor) > pa.MaxReadViewCursorBytes {
		return p, failure(pa.CapacityFull, "roster_cursor_bytes")
	}
	parts := strings.Split(q.Request.Cursor, ".")
	if len(parts) != 2 {
		return p, failure(pa.CursorExpired, "roster_cursor")
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(parts[0])
	sig, se := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil || se != nil || !hmac.Equal(sig, s.rosterMAC(q, raw)) || json.Unmarshal(raw, &p) != nil || p.Frontier < 0 || !p.Snapshot && p.After != "" {
		return p, failure(pa.CursorExpired, "roster_cursor")
	}
	return p, nil
}

// ReadRoster retains only a keyset position and the first snapshot frontier.
// Later pages may see newer rows. Replay always begins at the FIRST frontier,
// so changes behind the keyset, including insert/hide/delete, are not skipped.
func (s *ViewStore) ReadRoster(ctx context.Context, q app.RosterQuery) (app.RosterPage, error) {
	var out app.RosterPage
	if q.Request.MachineID == "" || q.AuthorizationBinding == "" || q.ReaderID == "" || q.ByteLimit <= 0 || q.ByteLimit > cloudops.ReadPlaintextBytesLimit {
		return out, failure(pa.ScopeDenied, "roster_query")
	}
	p, err := s.rosterPosition(q)
	if err != nil {
		return out, err
	}
	initialSnapshot := p.Snapshot
	tx, err := s.borrowedDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var frontier int64
	if err = tx.QueryRowContext(ctx, `SELECT coalesce(max(seq),0) FROM project_assistant_roster_changes WHERE machine=?`, q.Request.MachineID).Scan(&frontier); err != nil {
		return out, err
	}
	if q.Request.Cursor == "" {
		p.Frontier = frontier
	}
	if p.Frontier > frontier {
		return out, failure(pa.CursorExpired, "roster_frontier")
	}
	type change struct {
		seq                                 int64
		bot, project, conversation, binding string
		generation                          int64
		removed                             bool
	}
	var changes []change
	if p.Snapshot {
		rows, err := tx.QueryContext(ctx, `SELECT b.bot,coalesce((SELECT max(seq) FROM project_assistant_roster_changes c WHERE c.machine=b.machine AND c.bot=b.bot),0) FROM project_assistant_bots b WHERE b.machine=? AND b.bot>? AND json_extract(CAST(b.data AS TEXT),'$.BackgroundOwner') IS NULL AND (?='' OR json_extract(CAST(b.data AS TEXT),'$.Bot.project.project_id')=?) ORDER BY b.bot LIMIT ?`, q.Request.MachineID, p.After, q.Request.ProjectID, q.Request.ProjectID, pa.MaxPageRows)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var c change
			if err = rows.Scan(&c.bot, &c.seq); err != nil {
				break
			}
			changes = append(changes, c)
		}
		re := rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
		if re != nil {
			return out, re
		}
		out.More = len(changes) == pa.MaxPageRows
		if len(changes) > 0 {
			p.After = changes[len(changes)-1].bot
		}
		if !out.More {
			p.Snapshot = false
			p.After = ""
		}
	} else {
		// Scan the machine journal, including foreign Project changes, so a
		// quiet selected Project can still advance its signed machine cursor.
		rows, err := tx.QueryContext(ctx, `SELECT seq,bot,project,conversation,generation,binding,removed FROM project_assistant_roster_changes WHERE machine=? AND seq>? ORDER BY seq LIMIT ?`, q.Request.MachineID, p.Frontier, pa.MaxEventPageRows)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var c change
			if err = rows.Scan(&c.seq, &c.bot, &c.project, &c.conversation, &c.generation, &c.binding, &c.removed); err != nil {
				break
			}
			changes = append(changes, c)
		}
		re := rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
		if re != nil {
			return out, re
		}
		out.More = len(changes) == pa.MaxEventPageRows
		if len(changes) > 0 {
			p.Frontier = changes[len(changes)-1].seq
		}
	}
	bytesLeft := q.ByteLimit
	for _, c := range changes {
		key := pa.BotKey{MachineID: q.Request.MachineID, BotID: c.bot}
		row, id, revision, err := rosterSummary(ctx, tx, key, q.ReaderID, bytesLeft)
		if pa.Code(err) == pa.NotFound {
			if c.project == "" {
				return out, failure(pa.CursorExpired, "roster_snapshot_deleted")
			}
			if json.Unmarshal([]byte(c.project), &id.Project) != nil {
				return out, failure(pa.Invalid, "roster_project")
			}
			id.Key, id.ConversationID, id.Generation, id.BindingID, id.Removed = key, c.conversation, c.generation, c.binding, true
			revision = c.seq
		} else if err != nil {
			return out, err
		}
		if q.Request.ProjectID != "" && id.Project.ProjectID != q.Request.ProjectID {
			continue
		}
		removed := id.Removed || row.Bot.Hidden
		item := pa.RosterChange{Key: key, Revision: revision, Removed: removed}
		// Hidden Bots remain manageable. Only deletion loses their summary;
		// both kinds are removed from the visible list.
		if !id.Removed {
			item.Summary = &row
		}
		raw, _ := json.Marshal(item)
		bytesLeft -= len(raw)
		if bytesLeft < 0 {
			return out, failure(pa.CapacityFull, "roster_frame_bytes")
		}
		out.Frame.Changes = append(out.Frame.Changes, item)
		out.Identities = append(out.Identities, id)
	}
	out.Frame.Type = "changes"
	if initialSnapshot {
		out.Frame.Type = pa.RosterSnapshot
		if !p.Snapshot {
			out.Frame.Type = pa.RosterSnapshotComplete
		}
	}
	out.Frame.SourceEpoch = s.sourceEpoch
	out.Frame.Cursor, err = s.rosterCursor(q, p)
	if err != nil {
		return out, err
	}
	// Exact wire size includes token/field overhead, not just projected rows.
	raw, _ := json.Marshal(out.Frame)
	if len(raw) > q.ByteLimit {
		return out, failure(pa.CapacityFull, "roster_frame_bytes")
	}
	return out, tx.Commit()
}

// SQLite projects only the facts needed by projectRosterActivity: at most the
// active and last eligible admitted turn, one anchor message and pending controls.
// Histories, instructions on old turns, trace payloads and permission options
// never cross into Go. SQL checks bytes BEFORE returning a projected string.
// replaceRosterLead repairs every other sidebar designation in the same write transaction.
func replaceRosterLead(ctx context.Context, tx *sql.Tx, next aggregate) error {
	if !next.Bot.Lead || next.BackgroundOwner != nil {
		return failure(pa.ScopeDenied, "lead_sidebar")
	}
	key := next.Bot.Key
	rows, err := tx.QueryContext(ctx, `SELECT bot FROM project_assistant_bots WHERE machine=? AND bot<>?
		AND json_extract(CAST(data AS TEXT),'$.BackgroundOwner') IS NULL
		AND json_extract(CAST(data AS TEXT),'$.Bot.lead')=1`, key.MachineID, key.BotID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var prior []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		prior = append(prior, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range prior {
		previous, err := load(ctx, tx, pa.BotKey{MachineID: key.MachineID, BotID: id})
		if err != nil {
			return err
		}
		expected := previous.Bot.Version
		previous.Bot.Lead = false
		previous.Bot.SettingsVersion = max(1, previous.Bot.SettingsVersion) + 1
		previous.Bot.Version++
		previous.Bot.UpdatedAt = next.Bot.UpdatedAt
		if err := persist(ctx, tx, previous, expected); err != nil {
			return err
		}
	}
	return nil
}

// rosterOpenDecisionSQL is openPermission for one pending offer: a tool
// permission, or a decision that has not expired. A zero expiry never
// expires. Chat activity never closes a decision here; storage does.
func rosterOpenDecisionSQL(v string) string {
	field := func(path string) string { return "json_extract(" + v + ",'" + path + "')" }
	return `(coalesce(` + field("$.kind") + `,'')<>'decision' OR coalesce(` + field("$.expires_at") + `,'')='' OR ` + field("$.expires_at") + ` LIKE '0001-01-01T00:00:00%' OR julianday(` + field("$.expires_at") + `)>julianday('now'))`
}

// JSONB is query-local only. The pinned SQLite supports it; parsing the same
// large text aggregate inside each correlated scalar would retain the reader
// snapshot for seconds. Stored bytes and returned projections remain JSON.
var rosterProjectionSQL = `WITH a AS MATERIALIZED (
 SELECT jsonb(CAST(data AS TEXT)) j FROM project_assistant_bots WHERE machine=? AND bot=?
) ` + roomTurnsCTE + withdrawnTurnsCTE + `, turns AS MATERIALIZED (
 SELECT t.key,t.value FROM a,json_each(j,'$.Turns') t
), own_messages AS MATERIALIZED (
 SELECT m.key,m.value FROM a,json_each(j,'$.Messages') m WHERE coalesce(json_extract(m.value,'$.output.quiet'),0)<>1 AND ` + ownChatMessageSQL("json_extract(m.value,'$.turn_id')") + `
), admissions AS MATERIALIZED (
 SELECT m.key,m.value FROM own_messages m WHERE json_extract(m.value,'$.kind') IN ('user','notice')
), u AS MATERIALIZED (
 SELECT m.value message,t.key turnid FROM admissions m JOIN turns t ON t.key=json_extract(m.value,'$.turn_id')
 WHERE coalesce(json_extract(t.value,'$.joined_turn_id'),'')='' AND json_extract(t.value,'$.message_id')=json_extract(m.value,'$.id') ORDER BY CAST(m.key AS INTEGER) DESC LIMIT 1
), latest_message AS MATERIALIZED (
 SELECT m.value message FROM own_messages m ORDER BY CAST(m.key AS INTEGER) DESC LIMIT 1
), background(active,waiting,needs_input) AS MATERIALIZED (
 SELECT ` + backgroundWorkCountsSQL + backgroundWorkFromSQL + ` AND t.machine=?1 AND t.bot=?2
), projection AS MATERIALIZED (
 SELECT json_object('Bot',json_extract(j,'$.Bot'),'Conversation',json_object('id',json_extract(j,'$.Conversation.id'),'active_turn_id',json_extract(j,'$.Conversation.active_turn_id'),'generation',json_extract(j,'$.Conversation.generation'),'binding_id',json_extract(j,'$.Conversation.binding_id')),
 'Turns',json((SELECT json_group_object(t.key,json_object('id',json_extract(t.value,'$.id'),'message_id',json_extract(t.value,'$.message_id'),'joined_turn_id',json_extract(t.value,'$.joined_turn_id'),'room',json_extract(t.value,'$.room'),'fence',json_extract(t.value,'$.fence'),'state',json_extract(t.value,'$.state'),'failure',json_extract(t.value,'$.failure'))) FROM turns t WHERE t.key=json_extract(j,'$.Conversation.active_turn_id') OR t.key=(SELECT turnid FROM u))),
 'Messages',json((SELECT json_group_array(json(message)) FROM u)),
 'Permissions',json((SELECT json_group_object(p.key,json_object('id',json_extract(p.value,'$.id'),'fence',json_extract(p.value,'$.fence'),'turn_id',json_extract(p.value,'$.turn_id'),'state',json_extract(p.value,'$.state'),'action',json_object('title',json_extract(p.value,'$.action.title')),'kind',json_extract(p.value,'$.kind'),'anchor_message_seq',json_extract(p.value,'$.anchor_message_seq'),'expires_at',json_extract(p.value,'$.expires_at'),'decision',CASE WHEN json_extract(p.value,'$.kind')='decision' THEN json_object('question',json_extract(p.value,'$.decision.question'),'exchange_id',json_extract(p.value,'$.decision.exchange_id')) END)) FROM json_each(j,'$.Permissions') p WHERE json_extract(p.value,'$.state')='pending' AND (json_extract(p.value,'$.turn_id')=json_extract(j,'$.Conversation.active_turn_id') OR json_extract(p.value,'$.kind')='decision' AND ` + rosterOpenDecisionSQL("p.value") + `))),
 'Completion',json_extract(j,'$.Completion'),'RosterTrace',json_extract(j,'$.RosterTrace')) projected,
 coalesce((SELECT value FROM json_each(j,'$.ReadCursors') WHERE key=?),'') read_cursor,
 coalesce((SELECT nullif(json_extract(message,'$.text'),'') FROM latest_message),(SELECT group_concat(json_extract(value,'$.name'),' · ') FROM latest_message,json_each(message,'$.files')),'') latest,
 coalesce((SELECT json_extract(message,'$.at') FROM latest_message),json_extract(j,'$.Bot.created_at')) activity_at,
 (SELECT count(*) FROM own_messages WHERE json_extract(value,'$.kind') IN ` + unreadKindsSQL + ` AND json_extract(value,'$.seq')>coalesce((SELECT json_extract(value,'$.message_seq') FROM json_each(j,'$.ReadCursors') WHERE key=?),0)
   AND NOT (json_extract(value,'$.exchange') IS NOT NULL
     AND json_extract(value,'$.exchange.target.key.machine_id')=json_extract(j,'$.Bot.key.machine_id')
     AND json_extract(value,'$.exchange.target.key.bot_id')=json_extract(j,'$.Bot.key.bot_id'))) unread,
 (SELECT count(*) FROM json_each(j,'$.Permissions') p WHERE json_extract(p.value,'$.state')='pending' AND ` + rosterOpenDecisionSQL("p.value") + `) pending,
 (SELECT count(*) FROM json_each(j,'$.WorkLinks') WHERE json_extract(value,'$.state') IN ` + activeWorkLinkStatesSQL + `) work,
 coalesce((SELECT json_group_array(json_object('state',json_extract(value,'$.state'),'landing',json_extract(value,'$.landing'))) FROM json_each(j,'$.WorkLinks') WHERE json_extract(value,'$.state') IN ` + dispatchedWorkLinkStatesSQL + `),'[]') dispatched FROM a
), guarded AS MATERIALIZED (
 SELECT *,length(CAST(projected AS BLOB))+length(CAST(read_cursor AS BLOB))+length(CAST(latest AS BLOB))+length(CAST(activity_at AS BLOB))<=? fits FROM projection
)
SELECT CASE WHEN fits THEN projected END,CASE WHEN fits THEN read_cursor ELSE '' END,CASE WHEN fits THEN latest ELSE '' END,CASE WHEN fits THEN activity_at ELSE '' END,unread,pending,work,dispatched,active,waiting,needs_input,
 (SELECT coalesce(max(seq),0) FROM project_assistant_roster_changes WHERE machine=? AND bot=?) FROM guarded CROSS JOIN background`

func rosterSummary(ctx context.Context, tx *sql.Tx, key pa.BotKey, reader string, budget int) (pa.BotSummary, app.RosterIdentity, int64, error) {
	var row pa.BotSummary
	var id app.RosterIdentity
	var projected sql.NullString
	var read, latest, at string
	var unread, pending, work, revision int64
	var background pa.BackgroundWork
	var dispatched string
	err := tx.QueryRowContext(ctx, rosterProjectionSQL, key.MachineID, key.BotID, reader, reader, budget, key.MachineID, key.BotID).Scan(&projected, &read, &latest, &at, &unread, &pending, &work, &dispatched, &background.Working, &background.Waiting, &background.NeedsInput, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return row, id, 0, failure(pa.NotFound, "roster_bot")
	}
	if err != nil {
		return row, id, 0, err
	}
	if !projected.Valid {
		return row, id, 0, failure(pa.CapacityFull, "roster_projection_bytes")
	}
	var a aggregate
	if json.Unmarshal([]byte(projected.String), &a) != nil || a.Bot.Key != key || a.Bot.ConversationID != a.Conversation.ID || pa.ValidateScope(a.Bot.Project) != nil {
		return row, id, 0, failure(pa.Invalid, "roster_projection")
	}
	row = pa.BotSummary{Bot: a.Bot, Version: a.Bot.Version, LatestMessage: latest, UnreadCount: unread, PendingPermissions: pending, ActiveWork: work, ReadCursor: pa.ReadCursor{Key: key, ConversationID: a.Conversation.ID, ReaderID: reader}}
	if read != "" && json.Unmarshal([]byte(read), &row.ReadCursor) != nil {
		return row, id, 0, failure(pa.Invalid, "roster_reader")
	}
	if row.ReadCursor.ReaderID != reader || row.ReadCursor.Key != key || row.ReadCursor.ConversationID != a.Conversation.ID {
		return row, id, 0, failure(pa.ScopeDenied, "roster_reader")
	}
	row.ActivityAt, err = time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return row, id, 0, failure(pa.Invalid, "roster_activity_at")
	}
	var links []pa.WorkLink
	if json.Unmarshal([]byte(dispatched), &links) != nil {
		return row, id, 0, failure(pa.Invalid, "roster_dispatched_work")
	}
	row.DispatchedWork = countDispatchedWork(slices.Values(links))
	projectRosterActivity(a, &row)
	addRosterBackground(&row, background)
	id = app.RosterIdentity{Key: key, Project: a.Bot.Project, ConversationID: a.Conversation.ID, Generation: a.Conversation.Generation, BindingID: a.Conversation.BindingID}
	return row, id, revision, nil
}

func (s *ViewStore) ValidateRoster(ctx context.Context, ids []app.RosterIdentity) error {
	tx, err := s.borrowedDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		var project, conversation, binding string
		var generation int64
		err = tx.QueryRowContext(ctx, `SELECT json_extract(CAST(data AS TEXT),'$.Bot.project'),json_extract(CAST(data AS TEXT),'$.Conversation.id'),json_extract(CAST(data AS TEXT),'$.Conversation.generation'),json_extract(CAST(data AS TEXT),'$.Conversation.binding_id') FROM project_assistant_bots WHERE machine=? AND bot=?`, id.Key.MachineID, id.Key.BotID).Scan(&project, &conversation, &generation, &binding)
		if id.Removed && errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return failure(pa.CursorExpired, "roster_generation")
		}
		var scope pa.ProjectScope
		if id.Removed || json.Unmarshal([]byte(project), &scope) != nil || scope != id.Project || conversation != id.ConversationID || generation != id.Generation || binding != id.BindingID {
			return failure(pa.CursorExpired, "roster_generation")
		}
	}
	return tx.Commit()
}
