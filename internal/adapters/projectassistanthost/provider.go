package projectassistanthost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
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
	listBots   func(context.Context, pa.BotDirectoryQuery) (pa.BotDirectoryPage, error)
	messageBot func(context.Context, pa.BotMessageRequest) (pa.Exchange, error)
	routines   func(context.Context, app.RoutineRequest) (app.RoutineResult, error)
	send       func(context.Context, string, string) error
	dispatch   func(context.Context, string, string, []string) (pa.WorkLink, error)
	status     func(context.Context, string) (pa.WorkLink, error)
	memory     func(context.Context) ([]pa.MemoryEntry, error)
	edit       func(context.Context, string, app.MemoryEdit) error
	output     func(context.Context, string, app.OutputRequest) (pa.Output, error)
	artifacts  func(context.Context) ([]app.ArtifactInfo, error)

	browserStatus  func(context.Context) (app.BrowserStatus, error)
	browserCommand func(context.Context, app.BrowserCommand) (app.BrowserResult, error)
}

func (w NativeWork) Routines(ctx context.Context, q app.RoutineRequest) (app.RoutineResult, error) {
	if w.routines == nil {
		return app.RoutineResult{}, failure(pa.Disabled, "routine_tools")
	}
	return w.routines(ctx, q)
}

func (w NativeWork) PublishOutput(ctx context.Context, id string, q app.OutputRequest) (pa.Output, error) {
	if w.output == nil {
		return pa.Output{}, failure(pa.Disabled, "output_tools")
	}
	return w.output(ctx, id, q)
}
func (w NativeWork) ListArtifacts(ctx context.Context) ([]app.ArtifactInfo, error) {
	if w.artifacts == nil {
		return nil, failure(pa.Disabled, "output_tools")
	}
	return w.artifacts(ctx)
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
func (w NativeWork) Memory(ctx context.Context) ([]pa.MemoryEntry, error) {
	if w.memory == nil {
		return nil, failure(pa.Disabled, "native_memory")
	}
	return w.memory(ctx)
}
func (w NativeWork) EditMemory(ctx context.Context, id string, edit app.MemoryEdit) error {
	if w.edit == nil {
		return failure(pa.Disabled, "native_memory")
	}
	return w.edit(ctx, id, edit)
}

type workLifetime struct {
	closed     atomic.Bool
	used       atomic.Bool
	mu         sync.Mutex
	unregister func()
	stop       func() bool
	once       sync.Once
	// sealed is cancelled by invalidate, so a browser command still waiting
	// for its host is withdrawn when this execution ends.
	sealed context.Context
	seal   context.CancelFunc
}

func (l *workLifetime) invalidate() {
	if l.closed.Swap(true) {
		return
	}
	l.mu.Lock()
	unregister, stop := l.unregister, l.stop
	l.unregister = nil
	l.stop = nil
	seal := l.seal
	l.mu.Unlock()
	if seal != nil {
		seal()
	}
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
	life, tools, err := p.h.tools(ctx, q)
	if err != nil {
		return nil, &app.OpenNotStarted{Cause: err}
	}
	q.Tools = tools
	if p.h.o.NativeWork != nil {
		unregister, registerErr := p.h.o.NativeWork(ctx, q, tools)
		life.registered(unregister)
		err = registerErr
		if err == nil && unregister == nil {
			err = failure(pa.Invalid, "native_work_unregister")
		}
	}
	if err != nil {
		life.invalidate()
		return nil, &app.OpenNotStarted{Cause: err}
	}
	if life.closed.Load() || ctx.Err() != nil {
		life.invalidate()
		return nil, &app.OpenNotStarted{Cause: failure(pa.Revoked, "native_work_lifetime")}
	}
	session, err := p.p.Open(ctx, q, func(ctx context.Context, e app.ProtocolEvent) error {
		life.used.Store(true)
		if life.closed.Load() {
			return failure(pa.Revoked, "native_event_lifetime")
		}
		return sink(ctx, e)
	})
	if err != nil {
		// Seal first: a concurrent call either recorded used before this seal, or
		// sees closed and cannot reach an effect. Checking used before sealing races.
		life.invalidate()
		var refusal *app.PreSendRefusal
		if errors.As(err, &refusal) && life.used.Load() {
			err = errors.Join(failure(pa.ExecutionEffectUnknown, "open_effects"), err)
		}
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

// Steer forwards Send now to a native session that can take it.
func (s *ownedSession) Steer(ctx context.Context, text string) (app.SteerOutcome, error) {
	if steerer, ok := s.ProviderSession.(app.SteeringSession); ok {
		return steerer.Steer(ctx, text)
	}
	return "", failure(pa.Unsupported, "steering")
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
	turn, err := h.store.Turn(openCtx, q.Bot.Key, turnID)
	if err != nil {
		return nil, NativeWork{}, err
	}
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
	life.sealed, life.seal = context.WithCancel(context.Background())
	life.watching(context.AfterFunc(openCtx, life.invalidate))
	bound := func(ctx context.Context) (context.Context, context.CancelFunc, error) {
		life.used.Store(true)
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
	tools.listBots = func(ctx context.Context, request pa.BotDirectoryQuery) (pa.BotDirectoryPage, error) {
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return pa.BotDirectoryPage{}, e
		}
		defer cancel()
		return r.ListBots(ctx, q.Binding.Fence, turnID, request)
	}
	tools.messageBot = func(ctx context.Context, request pa.BotMessageRequest) (pa.Exchange, error) {
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return pa.Exchange{}, e
		}
		defer cancel()
		return r.MessageBot(ctx, q.Binding.Fence, turnID, request)
	}
	tools.routines = func(ctx context.Context, request app.RoutineRequest) (app.RoutineResult, error) {
		if turn.Origin == pa.BotRequestOrigin && request.Action != "list" {
			return app.RoutineResult{}, failure(pa.ScopeDenied, "bot_request_tools")
		}
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return app.RoutineResult{}, e
		}
		defer cancel()
		return r.Routine(ctx, q.Binding.Fence, turnID, request)
	}
	tools.output = func(ctx context.Context, id string, request app.OutputRequest) (pa.Output, error) {
		if turn.Origin == pa.BotRequestOrigin {
			return pa.Output{}, failure(pa.ScopeDenied, "bot_request_tools")
		}
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return pa.Output{}, e
		}
		defer cancel()
		if id == "" || len(id) > pa.MaxNameRunes || request.Path == "" {
			return pa.Output{}, failure(pa.Invalid, "output_request")
		}
		payload, _ := json.Marshal(struct {
			Request app.OutputRequest
			HTML    bool
		}{request, request.HTML})
		digest := sha256.Sum256(payload)
		requestHash := hex.EncodeToString(digest[:])
		identity := sha256.Sum256([]byte(turnID + "\x00" + id))
		outputID := hex.EncodeToString(identity[:])
		x, e := h.store.Export(ctx, q.Bot.Key)
		if e != nil {
			return pa.Output{}, e
		}
		for _, m := range x.Messages {
			if m.Output != nil && m.Output.ID == outputID {
				if m.Output.RequestHash != requestHash {
					return pa.Output{}, failure(pa.IdempotencyConflict, "output_request")
				}
				return *m.Output, nil
			}
		}
		name := request.Name
		if name == "" {
			name = filepath.Base(request.Path)
		}
		artifactID := ""
		if request.HTML {
			artifactID = request.Artifact
			name = strings.TrimSpace(request.Title)
			if artifactID != "" {
				found := false
				for _, o := range app.PublishedOutputs(x.Messages) {
					if o.ArtifactID == artifactID {
						found = true
						if name == "" {
							name = o.Name
						}
						break
					}
				}
				if !found {
					return pa.Output{}, failure(pa.NotFound, "artifact")
				}
			} else {
				artifactID = outputID
			}
		}
		if request.HTML && !pa.ArtifactTitle(name) || !request.HTML && !pa.OutputName(name) {
			return pa.Output{}, failure(pa.Invalid, "output_name")
		}
		check := func() error {
			c, done, err := bound(ctx)
			if err != nil {
				return err
			}
			defer done()
			return c.Err()
		}
		out, e := h.o.Outputs.Snapshot(ctx, q.Bot.Key, q.Directory, request.Path, request.HTML, check)
		if e != nil {
			return out, e
		}
		published := false
		defer func() {
			if !published {
				// A lost SQL commit acknowledgement is not proof of rollback.
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), h.o.Limits.CleanupTimeout)
				defer cancel()
				saved, err := h.store.Export(cleanup, q.Bot.Key)
				if err != nil {
					return
				}
				for _, m := range saved.Messages {
					if m.Output != nil && m.Output.SnapshotID == out.SnapshotID {
						return
					}
				}
				_ = h.o.Outputs.Discard(q.Bot.Key, out)
			}
		}()
		out.ID = outputID
		out.Name = name
		out.ArtifactID = artifactID
		out.RequestHash = requestHash
		if e = check(); e != nil {
			return pa.Output{}, e
		}
		e = r.Publish(ctx, app.ProtocolEvent{ID: "output-" + id, Fence: q.Binding.Fence, TurnID: turnID, Message: name, Output: &out})
		published = e == nil
		return out, e
	}
	tools.artifacts = func(ctx context.Context) ([]app.ArtifactInfo, error) {
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return nil, e
		}
		defer cancel()
		x, e := h.store.Export(ctx, q.Bot.Key)
		if e != nil {
			return nil, e
		}
		out := []app.ArtifactInfo{}
		for _, o := range app.PublishedOutputs(x.Messages) {
			if o.ArtifactID != "" {
				path, err := h.o.Outputs.Path(q.Bot.Key, o)
				if err != nil {
					return nil, err
				}
				out = append(out, app.ArtifactInfo{Output: o, Path: path})
			}
		}
		return out, nil
	}
	tools.memory = func(ctx context.Context) ([]pa.MemoryEntry, error) {
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return nil, e
		}
		defer cancel()
		return r.MemoryEntries(ctx, q.Bot.Key)
	}
	tools.edit = func(ctx context.Context, id string, edit app.MemoryEdit) error {
		if turn.Origin == pa.BotRequestOrigin || turn.Origin == pa.BotReplyOrigin {
			return failure(pa.ScopeDenied, "bot_reply_memory")
		}
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return e
		}
		defer cancel()
		_, e = r.EditMemory(ctx, q.Bot.Key, turnID, id, edit)
		return e
	}
	owner := BrowserOwner(q.Binding.Fence, q.Binding.ForgottenThrough)
	browser := h.o.Browser
	browserBound := func(ctx context.Context) (context.Context, context.CancelFunc, error) {
		if browser == nil || browser.Status == nil || browser.Command == nil {
			return nil, nil, failure(pa.Disabled, "browser_tools")
		}
		if e := browserScope(q.Binding, turn); e != nil {
			return nil, nil, e
		}
		ctx, done, e := bound(ctx)
		if e != nil {
			return nil, nil, e
		}
		stop := context.AfterFunc(life.sealed, done)
		return ctx, func() { stop(); done() }, nil
	}
	tools.browserStatus = func(ctx context.Context) (app.BrowserStatus, error) {
		ctx, cancel, e := browserBound(ctx)
		if e != nil {
			return app.BrowserStatus{}, e
		}
		defer cancel()
		return browser.Status(ctx, owner)
	}
	tools.browserCommand = func(ctx context.Context, command app.BrowserCommand) (app.BrowserResult, error) {
		ctx, cancel, e := browserBound(ctx)
		if e != nil {
			return app.BrowserResult{}, e
		}
		defer cancel()
		// A Bot hands a page to the person by keeping its tab: release would
		// drop the Bot as owner, and the person could no longer reach the tab
		// from the Bot's chat. Close still ends a tab the Bot is done with.
		if command.Op == "release" {
			return app.BrowserResult{Value: botTabKept}, nil
		}
		return browser.Command(ctx, owner, command)
	}
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

// botTabKept answers a Bot's release: the tab stays the Bot's, and the person
// takes it over from the Bot's chat.
var botTabKept = json.RawMessage(`{"kept":true,"note":"The tab stays yours so the person can take it over from your chat's browser and hand it back. Tell them what to do there; use close when you no longer need the tab."}`)
