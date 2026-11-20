package http

import (
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// memoryCapacityMeasures measures every row of the register's "memory" area
// (capacity.AreaOf), one reading per row.
func (s *Server) memoryCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		// A Project's shared memory (docs/project-memory.md): the fullest
		// Project's entries and groups, and four per-write guards.
		capacity.MemoryEntries: func() capacity.Reading {
			n, err := s.memoryStore().MostEntries(false)
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: int64(n), Note: "active and pending entries in the fullest Project"}
		},
		capacity.MemoryArchiveEntries: func() capacity.Reading {
			n, err := s.memoryStore().MostEntries(true)
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: int64(n), Note: "archived entries in the fullest Project"}
		},
		capacity.MemoryEntryBytes:       func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-write guard"} },
		capacity.MemoryDescriptionBytes: func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-write guard"} },
		capacity.MemoryNameBytes:        func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-write guard"} },
		capacity.MemoryGroups: func() capacity.Reading {
			n, err := s.memoryStore().MostGroups()
			if err != nil {
				return capacity.Unmeasured(err.Error())
			}
			return capacity.Reading{Known: true, Used: int64(n), Note: "groups in the Project with the most"}
		},
		capacity.MemoryGroupDescriptionBytes: func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-write guard"} },
	}
}
