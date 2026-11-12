package http

import (
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// browserCapacityMeasures measures every row of the register's "browser" area.
func (s *Server) browserCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		capacity.BrowserPendingCommands: func() capacity.Reading {
			return capacity.Reading{Known: true, Used: int64(s.browser().Status().Pending)}
		},
		capacity.BrowserAuditRows: func() capacity.Reading {
			rows, _ := s.browser().Records("", int(capacity.Default(capacity.BrowserAuditRows)))
			return capacity.Reading{Known: true, Used: int64(len(rows))}
		},
		capacity.BrowserAuditText:   func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-row cut"} },
		capacity.BrowserStdinText:   func() capacity.Reading { return capacity.Reading{Known: true, Note: "checked by the CLI"} },
		capacity.BrowserArgBytes:    func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-command guard"} },
		capacity.BrowserResultBytes: func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-result guard"} },
		capacity.BrowserHostMessage: func() capacity.Reading { return capacity.Reading{Known: true, Note: "checked by the host"} },
	}
}
