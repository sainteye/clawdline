package projectassistanthost

import (
	"context"

	serverstore "github.com/sainteye/clawdline/internal/adapters/botdevserverstore"
	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// ServerUsage counts the actual Host registry. Disabled reopened history has
// only durable receipts: active receipts retain occupancy but cannot prove a
// physical process is either alive or gone. Metadata inspection spawns nothing.
func (h *Host) ServerUsage(ctx context.Context) (servers.Usage, error) {
	h.mu.Lock()
	r := h.serverRegistry
	h.mu.Unlock()
	if r != nil {
		return r.Usage(), nil
	}
	c, err := serverstore.NewCatalog(ctx, h.o.Database)
	if err != nil {
		return servers.Usage{}, err
	}
	rows, err := c.List(ctx, capacity.MaximumBotServerDefinitions)
	if err != nil {
		return servers.Usage{}, err
	}
	u := servers.Usage{Definitions: len(rows), Known: true}
	for _, saved := range rows {
		switch saved.Attempt.State {
		case servers.Accepted, servers.Running, servers.Stopping, servers.Unknown:
			u.Processes++
			u.Known = false
		}
	}
	return u, nil
}
