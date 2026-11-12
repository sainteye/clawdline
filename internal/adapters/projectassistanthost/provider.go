package projectassistanthost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"sync/atomic"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type lazyProvider struct {
	mu      sync.Mutex
	factory func() (app.Provider, error)
	p       app.Provider
	err     error
	tried   bool
}

func (p *lazyProvider) get() (app.Provider, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.tried {
		p.tried = true
		if p.factory == nil {
			p.err = failure(pa.AdapterNotFound, "native_provider")
		} else {
			p.p, p.err = p.factory()
		}
		if p.p == nil && p.err == nil {
			p.err = failure(pa.AdapterNotFound, "native_provider")
		}
	}
	return p.p, p.err
}
func (p *lazyProvider) Capabilities(ctx context.Context, b pa.Bot) (pa.Capabilities, error) {
	provider, e := p.get()
	if e != nil {
		return pa.Capabilities{}, e
	}
	return provider.Capabilities(ctx, b)
}
func (p *lazyProvider) Open(ctx context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	provider, e := p.get()
	if e != nil {
		return nil, e
	}
	return provider.Open(ctx, q, sink)
}

// NativeWork carries private functions only. There is no serializable actor,
// credential or arbitrary-Fence signing entry point in the composition callback.
type NativeWork struct {
	send     func(context.Context, string, string) error
	dispatch func(context.Context, string, string, []string) (pa.WorkLink, error)
	status   func(context.Context, string) (pa.WorkLink, error)
}

func (w NativeWork) SendMessage(ctx context.Context, id, text string) error {
	if w.send == nil {
		return failure(pa.Disabled, "native_work")
	}
	return w.send(ctx, id, text)
}
func (w NativeWork) Dispatch(ctx context.Context, id, brief string, claims []string) (pa.WorkLink, error) {
	if w.dispatch == nil {
		return pa.WorkLink{}, failure(pa.Disabled, "native_work")
	}
	return w.dispatch(ctx, id, brief, claims)
}
func (w NativeWork) Status(ctx context.Context, taskID string) (pa.WorkLink, error) {
	if w.status == nil {
		return pa.WorkLink{}, failure(pa.Disabled, "native_work")
	}
	return w.status(ctx, taskID)
}

type workLifetime struct {
	closed     atomic.Bool
	mu         sync.Mutex
	unregister func()
	stop       func() bool
	once       sync.Once
}

func (l *workLifetime) invalidate() {
	if l.closed.Swap(true) {
		return
	}
	l.mu.Lock()
	unregister, stop := l.unregister, l.stop
	l.unregister = nil
	l.stop = nil
	l.mu.Unlock()
	if stop != nil {
		stop()
	}
	if unregister != nil {
		l.once.Do(unregister)
	}
}
func (l *workLifetime) registered(unregister func()) {
	l.mu.Lock()
	closed := l.closed.Load()
	if !closed {
		l.unregister = unregister
	}
	l.mu.Unlock()
	if closed && unregister != nil {
		l.once.Do(unregister)
	}
}
func (l *workLifetime) watching(stop func() bool) {
	l.mu.Lock()
	closed := l.closed.Load()
	if !closed {
		l.stop = stop
	}
	l.mu.Unlock()
	if closed {
		stop()
	}
}

type ownedProvider struct {
	h *Host
	p app.Provider
}

func (p *ownedProvider) Capabilities(ctx context.Context, b pa.Bot) (pa.Capabilities, error) {
	return p.p.Capabilities(ctx, b)
}
func (p *ownedProvider) Open(ctx context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	if p.h.o.NativeWork == nil {
		return p.p.Open(ctx, q, sink)
	}
	life, tools, err := p.h.tools(ctx, q)
	if err != nil {
		return nil, err
	}
	unregister, err := p.h.o.NativeWork(ctx, q, tools)
	life.registered(unregister)
	if err == nil && unregister == nil {
		err = failure(pa.Invalid, "native_work_unregister")
	}
	if err != nil {
		life.invalidate()
		return nil, err
	}
	if life.closed.Load() || ctx.Err() != nil {
		life.invalidate()
		return nil, failure(pa.Revoked, "native_work_lifetime")
	}
	session, err := p.p.Open(ctx, q, sink)
	if err != nil {
		life.invalidate()
		if session != nil {
			return wrapSession(session, life), err
		}
		return nil, err
	}
	if session == nil {
		life.invalidate()
		return nil, failure(pa.ExecutionEffectUnknown, "missing_session")
	}
	return wrapSession(session, life), nil
}

type ownedSession struct {
	app.ProviderSession
	life *workLifetime
}

func (s *ownedSession) Close(ctx context.Context) error {
	s.life.invalidate()
	return s.ProviderSession.Close(ctx)
}

type measuredSession struct {
	*ownedSession
	usage func() app.SessionUsage
}

