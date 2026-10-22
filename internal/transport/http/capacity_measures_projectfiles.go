package http

import (
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// projectFilesCapacityMeasures measures every row of the register's "projectfiles" area
// (capacity.AreaOf), one reading per row.
func (s *Server) projectFilesCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		"projects.workspace_metadata_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-file Git membership metadata read guard; no retained buffer"}
		},
		"projects.folder_rows": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-directory folder listing guard"}
		},
		"projectfiles.list":            func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-inventory file count guard"} },
		"projectfiles.scan_entries":    func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-skill-directory scan guard"} },
		"projectfiles.tree_path_bytes": func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-Project-tree request guard"} },
		"projectfiles.tree_depth":      func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-Project-tree request guard"} },
		"projectfiles.file_bytes":      func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-file read and save guard"} },
		"projectfiles.skill_summary_runes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Skill inventory display limit"}
		},
		"projectfiles.write_bytes": func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-request save body guard"} },
	}
}
