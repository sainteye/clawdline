package botnetwork

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"

	app "github.com/sainteye/clawdline/internal/app/botnetwork"
	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// AssistantStore is an isolated source namespace. Management writes are local
// composition ports only: no Bot tool or assistant transport exposes them.
type AssistantStore struct {
	db               *sql.DB
	account, machine string
}

func NewAssistantStore(ctx context.Context, db *sql.DB, account, machine string) (*AssistantStore, error) {
	if db == nil || account == "" || machine == "" {
		return nil, refuse(pa.Invalid, "assistant_store")
	}
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS project_assistant_gateway_policy(account TEXT NOT NULL,machine TEXT NOT NULL,version INTEGER NOT NULL,data BLOB NOT NULL,PRIMARY KEY(account,machine))`,
		`CREATE TABLE IF NOT EXISTS project_assistant_gateway_grants(account TEXT NOT NULL,machine TEXT NOT NULL,id TEXT NOT NULL,epoch INTEGER NOT NULL,data BLOB NOT NULL,PRIMARY KEY(account,machine,id))`,
		`CREATE TABLE IF NOT EXISTS project_assistant_gateway_receipts(account TEXT NOT NULL,machine TEXT NOT NULL,actor TEXT NOT NULL,id TEXT NOT NULL,digest TEXT NOT NULL,data BLOB NOT NULL,PRIMARY KEY(account,machine,actor,id))`,
	} {
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			return nil, err
		}
	}
	return &AssistantStore{db: db, account: account, machine: machine}, nil
}

// PutAssistantPolicy requires authenticated human management at composition.
// It is deliberately absent from AssistantService and all model tool ports.
// expectedVersion=0 creates the first policy; every change advances its CAS.
func (s *AssistantStore) PutAssistantPolicy(ctx context.Context, policy app.AssistantPolicy, expectedVersion int64) error {
	if policy.AccountID != s.account || policy.MachineID != s.machine || expectedVersion < 0 ||
		policy.Version != expectedVersion+1 ||
		(policy.GatewayKeyID == "") != (len(policy.GatewayPublicKey) == 0) ||
		(len(policy.GatewayPublicKey) != 0 && len(policy.GatewayPublicKey) != ed25519.PublicKeySize) {
		return refuse(pa.Invalid, "assistant_policy")
	}
	data, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	if expectedVersion == 0 {
		result, err := s.db.ExecContext(ctx, `INSERT INTO project_assistant_gateway_policy(account,machine,version,data) VALUES(?,?,?,?) ON CONFLICT(account,machine) DO NOTHING`, s.account, s.machine, policy.Version, data)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err == nil && n != 1 {
			return refuse(pa.StaleVersion, "assistant_policy")
		}
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE project_assistant_gateway_policy SET version=?,data=? WHERE account=? AND machine=? AND version=?`, policy.Version, data, s.account, s.machine, expectedVersion)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return refuse(pa.StaleVersion, "assistant_policy")
	}
	return err
}

func (s *AssistantStore) AssistantPolicy(ctx context.Context) (app.AssistantPolicy, error) {
	var data []byte
	var policy app.AssistantPolicy
	err := s.db.QueryRowContext(ctx, `SELECT data FROM project_assistant_gateway_policy WHERE account=? AND machine=?`, s.account, s.machine).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return policy, wire.AssistantUnavailable
	}
	if err == nil {
		err = json.Unmarshal(data, &policy)
	}
	return policy, err
}

