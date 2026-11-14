package capacity

const AntigravityConsentRequestBytesLimit = 4096

// projectFilesBounds is every registered row named projectfiles.* and
// projects.*.
func projectFilesBounds() []Entry {
	return []Entry{
		{Name: "projects.codegraph_service_roots", Class: Buffer, Unit: Rows, Limit: 4, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/codegraph.ServiceRootsLimit"}},
		{Name: "projects.codegraph_service_idle_seconds", Class: Buffer, Unit: Seconds, Limit: 600, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/codegraph.ServiceIdleSecondsLimit"}},
		{Name: "projects.codegraph_reconcile_seconds", Class: Buffer, Unit: Seconds, Limit: 60, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/codegraph.ReconcileSecondsLimit"}},
		{Name: "projects.codegraph_query_bytes", Class: Buffer, Unit: Bytes, Limit: 4096, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/codegraph.QueryBytesLimit"}},
		{Name: "projects.codegraph_query_body_bytes", Class: Buffer, Unit: Bytes, Limit: 16384, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/codegraph.QueryBodyBytesLimit"}},
		{Name: "projects.codegraph_reply_bytes", Class: Buffer, Unit: Bytes, Limit: 262144, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/codegraph.ReplyBytesLimit"}},
		{Name: "projects.codegraph_query_seconds", Class: Buffer, Unit: Seconds, Limit: 15, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/codegraph.QuerySecondsLimit"}},
		{Name: "projects.codegraph_store_bytes", Class: Buffer, Unit: Bytes, Limit: 536870912, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/codegraph.StoreBytesLimit"}},
		{Name: "projects.codegraph_view_poll_ms", Class: Buffer, Unit: Milliseconds, Limit: 5000, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/codegraph.ViewPollMillisecondsLimit"}},
		{Name: "projects.codegraph_build_jobs", Class: Buffer, Unit: Rows, Limit: 16, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/codegraph.BuildJobsLimit"}},
		{Name: "projects.codegraph_build_seconds", Class: Buffer, Unit: Seconds, Limit: 600, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/codegraph.BuildSecondsLimit"}},
		{Name: "projects.codegraph_build_poll_ms", Class: Buffer, Unit: Milliseconds, Limit: 1000, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/codegraph.BuildPollMillisecondsLimit"}},
		{Name: "projects.codegraph_build_log_bytes", Class: Buffer, Unit: Bytes, Limit: 65536, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/codegraph.BuildLogBytesLimit"}},
		{Name: "projects.codegraph_runtime_bytes", Class: Buffer, Unit: Bytes, Limit: 536870912, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/codegraph.RuntimeBytesLimit"}},
		{Name: "projects.codegraph_build_raw_bytes", Class: Buffer, Unit: Bytes, Limit: 134217728, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/codegraph.BuildRawBytesLimit"}},
		{Name: "projects.codegraph_build_workers", Class: Buffer, Unit: Rows, Limit: 1, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/codegraph.BuildWorkersLimit"}},
		{Name: "projects.codegraph_build_body_bytes", Class: Buffer, Unit: Bytes, Limit: 4096, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/codegraph.BuildBodyBytesLimit"}},

		// The Ops documents list (docs/ops-documents.md). Records are a
		// person's input: at a limit a write is refused and nothing is evicted.
		{Name: "projects.ops_entries", Class: Buffer, Unit: Rows, Limit: 500, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/opsdocs.EntriesPerProjectLimit"}},
		{Name: "projects.ops_collections", Class: Buffer, Unit: Rows, Limit: 64, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/opsdocs.CollectionsPerProjectLimit"}},
		{Name: "projects.ops_title_bytes", Class: Buffer, Unit: Bytes, Limit: 200, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/opsdocs.TitleBytesLimit"}},
		{Name: "projects.ops_collection_name_bytes", Class: Buffer, Unit: Bytes, Limit: 100, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/opsdocs.CollectionNameBytesLimit"}},
		{Name: "projects.ops_markdown_name_bytes", Class: Buffer, Unit: Bytes, Limit: 255, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/opsdocs.MarkdownNameBytesLimit"}},
		{Name: "projects.ops_url_bytes", Class: Buffer, Unit: Bytes, Limit: 2048, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/opsdocs.URLBytesLimit"}},
		{Name: "projects.ops_note_bytes", Class: Buffer, Unit: Bytes, Limit: 1000, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/opsdocs.NoteBytesLimit"}},
		{Name: "projects.ops_read_ids", Class: Buffer, Unit: Rows, Limit: 16, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/opsdocs.ReadIDsLimit"}},
		{Name: "projects.ops_github_probe_ms", Class: Buffer, Unit: Milliseconds, Limit: 3000, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/opsdocs.GitHubProbeMillisecondsLimit"}},
		{Name: "projects.ops_github_cache", Class: Cache, Unit: Rows, Limit: 64, AtLimit: EvictOldest, Told: []Channel{Diagnostics}, EvictedBy: Daemon, Sources: []string{"internal/adapters/opsdocs.GitHubCacheEntriesLimit"}},

		{Name: "projects.codegraph_zoom", Class: Buffer, Unit: Rows, Limit: 4, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon},
		{Name: "projects.codegraph_nodes", Class: Buffer, Unit: Rows, Limit: 300, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/codegraph.ViewNodeLimit"}},
		{Name: "projects.codegraph_links", Class: Buffer, Unit: Rows, Limit: 900, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/codegraph.ViewLinkLimit"}},
		{Name: "projects.codegraph_text", Class: Buffer, Unit: Characters, Limit: 120, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/codegraph.ViewTextLimit"}},
		{Name: "projects.codegraph_request_ms", Class: Buffer, Unit: Milliseconds, Limit: 15000, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon},
		// ANTIGRAVITY_CONSENT_REQUEST_MS in the Project consent website API
		// bounds the entire response, including its JSON body, without retry.
		{Name: "projects.antigravity_consent_request_ms", Class: Buffer, Unit: Milliseconds, Limit: 15000, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon},
		{Name: "projects.antigravity_consent_body_bytes", Class: Buffer, Unit: Bytes, Limit: AntigravityConsentRequestBytesLimit, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/transport/http.antigravityConsentBodyBytesLimit", "internal/domain/capacity.AntigravityConsentRequestBytesLimit"}},
		{Name: "projects.workspace_metadata_bytes", Class: Buffer, Unit: Bytes,
			Limit: 4096, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/projects.WorkspaceMetadataBytesLimit"}},
		{
			Name: "projects.folder_rows", Class: Buffer, Unit: Rows,
			Limit: 128, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.maxProjectFolderRows"},
		},
		{
			Name: "projects.folder_search_rows", Class: Buffer, Unit: Rows,
			Limit: 50, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.maxProjectFolderSearchRows"},
		},
		{
			Name: "projects.folder_search_reads", Class: Buffer, Unit: Rows,
			Limit: 200, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.maxProjectFolderSearchReads"},
		},
		{
			Name: "projects.folder_query_runes", Class: Buffer, Unit: Characters,
			Limit: 64, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/transport/http.maxProjectFolderQueryRunes"},
		},
		{
			Name: "projectfiles.list", Class: Buffer, Unit: Rows,
			Limit: 128, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/projectfiles.MaxFiles"},
		},
		{
			Name: "projectfiles.scan_entries", Class: Buffer, Unit: Rows,
			Limit: 1024, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/projectfiles.MaxScanEntries"},
		},
		{
			Name: "projectfiles.tree_path_bytes", Class: Buffer, Unit: Bytes,
			Limit: 4096, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/projectfiles.MaxTreePathBytes"},
		},
		{
			Name: "projectfiles.tree_depth", Class: Buffer, Unit: Rows,
			Limit: 64, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/projectfiles.MaxTreeDepth"},
		},
		{
			Name: "projectfiles.file_bytes", Class: Buffer, Unit: Bytes,
			Limit: 128 << 10, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/projectfiles.MaxFileBytes"},
		},
		{
			Name: "projectfiles.skill_summary_runes", Class: Observation, Unit: Characters,
			Limit: 240, AtLimit: EvictOldest, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/projectfiles.MaxSkillSummaryRunes"},
		},
		{
			Name: "projectfiles.write_bytes", Class: Buffer, Unit: Bytes,
			Limit: 256 << 10, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon,
			Sources: []string{"internal/adapters/projectfiles.MaxWriteBytes", "internal/transport/http.opsBodyLimit"},
		},
	}
}
