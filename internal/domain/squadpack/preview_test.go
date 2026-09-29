package squadpack

import "testing"

func examplePackage(t *testing.T) Package {
	t.Helper()
	m, files := example()
	b, err := Build(m, files)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAnalyzeNamesChangesAndImpactedReferences(t *testing.T) {
	p := examplePackage(t)
	old := []Existing{
		{Kind: "skill", ID: "example.squad.skill.draft", Version: "1.0.0", Name: "Draft", Digest: definitionDigest(p.Manifest.Skills[0])},
		{Kind: "persona", ID: "other.persona.writer", Version: "1.0.0", Name: "Other", References: []string{"example.squad.skill.draft"}},
	}
	preview, err := Analyze(p, 7, "global", old)
	if err != nil {
		t.Fatal(err)
	}
	if preview.CatalogVersion != 7 || preview.ArchiveDigest != p.Digest || preview.Digest == "" {
		t.Fatalf("incomplete preview: %+v", preview)
	}
	found := false
	for _, change := range preview.Changes {
		if change.ID == "example.squad.skill.draft" {
			found = true
			if change.Action != "unchanged" || len(change.Dependents) != 1 || change.Dependents[0] != "other.persona.writer" {
				t.Fatalf("incorrect impact: %+v", change)
			}
		}
	}
	if !found {
		t.Fatal("skill absent from preview")
	}
}

func TestConflictChoicesCannotLeaveMissingDependency(t *testing.T) {
	p := examplePackage(t)
	old := []Existing{{Kind: "skill", ID: "other.skill.draft", Version: "1.0.0", Name: "Draft"}}
	preview, err := Analyze(p, 4, "global", old)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateChoices(p, preview, nil, old); refusalCode(t, err) != ErrChoiceInvalid {
		t.Fatal(err)
	}
	if err := ValidateChoices(p, preview, map[string]string{"example.squad.skill.draft": "keep"}, old); refusalCode(t, err) != ErrReferenceConflict {
		t.Fatal(err)
	}
	if err := ValidateChoices(p, preview, map[string]string{"example.squad.skill.draft": "use"}, old); refusalCode(t, err) != ErrChoiceInvalid {
		t.Fatal(err)
	}
}

func TestPreviewDetectsChangedVersionContentAndDowngrade(t *testing.T) {
	p := examplePackage(t)
	d := p.Manifest.Skills[0]
	for _, tc := range []struct{ version, digest, code string }{
		{"1.0.0", "different", "version_content_conflict"},
		{"2.0.0", "different", "version_downgrade"},
	} {
		preview, err := Analyze(p, 2, "global", []Existing{{Kind: "skill", ID: d.ID, Version: tc.version, Digest: tc.digest, Name: d.Name}})
		if err != nil {
			t.Fatal(err)
		}
		for _, change := range preview.Changes {
			if change.ID == d.ID && change.ConflictCode != tc.code {
				t.Fatalf("got %+v, want %s", change, tc.code)
			}
		}
	}
}

func TestPreviewCarriesDeclaredMetadataAndPrivateScopes(t *testing.T) {
	p := examplePackage(t)
	p.Manifest.Private = []Private{
		{Scope: "repo", Project: "project-abc", Path: "private/project.json"},
		{Scope: "global", Path: "private/global.json"},
	}
	preview, err := Analyze(p, 7, "global", nil)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Source != p.Manifest.Source || preview.License != p.Manifest.License ||
		len(preview.PrivateScopes) != 2 || preview.PrivateScopes[0] != "global" ||
		preview.PrivateScopes[1] != "project-abc" || preview.Digest == "" {
		t.Fatalf("preview metadata = %+v", preview)
	}
}
