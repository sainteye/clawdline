package projectassistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type RuntimeOptions struct {
	Repository      Repository
	Memory          Memory
	WorkBridge      WorkBridge
	ProjectResolver ProjectResolver
	Provider        Provider
	Clock           func() time.Time
	ID              func() string
	// Authorize must read current source access, never a cached client claim.
	Authorize func(context.Context, pa.BotKey) (pa.FeatureState, pa.SourceStatus, error)
	Limits    RuntimeLimits
}

type Engine struct {
	o        RuntimeOptions
	mu       sync.Mutex
	actors   map[pa.BotKey]*runtimeActor
	slots    chan struct{}
	disabled map[pa.ProjectSelection]bool
}

type runtimeActor struct {
	mu      sync.Mutex
	key     pa.BotKey
	project pa.ProjectScope
	users   int // protected by Engine.mu; includes the worker reference
	working bool
	cancel  context.CancelFunc
	session ProviderSession
	turnID  string
	stopped bool
}

var _ Runtime = (*Engine)(nil)

func (r *Engine) Usage() RuntimeUsage {
	r.mu.Lock()
	usage := RuntimeUsage{ActiveActors: len(r.actors), Processes: len(r.slots), Known: true}
	var actors []*runtimeActor
	for _, a := range r.actors {
		a.users++
		actors = append(actors, a)
	}
	r.mu.Unlock()
	for _, a := range actors {
		if a.mu.TryLock() {
			if a.session != nil {
				if s, ok := a.session.(interface{ Usage() SessionUsage }); ok {
					usage.Sessions = append(usage.Sessions, s.Usage())
				} else {
					usage.Known = false
				}
			}
			a.mu.Unlock()
		} else {
			usage.Known = false
		}
		r.release(a)
	}
	return usage
}

func runtimeFailure(code pa.ErrorCode, field string) error {
	return &pa.Failure{Code: code, Field: field}
}

func NewRuntime(o RuntimeOptions) (*Engine, error) {
	if o.Repository == nil || o.Memory == nil || o.WorkBridge == nil || o.ProjectResolver == nil || o.Provider == nil || o.Clock == nil || o.ID == nil || o.Authorize == nil || o.Limits.MaxBots < 1 || o.Limits.MaxProcesses < 1 || o.Limits.PromptTimeout <= 0 || o.Limits.CleanupTimeout <= 0 {
		return nil, runtimeFailure(pa.Invalid, "runtime_options")
	}
	return &Engine{o: o, actors: make(map[pa.BotKey]*runtimeActor), slots: make(chan struct{}, o.Limits.MaxProcesses), disabled: make(map[pa.ProjectSelection]bool)}, nil
}

func (r *Engine) acquire(key pa.BotKey) (*runtimeActor, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.actors[key]
	if a == nil {
		if len(r.actors) >= r.o.Limits.MaxBots {
			return nil, runtimeFailure(pa.CapacityFull, "active_bots")
		}
		a = &runtimeActor{key: key}
		r.actors[key] = a
	}
	a.users++
	return a, nil
}
func (r *Engine) release(a *runtimeActor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a.users--
	if a.users == 0 {
		delete(r.actors, a.key)
	}
}
func (r *Engine) authorized(ctx context.Context, key pa.BotKey, project pa.ProjectScope) error {
	r.mu.Lock()
	disabled := r.disabled[pa.ProjectSelection{MachineID: project.MachineID, ProjectID: project.ProjectID}]
	r.mu.Unlock()
	if disabled {
		return runtimeFailure(pa.Disabled, "project")
	}
	f, s, err := r.o.Authorize(ctx, key)
	if err != nil {
		return err
	}
	if err = f.CheckWrite(); err != nil {
		return err
	}
	if s.MachineID != key.MachineID {
		return runtimeFailure(pa.ScopeDenied, "source")
	}
	return pa.CheckSource(s, true)
}
func currentFence(bot pa.Bot, c pa.Conversation) pa.Fence {
	return pa.Fence{Key: bot.Key, Project: bot.Project, ConversationID: c.ID, BindingID: c.BindingID, Generation: c.Generation}
}
func (r *Engine) replay(ctx context.Context, c pa.Command, op pa.Operation) (*pa.Receipt, error) {
	if c.Operation != op {
		return nil, runtimeFailure(pa.Invalid, "operation")
	}
	x, err := r.o.Repository.Receipt(ctx, c)
	if err != nil {
		return nil, err
	}
	_, err = pa.DecideReplay(c, x)
	return x, err
}
func (r *Engine) command(key pa.BotKey, op pa.Operation, id string, payload any) pa.Command {
	b, _ := json.Marshal(payload)
	h := sha256.Sum256(b)
	return pa.Command{Key: key, Operation: op, IdempotencyKey: id, ContentHash: hex.EncodeToString(h[:])}
}

