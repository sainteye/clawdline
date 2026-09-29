package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/squad"
)

func TestSquadSettingsPersistConflictAndRetry(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	limits := DefaultSquadLimits()
	global := json.RawMessage(`{"handbook":"global","auto_assign":true,"skills":[]}`)
	project := json.RawMessage(`{"handbook":"","auto_assign":false,"skills":[{"id":"x.skill.one","version":"1","enabled":true},{"id":"x.skill.two","version":"1","enabled":false}]}`)
	if v, err := s.SaveSquadSetting(ctx, "global", "clawdline.persona.backend", 0, global, "global-1", limits); err != nil || v != 1 {
		t.Fatalf("global save = %d, %v", v, err)
	}
	if v, err := s.SaveSquadSetting(ctx, "repo:a", "clawdline.persona.backend", 0, project, "a-1", limits); err != nil || v != 1 {
		t.Fatalf("project a save = %d, %v", v, err)
	}
	if v, err := s.SaveSquadSetting(ctx, "repo:b", "clawdline.persona.backend", 0,
		json.RawMessage(`{"handbook":"b"}`), "b-1", limits); err != nil || v != 1 {
		t.Fatalf("project b save = %d, %v", v, err)
	}
	if v, err := s.SaveSquadSetting(ctx, "repo:a", "clawdline.persona.backend", 0, project, "a-1", limits); err != nil || v != 1 {
		t.Fatalf("replay = %d, %v", v, err)
	}
	if _, err := s.SaveSquadSetting(ctx, "repo:a", "clawdline.persona.backend", 0,
		json.RawMessage(`{"handbook":"changed"}`), "a-1", limits); !errors.Is(err, ErrSquadReceiptConflict) {
		t.Fatalf("reused key: %v", err)
	}
	if _, err := s.SaveSquadSetting(ctx, "repo:a", "clawdline.persona.backend", 0,
		json.RawMessage(`{"handbook":"changed"}`), "a-2", limits); err == nil {
		t.Fatal("stale write passed")
	} else if conflict, ok := err.(SquadVersionConflict); !ok || conflict.Current != 1 {
		t.Fatalf("conflict = %v", err)
	}
	_ = s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for scope, want := range map[string]string{
		"global": "global", "repo:a": "", "repo:b": "b",
	} {
		rows, err := s.SquadSettings(ctx, scope)
		if err != nil || len(rows) != 1 {
			t.Fatalf("%s rows = %v, %v", scope, rows, err)
		}
		var got struct {
			Handbook string `json:"handbook"`
		}
		if err := json.Unmarshal(rows[0].Payload, &got); err != nil || got.Handbook != want {
			t.Fatalf("%s handbook = %q, %v", scope, got.Handbook, err)
		}
	}
	if v, err := s.SaveSquadSetting(ctx, "repo:a", "clawdline.persona.backend", 0, project, "a-1", limits); err != nil || v != 1 {
		t.Fatalf("receipt after restart = %d, %v", v, err)
	}
}

func TestSquadDefinitionVersionsAreImmutable(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	row := SquadEntityRow{Kind: "skill", ID: "example.skill.one", Version: "1",
		Digest: "first", Payload: json.RawMessage(`{"skill_id":"example.skill.one","content":"one"}`)}
	if v, err := s.SaveSquadEntity(ctx, row, 0, "entity-1", DefaultSquadLimits()); err != nil || v != 1 {
		t.Fatalf("save = %d, %v", v, err)
	}
	changed := row
	changed.Digest = "second"
	if _, err := s.SaveSquadEntity(ctx, changed, 1, "entity-2", DefaultSquadLimits()); !errors.Is(err, ErrSquadVersionContent) {
		t.Fatalf("same version changed: %v", err)
	}
	changed.Version = "2"
	if v, err := s.SaveSquadEntity(ctx, changed, 1, "entity-3", DefaultSquadLimits()); err != nil || v != 2 {
		t.Fatalf("next version = %d, %v", v, err)
	}
	if old, ok, err := s.SquadEntityVersion(ctx, "skill", row.ID, "1"); err != nil || !ok || old.Digest != "first" {
		t.Fatalf("old version = %+v, %t, %v", old, ok, err)
	}
	if version, rows, err := s.SquadCatalogRows(ctx, false); err != nil || version != 2 || len(rows) != 1 || rows[0].Version != "2" {
		t.Fatalf("current catalog = %d, %+v, %v", version, rows, err)
	}
}

