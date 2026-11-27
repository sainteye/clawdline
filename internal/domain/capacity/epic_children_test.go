package capacity

import (
	"testing"

	"github.com/sainteye/clawdline/internal/domain/work"
)

// The registered Epic child bound is the limit the domain enforces; the
// register said 64 for months while work refused the thirty-third child.
func TestEpicChildBoundIsTheEnforcedLimit(t *testing.T) {
	for _, e := range Register() {
		if e.Name == EpicChildItems {
			if e.Limit != work.EpicChildLimit {
				t.Fatalf("%s registered at %d, enforced at %d", e.Name, e.Limit, work.EpicChildLimit)
			}
			return
		}
	}
	t.Fatalf("%s is not registered", EpicChildItems)
}
