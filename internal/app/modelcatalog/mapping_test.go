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
