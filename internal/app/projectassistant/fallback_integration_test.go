package projectassistant

import (
	"context"
	"testing"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type fallbackRestReader struct {
	Repository
	now time.Time
	all bool
}

func (s fallbackRestReader) AgentRest(_ context.Context, machine string, agent pa.Agent) (*pa.AgentRest, error) {
	if s.all || agent == pa.ClaudeCode {
		return pa.RestAfter(machine, agent, "usage_limit", s.now, nil), nil
	}
	return nil, nil
}

func TestFallbackRestSelectsVisibleTurnsWithoutSubstitutingRoutines(t *testing.T) {
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	r := Engine{o: RuntimeOptions{Repository: fallbackRestReader{now: now}, Clock: func() time.Time { return now }}}
	bot := pa.Bot{Key: pa.BotKey{MachineID: "machine", BotID: "bot"}, Agent: pa.ClaudeCode, FallbackAgents: pa.FallbackConfigOf([]pa.Agent{pa.Codex})}
	agent, err := r.selectAgent(context.Background(), bot, pa.Turn{})
	if err != nil || agent != pa.Codex {
		t.Fatalf("visible input did not respect rest: %s %v", agent, err)
	}
	agent, err = r.selectAgent(context.Background(), bot, pa.Turn{RoutineRunID: "routine"})
	if err != nil || agent != pa.ClaudeCode {
		t.Fatalf("routine silently changed provider: %s %v", agent, err)
	}
}

func TestFallbackRestsNeverPreventANewPrimaryAttempt(t *testing.T) {
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	r := Engine{o: RuntimeOptions{Repository: fallbackRestReader{now: now, all: true}, Clock: func() time.Time { return now }}}
	for _, fallbacks := range [][]pa.Agent{nil, {pa.Codex}} {
		bot := pa.Bot{Key: pa.BotKey{MachineID: "machine", BotID: "bot"}, Agent: pa.ClaudeCode, FallbackAgents: pa.FallbackConfigOf(fallbacks)}
		agent, err := r.selectAgent(context.Background(), bot, pa.Turn{})
		if err != nil || agent != pa.ClaudeCode {
			t.Fatalf("new input blocked by shared rest (%v): %s %v", fallbacks, agent, err)
		}
		tried := pa.Turn{Attempts: []pa.AttemptFact{{Agent: pa.ClaudeCode}}}
		if agent, err = r.selectAgent(context.Background(), bot, tried); err == nil {
			t.Fatalf("an already attempted primary was retried: %s", agent)
		}
	}
}

type revokedResumeProvider struct {
	*runtimeProvider
	revoked func()
}

func (p revokedResumeProvider) Open(ctx context.Context, q OpenRequest, sink func(context.Context, ProtocolEvent) error) (ProviderSession, error) {
	session, err := p.runtimeProvider.Open(ctx, q, sink)
	if pa.Code(err) == pa.ResumeUnsupported {
		p.revoked()
	}
	return session, err
}
func TestResumeRefusalRechecksAuthorityWithoutLeakingOpeningUsage(t *testing.T) {
	f := newRuntimeFixture(t)
	f.r.o.Provider = revokedResumeProvider{runtimeProvider: f.provider, revoked: func() { f.revoked.Store(true) }}
	first, err := f.submit(t, "one", "one")
	if err != nil {
		t.Fatal(err)
	}
	receivePrompt(t, f.provider)
	f.provider.session().reply <- PromptResult{FinalText: "done"}
	awaitTurn(t, f, first.ResourceID, pa.TurnSucceeded)
	f.provider.mu.Lock()
	f.provider.refuseResume = true
	f.provider.mu.Unlock()
	second, err := f.submit(t, "two", "two")
	if err != nil {
		t.Fatal(err)
	}
	awaitTurn(t, f, second.ResourceID, pa.TurnUnknown)
	eventually(t, func() bool { u := f.r.Usage(); return u.Known && u.Processes == 0 })
	if f.provider.count() != 2 {
		t.Fatalf("replacement Open bypassed revoked authority: %d", f.provider.count())
	}
	select {
	case <-f.provider.sent:
		t.Fatal("revoked replacement sent a prompt")
	default:
	}
}

func TestAnUnqualifiedAlternateModeDoesNotBlockNewPrimaryInput(t *testing.T) {
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	r := Engine{o: RuntimeOptions{Repository: fallbackRestReader{now: now}, Clock: func() time.Time { return now }}}
	bot := pa.Bot{Key: pa.BotKey{MachineID: "machine", BotID: "bot"}, Agent: pa.ClaudeCode, Mode: "plan", FallbackAgents: pa.FallbackConfigOf([]pa.Agent{pa.Codex})}
	agent, err := r.selectAgent(context.Background(), bot, pa.Turn{})
	if err != nil || agent != pa.ClaudeCode {
		t.Fatalf("new primary input was blocked by an unqualified alternate mode: %s %v", agent, err)
	}
	agent, err = r.selectAgent(context.Background(), bot, pa.Turn{Attempts: []pa.AttemptFact{{Agent: pa.ClaudeCode}}})
	if pa.Code(err) != pa.Unsupported || agent != "" {
		t.Fatalf("existing turn relaxed its alternate mode: %s %v", agent, err)
	}
}
