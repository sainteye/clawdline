package http

import (
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// projectFilesCapacityMeasures measures every row of the register's "projectfiles" area
// (capacity.AreaOf), one reading per row.
func (s *Server) projectFilesCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		"projects.graphify_build_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "local index build build seconds guard; no queued builds"}
		},
		"projects.graphify_build_poll_ms": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "local index build build poll ms guard; no queued builds"}
		},
		"projects.graphify_build_log_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "local index build build log bytes guard; no queued builds"}
		},
		"projects.graphify_runtime_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "local index build runtime bytes guard; no queued builds"}
		},
		"projects.graphify_build_raw_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "local index build build raw bytes guard; no queued builds"}
		},
		"projects.graphify_build_body_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "local index build build body bytes guard; no queued builds"}
		},
		"projects.graphify_build_jobs": func() capacity.Reading {
			_, held := s.graphifyBuilds().Counts()
			return capacity.Reading{Known: true, Used: held, Note: "held build statuses; terminal rows expire after the operation window on the next start"}
		},
		"projects.graphify_build_workers": func() capacity.Reading {
			active, _ := s.graphifyBuilds().Counts()
			return capacity.Reading{Known: true, Used: active, Note: "one active machine-wide build; other roots are refused without a queue"}
		},

		"projects.ops_entries": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Project ceiling checked inside each create; a full list refuses new entries"}
		},
		"projects.ops_collections": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-Project ceiling checked inside each create; a full list refuses new collections"}
		},
		"projects.ops_title_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "request field guard; nothing retained"}
		},
		"projects.ops_collection_name_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "request field guard; nothing retained"}
		},
		"projects.ops_markdown_name_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "request field guard; nothing retained"}
		},
		"projects.ops_url_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "request field guard; nothing retained"}
		},
		"projects.ops_note_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "request field guard; nothing retained"}
		},
		"projects.ops_read_ids": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "CLI argument guard; nothing retained"}
		},
		"projects.ops_github_probe_ms": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "one bounded GitHub request per probe; nothing queued"}
		},
		"projects.ops_github_cache": func() capacity.Reading {
			return capacity.Reading{Known: true, Used: int64(s.opsProber().Cached()), Note: "repository visibility answers kept for ten minutes; the oldest is forgotten first"}
		},

		"projects.graphify_zoom": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "viewer zoom cap; no retained graph cache"}
		},
		"projects.graphify_nodes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "on-demand Graphify viewer guard; no retained graph cache"}
		},
		"projects.graphify_links": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "on-demand Graphify viewer guard; no retained graph cache"}
		},
		"projects.graphify_text": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "on-demand Graphify viewer guard; no retained graph cache"}
		},
		"projects.graphify_request_ms": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "on-demand Graphify viewer guard; no retained graph cache"}
		},
		"projects.antigravity_consent_request_ms": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "website whole-response deadline; no automatic retry or retained request"}
		},
		"projects.antigravity_consent_body_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-request Antigravity consent change guard; no retained body"}
		},
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
