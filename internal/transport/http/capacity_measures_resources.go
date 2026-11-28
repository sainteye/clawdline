package http

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/gocache"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

func (s *Server) resourcesCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		"resources.ready_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, WindowSeconds: taskresource.PreparationReadySecondsLimit, Note: "readiness expires and is consumed once; policy, allocation, use and service incarnation changes require fresh preparation"}
		},
		"resources.scratch_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, WindowSeconds: taskresource.ScratchRetentionSecondsLimit, Note: "per-completed-owner retention; presence and preservation are checked independently"}
		},
		"resources.log_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, WindowSeconds: taskresource.LogRetentionSecondsLimit, Note: "non-artifact execution-log retention; artifacts follow resources.artifact_seconds"}
		},
		capacity.ResourceArtifactRetention: func() capacity.Reading {
			return capacity.Reading{Known: true, WindowSeconds: taskresource.ArtifactRetentionSecondsLimit, Note: "per-task artifact retention after the task ended and its landing settled; pins, pending landings, unknown end times and open readers keep the files"}
		},
		"resources.page_rows": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-inspection-page admission guard"}
		},
		capacity.ResourceCleanupBatchRows: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-page resource discovery bound for automatic cleanup"}
		},
		capacity.ResourceCleanupItemSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, WindowSeconds: capacity.ResourceCleanupItemSecondsLimit, Note: "time budget for one automatic cleanup item, including any replan"}
		},
		capacity.ResourceCleanupPlanAttempts: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "plan attempt bound for one automatic cleanup item"}
		},
		capacity.ResourceCleanupBackoffSeconds: func() capacity.Reading {
			return capacity.Reading{Known: true, WindowSeconds: capacity.ResourceCleanupBackoffSecondsLimit, Note: "retry delay when automatic cleanup cannot read the registry or enter maintenance"}
		},
		"resources.scan_files": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-resource filesystem scan guard; incomplete scans never authorize deletion"}
		},
		"resources.cache_bytes":         func() capacity.Reading { return s.resourceUsageReading("cache") },
		"resources.cache_total_bytes":   func() capacity.Reading { return s.resourceUsageReading("cache_total") },
		"resources.scratch_total_bytes": func() capacity.Reading { return s.resourceUsageReading("scratch") },
		capacity.MachineDiskLow: func() capacity.Reading {
			r := s.store.Reading(s.cfg.Dir)
			if !r.HasDiskFree {
				return capacity.Unmeasured("filesystem free space is unavailable")
			}
			return lowDiskReading(r.DiskFree)
		},
		"resources.disk_source_scan_files": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-source file bound for a low free-space notice; a larger source is unmeasured"}
		},
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

// lowDiskReading counts the shortfall below the alert value, so the row is ok
// with room to spare and full (one notice per cooldown) below it.
func lowDiskReading(free int64) capacity.Reading {
	short := max(taskresource.LowDiskAlertBytesLimit-free, 0)
	return capacity.Reading{Known: true, Used: short, DiskFree: free, HasDiskFree: true,
		Failing: free < taskresource.LowDiskAlertBytesLimit,
		Note:    "used is the shortfall below the alert value; nothing is removed by this row"}
}

// lowDiskSourceTimeout bounds the whole largest-source measurement.
const lowDiskSourceTimeout = 20 * time.Second

// lowDiskSources measures the usual large users: the default Go build cache,
// this daemon's task data and the shared temporary directory. Each is bounded
// by file count; a source that cannot be read is reported unmeasured.
func (s *Server) lowDiskSources(ctx context.Context) []capacity.DiskSource {
	ctx, cancel := context.WithTimeout(ctx, lowDiskSourceTimeout)
	defer cancel()
	tmp := "/tmp"
	if runtime.GOOS == "windows" {
		tmp = os.TempDir()
	}
	paths := []struct{ what, dir string }{
		{"gocache", defaultGoCache()},
		{"tasks", filepath.Join(s.cfg.Dir, "tasks")},
		{"tmp", tmp},
	}
	out := make([]capacity.DiskSource, 0, len(paths))
	for _, p := range paths {
		src := capacity.DiskSource{What: p.what}
		if p.dir != "" {
			if n, complete, err := gocache.TreeBytes(ctx, p.dir, taskresource.DiskSourceScanFilesLimit); err == nil {
				src.Bytes, src.Known, src.Complete = n, true, complete
			}
		}
		out = append(out, src)
	}
	return out
}

// defaultGoCache is the cache Go uses without GOCACHE: the user cache
// directory's go-build. A daemon need not have `go` on its PATH.
func defaultGoCache() string {
	if v := os.Getenv("GOCACHE"); v != "" && v != "off" && filepath.IsAbs(v) {
		return v
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "go-build")
}