// mutate increments the aggregate version once. Repository.Commit owns all
// atomic facts/receipt/event allocation; no external IO runs inside its tx.
func (r *Engine) mutate(ctx context.Context, bot pa.Bot, m Mutation) (pa.Receipt, error) {
	if len(m.Messages) > 0 {
		if m.Conversation == nil {
			c, err := r.o.Repository.Conversation(ctx, bot.Key, bot.ConversationID)
			if err != nil {
				return pa.Receipt{}, err
			}
			m.Conversation = &c
		}
		for i := range m.Messages {
			if m.Messages[i].Seq == 0 {
				m.Conversation.LastMessageSeq++
				m.Messages[i].Seq = m.Conversation.LastMessageSeq
			}
			for j := range m.Events {
				if m.Events[j].Message != nil && m.Events[j].Message.ID == m.Messages[i].ID {
					msg := m.Messages[i]
					m.Events[j].Message = &msg
				}
			}
		}
	}
	m.ExpectedVersion = bot.Version
	bot.Version++
	bot.UpdatedAt = r.o.Clock()
	m.Bot = &bot
	if m.Receipt.ID == "" {
		m.Receipt.ID = r.o.ID()
	}
	m.Receipt.Command = m.Command
	for i := range m.Events {
		m.Events[i].Version = pa.ContractVersion
		m.Events[i].ID = r.o.ID()
		m.Events[i].At = r.o.Clock()
	}
	return r.o.Repository.Commit(ctx, m)
}

func (r *Engine) Submit(ctx context.Context, s Submission) (pa.Receipt, error) {
	if err := s.Feature.CheckWrite(); err != nil {
		return pa.Receipt{}, err
	}
	if err := pa.CheckSource(s.Source, true); err != nil {
		return pa.Receipt{}, err
	}
	if s.Command.Key != s.Message.Fence.Key || s.Source.MachineID != s.Command.Key.MachineID {
		return pa.Receipt{}, runtimeFailure(pa.ScopeDenied, "submission")
	}
	if err := pa.ValidateMessage(s.Message.Text); err != nil {
		return pa.Receipt{}, err
	}
	a, err := r.acquire(s.Command.Key)
	if err != nil {
		return pa.Receipt{}, err
	}
	defer r.release(a)
	a.mu.Lock()
	defer a.mu.Unlock()
	bot, err := r.o.Repository.Bot(ctx, a.key)
	if err != nil {
		return pa.Receipt{}, err
	}
	if err = r.authorized(ctx, a.key, bot.Project); err != nil {
		return pa.Receipt{}, err
	}
	if receipt, e := r.replay(ctx, s.Command, pa.SendOperation); e != nil {
		return pa.Receipt{}, e
	} else if receipt != nil {
		return *receipt, nil
	}
	c, err := r.o.Repository.Conversation(ctx, a.key, bot.ConversationID)
	if err != nil {
		return pa.Receipt{}, err
	}
	if err = pa.CheckSendFence(s.Message.Fence, currentFence(bot, c)); err != nil {
		return pa.Receipt{}, err
	}
	if s.Message.QuoteMessageID != "" {
		export, e := r.o.Repository.Export(ctx, a.key)
		if e != nil {
			return pa.Receipt{}, e
		}
		memory, e := r.o.Memory.Current(ctx, a.key)
		if e != nil {
			return pa.Receipt{}, e
		}
		if _, e = resolvePromptQuote(export, s.Message.QuoteMessageID, currentFence(bot, c), memory); e != nil {
			return pa.Receipt{}, e
		}
	}
	pending, err := r.o.Repository.PendingTurns(ctx, a.key)
	if err != nil {
		return pa.Receipt{}, err
	}
	for _, t := range pending {
		if t.State == pa.TurnUnknown || (!a.working && (t.State == pa.TurnRunning || t.State == pa.TurnWaitingPermission)) {
			return pa.Receipt{}, runtimeFailure(pa.OutcomeUnknown, "inspect_interrupted_turn")
		}
	}
	if err = pa.CheckAdmission(len(pending)); err != nil {
		return pa.Receipt{}, err
	}
	// Capability admission is local evidence inspection, not a model request.
	caps, err := r.o.Provider.Capabilities(ctx, bot)
	if err != nil {
		return pa.Receipt{}, err
	}
	if err = pa.CheckExecutionCapabilities(caps, currentFence(bot, c)); err != nil {
		return pa.Receipt{}, err
	}
	var binding *pa.ExecutionBinding
	if c.BindingID == "" {
		memory, e := r.o.Memory.Current(ctx, a.key)
		if e != nil {
			return pa.Receipt{}, e
		}
		c.BindingID = r.o.ID()
		c.Generation = 1
		b := pa.ExecutionBinding{Fence: currentFence(bot, c), Agent: bot.Agent, MemoryRevision: memory.Revision, ForgottenThrough: memory.ForgottenThrough, InstructionRevision: bot.Instructions.Revision}
		binding = &b
	}
	fence := currentFence(bot, c)
	turn := pa.Turn{ID: r.o.ID(), Fence: fence, MessageID: r.o.ID(), State: pa.TurnQueued, IdempotencyKey: s.Command.IdempotencyKey}
	message := pa.Message{ID: turn.MessageID, Fence: fence, TurnID: turn.ID, Kind: pa.UserMessage, Text: s.Message.Text, QuoteMessageID: s.Message.QuoteMessageID, At: r.o.Clock()}
	receipt, err := r.mutate(ctx, bot, Mutation{Command: s.Command, Conversation: &c, Binding: binding, Turn: &turn, Messages: []pa.Message{message}, Receipt: pa.Receipt{State: pa.EffectAccepted, ResourceID: turn.ID}, Events: []pa.Event{{Fence: fence, Kind: pa.MessageCreated, Message: &message}, {Fence: fence, Kind: pa.TurnUpdated, Turn: &turn}}})
	if err != nil {
		return pa.Receipt{}, err
	}
	a.project = bot.Project
	if !a.working {
		a.working = true
		r.mu.Lock()
		a.users++
		r.mu.Unlock()
		go r.work(a)
	}
	return receipt, nil
}

