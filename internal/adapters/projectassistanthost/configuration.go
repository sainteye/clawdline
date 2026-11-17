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
