package http

import (
	"context"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
)

// runningHeavyCallback returns the one callback that both declared heavy work
// and presently owns the machine's compile slot. A queued callback, a callback
// waiting for another result, and a direct heavy command are not this marker.
func (s *Server) runningHeavyCallback(ctx context.Context) (string, *contract.SessionHeavyWork) {
	if s.broker == nil {
		return "", nil
	}
	leases, err := s.broker.Leases(ctx)
	if err != nil {
		return "", nil
	}
	for _, lease := range leases {
		if lease.Resource != orchestrator.ResourceCompile || lease.Holder == nil {
			continue
		}
		holder := lease.Holder
		if holder.CallbackTaskID == "" || holder.Session == "" {
			return "", nil
		}
		record, _, err := s.broker.Record(ctx, holder.CallbackTaskID)
		if err != nil {
			return "", nil
		}
		return heavyCallbackOwner(holder, record)
	}
	return "", nil
}

func heavyCallbackOwner(holder *orchestrator.LeaseHolder, record orchestrator.Record) (string, *contract.SessionHeavyWork) {
	if holder == nil || holder.CallbackTaskID == "" || holder.Session == "" || holder.Phase != "running" || holder.Liveness != "alive" ||
		record.ID != holder.CallbackTaskID || record.Callback == nil || record.Callback.Intent != orchestrator.CallbackIntentHeavy ||
		record.Root == nil || record.Root.SessionID != holder.Session ||
		(record.State != orchestrator.StateQueued && record.State != orchestrator.StateSpawning && record.State != orchestrator.StateBriefed) {
		return "", nil
	}
	return holder.Session, &contract.SessionHeavyWork{TaskID: record.ID, Reason: holder.Reason}
}