// Recover is explicit host startup work. A possibly sent turn becomes unknown;
// only proven unsent queued turns can execute. It never repeats a prompt.
func (r *Engine) Recover(ctx context.Context, key pa.BotKey) error {
	a, err := r.acquire(key)
	if err != nil {
		return err
	}
	defer r.release(a)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.working {
		return nil
	}
	pending, err := r.o.Repository.PendingTurns(ctx, key)
	if err != nil {
		return err
	}
	for _, t := range pending {
		if t.State == pa.TurnRunning || t.State == pa.TurnWaitingPermission {
			if err = r.finishLocked(ctx, a, t, pa.TurnUnknown, "", runtimeFailure(pa.ExecutionEffectUnknown, "restart")); err != nil {
				return err
			}
		}
	}
	bot, err := r.o.Repository.Bot(ctx, key)
	if err != nil {
		return err
	}
	a.project = bot.Project
	a.working = true
	r.mu.Lock()
	a.users++
	r.mu.Unlock()
	go r.work(a)
	return nil
}

func (r *Engine) work(a *runtimeActor) {
	defer r.release(a)
	for {
		a.mu.Lock()
		pending, err := r.o.Repository.PendingTurns(context.Background(), a.key)
		if err != nil || len(pending) == 0 {
			a.working = false
			a.mu.Unlock()
			return
		}
		var turn *pa.Turn
		blocked := false
		for i := range pending {
			t := pending[i]
			if t.State == pa.TurnUnknown || t.State == pa.TurnRunning || t.State == pa.TurnWaitingPermission {
				blocked = true
				break
			}
			if turn == nil && (t.State == pa.TurnQueued || t.State == pa.TurnAccepted) {
				turn = &t
			}
		}
		if blocked || turn == nil {
			a.working = false
			a.mu.Unlock()
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), r.o.Limits.PromptTimeout)
		a.cancel = cancel
		a.turnID = turn.ID
		a.stopped = false
		a.mu.Unlock()
		// Waiting for a process slot remains cancellable and does not launch IO.
		select {
		case r.slots <- struct{}{}:
			err = r.execute(ctx, a, *turn)
			<-r.slots
		case <-ctx.Done():
			err = ctx.Err()
		}
		cancel()
		a.mu.Lock()
		a.cancel = nil
		a.session = nil
		a.turnID = ""
		if err != nil {
			latest, e := r.o.Repository.Turn(context.Background(), a.key, turn.ID)
			if e == nil && (latest.State == pa.TurnRunning || latest.State == pa.TurnWaitingPermission || latest.State == pa.TurnQueued) {
				state := pa.TurnUnknown
				if latest.State == pa.TurnQueued {
					state = pa.TurnFailed
				}
				if e = r.finishLocked(context.Background(), a, latest, state, "", err); e != nil {
					a.working = false
					a.mu.Unlock()
					return
				}
			}
		}
		a.mu.Unlock()
	}
}

