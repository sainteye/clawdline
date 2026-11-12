package projectassistantstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func validateStoredEvent(a aggregate, e pa.Event) error {
	if err := pa.ValidateEvent(e); err != nil {
		return err
	}
	f := e.Fence
	if f.Key != a.Bot.Key || f.Project != a.Bot.Project || f.ConversationID != a.Conversation.ID {
		return failure(pa.ScopeDenied, "event")
	}
	if f.BindingID == "" {
		if f.Generation != 0 {
			return failure(pa.StaleGeneration, "event")
		}
	} else {
		b, ok := a.Bindings[f.BindingID]
		if !ok || b.Fence != f {
			return failure(pa.StaleGeneration, "event")
		}
	}
	match := false
	switch e.Kind {
	case pa.BotCreated, pa.BotUpdated:
		match = reflect.DeepEqual(e.Bot.Bot, a.Bot)
		if e.Bot.ReadCursor.Key != (pa.BotKey{}) && e.Bot.ReadCursor.Key != a.Bot.Key || e.Bot.ReadCursor.ConversationID != "" && e.Bot.ReadCursor.ConversationID != a.Conversation.ID || e.Bot.Source.MachineID != "" && e.Bot.Source.MachineID != a.Bot.Key.MachineID {
			match = false
		}
		for _, cap := range e.Bot.Source.Capabilities {
			if cap.Key != a.Bot.Key || cap.Project != a.Bot.Project {
				match = false
			}
		}
	case pa.MessageCreated:
		for _, m := range a.Messages {
			if reflect.DeepEqual(m, *e.Message) && m.Fence == f {
				match = true
			}
		}
	case pa.TurnUpdated:
		t, ok := a.Turns[e.Turn.ID]
		match = ok && reflect.DeepEqual(t, *e.Turn) && t.Fence == f
	case pa.PermissionOffered, pa.PermissionAnswered:
		p, ok := a.Permissions[e.Permission.ID]
		match = ok && reflect.DeepEqual(p, *e.Permission) && p.Fence == f
	case pa.WorkUpdated:
		w, ok := a.WorkLinks[e.Work.ID]
		match = ok && reflect.DeepEqual(w, *e.Work) && w.Fence == f
	case pa.MemoryChanged:
		match = reflect.DeepEqual(a.Memory, *e.Memory)
	case pa.ReadAdvanced:
		r, ok := a.ReadCursors[e.Read.ReaderID]
		match = ok && reflect.DeepEqual(r, *e.Read)
	case pa.TraceUpdated:
		t, ok := a.Turns[e.Trace.TurnID]
		match = ok && t.Fence == f && f.Generation == a.Conversation.Generation && f.BindingID == a.Conversation.BindingID
	case pa.SourceUpdated:
		match = e.Source.MachineID == a.Bot.Key.MachineID
		for _, c := range e.Source.Capabilities {
			if c.Key != a.Bot.Key || c.Project != a.Bot.Project {
				match = false
			}
		}
	case pa.FeatureDisabled:
		match = true
	}
	if !match {
		return failure(pa.ScopeDenied, "event_payload")
	}
	return nil
}

func appendEvent(ctx context.Context, tx *sql.Tx, a aggregate, e pa.Event) (int64, error) {
	// Sequence allocation belongs to the transaction, never to the caller.
	e.Seq = 1
	if err := validateStoredEvent(a, e); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO project_assistant_events(machine,bot,event_id,data) VALUES(?,?,?,?)`, a.Bot.Key.MachineID, a.Bot.Key.BotID, e.ID, []byte("null"))
	if err != nil {
		return 0, err
	}
	seq, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	e.Seq = seq
	data, err := json.Marshal(e)
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE project_assistant_events SET data=? WHERE seq=?`, data, seq)
	return seq, err
}

// Read has no retention/eviction. Zero starts at the beginning; a cursor ahead
// of the source's durable log is expired (for example after restoring a backup).
func (s *Store) Read(ctx context.Context, q pa.EventQuery) (pa.EventPage, error) {
	out := pa.EventPage{Items: []pa.Event{}, NextSeq: q.AfterSeq}
	if err := pa.CheckPage(q.PageSize, true); err != nil {
		return out, err
	}
	if q.AfterSeq < 0 {
		return out, failure(pa.CursorExpired, "event_cursor")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if _, err = load(ctx, tx, q.Key); err != nil {
		return out, err
	}
	var last int64
	if err = tx.QueryRowContext(ctx, `SELECT coalesce(max(seq),0) FROM project_assistant_events WHERE machine=?`, q.Key.MachineID).Scan(&last); err != nil {
		return out, err
	}
	if q.AfterSeq > last {
		return out, failure(pa.CursorExpired, "event_cursor")
	}
	rows, err := tx.QueryContext(ctx, `SELECT data FROM project_assistant_events WHERE machine=? AND bot=? AND seq>? ORDER BY seq LIMIT ?`, q.Key.MachineID, q.Key.BotID, q.AfterSeq, q.PageSize+1)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return out, err
		}
		if len(out.Items) == q.PageSize {
			out.HasMore = true
			break
		}
		var e pa.Event
		if err = json.Unmarshal(raw, &e); err != nil {
			rows.Close()
			return out, err
		}
		out.Items = append(out.Items, e)
		out.NextSeq = e.Seq
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}
