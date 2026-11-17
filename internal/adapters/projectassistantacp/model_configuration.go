package projectassistantacp

import (
	"context"
	"encoding/json"
	"errors"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"sort"
	"strconv"
)

type nativeConfigOption struct {
	ID       string               `json:"id"`
	Name     string               `json:"name"`
	Category string               `json:"category"`
	Type     string               `json:"type"`
	Current  json.RawMessage      `json:"currentValue"`
	Options  []nativeConfigChoice `json:"options"`
}
type nativeConfigChoice struct {
	Value   string               `json:"value"`
	Name    string               `json:"name"`
	Options []nativeConfigChoice `json:"options"`
}
type nativeConfiguration struct {
	Options []nativeConfigOption `json:"configOptions"`
	Models  *struct {
		Current   string `json:"currentModelId"`
		Available []struct {
			ID   string `json:"modelId"`
			Name string `json:"name"`
		} `json:"availableModels"`
	} `json:"models"`
}

func parseConfiguration(raw []byte, agent pa.Agent) (pa.ConfigurationCatalog, error) {
	var native nativeConfiguration
	if json.Unmarshal(raw, &native) != nil || len(native.Options) > pa.MaxConfigurationOptions {
		return pa.ConfigurationCatalog{}, refusal(pa.Unsupported, "config_options")
	}
	out := pa.ConfigurationCatalog{Agent: agent, Options: []pa.ConfigOption{}}
	seen := map[string]bool{}
	for _, o := range native.Options {
		// Modes remain the independently validated approval control. Other
		// native keys cannot acquire launch, permission or login authority.
		category := o.Category
		if category == "" && (o.ID == "model" || o.ID == "models") {
			category = "model"
		}
		allowed := category == "model" || category == "thought_level" || o.ID == "reasoning_effort" || o.ID == "reasoning-effort" || o.ID == "effort" || o.ID == "thought_level" || o.ID == "fast" || o.ID == "fast-mode"
		if !allowed || category == "mode" {
			continue
		}
		if o.ID == "" || len(o.ID) > pa.MaxConfigurationValueBytes || seen[o.ID] {
			return out, refusal(pa.Unsupported, "config_options")
		}
		seen[o.ID] = true
		p := pa.ConfigOption{ID: o.ID, Name: o.Name, Category: category, Choices: []pa.ConfigChoice{}}
		if p.Name == "" {
			p.Name = p.ID
		}
		if o.Type == "boolean" && (o.ID == "fast" || o.ID == "fast-mode") {
			var b bool
			if json.Unmarshal(o.Current, &b) != nil {
				return out, refusal(pa.Unsupported, "config_boolean")
			}
			p.Current = strconv.FormatBool(b)
			p.Choices = []pa.ConfigChoice{{Value: "false", Name: "false"}, {Value: "true", Name: "true"}}
		} else if o.Type == "select" || o.Type == "" {
			if len(o.Current) > 0 && json.Unmarshal(o.Current, &p.Current) != nil {
				return out, refusal(pa.Unsupported, "config_current")
			}
			var collect func([]nativeConfigChoice) error
			collect = func(choices []nativeConfigChoice) error {
				for _, c := range choices {
					if len(c.Options) > 0 {
						if err := collect(c.Options); err != nil {
							return err
						}
					} else if c.Value != "" {
						name := c.Name
						if name == "" {
							name = c.Value
						}
						if len(c.Value) > pa.MaxConfigurationValueBytes {
							return refusal(pa.CapacityFull, "config_value")
						}
						p.Choices = append(p.Choices, pa.ConfigChoice{Value: c.Value, Name: name})
						if len(p.Choices) > pa.MaxConfigurationChoices {
							return refusal(pa.CapacityFull, "config_choices")
						}
					}
				}
				return nil
			}
			if err := collect(o.Options); err != nil {
				return out, err
			}
		} else {
			continue
		}
		if category == "model" {
			out.Model = p.Current
		}
		out.Options = append(out.Options, p)
	}
	if out.Model == "" && native.Models != nil {
		p := pa.ConfigOption{ID: "", Name: "Model", Category: "model", Current: native.Models.Current, Choices: []pa.ConfigChoice{}}
		for _, m := range native.Models.Available {
			p.Choices = append(p.Choices, pa.ConfigChoice{Value: m.ID, Name: m.Name})
		}
		if len(p.Choices) > pa.MaxConfigurationChoices {
			return out, refusal(pa.CapacityFull, "config_choices")
		}
		out.Model = p.Current
		out.Options = append(out.Options, p)
	}
	return out, nil
}
func offers(option pa.ConfigOption, value string) bool {
	for _, v := range option.Choices {
		if v.Value == value {
			return true
		}
	}
	return false
}

