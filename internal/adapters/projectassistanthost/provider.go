package projectassistanthost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	network "github.com/sainteye/clawdline/internal/app/botnetwork"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
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
	react         func(context.Context, string, string) (pa.Receipt, error)
	network       func(context.Context, app.NetworkToolRequest) (network.Response, error)
	extension     func(context.Context, pa.ExtensionRef) error
	teamAvailable func(context.Context) error
	team          func(context.Context, string, json.RawMessage) (any, error)
	listBots      func(context.Context, pa.BotDirectoryQuery) (pa.BotDirectoryPage, error)
	messageBot    func(context.Context, pa.BotMessageRequest) (pa.Exchange, error)
	routines      func(context.Context, app.RoutineRequest) (app.RoutineResult, error)
	send          func(context.Context, string, string) error
	dispatch      func(context.Context, string, string, []string) (pa.WorkLink, error)
	status        func(context.Context, string) (pa.WorkLink, error)
	memory        func(context.Context) ([]pa.MemoryEntry, error)
	memoryDetail  func(context.Context, string) (pa.MemoryDetail, error)
	edit          func(context.Context, string, app.MemoryEdit) error
	output        func(context.Context, string, app.OutputRequest) (pa.Output, error)
	artifacts     func(context.Context) ([]app.ArtifactInfo, error)

	browserStatus  func(context.Context) (app.BrowserStatus, error)
	browserCommand func(context.Context, app.BrowserCommand) (app.BrowserResult, error)
}