func (r *Engine) execute(ctx context.Context, a *runtimeActor, t pa.Turn) error {
	a.mu.Lock()
	if a.stopped || ctx.Err() != nil {
		a.mu.Unlock()
		return runtimeFailure(pa.ExecutionEffectUnknown, "cancelled_before_start")
	}
	bot, err := r.o.Repository.Bot(ctx, a.key)
	if err != nil {
		a.mu.Unlock()
		return err
	}
	if err = r.authorized(ctx, a.key, bot.Project); err != nil {
		a.mu.Unlock()
		return err
	}
	c, err := r.o.Repository.Conversation(ctx, a.key, bot.ConversationID)
	if err != nil {
		a.mu.Unlock()
		return err
	}
	// Queued turns were admitted against a previous idle snapshot. Bind them
	// to the current process only after checking their immutable ownership;
	// rotating a process must not strand the rest of the durable queue.
	if t.Fence.Key != bot.Key || t.Fence.Project != bot.Project || t.Fence.ConversationID != c.ID || t.Fence.Generation > c.Generation {
		a.mu.Unlock()
		return runtimeFailure(pa.ScopeDenied, "queued_fence")
	}
	if c.ActiveTurnID != "" {
		a.mu.Unlock()
		return runtimeFailure(pa.OutcomeUnknown, "active_turn")
	}
	resolution, err := r.o.ProjectResolver.Resolve(ctx, pa.ProjectSelection{MachineID: bot.Project.MachineID, ProjectID: bot.Project.ProjectID})
	if err == nil {
		err = pa.ValidateTarget(pa.ProjectSelection{MachineID: bot.Project.MachineID, ProjectID: bot.Project.ProjectID}, resolution)
	}
	if err == nil && resolution.Scope != bot.Project {
		err = runtimeFailure(pa.ScopeDenied, "project")
	}
	if err != nil {
		a.mu.Unlock()
		return err
	}
	caps, err := r.o.Provider.Capabilities(ctx, bot)
	if err == nil {
		err = pa.CheckExecutionCapabilities(caps, t.Fence)
	}
	if err != nil {
		a.mu.Unlock()
		return err
	}
	current, err := r.o.Memory.Current(ctx, a.key)
	if err != nil {
		a.mu.Unlock()
		return err
	}
	snapshot, err := r.o.Memory.Snapshot(ctx, a.key)
	if err != nil {
		a.mu.Unlock()
		return err
	}
	export, err := r.o.Repository.Export(ctx, a.key)
	if err != nil {
		a.mu.Unlock()
		return err
	}
	var input *pa.Message
	for i := range export.Messages {
		if export.Messages[i].ID == t.MessageID {
			msg := export.Messages[i]
			input = &msg
			break
		}
	}
	if input == nil {
		a.mu.Unlock()
		return runtimeFailure(pa.NotFound, "queued_message")
	}
	if input.Fence.Key != a.key || input.Fence.Project != bot.Project || input.Fence.ConversationID != c.ID || input.Kind != pa.UserMessage {
		a.mu.Unlock()
		return runtimeFailure(pa.ScopeDenied, "queued_message")
	}
	quote, err := resolvePromptQuote(export, input.QuoteMessageID, currentFence(bot, c), current)
	if err != nil {
		a.mu.Unlock()
		return err
	}
	binding, err := r.o.Repository.Binding(ctx, a.key, c.BindingID)
	if err != nil {
		a.mu.Unlock()
		return err
	}
	decision, err := pa.DecideResume(binding, current, snapshot, caps)
	if err != nil {
		a.mu.Unlock()
		return err
	}
	// Every fresh process has a new generation. Unsafe native history is never
	// loaded, including after forgetting or changing fixed instructions.
	resume := decision == pa.ResumeExisting && binding.InstructionRevision == bot.Instructions.Revision
	c.Generation++
	c.BindingID = r.o.ID()
	t.Fence = currentFence(bot, c)
	binding.Fence = t.Fence
	binding.Revoked = false
	if !resume {
		binding.NativeSessionID = ""
	}
	binding.MemoryRevision = current.Revision
	binding.ForgottenThrough = current.ForgottenThrough
	binding.InstructionRevision = bot.Instructions.Revision
	t.State = pa.TurnRunning
	t.MemoryRevision = current.Revision
	t.Instructions = bot.Instructions
	c.ActiveTurnID = t.ID
	c.MemoryRevision = current.Revision
	cmd := r.command(a.key, pa.SendOperation, "runtime-start-"+t.ID, t)
	_, err = r.mutate(ctx, bot, Mutation{Command: cmd, Conversation: &c, Binding: &binding, Turn: &t, Receipt: pa.Receipt{State: pa.EffectExecuting, ResourceID: t.ID}, Events: []pa.Event{{Fence: t.Fence, Kind: pa.TurnUpdated, Turn: &t}}})
	a.mu.Unlock()
	if err != nil {
		return err
	}
	session, err := r.o.Provider.Open(ctx, OpenRequest{Bot: bot, Binding: binding, Directory: resolution.Directory, Resume: resume}, func(ctx context.Context, e ProtocolEvent) error { return r.incoming(ctx, a, e) })
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), r.o.Limits.CleanupTimeout)
		defer cancel()
		_ = session.Close(cleanup)
	}()
	a.mu.Lock()
	a.session = session
	if a.stopped || ctx.Err() != nil {
		a.mu.Unlock()
		return runtimeFailure(pa.ExecutionEffectUnknown, "stopped_before_send")
	}
	bot, err = r.o.Repository.Bot(ctx, a.key)
	if err == nil {
		binding.NativeSessionID = session.SessionID()
		cmd = r.command(a.key, pa.SendOperation, "runtime-bound-"+t.ID, binding)
		_, err = r.mutate(ctx, bot, Mutation{Command: cmd, Binding: &binding, Receipt: pa.Receipt{State: pa.EffectExecuting, ResourceID: t.ID}})
	}
	a.mu.Unlock()
	if err != nil {
		return err
	}
	var pending PendingPrompt
	if err = r.authorized(ctx, a.key, bot.Project); err != nil {
		return err
	}
	err = r.o.Memory.WithCurrent(ctx, a.key, current.Revision, func(filtered pa.ContextSnapshot) error {
		if err := pa.CheckContext(filtered, current); err != nil {
			return err
		}
		var e error
		pending, e = session.Send(ctx, PromptRequest{Turn: t, Text: input.Text, Context: filtered, Quote: quote})
		return e
	})
	if err != nil {
		return err
	}
	result, err := pending.Wait(ctx)
	if err != nil {
		return err
	}
	cleanup, cleanupCancel := context.WithTimeout(context.Background(), r.o.Limits.CleanupTimeout)
	err = session.Close(cleanup)
	cleanupCancel()
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		return runtimeFailure(pa.ExecutionEffectUnknown, "cancelled")
	}
	if err = r.authorized(ctx, a.key, bot.Project); err != nil {
		return err
	}
	return r.finishLocked(ctx, a, t, pa.TurnSucceeded, result.FinalText, nil)
}

