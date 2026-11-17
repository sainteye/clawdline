package projectassistant

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// DefaultIdleCompactionThreshold is the default duration a Bot must be inactive
// before background dreaming and memory compaction may run.
const DefaultIdleCompactionThreshold = 10 * time.Minute

// IdleBotState represents activity counters used to evaluate whether a Bot
// is safe for background dreaming or compaction without interfering with humans.
type IdleBotState struct {
	LastActiveAt time.Time
	ActiveTurns  int
	PendingWork  int
}

// IsIdle checks if the Bot has no active turns, no pending work, and has rested
// past the specified idle threshold.
func (s IdleBotState) IsIdle(now time.Time, threshold time.Duration) bool {
	if s.ActiveTurns > 0 || s.PendingWork > 0 {
		return false
	}
	if s.LastActiveAt.IsZero() {
		return true
	}
	return now.Sub(s.LastActiveAt) >= threshold
}

// CompactedMemoryPlan represents the result of a memory compaction pass,
// holding new aggregated entries and the list of obsolete entry IDs to tombstone.
type CompactedMemoryPlan struct {
	AggregatedEntries []MemoryEntry
	TombstoneIDs      []string
	Summary           string
}

// CompactMemoryEntries takes a list of active memory entries and groups related
// facts or fragmented notes into structured concept summaries. Sources from all
// aggregated entries are preserved to maintain complete provenance.
func CompactMemoryEntries(entries []MemoryEntry, groupThreshold int) (CompactedMemoryPlan, bool) {
	if groupThreshold <= 1 {
		groupThreshold = 2
	}
	if len(entries) < groupThreshold {
		return CompactedMemoryPlan{}, false
	}

	// Filter out forgotten entries
	active := make([]MemoryEntry, 0, len(entries))
	for _, e := range entries {
		if !e.Forgotten && strings.TrimSpace(e.Text) != "" {
			active = append(active, e)
		}
	}
	if len(active) < groupThreshold {
		return CompactedMemoryPlan{}, false
	}

	// Group entries by topic prefix or cluster key.
	// If an entry starts with "topic: ..." or "[topic] ...", use that topic.
	// Otherwise group by leading word or general notes.
	groups := make(map[string][]MemoryEntry)
	for _, e := range active {
		topic := extractMemoryTopic(e.Text)
		groups[topic] = append(groups[topic], e)
	}

	var plan CompactedMemoryPlan
	var changed bool

	// Process groups deterministically
	topics := make([]string, 0, len(groups))
	for t := range groups {
		topics = append(topics, t)
	}
	sort.Strings(topics)

	for _, topic := range topics {
		items := groups[topic]
		if len(items) < groupThreshold {
			continue
		}

		// Merge items into an aggregated concept entry
		var combinedTexts []string
		sourceMap := make(map[string]MemorySource)
		var tombIDs []string

		for _, item := range items {
			combinedTexts = append(combinedTexts, strings.TrimSpace(item.Text))
			tombIDs = append(tombIDs, item.ID)
			for _, s := range item.Sources {
				sourceMap[s.ID] = s
			}
		}

		var mergedSources []MemorySource
		for _, s := range sourceMap {
			mergedSources = append(mergedSources, s)
		}
		sort.Slice(mergedSources, func(i, j int) bool {
			return mergedSources[i].ID < mergedSources[j].ID
		})

		first := items[0]
		combinedDigest := sha256.Sum256([]byte(strings.Join(tombIDs, ":")))
		aggID := fmt.Sprintf("compacted-%s-%s", topic, hex.EncodeToString(combinedDigest[:6]))

		aggText := fmt.Sprintf("[%s] %s", topic, strings.Join(combinedTexts, "; "))
		if len(aggText) > 2000 {
			aggText = aggText[:2000]
		}

		plan.AggregatedEntries = append(plan.AggregatedEntries, MemoryEntry{
			ID:        aggID,
			Key:       first.Key,
			Project:   first.Project,
			Text:      aggText,
			Sources:   mergedSources,
			Revision:  first.Revision,
			Forgotten: false,
		})
		plan.TombstoneIDs = append(plan.TombstoneIDs, tombIDs...)
		changed = true
	}

	if changed {
		plan.Summary = fmt.Sprintf("Compacted %d memory entries into %d aggregated concepts",
			len(plan.TombstoneIDs), len(plan.AggregatedEntries))
	}
	return plan, changed
}

func extractMemoryTopic(text string) string {
	text = strings.TrimSpace(text)
	if idx := strings.Index(text, ":"); idx > 0 && idx < 30 {
		prefix := strings.ToLower(strings.TrimSpace(text[:idx]))
		if !strings.Contains(prefix, " ") {
			return prefix
		}
	}
	if strings.HasPrefix(text, "[") {
		if idx := strings.Index(text, "]"); idx > 1 && idx < 30 {
			return strings.ToLower(strings.TrimSpace(text[1:idx]))
		}
	}
	words := strings.Fields(text)
	if len(words) > 0 {
		w := strings.ToLower(words[0])
		w = strings.Trim(w, ":,.;[]()")
		if len(w) > 2 && len(w) < 20 {
			return w
		}
	}
	return "general"
}
