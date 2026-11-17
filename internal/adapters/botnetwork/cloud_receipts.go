package botnetwork

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	app "github.com/sainteye/clawdline/internal/app/botnetwork"
	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type CloudReceipt struct {
	Header       wire.BotHeader `json:"header"`
	HeaderDigest string         `json:"header_digest"`
	FrameDigest  string         `json:"frame_digest"`
	DeliveryID   string         `json:"delivery_id"`
	Connection   string         `json:"connection"`
	Status       string         `json:"status"`
}

func (s *Store) InstallCloudReceipts(ctx context.Context) error {
	_, e := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS project_assistant_network_cloud_receipts(machine TEXT NOT NULL,direction TEXT NOT NULL,id TEXT NOT NULL,data BLOB NOT NULL,PRIMARY KEY(machine,direction,id))`)
	return e
}

// LeaseRevision survives ordinary source restarts. A pending API write is
// reconciled by an authenticated metadata read while the remote lease is live;
// if that evidence expires, the source stays unavailable instead of guessing CAS.
func (s *Store) LeaseRevision(ctx context.Context) (int64, bool, error) {
	if _, e := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS project_assistant_network_cloud_lease(machine TEXT PRIMARY KEY,revision INTEGER NOT NULL,pending INTEGER NOT NULL)`); e != nil {
		return 0, false, e
	}
	var revision int64
	var pending bool
	e := s.db.QueryRowContext(ctx, `SELECT revision,pending FROM project_assistant_network_cloud_lease WHERE machine=?`, s.machine).Scan(&revision, &pending)
	if errors.Is(e, sql.ErrNoRows) {
		e = nil
	}
	return revision, pending, e
}
func (s *Store) SaveLeaseRevision(ctx context.Context, revision int64, pending bool) error {
	_, e := s.db.ExecContext(ctx, `INSERT INTO project_assistant_network_cloud_lease(machine,revision,pending) VALUES(?,?,?) ON CONFLICT(machine) DO UPDATE SET revision=excluded.revision,pending=excluded.pending`, s.machine, revision, pending)
	return e
}

// RetainCloud writes exact routing/ciphertext correlation before socket send or
// target ACK. Retained unknown rows are never evicted to allow another effect.
func (s *Store) RetainCloud(ctx context.Context, direction string, r CloudReceipt) (bool, error) {
	if direction != "send" && direction != "receive" {
		return false, refuse(pa.Invalid, "bot_cloud_receipt")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	var raw []byte
	e = tx.QueryRowContext(ctx, `SELECT data FROM project_assistant_network_cloud_receipts WHERE machine=? AND direction=? AND id=?`, s.machine, direction, r.Header.RequestID).Scan(&raw)
	if e == nil {
		var old CloudReceipt
		if json.Unmarshal(raw, &old) != nil {
			return false, refuse(pa.OutcomeUnknown, "bot_cloud_receipt")
		}
		if old.Header != r.Header || old.HeaderDigest != r.HeaderDigest || old.FrameDigest != r.FrameDigest || old.DeliveryID != r.DeliveryID || old.Connection != r.Connection {
			return false, refuse(pa.IdempotencyConflict, "bot_cloud_receipt")
		}
		return false, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return false, e
	}
	var n int
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_network_cloud_receipts WHERE machine=?`, s.machine).Scan(&n); e != nil {
		return false, e
	}
	if n >= pa.MaxNetworkCloudReceipts {
		return false, refuse(pa.CapacityFull, "bot_cloud_receipts")
	}
	raw, _ = json.Marshal(r)
	if _, e = tx.ExecContext(ctx, `INSERT INTO project_assistant_network_cloud_receipts(machine,direction,id,data) VALUES(?,?,?,?)`, s.machine, direction, r.Header.RequestID, raw); e != nil {
		return false, e
	}
	return true, tx.Commit()
}
func (s *Store) RecordCloudStatus(ctx context.Context, id, header, frame, target, status string) error {
	if status != "relay_accepted" && status != "target_delivered" {
		return refuse(pa.Invalid, "bot_cloud_status")
	}
	r, e := s.db.ExecContext(ctx, `UPDATE project_assistant_network_cloud_receipts SET data=json_set(data,'$.status',CASE WHEN json_extract(data,'$.status')='target_delivered' THEN 'target_delivered' ELSE ? END) WHERE machine=? AND direction='send' AND id=? AND json_extract(data,'$.header_digest')=? AND json_extract(data,'$.frame_digest')=? AND json_extract(data,'$.header.target.machine_id')=?`, status, s.machine, id, header, frame, target)
	if e != nil {
		return e
	}
	n, e := r.RowsAffected()
	if e == nil && n != 1 {
		return refuse(pa.IdempotencyConflict, "bot_cloud_status")
	}
	return e
}
func canonical(v any) []byte {
	b, _ := json.Marshal(v)
	// All callers use closed structs whose numeric fields are safe integers.
	return cloudCanonical(b)
}
func CloudDigests(e wire.BotEnvelope) (string, string) {
	return app.Digest(canonical(e.Header)), app.Digest(canonical(e))
}
