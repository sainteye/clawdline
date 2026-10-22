package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	AntigravityProjectConsentLimit       = 1024
	AntigravityConsentRootBytesLimit     = 4096
	AntigravityConsentIdentityBytesLimit = 128
)

const antigravityIntegrationSchema = `
CREATE TABLE IF NOT EXISTS antigravity_project_consent (
 project_root TEXT PRIMARY KEY,
 revision INTEGER NOT NULL CHECK (revision > 0),
 phase TEXT NOT NULL CHECK (phase IN ('preparing','active','disabled')),
 operation_id TEXT NOT NULL,
 helper_sha256 TEXT NOT NULL,
 plan_sha256 TEXT NOT NULL
);
CREATE TRIGGER IF NOT EXISTS antigravity_consent_keep_revision
BEFORE DELETE ON antigravity_project_consent
BEGIN
 SELECT RAISE(ABORT, 'antigravity_consent_revision_required');
END;`

type AntigravityConsentPhase string

const (
	AntigravityConsentAbsent    AntigravityConsentPhase = "absent"
	AntigravityConsentPreparing AntigravityConsentPhase = "preparing"
	AntigravityConsentActive    AntigravityConsentPhase = "active"
	AntigravityConsentDisabled  AntigravityConsentPhase = "disabled"
)

type AntigravityConsentAction string

const (
	AntigravityConsentPrepare  AntigravityConsentAction = "prepare"
	AntigravityConsentActivate AntigravityConsentAction = "activate"
	AntigravityConsentDisable  AntigravityConsentAction = "disable"
)

// AntigravityConsentError is deliberately path-free. Actor is structural data,
// never evidence of authentication; the transport owns explicit person consent.
type AntigravityConsentError string

func (e AntigravityConsentError) Error() string { return string(e) }

const (
	AntigravityConsentInvalid           AntigravityConsentError = "antigravity_consent_invalid"
	AntigravityConsentVersionConflict   AntigravityConsentError = "antigravity_consent_version_conflict"
	AntigravityConsentPhaseConflict     AntigravityConsentError = "antigravity_consent_phase_conflict"
	AntigravityConsentDigestConflict    AntigravityConsentError = "antigravity_consent_digest_conflict"
	AntigravityConsentOperationConflict AntigravityConsentError = "antigravity_consent_operation_conflict"
	AntigravityConsentKeyConflict       AntigravityConsentError = "antigravity_consent_key_conflict"
	AntigravityConsentReceiptExpired    AntigravityConsentError = "antigravity_consent_receipt_expired"
	AntigravityConsentBusy              AntigravityConsentError = "antigravity_consent_busy"
	AntigravityConsentUnreadable        AntigravityConsentError = "antigravity_consent_unreadable"
	AntigravityConsentWriteFailed       AntigravityConsentError = "antigravity_consent_write_failed"
	AntigravityConsentCapacity          AntigravityConsentError = "antigravity_consent_capacity"
)

// AntigravityProjectConsent is a local snapshot. A command response (including
// an active historical receipt) is NOT an authority check. Read current state
// again and verify the effective workspace files before using a grant.
type AntigravityProjectConsent struct {
	Revision     int64                   `json:"revision"`
	Phase        AntigravityConsentPhase `json:"phase"`
	OperationID  string                  `json:"operation_id"`
	HelperSHA256 string                  `json:"helper_sha256"`
	PlanSHA256   string                  `json:"plan_sha256"`
}

// AntigravityConsentCommand contains every authority-relevant field. Prepare
// must commit before installation; Activate is called only after verified
// installation. Disable must commit before removing files. No filesystem I/O
// happens here. A trusted caller supplies the canonical catalog Project root.
type AntigravityConsentCommand struct {
	ProjectRoot      string
	Action           AntigravityConsentAction
	ExpectedRevision int64
	OperationID      string
	HelperSHA256     string
	PlanSHA256       string
	Actor            string
	Key              string
}

type antigravityConsentAnswer struct {
	State AntigravityProjectConsent `json:"state"`
	Code  AntigravityConsentError   `json:"code,omitempty"`
}

func validAntigravityRoot(root string) bool {
	return root != "" && len(root) <= AntigravityConsentRootBytesLimit && utf8.ValidString(root) &&
		filepath.IsAbs(root) && filepath.Clean(root) == root && !strings.ContainsFunc(root, unicode.IsControl)
}

