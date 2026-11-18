package projectassistantstore

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// MemoryEntries projects facts and note indexes in one snapshot. Chat, tool
// traces and note bodies are not transferred into the native memory index.
func (s *Store) MemoryEntries(ctx context.Context, key pa.BotKey) ([]pa.MemoryEntry, error) {
	index, err := s.MemoryIndex(ctx, key)
	return index.Entries, err
}

// MemoryIndex binds the revision and entries to the same SQLite snapshot.
func (s *Store) MemoryIndex(ctx context.Context, key pa.BotKey) (app.MemoryIndex, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return app.MemoryIndex{}, err
	}
	defer tx.Rollback()
	var memoryBytes, botBytes []byte
	err = tx.QueryRowContext(ctx, `SELECT json_extract(CAST(data AS TEXT),'$.Memory'),json_extract(CAST(data AS TEXT),'$.Bot') FROM project_assistant_bots WHERE machine=? AND bot=?`, key.MachineID, key.BotID).Scan(&memoryBytes, &botBytes)
	if err == sql.ErrNoRows {
		return app.MemoryIndex{}, failure(pa.NotFound, "bot")
	}
	if err != nil {
		return app.MemoryIndex{}, err
	}
	var memory pa.MemoryRevision
	var bot pa.Bot
	if err = json.Unmarshal(memoryBytes, &memory); err != nil {
		return app.MemoryIndex{}, err
	}
	if err = json.Unmarshal(botBytes, &bot); err != nil {
		return app.MemoryIndex{}, err
	}
	if memory.Key != key || bot.Key != key || memory.Project != bot.Project {
		return app.MemoryIndex{}, failure(pa.ScopeDenied, "memory_index")
	}
	const from = ` FROM project_assistant_bots b,json_each(CAST(b.data AS TEXT),'$.Entries') e WHERE b.machine=? AND b.bot=? AND NOT json_extract(e.value,'$.forgotten')`
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*)`+from, key.MachineID, key.BotID).Scan(&count); err != nil {
		return app.MemoryIndex{}, err
	}
	if count > pa.MaxMemoryEntries {
		return app.MemoryIndex{}, failure(pa.CapacityFull, "memory_entries")
	}
	rows, err := tx.QueryContext(ctx, `WITH live AS (SELECT json_set(e.value,'$.text',CASE WHEN json_extract(e.value,'$.kind')='note' THEN substr(json_extract(e.value,'$.summary'),1,?) ELSE json_extract(e.value,'$.text') END,'$.summary',CASE WHEN json_extract(e.value,'$.kind')='note' THEN substr(json_extract(e.value,'$.summary'),1,?) ELSE '' END) AS entry`+from+` ORDER BY json_extract(e.value,'$.revision'),json_extract(e.value,'$.id') LIMIT ?) SELECT CASE WHEN length(CAST(entry AS BLOB))<=? THEN entry ELSE NULL END FROM live`, pa.MaxMemoryPreviewRunes, pa.MaxMemoryPreviewRunes, key.MachineID, key.BotID, pa.MaxMemoryEntries, cloudops.ReadPlaintextBytesLimit)
	if err != nil {
		return app.MemoryIndex{}, err
	}
	defer rows.Close()
	out := []pa.MemoryEntry{}
	bytes := 0
	for rows.Next() {
		var raw sql.NullString
		if err = rows.Scan(&raw); err != nil {
			return app.MemoryIndex{}, err
		}
		bytes += len(raw.String)
		if !raw.Valid || bytes > cloudops.ReadPlaintextBytesLimit {
			return app.MemoryIndex{}, failure(pa.CapacityFull, "memory_read_bytes")
		}
		var entry pa.MemoryEntry
		if err = json.Unmarshal([]byte(raw.String), &entry); err != nil {
			return app.MemoryIndex{}, err
		}
		if entry.Key != key || entry.Project != memory.Project || entry.Revision < 1 || entry.Revision > memory.Revision {
			return app.MemoryIndex{}, failure(pa.ScopeDenied, "memory_entry")
		}
		if err = pa.ValidateMemoryContent(entry); err != nil {
			return app.MemoryIndex{}, err
		}
		if err = pa.CheckContext(pa.ContextSnapshot{Memory: memory, Text: entry.Text, Sources: entry.Sources, ProvenanceComplete: true}, memory); err != nil {
			return app.MemoryIndex{}, err
		}
		out = append(out, entry)
	}
	return app.MemoryIndex{Revision: memory.Revision, Entries: out}, rows.Err()
}