// configureModel follows CodyNC select_model: model first, then the options
// returned for that model. A saved selection is never a capability assertion.
func (c *Client) configureModel(ctx context.Context, bot pa.Bot, raw []byte, apply bool) (pa.ConfigurationCatalog, error) {
	catalog, err := parseConfiguration(raw, bot.Agent)
	if err != nil {
		return catalog, err
	}
	if bot.RuntimeSettings.Model != "" {
		var model *pa.ConfigOption
		for i := range catalog.Options {
			if catalog.Options[i].Category == "model" {
				model = &catalog.Options[i]
				break
			}
		}
		if model == nil || !offers(*model, bot.RuntimeSettings.Model) {
			return catalog, refusal(pa.Unsupported, "model")
		}
		var answer []byte
		if model.ID != "" {
			answer, err = c.call(ctx, "session/set_config_option", map[string]any{"sessionId": c.SessionID(), "configId": model.ID, "value": bot.RuntimeSettings.Model})
		} else {
			answer, err = c.call(ctx, "session/set_model", map[string]string{"sessionId": c.SessionID(), "modelId": bot.RuntimeSettings.Model})
		}
		if err != nil {
			return catalog, err
		}
		var changed struct {
			Options json.RawMessage `json:"configOptions"`
		}
		if json.Unmarshal(answer, &changed) != nil {
			return catalog, refusal(pa.Unsupported, "config_response")
		}
		if len(changed.Options) > 0 {
			catalog, err = parseConfiguration(answer, bot.Agent)
			if err != nil {
				return catalog, err
			}
		}
		if model.ID != "" && (len(changed.Options) == 0 || catalog.Model != bot.RuntimeSettings.Model) {
			return catalog, refusal(pa.Unsupported, "model_not_applied")
		}
		catalog.Model = bot.RuntimeSettings.Model
		if len(changed.Options) > 0 {
			raw = answer
		}
	}
	if !apply {
		return catalog, nil
	}
	ids := make([]string, 0, len(bot.RuntimeSettings.Options))
	for id := range bot.RuntimeSettings.Options {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		value := bot.RuntimeSettings.Options[id]
		var found *pa.ConfigOption
		for i := range catalog.Options {
			if catalog.Options[i].ID == id && catalog.Options[i].Category != "model" {
				found = &catalog.Options[i]
				break
			}
		}
		if found == nil || !offers(*found, value) {
			return catalog, refusal(pa.Unsupported, "model_option")
		}
		var v any = value
		// Boolean options are distinguished by their actual native type, not
		// by a guessed flag. Re-read the latest ACP response for that type.
		var native nativeConfiguration
		_ = json.Unmarshal(raw, &native)
		for _, o := range native.Options {
			if o.ID == id && o.Type == "boolean" {
				v = value == "true"
			}
		}
		answer, e := c.call(ctx, "session/set_config_option", map[string]any{"sessionId": c.SessionID(), "configId": id, "value": v})
		if e != nil {
			return catalog, e
		}
		var changed struct {
			Options json.RawMessage `json:"configOptions"`
		}
		if json.Unmarshal(answer, &changed) != nil {
			return catalog, refusal(pa.Unsupported, "config_response")
		}
		if len(changed.Options) == 0 {
			return catalog, refusal(pa.Unsupported, "model_option_not_applied")
		}
		if len(changed.Options) > 0 {
			raw = answer
			catalog, err = parseConfiguration(answer, bot.Agent)
			if err != nil {
				return catalog, err
			}
		}
		applied := false
		for _, o := range catalog.Options {
			if o.ID == id && o.Current == value {
				applied = true
			}
		}
		if !applied {
			return catalog, refusal(pa.Unsupported, "model_option_not_applied")
		}
	}
	return catalog, nil
}