func TestSquadPatchTouchesOnlyNamedFieldsAndSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	text := ""
	falseValue := false
	if v, err := s.PatchSquadSetting(ctx, "project-a", "clawdline.persona.backend", 0,
		squad.Patch{SetHandbook: true, Handbook: &text}, "patch-1", DefaultSquadLimits()); err != nil || v != 1 {
		t.Fatalf("first patch = %d, %v", v, err)
	}
	if v, err := s.PatchSquadSetting(ctx, "project-a", "clawdline.persona.backend", 1,
		squad.Patch{SetAutoAssign: true, AutoAssign: &falseValue}, "patch-2", DefaultSquadLimits()); err != nil || v != 2 {
		t.Fatalf("second patch = %d, %v", v, err)
	}
	if _, err := s.PatchSquadSetting(ctx, "project-a", "clawdline.persona.backend", 1,
		squad.Patch{SetHandbook: true}, "patch-stale", DefaultSquadLimits()); err == nil {
		t.Fatal("stale patch passed")
	}
	_ = s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows, err := s.SquadSettings(ctx, "project-a")
	if err != nil || len(rows) != 1 || rows[0].Version != 2 {
		t.Fatalf("rows = %+v, %v", rows, err)
	}
	var got squad.Override
	if err := json.Unmarshal(rows[0].Payload, &got); err != nil ||
		got.Handbook == nil || *got.Handbook != "" || got.AutoAssign == nil || *got.AutoAssign {
		t.Fatalf("merged override = %+v, %v", got, err)
	}
	if v, err := s.PatchSquadSetting(ctx, "project-a", "clawdline.persona.backend", 1,
		squad.Patch{SetAutoAssign: true, AutoAssign: &falseValue}, "patch-2", DefaultSquadLimits()); err != nil || v != 2 {
		t.Fatalf("receipt after restart = %d, %v", v, err)
	}
}

func TestSquadBatchRejectsOneConflictWithoutPublishingAnyRow(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	one := SquadEntityRow{Kind: "skill", ID: "example.skill.one", Version: "1", Digest: "one",
		Payload: json.RawMessage(`{"skill_id":"example.skill.one","content":"one"}`)}
	two := SquadEntityRow{Kind: "skill", ID: "example.skill.two", Version: "1", Digest: "two",
		Payload: json.RawMessage(`{"skill_id":"example.skill.two","content":"two"}`)}
	if _, err := s.SaveSquadEntity(ctx, two, 0, "seed", DefaultSquadLimits()); err != nil {
		t.Fatal(err)
	}
	bad := two
	bad.Digest = "changed"
	if _, err := s.SaveSquadEntities(ctx, []SquadEntityRow{one, bad}, 1,
		"batch-failed", DefaultSquadLimits()); !errors.Is(err, ErrSquadVersionContent) {
		t.Fatalf("batch conflict = %v", err)
	}
	if _, ok, err := s.SquadEntityVersion(ctx, one.Kind, one.ID, one.Version); err != nil || ok {
		t.Fatalf("partial row = %t, %v", ok, err)
	}
	if version, rows, err := s.SquadCatalogRows(ctx, false); err != nil || version != 1 || len(rows) != 1 {
		t.Fatalf("partial catalog = %d, %+v, %v", version, rows, err)
	}
	if v, err := s.SaveSquadEntities(ctx, []SquadEntityRow{one, two}, 1,
		"batch-good", DefaultSquadLimits()); err != nil || v != 2 {
		t.Fatalf("good batch = %d, %v", v, err)
	}
	if v, err := s.SaveSquadEntities(ctx, []SquadEntityRow{one, two}, 1,
		"batch-good", DefaultSquadLimits()); err != nil || v != 2 {
		t.Fatalf("batch replay = %d, %v", v, err)
	}
	if counts, err := s.SquadUsage(ctx); err != nil || counts.Entities != 2 || counts.Receipts != 2 {
		t.Fatalf("capacity usage = %+v, %v", counts, err)
	}
}
