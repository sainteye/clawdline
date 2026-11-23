package projectassistant

import (
	"context"
	"testing"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type catalogProbe struct {
	calls  int
	models []string
	fail   error
}

func (p *catalogProbe) read(_ context.Context, b pa.Bot, _ string, model string) (pa.ConfigurationCatalog, error) {
	p.calls++
	if p.fail != nil {
		return pa.ConfigurationCatalog{}, p.fail
	}
	choices := []pa.ConfigChoice{}
	for _, m := range p.models {
		choices = append(choices, pa.ConfigChoice{Value: m, Name: m})
	}
	return pa.ConfigurationCatalog{Agent: b.Agent, Model: model, Options: []pa.ConfigOption{{ID: "model", Name: "Model", Category: "model", Choices: choices}}}, nil
}

func catalogFixture(t *testing.T) (*serviceFixture, pa.Bot, *catalogProbe, *time.Time) {
	f := newServiceFixture(t)
	bot := f.create(t)
	probe := &catalogProbe{models: []string{"first", "second"}}
	f.s.opts.Configuration = probe.read
	now := time.Unix(1700000000, 0)
	f.s.opts.Clock = func() time.Time { return now }
	return f, bot, probe, &now
}

func readCatalog(t *testing.T, f *serviceFixture, bot pa.Bot, model string) pa.ConfigurationCatalog {
	t.Helper()
	out, check, err := f.s.Configuration(context.Background(), MemoryReadRequest{Key: bot.Key, ConversationID: bot.ConversationID}, bot.Agent, model)
	if err != nil || check == nil {
		t.Fatalf("catalog read failed: %v", err)
	}
	return out
}

func TestReopeningTheModelDialogReusesTheAgentsCatalogUntilItExpires(t *testing.T) {
	f, bot, probe, now := catalogFixture(t)
	first := readCatalog(t, f, bot, "")
	first.Options[0].Choices[0].Name = "changed by a caller"
	again := readCatalog(t, f, bot, "")
	if probe.calls != 1 {
		t.Fatalf("second open started another native probe: %d", probe.calls)
	}
	if again.Options[0].Choices[0].Name != "first" {
		t.Fatal("a caller's change leaked into the remembered catalog")
	}
	probe.models = []string{"third"}
	*now = now.Add(time.Duration(pa.MaxConfigurationCacheSeconds)*time.Second - time.Second)
	readCatalog(t, f, bot, "")
	if probe.calls != 1 {
		t.Fatal("catalog expired before its registered lifetime")
	}
	*now = now.Add(time.Second)
	expired := readCatalog(t, f, bot, "")
	if probe.calls != 2 || expired.Options[0].Choices[0].Value != "third" {
		t.Fatalf("an expired catalog was served instead of the Agent's current list: calls=%d %+v", probe.calls, expired)
	}
}

func TestADifferentModelOrFailedReadAsksTheAgent(t *testing.T) {
	f, bot, probe, _ := catalogFixture(t)
	readCatalog(t, f, bot, "")
	readCatalog(t, f, bot, "second")
	if probe.calls != 2 {
		t.Fatal("a draft model reused another model's catalog")
	}
	f2, bot2, probe2, _ := catalogFixture(t)
	probe2.fail = &pa.Failure{Code: pa.CapacityFull, Field: "configuration_discovery"}
	if _, _, err := f2.s.Configuration(context.Background(), MemoryReadRequest{Key: bot2.Key, ConversationID: bot2.ConversationID}, bot2.Agent, ""); pa.Code(err) != pa.CapacityFull {
		t.Fatal(err)
	}
	probe2.fail = nil
	readCatalog(t, f2, bot2, "")
	if probe2.calls != 2 {
		t.Fatal("a failed read was remembered")
	}
}

func TestDisabledFeatureRefusesEvenARememberedCatalog(t *testing.T) {
	f, bot, probe, _ := catalogFixture(t)
	readCatalog(t, f, bot, "")
	f.authority.Feature = pa.FeatureState{HasData: true}
	f.authority.Source.Access = pa.HistoryOnly
	f.authority.Source.Writable = false
	if _, _, err := f.s.Configuration(context.Background(), MemoryReadRequest{Key: bot.Key, ConversationID: bot.ConversationID}, bot.Agent, ""); pa.Code(err) != pa.Disabled {
		t.Fatalf("remembered catalog bypassed authority: %v", err)
	}
	if probe.calls != 1 {
		t.Fatal(probe.calls)
	}
}

func TestASaveTheAgentRefusesForgetsTheRememberedCatalog(t *testing.T) {
	f, bot, probe, _ := catalogFixture(t)
	readCatalog(t, f, bot, "")
	probe.fail = &pa.Failure{Code: pa.Unsupported, Field: "model"}
	current := f.repo.bot
	settings := pa.RuntimeSettings{Model: "first"}
	_, _, err := f.s.Edit(context.Background(), f.command(pa.EditOperation), pa.EditBot{ExpectedVersion: current.Version, Name: current.Name, Appearance: current.Appearance, Instructions: current.Instructions.Text, RuntimeSettings: &settings})
	if pa.Code(err) != pa.Unsupported {
		t.Fatalf("save was not validated by the Agent: %v", err)
	}
	probe.fail = nil
	calls := probe.calls
	readCatalog(t, f, bot, "")
	if probe.calls != calls+1 {
		t.Fatal("the dialog kept offering a catalog the Agent had just refused")
	}
}

func TestTheRememberedCatalogsStayWithinTheirRegisteredCount(t *testing.T) {
	c := newConfigurationCache()
	at := time.Unix(1700000000, 0)
	for i := 0; i <= pa.MaxConfigurationCacheEntries; i++ {
		c.put(configurationKey{Agent: pa.Codex, Model: string(rune('a' + i))}, pa.ConfigurationCatalog{Agent: pa.Codex}, at.Add(time.Duration(i)*time.Second))
	}
	if len(c.entries) != pa.MaxConfigurationCacheEntries {
		t.Fatalf("cache holds %d entries", len(c.entries))
	}
	if _, ok := c.get(configurationKey{Agent: pa.Codex, Model: "a"}, at.Add(time.Minute)); ok {
		t.Fatal("the oldest catalog was not the one dropped")
	}
}