// Configuration is a disposable protocol discovery session. It sends no prompt,
// attaches no tools, records no native ownership, and never resumes a Bot.
func probeConfiguration(ctx context.Context, config Configuration, caps pa.Capabilities, limits Limits, bot pa.Bot, dir, model string) (result pa.ConfigurationCatalog, err error) {
	ctx, cancel := context.WithTimeout(ctx, limits.RPCTimeout)
	defer cancel()
	p, err := StartProcess(ctx, ProcessOptions{Configuration: config, Directory: dir, Limits: limits})
	if err != nil {
		return result, err
	}
	bot.RuntimeSettings = pa.RuntimeSettings{Model: model, Options: bot.RuntimeSettings.Options}
	fence := pa.Fence{Key: bot.Key, Project: bot.Project, ConversationID: bot.ConversationID, BindingID: "configuration-discovery", Generation: 1}
	c, err := NewClient(p, limits, fence, func(context.Context, app.ProtocolEvent) error { return refusal(pa.ScopeDenied, "configuration_event") }, nil)
	if err != nil {
		cleanup, stop := context.WithTimeout(context.Background(), limits.CleanupTimeout)
		defer stop()
		return result, errors.Join(err, p.Close(cleanup))
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), limits.CleanupTimeout)
		defer stop()
		err = errors.Join(err, c.Close(cleanup))
	}()
	c.adapterVersion = caps.AdapterVersion
	c.discovery = true
	c.discoveryApply = len(bot.RuntimeSettings.Options) > 0
	err = c.initialize(ctx, app.OpenRequest{Bot: bot, Binding: pa.ExecutionBinding{Fence: fence, Agent: bot.Agent, InstructionRevision: bot.Instructions.Revision}, Directory: dir})
	return c.configuration, err
}

func (p *Provider) Configuration(ctx context.Context, bot pa.Bot, dir, model string) (pa.ConfigurationCatalog, error) {
	config, caps, err := p.admit(ctx, bot)
	if err != nil {
		return pa.ConfigurationCatalog{}, err
	}
	return probeConfiguration(ctx, config, caps, p.o.Limits, bot, dir, model)
}
func (p *installedClaude) Configuration(ctx context.Context, bot pa.Bot, dir, model string) (pa.ConfigurationCatalog, error) {
	config, caps, cleanup, err := p.prepare(ctx, bot, nil)
	defer cleanup()
	if err != nil {
		return pa.ConfigurationCatalog{}, err
	}
	return probeConfiguration(ctx, config, caps, nativeLimits(), bot, dir, model)
}
func (p *installedCodex) Configuration(ctx context.Context, bot pa.Bot, dir, model string) (pa.ConfigurationCatalog, error) {
	config, caps, cleanup, err := p.prepare(ctx, bot)
	defer cleanup()
	if err != nil {
		return pa.ConfigurationCatalog{}, err
	}
	return probeConfiguration(ctx, config, caps, nativeLimits(), bot, dir, model)
}
func (p *installedAgents) Configuration(ctx context.Context, bot pa.Bot, dir, model string) (pa.ConfigurationCatalog, error) {
	provider, err := p.pick(bot.Agent)
	if err != nil {
		return pa.ConfigurationCatalog{}, err
	}
	return provider.(interface {
		Configuration(context.Context, pa.Bot, string, string) (pa.ConfigurationCatalog, error)
	}).Configuration(ctx, bot, dir, model)
}
