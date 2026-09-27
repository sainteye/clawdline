package persona

import (
	"strings"
	"testing"
)

// The product, quality and operations teams are in the catalog, each persona
// in the teams it was written for, with a bot of its own.
func TestProductQualityAndOperationsPersonas(t *testing.T) {
	want := map[string]string{
		"product-manager":      "product",
		"sprint-prioritizer":   "product",
		"feedback-synthesizer": "product",
		"trend-researcher":     "product,marketing",
		"ux-researcher":        "product,design",
		"test-automation":      "quality,engineering",
		"accessibility":        "quality,design",
		"performance":          "quality,engineering",
		"api-tester":           "quality",
		"evidence-collector":   "quality",
		"sre":                  "operations,engineering",
		"devops":               "operations,engineering",
		"incident-commander":   "operations",
		"finops":               "operations",
		"secrets":              "operations,engineering",
	}
	for id, teams := range want {
		p, ok := Known(id)
		if !ok {
			t.Errorf("%s is not in the catalog", id)
			continue
		}
		if got := strings.Join(p.Teams, ","); got != teams {
			t.Errorf("%s is in %s, want %s", id, got, teams)
		}
		if len(p.SuggestedKinds) != 0 {
			t.Errorf("%s suggests itself for %v; a kind's default stays with the engineering team", id, p.SuggestedKinds)
		}
		if _, drawnTwice := icons[id]; drawnTwice {
			t.Errorf("%s is drawn in icons.go as well", id)
		}
	}
	// Told apart by colour: no two bots share a body.
	accents := map[string]string{}
	for _, p := range All() {
		if other, ok := accents[p.Icon.Accent]; ok {
			t.Errorf("%s has %s's colour %s", p.ID, other, p.Icon.Accent)
		}
		accents[p.Icon.Accent] = p.ID
	}
}
