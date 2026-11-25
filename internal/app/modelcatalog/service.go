package modelcatalog

import (
	"context"
	"errors"
	"sync"
	"time"

	mc "github.com/sainteye/clawdline/internal/domain/modelcatalog"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Options are the two tiers and the clock. Native must be quick: it is read
// on the caller's request when nothing is cached. Probe is the agent's own
// answer and may take seconds; it only ever runs in the background.
type Options struct {
	// Native lists agent's models from its own files or CLI, without starting
	// an agent session. nil for an agent without such a source.
	Native func(agent string) []mc.Model
	// Probe asks the agent itself, through a disposable discovery session
	// that needs no Bot. nil when this machine has no agent tier.
	Probe func(ctx context.Context, agent string) (pa.ConfigurationCatalog, error)
	Clock func() time.Time
	// Context bounds every background discovery; cancelling it stops them.
	Context context.Context
}

type entry struct {
	catalog mc.Catalog
	// agentAt is when the agent tier last answered; zero while the list is
	// still the native one.
	agentAt  time.Time
	nativeAt time.Time
	failedAt time.Time
}

// Service is the catalog. It holds one entry per agent and runs at most one
// agent discovery at a time on the whole machine: each one starts a native
// process, and the person's Bots use the same accounts.
type Service struct {
	o       Options
	mu      sync.Mutex
	entries map[string]*entry
	// probing is held by the one running discovery.
	probing sync.Mutex
	wg      sync.WaitGroup
}

// New is inert: nothing is read until the first Read.
func New(o Options) *Service {
	if o.Clock == nil {
		o.Clock = time.Now
	}
	if o.Context == nil {
		o.Context = context.Background()
	}
	return &Service{o: o, entries: map[string]*entry{}}
}

// ErrUnknownAgent is a read naming an agent without a catalog.
var ErrUnknownAgent = errors.New("model_catalog_unknown_agent")

// Read answers agent's catalog at once. With nothing cached it reads the
// native tier; whenever the agent's own answer is missing or older than
// mc.MaxRefreshSeconds it starts one background discovery, and the answer
// says so with Refreshing. A failed discovery keeps the last good list and
// names why in Problem; another is started no sooner than mc.MaxRetrySeconds.
func (s *Service) Read(agent string) (mc.Catalog, error) {
	if !mc.KnownAgent(agent) {
		return mc.Catalog{}, ErrUnknownAgent
	}
	now := s.o.Clock()
	s.mu.Lock()
	e := s.entries[agent]
	needNative := e == nil || e.agentAt.IsZero() && (s.o.Probe == nil || e.catalog.Problem != "") && now.Sub(e.nativeAt) >= time.Duration(mc.MaxRetrySeconds)*time.Second
	s.mu.Unlock()
	if needNative {
		var models []mc.Model
		if s.o.Native != nil {
			models = s.o.Native(agent)
		}
		if models == nil {
			models = []mc.Model{}
		}
		s.mu.Lock()
		if e = s.entries[agent]; e == nil {
			e = &entry{catalog: mc.Catalog{Agent: agent, Source: mc.Native}}
			s.entries[agent] = e
		}
		if e.agentAt.IsZero() {
			e.catalog.Models, e.catalog.ObservedAt, e.nativeAt = models, now, now
		}
		s.mu.Unlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e = s.entries[agent]
	stale := e.agentAt.IsZero() || now.Sub(e.agentAt) >= time.Duration(mc.MaxRefreshSeconds)*time.Second
	waited := e.failedAt.IsZero() || now.Sub(e.failedAt) >= time.Duration(mc.MaxRetrySeconds)*time.Second
	if s.o.Probe != nil && stale && waited && !e.catalog.Refreshing && s.o.Context.Err() == nil {
		e.catalog.Refreshing = true
		s.wg.Add(1)
		go s.refresh(agent)
	}
	return clone(e.catalog), nil
}

// ReadAll is Read for every agent, in mc.Agents order.
func (s *Service) ReadAll() []mc.Catalog {
	out := make([]mc.Catalog, 0, len(mc.Agents))
	for _, agent := range mc.Agents {
		c, _ := s.Read(agent)
		out = append(out, c)
	}
	return out
}

func (s *Service) refresh(agent string) {
	defer s.wg.Done()
	s.probing.Lock()
	defer s.probing.Unlock()
	ctx, cancel := context.WithTimeout(s.o.Context, time.Duration(mc.MaxProbeSeconds)*time.Second)
	defer cancel()
	answer, err := s.o.Probe(ctx, agent)
	if err == nil && string(answer.Agent) != agent {
		err = &pa.Failure{Code: pa.ScopeDenied, Field: "configuration_agent"}
	}
	now := s.o.Clock()
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.entries[agent]
	e.catalog.Refreshing = false
	if err != nil {
		e.failedAt = now
		e.catalog.Problem, e.catalog.ProblemField = problem(err)
		return
	}
	models, name := FromConfiguration(answer)
	e.agentAt, e.failedAt = now, time.Time{}
	e.catalog.Models, e.catalog.DefaultName, e.catalog.Source, e.catalog.ObservedAt = models, name, mc.Agent, now
	e.catalog.Problem, e.catalog.ProblemField = "", ""
}

func problem(err error) (string, string) {
	var f *pa.Failure
	if errors.As(err, &f) {
		return string(f.Code), f.Field
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "model_catalog_timeout", ""
	}
	return string(pa.Code(err)), ""
}

// Wait returns when every started discovery has finished. Tests and shutdown use it.
func (s *Service) Wait() { s.wg.Wait() }

// Usage reports the agents with an entry and the age in seconds of the oldest
// agent answer still served, for the capacity register.
func (s *Service) Usage() (entries int, oldestSeconds int64, refreshing int) {
	now := s.o.Clock()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		if !e.agentAt.IsZero() {
			oldestSeconds = max(oldestSeconds, int64(now.Sub(e.agentAt)/time.Second))
		}
		if e.catalog.Refreshing {
			refreshing++
		}
	}
	return len(s.entries), oldestSeconds, refreshing
}

func clone(c mc.Catalog) mc.Catalog {
	c.Models = append([]mc.Model{}, c.Models...)
	c.Efforts = mc.Efforts(c.Agent)
	return c
}
