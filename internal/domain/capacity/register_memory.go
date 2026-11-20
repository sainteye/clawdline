package capacity

// memoryBounds is every registered row named memory.*.
func memoryBounds() []Entry {
	return []Entry{
		{
			// The active set and the durable overflow share the live listing.
			// Refuse only once both have reached their safety bounds.
			Name: MemoryEntries, Class: Evidence, Unit: Rows,
			Limit: 1152, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/memory.LiveEntryLimit", "internal/adapters/memory.EntryCountLimit", "internal/adapters/memory.PendingEntryLimit"},
		},
		{
			// At the archive bound, candidates stay in the live set. Explicit
			// restore and forget can make room without losing any other entry.
			Name: MemoryArchiveEntries, Class: Evidence, Unit: Rows,
			Limit: 512, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/memory.ArchiveEntryLimit"},
		},
		{
			// One entry's body. An add or update past it is refused before
			// anything is written; the request body and the file read are
			// the same bound with room for the other fields.
			Name: MemoryEntryBytes, Class: Buffer, Unit: Bytes,
			Limit: 64 << 10, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources: []string{"internal/adapters/memory.EntryBodyLimit", "internal/adapters/memory.entryFileReadLimit",
				"internal/transport/http.memoryRequestBodyLimit"},
		},
		{
			// One entry's one-line description: what the index a launched
			// session is given repeats for every entry.
			Name: MemoryDescriptionBytes, Class: Buffer, Unit: Bytes,
			Limit: 512, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/memory.DescriptionByteLimit"},
		},
		{
			// One entry's name, which is also its file name.
			Name: MemoryNameBytes, Class: Buffer, Unit: Bytes,
			Limit: 64, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources:   []string{"internal/adapters/memory.nameByteLimit"},
		},
		{
			// <state dir>/memory/<repo key>/GROUPS.json: the groups an
			// entry may be filed under. The index a launch carries names
			// every group, so the bound is what fits in it with room for
			// resident entries; one more is refused with 409 memory_full.
			Name: MemoryGroups, Class: Evidence, Unit: Rows,
			Limit: 16, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender, Health},
			EvictedBy: Person,
			Sources:   []string{"internal/adapters/memory.GroupCountLimit"},
		},
		{
			// One group's "read this when" line, which the index repeats.
			// The groups file read and a group request body are bounded
			// with room for every group at this length.
			Name: MemoryGroupDescriptionBytes, Class: Buffer, Unit: Bytes,
			Limit: 256, AtLimit: Refuse,
			Told:      []Channel{Diagnostics, Sender},
			EvictedBy: Daemon,
			Sources: []string{"internal/adapters/memory.GroupDescriptionByteLimit", "internal/adapters/memory.groupsFileReadLimit",
				"internal/transport/http.memoryGroupRequestBodyLimit"},
		},
	}
}