func (w NativeWork) React(ctx context.Context, emoji, to string) (pa.Receipt, error) {
	if w.react == nil {
		return pa.Receipt{}, failure(pa.Unsupported, "reactions")
	}
	return w.react(ctx, emoji, to)
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
func (w NativeWork) MemoryDetail(ctx context.Context, id string) (pa.MemoryDetail, error) {
	if w.memoryDetail == nil {
		return pa.MemoryDetail{}, failure(pa.Disabled, "native_memory")
	}
	return w.memoryDetail(ctx, id)
}
func (w NativeWork) EditMemory(ctx context.Context, id string, edit app.MemoryEdit) error {
	if w.edit == nil {
		return failure(pa.Disabled, "native_memory")
	}
	return w.edit(ctx, id, edit)
}

type sendWriteContextKey struct{}

type workLifetime struct {
	extensions func(context.Context) error
	closed     atomic.Bool
	sending    atomic.Bool
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

// Only the context of the bounded Send write is non-reentrant. Independent
// native consumers may wait for the memory lock without blocking Send.
func (l *workLifetime) insideSend(ctx context.Context) bool {
	return l.sending.Load() && ctx.Value(sendWriteContextKey{}) == l
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
	if len(q.Bot.Extensions.Skills)+len(q.Bot.Extensions.Connectors) > 0 {
		if p.h.o.Extensions == nil {
			life.invalidate()
			return nil, &app.OpenNotStarted{Cause: failure(pa.Unsupported, "bot_extensions")}
		}
		q.Extensions, err = p.h.capturedExtensions(ctx, q)
		if err != nil {
			life.invalidate()
			return nil, &app.OpenNotStarted{Cause: err}
		}
		life.extensions = func(ctx context.Context) error {
			if _, e := p.h.capturedExtensions(ctx, q); e != nil {
				return e
			}
			for _, connector := range q.Extensions.Connectors {
				if connector.Credential != nil {
					value, e := connector.Credential(ctx)
					for i := range value {
						value[i] = 0
					}
					if e != nil {
						return e
					}
				}
			}
			return nil
		}
	}
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
		if life.insideSend(ctx) {
			return failure(pa.ExecutionEffectUnknown, "native_event_during_send")
		}
		if life.extensions != nil {
			if err := life.extensions(ctx); err != nil {
				return err
			}
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

// Send is a bounded write inside the engine memory lock. A provider must defer
// tool callbacks to its independent consumer/Wait, never await a journal here.
func (s *ownedSession) Send(ctx context.Context, q app.PromptRequest) (app.PendingPrompt, error) {
	if s.life.extensions != nil {
		if e := s.life.extensions(ctx); e != nil {
			return nil, e
		}
	}
	s.life.sending.Store(true)
	defer s.life.sending.Store(false)
	return s.ProviderSession.Send(context.WithValue(ctx, sendWriteContextKey{}, s.life), q)
}

// Steer forwards Send now to a native session that can take it.
func (s *ownedSession) Steer(ctx context.Context, text string) (app.SteerOutcome, error) {
	if s.life.extensions != nil {
		if e := s.life.extensions(ctx); e != nil {
			return "", e
		}
	}
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
		if life.insideSend(ctx) {
			return nil, nil, failure(pa.ExecutionEffectUnknown, "native_tool_during_send")
		}
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
	// Each actual Host callback owns one invocation identity. IDs are independent
	// of model titles/content and stay fixed across the journal's commit attempt.
	var invocation atomic.Uint64
	journal := func(ctx context.Context, tool string) error {
		id := "host-" + q.Binding.Fence.BindingID + "-" + tool + "-" + strconv.FormatUint(invocation.Add(1), 10)
		if life.closed.Load() || life.insideSend(ctx) || ctx.Err() != nil {
			return failure(pa.Revoked, "native_work_journal_lifetime")
		}
		return r.RecordToolActivity(ctx, q.Binding.Fence, turnID, id)
	}
	tools := NativeWork{}
	tools.react = func(ctx context.Context, emoji, to string) (pa.Receipt, error) {
		if (turn.Origin != pa.UserOrigin && turn.Origin != "") || turn.Room != nil || turn.Network != nil || to != "" {
			return pa.Receipt{}, failure(pa.ScopeDenied, "reaction_origin")
		}
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return pa.Receipt{}, e
		}
		defer cancel()
		if _, e = pa.ReactionEmoji(emoji); e != nil {
			return pa.Receipt{}, e
		}
		if e = journal(ctx, "react"); e != nil {
			return pa.Receipt{}, e
		}
		// Source policy and binding are checked again after the pre-effect journal.
		_, done, e := bound(ctx)
		if e != nil {
			return pa.Receipt{}, e
		}
		done()
		id := "native-react-" + turn.ID + "-" + strconv.FormatUint(invocation.Add(1), 10)
		hash := sha256.Sum256([]byte(emoji))
		result, e := h.store.React(ctx, app.ReactionWrite{ReactionRequest: app.ReactionRequest{Command: pa.Command{Key: q.Bot.Key, Operation: pa.ReactOperation, IdempotencyKey: id, ContentHash: hex.EncodeToString(hash[:])}, Fence: q.Binding.Fence, MessageID: turn.MessageID, Emoji: emoji, Present: true}, Actor: pa.ReactionActor{ID: "bot-" + q.Bot.Key.MachineID + "-" + q.Bot.Key.BotID, Kind: "bot", Bot: &q.Bot.Key, Name: q.Bot.Name}, NativeTurnID: turn.ID, NativeBot: &q.Bot, ReceiptID: h.o.ID(), EventID: h.o.ID()})
		if e != nil {
			return pa.Receipt{}, e
		}
		_, done, e = bound(ctx)
		if e != nil {
			return pa.Receipt{}, e
		}
		done()
		return result, nil
	}

	tools.network = func(ctx context.Context, request app.NetworkToolRequest) (network.Response, error) {
		if pa.IsPeerRequest(turn.Origin) || turn.Origin == pa.BotReplyOrigin {
			return network.Response{}, failure(pa.ScopeDenied, "network_peer_tools")
		}
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return network.Response{}, e
		}
		defer cancel()
		if h.o.Network == nil || h.o.NetworkTransport == nil {
			return network.Response{}, failure(pa.Unsupported, "network_unconfigured")
		}
		if e = journal(ctx, "networkBot"); e != nil {
			return network.Response{}, e
		}
		source := wire.BotEndpoint{AccountID: h.o.Network.AccountID(), MachineID: q.Bot.Key.MachineID, ProjectID: q.Bot.Project.ProjectID, BotID: q.Bot.Key.BotID, ConversationID: q.Binding.Fence.ConversationID, Generation: q.Binding.Fence.Generation}
		if request.Kind == "routes" {
			return h.o.Network.Routes(ctx, source, h.o.NetworkTransport)
		}
		transport, e := h.o.NetworkTransport(ctx, request.AuthorityID)
		if e != nil {
			return network.Response{}, e
		}
		result, e := h.o.Network.Call(ctx, network.Call{RequestID: request.RequestID, CorrelationID: request.CorrelationID, Kind: request.Kind, AuthorityID: request.AuthorityID, Source: source, Target: request.Target, Text: request.Text}, transport)
		if e != nil {
			return network.Response{}, e
		}
		if _, finish, e := bound(ctx); e != nil {
			return network.Response{}, e
		} else {
			finish()
		}
		return result, nil
	}
	tools.extension = func(ctx context.Context, ref pa.ExtensionRef) error {
		ctx, done, e := bound(ctx)
		if e != nil {
			return e
		}
		defer done()
		if h.o.Extensions == nil {
			return failure(pa.Unsupported, "bot_extensions")
		}
		selected := false
		for _, v := range q.Bot.Extensions.Connectors {
			if v == ref {
				selected = true
			}
		}
		if !selected {
			return failure(pa.ScopeDenied, "selected_connector")
		}
		if e = journal(ctx, "connector"); e != nil {
			return e
		}
		_, e = h.capturedExtensions(ctx, q)
		return e
	}
	tools.teamAvailable = func(ctx context.Context) error {
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return e
		}
		defer cancel()
		if e = journal(ctx, "teamAvailable"); e != nil {
			return e
		}
		_, e = r.ManagerContext(ctx, q.Binding.Fence, turnID)
		return e
	}
	tools.team = func(ctx context.Context, action string, args json.RawMessage) (any, error) {
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return nil, e
		}
		defer cancel()
		if e = journal(ctx, "team"); e != nil {
			return nil, e
		}
		h.mu.Lock()
		team := h.team
		h.mu.Unlock()
		if team == nil {
			return nil, failure(pa.Unsupported, "manage_team")
		}
		return team.Call(ctx, q.Binding.Fence, turnID, action, args)
	}
	tools.listBots = func(ctx context.Context, request pa.BotDirectoryQuery) (pa.BotDirectoryPage, error) {
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return pa.BotDirectoryPage{}, e
		}
		defer cancel()
		if e = journal(ctx, "listBots"); e != nil {
			return pa.BotDirectoryPage{}, e
		}
		return r.ListBots(ctx, q.Binding.Fence, turnID, request)
	}
	tools.messageBot = func(ctx context.Context, request pa.BotMessageRequest) (pa.Exchange, error) {
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return pa.Exchange{}, e
		}
		defer cancel()
		if e = journal(ctx, "messageBot"); e != nil {
			return pa.Exchange{}, e
		}
		return r.MessageBot(ctx, q.Binding.Fence, turnID, request)
	}
	tools.routines = func(ctx context.Context, request app.RoutineRequest) (app.RoutineResult, error) {
		if pa.IsPeerRequest(turn.Origin) && request.Action != "list" {
			return app.RoutineResult{}, failure(pa.ScopeDenied, "bot_request_tools")
		}
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return app.RoutineResult{}, e
		}
		defer cancel()
		if e = journal(ctx, "routines"); e != nil {
			return app.RoutineResult{}, e
		}
		return r.Routine(ctx, q.Binding.Fence, turnID, request)
	}
	tools.output = func(ctx context.Context, id string, request app.OutputRequest) (pa.Output, error) {
		if pa.IsPeerRequest(turn.Origin) {
			return pa.Output{}, failure(pa.ScopeDenied, "bot_request_tools")
		}
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return pa.Output{}, e
		}
		defer cancel()
		if e = journal(ctx, "output"); e != nil {
			return pa.Output{}, e
		}
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
				for _, o := range app.PublishedOutputs(x.DisplayMessages()) {
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
		if e = journal(ctx, "artifacts"); e != nil {
			return nil, e
		}
		x, e := h.store.Export(ctx, q.Bot.Key)
		if e != nil {
			return nil, e
		}
		out := []app.ArtifactInfo{}
		for _, o := range app.PublishedOutputs(x.DisplayMessages()) {
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
		if e = journal(ctx, "memory"); e != nil {
			return nil, e
		}
		return r.MemoryEntries(ctx, q.Bot.Key)
	}
	tools.memoryDetail = func(ctx context.Context, id string) (pa.MemoryDetail, error) {
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return pa.MemoryDetail{}, e
		}
		defer cancel()
		if e = journal(ctx, "memory-detail"); e != nil {
			return pa.MemoryDetail{}, e
		}
		return r.MemoryDetail(ctx, q.Bot.Key, id)
	}
	tools.edit = func(ctx context.Context, id string, edit app.MemoryEdit) error {
		if pa.IsPeerRequest(turn.Origin) || turn.Origin == pa.BotReplyOrigin {
			return failure(pa.ScopeDenied, "bot_reply_memory")
		}
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return e
		}
		defer cancel()
		if e = journal(ctx, "edit"); e != nil {
			return e
		}
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
		if e = journal(ctx, "browserStatus"); e != nil {
			return app.BrowserStatus{}, e
		}
		return browser.Status(ctx, owner)
	}
	tools.browserCommand = func(ctx context.Context, command app.BrowserCommand) (app.BrowserResult, error) {
		ctx, cancel, e := browserBound(ctx)
		if e != nil {
			return app.BrowserResult{}, e
		}
		defer cancel()
		if e = journal(ctx, "browserCommand"); e != nil {
			return app.BrowserResult{}, e
		}
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
		if e = journal(ctx, "send"); e != nil {
			return e
		}
		return r.Publish(ctx, app.ProtocolEvent{ID: id, Fence: q.Binding.Fence, TurnID: turnID, Message: text})
	}
	tools.dispatch = func(ctx context.Context, id, brief string, claims []string) (pa.WorkLink, error) {
		if pa.IsPeerRequest(turn.Origin) {
			return pa.WorkLink{}, failure(pa.ScopeDenied, "peer_dispatch")
		}
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return pa.WorkLink{}, e
		}
		defer cancel()
		if actor.CapabilityID == "" {
			return pa.WorkLink{}, failure(pa.ScopeDenied, "dispatch_policy")
		}
		if e = journal(ctx, "dispatch"); e != nil {
			return pa.WorkLink{}, e
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
		if e = journal(ctx, "status"); e != nil {
			return pa.WorkLink{}, e
		}
		return b.Status(ctx, actor, taskID)
	}
	return life, tools, nil
}

