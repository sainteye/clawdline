package http

import (
	"context"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/adapters/terminal/owned"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// terminalCapacityMeasures measures every row of the register's "terminal" area
// (capacity.AreaOf), one reading per row.
func (s *Server) terminalCapacityMeasures() map[string]func() capacity.Reading {
	return map[string]func() capacity.Reading{
		// The shells this daemon's own tmux server holds (limits N59).
		capacity.TerminalCount: func() capacity.Reading {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return owned.CountReading(ctx, s.cfg.Dir)
		},
		capacity.TerminalInputBytes:   func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-call guard"} },
		capacity.TerminalPasteBytes:   func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-call guard"} },
		capacity.TerminalHistoryLines: func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-read bound"} },
		// The terminal routes' own rows (terminals.go, limits N60).
		capacity.TerminalLane: func() capacity.Reading {
			svc := s.terminalServiceIfOpen()
			if svc == nil {
				return capacity.Reading{Known: true, Note: "no terminal route has been asked since the daemon started"}
			}
			st := svc.Lanes().Stats()
			return capacity.Reading{Known: true, Used: int64(st.Admitted)}
		},
		capacity.TerminalViewers: func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-terminal guard"} },
		capacity.TerminalStreams: func() capacity.Reading {
			_, streams := s.terminalStats()
			return capacity.Reading{Known: true, Used: int64(streams)}
		},
		capacity.TerminalLeaseSeconds:    func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-lease clock"} },
		capacity.TerminalBodyBytes:       func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-request guard"} },
		capacity.TerminalLaunchLineBytes: func() capacity.Reading { return capacity.Reading{Known: true, Note: "per-launch guard"} },
		capacity.TerminalLaunchScripts:   terminal.LaunchScriptsReading,
		"terminal.warm_shells": func() capacity.Reading {
			return capacity.Reading{Known: true, Used: int64(s.warmShells().Count())}
		},
		"terminal.warm_shell_seconds": func() capacity.Reading {
			return capacity.Reading{Known: true, Note: "per-shell age; an older shell is closed and started again"}
		},
	}
}
