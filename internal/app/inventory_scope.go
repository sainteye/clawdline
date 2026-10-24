package app

import (
	"context"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
	"time"
)

type scopedTerminal struct {
	ports.TerminalHost
	answer session.Inventory
}

func (s scopedTerminal) Inventory(context.Context) (session.Inventory, error) { return s.answer, nil }

type scopedProcess struct {
	ports.ProcessHost
	ttys map[string]bool
}

func (s scopedProcess) Scan(ctx context.Context) (session.Inventory, error) {
	if p, ok := s.ProcessHost.(interface {
		ScanTTYs(context.Context, map[string]bool) (session.Inventory, error)
	}); ok {
		return p.ScanTTYs(ctx, s.ttys)
	}
	return session.Inventory{Provenance: "ps", Disabled: "setting", ObservedAt: time.Now()}, nil
}

// Keep presence for hidden tmux panes so turning discovery off never proves a live task gone.
// Only selected TTYs reach provider-record binding, identity enrichment and screen capture.
func (in Inventory) managedOnly(ctx context.Context) Inventory {
	ttys := map[string]bool{}
	hosts := make([]ports.TerminalHost, 0, len(in.Terminals))
	for _, host := range in.Terminals {
		answer := session.Inventory{Provenance: host.Name(), Disabled: "setting", ObservedAt: time.Now()}
		if h, ok := host.(interface {
			ManagedInventory(context.Context) (session.Inventory, error)
		}); ok {
			answer, _ = h.ManagedInventory(ctx)
			for _, row := range answer.Sessions {
				if !row.DiscoveryExcluded {
					ttys[row.TTY] = true
				}
			}
		}
		hosts = append(hosts, scopedTerminal{TerminalHost: host, answer: answer})
	}
	in.Terminals = hosts
	if in.Process != nil {
		in.Process = scopedProcess{ProcessHost: in.Process, ttys: ttys}
	}
	in.DiscoverExternal = nil
	return in
}
