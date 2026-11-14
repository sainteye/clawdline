package projectassistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// MemoryEdit is one change a Bot asks for through its edit_memory tool, as in
// Codync (host/src/chat/memory/file_tools.rs): Append adds an entry; Old and
// New replace text that appears exactly once in the live entries; Delete with
// Old forgets the entry holding it. Clawdline keeps entries, not files, so a
// line of Codync's MEMORY.md is one entry here.
type MemoryEdit struct {
	Append string
	Old    string
	New    string
	Delete bool
}

// MemoryEntries returns a Bot's live entries in a stable order, oldest
// revision first, for its read_memory and search_memory tools.
func (r *Engine) MemoryEntries(ctx context.Context, key pa.BotKey) ([]pa.MemoryEntry, error) {
	x, err := r.o.Repository.Export(ctx, key)
	if err != nil {
		return nil, err
	}
	out := make([]pa.MemoryEntry, 0, len(x.MemoryEntries))
	for _, e := range x.MemoryEntries {
		if !e.Forgotten {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Revision != out[j].Revision {
			return out[i].Revision < out[j].Revision
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// SearchMemory returns the live entries holding every word of query, ignoring
// case. Past pa.MaxMemorySearchResults it returns that many and more=true, so
// the Bot narrows its query rather than reading a silently partial answer.
func SearchMemory(entries []pa.MemoryEntry, query string) (out []pa.MemoryEntry, more bool) {
	words := strings.Fields(strings.ToLower(query))
	out = []pa.MemoryEntry{}
	for _, e := range entries {
		text := strings.ToLower(e.Text)
		all := len(words) > 0
		for _, w := range words {
			all = all && strings.Contains(text, w)
		}
		if all {
			if len(out) == pa.MaxMemorySearchResults {
				return out, true
			}
			out = append(out, e)
		}
	}
	return out, false
}

// EditMemory applies a Bot's edit for its current turn. The caller has
// already checked that turnID is the Bot's active turn under the current
// fence; callID makes a repeated tool call the same change.
func (r *Engine) EditMemory(ctx context.Context, key pa.BotKey, turnID, callID string, edit MemoryEdit) (pa.MemoryRevision, error) {
	if turnID == "" || callID == "" {
		return pa.MemoryRevision{}, runtimeFailure(pa.Invalid, "memory_call")
	}
	bot, err := r.o.Repository.Bot(ctx, key)
	if err != nil {
		return pa.MemoryRevision{}, err
	}
	current, err := r.o.Memory.Current(ctx, key)
	if err != nil {
		return pa.MemoryRevision{}, err
	}
	entries, err := r.MemoryEntries(ctx, key)
	if err != nil {
		return pa.MemoryRevision{}, err
	}
	id := "runtime-memory-" + turnID + "-" + callID
	if edit.Append != "" {
		if edit.Old != "" || edit.New != "" || edit.Delete {
			return pa.MemoryRevision{}, runtimeFailure(pa.Invalid, "memory_edit")
		}
		text := strings.TrimSpace(edit.Append)
		sum := sha256.Sum256([]byte(id))
		entry := pa.MemoryEntry{ID: "bot-" + hex.EncodeToString(sum[:8]), Key: key, Project: bot.Project, Text: text}
		return r.saveBotMemory(ctx, id, entry, nil, current)
	}
	if edit.Old == "" {
		return pa.MemoryRevision{}, runtimeFailure(pa.Invalid, "memory_edit")
	}
	var target *pa.MemoryEntry
	count := 0
	for i := range entries {
		if n := strings.Count(entries[i].Text, edit.Old); n > 0 {
			count += n
			target = &entries[i]
		}
	}
	switch {
	case count == 0:
		return pa.MemoryRevision{}, runtimeFailure(pa.NotFound, "memory_old")
	case count > 1:
		return pa.MemoryRevision{}, runtimeFailure(pa.Invalid, "memory_old_repeated")
	}
	text := strings.TrimSpace(strings.Replace(target.Text, edit.Old, edit.New, 1))
	if edit.Delete || text == "" {
		if edit.Delete && edit.New != "" {
			return pa.MemoryRevision{}, runtimeFailure(pa.Invalid, "memory_edit")
		}
		cmd := r.command(key, pa.ForgetOperation, id, struct{ Entry string }{target.ID})
		return r.o.Memory.Forget(ctx, ForgetRequest{Command: cmd, Project: bot.Project, ExpectedRevision: current.Revision, SourceIDs: []string{target.ID}})
	}
	entry := *target
	entry.Text = text
	return r.saveBotMemory(ctx, id, entry, target.Sources, current)
}

func (r *Engine) saveBotMemory(ctx context.Context, id string, entry pa.MemoryEntry, sources []pa.MemorySource, current pa.MemoryRevision) (pa.MemoryRevision, error) {
	if err := pa.ValidateMessage(entry.Text); err != nil {
		return pa.MemoryRevision{}, err
	}
	// The entry keeps the sources it had, so forgetting what it came from
	// still forgets it, and gains the Bot's own as its latest author.
	own := pa.MemorySource{ID: "bot:" + entry.ID, Key: entry.Key, Project: entry.Project, Kind: "bot", Revision: current.Revision}
	entry.Sources = nil
	for _, s := range sources {
		if s.ID != own.ID {
			entry.Sources = append(entry.Sources, s)
		}
	}
	entry.Sources = append(entry.Sources, own)
	entry.Revision = current.Revision
	entry.Forgotten = false
	cmd := r.command(entry.Key, pa.MemorySaveOperation, id, struct {
		Entry            pa.MemoryEntry
		ExpectedRevision int64
	}{entry, current.Revision})
	return r.o.Memory.Save(ctx, MemoryWrite{Command: cmd, Entry: entry, ExpectedRevision: current.Revision})
}
