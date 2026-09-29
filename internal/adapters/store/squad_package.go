package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/sainteye/clawdline/internal/app/squadpackages"
	"github.com/sainteye/clawdline/internal/domain/squad"
	"github.com/sainteye/clawdline/internal/domain/squadpack"
)

const squadPackageSchema = `
CREATE TABLE IF NOT EXISTS squad_package_previews (
 token_hash TEXT PRIMARY KEY, actor TEXT NOT NULL, scope TEXT NOT NULL,
 archive_digest TEXT NOT NULL, catalog_version INTEGER NOT NULL,
 settings_digest TEXT NOT NULL, preview_digest TEXT NOT NULL,
 expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS squad_package_receipts (
 actor TEXT NOT NULL, request_id TEXT NOT NULL, fingerprint TEXT NOT NULL,
 receipt TEXT NOT NULL, PRIMARY KEY(actor,request_id)
);
CREATE TABLE IF NOT EXISTS squad_package_sources (
 kind TEXT NOT NULL, id TEXT NOT NULL, version TEXT NOT NULL,
 definition TEXT NOT NULL, body TEXT NOT NULL,
 PRIMARY KEY(kind,id,version)
);
`

func openSquadPackages(db *sql.DB) error {
	_, err := db.Exec(squadPackageSchema)
	return err
}

