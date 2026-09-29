package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/squad"
	"github.com/sainteye/clawdline/internal/domain/squadpack"
)

var ErrSquadSkillFolderExport = errors.New("skill_folder_export_requires_explicit_support")

func packageExportDefinition(ctx context.Context, tx *sql.Tx, row SquadEntityRow) (squadpack.Definition, []byte, error) {
	var raw, body string
	err := tx.QueryRowContext(ctx, `SELECT definition,body FROM squad_package_sources
	 WHERE kind=? AND id=? AND version=?`, row.Kind, row.ID, row.Version).Scan(&raw, &body)
	if err == nil {
		var d squadpack.Definition
		if err := json.Unmarshal([]byte(raw), &d); err != nil {
			return d, nil, err
		}
		return d, []byte(body), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return squadpack.Definition{}, nil, err
	}
	d := squadpack.Definition{ID: row.ID, Version: row.Version,
		Body: packageSourcePath(row.Kind, row.ID)}
	switch row.Kind {
	case "definition":
		var value squad.Definition
		if err := json.Unmarshal(row.Payload, &value); err != nil {
			return d, nil, err
		}
		d.Name, d.NameZhHant = value.Name.En, value.Name.ZhHant
		d.Summary, d.SummaryZhHant = value.Summary.En, value.Summary.ZhHant
		d.Icon = squadpack.Icon{Accent: value.Icon.Accent, Cells: value.Icon.Cells}
		d.Source, d.License = value.Source, value.License
		for _, r := range value.Skills {
			d.Skills = append(d.Skills, r.ID)
			if !r.Enabled {
				d.DisabledSkills = append(d.DisabledSkills, r.ID)
			}
		}
		return d, []byte(value.Body), nil
	case "team":
		var value squad.Team
		if err := json.Unmarshal(row.Payload, &value); err != nil {
			return d, nil, err
		}
		d.Name, d.NameZhHant = value.Name.En, value.Name.ZhHant
		d.Icon = squadpack.Icon{Accent: value.Icon.Accent, Cells: value.Icon.Cells}
		d.Source, d.License = value.Source, value.License
		for _, r := range value.Personas {
			d.Personas = append(d.Personas, r.ID)
		}
		return d, []byte{}, nil
	case "skill":
		var value squad.Skill
		if err := json.Unmarshal(row.Payload, &value); err != nil {
			return d, nil, err
		}
		if value.Folder || len(value.Files) > 0 {
			return d, nil, ErrSquadSkillFolderExport
		}
		d.Name, d.NameZhHant = value.Name.En, value.Name.ZhHant
		d.Purpose, d.PurposeZhHant = value.Purpose.En, value.Purpose.ZhHant
		d.Icon = squadpack.Icon{Accent: value.Icon.Accent, Cells: value.Icon.Cells}
		d.Source, d.License = value.Source, value.License
		return d, []byte(value.Content), nil
	default:
		return d, nil, packageRefusal(squadpack.ErrInvalidDefinition)
	}
}

func packagePrivateEntry(scope string) (squadpack.Private, error) {
	if scope == "global" {
		return squadpack.Private{Scope: "global", Path: "private/global.json"}, nil
	}
	p := squadpack.Private{Project: scope}
	switch {
	case strings.HasPrefix(scope, "project-"):
		p.Scope = "repo"
	case strings.HasPrefix(scope, "place:"):
		p.Scope = "place"
	default:
		return p, packageRefusal(squadpack.ErrPrivateScope)
	}
	h := sha256.Sum256([]byte(scope))
	p.Path = "private/" + hex.EncodeToString(h[:]) + ".json"
	return p, nil
}

func packageScopeSettings(ctx context.Context, tx *sql.Tx, scope string) (squadpack.PrivateDocument, error) {
	rows, err := tx.QueryContext(ctx, `SELECT definition_id,payload FROM squad_settings
	 WHERE scope_id=? ORDER BY definition_id`, scope)
	if err != nil {
		return squadpack.PrivateDocument{}, err
	}
	defer rows.Close()
	doc := squadpack.PrivateDocument{Settings: []squadpack.PrivateSetting{}}
	for rows.Next() {
		var id, payload string
		if err := rows.Scan(&id, &payload); err != nil {
			return doc, err
		}
		doc.Settings = append(doc.Settings, squadpack.PrivateSetting{
			DefinitionID: id, Payload: json.RawMessage(payload)})
	}
	return doc, rows.Err()
}

func (c PackageCatalog) ExportData(ctx context.Context, selected []string) (squadpack.Manifest, map[string][]byte, error) {
	if err := reading(); err != nil {
		return squadpack.Manifest{}, nil, err
	}
	tx, err := c.Store.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return squadpack.Manifest{}, nil, classify(err)
	}
	defer tx.Rollback()
	rows, err := packageActiveRows(ctx, tx)
	if err != nil {
		return squadpack.Manifest{}, nil, classify(err)
	}
	manifest := squadpack.Manifest{Namespace: "catalog.export", Source: "Local squad catalog",
		License: "See each definition", Teams: []squadpack.Definition{},
		Personas: []squadpack.Definition{}, Skills: []squadpack.Definition{}}
	files := map[string][]byte{}
	for _, row := range rows {
		d, body, err := packageExportDefinition(ctx, tx, row)
		if err != nil {
			return squadpack.Manifest{}, nil, err
		}
		if _, exists := files[d.Body]; exists {
			return squadpack.Manifest{}, nil, packageRefusal(squadpack.ErrArchivePathCollision)
		}
		files[d.Body] = body
		switch row.Kind {
		case "team":
			manifest.Teams = append(manifest.Teams, d)
		case "definition":
			manifest.Personas = append(manifest.Personas, d)
		case "skill":
			manifest.Skills = append(manifest.Skills, d)
		}
	}
	seen := map[string]bool{}
	for _, scope := range selected {
		if seen[scope] {
			return squadpack.Manifest{}, nil, packageRefusal(squadpack.ErrPrivateScope)
		}
		seen[scope] = true
		p, err := packagePrivateEntry(scope)
		if err != nil {
			return squadpack.Manifest{}, nil, err
		}
		doc, err := packageScopeSettings(ctx, tx, scope)
		if err != nil {
			return squadpack.Manifest{}, nil, classify(err)
		}
		body, err := json.Marshal(doc)
		if err != nil {
			return squadpack.Manifest{}, nil, err
		}
		manifest.Private = append(manifest.Private, p)
		files[p.Path] = body
	}
	if err := tx.Commit(); err != nil {
		return squadpack.Manifest{}, nil, classify(err)
	}
	return manifest, files, nil
}
