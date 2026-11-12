package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/domain/voicework"
)

const voiceWorkSchema = `CREATE TABLE IF NOT EXISTS voice_assistant_workspace (id INTEGER PRIMARY KEY CHECK (id=1), body BLOB NOT NULL);`

func voiceWorkState(raw []byte) (voicework.State, error) {
	if len(raw) > voicework.MaxStateBytes {
		return voicework.State{}, voicework.ErrFull
	}
	var state voicework.State
	if err := json.Unmarshal(raw, &state); err != nil {
		return state, err
	}
	if state.ID == "" || state.Revision < 1 {
		return state, voicework.ErrInvalid
	}
	return state, nil
}

func (s *Store) VoiceWork(ctx context.Context) (voicework.State, error) {
	var raw []byte
	err := s.rd.QueryRowContext(ctx, `SELECT body FROM voice_assistant_workspace WHERE id=1`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return voicework.New(""), nil
	}
	if err != nil {
		return voicework.State{}, err
	}
	return voiceWorkState(raw)
}

func (s *Store) UpdateVoiceWork(ctx context.Context, change func(*voicework.State) error) (voicework.State, error) {
	var state voicework.State
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		var raw []byte
		err := tx.QueryRowContext(ctx, `SELECT body FROM voice_assistant_workspace WHERE id=1`).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			state = voicework.New(uuid.NewString())
		} else if err != nil {
			return 0, err
		} else {
			state, err = voiceWorkState(raw)
			if err != nil {
				return 0, err
			}
		}
		if err := change(&state); err != nil {
			return 0, err
		}
		raw, err = json.Marshal(state)
		if err != nil {
			return 0, err
		}
		if len(raw) > voicework.MaxStateBytes {
			return 0, voicework.ErrFull
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO voice_assistant_workspace(id,body) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body`, raw)
		return 1, err
	})
	return state, err
}
