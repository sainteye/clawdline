package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

const maxTags = 5

type Server struct {
	store *Store
	loc   *time.Location
	// now is the clock; tests replace it.
	now func() time.Time
}

func NewServer(store *Store, loc *time.Location) *Server {
	return &Server{store: store, loc: loc, now: time.Now}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /tasks", s.listTasks)
	mux.HandleFunc("POST /tasks", s.createTask)
	mux.HandleFunc("GET /tasks/{id}", s.getTask)
	mux.HandleFunc("PATCH /tasks/{id}", s.patchTask)
	mux.HandleFunc("DELETE /tasks/{id}", s.deleteTask)
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func normalizeTags(tags []string) ([]string, error) {
	if len(tags) > maxTags {
		return nil, errors.New("at most 5 tags")
	}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			return nil, errors.New("empty tag")
		}
		out = append(out, t)
	}
	return out, nil
}

// today is the current calendar day in the service's configured time zone.
func (s *Server) today() Date {
	n := s.now().In(s.loc)
	return Date{time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)}
}

func hasTag(t Task, tag string) bool {
	for _, x := range t.Tags {
		if x == tag {
			return true
		}
	}
	return false
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	tag := strings.ToLower(r.URL.Query().Get("tag"))
	overdue := r.URL.Query().Get("overdue") == "true"
	today := s.today()
	out := []Task{}
	for _, t := range s.store.List() {
		if tag != "" && !hasTag(t, tag) {
			continue
		}
		if overdue && (t.Done || t.Due.After(today.Time)) {
			continue
		}
		out = append(out, t)
	}
	writeJSON(w, http.StatusOK, out)
}

type createReq struct {
	Title string   `json:"title"`
	Due   string   `json:"due"`
	Tags  []string `json:"tags"`
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var req createReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	due, err := ParseDate(req.Due)
	if err != nil {
		writeError(w, http.StatusBadRequest, "due must be YYYY-MM-DD")
		return
	}
	tags, err := normalizeTags(req.Tags)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	t, err := s.store.Create(Task{Title: strings.TrimSpace(req.Title), Due: due, Tags: tags}, s.now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save")
		return
	}
	w.Header().Set("Location", "/tasks/"+t.ID)
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.Get(r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

type patchReq struct {
	Title string   `json:"title"`
	Due   string   `json:"due"`
	Tags  []string `json:"tags"`
	Done  *bool    `json:"done"`
}

func (s *Server) patchTask(w http.ResponseWriter, r *http.Request) {
	var req patchReq
	json.NewDecoder(r.Body).Decode(&req)
	due, err := ParseDate(req.Due)
	if err != nil {
		writeError(w, http.StatusBadRequest, "due must be YYYY-MM-DD")
		return
	}
	t, err := s.store.Update(r.PathValue("id"), func(t *Task) {
		if req.Title != "" {
			t.Title = strings.TrimSpace(req.Title)
		}
		t.Due = due
		t.Tags = req.Tags
		if req.Done != nil {
			t.Done = *req.Done
		}
	})
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Delete(r.PathValue("id")); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
