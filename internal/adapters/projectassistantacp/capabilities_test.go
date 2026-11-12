package projectassistantacp

import (
	"context"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestProviderNeverAutoInstallsOrAssumesNativeSafety(t *testing.T) {
	bot := pa.Bot{Key: testFence().Key, Project: testFence().Project, Agent: pa.Codex}
	p, err := NewProvider(ProviderOptions{Limits: testLimits()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Capabilities(context.Background(), bot); pa.Code(err) != pa.AdapterNotFound {
		t.Fatal(err)
	}
	p, err = NewProvider(ProviderOptions{Limits: testLimits(), Configurations: map[pa.Agent]Configuration{pa.Codex: peerConfiguration(t, "")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Capabilities(context.Background(), bot); pa.Code(err) != pa.ReadOnlyUnverified {
		t.Fatal(err)
	}
	p.o.Verify = func(context.Context, pa.Bot, Configuration) (pa.Capabilities, error) {
		return pa.Capabilities{Key: bot.Key, Project: bot.Project, Agent: bot.Agent, CLIVersion: "scripted", AdapterVersion: "scripted", Evidence: "scripted fixture", ReadOnlyEnforced: true, NativeMemory: pa.NativeMemoryUnknown}, nil
	}
	if _, err = p.Capabilities(context.Background(), bot); pa.Code(err) != pa.MemoryIsolationUnverified {
		t.Fatal(err)
	}
}

func TestProviderRequiresTrustedAbsoluteExecutable(t *testing.T) {
	if _, err := NewProvider(ProviderOptions{Limits: testLimits(), Configurations: map[pa.Agent]Configuration{pa.Codex: {Executable: "npx"}}}); pa.Code(err) != pa.Invalid {
		t.Fatal(err)
	}
}
