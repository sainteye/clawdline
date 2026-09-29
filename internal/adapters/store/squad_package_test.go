package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/squadpackages"
	"github.com/sainteye/clawdline/internal/domain/squad"
	"github.com/sainteye/clawdline/internal/domain/squadpack"
)

func packageTestIcon() squadpack.Icon {
	cells := make([][]*string, 7)
	for y := range cells {
		cells[y] = make([]*string, 8)
	}
	return squadpack.Icon{Accent: "#123456", Cells: cells}
}

func packageTestArchive(t *testing.T, private bool) []byte {
	t.Helper()
	icon := packageTestIcon()
	m := squadpack.Manifest{Namespace: "example.squad", Source: "author", License: "CC0-1.0",
		Teams: []squadpack.Definition{{ID: "example.squad.team.alpha", Version: "1.0.0", Name: "Alpha",
			Icon: icon, Body: "definitions/team/alpha.md", Personas: []string{"example.squad.persona.writer"}}},
		Personas: []squadpack.Definition{{ID: "example.squad.persona.writer", Version: "1.0.0", Name: "Writer",
			NameZhHant: "寫作者", Summary: "Writes drafts", Icon: icon,
			Body: "definitions/persona/writer.md", Skills: []string{"example.squad.skill.draft"}}},
		Skills: []squadpack.Definition{{ID: "example.squad.skill.draft", Version: "1.0.0", Name: "Draft",
			Purpose: "Draft text", Icon: icon, Body: "definitions/skill/draft.md"}}}
	files := map[string][]byte{
		"definitions/team/alpha.md":     []byte("# Alpha\n"),
		"definitions/persona/writer.md": []byte("# Writer\n"),
		"definitions/skill/draft.md":    []byte("# Draft\n"),
	}
	if private {
		m.Private = []squadpack.Private{{Scope: "repo", Project: "project-abc", Path: "private/project.json"}}
		files["private/project.json"] = []byte(`{"settings":[{"definition_id":"example.squad.persona.writer","payload":{"handbook":"PROJECT_SECRET_MARKER"}}]}`)
	}
	archive, err := squadpack.Build(m, files)
	if err != nil {
		t.Fatal(err)
	}
	return archive
}

func packageTestInput(preview squadpackages.PreviewResult, archive []byte, key string, private bool) squadpackages.AdoptInput {
	in := squadpackages.AdoptInput{Actor: "local", Scope: "global", RequestID: key,
		PreviewToken: preview.Token, ArchiveDigest: preview.ArchiveDigest,
		PreviewDigest: preview.Digest, ExpectedVersion: preview.CatalogVersion,
		Archive: archive, Choices: map[string]string{}}
	if private {
		in.PrivateScopes = []string{"project-abc"}
		in.ConfirmPrivate = true
	}
	return in
}

