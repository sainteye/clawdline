package http

import (
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

func (s *Server) resourcesCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		"resources.scratch_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, WindowSeconds: taskresource.ScratchRetentionSecondsLimit, Note: "per-completed-owner retention; presence and preservation are checked independently"}
		},
		"resources.log_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, WindowSeconds: taskresource.LogRetentionSecondsLimit, Note: "non-artifact execution-log retention; retained outputs do not expire"}
		},
		"resources.page_rows": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-inspection-page admission guard"}
		},
		"resources.scan_files": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-resource filesystem scan guard; incomplete scans never authorize deletion"}
		},
		"resources.cache_bytes":         func() capacity.Reading { return s.resourceUsageReading("cache") },
		"resources.cache_total_bytes":   func() capacity.Reading { return s.resourceUsageReading("cache_total") },
		"resources.scratch_total_bytes": func() capacity.Reading { return s.resourceUsageReading("scratch") },
		"resources.disk_reserve_bytes": func() capacity.Reading {
			r := s.store.Reading(s.cfg.Dir)
			if !r.HasDiskFree {
				return capacity.Unmeasured("filesystem free space is unavailable")
			}
			return capacity.Reading{Known: true, DiskFree: r.DiskFree, HasDiskFree: true, Failing: r.DiskFree < taskresource.DiskReserveBytesLimit, Note: "new managed builds require the filesystem reserve; running jobs are not killed"}
		},
	}
}

// Resource scans are scheduled work, never a recursive walk on every dashboard
// read. Until a registered resource has a measured sweep, its usage stays unknown.
func (s *Server) resourceUsageReading(kind string) capacity.Reading {
	if s.broker != nil && s.broker.ResourceServiceRequired && s.broker.Resources == nil {
		return capacity.Unmeasured("resource service unavailable")
	}
	if s.broker == nil || s.broker.Resources == nil {
		return capacity.Unmeasured("resource service unavailable")
	}
	u := s.broker.Resources.Usage()
	if !u.Known {
		return capacity.Unmeasured("registered resource usage awaits a verified sweep")
	}
	used := u.LargestCache
	switch kind {
	case "cache_total":
		used = u.CacheTotal
	case "scratch":
		used = u.ScratchTotal
	}
	return capacity.Reading{Known: true, Used: used, Note: "last explicit or background resource measurement; admission rechecks before new work"}
}
