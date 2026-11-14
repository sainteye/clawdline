package projectassistantstore

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func (s *Store) Current(ctx context.Context, key pa.BotKey) (pa.MemoryRevision, error) {
	a, err := load(ctx, s.db, key)
	return a.Memory, err
}
func snapshot(a aggregate) pa.ContextSnapshot {
	out := pa.ContextSnapshot{Memory: a.Memory, ProvenanceComplete: true, Sources: []pa.MemorySource{}}
	ids := make([]string, 0, len(a.Entries))
	for id := range a.Entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	texts := []string{}
	seen := map[string]pa.MemorySource{}
	for _, id := range ids {
		e := a.Entries[id]
		if e.Forgotten {
			continue
		}
		if pa.ValidateMemoryWrite(pa.MemoryEntry{ID: e.ID, Key: e.Key, Project: e.Project, Text: e.Text, Sources: e.Sources, Revision: a.Memory.Revision}, a.Memory, a.Memory.Revision) != nil {
			out.ProvenanceComplete = false
			continue
		}
		texts = append(texts, e.Text)
		for _, source := range e.Sources {
			if old, ok := seen[source.ID]; ok && old != source {
				out.ProvenanceComplete = false
			}
			seen[source.ID] = source
		}
	}
	out.Text = strings.Join(texts, "\n")
	sourceIDs := make([]string, 0, len(seen))
	for id := range seen {
		sourceIDs = append(sourceIDs, id)
	}
	sort.Strings(sourceIDs)
	for _, id := range sourceIDs {
		out.Sources = append(out.Sources, seen[id])
	}
	return out
}
func (s *Store) Snapshot(ctx context.Context, key pa.BotKey) (pa.ContextSnapshot, error) {
	a, err := load(ctx, s.db, key)
	if err != nil {
		return pa.ContextSnapshot{}, err
	}
	out := snapshot(a)
	return out, pa.CheckContext(out, a.Memory)
}
func (s *Store) WithCurrent(ctx context.Context, key pa.BotKey, revision int64, callback func(pa.ContextSnapshot) error) error {
	if callback == nil {
		return failure(pa.Invalid, "callback")
	}
	unlock := s.lock(key)
	defer unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	a, err := load(ctx, s.db, key)
	if err != nil {
		return err
	}
	if a.Memory.Revision != revision {
		return failure(pa.ContextStale, "memory_revision")
	}
	out := snapshot(a)
	if err = pa.CheckContext(out, a.Memory); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return callback(out)
}
func (s *Store) Save(ctx context.Context, in app.MemoryWrite) (pa.MemoryRevision, error) {
	if in.Command.Operation != pa.MemorySaveOperation || in.Command.Key != in.Entry.Key {
		return pa.MemoryRevision{}, failure(pa.ScopeDenied, "memory_command")
	}
	unlock := s.lock(in.Command.Key)
	defer unlock()
	return s.changeMemory(ctx, in.Command, func(a *aggregate) error {
		if err := pa.ValidateMemoryWrite(in.Entry, a.Memory, in.ExpectedRevision); err != nil {
			return err
		}
		old, exists := a.Entries[in.Entry.ID]
		if exists && old.Forgotten {
			return failure(pa.ContextStale, "forgotten_entry")
		}
		if !exists {
			live := 0
			for _, e := range a.Entries {
				if !e.Forgotten {
					live++
				}
			}
			if live >= pa.MaxMemoryEntries {
				return failure(pa.CapacityFull, "memory_entries")
			}
		}
		a.Memory.Revision++
		entry := in.Entry
		entry.Revision = a.Memory.Revision
		a.Entries[entry.ID] = entry
		return nil
	})
}
func (s *Store) Forget(ctx context.Context, in app.ForgetRequest) (pa.MemoryRevision, error) {
	if in.Command.Operation != pa.ForgetOperation {
		return pa.MemoryRevision{}, failure(pa.Invalid, "forget_command")
	}
	unlock := s.lock(in.Command.Key)
	defer unlock()
	return s.changeMemory(ctx, in.Command, func(a *aggregate) error {
		if in.Project != a.Bot.Project {
			return failure(pa.ScopeDenied, "forget_project")
		}
		next, err := pa.Forget(a.Memory, in.ExpectedRevision, in.SourceIDs)
		if err != nil {
			return err
		}
		a.Memory = next
		for id, e := range a.Entries {
			if contains(next.ForgottenSourceIDs, id) {
				e.Forgotten = true
			}
			for _, source := range e.Sources {
				if contains(next.ForgottenSourceIDs, source.ID) {
					e.Forgotten = true
				}
			}
			a.Entries[id] = e
		}
		return nil
	})
}
func contains(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}
func (s *Store) changeMemory(ctx context.Context, cmd pa.Command, change func(*aggregate) error) (pa.MemoryRevision, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return pa.MemoryRevision{}, err
	}
	defer tx.Rollback()
	prior, err := receipt(ctx, tx, cmd)
	if err != nil {
		return pa.MemoryRevision{}, err
	}
	a, err := load(ctx, tx, cmd.Key)
	if err != nil {
		return pa.MemoryRevision{}, err
	}
	if prior != nil {
		// Return the original result even after later saves/forgets. Its event is
		// immutable and carries the complete revision/tombstone set.
		var raw []byte
		if err = tx.QueryRowContext(ctx, `SELECT data FROM project_assistant_events WHERE seq=? AND machine=? AND bot=?`, prior.EventSeq, cmd.Key.MachineID, cmd.Key.BotID).Scan(&raw); err != nil {
			return pa.MemoryRevision{}, err
		}
		var event pa.Event
		if err = json.Unmarshal(raw, &event); err != nil {
			return pa.MemoryRevision{}, err
		}
		if event.Memory == nil {
			return pa.MemoryRevision{}, failure(pa.Invalid, "memory_receipt")
		}
		return *event.Memory, nil
	}
	old := a.Memory
	if err = change(&a); err != nil {
		return old, err
	}
	if reflect.DeepEqual(old, a.Memory) {
		return old, failure(pa.Invalid, "memory_change")
	}
	expected := a.Bot.Version
	a.Bot.Version++
	if err = validateAggregate(a); err != nil {
		return old, err
	}
	f := pa.Fence{Key: a.Bot.Key, Project: a.Bot.Project, ConversationID: a.Conversation.ID, BindingID: a.Conversation.BindingID, Generation: a.Conversation.Generation}
	seq, err := appendEvent(ctx, tx, a, pa.Event{Version: pa.ContractVersion, ID: string(cmd.Operation) + "/" + cmd.IdempotencyKey, Fence: f, Kind: pa.MemoryChanged, Memory: &a.Memory, At: time.Now().UTC()})
	if err != nil {
		return old, err
	}
	if err = persist(ctx, tx, a, expected); err != nil {
		return old, err
	}
	r := pa.Receipt{Command: cmd, ID: string(cmd.Operation) + "/" + cmd.IdempotencyKey, State: pa.EffectDelivered, ResourceID: a.Bot.Key.BotID, EventSeq: seq}
	if err = writeReceipt(ctx, tx, r); err != nil {
		return old, err
	}
	return a.Memory, tx.Commit()
}
