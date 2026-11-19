package projectassistantstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"time"
)

const reactionsSchema = `CREATE TABLE IF NOT EXISTS project_assistant_reactions (
 machine TEXT NOT NULL, bot TEXT NOT NULL, conversation TEXT NOT NULL,
 message TEXT NOT NULL, actor TEXT NOT NULL, data BLOB NOT NULL,
 PRIMARY KEY(machine,bot,conversation,message,actor))`

func messageReactions(ctx context.Context, r interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, m *pa.Message) error {
	var size int64
	if e := r.QueryRowContext(ctx, `SELECT coalesce(sum(length(data)),0) FROM project_assistant_reactions WHERE machine=? AND bot=? AND conversation=? AND message=?`, m.Fence.Key.MachineID, m.Fence.Key.BotID, m.Fence.ConversationID, m.ID).Scan(&size); e != nil {
		return e
	}
	if size > cloudops.ReadPlaintextBytesLimit {
		return failure(pa.CapacityFull, "reaction_read_bytes")
	}
	rows, err := r.QueryContext(ctx, `SELECT data FROM project_assistant_reactions WHERE machine=? AND bot=? AND conversation=? AND message=? ORDER BY actor`, m.Fence.Key.MachineID, m.Fence.Key.BotID, m.Fence.ConversationID, m.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var a pa.ReactionActor
		if err = rows.Scan(&raw); err != nil {
			return err
		}
		if err = json.Unmarshal(raw, &a); err != nil {
			return err
		}
		m.Reactions = append(m.Reactions, a)
	}
	return rows.Err()
}

// React serializes with the aggregate but never rewrites it or changes its CAS.
// The existing durable receipt transaction makes an explicit retry the same
// operation even when its first HTTP response was lost.
func (s *Store) React(ctx context.Context, w app.ReactionWrite) (pa.Receipt, error) {
	unlock := s.lock(w.Command.Key)
	defer unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return pa.Receipt{}, err
	}
	defer tx.Rollback()
	a, err := load(ctx, tx, w.Command.Key)
	if err != nil {
		return pa.Receipt{}, err
	}
	if a.Bot.Project != w.Fence.Project || a.Conversation.ID != w.Fence.ConversationID {
		return pa.Receipt{}, failure(pa.ScopeDenied, "reaction_conversation")
	}
	var target *pa.Message
	for i := range a.Messages {
		if a.Messages[i].ID == w.MessageID {
			m := a.Messages[i]
			target = &m
			break
		}
	}
	if target == nil {
		return pa.Receipt{}, failure(pa.NotFound, "reaction_message")
	}
	if !pa.Reactable(*target) || withdrawnTurn(a.Turns[target.TurnID]) {
		return pa.Receipt{}, failure(pa.Invalid, "reaction_message")
	}
	if w.Actor.ID == "" || (w.Actor.Kind != "human" && w.Actor.Kind != "bot") {
		return pa.Receipt{}, failure(pa.ScopeDenied, "reaction_actor")
	}
	if w.NativeTurnID != "" {
		t, ok := a.Turns[w.NativeTurnID]
		if w.NativeBot == nil || a.Bot.Mode != w.NativeBot.Mode || a.Bot.PermissionPolicy != w.NativeBot.PermissionPolicy || a.Bot.Agent != w.NativeBot.Agent || a.Bot.LoginProfile != w.NativeBot.LoginProfile || a.Bot.Instructions != w.NativeBot.Instructions || a.Bot.FallbackAgents != w.NativeBot.FallbackAgents || a.Bot.FallbackSteps != w.NativeBot.FallbackSteps || !pa.SameRuntimeSettings(a.Bot.RuntimeSettings, w.NativeBot.RuntimeSettings) || !pa.SameExtensions(a.Bot.Extensions, w.NativeBot.Extensions) {
			return pa.Receipt{}, failure(pa.Revoked, "reaction_native_configuration")
		}
		if !ok || t.State != pa.TurnRunning || t.Fence != w.Fence || a.Conversation.ActiveTurnID != t.ID || a.Conversation.BindingID != w.Fence.BindingID || a.Conversation.Generation != w.Fence.Generation || t.MessageID != target.ID || (t.Origin != pa.UserOrigin && t.Origin != "") || t.Room != nil || t.Network != nil || w.Actor.Kind != "bot" || w.Actor.Bot == nil || *w.Actor.Bot != a.Bot.Key {
			return pa.Receipt{}, failure(pa.ScopeDenied, "reaction_native_turn")
		}
	} else if w.Actor.Kind != "human" || w.Actor.Bot != nil {
		return pa.Receipt{}, failure(pa.ScopeDenied, "reaction_actor")
	}
	if prior, e := receipt(ctx, tx, w.Command); e != nil {
		return pa.Receipt{}, e
	} else if prior != nil {
		return *prior, nil
	}
	emoji, err := pa.ReactionEmoji(w.Emoji)
	if err != nil {
		return pa.Receipt{}, err
	}
	if err = messageReactions(ctx, tx, target); err != nil {
		return pa.Receipt{}, err
	}
	actor := w.Actor
	actor.Emojis = []string{}
	for _, v := range target.Reactions {
		if v.ID == actor.ID {
			actor = v
			break
		}
	}
	if actor.Kind == "bot" {
		actor.Name = a.Bot.Name
		appearance := a.Bot.Appearance
		actor.Appearance = &appearance
		actor.Emojis = []string{emoji}
		actor.Revision++
	} else if actor, err = pa.ApplyHumanReaction(actor, emoji, w.Present, w.ExpectedRevision); err != nil {
		return pa.Receipt{}, err
	}
	actor.Own = false
	raw, err := json.Marshal(actor)
	if err != nil {
		return pa.Receipt{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_reactions(machine,bot,conversation,message,actor,data) VALUES(?,?,?,?,?,?) ON CONFLICT(machine,bot,conversation,message,actor) DO UPDATE SET data=excluded.data`, w.Command.Key.MachineID, w.Command.Key.BotID, w.Fence.ConversationID, w.MessageID, actor.ID, raw)
	if err != nil {
		return pa.Receipt{}, err
	}
	target.Reactions = nil
	if err = messageReactions(ctx, tx, target); err != nil {
		return pa.Receipt{}, err
	}
	*target = app.DisplayArtifactMessage(*target, a.ArtifactTitles)
	e := pa.Event{Version: pa.ContractVersion, ID: w.EventID, Fence: target.Fence, Kind: pa.ReactionsChanged, Message: target, At: time.Now().UTC()}
	// The event validates the immutable message and projects current metadata.
	a.Messages = append([]pa.Message(nil), a.Messages...)
	for i := range a.Messages {
		if a.Messages[i].ID == target.ID {
			a.Messages[i] = *target
		}
	}
	seq, err := appendEvent(ctx, tx, a, e)
	if err != nil {
		return pa.Receipt{}, err
	}
	r := pa.Receipt{ID: w.ReceiptID, Command: w.Command, State: pa.EffectDelivered, ResourceID: w.MessageID, EventSeq: seq}
	if err = writeReceipt(ctx, tx, r); err != nil {
		return pa.Receipt{}, err
	}
	if err = tx.Commit(); err != nil {
		return pa.Receipt{}, err
	}
	return r, nil
}
