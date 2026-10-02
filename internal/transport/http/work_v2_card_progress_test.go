package http

import (
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/work"
)

func TestWorkV2CardProgressReachesBoardWire(t *testing.T) {
	v := app.WorkV2View{
		Item:         work.ItemV2{ID: "10000000-0000-4000-8000-000000000001", ProjectID: "project-a", Phase: work.PhaseDone},
		CardProgress: store.WorkV2CardProgress{PhaseEnteredAt: 100, DeploymentEvidence: "Cloud build 123\nmore"},
	}
	w := (&Server{}).workV2ItemOf(nil, v)
	if w.PhaseEnteredAt == nil || *w.PhaseEnteredAt != 100 || w.DeploymentEvidence != "Cloud build 123\nmore" {
		t.Fatalf("card progress missing from wire: %+v", w)
	}
}
