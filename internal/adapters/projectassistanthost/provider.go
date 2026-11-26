package projectassistanthost

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"

	network "github.com/sainteye/clawdline/internal/app/botnetwork"
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
	background      func(context.Context, string, string, string) (pa.BackgroundTask, error)
	serverAvailable func(context.Context) error
	servers         func(context.Context, app.DevServerRequest) (app.DevServerResult, error)
	serverPublish   func(context.Context, func() error) error
	// Source command handlers may use the existing card path. This function
	// is private and is never exposed as an approval tool to the model.
	approveSource    func(context.Context, pa.PermissionAction, []pa.PermissionOption) (pa.PermissionOption, error)
	react            func(context.Context, string, string) (pa.Receipt, error)
	network          func(context.Context, app.NetworkToolRequest) (network.Response, error)
	extension        func(context.Context, pa.ExtensionRef) error
	teamAvailable    func(context.Context) error
	team             func(context.Context, string, json.RawMessage) (any, error)
	listBots         func(context.Context, pa.BotDirectoryQuery) (pa.BotDirectoryPage, error)
	messageBot       func(context.Context, pa.BotMessageRequest) (pa.Exchange, error)
	askPerson        func(context.Context, pa.AskPersonRequest) (pa.PermissionOffer, error)
	listQuestions    func(context.Context) ([]pa.PendingQuestion, error)
	withdrawQuestion func(context.Context, pa.WithdrawQuestionRequest) (pa.PermissionOffer, error)
	botRequest       func(context.Context, string, json.RawMessage) (any, error)
	routines         func(context.Context, app.RoutineRequest) (app.RoutineResult, error)
	send             func(context.Context, string, string) error
	dispatch         func(context.Context, string, string, string, []string, string, string, string, bool) (pa.WorkLink, error)
	status           func(context.Context, string) (pa.WorkLink, error)
	workReceipt      func(context.Context, string) (app.WorkReceipt, error)
	memory           func(context.Context) ([]pa.MemoryEntry, error)
	memoryIndex      func(context.Context) (app.MemoryIndex, error)
	memoryDetail     func(context.Context, string) (pa.MemoryDetail, error)
	edit             func(context.Context, string, app.MemoryEdit) error
	// memoryCard is true in a turn answering another Bot: edit_memory there
	// proposes a memory card for the person instead of writing.
	memoryCard bool
	output     func(context.Context, string, app.OutputRequest) (pa.Output, error)
	artifacts  func(context.Context) ([]app.ArtifactInfo, error)

	messageWork func(context.Context, string, string, string) (pa.WorkMessageReceipt, error)

	leadBotAvailable func(context.Context) error
	manageBots       func(context.Context, app.LeadBotRequest) (json.RawMessage, error)

	browserStatus  func(context.Context) (app.BrowserStatus, error)
	browserCommand func(context.Context, app.BrowserCommand) (app.BrowserResult, error)
}

func (w NativeWork) DevServersAvailable(ctx context.Context) error {
	if w.serverAvailable == nil {
		return failure(pa.Unsupported, "server_tools")
	}
	return w.serverAvailable(ctx)
}

