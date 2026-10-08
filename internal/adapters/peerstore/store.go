// Package peerstore holds locally confirmed machine peer keys and grants.
// The Cloud API's account membership is never a substitute for these rows.
package peerstore

import (
	"context"
	"crypto/ecdh"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sainteye/clawdline/internal/domain/agenthandoff"
	_ "modernc.org/sqlite"
)

const (
	PairLimit  = 128
	GrantLimit = 512
	InboxLimit = 512
	// One 512 KiB body plus its receipt fits the encrypted Cloud read frame.
	InboxPageLimit = 1
	OutboxLimit    = 512
	databaseFile   = "machine-peers-v1.sqlite"
)

var (
	ErrNotFound = errors.New("peer_record_not_found")
	ErrFull     = errors.New("peer_records_full")
	ErrRevoked  = errors.New("peer_record_revoked")
)

// Pair is installed only after both signatures and displayed fingerprints
// have been checked. PrivateKey is the local X25519 key's raw 32 bytes.
type Pair struct {
	ID                  string    `json:"id"`
	AccountID           string    `json:"account_id"`
	SourceMachineID     string    `json:"source_machine_id"`
	TargetMachineID     string    `json:"target_machine_id"`
	SourcePublicKey     string    `json:"source_public_key"`
	TargetPublicKey     string    `json:"target_public_key"`
	SourceFingerprint   string    `json:"source_fingerprint"`
	TargetFingerprint   string    `json:"target_fingerprint"`
	SourceEncryptionKey string    `json:"source_encryption_key"`
	TargetEncryptionKey string    `json:"target_encryption_key"`
	SourceSignature     string    `json:"source_signature"`
	TargetSignature     string    `json:"target_signature"`
	LocalPrivateKey     []byte    `json:"local_private_key"`
	ExpiresAt           time.Time `json:"expires_at"`
	RevokedAt           time.Time `json:"revoked_at,omitempty"`
}

type Grant struct {
	ID                   string                `json:"id"`
	PairID               string                `json:"pair_id"`
	Source               agenthandoff.Endpoint `json:"source"`
	Target               agenthandoff.Endpoint `json:"target"`
	SourceKeyFingerprint string                `json:"source_key_fingerprint"`
	AllowMessage         bool                  `json:"allow_message"`
	AllowHandoff         bool                  `json:"allow_handoff"`
	ExpiresAt            time.Time             `json:"expires_at"`
	RevokedAt            time.Time             `json:"revoked_at,omitempty"`
}

// InboxItem is a structured peer message or handoff, separate from chat.
// Reading this row alone does not prove that an Agent observed it.
type InboxItem struct {
	Request agenthandoff.Request `json:"request"`
	Body    string               `json:"body"`
	At      time.Time            `json:"at"`
}

// OutboxItem is source-side evidence only. It never claims target execution.
type OutboxItem struct {
	Request        agenthandoff.Request `json:"request"`
	RelayAccepted  string               `json:"relay_accepted"`
	RelayDelivered string               `json:"relay_delivered"`
	RelayConflict  bool                 `json:"relay_conflict,omitempty"`
	At             time.Time            `json:"at"`
}

