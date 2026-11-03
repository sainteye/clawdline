package http

import (
	"time"

	"github.com/sainteye/clawdline/internal/adapters/healthjournal"
	"github.com/sainteye/clawdline/internal/adapters/healthupload"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
)

// connectionHealthCapacityMeasures reads the cloud.health_* rows from the
// daemon's connection-health runtime: the tracker's snapshot for the slot
// rows, the journal's occupancy and lifetime eviction and refusal counts,
// and the watchdog's transition queue and its drops. Each read is atomics or
// the journal's own short lock; none calls Scan.
//
// With monitoring off there is no runtime, so nothing is held: each row is a
// known zero that says why, which the note keeps apart from an idle table.
func (s *Server) connectionHealthCapacityMeasures() map[string]func() capacity.Reading {
	rt := s.connectionHealth
	names := []string{
		"cloud.health_active_slots", "cloud.health_reserved_slots",
		"cloud.health_journal_rows", "cloud.health_journal_bytes", "cloud.health_record_bytes",
		"cloud.health_pending_transitions",
		"cloud.health_query_rows", "cloud.health_query_bytes",
		"cloud.health_capture_raw_bytes", "cloud.health_capture_export_bytes", "cloud.health_capture_frames",
		"cloud.health_captures_per_minute", "cloud.health_capture_budget_seconds", "cloud.health_owner_incidents",
	}
	out := s.connectionHealthStoreMeasures(rt != nil)
	for name, f := range s.connectionHealthUploadMeasures(rt != nil) {
		out[name] = f
	}
	for name, f := range s.connectionHealthSealedMeasures(rt != nil) {
		out[name] = f
	}
	if rt == nil {
		for _, name := range names {
			out[name] = func() capacity.Reading {
				return capacity.Reading{Known: true, Note: "connection-health monitoring is off in this daemon; nothing is held"}
			}
		}
		return out
	}
	slots := func(critical bool) capacity.Reading {
		phases, overflow := rt.Tracker.Snapshot()
		var used int64
		for _, p := range phases {
			if p.Spec.Critical == critical || !critical {
				used++
			}
		}
		refused := overflow.Total
		if critical {
			refused = overflow.Critical
		}
		return capacity.Reading{Known: true, Used: used, Counters: capacity.Counters{Refused: int64(refused)}}
	}
	journal := func(pick func(connectionhealth.JournalStats) int64) func() capacity.Reading {
		return func() capacity.Reading {
			st := rt.Journal.Stats()
			return capacity.Reading{Known: true, Used: pick(st),
				Note:     connectionhealth.DurabilityMemory,
				Counters: capacity.Counters{Evicted: int64(st.Evicted), Refused: int64(st.Refused)}}
		}
	}
	out["cloud.health_active_slots"] = func() capacity.Reading { return slots(false) }
	out["cloud.health_reserved_slots"] = func() capacity.Reading { return slots(true) }
	out["cloud.health_journal_rows"] = journal(func(st connectionhealth.JournalStats) int64 { return int64(st.Rows) })
	out["cloud.health_journal_bytes"] = journal(func(st connectionhealth.JournalStats) int64 { return int64(st.Bytes) })
	out["cloud.health_record_bytes"] = journal(func(st connectionhealth.JournalStats) int64 { return int64(st.LargestRecord) })
	out["cloud.health_pending_transitions"] = func() capacity.Reading {
		depth, lost := rt.Watchdog.Pending()
		return capacity.Reading{Known: true, Used: int64(depth), Counters: capacity.Counters{Dropped: int64(lost)}}
	}
	// The stall capture worker: its status counters say what each bound
	// refused; the raw buffer is held only while a capture runs.
	capture := func(pick func(connectionhealth.CaptureStatus, *connectionhealth.Capture) (int64, capacity.Counters)) func() capacity.Reading {
		return func() capacity.Reading {
			st := rt.Capture.Status()
			latest, _ := rt.Capture.Latest()
			used, c := pick(st, latest)
			r := capacity.Reading{Known: true, Used: used, Counters: c, Note: "stall capture " + st.State}
			return r
		}
	}
	out["cloud.health_capture_raw_bytes"] = capture(func(st connectionhealth.CaptureStatus, l *connectionhealth.Capture) (int64, capacity.Counters) {
		if l == nil {
			return 0, capacity.Counters{}
		}
		return int64(l.RawBytes), capacity.Counters{}
	})
	out["cloud.health_capture_export_bytes"] = capture(func(st connectionhealth.CaptureStatus, l *connectionhealth.Capture) (int64, capacity.Counters) {
		if l == nil {
			return 0, capacity.Counters{}
		}
		return 0, capacity.Counters{Dropped: int64(l.GoroutinesOmitted)}
	})
	out["cloud.health_capture_frames"] = capture(func(st connectionhealth.CaptureStatus, l *connectionhealth.Capture) (int64, capacity.Counters) {
		if l == nil {
			return 0, capacity.Counters{}
		}
		return 0, capacity.Counters{Dropped: int64(l.FramesOmitted)}
	})
	out["cloud.health_captures_per_minute"] = capture(func(st connectionhealth.CaptureStatus, _ *connectionhealth.Capture) (int64, capacity.Counters) {
		return 0, capacity.Counters{Refused: int64(st.RefusedRate)}
	})
	out["cloud.health_capture_budget_seconds"] = capture(func(st connectionhealth.CaptureStatus, _ *connectionhealth.Capture) (int64, capacity.Counters) {
		return st.RunningNS / int64(time.Second), capacity.Counters{Refused: int64(st.RefusedUnavailable)}
	})
	out["cloud.health_owner_incidents"] = func() capacity.Reading {
		return capacity.Reading{Known: true, Note: "the owner feed's remembered stalls are not counted here; a forgotten stall's recovery stays in the local export"}
	}
	// A page is built and answered within one request; nothing is held
	// between reads, so the per-page rows are a true zero.
	for _, name := range []string{"cloud.health_query_rows", "cloud.health_query_bytes"} {
		out[name] = func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "a bound per page; nothing is held between reads"}
		}
	}
	return out
}

