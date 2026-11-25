package modelcatalog

import (
	"reflect"
	"testing"

	mc "github.com/sainteye/clawdline/internal/domain/modelcatalog"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func configuration(agent pa.Agent, current string, choices ...pa.ConfigChoice) pa.ConfigurationCatalog {
	return pa.ConfigurationCatalog{Agent: agent, Options: []pa.ConfigOption{{ID: "model", Name: "Model", Category: "model", Current: current, Choices: choices}}}
}

func values(models []mc.Model) []string {
	out := []string{}
	for _, m := range models {
		out = append(out, m.Value)
	}
	return out
}

func TestClaudesDefaultEntryBecomesTheNamedDefaultAndIsNotOfferedTwice(t *testing.T) {
	models, name := FromConfiguration(configuration(pa.ClaudeCode, "default",
		pa.ConfigChoice{Value: "default", Name: "Default (recommended)", Description: "Opus 4.5"},
		pa.ConfigChoice{Value: "sonnet", Name: "Sonnet", Description: "Sonnet 4.5"},
		pa.ConfigChoice{Value: "haiku", Name: "Haiku"},
	))
	if name != "Opus 4.5" {
		t.Fatalf("default name = %q", name)
	}
	want := []mc.Model{{Value: "sonnet", Name: "Sonnet", Description: "Sonnet 4.5"}, {Value: "haiku", Name: "Haiku"}}
	if !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %#v", models)
	}
}

func TestCodexPerEffortEntriesCollapseToOneModelEach(t *testing.T) {
	models, name := FromConfiguration(configuration(pa.Codex, "gpt-5.5/high",
		pa.ConfigChoice{Value: "gpt-5.5/low", Name: "gpt-5.5 (low)"},
		pa.ConfigChoice{Value: "gpt-5.5/high", Name: "gpt-5.5 (high)"},
		pa.ConfigChoice{Value: "gpt-5.4-mini/medium", Name: "gpt-5.4-mini (medium)"},
	))
	if name != "gpt-5.5" {
		t.Fatalf("a current value without a default entry names the model that runs; got %q", name)
	}
	if got := values(models); !reflect.DeepEqual(got, []string{"gpt-5.5", "gpt-5.4-mini"}) {
		t.Fatalf("models = %v", got)
	}
}

func TestNamesTheLauncherWouldRefuseAreNeverOffered(t *testing.T) {
	models, _ := FromConfiguration(configuration(pa.ClaudeCode, "default",
		pa.ConfigChoice{Value: "default", Name: "Default"},
		pa.ConfigChoice{Value: "opus[1m]", Name: "Opus 1M"},
		pa.ConfigChoice{Value: "Sonnet", Name: "Sonnet"},
		pa.ConfigChoice{Value: "-x", Name: "dash"},
		pa.ConfigChoice{Value: "opus", Name: "Opus"},
	))
	if got := values(models); !reflect.DeepEqual(got, []string{"opus"}) {
		t.Fatalf("models = %v", got)
	}
}

func TestNoModelOptionOffersOnlyTheAgentDefault(t *testing.T) {
	models, name := FromConfiguration(pa.ConfigurationCatalog{Agent: pa.Grok})
	if len(models) != 0 || models == nil || name != "" {
		t.Fatalf("got %#v %q", models, name)
	}
}

func TestAConcreteCurrentModelIsNamedByItsNameNotItsDescription(t *testing.T) {
	// The shape Codex's adapter sent on 2026-11-25: no Default entry, notes that describe each model.
	models, name := FromConfiguration(configuration(pa.Codex, "model-sol",
		pa.ConfigChoice{Value: "model-sol", Name: "6.2 Sol", Description: "Fast, go-to model for coding and everyday work."},
		pa.ConfigChoice{Value: "model-astra", Name: "6.2 Astra", Description: "Our most capable model for complex coding and professional work."},
	))
	if name != "6.2 Sol" || models[0].Name != "6.2 Sol" || models[1].Name != "6.2 Astra" {
		t.Fatalf("got %#v %q", models, name)
	}
}

func TestADefaultEntryThatCopiesAModelsNoteIsNamedByThatModel(t *testing.T) {
	// The shape Codex's adapter sent on this machine on 2026-11-25.
	models, name := FromConfiguration(configuration(pa.Codex, "default",
		pa.ConfigChoice{Value: "default", Name: "Default", Description: "Fast, go-to model for coding and everyday work."},
		pa.ConfigChoice{Value: "model-sol", Name: "6.2 Sol", Description: "Fast, go-to model for coding and everyday work."},
		pa.ConfigChoice{Value: "model-astra", Name: "6.2 Astra", Description: "Our most capable model for complex coding and professional work."},
	))
	if name != "6.2 Sol" {
		t.Fatalf("default name = %q", name)
	}
	if got := values(models); !reflect.DeepEqual(got, []string{"model-sol", "model-astra"}) {
		t.Fatalf("models = %v", got)
	}
}

