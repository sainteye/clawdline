package capacity

// The admin connection-health page's own bounds
// (web/console/src/cloud/admin/health/cloud.ts HEALTH_UI_LIMITS, stored.ts
// STORED_BOUNDS and STORED_ID_MAX_CHARS, stored-export.ts
// STORED_EVIDENCE_MAX_BYTES, incident-export.ts through HEALTH_UI_LIMITS). They live in each open operator tab; the Cloud's
// own read bounds are the Cloud's, in its connection-health contract §4.
const (
	ConsoleHealthUIRequestSeconds = "console.health_ui_request_seconds"
	ConsoleHealthUIBudgetSeconds  = "console.health_ui_budget_seconds"
	ConsoleHealthUIRefreshSeconds = "console.health_ui_refresh_seconds"
	ConsoleHealthUIParallelReads  = "console.health_ui_parallel_reads"
	ConsoleHealthUIReadsPerLoad   = "console.health_ui_reads_per_load"
	ConsoleHealthUIBodyBytes      = "console.health_ui_body_bytes"
	ConsoleHealthUIPageRows       = "console.health_ui_page_rows"
	ConsoleHealthUIEventPages     = "console.health_ui_event_pages"
	ConsoleHealthUIExportBytes    = "console.health_ui_export_bytes"
	ConsoleHealthUIIDCharacters   = "console.health_ui_id_characters"
	// Incident reads and the audited export (HEALTH_UI_LIMITS incidentRows,
	// incidentExportBytes, incidentWindowMs).
	ConsoleHealthUIIncidentRows          = "console.health_ui_incident_rows"
	ConsoleHealthUIIncidentExportBytes   = "console.health_ui_incident_export_bytes"
	ConsoleHealthUIIncidentWindowSeconds = "console.health_ui_incident_window_seconds"
	// Incident-list pages held by "Load older" (HEALTH_UI_LIMITS incidentPages).
	ConsoleHealthUIIncidentPages = "console.health_ui_incident_pages"
	// The trend view over the Cloud's minute rollups (HEALTH_UI_LIMITS
	// trendLanes, trendSignals).
	ConsoleHealthUITrendLanes        = "console.health_ui_trend_lanes"
	ConsoleHealthUITrendSignals      = "console.health_ui_trend_signals"
	ConsoleHealthUIProbeTargets      = "console.health_ui_probe_targets"
	ConsoleHealthUIProbeHosts        = "console.health_ui_probe_hosts"
	ConsoleHealthUIProbeClaims       = "console.health_ui_probe_claims"
	ConsoleHealthUIInvestigations    = "console.health_ui_investigations"
	ConsoleHealthUIInvestigationRefs = "console.health_ui_investigation_refs"
)

