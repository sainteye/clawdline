package projectassistantstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"

	bh "github.com/sainteye/clawdline/internal/domain/botroutinehooks"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// CloudHookCandidate is private source authority, never part of an export or view.
type CloudHookCandidate struct {
	Routine pa.Routine
	Hook    bh.Hook
}

func checkCloudHookSchema(ctx context.Context, tx *sql.Tx) error {
	var exists, version int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='project_assistant_cloud_hook_schema'`).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	if err := tx.QueryRowContext(ctx, `SELECT version FROM project_assistant_cloud_hook_schema WHERE id=1`).Scan(&version); err != nil {
		return err
	}
	if version != 1 {
		return bh.Fail("webhook_schema_unsupported")
	}
	return nil
}

// CloudHooksInstalled performs no schema or feature activation.
func (s *Store) CloudHooksInstalled(ctx context.Context) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='project_assistant_cloud_hook_schema'`).Scan(&count)
	if err != nil || count == 0 {
		return false, err
	}
	var version int
	err = s.db.QueryRowContext(ctx, `SELECT version FROM project_assistant_cloud_hook_schema WHERE id=1`).Scan(&version)
	if err == nil && version != 1 {
		err = bh.Fail("webhook_schema_unsupported")
	}
	return err == nil, err
}

func (s *Store) CloudHookCandidates(ctx context.Context, machine string) ([]CloudHookCandidate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.data,h.secret,b.data FROM project_assistant_routines r JOIN project_assistant_routine_hooks h USING(machine,bot,id) JOIN project_assistant_bots b USING(machine,bot) WHERE r.machine=? AND r.deleted=0 ORDER BY r.id LIMIT ?`, machine, bh.MaxHooks+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CloudHookCandidate{}
	for rows.Next() {
		var raw, bot []byte
		var secret string
		var d pa.Routine
		var a aggregate
		if err = rows.Scan(&raw, &secret, &bot); err != nil {
			return nil, err
		}
		if json.Unmarshal(raw, &d) != nil || json.Unmarshal(bot, &a) != nil {
			return nil, bh.Fail("webhook_storage_unknown")
		}
		if d.Deleted || !d.HasHook() || d.Key != a.Bot.Key || d.Project != a.Bot.Project || d.ConversationID != a.Conversation.ID {
			return nil, bh.Fail("webhook_authority_changed")
		}
		out = append(out, CloudHookCandidate{Routine: d, Hook: bh.Hook{ID: d.ID, Key: secret, Enabled: d.Enabled}})
		if len(out) > bh.MaxHooks {
			return nil, bh.Fail("webhook_hooks_capacity")
		}
	}
	return out, rows.Err()
}

// PrepareCloudHooks durably reserves a canonical snapshot's revision before IO.
// Lost responses reuse the same revision; every changed snapshot advances it.
// Only a digest is retained here; hook keys remain in their existing private table.
func (s *Store) PrepareCloudHooks(ctx context.Context, id bh.Identity, hooks []bh.Hook) (bh.Enrollment, string, error) {
	set := bh.Enrollment{Protocol: bh.Protocol}
	canonical, err := bh.Canonical(hooks)
	if err != nil {
		return set, "", err
	}
	raw, err := json.Marshal(canonical)
	if err != nil || len(raw) > bh.MaxEnrollmentBytes {
		return set, "", bh.Fail("webhook_too_large")
	}
	sum := sha256.Sum256(raw)
	fingerprint := hex.EncodeToString(sum[:])
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return set, "", err
	}
	defer tx.Rollback()
	var revision int64
	var previous string
	err = tx.QueryRowContext(ctx, `SELECT revision,fingerprint FROM project_assistant_cloud_hooks WHERE account=? AND machine=?`, id.AccountID, id.MachineID).Scan(&revision, &previous)
	if err != nil && err != sql.ErrNoRows {
		return set, "", err
	}
	if err == sql.ErrNoRows && len(canonical) == 0 {
		return set, "", nil
	}
	if previous != fingerprint {
		if revision >= bh.MaxRevision {
			return set, "", bh.Fail("webhook_revision_conflict")
		}
		revision++
		_, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_cloud_hooks(account,machine,revision,fingerprint) VALUES(?,?,?,?) ON CONFLICT(account,machine) DO UPDATE SET revision=excluded.revision,fingerprint=excluded.fingerprint`, id.AccountID, id.MachineID, revision, fingerprint)
		if err != nil {
			return set, "", err
		}
	}
	set.Revision, set.Hooks = revision, canonical
	if err = tx.Commit(); err != nil {
		return bh.Enrollment{}, "", err
	}
	return set, fingerprint, nil
}

// CloudHookPending is a single durable forwarding/ack barrier per namespace.
// A crash with no actual HTTP result never authorizes another local effect.
type CloudHookPending struct {
	ID  string  `json:"id"`
	Ack *bh.Ack `json:"ack,omitempty"`
}

func (s *Store) CloudHookPending(ctx context.Context, id bh.Identity) (*CloudHookPending, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT data FROM project_assistant_cloud_hook_pending WHERE account=? AND machine=?`, id.AccountID, id.MachineID).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out CloudHookPending
	if json.Unmarshal(raw, &out) != nil || out.ID == "" {
		return nil, bh.Fail("webhook_storage_unknown")
	}
	return &out, nil
}
func (s *Store) BeginCloudHook(ctx context.Context, id bh.Identity, delivery string) error {
	raw, _ := json.Marshal(CloudHookPending{ID: delivery})
	result, err := s.db.ExecContext(ctx, `INSERT INTO project_assistant_cloud_hook_pending(account,machine,data) SELECT ?,?,? WHERE (SELECT count(*) FROM project_assistant_cloud_hook_pending WHERE account=? AND machine=?) < ?`, id.AccountID, id.MachineID, raw, id.AccountID, id.MachineID, bh.MaxPending)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return bh.Fail("webhook_outcome_unknown")
	}
	return nil
}
func (s *Store) FinishCloudHook(ctx context.Context, id bh.Identity, delivery string, ack bh.Ack) error {
	raw, _ := json.Marshal(CloudHookPending{ID: delivery, Ack: &ack})
	result, err := s.db.ExecContext(ctx, `UPDATE project_assistant_cloud_hook_pending SET data=? WHERE account=? AND machine=? AND json_extract(CAST(data AS TEXT),'$.id')=?`, raw, id.AccountID, id.MachineID, delivery)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return bh.Fail("webhook_storage_unknown")
	}
	return nil
}
func (s *Store) ClearCloudHook(ctx context.Context, id bh.Identity, delivery string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM project_assistant_cloud_hook_pending WHERE account=? AND machine=? AND json_extract(CAST(data AS TEXT),'$.id')=?`, id.AccountID, id.MachineID, delivery)
	return err
}
