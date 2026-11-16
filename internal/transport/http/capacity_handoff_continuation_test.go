package http

import (
	"testing"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

func TestFreshContinuationCapacityHasRequestOwnedReadings(t *testing.T) {
	s := capacityServer(t)
	measures := s.capacityMeasures()
	for _, name := range []string{capacity.BotsContinuationMessages, capacity.BotsContinuationQuoteBytes} {
		measure, ok := measures[name]
		if !ok {
			t.Fatal("registered continuation bound is absent from HTTP diagnostics", name)
		}
		r := measure()
		if !r.Known || r.Used != 0 || r.Note == "" || r.Err != "" {
			t.Fatal("request-owned excerpt invented retained occupancy", name, r)
		}
	}
}
