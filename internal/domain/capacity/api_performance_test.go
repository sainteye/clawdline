package capacity

import (
	"testing"

	"github.com/sainteye/clawdline/internal/domain/apiperformance"
)

func TestAPIPerformanceBoundsMatchRuntimeAndStayLocal(t *testing.T) {
	rows := apiPerformanceBounds()
	defaults := apiperformance.HardLimits()
	if len(rows) != len(defaults) {
		t.Fatalf("registered %d bounds, runtime %d", len(rows), len(defaults))
	}
	for name, n := range defaults {
		var found bool
		for _, e := range rows {
			if e.Name == "diagnostics.api_"+name {
				found = true
				if e.Limit != n {
					t.Errorf("%s: register %d runtime %d", name, e.Limit, n)
				}
				for _, channel := range e.Told {
					if channel == Health || channel == Notice {
						t.Errorf("%s: owner aggregates must not be uploaded or pushed", name)
					}
				}
			}
		}
		if !found {
			t.Errorf("%s not registered", name)
		}
	}
}
