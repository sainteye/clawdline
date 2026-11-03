package capacity

const AntigravityConsentRequestBytesLimit = 4096

// projectFilesBounds is every registered row named projectfiles.* and
// projects.*.
func projectFilesBounds() []Entry {
	return []Entry{
		{Name: "projects.graphify_build_jobs", Class: Buffer, Unit: Rows, Limit: 16, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/graphify.BuildJobsLimit"}},
		{Name: "projects.graphify_build_seconds", Class: Buffer, Unit: Seconds, Limit: 600, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/graphify.BuildSecondsLimit"}},
		{Name: "projects.graphify_build_poll_ms", Class: Buffer, Unit: Milliseconds, Limit: 1000, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/graphify.BuildPollMillisecondsLimit"}},
		{Name: "projects.graphify_build_log_bytes", Class: Buffer, Unit: Bytes, Limit: 65536, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/graphify.BuildLogBytesLimit"}},
		{Name: "projects.graphify_runtime_bytes", Class: Buffer, Unit: Bytes, Limit: 536870912, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/graphify.RuntimeBytesLimit"}},
		{Name: "projects.graphify_build_raw_bytes", Class: Buffer, Unit: Bytes, Limit: 134217728, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/graphify.BuildRawBytesLimit"}},
		{Name: "projects.graphify_build_workers", Class: Buffer, Unit: Rows, Limit: 1, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/graphify.BuildWorkersLimit"}},
		{Name: "projects.graphify_build_body_bytes", Class: Buffer, Unit: Bytes, Limit: 4096, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/graphify.BuildBodyBytesLimit"}},

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

		{Name: "projects.graphify_zoom", Class: Buffer, Unit: Rows, Limit: 4, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon},
		{Name: "projects.graphify_nodes", Class: Buffer, Unit: Rows, Limit: 300, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/graphify.ViewNodeLimit"}},
		{Name: "projects.graphify_links", Class: Buffer, Unit: Rows, Limit: 900, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/graphify.ViewLinkLimit"}},
		{Name: "projects.graphify_text", Class: Buffer, Unit: Characters, Limit: 120, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/graphify.ViewTextLimit"}},
		{Name: "projects.graphify_request_ms", Class: Buffer, Unit: Milliseconds, Limit: 15000, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon},
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
