package http

import (
	"net/http"

	"github.com/sainteye/clawdline/internal/adapters/updatecheck"
)

// updateRoute is GET /v1/update: whether this machine trails the cloud's
// latest build (docs/updates.md). It answers from the checker's last
// background read and never waits on the network. Like /v1/capacity it is for
// any paired device and says nothing about where this daemon keeps its state.
func (s *Server) updateRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeAuthRefusal(w, http.StatusMethodNotAllowed, "bad_request", "Whether this machine is up to date is read with GET.")
		return
	}
	writeJSON(w, s.updateChecker().Status())
}

func (s *Server) updateChecker() *updatecheck.Checker {
	s.updateOnce.Do(func() {
		if s.update == nil {
			s.update = updatecheck.New(WebRoot())
		}
	})
	return s.update
}
