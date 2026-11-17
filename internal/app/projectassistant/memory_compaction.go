package projectassistant

import (
	"context"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// CompactBotMemory executes background memory compaction for an idle Bot.
// It verifies idle eligibility, aggregates related fragmented entries into concepts,
// writes the aggregated entries with merged provenance, and tombstones the obsolete
// source entries through safe Forget revisions.
func (r *Engine) CompactBotMemory(ctx context.Context, key pa.BotKey, idleState pa.IdleBotState, idleThreshold time.Duration, groupThreshold int) (*pa.CompactedMemoryPlan, pa.MemoryRevision, error) {
	if !idleState.IsIdle(time.Now(), idleThreshold) {
		return nil, pa.MemoryRevision{}, runtimeFailure(pa.Invalid, "bot_not_idle")
	}

	bot, err := r.o.Repository.Bot(ctx, key)
	if err != nil {
		return nil, pa.MemoryRevision{}, err
	}

	current, err := r.o.Memory.Current(ctx, key)
	if err != nil {
		return nil, pa.MemoryRevision{}, err
	}

	entries, err := r.MemoryEntries(ctx, key)
	if err != nil {
		return nil, pa.MemoryRevision{}, err
	}

	plan, changed := pa.CompactMemoryEntries(entries, groupThreshold)
	if !changed {
		return nil, current, nil
	}

	// 1. Save all aggregated concept entries
	for _, agg := range plan.AggregatedEntries {
		id := "compaction-" + agg.ID
		cur, err := r.saveBotMemory(ctx, id, agg, agg.Sources, current)
		if err != nil {
			return nil, pa.MemoryRevision{}, err
		}
		current = cur
	}

	// 2. Tombstone the obsolete aggregated source entries
	if len(plan.TombstoneIDs) > 0 {
		forgetCmd := r.command(key, pa.ForgetOperation, "compaction-tombstone", struct {
			Entries []string
		}{plan.TombstoneIDs})

		cur, err := r.o.Memory.Forget(ctx, ForgetRequest{
			Command:          forgetCmd,
			Project:          bot.Project,
			ExpectedRevision: current.Revision,
			SourceIDs:        plan.TombstoneIDs,
		})
		if err != nil {
			return nil, pa.MemoryRevision{}, err
		}
		current = cur
	}

	return &plan, current, nil
}
