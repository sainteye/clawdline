package persona_test

import (
	"testing"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/persona"
)

// The registered bounds are the ones the catalog enforces: /v1/diagnostics
// and docs/limits.md say the number the loader refuses past.
func TestTheRegisteredBoundsAreTheCatalogs(t *testing.T) {
	want := map[string]int64{
		capacity.PersonasCatalog:   persona.MaxPersonas,
		capacity.PersonasTextBytes: persona.MaxPersonaBytes,
	}
	for _, e := range capacity.Register() {
		if limit, ok := want[e.Name]; ok {
			if e.Limit != limit {
				t.Errorf("%s is registered at %d; the catalog enforces %d", e.Name, e.Limit, limit)
			}
			delete(want, e.Name)
		}
	}
	for name := range want {
		t.Errorf("%s is not registered", name)
	}
	if persona.MaxPersonas != 64 {
		t.Errorf("MaxPersonas is %d; the catalog is sized for 64", persona.MaxPersonas)
	}
}