// connectionHealthStoreMeasures reads the cloud.health_store_* rows from the
// durable store's Status (atomics and a copy of its segment list). Before the
// store is open, and with monitoring off, it holds nothing: a known zero
// that says which. A store that failed to open leaves what is on disk
// unknown, so its rows are unmeasured with the failure code. Segment sizes
// and lost ranges are live; the directory bound describes the opening scan.
func (s *Server) connectionHealthStoreMeasures(monitoring bool) map[string]func() capacity.Reading {
	names := []string{
		"cloud.health_store_bytes", "cloud.health_store_seconds", "cloud.health_store_segment_bytes",
		"cloud.health_store_segments", "cloud.health_store_queue_rows", "cloud.health_store_queue_bytes",
		"cloud.health_store_frame_bytes", "cloud.health_store_readers", "cloud.health_store_dir_entries",
		"cloud.health_store_lost_ranges",
	}
	out := map[string]func() capacity.Reading{}
	for _, name := range names {
		name := name
		out[name] = func() capacity.Reading {
			d := s.connectionHealthStore.Load()
			switch {
			case !monitoring:
				return capacity.Reading{Known: true, Note: "connection-health monitoring is off in this daemon; nothing is held"}
			case d == nil || d.state == DurableOpening:
				return capacity.Reading{Known: true, Note: "the durable connection-health store is not open yet; nothing is held by it"}
			case d.store == nil:
				return capacity.Unmeasured("the durable connection-health store could not be opened: " + d.code)
			}
			return storeReading(name, d.store.Status(), time.Now())
		}
	}
	return out
}

