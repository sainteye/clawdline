package http

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/sainteye/clawdline/internal/adapters/devstack"
	"github.com/sainteye/clawdline/internal/contract"
)

// ProjectDevStackReply carries one Project's live DevStack state, probed ports,
// full Project Links, and read-only .devstack.json content for display in the
// Project settings panel.
type ProjectDevStackReply struct {
	Links       []contract.ProjectLink      `json:"links"`
	ObservedAt  float64                     `json:"observedAt"`
	Repository  contract.ProjectRepository  `json:"repository,omitempty"`
	DevStack    contract.ProjectServerSetup `json:"devStack"`
	ServerCount int64                       `json:"serverCount"`
	Stack       *contract.DevStack          `json:"stack,omitempty"`
	Raw         string                      `json:"raw,omitempty"`
	Root        string                      `json:"root,omitempty"`
}

// projectDevStackRoute answers GET /v1/projects/{place}/devstack.
// Like other project inspection routes, no commands from the file are executed.
func (s *Server) projectDevStackRoute(w http.ResponseWriter, r *http.Request, placeID string) {
	if r.URL.RawQuery != "" {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "Project devstack takes no query fields.")
		return
	}
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
		return
	}
	if r.ContentLength > 0 {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "A project devstack read takes no body.")
		return
	}
	project, found := s.workV2Project(r.Context(), placeID)
	if !found {
		writeRefusal(w, http.StatusNotFound, "project_not_found", "Choose a Project that this machine currently lists.")
		return
	}
	home, _ := os.UserHomeDir()
	reading, at := s.projectLinks(r.Context(), project.Path)
	spec, inspection := devstack.Inspect(project.Path, home)

	var devStackSetup contract.ProjectServerSetup
	var serverCount int64
	switch inspection {
	case devstack.InspectionReady:
		serverCount = int64(len(spec.Declared))
		if serverCount > 0 {
			devStackSetup = contract.ProjectServerSetupReady
		} else {
			devStackSetup = contract.ProjectServerSetupEmpty
		}
	case devstack.InspectionUnreadable:
		devStackSetup = contract.ProjectServerSetupAttention
	default:
		devStackSetup = contract.ProjectServerSetupMissing
	}

	reply := ProjectDevStackReply{
		Links:       wireLinks(reading.Links),
		ObservedAt:  seconds(at),
		Repository:  contract.ProjectRepository(reading.Repo),
		DevStack:    devStackSetup,
		ServerCount: serverCount,
		Root:        spec.Root,
	}
	if reply.Root == "" {
		reply.Root = project.Path
	}

	if inspection == devstack.InspectionReady {
		stacks := devstack.Read(r.Context(), []devstack.Spec{spec}, devstack.Listening)
		if len(stacks) > 0 {
			wire := s.wireDevStack(stacks[0])
			reply.Stack = &wire
		}
	}

	// Read raw .devstack.json if present
	devstackPath := filepath.Join(reply.Root, devstack.Filename)
	if data, err := os.ReadFile(devstackPath); err == nil && len(data) <= 128<<10 {
		reply.Raw = string(data)
	} else if inspection == devstack.InspectionUnreadable {
		// If unreadable, try project.Path directly in case root differed
		if directData, directErr := os.ReadFile(filepath.Join(project.Path, devstack.Filename)); directErr == nil && len(directData) <= 128<<10 {
			reply.Raw = string(directData)
		}
	}

	writeJSON(w, reply)
}