func (w NativeWork) PublishDevServers(ctx context.Context, publish func() error) error {
	if w.serverPublish == nil {
		return failure(pa.Unsupported, "server_publication")
	}
	return w.serverPublish(ctx, publish)
}
func (w NativeWork) DevServers(ctx context.Context, q app.DevServerRequest) (app.DevServerResult, error) {
	if w.servers == nil {
		return app.DevServerResult{}, failure(pa.Unsupported, "server_tools")
	}
	return w.servers(ctx, q)
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

func (w NativeWork) BackgroundAvailable() bool {
	return w.background != nil
}

func (w NativeWork) AddBackgroundWork(ctx context.Context, id, title, brief string) (pa.BackgroundTask, error) {
	if !w.BackgroundAvailable() {
		return pa.BackgroundTask{}, failure(pa.ScopeDenied, "background_origin")
	}
	return w.background(ctx, id, title, brief)
}

func (w NativeWork) SendMessage(ctx context.Context, id, text string) error {
	if w.send == nil {
		return failure(pa.Disabled, "native_work")
	}
	return w.send(ctx, id, text)
}
func (w NativeWork) Dispatch(ctx context.Context, id, title, brief string, claims []string) (pa.WorkLink, error) {
	return w.DispatchToProject(ctx, id, title, brief, claims, "")
}
func (w NativeWork) DispatchToProject(ctx context.Context, id, title, brief string, claims []string, project string) (pa.WorkLink, error) {
	return w.DispatchWithPersona(ctx, id, title, brief, claims, project, "")
}
func (w NativeWork) DispatchWithPersona(ctx context.Context, id, title, brief string, claims []string, project, persona string) (pa.WorkLink, error) {
	return w.DispatchWithBoard(ctx, id, title, brief, claims, project, persona, "")
}
func (w NativeWork) DispatchWithBoard(ctx context.Context, id, title, brief string, claims []string, project, persona, boardItemID string) (pa.WorkLink, error) {
	return w.dispatchWork(ctx, id, title, brief, claims, project, persona, boardItemID, false)
}
func (w NativeWork) DispatchWithCompletion(ctx context.Context, id, title, brief string, claims []string, project, persona, boardItemID string) (pa.WorkLink, error) {
	return w.dispatchWork(ctx, id, title, brief, claims, project, persona, boardItemID, true)
}
func (w NativeWork) dispatchWork(ctx context.Context, id, title, brief string, claims []string, project, persona, boardItemID string, continueOnCompletion bool) (pa.WorkLink, error) {
	if w.dispatch == nil {
		return pa.WorkLink{}, failure(pa.Disabled, "native_work")
	}
	return w.dispatch(ctx, id, title, brief, claims, project, persona, boardItemID, continueOnCompletion)
}
func (w NativeWork) Status(ctx context.Context, taskID string) (pa.WorkLink, error) {
	if w.status == nil {
		return pa.WorkLink{}, failure(pa.Disabled, "native_work")
	}
	return w.status(ctx, taskID)
}
func (w NativeWork) WorkReceipt(ctx context.Context, idempotencyKey string) (app.WorkReceipt, error) {
	if w.workReceipt == nil {
		return app.WorkReceipt{}, failure(pa.Disabled, "native_work")
	}
	return w.workReceipt(ctx, idempotencyKey)
}
func (w NativeWork) Memory(ctx context.Context) ([]pa.MemoryEntry, error) {
	if w.memory == nil {
		return nil, failure(pa.Disabled, "native_memory")
	}
	return w.memory(ctx)
}
func (w NativeWork) MemoryIndex(ctx context.Context) (app.MemoryIndex, error) {
	if w.memoryIndex == nil {
		return app.MemoryIndex{}, failure(pa.Disabled, "native_memory")
	}
	return w.memoryIndex(ctx)
}
func (w NativeWork) MemoryDetail(ctx context.Context, id string) (pa.MemoryDetail, error) {
	if w.memoryDetail == nil {
		return pa.MemoryDetail{}, failure(pa.Disabled, "native_memory")
	}
	return w.memoryDetail(ctx, id)
}

// MemoryNeedsPerson reports that a successful edit_memory in this turn
// created a memory card for the person rather than changing memory.
func (w NativeWork) MemoryNeedsPerson() bool { return w.memoryCard }

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

// NativeContext must cross the same sealed lifetime as native tools. Embedding
// ProviderSession alone hides this optional provider capability.
func (s *ownedSession) NativeContext() app.NativeContextState {
	if s.life.closed.Load() {
		return app.NativeContextState{}
	}
	reader, ok := s.ProviderSession.(app.NativeContextReader)
	if !ok {
		return app.NativeContextState{}
	}
	state := reader.NativeContext()
	if s.life.closed.Load() {
		return app.NativeContextState{}
	}
	return state
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

func (s *ownedSession) SupportsSteering(images bool) bool {
	steerer, ok := s.ProviderSession.(app.SteeringSession)
	return ok && !s.life.closed.Load() && steerer.SupportsSteering(images)
}

// Steer forwards Send now to a native session that can take it.
func (s *ownedSession) Steer(ctx context.Context, q app.PromptRequest) (app.SteerOutcome, error) {
	// Both refusals here come before the native session saw the text.
	if s.life.extensions != nil {
		if e := s.life.extensions(ctx); e != nil {
			return "", app.SteerRefused(e)
		}
	}
	if steerer, ok := s.ProviderSession.(app.SteeringSession); ok {
		return steerer.Steer(ctx, q)
	}
	return "", app.SteerRefused(failure(pa.Unsupported, app.SteeringField))
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

func (h *Host) prepareOutputEvent(ctx context.Context, ticket *pa.RoomTicket, event *app.ProtocolEvent, check func() error) error {
	if ticket != nil {
		picture, err := h.stageRoomOutputPicture(ctx, *ticket, *event.Output)
		if err != nil {
			return err
		}
		event.RoomPicture = picture
	}
	return check()
}

// stageRoomOutputPicture keeps the verified snapshot under the room's key so
// room history can still render it after the member or its own files are gone.
// A staged picture can only be downloaded once a committed room line names it.
func (h *Host) stageRoomOutputPicture(ctx context.Context, ticket pa.RoomTicket, out pa.Output) (*pa.Attachment, error) {
	pictures := roomPictures(h.o)
	if pictures == nil {
		return nil, nil
	}
	data, mediaType, err := h.o.Outputs.Picture(ctx, ticket.Member, out)
	if err != nil || data == nil {
		return nil, err
	}
	if err = h.rooms.Admit(ctx, ticket); err != nil {
		return nil, err
	}
	picture, err := pictures.Put(ticket.Group, mediaType, data)
	if err != nil {
		return nil, err
	}
	return &picture, nil
}

// botTabKept answers a Bot's release: the tab stays the Bot's, and the person
// takes it over from the Bot's chat.
var botTabKept = json.RawMessage(`{"kept":true,"note":"The tab stays yours so the person can take it over from your chat's browser and hand it back. Tell them what to do there; use close when you no longer need the tab."}`)

func (w NativeWork) LeadBotAvailable(ctx context.Context) error {
	if w.leadBotAvailable == nil {
		return failure(pa.ScopeDenied, "lead_user_turn")
	}
	return w.leadBotAvailable(ctx)
}

func (w NativeWork) ManageBots(ctx context.Context, request app.LeadBotRequest) (json.RawMessage, error) {
	if w.manageBots == nil {
		return nil, failure(pa.ScopeDenied, "lead_user_turn")
	}
	return w.manageBots(ctx, request)
}

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

func (s *ownedSession) UnconfirmedProcesses() []int {
	return app.UnconfirmedProcessesOf(s.ProviderSession)
}

// dispatchScope checks the request card scope of a dispatch_work call, then
// its mode: a Root is whole ownership handed to an independent Session, so
// only a turn the person started, or the holder of a person-rooted request
// card working on that card, may open one.
func dispatchScope(ctx context.Context, cards botRequestStore, bot string, turn pa.Turn, botRequestID string) error {
	cardScoped, err := botRequestDispatchScope(ctx, cards, nil, bot, turn, botRequestID)
	if err != nil {
		return err
	}
	if mode := pa.DispatchChoiceFrom(ctx).Mode; mode != "" && (mode != pa.DispatchModeRoot || !directUserTurn(turn) && !cardScoped) {
		return failure(pa.ScopeDenied, "dispatch_root_origin")
	}
	return nil
}

// peerMemoryEdit refuses what a turn answering another Bot may not change.
// Text from another Bot never becomes memory on a Bot's own say: a new short
// fact becomes a card the person saves with one press, any other change waits
// for the person's own chat, and a request from another machine's Bot keeps
// memory refused outright.
func peerMemoryEdit(origin pa.TurnOrigin, card bool, edit app.MemoryEdit) error {
	if pa.IsPeerRequest(origin) && !card {
		return failure(pa.ScopeDenied, "bot_reply_memory")
	}
	if card && (edit.Append == "" || edit.Old != "" || edit.New != "" || edit.Delete || edit.Note != nil) {
		return failure(pa.ScopeDenied, "bot_reply_memory_card_append_only")
	}
	return nil
}

// writeMemory changes the Bot's memory, or in a turn answering another Bot
// puts the new fact on a memory card for the person instead.
func writeMemory(ctx context.Context, r *app.Engine, card bool, q app.OpenRequest, turnID, id string, edit app.MemoryEdit) error {
	if card {
		_, err := r.ProposeMemory(ctx, q.Binding.Fence, turnID, edit.Append)
		return err
	}
	_, err := r.EditMemory(ctx, q.Bot.Key, turnID, id, edit)
	return err
}