func TestNativeRowsKeepOnlyLaunchableDistinctValues(t *testing.T) {
	got := FromNative([]NativeModel{{Name: "Opus 5.5", Command: "opus"}, {Name: "again", Command: "opus"}, {Name: "bad", Command: "Opus"}, {Command: "sonnet"}})
	want := []mc.Model{{Value: "opus", Name: "Opus 5.5"}, {Value: "sonnet", Name: "sonnet"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
}

func TestADefaultEntryCopyingAPerEffortEntrysNoteIsNamedByItsModel(t *testing.T) {
	_, name := FromConfiguration(configuration(pa.Codex, "default",
		pa.ConfigChoice{Value: "default", Name: "Default", Description: "Fast, go-to model."},
		pa.ConfigChoice{Value: "model-sol/high", Name: "6.2 Sol (high)", Description: "Fast, go-to model."},
	))
	if name != "model-sol" {
		t.Fatalf("default name = %q", name)
	}
}

func claudeAnswer() pa.ConfigurationCatalog {
	return configuration(pa.ClaudeCode, "default",
		pa.ConfigChoice{Value: "default", Name: "Default (recommended)", Description: "Opus 4.5"},
		pa.ConfigChoice{Value: "sonnet", Name: "Sonnet"},
		pa.ConfigChoice{Value: "haiku", Name: "Haiku"},
	)
}

func codexAnswer(current string) pa.ConfigurationCatalog {
	return configuration(pa.Codex, current,
		pa.ConfigChoice{Value: "gpt-5.5/low", Name: "gpt-5.5 (low)"},
		pa.ConfigChoice{Value: "gpt-5.5/medium", Name: "gpt-5.5 (medium)"},
		pa.ConfigChoice{Value: "gpt-5.5/high", Name: "gpt-5.5 (high)"},
		pa.ConfigChoice{Value: "gpt-5.4-mini/high", Name: "gpt-5.4-mini (high)"},
	)
}

func applies(t *testing.T, catalog pa.ConfigurationCatalog, choice string, want Configured) {
	t.Helper()
	got, ok := ToConfiguration(catalog, choice)
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("ToConfiguration(%q) = %#v %v, want %#v", choice, got, ok, want)
	}
}

func TestAClaudeCatalogModelIsItsOwnACPValueAndNoModelIsTheDefault(t *testing.T) {
	applies(t, claudeAnswer(), "sonnet", Configured{Model: "sonnet"})
	applies(t, claudeAnswer(), "", Configured{})
	// A value saved before the catalog, the agent's own Default entry, is still applied as it is.
	applies(t, claudeAnswer(), "default", Configured{Model: "default"})
	if m, e := FromChoice("default"); m != "" || e != "" {
		t.Fatalf("saved default reads as %q %q, want the agent default", m, e)
	}
	if m, e := FromChoice("sonnet"); m != "sonnet" || e != "" {
		t.Fatalf("saved sonnet reads as %q %q", m, e)
	}
	// An agent that no longer lists its Default entry runs its default for "default".
	applies(t, configuration(pa.ClaudeCode, "sonnet", pa.ConfigChoice{Value: "sonnet", Name: "Sonnet"}), "default", Configured{})
}

func TestACodexCatalogModelAndEffortIsItsPerEffortEntry(t *testing.T) {
	applies(t, codexAnswer("gpt-5.5/high"), Choice("gpt-5.5", "high"), Configured{Model: "gpt-5.5/high"})
	if got := Choice("gpt-5.5", "high"); got != "gpt-5.5/high" {
		t.Fatalf("Choice = %q", got)
	}
	if m, e := FromChoice("gpt-5.5/high"); m != "gpt-5.5" || e != "high" {
		t.Fatalf("saved gpt-5.5/high reads as %q %q", m, e)
	}
	// A model id with "/" that is not an effort stays one model.
	if m, e := FromChoice("models/gemini-pro"); m != "models/gemini-pro" || e != "" {
		t.Fatalf("got %q %q", m, e)
	}
}

func TestACodexCatalogModelWithoutAnEffortKeepsTheEffortThatRuns(t *testing.T) {
	// The model already runs: keep its effort.
	applies(t, codexAnswer("gpt-5.5/low"), "gpt-5.5", Configured{Model: "gpt-5.5/low"})
	// Another model runs: the same effort for the chosen model.
	applies(t, codexAnswer("gpt-5.4-mini/high"), "gpt-5.5", Configured{Model: "gpt-5.5/high"})
	// No effort runs that the chosen model has: Codex's own default effort.
	applies(t, codexAnswer("default"), "gpt-5.5", Configured{Model: "gpt-5.5/medium"})
	// The only entry for a model.
	applies(t, codexAnswer("gpt-5.5/low"), "gpt-5.4-mini", Configured{Model: "gpt-5.4-mini/high"})
}

func TestAnEffortTheAgentTakesAsItsOwnOptionIsSetBesideThePlainModel(t *testing.T) {
	catalog := configuration(pa.Codex, "model-sol",
		pa.ConfigChoice{Value: "model-sol", Name: "6.2 Sol"}, pa.ConfigChoice{Value: "model-astra", Name: "6.2 Astra"})
	catalog.Options = append(catalog.Options, pa.ConfigOption{ID: "reasoning_effort", Name: "Effort", Category: "thought_level", Current: "medium",
		Choices: []pa.ConfigChoice{{Value: "medium", Name: "Medium"}, {Value: "high", Name: "High"}}})
	applies(t, catalog, "model-astra/high", Configured{Model: "model-astra", Options: map[string]string{"reasoning_effort": "high"}})
	if _, ok := ToConfiguration(catalog, "model-astra/xhigh"); ok {
		t.Fatal("an effort the agent does not offer was mapped")
	}
}

func TestAnUnmappedSavedValueIsRefusedNotGuessed(t *testing.T) {
	for _, choice := range []string{"opus", "gpt-5.5/xhigh", "gpt-9"} {
		if got, ok := ToConfiguration(codexAnswer("gpt-5.5/high"), choice); ok {
			t.Fatalf("%q mapped to %#v", choice, got)
		}
	}
	if _, ok := ToConfiguration(pa.ConfigurationCatalog{Agent: pa.Grok}, "grok-4"); ok {
		t.Fatal("a model was mapped for an agent without a model option")
	}
	// The value itself is kept for display: it reads back as saved.
	if m, e := FromChoice("opus[1m]"); m != "opus[1m]" || e != "" {
		t.Fatalf("got %q %q", m, e)
	}
}
