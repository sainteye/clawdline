package http

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/sainteye/clawdline/internal/app/taskresources"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

// This adapter is machine-authenticated only. A future Bot adapter must
// construct a project-limited Inspect/Reclaim scope from its own live grant;
// it must not receive this token or accept a scope from a request body.
func (s *Server) taskResourcesRoute(w http.ResponseWriter, r *http.Request) {
	if !s.machineOnly(w, r) {
		return
	}
	service := s.broker.Resources
	if service == nil {
		writeRefusal(w, 503, "resource_service_unavailable", "Managed resource service is unavailable.")
		return
	}
	scope := taskresources.Scope{Actor: "machine", AllProjects: true, Inspect: true, Reclaim: true, Publish: true, Unpin: true}
	path := strings.TrimSuffix(routePath(r), "/")
	respond := func(v any, err error) {
		if err != nil {
			writeRefusal(w, 409, err.Error(), "Resource operation refused; inspect the reason and preserved data before retrying.")
			return
		}
		switch v.(type) {
		case taskresources.Page:
			var out contract.TaskResourcePage
			recast(v, &out)
			writeJSON(w, out)
		case taskresources.Allocation:
			var out contract.TaskResourceAllocation
			recast(v, &out)
			writeJSON(w, out)
		case taskresources.Plan:
			var out contract.TaskResourcePlan
			recast(v, &out)
			writeJSON(w, out)
		case taskresources.Result:
			var out contract.TaskResourceResult
			recast(v, &out)
			writeJSON(w, out)
		case taskresource.Resource:
			var out contract.TaskResourceResource
			recast(v, &out)
			writeJSON(w, out)
		case taskresource.Usage:
			var out contract.TaskResourceUsage
			recast(v, &out)
			writeJSON(w, out)
		default:
			writeRefusal(w, 500, "resource_contract_unknown", "Resource response contract is unavailable.")
		}
	}
	if path == "/v1/orchestrator/resources" && r.Method == http.MethodGet {
		limit := taskresource.PageRowsLimit
		if n := r.URL.Query().Get("limit"); n != "" {
			v, err := strconv.Atoi(n)
			if err != nil {
				writeRefusal(w, 400, "resource_page_size_invalid", "Invalid resource page size.")
				return
			}
			limit = v
		}
		out, err := service.Inventory(r.Context(), scope, r.URL.Query().Get("after"), limit)
		respond(out, err)
		return
	}
	if r.Method != http.MethodPost {
		writeNoSuchRoute(w, r)
		return
	}
	switch strings.TrimPrefix(path, "/v1/orchestrator/resources/") {
	case "allocate":
		var body struct {
			Kind  string             `json:"owner_kind"`
			Owner string             `json:"owner_id"`
			Class taskresource.Class `json:"class"`
		}
		if !decodeClosed(w, r, &body, "owner_kind", "owner_id", "class") {
			return
		}
		if err := s.broker.AdmitHeavyResources(r.Context()); err != nil {
			writeBrokerError(w, err)
			return
		}
		out, err := service.Allocate(r.Context(), scope, body.Kind, body.Owner, body.Class)
		respond(out, err)
	case "plan":
		var body struct {
			IDs []string `json:"resources"`
		}
		if !decodeClosed(w, r, &body, "resources") {
			return
		}
		out, err := service.Plan(r.Context(), scope, body.IDs)
		respond(out, err)
	case "apply":
		var body struct {
			Plan      taskresources.Plan `json:"plan"`
			Execution string             `json:"execution"`
		}
		if !decodeClosed(w, r, &body, "plan", "execution") {
			return
		}
		out, err := service.Apply(r.Context(), scope, body.Plan, body.Execution)
		respond(out, err)
	case "publish":
		var body struct {
			ID string `json:"resource"`
		}
		if !decodeClosed(w, r, &body, "resource") {
			return
		}
		out, err := service.Seal(r.Context(), scope, body.ID)
		respond(out, err)
	case "pin":
		var body struct {
			ID     string `json:"resource"`
			Pinned *bool  `json:"pinned"`
		}
		if !decodeClosed(w, r, &body, "resource", "pinned") {
			return
		}
		if body.Pinned == nil {
			writeRefusal(w, 400, "resource_protection_invalid", "Specify pinned explicitly.")
			return
		}
		out, err := service.Pin(r.Context(), scope, body.ID, *body.Pinned)
		respond(out, err)
	case "measure":
		var body struct{}
		if !decodeClosed(w, r, &body) {
			return
		}
		out, err := service.Measure(r.Context())
		respond(out, err)
	case "admit":
		var body struct{}
		if !decodeClosed(w, r, &body) {
			return
		}
		err := s.broker.AdmitHeavyResources(r.Context())
		if err != nil {
			writeBrokerError(w, err)
			return
		}
		writeJSON(w, contract.TaskResourceAdmission{Admitted: true})
	default:
		writeNoSuchRoute(w, r)
	}
}

func (s *Server) CloseTaskResources() error {
	if s.broker != nil && s.broker.Resources != nil {
		return s.broker.Resources.FS.Close()
	}
	return nil
}
