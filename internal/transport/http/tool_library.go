package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/toollibrary"
)

func (s *Server) toolLibraryRoute(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeRefusal(w, 503, "store_unavailable", "The tool library is unavailable.")
		return
	}
	if r.Method == http.MethodGet && routePath(r) == "/v1/tool-library" {
		state, err := s.store.ToolLibrary(r.Context())
		if err != nil {
			writeRefusal(w, 503, "store_unreadable", "The saved projects could not be read.")
			return
		}
		writeJSON(w, struct {
			store.ToolLibrary
			Catalog toollibrary.Catalog `json:"catalog"`
		}{state, toollibrary.Recommendations()})
		return
	}
	if r.Method != http.MethodPost || (routePath(r) != "/v1/tool-library" && routePath(r) != "/v1/tool-library/prepare") {
		writeNoSuchRoute(w, r)
		return
	}
	if !machineAuthed(r) && !maySend(r) {
		writeRefusal(w, 403, "forbidden", "Saving or preparing a tool needs send permission.")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" || len(key) > toollibrary.MaxRequestKeyBytes {
		writeRefusal(w, 400, "idempotency_key_required", "A tool library operation needs an Idempotency-Key.")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, CapacityLimit(capacity.ToolLibraryRequestBytes)+1))
	if err != nil {
		writeRefusal(w, 400, "bad_request", "The request could not be read.")
		return
	}
	if int64(len(raw)) > CapacityLimit(capacity.ToolLibraryRequestBytes) {
		writeRefusal(w, 413, "body_too_large", "The tool library request is too large.")
		return
	}
	principal := viewerPrincipal(r)
	if machineAuthed(r) {
		principal = "orchestrator"
	}
	s.receipted(w, r, store.ReceiptKey{Scope: "tool-library", Actor: principal, Key: key}, requestDigest([]byte(routePath(r)), raw), func(status int) bool { return status < 500 && status != 429 }, func(out http.ResponseWriter) {
		ctx := context.WithoutCancel(r.Context())
		if routePath(r) == "/v1/tool-library/prepare" {
			s.toolLibraryPrepare(ctx, out, r, raw)
			return
		}
		var in struct {
			Expected int64                `json:"expected_version"`
			Project  *toollibrary.Project `json:"project"`
			Remove   string               `json:"remove"`
		}
		if !toolLibraryDecode(raw, &in) || ((in.Project == nil) == (in.Remove == "")) {
			writeRefusal(out, 400, "bad_request", "Choose one project to save or remove.")
			return
		}
		if in.Project != nil {
			b, _ := json.Marshal(in.Project)
			if int64(len(b)) > CapacityLimit(capacity.ToolLibraryProjectBytes) {
				writeRefusal(out, 413, "project_too_large", "The project record is too large.")
				return
			}
		}
		if in.Project != nil {
			normalized, normalizeErr := toollibrary.Normalize(*in.Project)
			if normalizeErr != nil {
				writeRefusal(out, 400, "invalid_project", "Check the repository, category, revision and Skill entries.")
				return
			}
			in.Project = &normalized
		}
		state, err := s.store.ChangeToolProject(ctx, in.Expected, in.Remove, in.Project, CapacityLimit(capacity.ToolLibraryProjects))
		if errors.Is(err, store.ErrToolLibraryVersion) {
			writeRefusal(out, 409, "version_conflict", "The tool library changed. Read it again before saving.")
			return
		}
		if errors.Is(err, store.ErrToolLibraryFull) {
			writeRefusal(out, 429, "capacity_full", "The saved project library is full.")
			return
		}
		if err != nil {
			writeRefusal(out, 503, "store_unwritable", "The project could not be saved. Retry this operation with the same key.")
			return
		}
		writeJSON(out, state)
	})
}

func toolLibraryDecode(raw []byte, out any) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(out) == nil && d.Decode(new(any)) == io.EOF
}

func (s *Server) toolLibraryPrepare(ctx context.Context, w http.ResponseWriter, r *http.Request, raw []byte) {
	var in struct {
		toollibrary.Preparation
		Session      string `json:"session"`
		Conversation string `json:"conversation"`
		Generation   string `json:"generation"`
	}
	if !toolLibraryDecode(raw, &in) || in.Session == "" || in.Conversation == "" {
		writeRefusal(w, 400, "bad_request", "The task needs an explicit Session and conversation.")
		return
	}
	if !s.admitPinnedTarget(w, r, in.Session) {
		return
	}
	row, err := s.actions().Find(ctx, in.Session)
	if err != nil {
		writeActionRefusal(w, err)
		return
	}
	if in.Generation != "" {
		if held := r.Header.Get("X-Clawdline-Execution-Generation"); held != "" && held != in.Generation {
			writeRefusal(w, 409, "stale_session", "The Session execution changed.")
			return
		}
		r.Header.Set("X-Clawdline-Target-Machine", s.executionMachine())
		r.Header.Set("X-Clawdline-Execution-Generation", in.Generation)
		if !s.admitPinnedTarget(w, r, in.Session) {
			return
		}
	}
	if row.ConversationID != in.Conversation {
		writeRefusal(w, 409, "stale_session", "The Session identity changed. Prepare the task again.")
		return
	}
	state, err := s.store.ToolLibrary(ctx)
	if err != nil {
		writeRefusal(w, 503, "store_unreadable", "The tool library could not be read.")
		return
	}
	var selected *toollibrary.Project
	for _, p := range append(toollibrary.Recommendations().Projects, state.Saved...) {
		if p.ID == in.ProjectID {
			copy := p
			selected = &copy
		}
	}
	if selected == nil {
		writeRefusal(w, 404, "unknown_project", "That project is no longer in the library.")
		return
	}
	prepared, err := toollibrary.Prepare(*selected, in.Preparation)
	if err != nil {
		writeRefusal(w, 400, err.Error(), "The tool preparation needs a valid Skill, task and installation scope.")
		return
	}
	if !s.admitPinnedTarget(w, r, in.Session) {
		return
	}
	writeJSON(w, prepared)
}
