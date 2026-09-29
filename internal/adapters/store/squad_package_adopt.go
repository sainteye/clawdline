package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/sainteye/clawdline/internal/app/squadpackages"
	"github.com/sainteye/clawdline/internal/domain/squad"
	"github.com/sainteye/clawdline/internal/domain/squadpack"
)

func packageNames(en, zh string) squad.Names {
	if zh == "" {
		zh = en
	}
	return squad.Names{En: en, ZhHant: zh}
}

func packageSource(def squadpack.Definition, manifest squadpack.Manifest) (string, string) {
	source, license := def.Source, def.License
	if source == "" {
		source = manifest.Source
	}
	if license == "" {
		license = manifest.License
	}
	return source, license
}

func packageRows(pkg squadpack.Package, changes []squadpack.Change, existing []squadpack.Existing) ([]SquadEntityRow, error) {
	selected := map[string]bool{}
	for _, c := range changes {
		if c.Action == "add" || c.Action == "update" {
			selected[c.ID] = true
		}
	}
	versions := map[string]string{}
	for _, d := range pkg.Manifest.Teams {
		versions[d.ID] = d.Version
	}
	for _, d := range pkg.Manifest.Personas {
		versions[d.ID] = d.Version
	}
	for _, d := range pkg.Manifest.Skills {
		versions[d.ID] = d.Version
	}
	for _, change := range changes {
		if change.Action != "conflict" {
			continue
		}
		for _, old := range existing {
			if old.Kind == change.Kind && old.ID == change.ID {
				versions[change.ID] = old.Version
				break
			}
		}
	}
	activeTeams := map[string]bool{}
	for _, old := range existing {
		if old.Kind == "team" {
			activeTeams[old.ID] = true
		}
	}
	for _, change := range changes {
		if change.Kind == "team" && change.Action != "conflict" {
			activeTeams[change.ID] = true
		}
	}
	rows := []SquadEntityRow{}
	for _, d := range pkg.Manifest.Skills {
		if !selected[d.ID] {
			continue
		}
		source, license := packageSource(d, pkg.Manifest)
		purpose := d.Purpose
		if purpose == "" {
			purpose = d.Name
		}
		value := squad.Skill{SkillID: d.ID, Version: d.Version, Source: source,
			License: license, Name: packageNames(d.Name, d.NameZhHant),
			Purpose: packageNames(purpose, d.PurposeZhHant),
			Icon:    squad.Icon{Accent: d.Icon.Accent, Cells: d.Icon.Cells},
			Content: string(pkg.Files[d.Body])}
		value.Digest = squad.Digest(value)
		payload, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		rows = append(rows, SquadEntityRow{Kind: "skill", ID: d.ID, Version: d.Version,
			Digest: value.Digest, Payload: payload})
	}
	for _, d := range pkg.Manifest.Personas {
		if !selected[d.ID] {
			continue
		}
		source, license := packageSource(d, pkg.Manifest)
		skills := []squad.SkillReference{}
		disabled := map[string]bool{}
		for _, id := range d.DisabledSkills {
			disabled[id] = true
		}
		for _, id := range d.Skills {
			skills = append(skills, squad.SkillReference{ID: id, Version: versions[id], Enabled: !disabled[id]})
		}
		teams := []squad.Reference{}
		for _, team := range pkg.Manifest.Teams {
			for _, id := range team.Personas {
				if id == d.ID && activeTeams[team.ID] {
					teams = append(teams, squad.Reference{ID: team.ID, Version: versions[team.ID]})
				}
			}
		}
		value := squad.Definition{DefinitionID: d.ID, Version: d.Version, Source: source,
			License: license, Name: packageNames(d.Name, d.NameZhHant),
			Summary: packageNames(d.Summary, d.SummaryZhHant), Body: string(pkg.Files[d.Body]),
			Icon: squad.Icon{Accent: d.Icon.Accent, Cells: d.Icon.Cells}, Teams: teams, Skills: skills}
		value.Digest = squad.Digest(value)
		payload, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		rows = append(rows, SquadEntityRow{Kind: "definition", ID: d.ID, Version: d.Version,
			Digest: value.Digest, Payload: payload})
	}
	for _, d := range pkg.Manifest.Teams {
		if !selected[d.ID] {
			continue
		}
		source, license := packageSource(d, pkg.Manifest)
		refs := []squad.Reference{}
		for _, id := range d.Personas {
			refs = append(refs, squad.Reference{ID: id, Version: versions[id]})
		}
		value := squad.Team{TeamID: d.ID, Version: d.Version, Source: source,
			License: license, Name: packageNames(d.Name, d.NameZhHant),
			Icon: squad.Icon{Accent: d.Icon.Accent, Cells: d.Icon.Cells}, Personas: refs}
		value.Digest = squad.Digest(value)
		payload, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		rows = append(rows, SquadEntityRow{Kind: "team", ID: d.ID, Version: d.Version,
			Digest: value.Digest, Payload: payload})
	}
	return rows, nil
}

