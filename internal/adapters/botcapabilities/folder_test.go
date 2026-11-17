package botcapabilities

import (
	"context"
	"encoding/base64"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/squad"
)

func TestSelectedFolderFingerprintsCoverBinaryAssetsAndRemovedFiles(t *testing.T) {
	skill := squad.Skill{SkillID: "owned-folder", Folder: true, Content: "# Owned fixture\nRead assets/canary.bin.", Files: []squad.SkillFile{{Path: "assets/canary.bin", ContentBase64: base64.StdEncoding.EncodeToString([]byte{0, 255, 1, 128, 42})}}}
	bot := pa.Bot{Key: pa.BotKey{MachineID: "source", BotID: "bot"}, Project: pa.ProjectScope{MachineID: "source", ProjectID: "project", CanonicalID: "canonical", Kind: pa.PlaceProject}, Agent: pa.Codex}
	i := Inventory{State: t.TempDir(), Skills: func(context.Context) ([]squad.Skill, error) { return []squad.Skill{skill}, nil }}
	catalog, e := i.Catalog(context.Background(), bot, "")
	if e != nil || len(catalog.Items) != 1 {
		t.Fatal(catalog, e)
	}
	ref := pa.ExtensionRef{ID: catalog.Items[0].ID, Fingerprint: catalog.Items[0].Fingerprint}
	bot.Extensions.Skills = []pa.ExtensionRef{ref}
	resolved, e := i.Resolve(context.Background(), bot, "")
	if e != nil || len(resolved.Folders) != 1 || resolved.Skills != "" {
		t.Fatal(resolved, e)
	}
	if resolved.Folders[0].Files[0].ContentBase64 != skill.Files[0].ContentBase64 {
		t.Fatal("binary asset bytes changed")
	}
	skill.Files[0].ContentBase64 = base64.StdEncoding.EncodeToString([]byte{0, 255, 2, 128, 42})
	if _, e = i.Resolve(context.Background(), bot, ""); pa.Code(e) != pa.Revoked {
		t.Fatal("changed asset retained stale authority", e)
	}
	changed, e := i.Catalog(context.Background(), bot, "")
	if e != nil || changed.Items[0].Fingerprint == ref.Fingerprint {
		t.Fatal("asset bytes absent from fingerprint", e)
	}
	skill.Files = nil
	if _, e = i.Resolve(context.Background(), bot, ""); pa.Code(e) != pa.Revoked {
		t.Fatal("removed asset retained stale authority", e)
	}
	bot.Extensions.Skills = nil
	resolved, e = i.Resolve(context.Background(), bot, "")
	if e != nil || len(resolved.Folders) != 0 || resolved.Skills != "" {
		t.Fatal("deselected folder retained guidance", resolved, e)
	}
}

func TestFolderCatalogRejectsUnsafeAndCollidingAssetPaths(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("OWNED_ASSET_CANARY"))
	for _, paths := range [][]string{{"../escape"}, {"/absolute"}, {"assets/../escape"}, {"assets\\escape"}, {"SKILL.md"}, {"canary", "CANARY"}, {"canary", "canary/nested"}} {
		t.Run(paths[0], func(t *testing.T) {
			files := []squad.SkillFile{}
			for _, path := range paths {
				files = append(files, squad.SkillFile{Path: path, ContentBase64: encoded})
			}
			i := Inventory{State: t.TempDir(), Skills: func(context.Context) ([]squad.Skill, error) {
				return []squad.Skill{{SkillID: "unsafe", Folder: true, Content: "# Fixture", Files: files}}, nil
			}}
			bot := pa.Bot{Key: pa.BotKey{MachineID: "source", BotID: "bot"}, Project: pa.ProjectScope{MachineID: "source", ProjectID: "project", CanonicalID: "canonical", Kind: pa.PlaceProject}, Agent: pa.Codex}
			if _, e := i.Catalog(context.Background(), bot, ""); e == nil {
				t.Fatal("unsafe asset path offered for selection")
			}
		})
	}
}

func TestFolderCatalogRejectsInvalidBinaryManifest(t *testing.T) {
	for _, skill := range []squad.Skill{{SkillID: "invalid-base64", Folder: true, Content: "# Fixture", Files: []squad.SkillFile{{Path: "canary.bin", ContentBase64: "%%%"}}}, {SkillID: "assets-without-folder", Content: "# Fixture", Files: []squad.SkillFile{{Path: "canary.bin", ContentBase64: "AA=="}}}} {
		t.Run(skill.SkillID, func(t *testing.T) {
			i := Inventory{State: t.TempDir(), Skills: func(context.Context) ([]squad.Skill, error) { return []squad.Skill{skill}, nil }}
			bot := pa.Bot{Key: pa.BotKey{MachineID: "source", BotID: "bot"}, Project: pa.ProjectScope{MachineID: "source", ProjectID: "project", CanonicalID: "canonical", Kind: pa.PlaceProject}, Agent: pa.Codex}
			if _, e := i.Catalog(context.Background(), bot, ""); e == nil {
				t.Fatal("invalid asset manifest offered")
			}
		})
	}
}
