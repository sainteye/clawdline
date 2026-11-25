package modelcatalog

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mc "github.com/sainteye/clawdline/internal/domain/modelcatalog"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) Add(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }

func nativeOpus(string) []mc.Model { return []mc.Model{{Value: "opus", Name: "Opus"}} }

func TestTheFirstReadAnswersNativeAndTheProbeThenReplacesIt(t *testing.T) {
	c := &clock{now: time.Unix(1000, 0)}
	release := make(chan struct{})
	s := New(Options{Clock: c.Now, Native: nativeOpus, Probe: func(ctx context.Context, agent string) (pa.ConfigurationCatalog, error) {
		<-release
		return configuration(pa.ClaudeCode, "default", pa.ConfigChoice{Value: "default", Name: "Default", Description: "Opus 5.5"}, pa.ConfigChoice{Value: "sonnet", Name: "Sonnet"}), nil
	}})
	first, err := s.Read("claude")
	if err != nil || first.Source != mc.Native || !first.Refreshing || len(first.Models) != 1 || first.Models[0].Value != "opus" {
		t.Fatalf("first read = %#v %v", first, err)
	}
	close(release)
	s.Wait()
	next, _ := s.Read("claude")
	if next.Source != mc.Agent || next.Refreshing || next.DefaultName != "Opus 5.5" || values(next.Models)[0] != "sonnet" {
		t.Fatalf("after the probe = %#v", next)
	}
}

func TestOneProbePerAgentAndOneAtATimeOnTheMachine(t *testing.T) {
	c := &clock{now: time.Unix(1000, 0)}
	var calls, running, most atomic.Int32
	release := make(chan struct{})
	s := New(Options{Clock: c.Now, Native: nativeOpus, Probe: func(ctx context.Context, agent string) (pa.ConfigurationCatalog, error) {
		calls.Add(1)
		n := running.Add(1)
		for {
			m := most.Load()
			if n <= m || most.CompareAndSwap(m, n) {
				break
			}
		}
		<-release
		running.Add(-1)
		return pa.ConfigurationCatalog{Agent: pa.Agent(agent)}, nil
	}})
	for range 5 {
		s.Read("claude")
		s.Read("codex")
	}
	close(release)
	s.Wait()
	if calls.Load() != 2 {
		t.Fatalf("probes = %d, want one per agent", calls.Load())
	}
	if most.Load() != 1 {
		t.Fatalf("%d probes ran at once", most.Load())
	}
}

