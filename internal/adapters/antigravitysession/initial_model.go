package antigravitysession

import (
	"context"
	"encoding/json"
	"log"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/antigravityprocess"
	"github.com/sainteye/clawdline/internal/contract"
)

func resolveModelChoice(wanted string, models []contract.SessionModel, efforts []string) (string, string, bool) {
	if wanted == "" {
		return "", "", false
	}
	for _, m := range models {
		if m.ID == wanted {
			return wanted, "", true
		}
	}
	checkEffort := func(e string) (string, string, bool) {
		suffix := "-" + e
		if strings.HasSuffix(wanted, suffix) {
			base := strings.TrimSuffix(wanted, suffix)
			for _, m := range models {
				if m.ID == base {
					return base, e, true
				}
			}
		}
		return "", "", false
	}
	for _, e := range efforts {
		if base, effort, ok := checkEffort(e); ok {
			return base, effort, true
		}
	}
	for _, e := range []string{"low", "medium", "high", "xhigh", "max"} {
		if base, effort, ok := checkEffort(e); ok {
			return base, effort, true
		}
	}
	return "", "", false
}

func selectInitialModel(ctx context.Context, client *antigravityprocess.Client, c *conversation, wanted string) error {
	if wanted == "" {
		return nil
	}
	c.mu.Lock()
	currentModel, currentEffort, structured := c.model, c.effort, c.structuredConfig
	models := c.models
	efforts := c.efforts
	c.mu.Unlock()
	targetModel, targetEffort, ok := resolveModelChoice(wanted, models, efforts)
	if !ok {
		return refusal("model_not_offered")
	}
	if !structured {
		if currentModel == targetModel {
			return nil
		}
		if err := client.SetModelChecked(ctx, targetModel, nil); err != nil {
			return err
		}
		c.mu.Lock()
		c.model, c.chosenModel = targetModel, targetModel
		c.mu.Unlock()
		return nil
	}
	if currentModel != targetModel {
		err := client.SetConfigOptionChecked(ctx, "model", targetModel, nil, func(raw json.RawMessage) error {
			c.mu.Lock()
			defer c.mu.Unlock()
			if err := c.readMetadata(raw, false); err != nil {
				return err
			}
			if c.model != targetModel {
				return refusal("config_not_acknowledged")
			}
			c.chosenModel = targetModel
			return nil
		})
		if err != nil {
			return err
		}
	} else {
		c.mu.Lock()
		c.chosenModel = targetModel
		c.mu.Unlock()
	}
	if targetEffort != "" {
		c.mu.Lock()
		effortConfigID := c.effortConfigID()
		effortNeeded := c.effort != targetEffort
		c.mu.Unlock()
		if effortNeeded && effortConfigID != "" {
			err := client.SetConfigOptionChecked(ctx, effortConfigID, targetEffort, nil, func(raw json.RawMessage) error {
				c.mu.Lock()
				defer c.mu.Unlock()
				if err := c.readMetadata(raw, false); err != nil {
					return err
				}
				if c.effort != targetEffort {
					return refusal("config_not_acknowledged")
				}
				c.chosenEffort = targetEffort
				return nil
			})
			if err != nil {
				return err
			}
		} else {
			c.mu.Lock()
			c.chosenEffort = targetEffort
			c.mu.Unlock()
		}
	} else if currentEffort != "" {
		c.mu.Lock()
		c.chosenEffort = currentEffort
		c.mu.Unlock()
	}
	return nil
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
