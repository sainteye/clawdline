package antigravitysession

import (
	"context"
	"encoding/json"

	"github.com/sainteye/clawdline/internal/adapters/antigravityprocess"
)

func selectInitialModel(ctx context.Context, client *antigravityprocess.Client, c *conversation, wanted string) error {
	if wanted == "" {
		return nil
	}
	c.mu.Lock()
	current, structured := c.model, c.structuredConfig
	offered := false
	for _, model := range c.models {
		offered = offered || model.ID == wanted
	}
	c.mu.Unlock()
	if !offered {
		return refusal("model_not_offered")
	}
	if current == wanted {
		return nil
	}
	if !structured {
		if err := client.SetModelChecked(ctx, wanted, nil); err != nil {
			return err
		}
		c.mu.Lock()
		c.model = wanted
		c.mu.Unlock()
		return nil
	}
	return client.SetConfigOptionChecked(ctx, "model", wanted, nil, func(raw json.RawMessage) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		if err := c.readMetadata(raw, false); err != nil {
			return err
		}
		if c.model != wanted {
			return refusal("config_not_acknowledged")
		}
		return nil
	})
}