func TestAnAgentAnswerIsServedUntilItsRefreshTime(t *testing.T) {
	c := &clock{now: time.Unix(1000, 0)}
	var calls atomic.Int32
	s := New(Options{Clock: c.Now, Native: nativeOpus, Probe: func(ctx context.Context, agent string) (pa.ConfigurationCatalog, error) {
		calls.Add(1)
		return configuration(pa.Codex, "x", pa.ConfigChoice{Value: "x", Name: "X"}), nil
	}})
	s.Read("codex")
	s.Wait()
	c.Add(time.Duration(mc.MaxRefreshSeconds-1) * time.Second)
	if got, _ := s.Read("codex"); got.Refreshing || calls.Load() != 1 {
		t.Fatalf("a fresh answer started another probe: %#v calls=%d", got, calls.Load())
	}
	c.Add(time.Second)
	got, _ := s.Read("codex")
	if !got.Refreshing || got.Source != mc.Agent {
		t.Fatalf("an expired answer is served while it refreshes: %#v", got)
	}
	s.Wait()
	if calls.Load() != 2 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestAFailedProbeKeepsTheLastGoodListAndSaysWhy(t *testing.T) {
	c := &clock{now: time.Unix(1000, 0)}
	var fail atomic.Bool
	var calls atomic.Int32
	s := New(Options{Clock: c.Now, Native: nativeOpus, Probe: func(ctx context.Context, agent string) (pa.ConfigurationCatalog, error) {
		calls.Add(1)
		if fail.Load() {
			return pa.ConfigurationCatalog{}, &pa.Failure{Code: pa.MemoryIsolationUnverified, Field: "codex_features"}
		}
		return configuration(pa.Codex, "model-sol", pa.ConfigChoice{Value: "model-sol", Name: "6.2 Sol"}), nil
	}})
	s.Read("codex")
	s.Wait()
	fail.Store(true)
	c.Add(time.Duration(mc.MaxRefreshSeconds) * time.Second)
	s.Read("codex")
	s.Wait()
	got, _ := s.Read("codex")
	if got.Source != mc.Agent || values(got.Models)[0] != "model-sol" || got.Problem != string(pa.MemoryIsolationUnverified) || got.ProblemField != "codex_features" {
		t.Fatalf("after a failure = %#v", got)
	}
	if got.Refreshing || calls.Load() != 2 {
		t.Fatalf("a failure is retried no sooner than the retry time: %#v calls=%d", got, calls.Load())
	}
	c.Add(time.Duration(mc.MaxRetrySeconds) * time.Second)
	fail.Store(false)
	s.Read("codex")
	s.Wait()
	if got, _ = s.Read("codex"); got.Problem != "" || calls.Load() != 3 {
		t.Fatalf("a later success clears the problem: %#v", got)
	}
}

func TestAFirstProbeFailureKeepsTheNativeList(t *testing.T) {
	c := &clock{now: time.Unix(1000, 0)}
	s := New(Options{Clock: c.Now, Native: nativeOpus, Probe: func(ctx context.Context, agent string) (pa.ConfigurationCatalog, error) {
		return pa.ConfigurationCatalog{}, &pa.Failure{Code: pa.Disabled, Field: "bots"}
	}})
	s.Read("claude")
	s.Wait()
	got, _ := s.Read("claude")
	if got.Source != mc.Native || len(got.Models) != 1 || got.Problem != string(pa.Disabled) || got.Refreshing {
		t.Fatalf("got %#v", got)
	}
}

func TestGrokWithoutANativeListAnswersEmptyAndRefreshing(t *testing.T) {
	s := New(Options{Native: func(agent string) []mc.Model { return nil }, Probe: func(ctx context.Context, agent string) (pa.ConfigurationCatalog, error) {
		return configuration(pa.Grok, "grok-4", pa.ConfigChoice{Value: "grok-4", Name: "Grok 4"}), nil
	}})
	first, _ := s.Read("grok")
	if first.Models == nil || len(first.Models) != 0 || !first.Refreshing {
		t.Fatalf("first = %#v", first)
	}
	s.Wait()
	if got, _ := s.Read("grok"); values(got.Models)[0] != "grok-4" || got.DefaultName != "Grok 4" {
		t.Fatalf("got %#v", got)
	}
}

func TestEffortsComeFromTheOneTableAndUnknownAgentsAreRefused(t *testing.T) {
	s := New(Options{})
	codex, _ := s.Read("codex")
	if len(codex.Efforts) != 2 || codex.Efforts[0] != "high" {
		t.Fatalf("codex efforts = %v", codex.Efforts)
	}
	if claude, _ := s.Read("claude"); claude.Efforts == nil || len(claude.Efforts) != 0 {
		t.Fatalf("claude efforts = %#v", claude.Efforts)
	}
	if _, err := s.Read("gpt"); err != ErrUnknownAgent {
		t.Fatalf("err = %v", err)
	}
}

func TestAProbeAnsweringForAnotherAgentIsAFailure(t *testing.T) {
	s := New(Options{Native: nativeOpus, Probe: func(ctx context.Context, agent string) (pa.ConfigurationCatalog, error) {
		return pa.ConfigurationCatalog{Agent: pa.Codex}, nil
	}})
	s.Read("claude")
	s.Wait()
	if got, _ := s.Read("claude"); got.Source != mc.Native || got.Problem != string(pa.ScopeDenied) {
		t.Fatalf("got %#v", got)
	}
}
