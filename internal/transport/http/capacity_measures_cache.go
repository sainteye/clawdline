package http

import (
	"fmt"
	"strconv"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// cacheCapacityMeasures measures every row of the register's "cache" area
// (capacity.AreaOf), one reading per row.
func (s *Server) cacheCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		"cache.provider_quota": func() capacity.Reading { return s.providerQuotaReader().Reading() },
		"cache.provider_quota_response_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-read ceiling on credential and provider response bytes; oversized data is refused"}
		},
		"cache.provider_quota_refresh_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "minimum interval between provider metadata reads; Retry-After can extend it"}
		},
		"cache.provider_quota_request_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-provider deadline across metadata requests; no retries"}
		},
		capacity.CacheTranscriptTitles: func() capacity.Reading {
			h, ok := s.inventory.Identity.(*transcript.Host)
			if !ok {
				return capacity.Unmeasured("this inventory does not keep conversation titles")
			}
			return h.Titles().Reading()
		},
		capacity.CacheSessionActivity: func() capacity.Reading {
			h, ok := s.inventory.Identity.(*transcript.Host)
			if !ok {
				return capacity.Unmeasured("this inventory reads no session records")
			}
			return h.Movements().Reading()
		},
		capacity.CacheBackgroundAgents: func() capacity.Reading {
			if s.agents == nil {
				return capacity.Unmeasured("this server keeps no background-work cache")
			}
			return s.agents.Reading()
		},
		capacity.CacheSessionSkills: func() capacity.Reading { return s.skillsReading() },
		capacity.CacheSessionLinks:  func() capacity.Reading { return s.linksReading() },
		// The screens the session list holds, and the captures it has in
		// flight (internal/app/screen_held.go).
		// An inventory with no held screens is a known zero, not an
		// unmeasured row: it reads its terminals live, so it holds none and
		// has none in flight.
		capacity.CacheTerminalScreens: func() capacity.Reading {
			if s.inventory.Held == nil {
				return capacity.Reading{Known: true, Note: "this inventory holds no screens"}
			}
			held, _ := s.inventory.Held.Reading()
			return held
		},
		capacity.CacheSessionInventory: func() capacity.Reading {
			if s.readings == nil {
				return capacity.Reading{Known: true, Note: "this server retains no inventory readings"}
			}
			return s.readings.RetentionReading()
		},
		capacity.CacheSourceAnswer: func() capacity.Reading {
			if s.readings == nil {
				return capacity.Reading{Known: true, Note: "this server keeps no source answers"}
			}
			return s.readings.AnswerReading()
		},
		capacity.CachePinnedRead: func() capacity.Reading {
			if s.readings == nil {
				return capacity.Reading{Known: true, Note: "this server checks every pinned read against a scan of its own"}
			}
			return s.readings.PinnedReading(time.Duration(CapacityLimit(capacity.CachePinnedRead)) * time.Second)
		},
		capacity.CacheGitNotRepo:     func() capacity.Reading { return gitNotRepos().AgeReading() },
		capacity.CacheGitNotRepoRows: func() capacity.Reading { return gitNotRepos().RowsReading() },
		capacity.CacheReclaimSummary: func() capacity.Reading {
			s.reclaimSummaryMu.Lock()
			defer s.reclaimSummaryMu.Unlock()
			if s.reclaimSummaryAt.IsZero() {
				return capacity.Reading{Known: true, Note: "no grouped reclaim reading yet"}
			}
			if time.Since(s.reclaimSummaryAt) >= reclaimSummaryAge() {
				s.reclaimSummaryAt = time.Time{}
				s.reclaimSummary = contract.MachineReclaimSummary{}
				return capacity.Reading{Known: true, Note: "expired grouped reclaim reading"}
			}
			return capacity.Reading{Known: true, Used: int64(time.Since(s.reclaimSummaryAt) / time.Second)}
		},
		// The plan-window readings held. Measuring counts the map; it takes no
		// reading, which would be this row measuring itself into existence.
		capacity.CacheAssistantQuota: func() capacity.Reading { return quotaReader().Reading() },
		capacity.CacheSessionExecutionMemo: func() capacity.Reading {
			held, skipped := s.executions.reading()
			return capacity.Reading{Known: true, Used: int64(held),
				Note: strconv.FormatInt(skipped, 10) + " unchanged scan(s) answered without a write"}
		},
		capacity.CacheSessionProjectKeys: func() capacity.Reading { return s.projectKeys.Reading() },
		capacity.CacheImageThumbs: func() capacity.Reading {
			held, entries, evicted, lastAt := s.thumbs.Reading()
			return capacity.Reading{Known: true, Used: held,
				Counters: capacity.Counters{Evicted: evicted, LastActionAt: lastAt},
				Note:     fmt.Sprintf("%d thumbnail(s) held", entries)}
		},
	}
}
