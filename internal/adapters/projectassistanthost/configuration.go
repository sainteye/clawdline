package projectassistanthost

import (
	"context"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func (h *Host) configuration(ctx context.Context, bot pa.Bot, dir, model string) (pa.ConfigurationCatalog, error) {
	// Discovery is serialized separately from real execution and never borrows
	// an active Bot's native session or execution-bound MCP capability.
	select {
	case h.configurationSlots <- struct{}{}:
		defer func() { <-h.configurationSlots }()
	default:
		return pa.ConfigurationCatalog{}, failure(pa.CapacityFull, "configuration_discovery")
	}
	if h.o.Provider == nil {
		return pa.ConfigurationCatalog{}, failure(pa.Unsupported, "configuration")
	}
	p, err := h.o.Provider()
	if err != nil {
		return pa.ConfigurationCatalog{}, err
	}
	reader, ok := p.(interface {
		Configuration(context.Context, pa.Bot, string, string) (pa.ConfigurationCatalog, error)
	})
	if !ok {
		return pa.ConfigurationCatalog{}, failure(pa.Unsupported, "configuration")
	}
	return reader.Configuration(ctx, bot, dir, model)
}

func (h *Host) ConfigurationUsage() int { return len(h.configurationSlots) }

// DiscoverModels asks agent for its configuration under the machine's own
// model-catalog identity, in dir, which must be that identity's personal
// workspace. It takes the same discovery slot a Bot's model dialog does, so
// it never runs beside one; a busy slot is a CapacityFull refusal.
func (h *Host) DiscoverModels(ctx context.Context, agent pa.Agent, dir string) (pa.ConfigurationCatalog, error) {
	if !agent.Valid() {
		return pa.ConfigurationCatalog{}, failure(pa.Unsupported, "agent")
	}
	return h.configuration(ctx, pa.ModelCatalogBot(h.o.MachineID, agent), dir, "")
}
