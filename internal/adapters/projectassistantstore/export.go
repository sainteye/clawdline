package projectassistantstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sort"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Archive adds an integrity manifest without changing the frozen port Export.
// SHA256 detects damaged transfer bytes; it is not a signature or authorization.
type Archive struct {
	Data     app.Export `json:"data"`
	Manifest Manifest   `json:"manifest"`
}
type Manifest struct {
	ContractVersion string `json:"contract_version"`
	SchemaVersion   int    `json:"schema_version"`
	SHA256          string `json:"sha256"`
}

func digest(data app.Export) (string, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func (s *Store) ExportArchive(ctx context.Context, key pa.BotKey) (Archive, error) {
	data, err := s.Export(ctx, key)
	if err != nil {
		return Archive{}, err
	}
	sum, err := digest(data)
	return Archive{Data: data, Manifest: Manifest{ContractVersion: pa.ContractVersion, SchemaVersion: pa.SchemaVersion, SHA256: sum}}, err
}
func (s *Store) RestoreArchive(ctx context.Context, in Archive) error {
	sum, err := digest(in.Data)
	if err != nil {
		return err
	}
	if in.Manifest.ContractVersion != pa.ContractVersion || in.Manifest.SchemaVersion != pa.SchemaVersion {
		return failure(pa.Unsupported, "manifest_version")
	}
	if sum != in.Manifest.SHA256 {
		return failure(pa.Invalid, "manifest_integrity")
	}
	return s.Restore(ctx, in.Data)
}

func (s *Store) Export(ctx context.Context, key pa.BotKey) (app.Export, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return app.Export{}, err
	}
	defer tx.Rollback()
	a, err := load(ctx, tx, key)
	if err != nil {
		return app.Export{}, err
	}
	out := app.Export{ContractVersion: pa.ContractVersion, SchemaVersion: pa.SchemaVersion, Bot: a.Bot, Conversation: a.Conversation, Messages: a.Messages, Memory: a.Memory, Context: snapshot(a), Turns: []pa.Turn{}, Bindings: []pa.ExecutionBinding{}, Permissions: []pa.PermissionOffer{}, WorkLinks: []pa.WorkLink{}, MemoryEntries: []pa.MemoryEntry{}, ReadCursors: []pa.ReadCursor{}, Events: []pa.Event{}, Receipts: []pa.Receipt{}}
	for _, v := range a.Turns {
		out.Turns = append(out.Turns, v)
	}
	sort.Slice(out.Turns, func(i, j int) bool { return out.Turns[i].ID < out.Turns[j].ID })
	for _, v := range a.Bindings {
		out.Bindings = append(out.Bindings, v)
	}
	sort.Slice(out.Bindings, func(i, j int) bool { return out.Bindings[i].Fence.BindingID < out.Bindings[j].Fence.BindingID })
	for _, v := range a.Permissions {
		out.Permissions = append(out.Permissions, v)
	}
	sort.Slice(out.Permissions, func(i, j int) bool { return out.Permissions[i].ID < out.Permissions[j].ID })
	for _, v := range a.WorkLinks {
		out.WorkLinks = append(out.WorkLinks, v)
	}
	sort.Slice(out.WorkLinks, func(i, j int) bool { return out.WorkLinks[i].ID < out.WorkLinks[j].ID })
	for _, v := range a.Entries {
		out.MemoryEntries = append(out.MemoryEntries, v)
	}
	sort.Slice(out.MemoryEntries, func(i, j int) bool { return out.MemoryEntries[i].ID < out.MemoryEntries[j].ID })
	for _, v := range a.ReadCursors {
		out.ReadCursors = append(out.ReadCursors, v)
	}
	sort.Slice(out.ReadCursors, func(i, j int) bool { return out.ReadCursors[i].ReaderID < out.ReadCursors[j].ReaderID })
	rows, err := tx.QueryContext(ctx, `SELECT data FROM project_assistant_events WHERE machine=? AND bot=? ORDER BY seq`, key.MachineID, key.BotID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return out, err
		}
		var e pa.Event
		if err = json.Unmarshal(raw, &e); err != nil {
			rows.Close()
			return out, err
		}
		out.Events = append(out.Events, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT data FROM project_assistant_receipts WHERE machine=? AND bot=? ORDER BY operation,command_key`, key.MachineID, key.BotID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return out, err
		}
		var r pa.Receipt
		if err = json.Unmarshal(raw, &r); err != nil {
			rows.Close()
			return out, err
		}
		out.Receipts = append(out.Receipts, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}

// Restore inserts a missing Bot and its complete journal. It never overwrites
// existing facts. Sequence/ID collisions reject the entire transaction. The
// supported disaster-recovery path restores into a caller-owned empty schema.
func (s *Store) Restore(ctx context.Context, in app.Export) error {
	if in.ContractVersion != pa.ContractVersion || in.SchemaVersion != pa.SchemaVersion {
		return failure(pa.Unsupported, "export_version")
	}
	unlock := s.lock(in.Bot.Key)
	defer unlock()
	a := aggregate{Bot: in.Bot, Conversation: in.Conversation, Messages: in.Messages, Memory: in.Memory, Turns: map[string]pa.Turn{}, Bindings: map[string]pa.ExecutionBinding{}, Permissions: map[string]pa.PermissionOffer{}, WorkLinks: map[string]pa.WorkLink{}, Entries: map[string]pa.MemoryEntry{}, ReadCursors: map[string]pa.ReadCursor{}}
	for _, v := range in.Turns {
		a.Turns[v.ID] = v
	}
	for _, v := range in.Bindings {
		a.Bindings[v.Fence.BindingID] = v
	}
	for _, v := range in.Permissions {
		a.Permissions[v.ID] = v
	}
	for _, v := range in.WorkLinks {
		a.WorkLinks[v.ID] = v
	}
	for _, v := range in.MemoryEntries {
		a.Entries[v.ID] = v
	}
	for _, v := range in.ReadCursors {
		a.ReadCursors[v.ReaderID] = v
	}
	if len(a.Turns) != len(in.Turns) || len(a.Bindings) != len(in.Bindings) || len(a.Permissions) != len(in.Permissions) || len(a.WorkLinks) != len(in.WorkLinks) || len(a.Entries) != len(in.MemoryEntries) || len(a.ReadCursors) != len(in.ReadCursors) {
		return failure(pa.Invalid, "duplicate_export_fact")
	}
	if err := validateAggregate(a); err != nil {
		return err
	}
	if !reflect.DeepEqual(snapshot(a), in.Context) {
		return failure(pa.ContextStale, "export_context")
	}
	last := int64(0)
	eventIDs := map[string]bool{}
	eventSeqs := map[int64]bool{}
	for _, e := range in.Events {
		if err := pa.ValidateEvent(e); err != nil {
			return err
		}
		if e.Fence.Key != a.Bot.Key || e.Fence.Project != a.Bot.Project || e.Fence.ConversationID != a.Conversation.ID {
			return failure(pa.ScopeDenied, "export_event")
		}
		if e.Seq <= last || eventIDs[e.ID] {
			return failure(pa.Invalid, "export_event_order")
		}
		last = e.Seq
		eventIDs[e.ID] = true
		eventSeqs[e.Seq] = true
		if e.Fence.Generation < 0 || e.Fence.Generation > a.Conversation.Generation {
			return failure(pa.StaleGeneration, "export_event")
		}
		if e.Fence.BindingID != "" {
			binding, ok := a.Bindings[e.Fence.BindingID]
			if !ok || binding.Fence != e.Fence {
				return failure(pa.ScopeDenied, "export_event_binding")
			}
		} else if e.Fence.Generation != 0 {
			return failure(pa.Invalid, "export_event_binding")
		}
		// Historical events retain old versions, but never a different authority.
		if e.Message != nil && e.Message.Fence != e.Fence || e.Turn != nil && e.Turn.Fence != e.Fence || e.Permission != nil && e.Permission.Fence != e.Fence || e.Work != nil && e.Work.Fence != e.Fence {
			return failure(pa.ScopeDenied, "export_event_payload")
		}
		if e.Memory != nil && (e.Memory.Key != a.Bot.Key || e.Memory.Project != a.Bot.Project || e.Memory.Revision > a.Memory.Revision) {
			return failure(pa.ScopeDenied, "export_memory_event")
		}
		if e.Bot != nil && (e.Bot.Bot.Key != a.Bot.Key || e.Bot.Bot.Project != a.Bot.Project) {
			return failure(pa.ScopeDenied, "export_bot_event")
		}
		if e.Read != nil && (e.Read.Key != a.Bot.Key || e.Read.ConversationID != a.Conversation.ID) {
			return failure(pa.ScopeDenied, "export_read_event")
		}
		if e.Source != nil && e.Source.MachineID != a.Bot.Key.MachineID {
			return failure(pa.ScopeDenied, "export_source_event")
		}
		if e.Source != nil {
			for _, c := range e.Source.Capabilities {
				if c.Key != a.Bot.Key || c.Project != a.Bot.Project {
					return failure(pa.ScopeDenied, "export_source_capability")
				}
			}
		}
		if e.Bot != nil {
			if e.Bot.ReadCursor.Key != (pa.BotKey{}) && e.Bot.ReadCursor.Key != a.Bot.Key || e.Bot.ReadCursor.ConversationID != "" && e.Bot.ReadCursor.ConversationID != a.Conversation.ID || e.Bot.Source.MachineID != "" && e.Bot.Source.MachineID != a.Bot.Key.MachineID {
				return failure(pa.ScopeDenied, "export_summary")
			}
			for _, c := range e.Bot.Source.Capabilities {
				if c.Key != a.Bot.Key || c.Project != a.Bot.Project {
					return failure(pa.ScopeDenied, "export_summary_capability")
				}
			}
		}
		if e.Trace != nil {
			turn, ok := a.Turns[e.Trace.TurnID]
			if !ok || turn.Fence != e.Fence {
				return failure(pa.ScopeDenied, "export_trace")
			}
		}
	}
	receiptIDs := map[string]bool{}
	commands := map[string]bool{}
	for _, r := range in.Receipts {
		if _, err := pa.DecideReplay(r.Command, nil); err != nil {
			return err
		}
		key := string(r.Command.Operation) + "/" + r.Command.IdempotencyKey
		if r.Command.Key != a.Bot.Key || r.ID == "" || receiptIDs[r.ID] || commands[key] || !validEffect(r.State) || r.EventSeq < 0 || r.EventSeq != 0 && !eventSeqs[r.EventSeq] {
			return failure(pa.Invalid, "export_receipt")
		}
		receiptIDs[r.ID] = true
		commands[key] = true
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_bots WHERE machine=? AND bot=?`, a.Bot.Key.MachineID, a.Bot.Key.BotID).Scan(&n); err != nil {
		return err
	}
	if n != 0 {
		return failure(pa.IdempotencyConflict, "restore_bot")
	}
	if err = persist(ctx, tx, a, 0); err != nil {
		return err
	}
	for _, e := range in.Events {
		raw, e2 := json.Marshal(e)
		if e2 != nil {
			return e2
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_events(seq,machine,bot,event_id,data) VALUES(?,?,?,?,?)`, e.Seq, a.Bot.Key.MachineID, a.Bot.Key.BotID, e.ID, raw); err != nil {
			return err
		}
	}
	for _, r := range in.Receipts {
		if err = writeReceipt(ctx, tx, r); err != nil {
			return err
		}
	}
	return tx.Commit()
}
