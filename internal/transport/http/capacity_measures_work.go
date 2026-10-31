package http

import (
	"context"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// workCapacityMeasures measures every row of the register's "work" area
// (capacity.AreaOf), one reading per row.
func (s *Server) workCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		capacity.WorkOpen: func() capacity.Reading {
			n, err := s.store.WorkOpenCount(context.Background())
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			v2, err := s.store.WorkV2CapacityCounts(context.Background())
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: n + v2["open"]}
		},
		capacity.WorkPlanning: func() capacity.Reading {
			return s.workV2CapacityReading("planning")
		},
		capacity.WorkAssignments: func() capacity.Reading {
			return s.workV2CapacityReading("assignments")
		},
		capacity.WorkDocumentsPerItem: func() capacity.Reading {
			return s.workV2DocumentCapacityReading()
		},
		capacity.WorkImagesPerItem: func() capacity.Reading {
			return s.workV2CapacityReading("images_per_item")
		},
		capacity.WorkImageBytes: func() capacity.Reading {
			return s.workV2CapacityReading("image_bytes_max")
		},
		capacity.WorkImageBytesPerItem: func() capacity.Reading {
			return s.workV2CapacityReading("image_bytes_per_item")
		},
		capacity.WorkImageBytesTotal: func() capacity.Reading {
			return s.workV2CapacityReading("image_bytes_total")
		},
		capacity.WorkImageRequestBodyBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-request guard; no retained buffer"}
		},
		capacity.WorkStepsPerItem: func() capacity.Reading {
			return s.workV2CapacityReading("steps_per_item")
		},
		capacity.WorkRootLandingsPerItem: func() capacity.Reading {
			return s.workV2CapacityReading("root_landings_per_item")
		},
		capacity.WorkItemTitleBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-write guard; no retained buffer"}
		},
		capacity.WorkItemDescriptionBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-write guard; no retained buffer"}
		},
		capacity.WorkItemUserActionBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-write guard; no retained buffer"}
		},
		capacity.WorkCompletionReasonBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-write guard; no retained buffer"}
		},
		capacity.WorkRequestBodyBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-request guard; no retained buffer"}
		},
		capacity.WorkDigests: func() capacity.Reading {
			n, err := s.store.DigestCount(context.Background())
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: n}
		},
		capacity.WorkCardDescriptionRunes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-card description preview; complete text remains on the one-item read"}
		},
		capacity.WorkListPageRows: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-answer Board page; later rows remain behind next_cursor"}
		},
		capacity.WorkIssueSyncResponseBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-answer guard on GitHub replies; no retained buffer"}
		},
		capacity.WorkIssueSyncTokenFileBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-read guard on the one GitHub token file"}
		},
		capacity.WorkIssueSyncDeviceSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-flow deadline; a new start issues a new code"}
		},
	}
}