func storeReading(name string, st healthjournal.Status, now time.Time) capacity.Reading {
	pruned := int64(st.PrunedAge + st.PrunedBytes + st.PrunedCount)
	r := capacity.Reading{Known: true, Failing: st.State != "ok" && st.State != "closed",
		Counters: capacity.Counters{Evicted: pruned, Dropped: int64(st.Lost)}}
	switch name {
	case "cloud.health_store_bytes":
		r.Used = st.Bytes
		r.Note = st.CrashLoss
	case "cloud.health_store_seconds":
		if st.Floor == nil {
			r.Note = "nothing is retained yet"
			break
		}
		r.Used = int64(now.Sub(*st.Floor) / time.Second)
		r.OldestAt = *st.Floor
	case "cloud.health_store_segment_bytes":
		r.Used = st.LargestSegmentBytes
	case "cloud.health_store_lost_ranges":
		r.Used = int64(st.LostRanges)
	case "cloud.health_store_dir_entries":
		r.Used = int64(st.Recovery.EntriesExamined)
		r.Failing = r.Failing || st.Recovery.ListingTruncated
		r.Note = "entries examined when this store opened; not a live directory scan"
	case "cloud.health_store_segments":
		r.Used = int64(st.Segments)
	case "cloud.health_store_queue_rows":
		r.Used = st.QueueRows
		r.Counters.Refused = int64(st.Refused)
	case "cloud.health_store_queue_bytes":
		r.Used = st.QueueBytes
		r.Counters.Refused = int64(st.Refused)
	case "cloud.health_store_readers":
		r.Used = int64(st.Readers)
	case "cloud.health_store_frame_bytes":
		r.Note = "a bound per frame; nothing is held between writes"
		r.Counters.Refused = int64(st.TooLarge)
	default:
		return capacity.Unmeasured("the durable store's status does not report this row's occupancy")
	}
	return r
}

// connectionHealthUploadMeasures reads the cloud.health_upload_* rows from the
// uploader's Status, a copy under its own short lock that never waits on the
// network. The hourly rows are this clock hour's spending; the pending batch
// is the one body held; the receipts are what the progress file keeps. A
// per-request bound holds nothing between requests.
func (s *Server) connectionHealthUploadMeasures(monitoring bool) map[string]func() capacity.Reading {
	names := []string{
		"cloud.health_upload_batch_bytes", "cloud.health_upload_batch_events", "cloud.health_upload_hourly_bytes",
		"cloud.health_upload_hourly_requests", "cloud.health_upload_attempts", "cloud.health_upload_retry_seconds",
		"cloud.health_upload_retry_after_seconds", "cloud.health_upload_attempt_seconds",
		"cloud.health_upload_response_bytes", "cloud.health_upload_state_bytes", "cloud.health_upload_receipts",
		"cloud.health_upload_step_pages",
	}
	out := map[string]func() capacity.Reading{}
	for _, name := range names {
		name := name
		out[name] = func() capacity.Reading {
			f := s.connectionHealthUpload.Load()
			switch {
			case !monitoring:
				return capacity.Reading{Known: true, Note: "connection-health monitoring is off in this daemon; nothing is held"}
			case f == nil:
				return capacity.Reading{Known: true, Note: "the connection-health uploader has not started; nothing is held"}
			}
			return uploadReading(name, (*f)(), time.Now())
		}
	}
	return out
}

func uploadReading(name string, st healthupload.Status, now time.Time) capacity.Reading {
	failing := false
	switch st.State {
	case healthupload.CodeAuthRefused, healthupload.CodeStateUnwritable, healthupload.CodeStateUnreadable,
		healthupload.CodeSourceFailed, healthupload.CodeOtherOrigin, healthupload.CodeIdentityUnread:
		failing = true
	}
	r := capacity.Reading{Known: true, Failing: failing, Note: string(st.State),
		Counters: capacity.Counters{Dropped: int64(st.Totals.AbandonedEvents + st.Totals.RefusedEvents)}}
	thisHour := st.Hour.Start.Equal(now.UTC().Truncate(time.Hour))
	switch name {
	case "cloud.health_upload_hourly_bytes":
		if thisHour {
			r.Used = st.Hour.Bytes
		}
	case "cloud.health_upload_hourly_requests":
		if thisHour {
			r.Used = st.Hour.Requests
		}
	case "cloud.health_upload_batch_bytes":
		if st.Pending != nil {
			r.Used = int64(st.Pending.Bytes)
		}
	case "cloud.health_upload_batch_events":
		if st.Pending != nil {
			r.Used = int64(st.Pending.Events)
		}
	case "cloud.health_upload_attempts":
		if st.Pending != nil {
			r.Used = int64(st.Pending.Attempts)
		}
		r.Counters.Evicted = int64(st.Totals.AbandonedBatches)
	case "cloud.health_upload_receipts":
		r.Used = int64(st.ReceiptsKept)
	case "cloud.health_upload_state_bytes":
		if st.StateBytes == nil {
			return capacity.Unmeasured("the uploader has not read its progress file yet")
		}
		r.Used = int64(*st.StateBytes)
	default:
		r.Note = "a bound per request or per cycle; nothing is held between them (" + string(st.State) + ")"
	}
	return r
}

