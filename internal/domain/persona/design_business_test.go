package persona

import (
	"strings"
	"testing"
)

// catalogFiles is how many persona files catalog/ holds, so a count follows
// the catalog rather than a number every new team has to edit.
func catalogFiles(t *testing.T) int {
	t.Helper()
	entries, err := files.ReadDir("catalog")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			n++
		}
	}
	return n
}

// The design and business teams: each persona is in the catalog with the
// teams it was written for, in that order, suggests no Board kind, and has a
// bot of its own team's family.
func TestTheDesignAndBusinessTeamsAreInTheCatalog(t *testing.T) {
	want := []struct{ id, teams, en, zh string }{
		{"ui-designer", "design", "UI Designer", "UI 設計師"},
		{"ux-architect", "design,engineering", "UX Architect", "UX 架構師"},
		{"brand-guardian", "design,marketing", "Brand Guardian", "品牌守護者"},
		{"ui-finish-gate", "design,quality", "UI Finish Gate", "UI 上線把關"},
		{"image-prompt", "design,marketing", "Image Prompt Engineer", "圖像提示工程師"},
		{"pricing", "business", "Pricing Analyst", "定價分析師"},
		{"customer-success", "business", "Customer Success Manager", "客戶成功經理"},
		{"support", "business", "Support Responder", "客服專員"},
		{"analytics", "business,marketing,product", "Analytics Reporter", "數據分析師"},
		{"devrel", "business,marketing", "Developer Advocate", "開發者推廣"},
		{"privacy", "business,operations", "Privacy & Compliance Officer", "隱私法遵官"},
	}
	family := map[string]map[string]bool{
		"design":   {designPink: true, designViolet: true, designMagenta: true, designPurple: true, designRose: true},
		"business": {businessNavy: true, businessCobalt: true, businessDenim: true, businessSteel: true, businessIndigo: true, businessMidnight: true},
	}
	accents := map[string]string{}
	for _, p := range All() {
		if prev, ok := accents[p.Icon.Accent]; ok {
			t.Errorf("%s and %s share the colour %s", prev, p.ID, p.Icon.Accent)
		}
		accents[p.Icon.Accent] = p.ID
	}
	for _, w := range want {
		p, ok := Known(w.id)
		if !ok {
			t.Errorf("%s is not in the catalog", w.id)
			continue
		}
		if got := strings.Join(p.Teams, ","); got != w.teams {
			t.Errorf("%s is in %s, want %s", w.id, got, w.teams)
		}
		if p.Name.En != w.en || p.Name.ZhHant != w.zh {
			t.Errorf("%s is named %q / %q, want %q / %q", w.id, p.Name.En, p.Name.ZhHant, w.en, w.zh)
		}
		if len(p.SuggestedKinds) != 0 {
			t.Errorf("%s suggests %v; a kind's default stays with the engineering personas", w.id, p.SuggestedKinds)
		}
		if !family[p.Teams[0]][p.Icon.Accent] {
			t.Errorf("%s's bot is %s, not a colour of the %s family", w.id, p.Icon.Accent, p.Teams[0])
		}
	}
	privacy, _ := Known("privacy")
	if !strings.Contains(privacy.Body, "not legal advice") {
		t.Error("privacy does not say that it is not legal advice")
	}
}