func (s *measuredSession) Usage() app.SessionUsage { return s.usage() }
func wrapSession(session app.ProviderSession, life *workLifetime) app.ProviderSession {
	owned := &ownedSession{ProviderSession: session, life: life}
	if measured, ok := session.(interface{ Usage() app.SessionUsage }); ok {
		return &measuredSession{ownedSession: owned, usage: measured.Usage}
	}
	return owned
}

func (h *Host) tools(openCtx context.Context, q app.OpenRequest) (*workLifetime, NativeWork, error) {
	deadline, ok := openCtx.Deadline()
	if !ok {
		return nil, NativeWork{}, failure(pa.Invalid, "native_work_deadline")
	}
	c, err := h.current(openCtx, q.Bot.Key)
	if err != nil {
		return nil, NativeWork{}, err
	}
	if c.Fence != q.Binding.Fence || c.ActiveTurnID == "" || c.BindingRevoked {
		return nil, NativeWork{}, failure(pa.StaleGeneration, "native_work_binding")
	}
	if err = c.Feature.CheckWrite(); err != nil {
		return nil, NativeWork{}, err
	}
	if err = pa.CheckSource(c.Source, true); err != nil {
		return nil, NativeWork{}, err
	}
	turnID := c.ActiveTurnID
	h.mu.Lock()
	r, a, b := h.engine, h.authority, h.bridge
	h.mu.Unlock()
	if r == nil || a == nil || b == nil {
		return nil, NativeWork{}, failure(pa.Disabled, "native_work")
	}
	// Signing occurs only from this actual provider Open. The authority may
	// refuse dispatch while chat remains usable under a missing work policy.
	var actor pa.AssistantExecutionActor
	if c.DispatchAllowed {
		actor, err = a.Issue(openCtx, c.Fence, []pa.ActorOperation{pa.DispatchWork, pa.ReadWork}, deadline)
		if err != nil {
			return nil, NativeWork{}, err
		}
	}
	life := &workLifetime{}
	life.watching(context.AfterFunc(openCtx, life.invalidate))
	bound := func(ctx context.Context) (context.Context, context.CancelFunc, error) {
		if life.closed.Load() || openCtx.Err() != nil {
			return nil, nil, failure(pa.Revoked, "native_work_lifetime")
		}
		ctx, cancel := context.WithDeadline(ctx, deadline)
		stop := context.AfterFunc(openCtx, cancel)
		finish := func() { stop(); cancel() }
		current, e := h.current(ctx, q.Bot.Key)
		if e == nil && (!current.Feature.Enabled || current.BindingRevoked) {
			e = failure(pa.Revoked, "native_work")
		}
		if e == nil {
			e = pa.CheckSource(current.Source, true)
		}
		if e == nil && (current.Fence != q.Binding.Fence || current.ActiveTurnID != turnID) {
			e = failure(pa.StaleGeneration, "native_work_turn")
		}
		if e == nil && (life.closed.Load() || openCtx.Err() != nil || ctx.Err() != nil) {
			e = failure(pa.Revoked, "native_work_lifetime")
		}
		if e != nil {
			finish()
			life.invalidate()
			return nil, nil, e
		}
		return ctx, finish, nil
	}
	tools := NativeWork{}
	tools.send = func(ctx context.Context, id, text string) error {
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return e
		}
		defer cancel()
		return r.Publish(ctx, app.ProtocolEvent{ID: id, Fence: q.Binding.Fence, TurnID: turnID, Message: text})
	}
	tools.dispatch = func(ctx context.Context, id, brief string, claims []string) (pa.WorkLink, error) {
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return pa.WorkLink{}, e
		}
		defer cancel()
		if actor.CapabilityID == "" {
			return pa.WorkLink{}, failure(pa.ScopeDenied, "dispatch_policy")
		}
		payload, _ := json.Marshal(struct {
			Fence       pa.Fence
			Turn, Brief string
			Claims      []string
		}{q.Binding.Fence, turnID, brief, claims})
		hash := sha256.Sum256(payload)
		command := pa.Command{Key: q.Bot.Key, Operation: pa.DispatchOperation, IdempotencyKey: id, ContentHash: hex.EncodeToString(hash[:])}
		return r.DispatchWork(ctx, pa.WorkRequest{Actor: actor, Command: command, TurnID: turnID, Brief: brief, Claims: append([]string(nil), claims...)})
	}
	tools.status = func(ctx context.Context, taskID string) (pa.WorkLink, error) {
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return pa.WorkLink{}, e
		}
		defer cancel()
		if actor.CapabilityID == "" {
			return pa.WorkLink{}, failure(pa.ScopeDenied, "dispatch_policy")
		}
		return b.Status(ctx, actor, taskID)
	}
	return life, tools, nil
}
