package http

import (
	"context"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// boardCapacityMeasures measures every row of the register's "board" area
// (capacity.AreaOf), one reading per row.
func (s *Server) boardCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		"usage.daily_days": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "57 local calendar days per source; older days remain missing"}
		},
		"usage.daily_rows": func() capacity.Reading {
			n, err := s.store.UsageDailyCount(context.Background())
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			_, _, evicted, err := s.store.UsageDailyCoverage(context.Background())
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: n, Counters: capacity.Counters{Evicted: evicted}}
		},
		"usage.overview_tasks": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-read bound; overview coverage says truncated"}
		},
		capacity.UsageWorkCursorRows: func() capacity.Reading {
			n, err := s.store.WorkCursorCount(context.Background())
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: n}
		},
		capacity.UsageWorkCursorQueue: func() capacity.Reading {
			return capacity.Reading{Known: true, Used: int64(s.workUnits().Waiting())}
		},
		capacity.UsageWorkUnitsInAnswer: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-read bound; the answer says truncated"}
		},
		capacity.RunCreatedItems: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-run guard, counted inside each create; no retained buffer"}
		},
		capacity.RunDecisionAnswerAge: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-request age check of the answer a Bot item cites; no retained buffer"}
		},
		capacity.RunClaimedItems: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-run guard, counted inside each claim; no retained buffer"}
		},
		capacity.EpicChildItems: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Epic guard, counted inside each child create; no retained buffer"}
		},
		capacity.PlanReviewBlockingListed: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-refusal list of blocking findings; no retained buffer"}
		},
		// T4's three rows (proposals.go).
		capacity.ProposalsOpen: func() capacity.Reading {
			c, err := s.participation().Counts(context.Background())
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			v2, err := s.store.WorkV2CapacityCounts(context.Background())
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: c.Pending + v2["proposals"], OldestAt: c.Oldest}
		},
		capacity.DecisionsOpen: func() capacity.Reading {
			counts, err := s.store.DecisionCounts(context.Background())
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: counts[work.DecisionOpen]}
		},
		capacity.BoardReceipts: func() capacity.Reading { return s.boardReceiptReading() },
	}
}