// connectionHealthSealedMeasures reads the cloud.health_sealed_* rows from the
// owner-sealed sender the daemon runs (internal/adapters/healthsealed, wired
// in cmd/clawdline/health.go), through the uploader status it is reported
// in. Its queue depth is read now; its local records as last counted on its
// own goroutine (unmeasured until then); the per-request and per-seal rows
// hold nothing between requests. A sender that cannot seal (no recorded
// content-key epoch, no key, a store that cannot be written) is failing
// with its code, never an idle-looking 0.
func (s *Server) connectionHealthSealedMeasures(monitoring bool) map[string]func() capacity.Reading {
	names := []string{
		"cloud.health_sealed_ciphertext_bytes", "cloud.health_sealed_request_bytes",
		"cloud.health_sealed_life_seconds", "cloud.health_sealed_attempts",
		"cloud.health_sealed_attempt_seconds", "cloud.health_sealed_response_bytes",
		"cloud.health_sealed_queued_jobs", "cloud.health_sealed_revisions",
		"cloud.health_sealed_snapshot_bytes", "cloud.health_sealed_snapshot_field_chars",
		"cloud.health_sealed_snapshot_coverage", "cloud.health_sealed_snapshot_frames",
		"cloud.health_sealed_snapshot_sources", "cloud.health_sealed_store_attachments",
		"cloud.health_sealed_store_bytes",
	}
	out := map[string]func() capacity.Reading{}
	for _, name := range names {
		out[name] = func() capacity.Reading {
			f := s.connectionHealthUpload.Load()
			switch {
			case !monitoring:
				return capacity.Reading{Known: true, Note: "connection-health monitoring is off in this daemon; nothing is held"}
			case f == nil:
				return capacity.Reading{Known: true, Note: "the owner-sealed diagnostic sender has not started; nothing is held"}
			}
			d := (*f)().OwnerDiagnostics
			if d == nil {
				return capacity.Reading{Known: true, Note: "the owner-sealed diagnostic sender has not started; nothing is held"}
			}
			return sealedReading(name, *d)
		}
	}
	// Identity pairing runs only when the person starts it from the local
	// route (cloud_identity_pairing.go). Each row bounds one request or one
	// run; nothing is held between them, so there is no level to report.
	for _, name := range []string{"cloud.content_key_epoch_record_bytes", "cloud.identity_pairing_poll_attempts",
		"cloud.identity_pairing_decision_bytes", "cloud.identity_pairing_run_seconds"} {
		out[name] = func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "identity pairing bounds one local request or run; nothing is held between them"}
		}
	}
	return out
}

func sealedReading(name string, d healthupload.OwnerDiagnostics) capacity.Reading {
	note := d.Coverage
	if d.Code != "" {
		note += " (" + d.Code + ")"
	}
	r := capacity.Reading{Known: true, Failing: d.Coverage == "failed", Note: note,
		Counters: capacity.Counters{Dropped: d.Dropped}}
	switch name {
	case "cloud.health_sealed_queued_jobs":
		r.Used = int64(d.Queued)
	case "cloud.health_sealed_store_attachments":
		if d.Attachments == nil {
			return capacity.Unmeasured("the sender has not counted its local records yet")
		}
		r.Used = int64(*d.Attachments)
	case "cloud.health_sealed_store_bytes":
		if d.StoreBytes == nil {
			return capacity.Unmeasured("the sender has not counted its local records yet")
		}
		r.Used = *d.StoreBytes
	default:
		r.Note = "a bound per seal or per request; nothing is held between them (" + note + ")"
	}
	return r
}