func (s *Store) SquadPackagePreviewUsage(ctx context.Context) (int64, error) {
	if err := reading(); err != nil {
		return 0, err
	}
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM squad_package_previews`).Scan(&n)
	return n, classify(err)
}

// PackageCatalog adapts the private squad tables to the offline package
// service. A preview holds hashes only; adoption owns a single store write.
type PackageCatalog struct {
	Store       *Store
	Limits      SquadLimits
	PreviewRows int64
}

func (c PackageCatalog) limits() SquadLimits {
	if c.Limits.Entities > 0 && c.Limits.SettingsRows > 0 && c.Limits.Receipts > 0 {
		return c.Limits
	}
	return DefaultSquadLimits()
}

func (c PackageCatalog) previewRows() int64 {
	if c.PreviewRows > 0 && c.PreviewRows <= squadpackages.MaxPreviewRows {
		return c.PreviewRows
	}
	return squadpackages.MaxPreviewRows
}

type packageQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func packageSettingsDigest(ctx context.Context, q packageQuerier) (string, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT scope_id,definition_id,version,payload FROM squad_settings ORDER BY scope_id,definition_id`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	h := sha256.New()
	for rows.Next() {
		var scope, id, payload string
		var version int64
		if err := rows.Scan(&scope, &id, &version, &payload); err != nil {
			return "", err
		}
		b, _ := json.Marshal([]any{scope, id, version, payload})
		_, _ = h.Write(b)
		_, _ = h.Write([]byte{'\n'})
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func packageActiveRows(ctx context.Context, q packageQuerier) ([]SquadEntityRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT e.kind,e.id,e.version,e.digest,e.payload FROM squad_entities e
	 JOIN squad_active a ON a.kind=e.kind AND a.id=e.id AND a.version=e.version ORDER BY e.kind,e.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SquadEntityRow{}
	for rows.Next() {
		var row SquadEntityRow
		var payload string
		if err := rows.Scan(&row.Kind, &row.ID, &row.Version, &row.Digest, &payload); err != nil {
			return nil, err
		}
		row.Payload = json.RawMessage(payload)
		out = append(out, row)
	}
	return out, rows.Err()
}

func packageExisting(rows []SquadEntityRow) ([]squadpack.Existing, error) {
	out := []squadpack.Existing{}
	builtin := squad.Builtins()
	for _, d := range builtin.Definitions {
		out = append(out, squadpack.Existing{Kind: "persona", ID: d.DefinitionID,
			Version: d.Version, Name: d.Name.En, Digest: d.Digest, Builtin: true})
	}
	for _, d := range builtin.Teams {
		out = append(out, squadpack.Existing{Kind: "team", ID: d.TeamID,
			Version: d.Version, Name: d.Name.En, Digest: d.Digest, Builtin: true})
	}
	for _, row := range rows {
		switch row.Kind {
		case "definition":
			var d squad.Definition
			if err := json.Unmarshal(row.Payload, &d); err != nil {
				return nil, err
			}
			refs := []string{}
			for _, r := range d.Skills {
				refs = append(refs, r.ID)
			}
			out = append(out, squadpack.Existing{Kind: "persona", ID: row.ID,
				Version: row.Version, Digest: row.Digest, Name: d.Name.En, References: refs})
		case "team":
			var d squad.Team
			if err := json.Unmarshal(row.Payload, &d); err != nil {
				return nil, err
			}
			refs := []string{}
			for _, r := range d.Personas {
				refs = append(refs, r.ID)
			}
			out = append(out, squadpack.Existing{Kind: "team", ID: row.ID,
				Version: row.Version, Digest: row.Digest, Name: d.Name.En, References: refs})
		case "skill":
			var d squad.Skill
			if err := json.Unmarshal(row.Payload, &d); err != nil {
				return nil, err
			}
			out = append(out, squadpack.Existing{Kind: "skill", ID: row.ID,
				Version: row.Version, Digest: row.Digest, Name: d.Name.En})
		default:
			return nil, fmt.Errorf("unknown squad entity kind %q", row.Kind)
		}
	}
	return out, nil
}

func (c PackageCatalog) Snapshot(ctx context.Context) (squadpackages.Snapshot, error) {
	if err := reading(); err != nil {
		return squadpackages.Snapshot{}, err
	}
	var version int64
	if err := c.Store.db.QueryRowContext(ctx,
		`SELECT version FROM squad_catalog_state WHERE singleton=1`).Scan(&version); err != nil {
		return squadpackages.Snapshot{}, classify(err)
	}
	rows, err := packageActiveRows(ctx, c.Store.db)
	if err != nil {
		return squadpackages.Snapshot{}, classify(err)
	}
	existing, err := packageExisting(rows)
	if err != nil {
		return squadpackages.Snapshot{}, err
	}
	settings, err := packageSettingsDigest(ctx, c.Store.db)
	if err != nil {
		return squadpackages.Snapshot{}, classify(err)
	}
	return squadpackages.Snapshot{Version: version, SettingsDigest: settings, Existing: existing}, nil
}

func (c PackageCatalog) SavePreview(ctx context.Context, grant squadpackages.PreviewGrant) error {
	return c.Store.write(ctx, func(tx *sql.Tx) (int64, error) {
		if _, err := tx.ExecContext(ctx, `DELETE FROM squad_package_previews WHERE expires_at<=?`,
			time.Now().Unix()); err != nil {
			return 0, err
		}
		if n, err := countRows(ctx, tx, "squad_package_previews"); err != nil {
			return 0, err
		} else if n >= c.previewRows() {
			return 0, ErrSquadCapacityFull
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO squad_package_previews
		 (token_hash,actor,scope,archive_digest,catalog_version,settings_digest,preview_digest,expires_at)
		 VALUES(?,?,?,?,?,?,?,?)`, grant.TokenHash, grant.Actor, grant.Scope, grant.ArchiveDigest,
			grant.CatalogVersion, grant.SettingsDigest, grant.PreviewDigest, grant.ExpiresAt.Unix())
		return 1, err
	})
}

func (c PackageCatalog) Replay(ctx context.Context, actor, key, fingerprint string) (squadpackages.Receipt, bool, error) {
	if err := reading(); err != nil {
		return squadpackages.Receipt{}, false, err
	}
	var savedFingerprint, raw string
	err := c.Store.db.QueryRowContext(ctx,
		`SELECT fingerprint,receipt FROM squad_package_receipts WHERE actor=? AND request_id=?`, actor, key).
		Scan(&savedFingerprint, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return squadpackages.Receipt{}, false, nil
	}
	if err != nil {
		return squadpackages.Receipt{}, false, classify(err)
	}
	if savedFingerprint != fingerprint {
		return squadpackages.Receipt{}, false, ErrSquadReceiptConflict
	}
	var receipt squadpackages.Receipt
	if err := json.Unmarshal([]byte(raw), &receipt); err != nil {
		return receipt, false, err
	}
	return receipt, true, nil
}

func packageRefusal(code string) error { return &squadpack.Refusal{Code: code} }

func packageScope(p squadpack.Private) string {
	if p.Scope == "global" {
		return "global"
	}
	return p.Project
}

func packageIDKind(kind string) string {
	if kind == "persona" {
		return "definition"
	}
	return kind
}

func packageSourcePath(kind, id string) string {
	return "definitions/" + kind + "/" + id + ".md"
}
