package capacity

// Operational owner history, never evidence uploaded to an operator. All rows
// are diagnostic-only, including store failures and shed observations.
func apiPerformanceBounds() []Entry {
	row := func(name string, unit Unit, n int64, action Action, sources ...string) Entry {
		class := Buffer
		if action == EvictOldest {
			class = Observation
		}
		for i := range sources {
			sources[i] = "internal/domain/apiperformance." + sources[i]
		}
		return Entry{Name: "diagnostics.api_" + name, Class: class, Unit: unit, Limit: n, AtLimit: action, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: sources}
	}
	return []Entry{
		row("queue", Rows, 1024, Refuse, "MaxQueue", "New:chan(queue)"),
		row("routes", Rows, 128, Refuse, "MaxRoutes"),
		row("route_bytes", Bytes, 256, Refuse, "MaxRouteBytes"),
		row("minutes", Rows, 61, EvictOldest, "MaxMinutes"),
		row("hours", Rows, 169, EvictOldest, "MaxHours"),
		row("readers", Rows, 2, Refuse, "MaxReaders", "New:chan(readers)"),
		row("active", Rows, 512, Refuse, "MaxActive"),
		row("query_bytes", Bytes, 2<<20, Refuse, "MaxQueryBytes"),
		row("query_seconds", Seconds, 2, Refuse, "MaxQuerySeconds"),
		row("store", Bytes, 64<<20, Refuse, "MaxStoreBytes"),
		row("snapshot", Bytes, 32<<20, Refuse, "MaxSnapshotBytes"),
		row("retention", Seconds, 169*60*60, EvictOldest, "MaxRetentionSeconds"),
		row("catchup", Seconds, 2, Refuse, "MaxCatchupSeconds"),
	}
}
