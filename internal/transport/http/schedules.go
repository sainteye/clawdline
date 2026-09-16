package http

import (
	"encoding/json"
	"net/http"
	"time"

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
		var body struct {
			ID        string   `json:"id"`
			Name      string   `json:"name"`
			When      string   `json:"when"`
			Assistant string   `json:"assistant"`
			Dir       string   `json:"dir"`
			Brief     string   `json:"brief"`
			Claims    []string `json:"claims"`
		}
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
			Assistant: body.Assistant, Dir: body.Dir, Brief: body.Brief,
			Claims: body.Claims, Enabled: true,
		}
		if err := s.store.SaveSchedule(ctx, sc); err != nil {
			writeRefusal(w, http.StatusInternalServerError, "store_unwritable", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": sc.ID, "when": sc.When.String()})
		return
	}

	all, err := s.store.Schedules(ctx)
	if err != nil {
		writeRefusal(w, http.StatusInternalServerError, "store_unreadable", err.Error())
		return
	}
	rows := make([]map[string]any, 0, len(all))
	for _, sc := range all {
		rows = append(rows, map[string]any{
			"id": sc.ID, "name": sc.Name, "when": sc.When.String(),
			"assistant": sc.Assistant, "dir": sc.Dir,
			"enabled": sc.Enabled, "lastRun": lastRun(sc.LastRun), "lastTask": sc.LastTask,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"schedules": rows})
}