func packageDefinition(pkg squadpack.Package, kind, id string) (squadpack.Definition, bool) {
	var defs []squadpack.Definition
	switch kind {
	case "definition":
		defs = pkg.Manifest.Personas
	case "team":
		defs = pkg.Manifest.Teams
	case "skill":
		defs = pkg.Manifest.Skills
	}
	for _, d := range defs {
		if d.ID == id {
			return d, true
		}
	}
	return squadpack.Definition{}, false
}

func packageReceiptTx(ctx context.Context, tx *sql.Tx, actor, key, fingerprint string) (squadpackages.Receipt, bool, error) {
	var stored, raw string
	err := tx.QueryRowContext(ctx, `SELECT fingerprint,receipt FROM squad_package_receipts
	 WHERE actor=? AND request_id=?`, actor, key).Scan(&stored, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return squadpackages.Receipt{}, false, nil
	}
	if err != nil {
		return squadpackages.Receipt{}, false, err
	}
	if stored != fingerprint {
		return squadpackages.Receipt{}, false, ErrSquadReceiptConflict
	}
	var receipt squadpackages.Receipt
	if err := json.Unmarshal([]byte(raw), &receipt); err != nil {
		return receipt, false, err
	}
	return receipt, true, nil
}

func packageCurrentVersion(ctx context.Context, tx *sql.Tx) (int64, error) {
	var version int64
	err := tx.QueryRowContext(ctx, `SELECT version FROM squad_catalog_state WHERE singleton=1`).Scan(&version)
	return version, err
}

func (c PackageCatalog) Adopt(ctx context.Context, a squadpackages.Adoption) (squadpackages.Receipt, error) {
	var out squadpackages.Receipt
	err := c.Store.write(ctx, func(tx *sql.Tx) (int64, error) {
		if prior, found, err := packageReceiptTx(ctx, tx, a.Actor, a.RequestID, a.Fingerprint); err != nil {
			return 0, err
		} else if found {
			out = prior
			return 0, nil
		}
		var grant squadpackages.PreviewGrant
		var expires int64
		err := tx.QueryRowContext(ctx, `SELECT actor,scope,archive_digest,catalog_version,
		 settings_digest,preview_digest,expires_at FROM squad_package_previews WHERE token_hash=?`,
			a.TokenHash).Scan(&grant.Actor, &grant.Scope, &grant.ArchiveDigest,
			&grant.CatalogVersion, &grant.SettingsDigest, &grant.PreviewDigest, &expires)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, packageRefusal(squadpackages.ErrPreviewInvalid)
		}
		if err != nil {
			return 0, err
		}
		if a.At.Unix() >= expires {
			return 0, packageRefusal("preview_expired")
		}
		if grant.Actor != a.Actor || grant.Scope != a.Scope ||
			grant.ArchiveDigest != a.ArchiveDigest || grant.PreviewDigest != a.PreviewDigest ||
			grant.CatalogVersion != a.ExpectedVersion {
			return 0, packageRefusal(squadpackages.ErrPreviewInvalid)
		}
		current, err := packageCurrentVersion(ctx, tx)
		if err != nil {
			return 0, err
		}
		if current != a.ExpectedVersion {
			return 0, SquadVersionConflict{Current: current}
		}
		settingsDigest, err := packageSettingsDigest(ctx, tx)
		if err != nil {
			return 0, err
		}
		if settingsDigest != grant.SettingsDigest {
			return 0, packageRefusal("settings_changed")
		}
		active, err := packageActiveRows(ctx, tx)
		if err != nil {
			return 0, err
		}
		existing, err := packageExisting(ctx, tx, active)
		if err != nil {
			return 0, err
		}
		preview, err := squadpack.Analyze(a.Package, current, a.Scope, existing)
		if err != nil {
			return 0, err
		}
		if preview.Digest != a.PreviewDigest {
			return 0, packageRefusal(squadpackages.ErrPreviewInvalid)
		}
		if err := squadpack.ValidateChoices(a.Package, preview, a.Choices, existing); err != nil {
			return 0, err
		}
		rows, err := packageRows(a.Package, preview.Changes, existing)
		if err != nil {
			return 0, err
		}
		private, err := packageSelectedSettings(a.Package, a.PrivateScopes, a.ConfirmPrivate)
		if err != nil {
			return 0, err
		}
		if err := packageValidateSettings(ctx, tx, private, existing, rows); err != nil {
			return 0, err
		}
		limits := c.limits()
		if n, err := packageReceiptCount(ctx, tx); err != nil {
			return 0, err
		} else if n >= limits.Receipts {
			return 0, ErrSquadCapacityFull
		}
		next, changed, err := saveSquadEntitiesTx(ctx, tx, rows, current, limits)
		if err != nil {
			return 0, err
		}
		for _, row := range rows {
			def, ok := packageDefinition(a.Package, row.Kind, row.ID)
			if !ok {
				return 0, packageRefusal(squadpack.ErrInvalidDefinition)
			}
			raw, _ := json.Marshal(def)
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO squad_package_sources
			 (kind,id,version,definition,body) VALUES(?,?,?,?,?)`,
				row.Kind, row.ID, row.Version, string(raw), string(a.Package.Files[def.Body])); err != nil {
				return 0, err
			}
		}
		for _, item := range private {
			var expected int64
			err := tx.QueryRowContext(ctx, `SELECT version FROM squad_settings WHERE scope_id=? AND definition_id=?`,
				item.scope, item.row.DefinitionID).Scan(&expected)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return 0, err
			}
			if _, err := saveSquadSettingTx(ctx, tx, item.scope, item.row.DefinitionID,
				expected, item.row.Payload, limits); err != nil {
				return 0, err
			}
		}
		out = squadpackages.Receipt{CatalogVersion: next, ArchiveDigest: a.ArchiveDigest,
			AdoptedIDs: []string{}, PrivateScopes: append([]string{}, a.PrivateScopes...)}
		for _, row := range rows {
			out.AdoptedIDs = append(out.AdoptedIDs, row.ID)
		}
		raw, _ := json.Marshal(out)
		if _, err := tx.ExecContext(ctx, `INSERT INTO squad_package_receipts
		 (actor,request_id,fingerprint,receipt) VALUES(?,?,?,?)`,
			a.Actor, a.RequestID, a.Fingerprint, string(raw)); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM squad_package_previews WHERE token_hash=?`, a.TokenHash); err != nil {
			return 0, err
		}
		return changed + int64(len(private)) + int64(len(rows)) + 2, nil
	})
	return out, err
}

