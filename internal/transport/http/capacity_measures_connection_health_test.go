package http

import (
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/healthjournal"
	"github.com/sainteye/clawdline/internal/adapters/healthupload"
)

// cloud.health_upload_state_bytes is the progress file's size when the
// uploader knows it, and unmeasured — never a zero — before.
func TestTheUploadStateRowReadsTheProgressFileSize(t *testing.T) {
	now := time.Now()
	if r := uploadReading("cloud.health_upload_state_bytes", healthupload.Status{State: healthupload.CodeIdle}, now); r.Known {
		t.Fatalf("an unread progress file must be unmeasured: %+v", r)
	}
	n := 1234
	r := uploadReading("cloud.health_upload_state_bytes", healthupload.Status{State: healthupload.CodeIdle, StateBytes: &n}, now)
	if !r.Known || r.Used != 1234 {
		t.Fatalf("state_bytes 1234 read as %+v", r)
	}
}

func TestDurableStoreCapacityReadsSegmentsRecoveryAndLostRanges(t *testing.T) {
	st := healthjournal.Status{State: "ok", LargestSegmentBytes: 1234, LostRanges: 3, Recovery: healthjournal.Recovery{EntriesExamined: 17}}
	for name, want := range map[string]int64{"cloud.health_store_segment_bytes": 1234, "cloud.health_store_lost_ranges": 3, "cloud.health_store_dir_entries": 17} {
		got := storeReading(name, st, time.Now())
		if !got.Known || got.Used != want || got.Failing {
			t.Fatalf("%s: %+v", name, got)
		}
	}
	st.Recovery.ListingTruncated = true
	if got := storeReading("cloud.health_store_dir_entries", st, time.Now()); !got.Known || !got.Failing {
		t.Fatal("truncated recovery was hidden", got)
	}
}