// PutAssistantGrant is likewise a human management port. Revocation is a newer
// retained epoch, not deletion; a grant ID never changes its scoped principal.
func (s *AssistantStore) PutAssistantGrant(ctx context.Context, grant wire.AssistantGrant, expectedEpoch int64) error {
	if grant.ID == "" || grant.KeyID == "" || grant.PrincipalID == "" || grant.AccountID != s.account || grant.MachineID != s.machine ||
		grant.ProjectID == "" || len(grant.PublicKey) != ed25519.PublicKeySize || grant.Epoch != expectedEpoch+1 || expectedEpoch < 0 ||
		grant.NotBefore.IsZero() || !grant.NotBefore.Before(grant.ExpiresAt) || len(grant.BotIDs) == 0 || len(grant.BotIDs) > pa.MaxPageRows {
		return refuse(pa.Invalid, "assistant_grant")
	}
	seen := make(map[string]bool, len(grant.BotIDs))
	for _, id := range grant.BotIDs {
		if id == "" || seen[id] {
			return refuse(pa.Invalid, "assistant_grant_scope")
		}
		seen[id] = true
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var data []byte
	err = tx.QueryRowContext(ctx, `SELECT data FROM project_assistant_gateway_grants WHERE account=? AND machine=? AND id=?`, s.account, s.machine, grant.ID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		if expectedEpoch != 0 {
			return refuse(pa.StaleVersion, "assistant_grant")
		}
		var n int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_gateway_grants WHERE account=? AND machine=?`, s.account, s.machine).Scan(&n); err != nil {
			return err
		}
		if n >= pa.MaxNetworkAssistantGrants {
			return refuse(pa.CapacityFull, "assistant_grants")
		}
	} else if err != nil {
		return err
	} else {
		var previous wire.AssistantGrant
		if err = json.Unmarshal(data, &previous); err != nil {
			return err
		}
		if previous.Epoch != expectedEpoch || previous.PrincipalID != grant.PrincipalID || previous.AccountID != grant.AccountID ||
			previous.MachineID != grant.MachineID || previous.ProjectID != grant.ProjectID {
			return refuse(pa.StaleVersion, "assistant_grant")
		}
	}
	data, err = json.Marshal(grant)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_gateway_grants(account,machine,id,epoch,data) VALUES(?,?,?,?,?) ON CONFLICT(account,machine,id) DO UPDATE SET epoch=excluded.epoch,data=excluded.data`, s.account, s.machine, grant.ID, grant.Epoch, data)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *AssistantStore) AssistantGrant(ctx context.Context, id string) (wire.AssistantGrant, error) {
	var data []byte
	var grant wire.AssistantGrant
	err := s.db.QueryRowContext(ctx, `SELECT data FROM project_assistant_gateway_grants WHERE account=? AND machine=? AND id=?`, s.account, s.machine, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return grant, wire.AssistantUnavailable
	}
	if err == nil {
		err = json.Unmarshal(data, &grant)
	}
	return grant, err
}

func (s *AssistantStore) ClaimAssistant(ctx context.Context, call wire.AssistantCall, principal wire.VerifiedAssistant) (app.AssistantReceipt, bool, error) {
	var receipt app.AssistantReceipt
	if principal.PrincipalID() == "" || principal.AccountID() != s.account || call.Target.AccountID != s.account || call.Target.MachineID != s.machine || principal.GrantID() != call.GrantID || principal.Epoch() != call.GrantEpoch {
		return receipt, false, wire.AssistantDenied
	}
	actor, id, digest := app.AssistantReceiptIdentity(call, principal.PrincipalID())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return receipt, false, err
	}
	defer tx.Rollback()
	var data []byte
	err = tx.QueryRowContext(ctx, `SELECT data FROM project_assistant_gateway_receipts WHERE account=? AND machine=? AND actor=? AND id=?`, s.account, s.machine, actor, id).Scan(&data)
	if err == nil {
		if err = json.Unmarshal(data, &receipt); err != nil {
			return receipt, false, err
		}
		retainedActor, retainedID, retainedDigest := app.AssistantReceiptIdentity(receipt.Call, receipt.PrincipalID)
		// A gateway may sign the same submission again within current consent.
		// Keep the original receipt and its digest, and compare the effect inputs
		// after matching only the new signature's authentication metadata.
		retainedCall := receipt.Call
		retainedCall.IssuedAt, retainedCall.ExpiresAt, retainedCall.Signature = call.IssuedAt, call.ExpiresAt, call.Signature
		if retainedActor != actor || retainedID != id || retainedDigest != receipt.Digest || retainedCall != call {
			return receipt, false, refuse(pa.IdempotencyConflict, "assistant_request")
		}
		return receipt, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return receipt, false, err
	}
	var n int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_gateway_receipts WHERE account=? AND machine=?`, s.account, s.machine).Scan(&n); err != nil {
		return receipt, false, err
	}
	if n >= pa.MaxNetworkAssistantReceipts {
		return receipt, false, refuse(pa.CapacityFull, "assistant_receipts")
	}
	receipt = app.AssistantReceipt{Call: call, PrincipalID: principal.PrincipalID(), Digest: digest, State: "unknown"}
	data, err = json.Marshal(receipt)
	if err != nil {
		return receipt, false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_gateway_receipts(account,machine,actor,id,digest,data) VALUES(?,?,?,?,?,?)`, s.account, s.machine, actor, id, digest, data)
	if err != nil {
		return receipt, false, err
	}
	return receipt, true, tx.Commit()
}

func (s *AssistantStore) AssistantReceipt(ctx context.Context, call wire.AssistantCall, principal wire.VerifiedAssistant, id string) (app.AssistantReceipt, error) {
	var receipt app.AssistantReceipt
	if principal.PrincipalID() == "" || principal.AccountID() != s.account || principal.GrantID() != call.GrantID || principal.Epoch() != call.GrantEpoch {
		return receipt, wire.AssistantDenied
	}
	actor, _, _ := app.AssistantReceiptIdentity(call, principal.PrincipalID())
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT data FROM project_assistant_gateway_receipts WHERE account=? AND machine=? AND actor=? AND id=?`, s.account, s.machine, actor, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return receipt, refuse(pa.NotFound, "assistant_receipt")
	}
	if err == nil {
		err = json.Unmarshal(data, &receipt)
	}
	if err == nil && (receipt.PrincipalID != principal.PrincipalID() || receipt.Call.Target != call.Target || receipt.Call.GrantID != call.GrantID || receipt.Call.GrantEpoch != call.GrantEpoch) {
		err = wire.AssistantStale
	}
	return receipt, err
}

func (s *AssistantStore) SaveAssistant(ctx context.Context, receipt app.AssistantReceipt) error {
	actor, id, digest := app.AssistantReceiptIdentity(receipt.Call, receipt.PrincipalID)
	if receipt.PrincipalID == "" || receipt.Call.Target.AccountID != s.account || receipt.Call.Target.MachineID != s.machine || receipt.Digest != digest {
		return refuse(pa.IdempotencyConflict, "assistant_receipt")
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `WITH incoming(value) AS (SELECT ?) UPDATE project_assistant_gateway_receipts SET data=`+receiptPublicationSQL+` WHERE account=? AND machine=? AND actor=? AND id=? AND digest=?`, data, s.account, s.machine, actor, id, digest)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return refuse(pa.NotFound, "assistant_receipt")
	}
	return err
}

// AssistantUsage measures retained rows, including revoked/unknown facts.
func (s *AssistantStore) AssistantUsage(ctx context.Context) (grants, receipts int64, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_gateway_grants WHERE account=? AND machine=?`, s.account, s.machine).Scan(&grants)
	if err == nil {
		err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_gateway_receipts WHERE account=? AND machine=?`, s.account, s.machine).Scan(&receipts)
	}
	return
}
