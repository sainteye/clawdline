package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ExecutionRecordsLimit bounds the machine's durable terminal identity index.
// Full is a typed refusal; an old identity is never evicted to make a new
// terminal appear to have the old execution's authority.
const ExecutionRecordsLimit = 4096

var (
	ErrExecutionsFull             = errors.New("execution_records_full")
	ErrExecutionSourceUnknown     = errors.New("execution_source_unknown")
	ErrExecutionTargetRequired    = errors.New("execution_target_required")
	ErrExecutionTargetMissing     = errors.New("execution_target_missing")
	ErrExecutionGenerationChanged = errors.New("execution_generation_changed")
)

type ExecutionSeen struct {
	ID, Source, Fingerprint string
}

const executionSchema = `CREATE TABLE IF NOT EXISTS session_executions (
 machine TEXT NOT NULL, terminal TEXT NOT NULL, source TEXT NOT NULL,
 fingerprint TEXT NOT NULL, generation TEXT NOT NULL, present INTEGER NOT NULL,
 updated_at INTEGER NOT NULL, PRIMARY KEY(machine, terminal));`

// ObserveExecutions commits one scan's identities and proven absences together.
// A failed write leaves the prior generation intact. The callback is the
// inventory's source-specific absence proof, including its unresolved gaps.
func (s *Store) ObserveExecutions(ctx context.Context, machine string, seen []ExecutionSeen,
	provesAbsence func(id, source string) bool) (map[string]string, error) {
	if machine == "" || provesAbsence == nil {
		return nil, ErrExecutionSourceUnknown
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, classify(err)
	}
	defer tx.Rollback()
	type prior struct {
		source, fingerprint, generation string
		present                         bool
	}
	known := map[string]prior{}
	rows, err := tx.QueryContext(ctx, `SELECT terminal, source, fingerprint, generation, present FROM session_executions WHERE machine=?`, machine)
	if err != nil {
		return nil, classify(err)
	}
	for rows.Next() {
		var id string
		var p prior
		if err = rows.Scan(&id, &p.source, &p.fingerprint, &p.generation, &p.present); err != nil {
			break
		}
		known[id] = p
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, classify(err)
	}
	result := map[string]string{}
	current := map[string]bool{}
	for _, item := range seen {
		current[item.ID] = true
	}
	// Once this scan proves absence, the old nonce has no authority left.
	// Removing the row also bounds long-lived machines that reuse terminal ids
	// over years; an uncertain absence keeps its row and cannot be evicted.
	for id, old := range known {
		if current[id] || !provesAbsence(id, old.source) {
			continue
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM session_executions WHERE machine=? AND terminal=?`, machine, id); err != nil {
			return nil, classify(err)
		}
		delete(known, id)
	}
	for _, item := range seen {
		if item.ID == "" || item.Source == "" || item.Fingerprint == "" || result[item.ID] != "" {
			return nil, ErrExecutionSourceUnknown
		}
		old, exists := known[item.ID]
		if !exists && len(known) >= ExecutionRecordsLimit {
			return nil, ErrExecutionsFull
		}
		if !exists {
			known[item.ID] = prior{}
		}
		generation := old.generation
		if !exists || !old.present || old.fingerprint != item.Fingerprint || old.source != item.Source {
			var nonce [16]byte
			if _, err = rand.Read(nonce[:]); err != nil {
				return nil, fmt.Errorf("execution nonce: %w", err)
			}
			generation = hex.EncodeToString(nonce[:])
		}
		if exists && old.present && old.fingerprint == item.Fingerprint && old.source == item.Source {
			result[item.ID] = generation
			continue
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO session_executions(machine,terminal,source,fingerprint,generation,present,updated_at)
			VALUES(?,?,?,?,?,1,?) ON CONFLICT(machine,terminal) DO UPDATE SET source=excluded.source,
			fingerprint=excluded.fingerprint,generation=excluded.generation,present=1,updated_at=excluded.updated_at`,
			machine, item.ID, item.Source, item.Fingerprint, generation, time.Now().Unix()); err != nil {
			return nil, classify(err)
		}
		result[item.ID] = generation
	}
	if err = tx.Commit(); err != nil {
		return nil, classify(err)
	}
	return result, nil
}

// AdmitExecution is the last machine-side check before a pinned read or write.
// It is intentionally a read of the durable index; the caller must first take
// a fresh authoritative terminal reading and reconcile it above.
func (s *Store) AdmitExecution(ctx context.Context, machine, id, expected string) error {
	if machine == "" || id == "" || expected == "" {
		return ErrExecutionTargetRequired
	}
	var got string
	var present bool
	err := s.rd.QueryRowContext(ctx, `SELECT generation,present FROM session_executions WHERE machine=? AND terminal=?`, machine, id).Scan(&got, &present)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrExecutionTargetMissing
	}
	if err != nil {
		return fmt.Errorf("execution_check_unavailable: %w", classify(err))
	}
	if !present {
		return ErrExecutionTargetMissing
	}
	if got != expected {
		return ErrExecutionGenerationChanged
	}
	return nil
}
