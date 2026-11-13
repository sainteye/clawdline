package http

import (
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// browserCapacityMeasures measures every row of the register's "browser" area.
func (s *Server) browserCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		capacity.BrowserSessionChoices: func() capacity.Reading {
			h := s.browsers()
			if h.StateError() {
				return capacity.Unmeasured("browser selection state is not readable")
			}
			return capacity.Reading{Known: true, Used: int64(h.SelectionCount())}
		},
		capacity.BrowserProfileStateBytes: func() capacity.Reading {
			size, err := s.browsers().StateBytes()
			if err != nil {
				return capacity.Unmeasured("browser selection state size is not readable")
			}
			return capacity.Reading{Known: true, Used: size}
		},
		capacity.BrowserCloseReceipts: func() capacity.Reading {
			return capacity.Unmeasured("close receipts are owned by the native host; occupancy is not measured by the daemon")
		},
		capacity.BrowserCloseReceiptBytes: func() capacity.Reading {
			return capacity.Unmeasured("close receipt file usage is not measured by the daemon")
		},
		"browser.keyboard_pending_edits": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-viewer native input queue; live browser occupancy is not measured by the daemon"}
		},
		"browser.keyboard_pending_bytes": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-viewer native input byte guard; live browser occupancy is not measured by the daemon"}
		},
		capacity.BrowserPendingCommands: func() capacity.Reading {
			pending, _, _ := s.browsers().CapacityMaxima()
			return capacity.Reading{Known: true, Used: int64(pending), Note: "largest per-profile queue"}
		},
		capacity.BrowserAuditRows: func() capacity.Reading {
			_, rows, _ := s.browsers().CapacityMaxima()
			return capacity.Reading{Known: true, Used: int64(rows), Note: "largest per-profile audit"}
		},
		capacity.BrowserAuditText: func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-row cut"} },
		capacity.BrowserStdinText: func() capacity.Reading { return capacity.Reading{Known: true, Note: "checked by the CLI"} },
		capacity.BrowserActivityRows: func() capacity.Reading {
			_, _, rows := s.browsers().CapacityMaxima()
			return capacity.Reading{Known: true, Used: int64(rows), Note: "largest per-profile picture history"}
		},
		capacity.BrowserActivityBytes: func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-picture guard"} },
		capacity.BrowserArgBytes:      func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-command guard"} },
		capacity.BrowserResultBytes:   func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-result guard"} },
		capacity.BrowserHostMessage:   func() capacity.Reading { return capacity.Reading{Known: true, Note: "checked by the host"} },
	}
}
