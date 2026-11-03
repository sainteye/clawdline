package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/doctor"
)

func (s *Server) doctorHistory() *doctor.Recorder {
	s.doctorOnce.Do(func() {
		if s.doctorRecorder == nil {
			s.doctorRecorder = doctor.New(s.cfg.Dir)
		}
	})
	return s.doctorRecorder
}

func (s *Server) doctorReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Doctor reports are read with GET.")
		return
	}
	// Version probes cannot accumulate behind repeated presses or slow CLIs.
	if !s.doctorBusy.CompareAndSwap(false, true) {
		writeRefusal(w, http.StatusConflict, "doctor_busy", "A Doctor check is already running. Wait for it to finish.")
		return
	}
	defer s.doctorBusy.Store(false)
	snapshot := s.providerQuotaReader().Inspect(time.Now())
	report := doctor.Build(r.Context(), s.version, snapshot, s.doctorHistory())
	b, err := json.Marshal(report)
	if err != nil || len(b) > doctor.ReportBytesLimit {
		writeRefusal(w, http.StatusInternalServerError, "doctor_report_too_large", "The Doctor report exceeded its safe export limit.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
}