type packageSetting struct {
	scope string
	row   squadpack.PrivateSetting
}

func packageSelectedSettings(pkg squadpack.Package, selected []string, confirmed bool) ([]packageSetting, error) {
	if len(selected) > 0 && !confirmed {
		return nil, packageRefusal(squadpack.ErrPrivateConfirmation)
	}
	entries := map[string]squadpack.Private{}
	for _, p := range pkg.Manifest.Private {
		entries[packageScope(p)] = p
	}
	seen := map[string]bool{}
	out := []packageSetting{}
	for _, scope := range selected {
		p, ok := entries[scope]
		if !ok || seen[scope] {
			return nil, packageRefusal(squadpack.ErrPrivateScope)
		}
		seen[scope] = true
		doc, err := squadpack.ParsePrivateDocument(pkg.Files[p.Path])
		if err != nil {
			return nil, err
		}
		for _, row := range doc.Settings {
			out = append(out, packageSetting{scope: scope, row: row})
		}
	}
	return out, nil
}

func packageReceiptCount(ctx context.Context, tx *sql.Tx) (int64, error) {
	var total int64
	err := tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM squad_receipts)+
	 (SELECT COUNT(*) FROM squad_package_receipts)`).Scan(&total)
	return total, err
}

func packageValidateSettings(ctx context.Context, tx *sql.Tx, selected []packageSetting,
	existing []squadpack.Existing, adopted []SquadEntityRow) error {
	personas := map[string]bool{}
	skills := map[string]bool{}
	for _, old := range existing {
		if old.Kind == "persona" {
			personas[old.ID] = true
		}
		if old.Kind == "skill" {
			skills[old.ID+"\x00"+old.Version] = true
		}
	}
	for _, row := range adopted {
		if row.Kind == "definition" {
			personas[row.ID] = true
		}
		if row.Kind == "skill" {
			skills[row.ID+"\x00"+row.Version] = true
		}
	}
	for _, item := range selected {
		if item.row.DefinitionID == "" {
			continue
		}
		if !personas[item.row.DefinitionID] {
			return packageRefusal(squadpack.ErrMissingReference)
		}
		var override squad.Override
		if err := json.Unmarshal(item.row.Payload, &override); err != nil {
			return packageRefusal(squadpack.ErrManifestInvalid)
		}
		if override.Skills == nil {
			continue
		}
		for _, ref := range *override.Skills {
			if skills[ref.ID+"\x00"+ref.Version] {
				continue
			}
			var present int
			err := tx.QueryRowContext(ctx, `SELECT 1 FROM squad_entities WHERE kind='skill' AND id=? AND version=?`,
				ref.ID, ref.Version).Scan(&present)
			if errors.Is(err, sql.ErrNoRows) {
				return packageRefusal(squadpack.ErrMissingReference)
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
}