func validAntigravityIdentity(v string) bool {
	if len(v) == 0 || len(v) > AntigravityConsentIdentityBytesLimit {
		return false
	}
	for _, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

func validAntigravityDigest(v string) bool {
	b, err := hex.DecodeString(v)
	return err == nil && len(b) == sha256.Size && strings.ToLower(v) == v
}

const antigravityConsentColumns = `revision, phase, operation_id, helper_sha256, plan_sha256`

func scanAntigravityConsent(sc scanner) (AntigravityProjectConsent, error) {
	var c AntigravityProjectConsent
	err := sc.Scan(&c.Revision, &c.Phase, &c.OperationID, &c.HelperSHA256, &c.PlanSHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return AntigravityProjectConsent{Phase: AntigravityConsentAbsent}, nil
	}
	if err != nil {
		return AntigravityProjectConsent{}, AntigravityConsentUnreadable
	}
	if c.Revision <= 0 || !validAntigravityIdentity(c.OperationID) ||
		(c.Phase != AntigravityConsentDisabled && c.Phase != AntigravityConsentPreparing && c.Phase != AntigravityConsentActive) ||
		(c.Phase != AntigravityConsentDisabled && (!validAntigravityDigest(c.HelperSHA256) || !validAntigravityDigest(c.PlanSHA256))) ||
		(c.Phase == AntigravityConsentDisabled && (c.HelperSHA256 != "" || c.PlanSHA256 != "")) {
		return AntigravityProjectConsent{}, AntigravityConsentUnreadable
	}
	return c, nil
}

func (s *Store) AntigravityProjectConsent(ctx context.Context, root string) (AntigravityProjectConsent, error) {
	if !validAntigravityRoot(root) {
		return AntigravityProjectConsent{}, AntigravityConsentInvalid
	}
	if err := reading(); err != nil {
		return AntigravityProjectConsent{}, AntigravityConsentUnreadable
	}
	return scanAntigravityConsent(s.rd.QueryRowContext(ctx, `SELECT `+antigravityConsentColumns+` FROM antigravity_project_consent WHERE project_root = ?`, root))
}

func decideAntigravityConsent(cur AntigravityProjectConsent, cmd AntigravityConsentCommand) (AntigravityProjectConsent, AntigravityConsentError) {
	if cur.Revision != cmd.ExpectedRevision || cur.Revision == math.MaxInt64 {
		return cur, AntigravityConsentVersionConflict
	}
	next := AntigravityProjectConsent{Revision: cur.Revision + 1, OperationID: cmd.OperationID, HelperSHA256: cmd.HelperSHA256, PlanSHA256: cmd.PlanSHA256}
	switch cmd.Action {
	case AntigravityConsentPrepare:
		next.Phase = AntigravityConsentPreparing
	case AntigravityConsentActivate:
		if cur.Phase != AntigravityConsentPreparing {
			return cur, AntigravityConsentPhaseConflict
		}
		if cur.OperationID != cmd.OperationID {
			return cur, AntigravityConsentOperationConflict
		}
		if cur.HelperSHA256 != cmd.HelperSHA256 || cur.PlanSHA256 != cmd.PlanSHA256 {
			return cur, AntigravityConsentDigestConflict
		}
		next.Phase = AntigravityConsentActive
	case AntigravityConsentDisable:
		next.Phase = AntigravityConsentDisabled
	}
	return next, ""
}

// ApplyAntigravityProjectConsent atomically admits a bounded request in the
// existing receipt table, decides the CAS transition, and completes its receipt.
// Replays never execute a transition. Refusals after admission are also replayed.
// SQLite BEGIN IMMEDIATE owns concurrency across handles; no goroutine or
// process-local synchronization is introduced. Storage failures roll back both
// admission and state, allowing a retry of the very same operation identity.
func (s *Store) ApplyAntigravityProjectConsent(ctx context.Context, cmd AntigravityConsentCommand) (AntigravityProjectConsent, error) {
	if !validAntigravityRoot(cmd.ProjectRoot) || cmd.ExpectedRevision < 0 ||
		!validAntigravityIdentity(cmd.Actor) || !validAntigravityIdentity(cmd.Key) || !validAntigravityIdentity(cmd.OperationID) {
		return AntigravityProjectConsent{}, AntigravityConsentInvalid
	}
	switch cmd.Action {
	case AntigravityConsentPrepare, AntigravityConsentActivate:
		if !validAntigravityDigest(cmd.HelperSHA256) || !validAntigravityDigest(cmd.PlanSHA256) {
			return AntigravityProjectConsent{}, AntigravityConsentInvalid
		}
	case AntigravityConsentDisable:
		if cmd.HelperSHA256 != "" || cmd.PlanSHA256 != "" {
			return AntigravityProjectConsent{}, AntigravityConsentInvalid
		}
	default:
		return AntigravityProjectConsent{}, AntigravityConsentInvalid
	}
	encoded, _ := json.Marshal(cmd)
	digest := sha256.Sum256(encoded)
	k := ReceiptKey{Scope: "antigravity.project.consent.v1", Actor: cmd.Actor, Key: cmd.Key}
	var answer antigravityConsentAnswer
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		var stored, state string
		var body []byte
		var created int64
		err := tx.QueryRowContext(ctx, `SELECT digest, state, body, created_at FROM request_receipts WHERE scope = ? AND actor = ? AND key = ?`, k.Scope, k.Actor, k.Key).Scan(&stored, &state, &body, &created)
		if err == nil {
			if stored != hex.EncodeToString(digest[:]) {
				return 0, AntigravityConsentKeyConflict
			}
			if state == "expired" || time.Unix(created, 0).Add(ReceiptWindow).Before(time.Now()) {
				return 0, AntigravityConsentReceiptExpired
			}
			if state != "complete" {
				return 0, AntigravityConsentBusy
			}
			if json.Unmarshal(body, &answer) != nil {
				return 0, AntigravityConsentUnreadable
			}
			return 0, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, AntigravityConsentUnreadable
		}
		var live int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM request_receipts WHERE scope = ? AND state IN ('pending','complete') AND created_at >= ?`, k.Scope, time.Now().Add(-ReceiptWindow).Unix()).Scan(&live); err != nil {
			return 0, AntigravityConsentUnreadable
		}
		if live >= ReceiptLimit {
			return 0, AntigravityConsentCapacity
		}
		cur, err := scanAntigravityConsent(tx.QueryRowContext(ctx, `SELECT `+antigravityConsentColumns+` FROM antigravity_project_consent WHERE project_root = ?`, cmd.ProjectRoot))
		if err != nil {
			return 0, err
		}
		answer.State, answer.Code = decideAntigravityConsent(cur, cmd)
		if answer.Code == "" && cur.Phase == AntigravityConsentAbsent {
			var count int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM antigravity_project_consent`).Scan(&count); err != nil {
				return 0, AntigravityConsentUnreadable
			}
			if count >= AntigravityProjectConsentLimit {
				answer.State, answer.Code = cur, AntigravityConsentCapacity
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO request_receipts (scope, actor, key, digest, state, owner, created_at) VALUES (?, ?, ?, ?, 'pending', ?, ?)`, k.Scope, k.Actor, k.Key, hex.EncodeToString(digest[:]), s.owner.String(), time.Now().Unix()); err != nil {
			return 0, err
		}
		if answer.Code == "" {
			c := answer.State
			_, err := tx.ExecContext(ctx, `INSERT INTO antigravity_project_consent (project_root, revision, phase, operation_id, helper_sha256, plan_sha256) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(project_root) DO UPDATE SET revision=excluded.revision, phase=excluded.phase, operation_id=excluded.operation_id, helper_sha256=excluded.helper_sha256, plan_sha256=excluded.plan_sha256`, cmd.ProjectRoot, c.Revision, c.Phase, c.OperationID, c.HelperSHA256, c.PlanSHA256)
			if err != nil {
				return 0, err
			}
		}
		body, _ = json.Marshal(answer)
		if err := s.completeReceipt(ctx, tx, k, ReceiptAnswer{Status: 200, Body: body}); err != nil {
			return 0, err
		}
		return 2, nil
	})
	if err != nil {
		var code AntigravityConsentError
		if errors.As(err, &code) {
			return AntigravityProjectConsent{}, code
		}
		if errors.Is(err, ErrBusy) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return AntigravityProjectConsent{}, AntigravityConsentBusy
		}
		return AntigravityProjectConsent{}, AntigravityConsentWriteFailed
	}
	if answer.Code != "" {
		return answer.State, answer.Code
	}
	return answer.State, nil
}
