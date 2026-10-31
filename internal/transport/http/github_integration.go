package http

import (
	"errors"
	"net/http"
	"path/filepath"

	"github.com/sainteye/clawdline/internal/adapters/githubissues"
)

// githubIntegration is this machine's one GitHub authorization for issue
// sync (docs/issue-sync.md). It is made on first use, so a machine that never
// connects GitHub never reads its token file.
func (s *Server) githubIntegration() *githubissues.Connection {
	s.githubMu.Lock()
	defer s.githubMu.Unlock()
	if s.githubConn == nil {
		s.githubConn = githubissues.NewConnection(githubissues.NewClient(), filepath.Join(s.cfg.Dir, "integrations", "github"))
	}
	return s.githubConn
}

// StopGitHub ends a device flow still waiting for the person.
func (s *Server) StopGitHub() {
	s.githubMu.Lock()
	defer s.githubMu.Unlock()
	if s.githubConn != nil {
		s.githubConn.Close()
	}
}

// githubIntegrationRoute answers the connection's state (GET), forgets the
// token (DELETE). No answer carries a token or the device code.
func (s *Server) githubIntegrationRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	conn := s.githubIntegration()
	switch r.Method {
	case http.MethodGet, http.MethodHead:
	case http.MethodDelete:
		if err := conn.Disconnect(); err != nil {
			writeRefusal(w, http.StatusInternalServerError, "github_token_unwritable", "The GitHub token could not be removed from this machine.")
			return
		}
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or DELETE")
		return
	}
	writeJSON(w, conn.Status())
}

// githubDeviceRoute starts the device flow (POST) and reads its progress
// (GET). Starting again while a code is waiting answers the same code, so a
// retried POST cannot open a second authorization.
func (s *Server) githubDeviceRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	conn := s.githubIntegration()
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		writeJSON(w, conn.Status())
	case http.MethodPost:
		status, err := conn.Start(r.Context())
		switch {
		case err == nil:
			writeJSON(w, status)
		case errors.Is(err, githubissues.ErrFlowDisabled):
			writeRefusal(w, http.StatusBadGateway, "device_flow_disabled", "GitHub has device sign-in turned off for Clawdline's app.")
		default:
			writeRefusal(w, http.StatusBadGateway, "github_unreachable", "GitHub did not answer the sign-in request. Try again.")
		}
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or POST")
	}
}
