// Package botnetwork implements the isolated durable Bot network namespace.
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

type Store struct {
	db      *sql.DB
	machine string
}

func NewStore(ctx context.Context, db *sql.DB, machine string) (*Store, error) {
	if db == nil || machine == "" {
		return nil, refuse(pa.Invalid, "network_store")
	}
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS project_assistant_network_grants(machine TEXT NOT NULL,id TEXT NOT NULL,data BLOB NOT NULL,PRIMARY KEY(machine,id))`,
		`CREATE TABLE IF NOT EXISTS project_assistant_network_receipts(machine TEXT NOT NULL,actor TEXT NOT NULL,id TEXT NOT NULL,digest TEXT NOT NULL,data BLOB NOT NULL,PRIMARY KEY(machine,actor,id))`,
	} {
		if _, e := db.ExecContext(ctx, ddl); e != nil {
			return nil, e
		}
	}
	return &Store{db: db, machine: machine}, nil
}
func refuse(code pa.ErrorCode, field string) error { return &pa.Failure{Code: code, Field: field} }

// PutGrant is a local management port, absent from the network dispatcher and
// native tools. The composition must authenticate the human before calling it.
func (s *Store) PutGrant(ctx context.Context, g app.Grant) error {
	if g.ID == "" || g.Epoch < 1 || g.AccountID == "" || g.Source.AccountID != g.AccountID || g.Target.AccountID != g.AccountID {
		return refuse(pa.Invalid, "network_grant")
	}
	if e := wire.VerifyTranscript(g.Pair, s.machine, g.ComparedFingerprint); e != nil {
		return refuse(pa.Unpaired, "network_pair")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var prior app.Grant
	var data []byte
	e = tx.QueryRowContext(ctx, `SELECT data FROM project_assistant_network_grants WHERE machine=? AND id=?`, s.machine, g.ID).Scan(&data)
	if e == nil {
		e = json.Unmarshal(data, &prior)
		if e != nil {
			return e
		}
	}
	if e == nil {
		if prior.AccountID != g.AccountID || prior.Source.AccountID != g.Source.AccountID || prior.Source.MachineID != g.Source.MachineID || prior.Source.ProjectID != g.Source.ProjectID || prior.Source.BotID != g.Source.BotID || prior.Source.ConversationID != g.Source.ConversationID || prior.Target.AccountID != g.Target.AccountID || prior.Target.MachineID != g.Target.MachineID || prior.Target.ProjectID != g.Target.ProjectID || prior.Target.BotID != g.Target.BotID || prior.Target.ConversationID != g.Target.ConversationID || prior.Pair != g.Pair || g.Epoch != prior.Epoch+1 {
			return refuse(pa.StaleVersion, "network_grant")
		}
	} else if errors.Is(e, sql.ErrNoRows) {
		var n int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_network_grants WHERE machine=?`, s.machine).Scan(&n); e != nil {
			return e
		}
		if n >= pa.MaxNetworkGrants {
			return refuse(pa.CapacityFull, "network_grants")
		}
	} else {
		return e
	}
	b, e := json.Marshal(g)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO project_assistant_network_grants(machine,id,data) VALUES(?,?,?) ON CONFLICT(machine,id) DO UPDATE SET data=excluded.data`, s.machine, g.ID, b)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Grant(ctx context.Context, id string) (app.Grant, error) {
	var b []byte
	var g app.Grant
	e := s.db.QueryRowContext(ctx, `SELECT data FROM project_assistant_network_grants WHERE machine=? AND id=?`, s.machine, id).Scan(&b)
	if errors.Is(e, sql.ErrNoRows) {
		return g, refuse(pa.Unpaired, "network_grant")
	}
	if e == nil {
		e = json.Unmarshal(b, &g)
	}
	return g, e
}
func (s *Store) Claim(ctx context.Context, h wire.BotHeader) (app.Receipt, bool, error) {
	actor, id, digest := wire.BotReceiptIdentity(h)
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return app.Receipt{}, false, e
	}
	defer tx.Rollback()
	var old []byte
	e = tx.QueryRowContext(ctx, `SELECT data FROM project_assistant_network_receipts WHERE machine=? AND actor=? AND id=?`, s.machine, actor, id).Scan(&old)
	if e == nil {
		var r app.Receipt
		if e = json.Unmarshal(old, &r); e != nil {
			return r, false, e
		}
		if r.Digest != digest {
			return r, false, refuse(pa.IdempotencyConflict, "network_request")
		}
		return r, false, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return app.Receipt{}, false, e
	}
	// The network receipt budget includes terminal and unknown effects.
	var n int
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_network_receipts WHERE machine=?`, s.machine).Scan(&n); e != nil {
		return app.Receipt{}, false, e
	}
	if n >= pa.MaxNetworkReceipts {
		return app.Receipt{}, false, refuse(pa.CapacityFull, "network_receipts")
	}
	r := app.Receipt{Header: h, Digest: digest, State: "unknown"}
	b, _ := json.Marshal(r)
	if _, e = tx.ExecContext(ctx, `INSERT INTO project_assistant_network_receipts(machine,actor,id,digest,data) VALUES(?,?,?,?,?)`, s.machine, actor, id, digest, b); e != nil {
		return r, false, e
	}
	return r, true, tx.Commit()
}
func (s *Store) Receipt(ctx context.Context, h wire.BotHeader, id string) (app.Receipt, error) {
	actor, _, _ := wire.BotReceiptIdentity(h)
	var b []byte
	var r app.Receipt
	e := s.db.QueryRowContext(ctx, `SELECT data FROM project_assistant_network_receipts WHERE machine=? AND actor=? AND id=?`, s.machine, actor, id).Scan(&b)
	if errors.Is(e, sql.ErrNoRows) {
		return r, refuse(pa.NotFound, "network_receipt")
	}
	if e == nil {
		e = json.Unmarshal(b, &r)
	}
	// A read belongs to the exact requester and target captured at admission.
	if e == nil && (r.Header.Source != h.Source || r.Header.Target != h.Target || r.Header.AuthorityID != h.AuthorityID || r.Header.AuthorityEpoch != h.AuthorityEpoch) {
		e = refuse(pa.ScopeDenied, "network_receipt_capture")
	}
	return r, e
}
func (s *Store) Save(ctx context.Context, r app.Receipt) error {
	actor, id, digest := wire.BotReceiptIdentity(r.Header)
	if r.Digest != digest {
		return refuse(pa.IdempotencyConflict, "network_receipt")
	}
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	result, e := s.db.ExecContext(ctx, `WITH incoming(value) AS (SELECT ?) UPDATE project_assistant_network_receipts SET data=`+receiptPublicationSQL+` WHERE machine=? AND actor=? AND id=? AND digest=?`, b, s.machine, actor, id, digest)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e == nil && n != 1 {
		return refuse(pa.NotFound, "network_receipt")
	}
	return e
}
func (s *Store) Usage(ctx context.Context) (int64, error) {
	var n int64
	e := s.db.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_network_receipts WHERE machine=?`, s.machine).Scan(&n)
	return n, e
}