func TestPackageAdoptionIsAtomicIdempotentAndPrivateSafeAcrossRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	archive := packageTestArchive(t, true)
	service := squadpackages.Service{Catalog: PackageCatalog{Store: s}}
	preview, err := service.Preview(ctx, "local", "global", archive)
	if err != nil {
		t.Fatal(err)
	}
	input := packageTestInput(preview, archive, "adopt-1", true)
	if _, err := s.db.Exec(`CREATE TRIGGER fail_package_source BEFORE INSERT ON squad_package_sources
	 WHEN NEW.id='example.squad.persona.writer' BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Adopt(ctx, input); err == nil {
		t.Fatal("injected Nth source write succeeded")
	}
	if version, rows, err := s.SquadCatalogRows(ctx, false); err != nil || version != 0 || len(rows) != 0 {
		t.Fatalf("partial catalog after failure: %d, %d, %v", version, len(rows), err)
	}
	if rows, err := s.SquadSettings(ctx, "project-abc"); err != nil || len(rows) != 0 {
		t.Fatalf("partial private settings after failure: %+v, %v", rows, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_package_source`); err != nil {
		t.Fatal(err)
	}
	receipt, err := service.Adopt(ctx, input)
	if err != nil || receipt.CatalogVersion != 1 || len(receipt.AdoptedIDs) != 3 {
		t.Fatalf("adoption = %+v, %v", receipt, err)
	}
	active, err := packageActiveRows(ctx, s.db)
	if err != nil || len(active) != 3 {
		t.Fatalf("active rows after adoption = %d, %v", len(active), err)
	}
	for _, row := range active {
		var digest, recalculated string
		switch row.Kind {
		case "definition":
			var value squad.Definition
			if err := json.Unmarshal(row.Payload, &value); err != nil {
				t.Fatal(err)
			}
			digest, value.Digest = value.Digest, ""
			recalculated = squad.Digest(value)
		case "team":
			var value squad.Team
			if err := json.Unmarshal(row.Payload, &value); err != nil {
				t.Fatal(err)
			}
			digest, value.Digest = value.Digest, ""
			recalculated = squad.Digest(value)
		case "skill":
			var value squad.Skill
			if err := json.Unmarshal(row.Payload, &value); err != nil {
				t.Fatal(err)
			}
			digest, value.Digest = value.Digest, ""
			recalculated = squad.Digest(value)
		}
		if row.Digest != digest || digest != recalculated {
			t.Fatalf("%s %s has inconsistent catalog digest", row.Kind, row.ID)
		}
	}
	again, err := service.Preview(ctx, "local", "global", archive)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range again.Changes {
		if change.Action != "unchanged" {
			t.Fatalf("adopted package is no longer unchanged: %+v", change)
		}
	}
	public, err := service.Export(ctx, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(public, []byte("PROJECT_SECRET_MARKER")) {
		t.Fatal("private handbook leaked into default export")
	}
	parsed, err := squadpack.Parse(public)
	if err != nil || len(parsed.Manifest.Private) != 0 || len(parsed.Manifest.Teams) != 1 ||
		len(parsed.Manifest.Personas) != 1 || len(parsed.Manifest.Skills) != 1 {
		t.Fatalf("public roundtrip = %+v, %v", parsed.Manifest, err)
	}
	if got := parsed.Manifest.Personas[0]; got.ID != "example.squad.persona.writer" ||
		got.Version != "1.0.0" || got.Icon.Accent != "#123456" ||
		len(got.Skills) != 1 || got.Skills[0] != "example.squad.skill.draft" {
		t.Fatalf("roundtrip changed definition: %+v", got)
	}
	selected, err := service.Export(ctx, []string{"project-abc"}, true)
	if err != nil || !bytes.Contains(selected, []byte("PROJECT_SECRET_MARKER")) {
		t.Fatalf("selected private export: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	service.Catalog = PackageCatalog{Store: s}
	replayed, err := service.Adopt(ctx, input)
	if err != nil || replayed.CatalogVersion != receipt.CatalogVersion || len(replayed.AdoptedIDs) != 3 {
		t.Fatalf("restart replay = %+v, %v", replayed, err)
	}
	input.ArchiveDigest = "sha256:different"
	if _, err := service.Adopt(ctx, input); !errors.Is(err, ErrSquadReceiptConflict) {
		t.Fatalf("different bytes reused key: %v", err)
	}
}

func TestPackageAdoptionRejectsChangedCatalogAfterPreview(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	service := squadpackages.Service{Catalog: PackageCatalog{Store: s}}
	archive := packageTestArchive(t, false)
	preview, err := service.Preview(ctx, "local", "global", archive)
	if err != nil {
		t.Fatal(err)
	}
	row := SquadEntityRow{Kind: "skill", ID: "other.skill.one", Version: "1", Digest: "digest",
		Payload: json.RawMessage(`{"skill_id":"other.skill.one","name":{"en":"Other"}}`)}
	if _, err := s.SaveSquadEntity(ctx, row, 0, "other-1", DefaultSquadLimits()); err != nil {
		t.Fatal(err)
	}
	_, err = service.Adopt(ctx, packageTestInput(preview, archive, "adopt-stale", false))
	var conflict SquadVersionConflict
	if !errors.As(err, &conflict) || conflict.Current != 1 {
		t.Fatalf("stale adoption = %v", err)
	}
	if version, rows, err := s.SquadCatalogRows(ctx, false); err != nil || version != 1 || len(rows) != 1 {
		t.Fatalf("stale adoption wrote package: %d, %d, %v", version, len(rows), err)
	}
}

func TestPackageAdoptionRejectsChangedSettingsAfterPreview(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	service := squadpackages.Service{Catalog: PackageCatalog{Store: s}}
	archive := packageTestArchive(t, true)
	preview, err := service.Preview(ctx, "local", "global", archive)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveSquadSetting(ctx, "project-abc", "clawdline.persona.backend", 0,
		json.RawMessage(`{"handbook":"new"}`), "setting-1", DefaultSquadLimits()); err != nil {
		t.Fatal(err)
	}
	_, err = service.Adopt(ctx, packageTestInput(preview, archive, "adopt-settings-stale", true))
	var refusal *squadpack.Refusal
	if !errors.As(err, &refusal) || refusal.Code != "settings_changed" {
		t.Fatalf("settings change = %v", err)
	}
	if version, rows, err := s.SquadCatalogRows(ctx, false); err != nil || version != 0 || len(rows) != 0 {
		t.Fatalf("settings conflict wrote catalog: %d, %d, %v", version, len(rows), err)
	}
}

func TestPackageAdoptionRejectsExpiredAndOtherActorsPreview(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	service := squadpackages.Service{Catalog: PackageCatalog{Store: s}, Now: func() time.Time { return now }}
	archive := packageTestArchive(t, false)
	preview, err := service.Preview(ctx, "local", "global", archive)
	if err != nil {
		t.Fatal(err)
	}
	input := packageTestInput(preview, archive, "adopt-other-actor", false)
	input.Actor = "other"
	_, err = service.Adopt(ctx, input)
	var refusal *squadpack.Refusal
	if !errors.As(err, &refusal) || refusal.Code != squadpackages.ErrPreviewInvalid {
		t.Fatalf("another actor used preview: %v", err)
	}
	service.Now = func() time.Time { return now.Add(15 * time.Minute) }
	input = packageTestInput(preview, archive, "adopt-expired", false)
	_, err = service.Adopt(ctx, input)
	if !errors.As(err, &refusal) || refusal.Code != "preview_expired" {
		t.Fatalf("expired preview: %v", err)
	}
	if version, rows, err := s.SquadCatalogRows(ctx, false); err != nil || version != 0 || len(rows) != 0 {
		t.Fatalf("rejected preview wrote catalog: %d, %d, %v", version, len(rows), err)
	}
}
