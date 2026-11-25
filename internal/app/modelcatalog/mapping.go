// Package modelcatalog is the daemon's one model catalog: every console that
// offers a model for an agent reads it through GET /v1/model-catalog or the
// Cloud word model-catalog (docs/model-catalog.md). A read answers at once
// from what is cached; the agent's own answer refines the native list in the
// background.
package modelcatalog

import (
	"strings"

	mc "github.com/sainteye/clawdline/internal/domain/modelcatalog"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// modelPart is the model of one ACP choice. Codex's adapter lists each model
// once per effort ("gpt-5.5/high"); a launch names the model and the effort
// separately, so only the part before "/" is a model.
func modelPart(value string) string {
	model, _, _ := strings.Cut(value, "/")
	return model
}

// isDefault finds the agent's own "Default" entry: naming it is the same as
// naming no model, which every picker offers first.
func isDefault(c pa.ConfigChoice) bool {
	return c.Value == "default" || strings.HasPrefix(strings.ToLower(c.Name), "default")
}

// FromConfiguration turns an agent's ACP configuration answer into the
// launchable models a picker offers and the name of what an unset model runs.
//
// The Default entry is left out. Codex's per-effort entries collapse to one
// model each. A value the launcher refuses (mc.ModelName) is never offered.
// The default is named as the agent names it: Claude's Default entry carries
// the model in its note; Codex's Default entry copies the default model's own
// note ("Fast, go-to model for …"), which is a description, not a name, so
// the model with that same note is the one meant. Without a Default entry the
// agent's current concrete choice runs.
func FromConfiguration(catalog pa.ConfigurationCatalog) (models []mc.Model, defaultName string) {
	models = []mc.Model{}
	var option *pa.ConfigOption
	for i := range catalog.Options {
		if catalog.Options[i].Category == "model" {
			option = &catalog.Options[i]
			break
		}
	}
	if option == nil {
		return models, ""
	}
	var own *pa.ConfigChoice
	for i := range option.Choices {
		if isDefault(option.Choices[i]) {
			own = &option.Choices[i]
			break
		}
	}
	seen := map[string]bool{}
	for _, choice := range option.Choices {
		if own != nil && choice.Value == own.Value {
			continue
		}
		value := modelPart(choice.Value)
		if _, ok := mc.ModelName(value); !ok || seen[value] {
			continue
		}
		seen[value] = true
		if len(models) == mc.MaxCatalogModels {
			break
		}
		if value != choice.Value {
			models = append(models, mc.Model{Value: value, Name: value})
			continue
		}
		name := choice.Name
		if name == "" {
			name = value
		}
		models = append(models, mc.Model{Value: value, Name: name, Description: choice.Description})
	}
	if own != nil {
		if own.Description != "" {
			for _, c := range option.Choices {
				if c.Value == own.Value || c.Description != own.Description {
					continue
				}
				// A per-effort entry is offered as its model, so it is named so.
				if model := modelPart(c.Value); model != c.Value {
					return models, model
				}
				if c.Name != "" {
					return models, c.Name
				}
			}
		}
		return models, own.Description
	}
	for _, c := range option.Choices {
		if c.Value != option.Current {
			continue
		}
		if model := modelPart(c.Value); model != c.Value {
			return models, model
		}
		if c.Name != "" {
			return models, c.Name
		}
		return models, c.Value
	}
	return models, ""
}

// NativeModel is one row of the fast tier, as the agent's own files or CLI
// list it: Command is the launchable value, Name what a person reads.
type NativeModel struct{ Name, Command string }

// FromNative keeps the launchable, distinct rows of the fast tier.
func FromNative(rows []NativeModel) []mc.Model {
	out := []mc.Model{}
	seen := map[string]bool{}
	for _, r := range rows {
		if _, ok := mc.ModelName(r.Command); !ok || seen[r.Command] || len(out) == mc.MaxCatalogModels {
			continue
		}
		seen[r.Command] = true
		name := r.Name
		if name == "" {
			name = r.Command
		}
		out = append(out, mc.Model{Value: r.Command, Name: name})
	}
	return out
}
