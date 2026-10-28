package http

import (
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// sessionsCapacityMeasures measures every row of the register's "sessions" area
// (capacity.AreaOf), one reading per row.
func (s *Server) sessionsCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		"sessions.claude_remote_request_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "deadline for one native Remote Control request; no retry or token refresh"}
		},
		"sessions.claude_remote_response_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-read byte ceiling for native Remote Control responses and credentials; oversized data is refused"}
		},
		capacity.SessionsShellOutputBytes: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-read window; no retained buffer"}
		},
		capacity.SessionsAgentRows: func() capacity.Reading {
			if s.agents == nil {
				return capacity.Unmeasured("this server reads no provider background work")
			}
			return s.agents.RowsReading()
		},
		// What the last reading of the machine spent asking when each row
		// last moved (internal/app/activity_reads.go). An inventory with no
		// bound of its own is a known zero, not an unmeasured row: it reads
		// no activity times at all.
		capacity.SessionsActivityReads: func() capacity.Reading {
			return s.inventory.Activity.Reading()
		},
		capacity.SessionsRestoreRows: s.restoreRowsReading,
		capacity.SessionsRestoreBoots: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "this boot and the one before it; older boots are deleted at each write"}
		},
		capacity.SessionsRestoreBatch: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-request guard; no retained buffer"}
		},
		capacity.SessionsRestoreSeenAge: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "refresh interval for an unchanged set; no retained buffer"}
		},
		capacity.SessionsRestoreBeat: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "heartbeat interval for the boot's last_seen; no retained buffer"}
		},
		capacity.SessionsRestoreGrace: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "offer window before the previous boot's last sight; no retained buffer"}
		},
		capacity.SessionsArchiveRows: s.archiveRowsReading,
		capacity.SessionsArchiveBatch: func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-request guard; no retained buffer"}
		},
	}
}