type Store struct{ db *sql.DB }

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	directory, err := os.Lstat(dir)
	if err != nil || !directory.IsDir() || directory.Mode()&os.ModeSymlink != 0 || directory.Mode().Perm()&0077 != 0 {
		return nil, errors.New("peer_store_directory_not_private")
	}
	path := filepath.Join(dir, databaseFile)
	if prior, err := os.Lstat(path); err == nil {
		if !prior.Mode().IsRegular() || prior.Mode().Perm()&0077 != 0 {
			return nil, errors.New("peer_store_file_not_private")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		"PRAGMA busy_timeout=5000",
		"CREATE TABLE IF NOT EXISTS peer_pairs (id TEXT PRIMARY KEY, body BLOB NOT NULL)",
		"CREATE TABLE IF NOT EXISTS peer_grants (id TEXT PRIMARY KEY, pair_id TEXT NOT NULL, body BLOB NOT NULL)",
		"CREATE INDEX IF NOT EXISTS peer_grants_pair ON peer_grants(pair_id)",
		"CREATE TABLE IF NOT EXISTS peer_inbox (id TEXT PRIMARY KEY, target_session TEXT NOT NULL, body BLOB NOT NULL)",
		"CREATE INDEX IF NOT EXISTS peer_inbox_target ON peer_inbox(target_session)",
		"CREATE TABLE IF NOT EXISTS peer_outbox (id TEXT PRIMARY KEY, body BLOB NOT NULL)",
	} {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			return nil, err
		}
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

type Usage struct {
	Pairs, Grants, Inbox, Outbox int64
}

// ReadUsage leaves an unused Cloud peer store absent. An existing store that
// cannot be read is an unknown measurement, never a claimed empty one.
func ReadUsage(ctx context.Context, dir string) (Usage, error) {
	if _, err := os.Lstat(filepath.Join(dir, databaseFile)); errors.Is(err, os.ErrNotExist) {
		return Usage{}, nil
	} else if err != nil {
		return Usage{}, err
	}
	st, err := Open(dir)
	if err != nil {
		return Usage{}, err
	}
	defer st.Close()
	var usage Usage
	err = st.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM peer_pairs), (SELECT COUNT(*) FROM peer_grants),
		(SELECT COUNT(*) FROM peer_inbox), (SELECT COUNT(*) FROM peer_outbox)`).
		Scan(&usage.Pairs, &usage.Grants, &usage.Inbox, &usage.Outbox)
	return usage, err
}

// SavePending keeps the source's private X25519 key after the Cloud start
// receipt. The source later reads the accepted transcript and promotes this
// same row. An absent receipt leaves no valid pair.
func (s *Store) SavePending(ctx context.Context, pair Pair) error {
	if pair.ID == "" || pair.AccountID == "" || pair.SourceMachineID == "" ||
		pair.TargetMachineID == "" || pair.TargetFingerprint == "" ||
		pair.SourceEncryptionKey == "" || pair.SourceSignature == "" ||
		len(pair.LocalPrivateKey) != 32 || !time.Now().Before(pair.ExpiresAt) ||
		pair.TargetSignature != "" {
		return errors.New("peer_pending_invalid")
	}
	return s.insert(ctx, "peer_pairs", PairLimit, pair.ID, "", pair)
}

// PinPair stores an already authenticated transcript and never overwrites an
// existing pair ID. Re-pairing creates a new ID and key.
func (s *Store) PinPair(ctx context.Context, pair Pair, localMachineID, comparedPeerFingerprint string) error {
	if pair.ID == "" || len(pair.LocalPrivateKey) != 32 || pair.AccountID == "" ||
		pair.SourcePublicKey == "" || pair.TargetPublicKey == "" ||
		pair.SourceSignature == "" || pair.TargetSignature == "" ||
		!time.Now().Before(pair.ExpiresAt) || !pair.RevokedAt.IsZero() {
		return errors.New("peer_pair_invalid")
	}
	transcript := agenthandoff.PairTranscript{
		PairID: pair.ID, SourceMachineID: pair.SourceMachineID,
		TargetMachineID: pair.TargetMachineID,
		SourcePublicKey: pair.SourcePublicKey, TargetPublicKey: pair.TargetPublicKey,
		SourceFingerprint: pair.SourceFingerprint, TargetFingerprint: pair.TargetFingerprint,
		SourceEncryptionKey: pair.SourceEncryptionKey, TargetEncryptionKey: pair.TargetEncryptionKey,
		SourceOfferSignature: pair.SourceSignature, TargetAcceptSignature: pair.TargetSignature,
	}
	if err := agenthandoff.VerifyTranscript(transcript, localMachineID, comparedPeerFingerprint); err != nil {
		return err
	}
	private, err := ecdh.X25519().NewPrivateKey(pair.LocalPrivateKey)
	if err != nil {
		return err
	}
	keys := agenthandoff.PairKeys{
		PairID: pair.ID, SourceMachineID: pair.SourceMachineID, TargetMachineID: pair.TargetMachineID,
		SourceEncryptionKey: pair.SourceEncryptionKey, TargetEncryptionKey: pair.TargetEncryptionKey,
	}
	if _, _, err := agenthandoff.PairKey(keys, private, localMachineID == pair.SourceMachineID); err != nil {
		return err
	}
	prior, err := s.Pair(ctx, pair.ID)
	if err == nil {
		if prior.TargetSignature != "" {
			old, _ := json.Marshal(prior)
			current, _ := json.Marshal(pair)
			if string(old) == string(current) {
				return nil
			}
			return errors.New("peer_pair_mismatch")
		}
		if prior.AccountID != pair.AccountID ||
			prior.SourceMachineID != pair.SourceMachineID ||
			prior.TargetMachineID != pair.TargetMachineID ||
			prior.TargetFingerprint != pair.TargetFingerprint ||
			prior.SourceEncryptionKey != pair.SourceEncryptionKey ||
			prior.SourceSignature != pair.SourceSignature ||
			string(prior.LocalPrivateKey) != string(pair.LocalPrivateKey) {
			return errors.New("peer_pending_mismatch")
		}
		return s.replace(ctx, "peer_pairs", pair.ID, pair)
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	return s.insert(ctx, "peer_pairs", PairLimit, pair.ID, "", pair)
}

func (s *Store) Pair(ctx context.Context, id string) (Pair, error) {
	var pair Pair
	err := s.db.QueryRowContext(ctx, "SELECT body FROM peer_pairs WHERE id=?", id).Scan(&pairBody{into: &pair})
	if errors.Is(err, sql.ErrNoRows) {
		return Pair{}, ErrNotFound
	}
	return pair, err
}

func (s *Store) PinGrant(ctx context.Context, grant Grant) error {
	if grant.ID == "" || grant.PairID == "" || grant.Source.MachineID == "" ||
		grant.Source.SessionID == "" || grant.Source.ExecutionGeneration == "" ||
		grant.Target.MachineID == "" || grant.Target.SessionID == "" ||
		grant.Target.ExecutionGeneration == "" || !time.Now().Before(grant.ExpiresAt) ||
		(!grant.AllowMessage && !grant.AllowHandoff) {
		return errors.New("peer_grant_invalid")
	}
	pair, err := s.Pair(ctx, grant.PairID)
	if err != nil {
		return err
	}
	if pair.TargetSignature == "" || !pair.RevokedAt.IsZero() ||
		!time.Now().Before(pair.ExpiresAt) ||
		grant.ExpiresAt.After(pair.ExpiresAt) ||
		grant.Source.MachineID != pair.SourceMachineID ||
		grant.Target.MachineID != pair.TargetMachineID ||
		grant.SourceKeyFingerprint != pair.SourceFingerprint {
		return errors.New("peer_grant_pair_mismatch")
	}
	prior, err := s.Grant(ctx, grant.ID)
	if err == nil {
		old, _ := json.Marshal(prior)
		newValue, _ := json.Marshal(grant)
		if string(old) == string(newValue) {
			return nil
		}
		return errors.New("peer_grant_mismatch")
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	return s.insert(ctx, "peer_grants", GrantLimit, grant.ID, grant.PairID, grant)
}

func (s *Store) Grant(ctx context.Context, id string) (Grant, error) {
	var grant Grant
	err := s.db.QueryRowContext(ctx, "SELECT body FROM peer_grants WHERE id=?", id).Scan(&pairBody{into: &grant})
	if errors.Is(err, sql.ErrNoRows) {
		return Grant{}, ErrNotFound
	}
	return grant, err
}

// PutInbox is idempotent only for byte-identical request and content. The
// caller has already claimed the request receipt and rechecked admission.
func (s *Store) PutInbox(ctx context.Context, item InboxItem) error {
	if item.Request.RequestID == "" || item.Request.Target.SessionID == "" ||
		item.Body == "" || item.At.IsZero() {
		return errors.New("peer_inbox_invalid")
	}
	prior, err := s.Inbox(ctx, item.Request.RequestID)
	if err == nil {
		if prior.Request == item.Request && prior.Body == item.Body {
			return nil
		}
		return errors.New("peer_inbox_request_conflict")
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	encoded, err := json.Marshal(item)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM peer_inbox").Scan(&count); err != nil {
		return err
	}
	if count >= InboxLimit {
		return ErrFull
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO peer_inbox(id,target_session,body) VALUES(?,?,?)",
		item.Request.RequestID, item.Request.Target.SessionID, encoded); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Inbox(ctx context.Context, id string) (InboxItem, error) {
	var item InboxItem
	err := s.db.QueryRowContext(ctx, "SELECT body FROM peer_inbox WHERE id=?", id).Scan(&pairBody{into: &item})
	if errors.Is(err, sql.ErrNoRows) {
		return InboxItem{}, ErrNotFound
	}
	return item, err
}

// InboxPage returns one exact execution's newest work before an optional
// request ID. The caller receives a cursor only when another matching item
// exists, so no Cloud reply grows with the durable inbox's total size.
func (s *Store) InboxPage(ctx context.Context, target agenthandoff.Endpoint, beforeID string) ([]InboxItem, string, error) {
	before := int64(1<<63 - 1)
	if beforeID != "" {
		var cursor InboxItem
		if err := s.db.QueryRowContext(ctx, "SELECT rowid,body FROM peer_inbox WHERE id=?", beforeID).
			Scan(&before, &pairBody{into: &cursor}); errors.Is(err, sql.ErrNoRows) {
			return nil, "", ErrNotFound
		} else if err != nil {
			return nil, "", err
		} else if cursor.Request.Target != target {
			return nil, "", ErrNotFound
		}
	}
	rows, err := s.db.QueryContext(ctx, "SELECT body FROM peer_inbox WHERE target_session=? AND rowid<? ORDER BY rowid DESC LIMIT ?",
		target.SessionID, before, InboxLimit)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := make([]InboxItem, 0, InboxPageLimit)
	for rows.Next() {
		var item InboxItem
		if err := rows.Scan(&pairBody{into: &item}); err != nil {
			return nil, "", err
		}
		if item.Request.Target != target {
			continue
		}
		if len(items) == InboxPageLimit {
			return items, items[len(items)-1].Request.RequestID, nil
		}
		items = append(items, item)
	}
	return items, "", rows.Err()
}

func (s *Store) SaveOutbox(ctx context.Context, item OutboxItem) error {
	if item.Request.RequestID == "" || item.At.IsZero() ||
		item.RelayAccepted != "unknown" || item.RelayDelivered != "unknown" {
		return errors.New("peer_outbox_invalid")
	}
	prior, err := s.Outbox(ctx, item.Request.RequestID)
	if err == nil {
		if prior.Request == item.Request {
			return nil
		}
		return errors.New("peer_outbox_request_conflict")
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	return s.insert(ctx, "peer_outbox", OutboxLimit, item.Request.RequestID, "", item)
}

func (s *Store) Outbox(ctx context.Context, id string) (OutboxItem, error) {
	var item OutboxItem
	err := s.db.QueryRowContext(ctx, "SELECT body FROM peer_outbox WHERE id=?", id).Scan(&pairBody{into: &item})
	if errors.Is(err, sql.ErrNoRows) {
		return OutboxItem{}, ErrNotFound
	}
	return item, err
}

// MarkOutbox applies only evidence the relay returned for a saved request.
// Conflicting relay answers fall back to unknown rather than choosing one.
func (s *Store) MarkOutbox(ctx context.Context, id, accepted, delivered string) error {
	item, err := s.Outbox(ctx, id)
	if err != nil {
		return err
	}
	if accepted != "" {
		if accepted != "accepted" {
			return errors.New("peer_outbox_status_invalid")
		}
		item.RelayAccepted = "accepted"
	}
	if delivered != "" {
		switch delivered {
		case "delivered", "machine_offline", "unknown":
		default:
			return errors.New("peer_outbox_status_invalid")
		}
		if item.RelayConflict || item.RelayDelivered != "unknown" && item.RelayDelivered != delivered {
			item.RelayDelivered = "unknown"
			item.RelayConflict = true
		} else {
			item.RelayDelivered = delivered
		}
	}
	return s.replace(ctx, "peer_outbox", id, item)
}

func (s *Store) insert(ctx context.Context, table string, limit int, id, pairID string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
		return err
	}
	if count >= limit {
		return ErrFull
	}
	query := "INSERT INTO peer_pairs(id,body) VALUES(?,?)"
	args := []any{id, encoded}
	if table == "peer_grants" {
		query = "INSERT INTO peer_grants(id,pair_id,body) VALUES(?,?,?)"
		args = []any{id, pairID, encoded}
	} else if table == "peer_outbox" {
		query = "INSERT INTO peer_outbox(id,body) VALUES(?,?)"
	}
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return err
	}
	return tx.Commit()
}

// RevokePair and RevokeGrant persist a local denial before the Cloud DELETE.
// A failed Cloud call therefore cannot leave this machine accepting work.
func (s *Store) RevokePair(ctx context.Context, id string, at time.Time) error {
	if at.IsZero() {
		at = time.Now()
	}
	pair, err := s.Pair(ctx, id)
	if err != nil {
		return err
	}
	pair.RevokedAt = at
	return s.replace(ctx, "peer_pairs", id, pair)
}

func (s *Store) RevokeGrant(ctx context.Context, id string, at time.Time) error {
	if at.IsZero() {
		at = time.Now()
	}
	grant, err := s.Grant(ctx, id)
	if err != nil {
		return err
	}
	grant.RevokedAt = at
	return s.replace(ctx, "peer_grants", id, grant)
}

func (s *Store) replace(ctx context.Context, table, id string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, "UPDATE "+table+" SET body=? WHERE id=?", encoded, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

type pairBody struct{ into any }

func (p *pairBody) Scan(src any) error {
	data, ok := src.([]byte)
	if !ok {
		return fmt.Errorf("peer_record_unreadable")
	}
	if err := json.Unmarshal(data, p.into); err != nil {
		return fmt.Errorf("peer_record_unreadable: %w", err)
	}
	return nil
}
