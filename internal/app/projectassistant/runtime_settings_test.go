package projectassistant

import (
	"context"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"strings"
	"testing"
)

func TestDisabledFeatureDoesNotProbeNativeConfiguration(t *testing.T) {
	f := newServiceFixture(t)
	bot := f.create(t)
	calls := 0
	f.s.opts.Configuration = func(context.Context, pa.Bot, string, string) (pa.ConfigurationCatalog, error) {
		calls++
		return pa.ConfigurationCatalog{}, nil
	}
	f.authority.Feature = pa.FeatureState{HasData: true}
	f.authority.Source.Access = pa.HistoryOnly
	f.authority.Source.Writable = false
	_, _, err := f.s.Configuration(context.Background(), MemoryReadRequest{Key: bot.Key, ConversationID: bot.ConversationID}, bot.Agent, "")
	if pa.Code(err) != pa.Disabled || calls != 0 {
		t.Fatal("disabled metadata created native resources", err, calls)
	}
}

func TestRuntimeAgentSettingsWaitForNextGeneration(t *testing.T) {
	f := newRuntimeFixture(t)
	firstReceipt, err := f.submit(t, "first-config", "first")
	if err != nil {
		t.Fatal(err)
	}
	first := receivePrompt(t, f.provider)
	running := f.provider.session()
	oldAgent := running.q.Bot.Agent
	f.store.mu.Lock()
	before := f.store.bot
	before.Name = "Synthetic settings Bot"
	before.Appearance = pa.Appearance{Shape: pa.Blob, Color: pa.EntryColor}
	nextAgent := pa.Codex
	if before.Agent == pa.Codex {
		nextAgent = pa.ClaudeCode
	}
	login := strings.Repeat("a", 32)
	settings := pa.RuntimeSettings{Model: "synthetic-next", Options: map[string]string{"effort": "high"}}
	f.store.bot, err = pa.ApplyEdit(before, pa.EditBot{ExpectedVersion: before.Version, Name: before.Name, Appearance: before.Appearance, Instructions: before.Instructions.Text, Agent: nextAgent, LoginProfile: &login, RuntimeSettings: &settings})
	f.store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if running.q.Bot.Agent != oldAgent || running.q.Bot.RuntimeSettings.Model != "" || running.q.Bot.LoginProfile != "" {
		t.Fatal("current execution changed in place")
	}
	running.reply <- PromptResult{FinalText: "old configuration finished"}
	awaitTurn(t, f, firstReceipt.ResourceID, pa.TurnSucceeded)
	nextReceipt, err := f.submit(t, "next-config", "next")
	if err != nil {
		t.Fatal(err)
	}
	next := receivePrompt(t, f.provider)
	session := f.provider.session()
	if session.q.Bot.Agent != nextAgent || session.q.Resume || session.q.Binding.NativeSessionID != "" || session.q.Bot.RuntimeSettings.Model != "synthetic-next" || session.q.Bot.LoginProfile != login || next.Turn.Fence.Generation <= first.Turn.Fence.Generation {
		t.Fatalf("next settings did not start a fresh generation: %+v", session.q)
	}
	session.reply <- PromptResult{}
	awaitTurn(t, f, nextReceipt.ResourceID, pa.TurnSucceeded)
}
