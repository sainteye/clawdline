package http

import (
	"net/http"
	"os"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/providerquota"
)

func (s *Server) providerQuotaReader() *providerquota.Service {
	s.providerQuotaOnce.Do(func() {
		if s.providerQuotaService == nil {
			home, _ := os.UserHomeDir()
			s.providerQuotaService = providerquota.New(home, quotaReader())
		}
	})
	return s.providerQuotaService
}
func (s *Server) providerQuotas(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Provider quotas are read with GET.")
		return
	}
	writeJSON(w, s.providerQuotaReader().Snapshot(time.Now()))
}
