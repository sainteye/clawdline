package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/sainteye/clawdline-go/internal/contract"
	"github.com/sainteye/clawdline-go/internal/domain/schedule"
)

// lastRun reports 0 for a schedule that has never run, rather than the Unix
// value of the zero time.
func lastRun(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func (s *Server) schedules(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method == http.MethodPost {
		s.scheduleSave(w, r)
		return
	}

	all, err := s.store.Schedules(ctx)
	if err != nil {
		writeRefusal(w, http.StatusInternalServerError, "store_unreadable", err.Error())
		return
	}
	rows := make([]contract.ScheduleRow, 0, len(all))
	for _, sc := range all {
		rows = append(rows, contract.ScheduleRow{
			ID:         sc.ID,
			Name:       sc.Name,
			When:       whenText(sc),
			Unreadable: sc.Unreadable,
			Assistant:  contract.Assistant(sc.Assistant),
			Dir:        sc.Dir,
			Enabled:    sc.Enabled,
			LastRun:    lastRun(sc.LastRun),
			LastTask:   sc.LastTask,
		})
	}
	writeJSON(w, contract.ScheduleList{Schedules: rows})
}

// whenText is what this schedule says about its own clock.
//
// For a schedule that parsed, that is the normalised form. For one that did
// not, it is the spelling as stored: the parsed value is a zero, and printing a
// zero as `every 0s` would present the failure as a setting.
func whenText(sc schedule.Schedule) string {
	if sc.Unreadable {
		return sc.Spec
	}
	return sc.When.String()
}

// scheduleSave accepts one schedule.
//
// The request body is a generated type too. That matters here more than
// anywhere else on this daemon: `claims` absent and `claims` empty are
// different requests, and a decoder that flattened them would let a schedule
// dispatch work every minute having reserved nothing.
func (s *Server) scheduleSave(w http.ResponseWriter, r *http.Request) {
	var body contract.ScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "that body is not a schedule")
		return
	}
	when, err := schedule.ParseWhen(body.When)
	if err != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_schedule", err.Error())
		return
	}
	if body.Claims == nil {
		writeRefusal(w, http.StatusUnprocessableEntity, "claims_required",
			"a schedule dispatches work, so it declares paths like any other dispatch")
		return
	}
	sc := schedule.Schedule{
		ID: body.ID, Name: body.Name, When: when,
		Assistant: string(body.Assistant), Dir: body.Dir, Brief: body.Brief,
		Claims: body.Claims, Enabled: true,
	}
	if err := s.store.SaveSchedule(r.Context(), sc); err != nil {
		writeRefusal(w, http.StatusInternalServerError, "store_unwritable", err.Error())
		return
	}
	writeJSON(w, contract.ScheduleSaved{OK: true, ID: sc.ID, When: sc.When.String()})
}