// connectionHealthUIBounds is every console.health_ui_* row.
func connectionHealthUIBounds() []Entry {
	return []Entry{
		{
			// INVESTIGATION_BOUNDS.perIncident rejects oversized remote investigation lists.
			Name: ConsoleHealthUIInvestigations, Class: Buffer, Unit: Rows, Limit: 5, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// INVESTIGATION_BOUNDS.reportRefs rejects oversized result evidence references.
			Name: ConsoleHealthUIInvestigationRefs, Class: Buffer, Unit: Rows, Limit: 8, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// One stored read's deadline (requestMs, 6 s, just above the
			// Cloud's own 5 s request deadline). Past it that read is
			// "timeout" in its own panel; the reads that answered stay shown.
			Name: ConsoleHealthUIRequestSeconds, Class: Cache, Unit: Seconds,
			Limit: 6, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// The whole load, every read included (budgetMs). Reads still
			// running or never started are "budget", never dropped silently.
			Name: ConsoleHealthUIBudgetSeconds, Class: Cache, Unit: Seconds,
			Limit: 12, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// A live window reads again at this pace, one load outstanding
			// (refreshMs); a paused window does not.
			Name: ConsoleHealthUIRefreshSeconds, Class: Cache, Unit: Seconds,
			Limit: 15, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// Reads in flight at once (parallel): the Cloud admits two
			// in-flight health reads per principal. A third waits its turn.
			Name: ConsoleHealthUIParallelReads, Class: Buffer, Unit: Rows,
			Limit: 2, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// Reads one screen's load issues (readsPerLoad): a fixed plan per
			// screen, never one per row.
			Name: ConsoleHealthUIReadsPerLoad, Class: Buffer, Unit: Rows,
			Limit: 3, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// One response body (bodyBytes, 1.5 MiB against the Cloud's 1 MiB
			// page). Past it the read is refused unread as "body_too_large".
			Name: ConsoleHealthUIBodyBytes, Class: Buffer, Unit: Bytes,
			Limit: 1536 * 1024, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// Rows asked for and accepted per page (pageRows, the Cloud's own
			// maximum). A longer list is refused as contract_bound, not cut.
			Name: ConsoleHealthUIPageRows, Class: Buffer, Unit: Rows,
			Limit: 200, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// Pages of one event list held at once (eventPages). At the bound
			// "Load older" is replaced by a note to narrow the window.
			Name: ConsoleHealthUIEventPages, Class: Buffer, Unit: Rows,
			Limit: 5, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// The copied evidence record (STORED_EVIDENCE_MAX_BYTES): a buffer
			// on its way to the clipboard, not retained evidence. Past it
			// nothing is copied and the page says how large it would be.
			Name: ConsoleHealthUIExportBytes, Class: Buffer, Unit: Bytes,
			Limit: 512 * 1024, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// A trace or boot id in an address (STORED_ID_MAX_CHARS, the
			// Cloud's opaque-id rule). A longer one names no screen.
			Name: ConsoleHealthUIIDCharacters, Class: Buffer, Unit: Characters,
			Limit: 64, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// Incidents asked for per list read (incidentRows, the Cloud's
			// default page). More are named as "more opened in this window",
			// never as none.
			Name: ConsoleHealthUIIncidentRows, Class: Buffer, Unit: Rows,
			Limit: 50, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// One incident export held for download or copy
			// (incidentExportBytes, the Cloud's own 1 MiB export bound). Past
			// it reading stops and nothing is kept or offered.
			Name: ConsoleHealthUIIncidentExportBytes, Class: Buffer, Unit: Bytes,
			Limit: 1024 * 1024, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// The incident list's window over opened_at (incidentWindowMs,
			// the Cloud's own 7-day maximum). Older incidents are not listed.
			Name: ConsoleHealthUIIncidentWindowSeconds, Class: Cache, Unit: Seconds,
			Limit: 7 * 24 * 3600, AtLimit: Expire,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// Incident-list pages held at once (incidentPages), the first
			// included: 250 incidents at 50 a page. At the bound "Load older"
			// is replaced by a note to pause at an earlier time.
			Name: ConsoleHealthUIIncidentPages, Class: Buffer, Unit: Rows,
			Limit: 5, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// Trend lanes drawn as charts (trendLanes), each up to the
			// Cloud's 360 buckets. Past it the rest are listed in the table
			// with "n of m drawn", never dropped.
			Name: ConsoleHealthUITrendLanes, Class: Buffer, Unit: Rows,
			Limit: 12, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// Trend buckets listed to investigate (trendSignals), newest
			// first. Past it the older ones are counted, not listed.
			Name: ConsoleHealthUITrendSignals, Class: Buffer, Unit: Rows,
			Limit: 20, AtLimit: Refuse,
			Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// PROBE_BOUNDS decoder limit; an oversized remote list is refused.
			Name: ConsoleHealthUIProbeTargets, Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// PROBE_BOUNDS decoder limit; an oversized remote list is refused.
			Name: ConsoleHealthUIProbeHosts, Class: Buffer, Unit: Rows,
			Limit: 204, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
		{
			// PROBE_BOUNDS decoder limit; an oversized remote list is refused.
			Name: ConsoleHealthUIProbeClaims, Class: Buffer, Unit: Rows,
			Limit: 8, AtLimit: Refuse, Told: []Channel{Diagnostics}, EvictedBy: Daemon,
		},
	}
}
