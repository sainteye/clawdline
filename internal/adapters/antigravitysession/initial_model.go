package antigravitysession

import (
	"context"
	"encoding/json"
	"log"

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
		c.model, c.chosenModel = wanted, wanted
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
		c.chosenModel = wanted
		return nil
	})
}

// reapplyChoices selects the person's saved model and effort again after a
// load, because AGY 1.5 restores its defaults. It returns the metadata the
// runtime last acknowledged. A choice no longer offered, or a refused write,
// leaves the runtime's own value in place so the Session still opens and shows
// what will actually run.
func reapplyChoices(ctx context.Context, client *antigravityprocess.Client, metadata json.RawMessage, model, effort string) json.RawMessage {
	for _, choice := range []struct {
		effort bool
		value  string
	}{{false, model}, {true, effort}} {
		if choice.value == "" {
			continue
		}
		var current conversation
		if current.readMetadata(metadata, true) != nil || !current.structuredConfig {
			return metadata
		}
		id, now, offered := "model", current.model, false
		if choice.effort {
			id, now = current.effortConfigID(), current.effort
			for _, e := range current.efforts {
				offered = offered || e == choice.value
			}
		} else {
			for _, m := range current.models {
				offered = offered || m.ID == choice.value
			}
		}
		if !offered || now == choice.value {
			continue
		}
		var acknowledged json.RawMessage
		err := client.SetConfigOptionChecked(ctx, id, choice.value, nil, func(raw json.RawMessage) error {
			acknowledged = append(json.RawMessage(nil), raw...)
			return nil
		})
		if err != nil {
			log.Printf("antigravity: saved %s was not restored after load: %v", id, err)
			continue
		}
		var check conversation
		if check.readMetadata(acknowledged, false) == nil {
			metadata = acknowledged
		}
	}
	return metadata
}