// Historical messages do not carry complete memory provenance. After a forget
// fence, only a known turn captured at/after that fence can supply a quote.
// Export is a single repository snapshot; no history read occurs in WithCurrent.
func resolvePromptQuote(export Export, id string, fence pa.Fence, memory pa.MemoryRevision) (*PromptQuote, error) {
	if id == "" {
		return nil, nil
	}
	if memory.Key != fence.Key || memory.Project != fence.Project {
		return nil, runtimeFailure(pa.ScopeDenied, "quote_memory")
	}
	var message *pa.Message
	for i := range export.Messages {
		m := export.Messages[i]
		if m.ID != id {
			continue
		}
		if message != nil {
			return nil, runtimeFailure(pa.Invalid, "ambiguous_quote")
		}
		message = &m
	}
	if message == nil {
		return nil, runtimeFailure(pa.NotFound, "quote")
	}
	if message.Fence.Key != fence.Key || message.Fence.Project != fence.Project || message.Fence.ConversationID != fence.ConversationID {
		return nil, runtimeFailure(pa.ScopeDenied, "quote")
	}
	var source *pa.Turn
	for i := range export.Turns {
		t := export.Turns[i]
		if t.ID == message.TurnID {
			if source != nil {
				return nil, runtimeFailure(pa.Invalid, "ambiguous_quote_turn")
			}
			source = &t
		}
	}
	if source == nil {
		return nil, runtimeFailure(pa.ContextStale, "quote_provenance")
	}
	if source.Fence.Key != fence.Key || source.Fence.Project != fence.Project || source.Fence.ConversationID != fence.ConversationID {
		return nil, runtimeFailure(pa.ScopeDenied, "quote_turn")
	}
	for _, sourceID := range []string{message.ID, source.ID, source.MessageID} {
		if slices.Contains(memory.ForgottenSourceIDs, sourceID) {
			return nil, runtimeFailure(pa.ContextStale, "forgotten_quote")
		}
	}
	if memory.ForgottenThrough > 0 && (source.MemoryRevision < memory.ForgottenThrough || source.State == pa.TurnUnknown) {
		return nil, runtimeFailure(pa.ContextStale, "quote_memory_revision")
	}
	if source.MemoryRevision > memory.Revision {
		return nil, runtimeFailure(pa.ContextStale, "quote_memory_revision")
	}
	if err := pa.ValidateMessage(message.Text); err != nil {
		return nil, err
	}
	return &PromptQuote{MessageID: message.ID, Text: message.Text}, nil
}

func (r *Engine) finishLocked(ctx context.Context, a *runtimeActor, t pa.Turn, state pa.TurnState, text string, cause error) error {
	bot, err := r.o.Repository.Bot(ctx, a.key)
	if err != nil {
		return err
	}
	c, err := r.o.Repository.Conversation(ctx, a.key, bot.ConversationID)
	if err != nil {
		return err
	}
	var export Export
	if state == pa.TurnSucceeded || t.State == pa.TurnRunning || t.State == pa.TurnWaitingPermission || t.State == pa.TurnUnknown {
		export, err = r.o.Repository.Export(ctx, a.key)
		if err != nil {
			return err
		}
	}
	if state == pa.TurnSucceeded {
		if err = pa.CheckFence(t.Fence, currentFence(bot, c)); err != nil {
			return err
		}
	}
	t.State = state
	if cause != nil {
		t.Failure = &pa.Failure{Code: pa.Code(cause), Field: "execution"}
	}
	if c.ActiveTurnID == t.ID {
		c.ActiveTurnID = ""
	}
	var messages []pa.Message
	if state == pa.TurnSucceeded {
		// Visible messages have stable IDs. Querying this turn from export also
		// covers long conversations whose user message is outside the last page.
		var explicit []string
		for _, m := range export.Messages {
			if m.TurnID == t.ID && m.Kind == pa.AssistantMessage {
				explicit = append(explicit, m.ID)
			}
		}
		if final, ok := pa.VisibleReply(explicit, text, false); ok {
			if err = pa.ValidateMessage(final); err != nil {
				return err
			}
			messages = append(messages, pa.Message{ID: "reply-" + t.ID, Fence: t.Fence, TurnID: t.ID, Kind: pa.AssistantMessage, Text: final, At: r.o.Clock()})
		}
	}
	events := []pa.Event{{Fence: t.Fence, Kind: pa.TurnUpdated, Turn: &t}}
	var permissions []pa.PermissionOffer
	for _, offer := range export.Permissions {
		if offer.TurnID != t.ID || offer.State != pa.PermissionPending {
			continue
		}
		if offer.Fence != t.Fence {
			return runtimeFailure(pa.ScopeDenied, "terminal_permission")
		}
		if state == pa.TurnUnknown {
			offer.State = pa.PermissionUnknown
		} else if !r.o.Clock().Before(offer.ExpiresAt) {
			offer.State = pa.PermissionExpired
		} else {
			offer.State = pa.PermissionCancelled
		}
		permissions = append(permissions, offer)
		p := offer
		events = append(events, pa.Event{Fence: t.Fence, Kind: pa.PermissionAnswered, Permission: &p})
	}
	for i := range messages {
		events = append(events, pa.Event{Fence: t.Fence, Kind: pa.MessageCreated, Message: &messages[i]})
	}
	if state == pa.TurnSucceeded && len(messages) == 0 && text == "" {
		trace := pa.Trace{TurnID: t.ID, Category: "no_visible_reply", Complete: true}
		events = append(events, pa.Event{Fence: t.Fence, Kind: pa.TraceUpdated, Trace: &trace})
	}
	effect := pa.EffectDelivered
	if state == pa.TurnUnknown {
		effect = pa.EffectUnknown
	} else if state != pa.TurnSucceeded {
		effect = pa.EffectFailed
	}
	cmd := r.command(a.key, pa.SendOperation, "runtime-finish-"+t.ID, t)
	_, err = r.mutate(ctx, bot, Mutation{Command: cmd, Conversation: &c, Turn: &t, Messages: messages, Permissions: permissions, Receipt: pa.Receipt{State: effect, ResourceID: t.ID}, Events: events})
	return err
}

