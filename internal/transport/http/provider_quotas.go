package http

import (
	"context"
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
			if host := s.antigravitySessions.Load(); host != nil {
				s.providerQuotaService.WithGemini(func(ctx context.Context) ([]providerquota.Window, string) {
					reading, err := host.AccountQuota(ctx)
					if err != nil {
						return nil, "unavailable"
					}
					windows := []providerquota.Window{}
					for _, w := range reading.Windows {
						window := providerquota.Window{Name: w.Name, UsedPercent: w.UsedPercent}
						if w.ResetsAt != 0 {
							reset := w.ResetsAt
							window.ResetsAt = &reset
						}
						windows = append(windows, window)
					}
					return windows, ""
				})
			}
		}
	})
	return s.providerQuotaService
}
func (s *Server) providerQuotas(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Provider quotas are read with GET.")
		return
	}
	snapshot := s.providerQuotaReader().Snapshot(time.Now())
	s.doctorHistory().Record(snapshot)
	writeJSON(w, snapshot)
}