// botTabKept answers a Bot's release: the tab stays the Bot's, and the person
// takes it over from the Bot's chat.
var botTabKept = json.RawMessage(`{"kept":true,"note":"The tab stays yours so the person can take it over from your chat's browser and hand it back. Tell them what to do there; use close when you no longer need the tab."}`)

func (w NativeWork) TeamAvailable(ctx context.Context) error {
	if w.teamAvailable == nil {
		return failure(pa.Disabled, "manage_team")
	}
	return w.teamAvailable(ctx)
}
func (w NativeWork) Team(ctx context.Context, action string, args json.RawMessage) (any, error) {
	if w.team == nil {
		return nil, failure(pa.Disabled, "manage_team")
	}
	return w.team(ctx, action, args)
}

func (w NativeWork) CheckExtension(ctx context.Context, ref pa.ExtensionRef) error {
	if w.extension == nil {
		return failure(pa.Unsupported, "bot_extensions")
	}
	return w.extension(ctx, ref)
}

// Narrowing a live selection revokes its application-managed capabilities now.
// Additions remain a next-generation change and never enter an old session.
func (h *Host) capturedExtensions(ctx context.Context, q app.OpenRequest) (app.NativeExtensions, error) {
	if h.o.Extensions == nil {
		return app.NativeExtensions{}, failure(pa.Unsupported, "bot_extensions")
	}
	current, e := h.store.Bot(ctx, q.Bot.Key)
	if e != nil {
		return app.NativeExtensions{}, e
	}
	primary := q.PrimaryAgent
	if primary == "" {
		primary = q.Bot.Agent
	}
	if current.Project != q.Bot.Project || current.Agent != primary {
		return app.NativeExtensions{}, failure(pa.Revoked, "extension_scope")
	}
	for index, refs := range [][]pa.ExtensionRef{q.Bot.Extensions.Skills, q.Bot.Extensions.Connectors} {
		now := current.Extensions.Skills
		if index == 1 {
			now = current.Extensions.Connectors
		}
		for _, ref := range refs {
			found := false
			for _, allowed := range now {
				if ref == allowed {
					found = true
					break
				}
			}
			if !found {
				return app.NativeExtensions{}, failure(pa.Revoked, "selected_extension")
			}
		}
	}
	return h.o.Extensions.Resolve(ctx, q.Bot, q.Directory)
}
