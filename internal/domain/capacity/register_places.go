package capacity

// placesBounds is every registered row named places.*.
func placesBounds() []Entry {
	return []Entry{
		{
			// Executable compatibility probes retain only one bounded stdout.
			Name: "places.antigravity_probe_output_bytes", Class: Buffer, Unit: Bytes,
			Limit: 64 << 10, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/antigravity.CLIProbeOutputLimit"},
		},
		{
			// One changed executable gets one deadline; orphan pipe draining
			// is independently bounded by the same WaitDelay.
			Name: "places.antigravity_probe_seconds", Class: Buffer, Unit: Seconds,
			Limit: 3, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/antigravity.CLIProbeTimeLimit"},
		},
		{
			// CLAWDLINE_NEXT_DIR/places.json: explicit project directories and
			// directories deliberately hidden from the lists. Each
			// row is a person's choice, so the daemon never evicts one; a full
			// registry refuses a new path and `clawdline project remove` is its
			// deliberate exit.
			Name: PlacesRegistered, Class: Evidence, Unit: Rows,
			Limit: 512, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Notice, Health},
			EvictedBy: Person,
			Projects:  true,
		},
		{
			// How much of one checkout's git config GET /v1/places reads to
			// name the repository a place clones (its `repo`). A longer config
			// is not parsed and the place names no repository, which the
			// console says as "this project has no origin" when a schedule is
			// moved to another machine.
			Name: "places.git_config_bytes", Class: Buffer, Unit: Bytes,
			Limit: 64 << 10, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/projects.MaxGitConfigBytes"},
		},
		{
			// GET /v1/places remembers at most forty optional repository
			// lookups. A blocked filesystem read keeps its slot so a new request
			// cannot multiply it; completed lookups outside the current list
			// are discarded first.
			Name: "places.repo_lookups", Class: Cache, Unit: Rows,
			Limit: 40, AtLimit: EvictOldest, Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/projects.MaxRepoLookups"},
		},
		{
			// No more than four Git config reads run together for Project
			// origins, even if one waits on a macOS file permission prompt.
			Name: "places.repo_readers", Class: Buffer, Unit: Rows,
			Limit: 4, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/projects.MaxRepoReaders", "internal/adapters/projects.NewRepoLookup:chan(MaxRepoReaders)"},
		},
		{
			// The first list waits one second for optional repository
			// metadata; after that it answers the places it knows. The next
			// request reads completed results without waiting on an old stall.
			Name: "places.repo_wait_seconds", Class: Buffer, Unit: Seconds,
			Limit: 1, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/projects.MaxRepoWaitSeconds"},
		},
		{
			// Optional Project readiness rows may open repository files. A
			// blocked read keeps its slot; completed rows outside the current
			// place list are evicted first.
			Name: "places.setup_lookups", Class: Cache, Unit: Rows,
			Limit: 40, AtLimit: EvictOldest, Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.MaxProjectSetupLookups"},
		},
		{
			Name: "places.setup_readers", Class: Buffer, Unit: Rows,
			Limit: 4, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.MaxProjectSetupReaders", "internal/transport/http.newSetupLookup:chan(MaxProjectSetupReaders)"},
		},
		{
			// One request waits at most this long for new optional setup
			// reads, then returns the available rows.
			Name: "places.setup_wait_seconds", Class: Buffer, Unit: Seconds,
			Limit: 1, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.MaxProjectSetupWaitSeconds"},
		},
		{
			// Completed setup rows refresh after this age, so local
			// Project configuration changes eventually appear on the list.
			Name: "places.setup_age_seconds", Class: Cache, Unit: Seconds,
			Limit: 60, AtLimit: EvictOldest, Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.MaxProjectSetupAgeSeconds"},
		},
	}
}
