package projectassistantstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

const groupReactionsSchema = `CREATE TABLE IF NOT EXISTS project_assistant_group_reactions (
 machine TEXT NOT NULL, grp TEXT NOT NULL, seq INTEGER NOT NULL,
 actor TEXT NOT NULL, data BLOB NOT NULL,
 PRIMARY KEY(machine,grp,seq,actor),
 FOREIGN KEY(machine,grp,seq) REFERENCES project_assistant_group_lines(machine,grp,seq) ON DELETE CASCADE)`

func projectGroupReactions(ctx context.Context, tx *sql.Tx, key pa.BotKey, lines []pa.RoomEntry) (map[int64][]pa.ReactionActor, error) {
	if len(lines) == 0 {
		return nil, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT seq,data FROM project_assistant_group_reactions
 WHERE machine=? AND grp=? AND seq BETWEEN ? AND ? ORDER BY seq,actor`, key.MachineID, key.BotID, lines[0].Seq, lines[len(lines)-1].Seq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[int64][]pa.ReactionActor)
	for rows.Next() {
		var seq int64
		var raw []byte
		if err := rows.Scan(&seq, &raw); err != nil {
			return nil, err
		}
		var actor pa.ReactionActor
		if err := json.Unmarshal(raw, &actor); err != nil {
			return nil, err
		}
		result[seq] = append(result[seq], actor)
	}
	return result, rows.Err()
}

func (s *GroupStore) ReactToGroupLine(ctx context.Context, q app.GroupReactionRequest, actorID string) (pa.Receipt, error) {
	var zero pa.Receipt
	tx, err := s.begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	key := q.Command.Key
	group, err := groupRead(ctx, tx, key)
	if err != nil {
		return zero, err
	}
	if group.Deleted || group.ConversationID != q.ConversationID {
		return zero, failure(pa.ScopeDenied, "group_reaction_scope")
	}
	if prior, err := replayed(ctx, tx, q.Command, pa.ReactOperation); err != nil {
		return zero, err
	} else if prior != nil {
		return *prior, nil
	}
	if group.Version != q.GroupVersion {
		return zero, failure(pa.StaleVersion, "group_reaction_version")
	}
	entry, err := roomLine(ctx, tx, key, q.MessageSeq)
	if err != nil {
		return zero, err
	}
	if entry.Kind != pa.RoomPersonEntry && entry.Kind != pa.RoomMemberEntry {
		return zero, failure(pa.Invalid, "group_reaction_message")
	}
	actor := pa.ReactionActor{ID: actorID, Kind: "human", Emojis: []string{}}
	var old []byte
	err = tx.QueryRowContext(ctx, `SELECT data FROM project_assistant_group_reactions
 WHERE machine=? AND grp=? AND seq=? AND actor=?`, key.MachineID, key.BotID, q.MessageSeq, actorID).Scan(&old)
	if err == nil {
		if err := json.Unmarshal(old, &actor); err != nil {
			return zero, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return zero, err
	}
	actor, err = pa.ApplyHumanReaction(actor, q.Emoji, q.Present, q.ExpectedRevision)
	if err != nil {
		return zero, err
	}
	raw, err := json.Marshal(actor)
	if err != nil {
		return zero, err
	}
	// Limit the metadata for one line to the existing message byte budget.
	// Even a heavily reacted-to line must fit in a whole-line Cloud history page.
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT coalesce(sum(length(data)),0)
 FROM project_assistant_group_reactions WHERE machine=? AND grp=? AND seq=?`, key.MachineID, key.BotID, q.MessageSeq).Scan(&total); err != nil {
		return zero, err
	}
	if total-len(old)+len(raw) > pa.MaxMessageBytes {
		return zero, failure(pa.CapacityFull, "group_reaction_bytes")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO project_assistant_group_reactions(machine,grp,seq,actor,data)
 VALUES(?,?,?,?,?) ON CONFLICT(machine,grp,seq,actor) DO UPDATE SET data=excluded.data`,
		key.MachineID, key.BotID, q.MessageSeq, actorID, raw); err != nil {
		return zero, err
	}
	return s.finish(ctx, tx, q.Command, strconv.FormatInt(q.MessageSeq, 10))
}
