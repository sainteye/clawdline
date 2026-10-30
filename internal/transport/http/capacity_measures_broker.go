package http

import (
	"context"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// brokerCapacityMeasures measures every row of the register's "broker" area
// (capacity.AreaOf), one reading per row.
func (s *Server) brokerCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		"orchestrator.delivery_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-delivery evidence admission guard"}
		},
		"orchestrator.delivery_commits": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-delivery evidence admission guard"}
		},
		capacity.DispatchAlsoWorkIDs: func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-dispatch guard"} },
		capacity.HandoffWorktrees:    func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-handoff guard"} },
		capacity.HandoffPatchBytes:   func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-handoff guard"} },
		capacity.HandoffPackBytes:    func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-handoff guard"} },
		capacity.OpeningStuckSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-row age ceiling; the row's updated time persists; no retained buffer"}
		},
		// W5's three rows, read from the store the broker writes them to.
		capacity.LeasesQueue: func() capacity.Reading {
			leases, err := s.store.Leases(context.Background())
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			r := capacity.Reading{Known: true}
			for _, l := range leases {
				if n := int64(len(l.Waiters)); n > r.Used {
					r.Used, r.Note = n, "longest line: "+l.Resource
				}
			}
			return r
		},
		capacity.CoordinatorAliases: func() capacity.Reading {
			rec, _, err := s.store.Coordinator(context.Background())
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			r := capacity.Reading{Known: true}
			if rec != nil {
				r.Used = int64(len(rec.Aliases))
			}
			return r
		},
		capacity.CoordinatorBindAttempts: func() capacity.Reading {
			// Bind retries live only inside a separate, short-lived CLI process.
			// The daemon retains no attempts; each CLI prints the refusal itself.
			return capacity.Reading{Known: true, Note: "CLI-local attempts; none retained by the daemon"}
		},
		capacity.WaitsOpen: func() capacity.Reading {
			n, err := s.store.OpenWaitCount(context.Background())
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: int64(n)}
		},
	}
}
