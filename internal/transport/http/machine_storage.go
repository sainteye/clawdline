package http

import (
	"context"
	"sort"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

const reclaimSummaryLimit = 30 * time.Second

func reclaimSummaryAge() time.Duration {
	return min(reclaimSummaryLimit, time.Duration(CapacityLimit(capacity.CacheReclaimSummary))*time.Second)
}

// machineDiskSummary uses the capacity beat's existing statfs reading. An
// absent or stalled reading is unknown; zero free bytes is a known result.
func (s *Server) machineDiskSummary() contract.MachineDiskSummary {
	b := s.capacity()
	_, stalled := b.verdict(time.Now())
	b.mu.Lock()
	defer b.mu.Unlock()
	return machineDiskFromCapacity(b.rows, b.at, stalled)
}

func machineDiskFromCapacity(rows []capacity.Status, at time.Time, stalled bool) contract.MachineDiskSummary {
	out := contract.MachineDiskSummary{}
	if stalled || at.IsZero() {
		return out
	}
	out.At = at.Unix()
	for _, row := range rows {
		if row.Entry.Name == capacity.StoreDB && row.Reading.Known && row.Reading.HasDiskFree {
			out.Known = true
			out.FreeBytes = row.Reading.DiskFree
			break
		}
	}
	return out
}

// Only these fixed codes can reach a paired device. A corrupted or newer
// producer's reason still contributes to the kept total, without leaking its
// arbitrary text (which could contain a path or identifier).
var machineReclaimReasons = map[string]bool{
	orchestrator.WhyGrace: true, orchestrator.WhyOwnerPresent: true,
	orchestrator.WhyOwnerUnknown: true, orchestrator.WhyPathNotOwned: true,
	orchestrator.WhyUnreadable: true, orchestrator.WhyNested: true,
	orchestrator.WhyFiltered: true, orchestrator.WhyPreserveFail: true,
	orchestrator.WhyChanged: true, orchestrator.WhyUnrecorded: true,
	orchestrator.WhyRemoveFailed: true,
}

var machineReclaimFailures = map[string]bool{
	orchestrator.WhyUnreadable: true, orchestrator.WhyPreserveFail: true,
	orchestrator.WhyUnrecorded: true, orchestrator.WhyRemoveFailed: true,
}

// machineReclaimCounts accepts grouped SQL counts, never individual rows.
func machineReclaimCounts(rows []store.ReclaimCount, at int64) contract.MachineReclaimSummary {
	out := contract.MachineReclaimSummary{Known: true, At: at, Reasons: []contract.MachineReclaimReason{}}
	for _, row := range rows {
		switch row.Outcome {
		case orchestrator.ReclaimKept:
			out.Kept += row.Count
			if machineReclaimFailures[row.Reason] {
				out.Failures += row.Count
			}
			if machineReclaimReasons[row.Reason] {
				out.Reasons = append(out.Reasons, contract.MachineReclaimReason{
					Code: contract.MachineReclaimReasonCode(row.Reason), Count: row.Count,
				})
			}
		case "removing":
			out.Removing += row.Count
		}
	}
	sort.Slice(out.Reasons, func(i, j int) bool {
		if out.Reasons[i].Count == out.Reasons[j].Count {
			return out.Reasons[i].Code < out.Reasons[j].Code
		}
		return out.Reasons[i].Count > out.Reasons[j].Count
	})
	return out
}

// machineReclaimSummary holds grouped decisions for the registered age. One
// dashboard polling every three seconds causes at most one SQL aggregation per
// cache interval, including when the store read fails.
func (s *Server) machineReclaimSummary(ctx context.Context) contract.MachineReclaimSummary {
	s.reclaimSummaryMu.Lock()
	defer s.reclaimSummaryMu.Unlock()
	now := time.Now()
	if s.reclaimSummaryAt.IsZero() || now.Sub(s.reclaimSummaryAt) >= reclaimSummaryAge() {
		s.reclaimSummaryAt = now
		s.reclaimSummary = contract.MachineReclaimSummary{At: now.Unix(), Reasons: []contract.MachineReclaimReason{}}
		if s.store != nil {
			if rows, err := s.store.ReclaimCounts(ctx); err == nil {
				s.reclaimSummary = machineReclaimCounts(rows, now.Unix())
			}
		}
	}
	out := s.reclaimSummary
	if s.broker != nil {
		if last := s.broker.LastReclaim(); last != nil {
			out.LastSweepAt = last.At.Unix()
			out.Deferred = int64(last.Deferred)
		}
	}
	return out
}
