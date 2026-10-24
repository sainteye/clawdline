package capacity

// cacheBounds is every registered row named cache.*.
func cacheBounds() []Entry {
	return []Entry{
		{Name: "cache.provider_quota", Class: Cache, Unit: Rows, Limit: 4, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/providerquota.CacheLimit"}},
		{Name: "cache.provider_quota_response_bytes", Class: Buffer, Unit: Bytes, Limit: 1048576, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/providerquota.ResponseBytesLimit"}},
		{Name: "cache.provider_quota_refresh_seconds", Class: Observation, Unit: Seconds, Limit: 900, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/providerquota.RefreshSecondsLimit"}},
		{Name: "cache.provider_quota_request_seconds", Class: Observation, Unit: Seconds, Limit: 10, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/providerquota.RequestSecondsLimit"}},

		{
			// The one execution scan observeExecutions last committed, by
			// terminal. A scan equal to it is answered from it and opens no
			// write transaction; a scan naming more terminals than this is not
			// remembered and goes to the store, as every scan did before.
			Name: CacheSessionExecutionMemo, Class: Cache, Unit: Rows,
			Limit: 4096, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.ExecutionMemoLimit"},
		},
		{
			// Each working directory's Project key as the claim decides it
			// (projects.CanonicalProjectKey), for the session list's
			// `project_key`. An answer stands for thirty seconds
			// (projectKeyFresh); past the limit the one read longest ago is
			// let go, and a miss walks up to the repository marker again.
			Name: CacheSessionProjectKeys, Class: Cache, Unit: Rows,
			Limit: 256, AtLimit: EvictOldest,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.ProjectKeyCacheLimit"},
		},
		{
			// Each conversation's title as last read, keyed on the file's size
			// and time (limits N18). A miss reads the transcript's tail again.
			Name: CacheTranscriptTitles, Class: Cache, Unit: Rows,
			Limit: 256, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
		},
		{
			// Claude sidecars, changing transcript tails and append cursors.
			// Each cache is bounded at this row; Used is the fullest of them.
			Name: CacheBackgroundAgents, Class: Cache, Unit: Rows,
			Limit: 256, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/subagents.MaximumCache"},
		},
		{
			// The thumbnails a Board card or a to-do row asks for
			// (`GET /v1/work/v2/images/{id}?size=thumb`), drawn from the
			// stored image on a miss and held by id and digest. A thumbnail
			// of a 1600 × 873 screenshot is tens of kilobytes; eight
			// mebibytes is a couple of hundred of them. Past the limit the
			// thumbnail used longest ago is let go and drawn again when it
			// is next asked for; nothing durable is lost.
			Name: CacheImageThumbs, Class: Cache, Unit: Bytes,
			Limit: 8 << 20, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
		},
		{
			// The skills each session's slash menu offers, one reading per
			// working directory (or Codex rollout) and assistant, served for
			// five minutes as the Swift app's SessionLinksCache.skills serves
			// them. Past the limit the reading used longest ago is let go; a
			// miss walks the skills directories again.
			Name: CacheSessionSkills, Class: Cache, Unit: Rows,
			Limit: 64, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
		},
		{
			// What the providers last said about each account's plan windows
			// (limits N18), one reading per assistant, held for five seconds.
			// The row is here for what it rules out as much as for what it
			// holds: the reading is taken when somebody asks, out of files
			// the providers write anyway, so this cache grows with the number
			// of assistants and with nothing else — not with sessions, not
			// with projects, and never on a beat that would spend quota to
			// ask how much quota is left. Past the limit the reading handed
			// out longest ago is let go, and a miss reads the files again.
			// Two readings is all it has ever held; the limit is headroom,
			// and what the row is for is that nothing makes it grow.
			Name: CacheAssistantQuota, Class: Cache, Unit: Rows,
			Limit: 32, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/limits.quotaCacheLimit"},
		},
		{
			// The last complete per-terminal-source inventory kept across a
			// failed scan (internal/app/inventory_reading.go). At this age it
			// expires: two minutes is also the iTerm2 listing and held-screen
			// backoff ceiling, while anything older would be a claim about the
			// present rather than a named prior observation.
			Name: CacheSessionInventory, Class: Cache, Unit: Seconds,
			Limit: 120, AtLimit: Expire,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.LastGoodInventoryAgeLimit"},
		},
		{
			// What each source said in the scan behind the held reading, and
			// in the one running now (internal/app/inventory_reading.go). A
			// drawing taken while iTerm2 holds the refresh keeps the rows of a
			// source whose answer is complete, lists exactly those rows and is
			// younger than this. At this age the answer expires and that
			// source's rows read unverified, as every source's did before.
			// Thirty seconds is the scan budget, the longest one refresh runs.
			Name: CacheSourceAnswer, Class: Cache, Unit: Seconds,
			Limit: 30, AtLimit: Expire,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.SourceAnswerAgeLimit"},
		},
		{
			// A pinned Cloud read (transcript, info, git, to-dos) checks that
			// the execution it names is still the one running. A scan that
			// finished less than this ago answers that check; an older one
			// is replaced by a scan taken for the read, as every check was
			// before. Three seconds: a scan started now returns rows observed
			// as it started, so this is at most three seconds older than the
			// fresh answer, against the 8.9 s and 13.0 s medians measured for
			// the slow pinned reads that waited for one. Writes never use it.
			Name: CachePinnedRead, Class: Cache, Unit: Seconds,
			Limit: 3, AtLimit: Expire,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.PinnedReadAgeLimit"},
		},
		{
			// A directory git answered "not a repository" for is answered
			// that way for a minute without running git again
			// (internal/adapters/git/notrepo.go). Only that answer is kept.
			Name: CacheGitNotRepo, Class: Cache, Unit: Seconds,
			Limit: 60, AtLimit: Expire,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/git.NotRepoAgeLimit"},
		},
		{
			// How many such directories are remembered. Past it the one
			// remembered longest ago is let go; a miss is one `git status`.
			Name: CacheGitNotRepoRows, Class: Cache, Unit: Rows,
			Limit: 256, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/git.NotRepoRowsLimit"},
		},
		{
			// The machine dashboard polls every three seconds. Grouped reclaim
			// decisions come from the store at most once per thirty seconds.
			Name: CacheReclaimSummary, Class: Cache, Unit: Seconds,
			Limit: 30, AtLimit: Expire,
			Told:      []Channel{Diagnostics},
			EvictedBy: Daemon,
			Sources:   []string{"internal/transport/http.reclaimSummaryLimit"},
		},
		{
			// The screens held between those captures, one per session the
			// list has asked about. Past the limit the screen asked about
			// longest ago is let go, and a screen nobody has asked about for
			// two minutes goes with it; a miss is one capture behind the
			// answer, like every other refresh here.
			Name: CacheTerminalScreens, Class: Cache, Unit: Rows,
			Limit: 64, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
			Sources:   []string{"internal/app.ScreenHeldLimit"},
		},
		{
			// What the last reading found about each session's own record:
			// where it is, and what its newest turn said the time was when the
			// file was that size. It is what keeps a list redrawn every two
			// seconds to one stat per row — the record itself is opened only
			// when it has changed. Past the limit the conversation read
			// longest ago is let go, and its next reading opens the file
			// again.
			Name: CacheSessionActivity, Class: Cache, Unit: Rows,
			Limit: 64, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
		},

		{
			// Where each project can be opened: one reading per working
			// directory, held as the Swift app's SessionLinksCache holds it.
			// A reading costs a `git remote` and a handful of file reads, and
			// the Swift app measured five consecutive walks at 319-390 ms
			// with nothing holding them. Past the limit the directory read
			// longest ago is let go; a miss walks it again, which is why this
			// is a cache and not evidence.
			Name: CacheSessionLinks, Class: Cache, Unit: Rows,
			Limit: 64, AtLimit: EvictOldest,
			Told:      []Channel{Diagnostics, Notice},
			EvictedBy: Daemon,
		},
	}
}