func (r *Engine) incoming(ctx context.Context, a *runtimeActor, e ProtocolEvent) error {
	a.mu.Lock()
	locked := true
	defer func() {
		if locked {
			a.mu.Unlock()
		}
	}()
	if a.stopped || e.ID == "" || e.TurnID != a.turnID {
		return runtimeFailure(pa.StaleGeneration, "incoming_turn")
	}
	bot, err := r.o.Repository.Bot(ctx, a.key)
	if err != nil {
		return err
	}
	if err = r.authorized(ctx, a.key, bot.Project); err != nil {
		return err
	}
	c, err := r.o.Repository.Conversation(ctx, a.key, bot.ConversationID)
	if err != nil {
		return err
	}
	if err = pa.CheckFence(e.Fence, currentFence(bot, c)); err != nil {
		return err
	}
	t, err := r.o.Repository.Turn(ctx, a.key, e.TurnID)
	if err != nil {
		return err
	}
	if t.State != pa.TurnRunning && t.State != pa.TurnWaitingPermission {
		return runtimeFailure(pa.StaleVersion, "incoming_turn")
	}
	cmd := r.command(a.key, pa.SendOperation, "runtime-event-"+t.ID+"-"+e.ID, e)
	if rec, err := r.replay(ctx, cmd, pa.SendOperation); err != nil {
		return err
	} else if rec != nil {
		return nil
	}
	m := Mutation{Command: cmd, Receipt: pa.Receipt{State: pa.EffectDelivered, ResourceID: e.ID}}
	count := 0
	if e.Message != "" {
		count++
		if err = pa.ValidateMessage(e.Message); err != nil {
			return err
		}
		msg := pa.Message{ID: "explicit-" + t.ID + "-" + e.ID, Fence: e.Fence, TurnID: t.ID, Kind: pa.AssistantMessage, Text: e.Message, At: r.o.Clock()}
		m.Messages = []pa.Message{msg}
		m.Events = append(m.Events, pa.Event{Fence: e.Fence, Kind: pa.MessageCreated, Message: &msg})
	}
	if e.Trace != nil {
		count++
		trace := *e.Trace
		trace.TurnID = t.ID
		m.Events = append(m.Events, pa.Event{Fence: e.Fence, Kind: pa.TraceUpdated, Trace: &trace})
	}
	if e.Permission != nil {
		count++
		p := *e.Permission
		if p.Fence != e.Fence || p.TurnID != t.ID || p.NativeRequestID == "" || p.ID == "" {
			return runtimeFailure(pa.ScopeDenied, "permission")
		}
		p.State = pa.PermissionPending
		m.Permissions = []pa.PermissionOffer{p}
		t.State = pa.TurnWaitingPermission
		m.Turn = &t
		m.Events = append(m.Events, pa.Event{Fence: e.Fence, Kind: pa.PermissionOffered, Permission: &p})
	}
	if count != 1 {
		return runtimeFailure(pa.Invalid, "incoming_payload")
	}
	_, err = r.mutate(ctx, bot, m)
	if err != nil {
		return err
	}
	if e.Permission != nil && bot.PermissionPolicy == pa.AutoRead {
		for _, option := range e.Permission.Options {
			if option.Allow && option.ReadOnly && option.Kind == pa.AllowOnce {
				answer := pa.PermissionAnswer{Fence: e.Fence, OfferID: e.Permission.ID, TurnID: e.TurnID, OptionID: option.ID}
				command := r.command(a.key, pa.AnswerOperation, "runtime-auto-read-"+e.Permission.ID, answer)
				a.mu.Unlock()
				locked = false
				_, err = r.Answer(ctx, AnswerRequest{Command: command, Answer: answer})
				return err
			}
		}
	}
	return nil
}

// Publish is the host's owned chat-tool entry point. Native MCP servers use
// their authenticated fence; arbitrary model-supplied Bot identities are not
// sufficient to reach this method through a transport.
func (r *Engine) Publish(ctx context.Context, e ProtocolEvent) error {
	r.mu.Lock()
	a := r.actors[e.Fence.Key]
	if a != nil {
		a.users++
	}
	r.mu.Unlock()
	if a == nil {
		return runtimeFailure(pa.StaleGeneration, "chat_tool")
	}
	defer r.release(a)
	return r.incoming(ctx, a, e)
}

