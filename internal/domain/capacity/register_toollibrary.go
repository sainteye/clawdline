package capacity

func toolLibraryBounds() []Entry {
	return []Entry{
		{Name: ToolLibraryProjects, Class: Evidence, Unit: Rows, Limit: 100, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender, Health}, EvictedBy: Person, Sources: []string{"internal/domain/toollibrary.MaxSavedProjects"}},
		{Name: ToolLibraryProjectBytes, Class: Buffer, Unit: Bytes, Limit: 8 << 10, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/toollibrary.MaxProjectBytes"}},
		{Name: ToolLibraryRequestBytes, Class: Buffer, Unit: Bytes, Limit: 128 << 10, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/toollibrary.MaxRequestBytes"}},
		{Name: ToolLibraryRequestKeyBytes, Class: Buffer, Unit: Bytes, Limit: 200, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/domain/toollibrary.MaxRequestKeyBytes"}},
	}
}
