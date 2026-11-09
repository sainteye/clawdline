package http

import (
	"context"
	"math"
	"net/http"
	"os"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/providerquota"
	"github.com/sainteye/clawdline/internal/contract"
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

// Both full and summary Session Info read the responding machine's account
// cache. No browser-side provider cache can substitute another machine's row.
func (s *Server) grokSessionLimits(now time.Time) *contract.SessionLimits {
	return grokLimits(s.providerQuotaReader().Grok(now), now)
}

func grokLimits(row providerquota.Row, now time.Time) *contract.SessionLimits {
	out := &contract.SessionLimits{Detail: "Grok", ReadAtMs: now.UnixMilli(), Windows: []contract.SessionLimitWindow{}, Stale: row.Stale || row.Problem != ""}
	if row.ObservedAt != nil {
		out.At = *row.ObservedAt
		age := max(int64(0), now.Unix()-out.At)
		fresh := int64(providerquota.RefreshSecondsLimit)
		out.AgeSeconds, out.FreshForSeconds = &age, &fresh
		out.Stale = out.Stale || age >= fresh
	}
	for _, w := range row.Windows {
		if math.IsNaN(w.UsedPercent) || math.IsInf(w.UsedPercent, 0) || w.UsedPercent < 0 || w.UsedPercent > 100 {
			continue
		}
		v := contract.SessionLimitWindow{Name: w.Name, UsedPercent: w.UsedPercent, Hit: w.UsedPercent >= 100}
		if w.ResetsAt != nil {
			v.ResetsAt = *w.ResetsAt
		}
		out.Windows = append(out.Windows, v)
	}
	if len(out.Windows) == 0 {
		out.UnknownReason = contract.AssistantUnknownReasonNoReading
		if row.Problem == "not_signed_in" {
			out.UnknownReason = contract.AssistantUnknownReasonNoRecord
		}
	}
	return out
}
