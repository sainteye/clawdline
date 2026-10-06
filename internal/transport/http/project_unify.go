package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/projectfiles"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/contract"
)

// projectUnifyRoute plans (GET) and applies (POST) one rules file and one
// skills directory for a Project's Claude and Codex sessions
// (docs/project-files.md, Unify). Like the files route it takes only a
// current place id: the repository root comes from the daemon's catalog.
func (s *Server) projectUnifyRoute(w http.ResponseWriter, r *http.Request, placeID string) {
	if r.URL.RawQuery != "" {
		writeRefusal(w, 400, "bad_request", "Unify takes no query fields.")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeRefusal(w, 405, "method_not_allowed", "Read the plan with GET or apply it with POST.")
		return
	}
	project, found := s.workV2Project(r.Context(), placeID)
	if !found {
		writeRefusal(w, 404, "project_not_found", "Choose a Project that this machine currently lists.")
		return
	}
	if r.Method == http.MethodGet {
		if r.ContentLength > 0 {
			writeRefusal(w, 400, "bad_request", "A unify plan read takes no body.")
			return
		}
		plan, err := projectfiles.Plan(project.Path)
		if err != nil {
			writeProjectFileRefusal(w, err)
			return
		}
		writeJSON(w, plan)
		return
	}
	if !maySend(r) {
		writeRefusal(w, 403, "forbidden", "This device may read the unify plan but cannot apply it.")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeRefusal(w, 400, "bad_request", "Applying a unify plan needs an Idempotency-Key.")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<10+1))
	if err != nil || len(body) > 4<<10 {
		writeRefusal(w, 400, "bad_request", "Send {\"version\": \"<the plan's version>\"}.")
		return
	}
	var input contract.ProjectUnifyApply
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil || dec.Decode(new(any)) != io.EOF || input.Version == "" {
		writeRefusal(w, 400, "bad_request", "Send {\"version\": \"<the plan's version>\"}.")
		return
	}
	// The receipt makes a retried key answer what the first request did,
	// stopped or applied, instead of running the actions a second time.
	receipt := store.ReceiptKey{Scope: scopePlaces, Actor: accessOf(r).verdict.Device, Key: key}
	s.receipted(w, r, receipt, requestDigest([]byte(r.Method), []byte(routePath(r)), body),
		func(status int) bool { return status != http.StatusTooManyRequests },
		func(w http.ResponseWriter) {
			out, err := projectfiles.Apply(project.Path, input.Version)
			if err == nil {
				writeJSON(w, out)
				return
			}
			if out.Outcome != contract.ProjectUnifyOutcomeStopped {
				writeUnifyRefusal(w, err)
				return
			}
			// Stopped part-way: a refusal a generic reader understands, that
			// also says which actions ran and what disk holds now.
			_, out.Error, out.Detail = projectfiles.UnifyRefusal(err)
			if errors.Is(err, os.ErrPermission) {
				out.Error, out.Detail = "file_permission", "A file or directory could not be written with this machine's permissions."
			}
			out.Detail = "Unify stopped: " + out.Detail
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(out)
		})
}

func writeUnifyRefusal(w http.ResponseWriter, err error) {
	if errors.Is(err, os.ErrPermission) {
		writeRefusal(w, 403, "file_permission", "A file or directory could not be written with this machine's permissions.")
		return
	}
	status, code, detail := projectfiles.UnifyRefusal(err)
	writeRefusal(w, status, code, detail)
}
