package http

import (
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"testing"
	"time"
)

func TestBrowserCloseReceiptUsageIsUnknownUntilMeasured(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	measures := s.browserCapacityMeasures()
	for _, name := range []string{capacity.BrowserCloseReceipts, capacity.BrowserCloseReceiptBytes} {
		reading := measures[name]()
		var entry capacity.Entry
		for _, e := range capacity.Register() {
			if e.Name == name {
				entry = e
				break
			}
		}
		st, _ := capacity.NewTracker().Observe(capacity.Resolved{Entry: entry, Limit: entry.Limit}, reading, time.Now())
		if reading.Known || st.State != capacity.Unknown {
			t.Fatalf("unmeasured %s appeared empty: %+v %+v", name, reading, st)
		}
	}
}
