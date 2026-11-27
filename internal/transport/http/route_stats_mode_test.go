package http

import (
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Every mode the Bot read view accepts gets its own API-history counter;
// only a malformed or unknown mode shares the unclassified bucket.
func TestEveryAcceptedReadModeHasItsOwnCounter(t *testing.T) {
	for _, mode := range []pa.ReadViewMode{pa.ReadViewSnapshot, pa.ReadViewHistory, pa.ReadViewEvents,
		pa.ReadViewPermissions, pa.ReadViewTraces, pa.ReadViewProgress, pa.ReadViewDetail} {
		if got, want := botReadModeLabel("mode="+string(mode)), " ["+string(mode)+"]"; got != want {
			t.Errorf("mode %s is counted as %q, want %q", mode, got, want)
		}
	}
	if got := botReadModeLabel("mode=private-mode"); got != " [unclassified]" {
		t.Errorf("an unknown mode is counted as %q", got)
	}
}
