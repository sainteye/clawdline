package projectassistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// editTopicMemory consumes a literal replacement with an explicit index revision.
// Its command digest is derived from the requested edit, never a later snapshot.
func (r *Engine) editTopicMemory(ctx context.Context, key pa.BotKey, turnID, callID string, note MemoryNoteEdit) (pa.MemoryRevision, error) {
	if note.ExpectedRevision < 1 {
		return pa.MemoryRevision{}, runtimeFailure(pa.Invalid, "memory_revision")
	}
	op := pa.MemorySaveOperation
	if note.Forget {
		if note.EntryID == "" || note.Title != "" || note.Summary != "" || note.Body != "" {
			return pa.MemoryRevision{}, runtimeFailure(pa.Invalid, "memory_note_forget")
		}
		op = pa.ForgetOperation
	}
	command := r.command(key, op, "runtime-topic-memory-"+turnID+"-"+callID, note)
	prior, err := r.replay(ctx, command, op)
	if err != nil {
		return pa.MemoryRevision{}, err
	}
	if prior != nil {
		feed, ok := r.o.Repository.(EventFeed)
		if !ok || prior.EventSeq < 1 {
			return pa.MemoryRevision{}, runtimeFailure(pa.Unsupported, "memory_receipt")
		}
		page, e := feed.Read(ctx, pa.EventQuery{Key: key, AfterSeq: prior.EventSeq - 1, PageSize: 1})
		if e != nil {
			return pa.MemoryRevision{}, e
		}
		if len(page.Items) != 1 || page.Items[0].Seq != prior.EventSeq || page.Items[0].Kind != pa.MemoryChanged || page.Items[0].Memory == nil || page.Items[0].Memory.Key != key {
			return pa.MemoryRevision{}, runtimeFailure(pa.ContextStale, "memory_receipt")
		}
		return *page.Items[0].Memory, nil
	}
	index, err := r.MemoryIndex(ctx, key)
	if err != nil {
		return pa.MemoryRevision{}, err
	}
	if index.Revision != note.ExpectedRevision {
		return pa.MemoryRevision{}, runtimeFailure(pa.StaleVersion, "memory")
	}
	current, err := r.o.Memory.Current(ctx, key)
	if err != nil {
		return pa.MemoryRevision{}, err
	}
	if current.Revision != index.Revision {
		return pa.MemoryRevision{}, runtimeFailure(pa.StaleVersion, "memory")
	}
	id := note.EntryID
	var sources []pa.MemorySource
	if id == "" {
		sum := sha256.Sum256([]byte(turnID + "\x00" + callID))
		id = "note-" + hex.EncodeToString(sum[:])
	} else {
		found := false
		for _, entry := range index.Entries {
			if entry.ID == id && entry.Kind == pa.TopicMemory {
				sources = entry.Sources
				found = true
				break
			}
		}
		if !found {
			return pa.MemoryRevision{}, runtimeFailure(pa.NotFound, "memory_note")
		}
	}
	if note.Forget {
		// Match the human Forget path: invalidate the entry and its recorded
		// provenance so dependent summaries cannot retain the forgotten fact.
		ids := []string{id}
		for _, source := range sources {
			ids = append(ids, source.ID)
		}
		return r.o.Memory.Forget(ctx, ForgetRequest{Command: command, Project: current.Project, ExpectedRevision: note.ExpectedRevision, SourceIDs: ids})
	}
	entry := pa.MemoryEntry{ID: id, Key: key, Project: current.Project, Kind: pa.TopicMemory, Title: note.Title, Summary: note.Summary, Text: note.Body, Revision: note.ExpectedRevision}
	if err = pa.ValidateMemoryContent(entry); err != nil {
		return pa.MemoryRevision{}, err
	}
	own := pa.MemorySource{ID: "bot:" + id, Key: key, Project: current.Project, Kind: "bot", Revision: note.ExpectedRevision}
	for _, source := range sources {
		if source.ID != own.ID {
			entry.Sources = append(entry.Sources, source)
		}
	}
	entry.Sources = append(entry.Sources, own)
	return r.o.Memory.Save(ctx, MemoryWrite{Command: command, Entry: entry, ExpectedRevision: note.ExpectedRevision})
}
