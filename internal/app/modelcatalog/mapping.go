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

// effortWords are the effort names an ACP model value may carry after "/"
// (Codex's per-effort entries) and a saved Bot choice may name. Only these
// split a value: a model id that merely contains "/" stays one model.
var effortWords = map[string]bool{"none": true, "minimal": true, "low": true, "medium": true, "high": true, "xhigh": true, "max": true}

// Choice is what a Bot's saved runtime model holds when a person picks a
// catalog model and an effort: the model alone, or "model/effort". It is the
// same spelling as Codex's per-effort ACP values, so a saved ACP value and a
// saved catalog choice read alike.
func Choice(model, effort string) string {
	if model == "" || effort == "" {
		return model
	}
	return model + "/" + effort
}

// FromChoice reads a saved runtime model as the catalog entry a picker shows:
// "gpt-5.5/high" is model gpt-5.5 with effort high; the agent's own "default"
// entry is no model (the agent default); anything else is a model as saved.
func FromChoice(value string) (model, effort string) {
	if value == "default" {
		return "", ""
	}
	if i := strings.LastIndex(value, "/"); i > 0 && effortWords[value[i+1:]] {
		return value[:i], value[i+1:]
	}
	return value, ""
}

// Configured is what one saved choice asks of the agent's ACP session.
type Configured struct {
	// Model is the ACP model value to set; "" sets none (the agent default).
	Model string
	// Options are further ACP options the choice implies: an effort the agent
	// takes as its own option rather than inside the model value.
	Options map[string]string
}

func choiceOffered(o *pa.ConfigOption, value string) bool {
	for _, c := range o.Choices {
		if c.Value == value {
			return true
		}
	}
	return false
}

// effortOption is the agent's own effort option that offers effort, if any.
func effortOption(catalog pa.ConfigurationCatalog, effort string) string {
	for i := range catalog.Options {
		o := &catalog.Options[i]
		if o.Category == "model" {
			continue
		}
		if o.Category == "thought_level" || o.ID == "reasoning_effort" || o.ID == "reasoning-effort" || o.ID == "effort" || o.ID == "thought_level" {
			if choiceOffered(o, effort) {
				return o.ID
			}
		}
	}
	return ""
}

// ToConfiguration is FromConfiguration's inverse for one saved choice: the
// ACP value (and any effort option) that applies it in this agent's answer.
//
// A value the agent lists exactly is applied as it is, so every ACP value
// saved before the catalog keeps working. A catalog model with an effort is
// the agent's per-effort entry, or the plain model plus the agent's own
// effort option. A catalog model without an effort is the per-effort entry
// the agent runs now for that model, else the one with the effort it runs
// now, else its "medium" entry (Codex's own default effort), else its only
// entry. "default" the agent no longer lists is the agent default. Anything
// else is not ok: the caller refuses rather than guessing.
func ToConfiguration(catalog pa.ConfigurationCatalog, choice string) (Configured, bool) {
	if choice == "" {
		return Configured{}, true
	}
	var option *pa.ConfigOption
	for i := range catalog.Options {
		if catalog.Options[i].Category == "model" {
			option = &catalog.Options[i]
			break
		}
	}
	if option == nil {
		return Configured{}, false
	}
	if choiceOffered(option, choice) {
		return Configured{Model: choice}, true
	}
	model, effort := FromChoice(choice)
	if model == "" {
		return Configured{}, true
	}
	if effort != "" {
		if choiceOffered(option, model) {
			if id := effortOption(catalog, effort); id != "" {
				return Configured{Model: model, Options: map[string]string{id: effort}}, true
			}
		}
		return Configured{}, false
	}
	entries := []string{}
	for _, c := range option.Choices {
		if c.Value != model && modelPart(c.Value) == model {
			entries = append(entries, c.Value)
		}
	}
	if len(entries) == 0 {
		return Configured{}, false
	}
	if modelPart(option.Current) == model && choiceOffered(option, option.Current) {
		return Configured{Model: option.Current}, true
	}
	if _, now := FromChoice(option.Current); now != "" && choiceOffered(option, Choice(model, now)) {
		return Configured{Model: Choice(model, now)}, true
	}
	if choiceOffered(option, Choice(model, "medium")) {
		return Configured{Model: Choice(model, "medium")}, true
	}
	if len(entries) == 1 {
		return Configured{Model: entries[0]}, true
	}
	return Configured{}, false
}
