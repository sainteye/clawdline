package projectassistantstore

import (
	"context"
	"database/sql"
	"encoding/json"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"strconv"
)

// ReadMemory extracts just memory JSON in one SQLite snapshot. LIMIT bounds
// rows and substr bounds previews; full entry bytes are fetched only by ID.
func (s *Store) ReadMemory(ctx context.Context, key pa.BotKey, id string) (pa.MemoryViewState, pa.MemoryLayer, string, error) {
	layer := pa.MemoryLayer{Layer: "bot", Cap: pa.MaxMemoryEntries, Available: true, Entries: []pa.MemoryRow{}}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return pa.MemoryViewState{}, layer, "", err
	}
	defer tx.Rollback()
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT json_object('key',json_extract(CAST(data AS TEXT),'$.Memory.key'),'project',json_extract(CAST(data AS TEXT),'$.Memory.project'),'revision',json_extract(CAST(data AS TEXT),'$.Memory.revision'),'forgotten_through',json_extract(CAST(data AS TEXT),'$.Memory.forgotten_through'),'forgotten_count',coalesce(json_array_length(CAST(data AS TEXT),'$.Memory.forgotten_source_ids'),0)) FROM project_assistant_bots WHERE machine=? AND bot=?`, key.MachineID, key.BotID).Scan(&raw)
	if err == sql.ErrNoRows {
		return pa.MemoryViewState{}, layer, "", failure(pa.NotFound, "bot")
	}
	if err != nil {
		return pa.MemoryViewState{}, layer, "", err
	}
	var revision pa.MemoryViewState
	if err = json.Unmarshal(raw, &revision); err != nil {
		return revision, layer, "", err
	}
	layer.Revision = strconv.FormatInt(revision.Revision, 10)
	const entries = ` FROM project_assistant_bots b, json_each(CAST(b.data AS TEXT),'$.Entries') e WHERE b.machine=? AND b.bot=? AND NOT json_extract(e.value,'$.forgotten')`
	err = tx.QueryRowContext(ctx, `SELECT count(*)`+entries, key.MachineID, key.BotID).Scan(&layer.Used)
	if err != nil {
		return revision, layer, "", err
	}
	layer.Truncated = layer.Used > pa.MaxMemoryEntries
	text := ""
	if id != "" {
		var size int
		err = tx.QueryRowContext(ctx, `SELECT length(CAST(json_extract(e.value,'$.text') AS BLOB)),substr(json_extract(e.value,'$.text'),1,?)`+entries+` AND json_extract(e.value,'$.id')=?`, pa.MaxMessageBytes, key.MachineID, key.BotID, id).Scan(&size, &text)
		if err == nil && size > pa.MaxMessageBytes {
			err = failure(pa.CapacityFull, "memory_detail_bytes")
		}
		if err == sql.ErrNoRows {
			err = failure(pa.NotFound, "memory_entry")
		}
		return revision, layer, text, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT json_extract(e.value,'$.id'),substr(json_extract(e.value,'$.text'),1,?),json_extract(e.value,'$.sources'),coalesce(json_extract(e.value,'$.updated_at'),'')`+entries+` ORDER BY json_extract(e.value,'$.revision'),e.key LIMIT ?`, pa.MaxMemoryPreviewRunes, key.MachineID, key.BotID, pa.MaxMemoryEntries)
	if err != nil {
		return revision, layer, "", err
	}
	defer rows.Close()
	for rows.Next() {
		var row pa.MemoryRow
		var sources []byte
		if err = rows.Scan(&row.ID, &row.Text, &sources, &row.UpdatedAt); err != nil {
			return revision, layer, "", err
		}
		row.Sources = []pa.MemorySource{}
		if len(sources) > 0 {
			if err = json.Unmarshal(sources, &row.Sources); err != nil {
				return revision, layer, "", err
			}
		}
		if len(row.Sources) > 0 {
			row.Author = row.Sources[len(row.Sources)-1].Kind
		}
		layer.Entries = append(layer.Entries, row)
	}
	return revision, layer, "", rows.Err()
}

// MemoryBot reads only the current Bot header, leaving its history in SQLite.
func (s *Store) MemoryBot(ctx context.Context, key pa.BotKey) (pa.Bot, error) {
	var bytes []byte
	err := s.db.QueryRowContext(ctx, `SELECT json_extract(CAST(data AS TEXT),'$.Bot') FROM project_assistant_bots WHERE machine=? AND bot=?`, key.MachineID, key.BotID).Scan(&bytes)
	if err == sql.ErrNoRows {
		return pa.Bot{}, failure(pa.NotFound, "bot")
	}
	if err != nil {
		return pa.Bot{}, err
	}
	var bot pa.Bot
	err = json.Unmarshal(bytes, &bot)
	return bot, err
}