// DispatchWork delegates mutations to the existing claims/worktree broker.
// The actor must be authenticated by the host before this entry point. Unknown
// broker effects are persisted before IO and are never retried automatically.
func (r *Engine) DispatchWork(ctx context.Context, q pa.WorkRequest) (pa.WorkLink, error) {
	a, err := r.acquire(q.Command.Key)
	if err != nil {
		return pa.WorkLink{}, err
	}
	defer r.release(a)
	a.mu.Lock()
	bot, err := r.o.Repository.Bot(ctx, a.key)
	if err != nil {
		a.mu.Unlock()
		return pa.WorkLink{}, err
	}
	if err = r.authorized(ctx, a.key, bot.Project); err != nil {
		a.mu.Unlock()
		return pa.WorkLink{}, err
	}
	c, err := r.o.Repository.Conversation(ctx, a.key, bot.ConversationID)
	if err != nil {
		a.mu.Unlock()
		return pa.WorkLink{}, err
	}
	fence := currentFence(bot, c)
	if a.stopped || q.TurnID != c.ActiveTurnID || q.TurnID == "" {
		a.mu.Unlock()
		return pa.WorkLink{}, runtimeFailure(pa.StaleVersion, "work_turn")
	}
	if err = pa.CheckWorkRequest(q, fence, r.o.Clock()); err != nil {
		a.mu.Unlock()
		return pa.WorkLink{}, err
	}
	if receipt, e := r.replay(ctx, q.Command, pa.DispatchOperation); e != nil {
		a.mu.Unlock()
		return pa.WorkLink{}, e
	} else if receipt != nil {
		a.mu.Unlock()
		return pa.WorkLink{}, runtimeFailure(pa.OutcomeUnknown, "inspect_work_receipt")
	}
	_, err = r.mutate(ctx, bot, Mutation{Command: q.Command, Receipt: pa.Receipt{State: pa.EffectUnknown, ResourceID: q.TurnID}})
	a.mu.Unlock()
	if err != nil {
		return pa.WorkLink{}, err
	}
	link, err := r.o.WorkBridge.Dispatch(ctx, q)
	if err != nil {
		return pa.WorkLink{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	bot, err = r.o.Repository.Bot(ctx, a.key)
	if err != nil {
		return link, err
	}
	if err = r.authorized(ctx, a.key, bot.Project); err != nil {
		return link, err
	}
	c, err = r.o.Repository.Conversation(ctx, a.key, bot.ConversationID)
	if err != nil {
		return link, err
	}
	if err = pa.CheckFence(fence, currentFence(bot, c)); err != nil {
		return link, err
	}
	if link.Fence != fence || link.TurnID != q.TurnID || link.ID == "" {
		return link, runtimeFailure(pa.ScopeDenied, "work_link")
	}
	cmd := r.command(a.key, pa.DispatchOperation, "runtime-work-"+link.ID, link)
	_, err = r.mutate(ctx, bot, Mutation{Command: cmd, WorkLinks: []pa.WorkLink{link}, Receipt: pa.Receipt{State: pa.EffectDelivered, ResourceID: link.ID}, Events: []pa.Event{{Fence: fence, Kind: pa.WorkUpdated, Work: &link}}})
	return link, err
}

func (r *Engine) Answer(ctx context.Context, q AnswerRequest) (pa.Receipt, error) {
	a, err := r.acquire(q.Command.Key)
	if err != nil {
		return pa.Receipt{}, err
	}
	defer r.release(a)
	a.mu.Lock()
	defer a.mu.Unlock()
	bot, err := r.o.Repository.Bot(ctx, a.key)
	if err != nil {
		return pa.Receipt{}, err
	}
	if err = r.authorized(ctx, a.key, bot.Project); err != nil {
		return pa.Receipt{}, err
	}
	if receipt, e := r.replay(ctx, q.Command, pa.AnswerOperation); e != nil {
		return pa.Receipt{}, e
	} else if receipt != nil {
		return *receipt, nil
	}
	c, err := r.o.Repository.Conversation(ctx, a.key, bot.ConversationID)
	if err != nil {
		return pa.Receipt{}, err
	}
	p, err := r.o.Repository.Permission(ctx, a.key, q.Answer.OfferID)
	if err != nil {
		return pa.Receipt{}, err
	}
	b, err := r.o.Repository.Binding(ctx, a.key, c.BindingID)
	if err != nil {
		return pa.Receipt{}, err
	}
	f, s, err := r.o.Authorize(ctx, a.key)
	if err != nil {
		return pa.Receipt{}, err
	}
	if err = pa.CheckPermission(p, q.Answer, pa.PermissionContext{Fence: currentFence(bot, c), ActiveTurnID: c.ActiveTurnID, BindingRevoked: b.Revoked, Now: r.o.Clock(), Source: s, Feature: f}); err != nil {
		return pa.Receipt{}, err
	}
	if q.Command.Key != q.Answer.Fence.Key || a.session == nil || a.stopped {
		return pa.Receipt{}, runtimeFailure(pa.StaleGeneration, "permission_session")
	}
	// Persist unknown before the external write. A crash here is inspectable,
	// never an automatic repeat of a native permission response.
	p.State = pa.PermissionUnknown
	p.SelectedOptionID = q.Answer.OptionID
	receipt, err := r.mutate(ctx, bot, Mutation{Command: q.Command, Permissions: []pa.PermissionOffer{p}, Receipt: pa.Receipt{State: pa.EffectUnknown, ResourceID: p.ID}, Events: []pa.Event{{Fence: p.Fence, Kind: pa.PermissionAnswered, Permission: &p}}})
	if err != nil {
		return pa.Receipt{}, err
	}
	if err = a.session.Answer(ctx, p.NativeRequestID, q.Answer.OptionID); err != nil {
		return receipt, err
	}
	bot, err = r.o.Repository.Bot(ctx, a.key)
	if err != nil {
		return receipt, err
	}
	p.State = pa.PermissionResolved
	t, err := r.o.Repository.Turn(ctx, a.key, p.TurnID)
	if err != nil {
		return receipt, err
	}
	t.State = pa.TurnRunning
	cmd := r.command(a.key, pa.AnswerOperation, "runtime-answer-"+p.ID, p)
	_, err = r.mutate(ctx, bot, Mutation{Command: cmd, Turn: &t, Permissions: []pa.PermissionOffer{p}, Receipt: pa.Receipt{State: pa.EffectDelivered, ResourceID: p.ID}, Events: []pa.Event{{Fence: p.Fence, Kind: pa.PermissionAnswered, Permission: &p}}})
	return receipt, err
}

func (r *Engine) Stop(ctx context.Context, q StopRequest) (pa.Receipt, error) {
	a, err := r.acquire(q.Command.Key)
	if err != nil {
		return pa.Receipt{}, err
	}
	defer r.release(a)
	a.mu.Lock()
	defer a.mu.Unlock()
	bot, err := r.o.Repository.Bot(ctx, a.key)
	if err != nil {
		return pa.Receipt{}, err
	}
	if err = r.authorized(ctx, a.key, bot.Project); err != nil && !errors.Is(err, context.Canceled) {
		return pa.Receipt{}, err
	}
	if receipt, e := r.replay(ctx, q.Command, pa.StopOperation); e != nil {
		return pa.Receipt{}, e
	} else if receipt != nil {
		return *receipt, nil
	}
	c, err := r.o.Repository.Conversation(ctx, a.key, bot.ConversationID)
	if err != nil {
		return pa.Receipt{}, err
	}
	if err = pa.CheckFence(q.Fence, currentFence(bot, c)); err != nil {
		return pa.Receipt{}, err
	}
	if q.TurnID != "" && c.ActiveTurnID != q.TurnID {
		return pa.Receipt{}, runtimeFailure(pa.StaleVersion, "stop_turn")
	}
	a.stopped = true
	if a.cancel != nil {
		a.cancel()
	}
	pending, err := r.o.Repository.PendingTurns(ctx, a.key)
	if err != nil {
		return pa.Receipt{}, err
	}
	for _, t := range pending {
		if t.State == pa.TurnUnknown {
			continue
		}
		state := pa.TurnCancelled
		if t.State == pa.TurnRunning || t.State == pa.TurnWaitingPermission || t.State == pa.TurnUnknown {
			state = pa.TurnUnknown
		}
		if err = r.finishLocked(ctx, a, t, state, "", runtimeFailure(pa.ExecutionEffectUnknown, "stopped")); err != nil {
			return pa.Receipt{}, err
		}
	}
	bot, err = r.o.Repository.Bot(ctx, a.key)
	if err != nil {
		return pa.Receipt{}, err
	}
	// The receipt acknowledges the durable stop intent. An active native turn
	// retains its unknown effect until cleanup is separately established.
	state := pa.EffectDelivered
	if a.turnID != "" {
		state = pa.EffectAccepted
	}
	receipt, err := r.mutate(ctx, bot, Mutation{Command: q.Command, Receipt: pa.Receipt{State: state, ResourceID: q.TurnID}})
	if a.session != nil {
		_ = a.session.Cancel(ctx)
	}
	return receipt, err
}

func (r *Engine) Disable(ctx context.Context, project pa.ProjectSelection) error {
	r.mu.Lock()
	r.disabled[project] = true
	var actors []*runtimeActor
	for _, a := range r.actors {
		a.users++
		actors = append(actors, a)
	}
	r.mu.Unlock()
	var result error
	for _, a := range actors {
		a.mu.Lock()
		var session ProviderSession
		if a.project.MachineID == project.MachineID && a.project.ProjectID == project.ProjectID {
			a.stopped = true
			if a.cancel != nil {
				a.cancel()
			}
			session = a.session
			pending, err := r.o.Repository.PendingTurns(ctx, a.key)
			result = errors.Join(result, err)
			for _, t := range pending {
				if t.State == pa.TurnUnknown {
					continue
				}
				state := pa.TurnCancelled
				if t.State == pa.TurnRunning || t.State == pa.TurnWaitingPermission || t.State == pa.TurnUnknown {
					state = pa.TurnUnknown
				}
				result = errors.Join(result, r.finishLocked(ctx, a, t, state, "", runtimeFailure(pa.ExecutionEffectUnknown, "disabled")))
			}
		}
		a.mu.Unlock()
		if session != nil {
			cleanup, cancel := context.WithTimeout(ctx, r.o.Limits.CleanupTimeout)
			result = errors.Join(result, session.Cancel(cleanup))
			result = errors.Join(result, session.Close(cleanup))
			cancel()
		}
		r.release(a)
	}
	return result
}
