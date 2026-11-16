package toollibrary

import "testing"

func TestSearchIncludesSavedSourcesAndSkillsInBothLanguages(t *testing.T) {
	catalog := Catalog{Projects: []Project{
		{ID: "one", RepoURL: "https://github.com/Owner/One", Name: Names{"en": "Drawing"}},
		{ID: "two", RepoURL: "https://github.com/owner/two", Skills: []Skill{{Purpose: Names{"en": "security audit", "zh-Hant": "安全審查"}}}},
	}}
	saved := []Project{
		{ID: "one", RepoURL: "https://github.com/owner/one", Name: Names{"en": "Updated drawing"}},
		{ID: "custom", RepoURL: "https://github.com/owner/custom", Summary: Names{"zh-Hant": "互動示意圖"}},
	}
	for _, tc := range []struct{ query, want string }{
		{"DRAWING", "one"}, {"security", "two"}, {"安全", "two"}, {"互動", "custom"}, {"missing 安全", "two"},
	} {
		got := Search(catalog, saved, tc.query)
		if len(got) != 1 || got[0].ID != tc.want {
			t.Fatalf("query=%q: %+v", tc.query, got)
		}
	}
	all := Search(catalog, saved, "")
	if len(all) != 3 || all[0].Name["en"] != "Updated drawing" {
		t.Fatalf("saved source was omitted or duplicated: %+v", all)
	}
	if len(Search(catalog, saved, "absent")) != 0 {
		t.Fatal("unrelated query matched")
	}
}
