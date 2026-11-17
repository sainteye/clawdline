package projectassistantstore

import (
	"context"
	"database/sql"
	"encoding/json"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func restoreReactions(ctx context.Context, tx *sql.Tx, a aggregate, records []pa.ReactionRecord) error {
	seen := map[string]bool{}
	for _, r := range records {
		found := false
		for _, m := range a.Messages {
			if m.ID == r.MessageID && m.Fence == r.Fence && pa.Reactable(m) {
				found = true
				break
			}
		}
		v := r.Actor
		if v.Appearance != nil && pa.ValidateAppearance(*v.Appearance) != nil {
			return failure(pa.Invalid, "reaction_appearance")
		}
		identity := r.MessageID + "/" + v.ID
		if !found || v.ID == "" || v.Revision < 1 || v.Own || seen[identity] || v.Kind != "human" && v.Kind != "bot" || v.Kind == "human" && (v.Bot != nil || len(v.Emojis) > pa.MaxHumanReactions) || v.Kind == "bot" && (v.Bot == nil || *v.Bot != a.Bot.Key || len(v.Emojis) != 1) {
			return failure(pa.Invalid, "reaction_archive")
		}
		seen[identity] = true
		emojis := map[string]bool{}
		for _, emoji := range v.Emojis {
			normalized, e := pa.ReactionEmoji(emoji)
			if e != nil || normalized != emoji || emojis[emoji] {
				return failure(pa.Invalid, "reaction_archive")
			}
			emojis[emoji] = true
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_reactions(machine,bot,conversation,message,actor,data) VALUES(?,?,?,?,?,?)`, r.Fence.Key.MachineID, r.Fence.Key.BotID, r.Fence.ConversationID, r.MessageID, v.ID, raw); err != nil {
			return err
		}
	}
	return nil
}

// Archives read their dedicated relation once; immutable message facts stay untouched.
func exportReactions(ctx context.Context, tx *sql.Tx, messages []pa.Message) ([]pa.ReactionRecord, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	first := messages[0]
	fences := map[string]pa.Fence{}
	for _, m := range messages {
		fences[m.ID] = m.Fence
	}
	rows, err := tx.QueryContext(ctx, `SELECT message,data FROM project_assistant_reactions WHERE machine=? AND bot=? AND conversation=? ORDER BY message,actor`, first.Fence.Key.MachineID, first.Fence.Key.BotID, first.Fence.ConversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pa.ReactionRecord
	for rows.Next() {
		var id string
		var raw []byte
		var actor pa.ReactionActor
		if err = rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		f, ok := fences[id]
		if !ok {
			return nil, failure(pa.ScopeDenied, "reaction_archive_message")
		}
		if err = json.Unmarshal(raw, &actor); err != nil {
			return nil, err
		}
		out = append(out, pa.ReactionRecord{Fence: f, MessageID: id, Actor: actor})
	}
	return out, rows.Err()
}
