package capacity

// projectFilesBounds is every registered row named projectfiles.* and
// projects.*.
func projectFilesBounds() []Entry {
	return []Entry{
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
			Sources: []string{"internal/adapters/projectfiles.MaxWriteBytes"},
		},
	}
}
