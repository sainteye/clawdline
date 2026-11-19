package projectassistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type RuntimeOptions struct {
	NetworkCheck    func(context.Context, pa.Turn) error
	NativeHistory   NativeMaintenanceHistory
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
	// Attachments reads a message's stored pictures for its prompt.
	Attachments AttachmentReader
	Inputs      InputReader
	// Rooms gates group members' room turns; without it a room submission
	// is unsupported. Its repository must implement RoomRepository.
	Rooms RoomGate
}

// AttachmentReader returns the bytes stored for one attachment of a Bot.
type AttachmentReader interface {
	Read(key pa.BotKey, a pa.Attachment) ([]byte, error)
}

type Engine struct {
	o        RuntimeOptions
	mu       sync.Mutex
	actors   map[pa.BotKey]*runtimeActor
	slots    chan struct{}
	disabled map[pa.ProjectSelection]bool
	closed   bool
	// restarting marks the daemon's own shutdown (CloseForRestart).
	restarting   bool
	changed      chan struct{}
	pendingWakes map[pa.BotKey]struct{} // committed collaboration work, never native retry
	waking       bool                   // Engine.mu: one waiter for actor capacity per runtime
}

type routineCompletion struct {
	turn pa.Turn
	text string
}

type runtimeActor struct {
	mu         sync.Mutex
	key        pa.BotKey
	project    pa.ProjectScope
	users      int // protected by Engine.mu; includes the worker reference
	lifetime   context.Context
	stop       context.CancelFunc
	slot       bool               // Engine.mu: includes setup and unknown cleanup
	retained   bool               // Engine.mu: cleanup owner and process slot survive unknown cleanup
	cleanupErr error              // Engine.mu
	completion *routineCompletion // actor.mu; persisted before releasing a retained owner
	opening    bool               // actor.mu
	closing    bool               // actor.mu: seals live readings before native cleanup
	working    bool
	cancel     context.CancelFunc
	session    ProviderSession
	turnID     string
	// Includes only pending offers durably committed for this active turn.
	pendingPermissions int
	permissionFence    pa.Fence
	sourcePermissions  map[string]*sourcePermission
	stopped            atomic.Bool
	// steering holds queued turns handed to the agent by Send now until it
	// answers; the worker does not start one meanwhile (actor.mu).
	steering         map[string]bool
	steeredTurnID    string
	handoffAuthority handoffInputAuthority

	maintenance bool // immutable: owns the actor without a chat turn
}

var _ Runtime = (*Engine)(nil)

func (r *Engine) Usage() RuntimeUsage {
	r.mu.Lock()
	usage := RuntimeUsage{ActiveActors: len(r.actors), Processes: len(r.slots), Known: true}
	for _, a := range r.actors {
		if a.retained {
			usage.Known = false
		}
	}
	var actors []*runtimeActor
	for _, a := range r.actors {
		a.users++
		actors = append(actors, a)
	}
	r.mu.Unlock()
	for _, a := range actors {
		if a.mu.TryLock() {
			if a.opening {
				usage.Known = false
			}
			if a.session != nil {
				if s, ok := a.session.(interface{ Usage() SessionUsage }); ok {
					reading := s.Usage()
					reading.PendingPermissions = max(reading.PendingPermissions, a.pendingPermissions)
					usage.Sessions = append(usage.Sessions, reading)
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
	return &Engine{o: o, actors: make(map[pa.BotKey]*runtimeActor), slots: make(chan struct{}, o.Limits.MaxProcesses), disabled: make(map[pa.ProjectSelection]bool), changed: make(chan struct{})}, nil
}

func (r *Engine) acquire(key pa.BotKey) (*runtimeActor, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, runtimeFailure(pa.Disabled, "runtime")
	}
	a := r.actors[key]
	if a != nil && a.retained {
		return nil, runtimeFailure(pa.ExecutionEffectUnknown, "owned_cleanup")
	}
	if a != nil && a.maintenance {
		return nil, runtimeFailure(pa.CapacityFull, "native_maintenance_busy")
	}
	if a == nil {
		if len(r.actors) >= r.o.Limits.MaxBots {
			return nil, runtimeFailure(pa.CapacityFull, "active_bots")
		}
		lifetime, stop := context.WithCancel(context.Background())
		a = &runtimeActor{key: key, lifetime: lifetime, stop: stop}
		r.actors[key] = a
	}
	a.users++
	return a, nil
}
func (r *Engine) release(a *runtimeActor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a.users--
	if a.users == 0 && !a.retained {
		a.stop()
		delete(r.actors, a.key)
	}
	r.notifyLocked()
}
func (r *Engine) authorized(ctx context.Context, key pa.BotKey, project pa.ProjectScope) error {
	r.mu.Lock()
	disabled := r.closed || r.disabled[pa.ProjectSelection{MachineID: project.MachineID, ProjectID: project.ProjectID}]
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
	return runtimeCommand(key, op, id, payload)
}

func runtimeCommand(key pa.BotKey, op pa.Operation, id string, payload any) pa.Command {
	b, _ := json.Marshal(payload)
	h := sha256.Sum256(b)
	return pa.Command{Key: key, Operation: op, IdempotencyKey: id, ContentHash: hex.EncodeToString(h[:])}
}

// mutate increments the aggregate version once. Repository.Commit owns all
// atomic facts/receipt/event allocation; no external IO runs inside its tx.
func (r *Engine) mutate(ctx context.Context, bot pa.Bot, m Mutation) (pa.Receipt, error) {
	prepared, err := r.prepareMutation(ctx, bot, m)
	if err != nil {
		return pa.Receipt{}, err
	}
	return r.o.Repository.Commit(ctx, prepared)
}

func (r *Engine) prepareMutation(ctx context.Context, bot pa.Bot, m Mutation) (Mutation, error) {
	if len(m.Messages) > 0 {
		if m.Conversation == nil {
			c, err := r.o.Repository.Conversation(ctx, bot.Key, bot.ConversationID)
			if err != nil {
				return Mutation{}, err
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
	return m, nil
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
	if err := pa.ValidateInputSend(s.Message.Text, s.Message.Attachments, s.Message.UploadIDs); err != nil {
		return pa.Receipt{}, err
	}
	if s.Message.Handoff != nil {
		if err := pa.ValidateHandoffRef(*s.Message.Handoff); err != nil {
			return pa.Receipt{}, err
		}
		if s.Notice || s.Network != nil || s.Room != nil || s.AuthorityCheck == nil {
			return pa.Receipt{}, runtimeFailure(pa.ScopeDenied, "handoff_input")
		}
	}
	if s.Network != nil && (!s.Notice || s.Room != nil || len(s.Message.Attachments) > 0 || len(s.Message.UploadIDs) > 0 || s.Message.QuoteMessageID != "") {
		return pa.Receipt{}, runtimeFailure(pa.ScopeDenied, "network_submission")
	}
	if s.Room != nil {
		if err := r.admitRoomSubmission(ctx, s); err != nil {
			return pa.Receipt{}, err
		}
	}

	// Resolve and hash outside the actor. Receipt replay precedes file IO so an
	// uncertain durable acknowledgement never submits another native effect.
	var files []pa.InputFile
	owner := s.InputOwner
	if len(s.Message.UploadIDs) > 0 {
		if receipt, e := r.replay(ctx, s.Command, pa.SendOperation); e != nil {
			return pa.Receipt{}, e
		} else if receipt != nil {
			return *receipt, nil
		}
		if r.o.Inputs == nil {
			return pa.Receipt{}, runtimeFailure(pa.Unsupported, "input_files")
		}
		if s.InputCheck == nil {
			return pa.Receipt{}, runtimeFailure(pa.ScopeDenied, "input_authority")
		}
		config, e := r.o.Repository.Export(ctx, s.Command.Key)
		if e != nil {
			return pa.Receipt{}, e
		}
		owner.Revision = config.Bot.Instructions.Revision
		owner.Lifecycle = inputLifecycle(config)
		for _, id := range s.Message.UploadIDs {
			file, _, e := r.o.Inputs.Resolve(ctx, s.Message.Fence, id, owner)
			if e != nil {
				return pa.Receipt{}, e
			}
			files = append(files, file)
		}
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
	r.setProject(a, bot.Project)
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

	if len(files) > 0 {
		if e := s.InputCheck(ctx); e != nil {
			return pa.Receipt{}, e
		}
		if bot.Instructions.Revision != owner.Revision {
			return pa.Receipt{}, runtimeFailure(pa.StaleVersion, "upload_configuration")
		}
		x, e := r.o.Repository.Export(ctx, a.key)
		if e != nil {
			return pa.Receipt{}, e
		}
		if inputLifecycle(x) != owner.Lifecycle {
			return pa.Receipt{}, runtimeFailure(pa.StaleVersion, "upload_stopped")
		}
		for _, m := range x.Messages {
			for _, old := range m.Files {
				for _, file := range files {
					if old.ID == file.ID {
						return pa.Receipt{}, runtimeFailure(pa.IdempotencyConflict, "upload_bound")
					}
				}
			}
		}
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
	// An unknown turn is never replayed. A queue admitted before it ended
	// stays held behind it, and so does a new message. When only unknown
	// turns remain, as in CodyNC, the person's new message acknowledges
	// them: each becomes failed with an unknown effect, its receipt stays
	// unknown, and the new turn runs. An owned process whose cleanup is
	// unknown was already refused by acquire.
	var unknown []pa.Turn
	waiting := 0
	for _, t := range pending {
		if t.State == pa.TurnUnknown {
			unknown = append(unknown, t)
			continue
		}
		if !a.working && (t.State == pa.TurnRunning || t.State == pa.TurnWaitingPermission) {
			return pa.Receipt{}, runtimeFailure(pa.OutcomeUnknown, "inspect_interrupted_turn")
		}
		waiting++
	}
	if len(unknown) > 0 && s.Network != nil {
		return pa.Receipt{}, runtimeFailure(pa.ExecutionEffectUnknown, "network_unknown_barrier")
	}
	if len(unknown) > 0 && waiting > 0 {
		return pa.Receipt{}, runtimeFailure(pa.OutcomeUnknown, "inspect_interrupted_turn")
	}
	if err = pa.CheckAdmission(waiting); err != nil {
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
	if err = r.authorized(ctx, a.key, bot.Project); err != nil {
		return pa.Receipt{}, err
	}
	// The capability read can take seconds; admit against the current Bot
	// when only its version moved, as a turn's start does.
	if bot, c, err = r.currentForStart(ctx, a.key, bot, c); err != nil {
		return pa.Receipt{}, err
	}
	// The sender's grant can change while capabilities are read; nothing,
	// not even an acknowledgement of an unknown turn, is written under the
	// grant the request arrived with once it has been replaced.
	if s.AuthorityCheck != nil {
		if err = s.AuthorityCheck(ctx); err != nil {
			return pa.Receipt{}, err
		}
	}
	if len(files) > 0 && bot.Instructions.Revision != owner.Revision {
		return pa.Receipt{}, runtimeFailure(pa.StaleVersion, "upload_configuration")
	}
	var binding *pa.ExecutionBinding
	if c.BindingID == "" {
		memory, e := r.o.Memory.Current(ctx, a.key)
		if e != nil {
			return pa.Receipt{}, e
		}
		c.BindingID = r.o.ID()
		c.Generation = 1
		b := pa.ExecutionBinding{Fence: currentFence(bot, c), Agent: bot.Agent, LoginProfile: bot.LoginProfile, MemoryRevision: memory.Revision, ForgottenThrough: memory.ForgottenThrough, InstructionRevision: bot.Instructions.Revision}
		binding = &b
	}
	for _, t := range unknown {
		t.State = pa.TurnFailed
		t.Failure = &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: "acknowledged"}
		cmd := r.command(a.key, pa.SendOperation, "runtime-acknowledge-"+t.ID, t)
		if _, err = r.mutate(ctx, bot, Mutation{Command: cmd, Turn: &t, Receipt: pa.Receipt{State: pa.EffectUnknown, ResourceID: t.ID}, Events: []pa.Event{{Fence: t.Fence, Kind: pa.TurnUpdated, Turn: &t}}}); err != nil {
			return pa.Receipt{}, err
		}
		if bot, err = r.o.Repository.Bot(ctx, a.key); err != nil {
			return pa.Receipt{}, err
		}
	}
	if len(files) > 0 {
		if e := s.InputCheck(ctx); e != nil {
			return pa.Receipt{}, e
		}
	}
	fence := currentFence(bot, c)
	turn := pa.Turn{ID: r.o.ID(), Fence: fence, MessageID: r.o.ID(), State: pa.TurnQueued, IdempotencyKey: s.Command.IdempotencyKey}
	turn.Origin = pa.UserOrigin
	kind := pa.UserMessage
	if s.Notice {
		kind = pa.NoticeMessage
		turn.Origin = pa.HostNoticeOrigin
	}
	if s.Network != nil {
		turn.Origin = pa.NetworkRequestOrigin
		captured := *s.Network
		turn.Network = &captured
	}
	if s.Room != nil {
		ticket := *s.Room
		turn.Room = &ticket
	}
	message := pa.Message{ID: turn.MessageID, Fence: fence, TurnID: turn.ID, Kind: kind, Text: s.Message.Text, QuoteMessageID: s.Message.QuoteMessageID, At: r.o.Clock(), Attachments: s.Message.Attachments, Files: files}
	receipt, err := r.commitHandoffAwareInput(ctx, bot, s, Mutation{Command: s.Command, Conversation: &c, Binding: binding, Turn: &turn, Messages: []pa.Message{message}, Receipt: pa.Receipt{State: pa.EffectAccepted, ResourceID: turn.ID}, Events: []pa.Event{{Fence: fence, Kind: pa.MessageCreated, Message: &message}, {Fence: fence, Kind: pa.TurnUpdated, Turn: &turn}}})
	if err != nil {
		return pa.Receipt{}, err
	}
	if s.Message.Handoff != nil && receipt.ResourceID == turn.ID {
		a.handoffAuthority = handoffInputAuthority{TurnID: turn.ID, Check: afterRequest(ctx, s.AuthorityCheck)}
	}
	r.setProject(a, bot.Project)
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
// resumePrompt is Codync's (host/src/agent/bot/mod.rs RESUME_PROMPT) for a
// turn a restart cut off.
const resumePrompt = "[Clawdline restarted on this computer and interrupted you mid-task. You've been resumed with your conversation intact. Continue exactly where you left off and finish what you were doing. If your previous step already completed an action, do NOT repeat it — just carry on from there.]"

// staleResume is Codync's STALE_RESUME_MS: an older interrupted task is more
// likely unwanted than awaited, so it stays unknown for the person.
const staleResume = time.Hour

// Recover marks turns a restart cut off as unknown, as before. As in Codync,
// one started within the last hour is then resumed: Clawdline sends the
// resume prompt as a notice, which acknowledges the unknown turn (its receipt
// stays unknown) and runs in the same native session when it can be loaded.
// If anything else is still queued, nothing is resumed and the person's next
// message decides, as before.
func (r *Engine) Recover(ctx context.Context, key pa.BotKey) error {
	resumable, err := r.recover(ctx, key)
	if err != nil {
		return err
	}
	for _, t := range resumable {
		if len(t.Attempts) > 0 {
			continue
		}
		if card, e := r.handoffForTurn(ctx, t); e != nil {
			return e
		} else if card != nil {
			continue
		}
		_ = r.resumeInterrupted(ctx, key, t)
	}
	return nil
}

func (r *Engine) resumeInterrupted(ctx context.Context, key pa.BotKey, t pa.Turn) error {
	feature, source, err := r.o.Authorize(ctx, key)
	if err != nil {
		return err
	}
	bot, err := r.o.Repository.Bot(ctx, key)
	if err != nil {
		return err
	}
	c, err := r.o.Repository.Conversation(ctx, key, bot.ConversationID)
	if err != nil {
		return err
	}
	// Preserve the original interrupted turn when its native history is
	// fenced. Even accepting a restart notice would acknowledge the old
	// turn before the worker could refuse to load it.
	if r.o.NativeHistory != nil && t.Fence.BindingID != "" {
		binding, err := r.o.Repository.Binding(ctx, key, t.Fence.BindingID)
		if err != nil {
			return err
		}
		if err = r.checkNativeHistory(ctx, binding); err != nil {
			return err
		}
	}
	_, err = r.Submit(ctx, Submission{Command: r.command(key, pa.SendOperation, "runtime-resume-"+t.ID, t.ID),
		Message: pa.SendMessage{Fence: currentFence(bot, c), Text: resumePrompt}, Feature: feature, Source: source, Notice: true})
	return err
}

func (r *Engine) recover(ctx context.Context, key pa.BotKey) ([]pa.Turn, error) {
	var resumable []pa.Turn
	a, err := r.acquire(key)
	if err != nil {
		return nil, err
	}
	defer r.release(a)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.working {
		return nil, nil
	}
	if p, ok := r.o.Repository.(CollaborationRepository); ok {
		if err = p.DropExchangeReplies(ctx, key, r.o.Clock()); err != nil {
			return nil, err
		}
	}
	pending, err := r.o.Repository.PendingTurns(ctx, key)
	if err != nil {
		return nil, err
	}
	var export Export
	interrupted := 0
	for _, t := range pending {
		if t.State == pa.TurnRunning || t.State == pa.TurnWaitingPermission {
			interrupted++
		}
	}
	if interrupted > 0 {
		if export, err = r.o.Repository.Export(ctx, key); err != nil {
			return nil, err
		}
	}
	for _, t := range pending {
		if pa.IsPeerRequest(t.Origin) || t.Origin == pa.BotReplyOrigin && (t.State == pa.TurnQueued || t.State == pa.TurnAccepted) {
			state := pa.TurnCancelled
			if t.State == pa.TurnRunning || t.State == pa.TurnWaitingPermission {
				state = pa.TurnUnknown
			}
			if t.State != pa.TurnUnknown {
				if err = r.finishLocked(ctx, a, t, state, "", runtimeFailure(pa.ExecutionEffectUnknown, "restart")); err != nil {
					return nil, err
				}
			}
			continue
		}
		if t.State == pa.TurnRunning || t.State == pa.TurnWaitingPermission {
			if err = r.finishLocked(ctx, a, t, pa.TurnUnknown, "", runtimeFailure(pa.ExecutionEffectUnknown, "restart")); err != nil {
				return nil, err
			}
			for _, m := range export.Messages {
				if !pa.IsPeerRequest(t.Origin) && t.RoutineRunID == "" && m.ID == t.MessageID && r.o.Clock().Sub(m.At) < staleResume && len(pending) == interrupted && !interruptedPermission(t, export.Permissions) {
					resumable = append(resumable, t)
				}
			}
		}
	}
	bot, err := r.o.Repository.Bot(ctx, key)
	if err != nil {
		return nil, err
	}
	r.setProject(a, bot.Project)
	if len(resumable) > 0 {
		// The resume notice's own submission starts the worker; one started
		// now would see the unknown turn, stop, and could miss the notice.
		return resumable, nil
	}
	a.working = true
	r.mu.Lock()
	a.users++
	r.mu.Unlock()
	go r.work(a)
	return resumable, nil
}

func (r *Engine) work(a *runtimeActor) {
	defer r.release(a)
	for {
		a.mu.Lock()
		if a.lifetime.Err() != nil {
			ctx, cancel := context.WithTimeout(context.Background(), r.o.Limits.CleanupTimeout)
			err := r.stopPendingLocked(ctx, a)
			cancel()
			if err != nil {
				r.retain(a, err)
			}
			a.working = false
			a.mu.Unlock()
			return
		}
		if err := r.promoteExchangeReply(a.lifetime, a.key); err != nil {
			a.working = false
			a.mu.Unlock()
			return
		}
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
			if t.State == pa.TurnUnknown || t.State == pa.TurnRunning || t.State == pa.TurnWaitingPermission || a.steering[t.ID] {
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
		if turn.RoutineRunID != "" {
			x, e := r.o.Repository.Export(context.Background(), a.key)
			if e != nil {
				a.working = false
				a.mu.Unlock()
				return
			}
			for _, run := range x.RoutineRuns {
				if run.ID == turn.RoutineRunID && run.NotBefore.After(r.o.Clock()) {
					a.working = false
					a.mu.Unlock()
					return
				}
			}
		}
		timeout := r.o.Limits.PromptTimeout
		if turn.RoutineRunID != "" {
			x, e := r.o.Repository.Export(context.Background(), a.key)
			if e != nil {
				a.working = false
				a.mu.Unlock()
				return
			}
			d, _, e := routineForTurn(x, *turn)
			if e == nil {
				timeout = time.Duration(d.TimeoutSeconds) * time.Second
			}
		}
		ctx, cancel := context.WithTimeout(a.lifetime, timeout)
		a.cancel = cancel
		a.turnID = turn.ID
		a.pendingPermissions = 0
		a.stopped.Store(false)
		a.mu.Unlock()
		// Waiting for a process slot remains cancellable and does not launch IO.
		select {
		case r.slots <- struct{}{}:
			r.mu.Lock()
			a.slot = true
			r.mu.Unlock()
			err = r.execute(ctx, a, *turn)
			r.mu.Lock()
			retained := a.retained
			r.mu.Unlock()
			if !retained {
				r.mu.Lock()
				a.slot = false
				<-r.slots
				r.mu.Unlock()
			}
		case <-ctx.Done():
			err = ctx.Err()
		}
		cancel()
		r.wakeExchangeReply(*turn)
		a.mu.Lock()
		a.cancel = nil
		if a.handoffAuthority.TurnID == turn.ID {
			a.handoffAuthority = handoffInputAuthority{}
		}
		a.turnID = ""
		// Only an already completed routine awaiting its terminal commit keeps
		// that success in memory. Unknown Open/Close still persists TurnUnknown.
		if err != nil && a.completion == nil {
			latest, e := r.o.Repository.Turn(context.Background(), a.key, turn.ID)
			if e == nil {
				retried, retryErr := r.retryRoutineNotStartedLocked(context.Background(), a, latest, err)
				if retried || retryErr != nil {
					a.working = false
					a.mu.Unlock()
					return
				}
			}
			if e == nil && (latest.State == pa.TurnRunning || latest.State == pa.TurnWaitingPermission || latest.State == pa.TurnQueued) {
				state := pa.TurnUnknown
				if latest.State == pa.TurnQueued {
					state = pa.TurnFailed
					if a.lifetime.Err() != nil {
						state = pa.TurnCancelled
					}
				}
				if latest.State == pa.TurnRunning && pa.SafeAttemptClosed(latest) && latest.Attempts[len(latest.Attempts)-1].SendReturned {
					state = pa.TurnFailed
				}
				var notStarted *routineNotStarted
				if latest.RoutineRunID != "" && errors.As(err, &notStarted) && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
					state = pa.TurnFailed
				}
				if e = r.finishLocked(context.Background(), a, latest, state, "", err); e != nil {
					a.working = false
					a.mu.Unlock()
					return
				}
			}
		}
		// Any terminal failure may also have queued a correlated source reply.
		a.mu.Unlock()
		r.wakeExchangeReply(*turn)
		a.mu.Lock()
		r.mu.Lock()
		retained := a.retained
		r.mu.Unlock()
		if retained {
			a.working = false
			a.mu.Unlock()
			return
		}
		a.mu.Unlock()
	}
}

func (r *Engine) execute(ctx context.Context, a *runtimeActor, t pa.Turn) error {
	return r.executeAttempt(ctx, a, t, nil)
}

func (r *Engine) executeAttempt(ctx context.Context, a *runtimeActor, t pa.Turn, original *pa.Bot) (resultErr error) {
	a.mu.Lock()
	if a.stopped.Load() || ctx.Err() != nil {
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
	handoffCard, err := r.handoffForTurn(ctx, t)
	if err != nil {
		a.mu.Unlock()
		return err
	}
	var handoffCheck func(context.Context) error
	if handoffCard != nil {
		if a.handoffAuthority.TurnID != t.ID || a.handoffAuthority.Check == nil {
			a.mu.Unlock()
			return runtimeFailure(pa.Revoked, "handoff_authority")
		}
		handoffCheck = a.handoffAuthority.Check
	}
	preparedBot := bot
	if original != nil && !sameExecutionConfig(bot, *original) {
		a.mu.Unlock()
		return runtimeFailure(pa.StaleVersion, "fallback_configuration")
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
	if c.ActiveTurnID != "" && !(c.ActiveTurnID == t.ID && t.State == pa.TurnRunning && pa.SafeAttemptClosed(t)) {
		a.mu.Unlock()
		return runtimeFailure(pa.OutcomeUnknown, "active_turn")
	}
	// A room turn runs only while the room still wants it: checked here,
	// before any native effect, so a refusal ends a queued turn as failed.
	if t.Room != nil && t.State == pa.TurnQueued {
		if err = r.admitRoomTurn(ctx, *t.Room); err != nil {
			a.mu.Unlock()
			return err
		}
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
	effectiveStep, err := r.selectStep(ctx, bot, t)
	effectiveAgent := effectiveStep.Agent
	if err != nil {
		a.mu.Unlock()
		return err
	}
	effectiveBot := bot
	effectiveBot.Agent = effectiveAgent
	effectiveBot.LoginProfile = effectiveStep.LoginProfile
	if effectiveAgent != bot.Agent || effectiveStep.LoginProfile != bot.LoginProfile {
		effectiveBot.RuntimeSettings = pa.RuntimeSettings{}
	}
	caps, err := r.o.Provider.Capabilities(ctx, effectiveBot)
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
	if handoffCard != nil {
		if err = checkHandoffExecution(*handoffCard, t, bot, currentFence(bot, c), current, original == nil); err == nil {
			err = handoffCheck(ctx)
		}
		if err != nil {
			a.mu.Unlock()
			return err
		}
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
	if input.Fence.Key != a.key || input.Fence.Project != bot.Project || input.Fence.ConversationID != c.ID || input.Kind != pa.UserMessage && input.Kind != pa.NoticeMessage {
		a.mu.Unlock()
		return runtimeFailure(pa.ScopeDenied, "queued_message")
	}
	if err = collaborationPrompt(t, input); err != nil {
		a.mu.Unlock()
		return err
	}
	if pa.IsPeerRequest(t.Origin) && (bot.Hidden || len(input.Files) > 0 || len(input.Attachments) > 0) {
		a.mu.Unlock()
		return runtimeFailure(pa.ScopeDenied, "collaboration_recipient")
	}
	quote, err := resolvePromptQuote(export, input.QuoteMessageID, currentFence(bot, c), current)
	if err != nil {
		a.mu.Unlock()
		return err
	}
	mainID := c.MainBindingID
	if mainID == "" {
		mainID = c.BindingID
	}
	binding, err := r.o.Repository.Binding(ctx, a.key, mainID)
	if err != nil {
		a.mu.Unlock()
		return err
	}
	var routineRun *pa.RoutineRun
	isolatedRoutine := t.RoutineRunID != ""
	if t.RoutineRunID != "" {
		if !caps.Resume {
			a.mu.Unlock()
			return runtimeFailure(pa.ResumeUnsupported, "routine_main_session")
		}
		d, run, e := routineForTurn(export, t)
		if e != nil {
			a.mu.Unlock()
			return e
		}
		if run.State != pa.TurnQueued {
			a.mu.Unlock()
			return runtimeFailure(pa.OutcomeUnknown, "routine_run")
		}
		input.Text = routinePrompt(d, current, run.Event)
		isolatedRoutine = !d.FollowUp
		run.Instruction = d.Instruction
		run.TimeoutSeconds = d.TimeoutSeconds
		routineRun = &run
	}
	if isolatedRoutine && (binding.Agent != effectiveAgent || binding.Revoked) {
		a.mu.Unlock()
		return runtimeFailure(pa.Unsupported, "routine_effective_agent")
	}
	fromAgent := binding.Agent
	var resumedFrom string
	var carryKind string
	var carryAfter int64
	// Each configured identity keeps its own native conversation. A switch
	// chooses only its newest confirmed-clean binding, never the active
	// alternate's history. All execution authority is minted afresh below.
	if handoffCard == nil && !isolatedRoutine && pa.HasFallbacks(bot) && (binding.Agent != effectiveAgent || binding.LoginProfile != effectiveBot.LoginProfile) {
		carryKind = "recap"
		if parked, seen, ok := parkedAttempt(export, t, effectiveAgent, effectiveBot.LoginProfile); ok {
			binding = parked
			binding.Revoked = false // old fence stays revoked; only the new copy is used
			resumedFrom, carryAfter = parked.Fence.BindingID, seen
		}
	}
	if binding.Agent != effectiveAgent || binding.LoginProfile != effectiveBot.LoginProfile || binding.Revoked {
		binding.Agent = effectiveAgent
		binding.LoginProfile = effectiveBot.LoginProfile
		binding.NativeSessionID = ""
		binding.Revoked = false
	}
	// Check the selected history, including a parked fallback, before an
	// adapter can attempt a load or fall back to a fresh open in the same
	// history directory. Isolated routines, Forget and changed instructions
	// use separate history partitions through the existing filtered-start path.
	if !isolatedRoutine && binding.InstructionRevision == bot.Instructions.Revision && binding.ForgottenThrough == current.ForgottenThrough {
		if err = r.checkNativeHistory(ctx, binding); err != nil {
			a.mu.Unlock()
			return err
		}
	}
	decision, err := pa.DecideResume(binding, current, snapshot, caps)
	if err != nil {
		a.mu.Unlock()
		return err
	}
	// Every fresh process has a new generation. Unsafe native history is never
	// loaded, including after forgetting or changing fixed instructions.
	resume := handoffCard == nil && (len(t.Attempts) == 0 || resumedFrom != "") && !isolatedRoutine && decision == pa.ResumeExisting && binding.InstructionRevision == bot.Instructions.Revision
	if resumedFrom != "" && resume {
		carryKind = "catch_up"
	} else {
		resumedFrom, carryAfter = "", 0
	}
	// Preparation can block on capability, memory or history reads. Authority
	// checked before those reads does not authorize a later execution start.
	if err = r.authorized(ctx, a.key, bot.Project); err != nil {
		a.mu.Unlock()
		return err
	}
	// Preparation can take seconds, and the Bot's version also moves for
	// writes that do not concern this start, such as a reader's position.
	// Start from the current Bot and conversation when nothing this turn
	// depends on has changed; anything that has stays a stale refusal.
	if bot, c, err = r.currentForStart(ctx, a.key, bot, c); err != nil {
		a.mu.Unlock()
		return err
	}
	if c.MainBindingID == "" {
		c.MainBindingID = c.BindingID
	}
	c.Generation++
	c.BindingID = r.o.ID()
	if isolatedRoutine {
		binding.SessionSlot = "routine-" + t.RoutineRunID
	} else {
		binding.SessionSlot = ""
		c.MainBindingID = c.BindingID
	}
	t.Fence = currentFence(bot, c)
	binding.Fence = t.Fence
	binding.PrimarySupersededAt = 0
	binding.Revoked = false
	if !resume {
		binding.NativeSessionID = ""
	}
	// A resumed session last heard memory at its binding's revision.
	memoryChanged := binding.MemoryRevision != current.Revision
	binding.MemoryRevision = current.Revision
	binding.ForgottenThrough = current.ForgottenThrough
	binding.InstructionRevision = bot.Instructions.Revision
	t.State = pa.TurnRunning
	started := r.o.Clock().UTC()
	if t.StartedAt == nil {
		t.StartedAt = &started
	}
	t.MemoryRevision = current.Revision
	t.Instructions = bot.Instructions
	c.ActiveTurnID = t.ID
	c.MemoryRevision = current.Revision
	ordinal := 1
	if len(t.Attempts) > 0 {
		ordinal = t.Attempts[len(t.Attempts)-1].Ordinal + 1
	}
	if pa.HasFallbacks(bot) {
		attempt := pa.AttemptFact{Ordinal: ordinal, Agent: effectiveAgent, LoginProfile: effectiveBot.LoginProfile, Fence: t.Fence, Stage: pa.AttemptOpening, Effect: "unknown", ResumeBindingID: resumedFrom, CarryKind: carryKind, CarryAfterSeq: carryAfter}
		if t.Room != nil {
			attempt.Fallback = effectiveAgent != bot.Agent || effectiveBot.LoginProfile != bot.LoginProfile
			attempt.PrimaryRestReason = effectiveStep.primaryRestReason
		}
		t.Attempts = append(slices.Clone(t.Attempts), attempt)
	}
	cmd := r.command(a.key, pa.SendOperation, "runtime-start-"+t.ID+"-"+binding.Fence.BindingID, t)
	if routineRun != nil {
		routineRun.State = pa.TurnRunning
		routineRun.NotStartedProved = false
		routineRun.BindingID = binding.Fence.BindingID
	}
	_, err = r.mutate(ctx, bot, Mutation{Command: cmd, Conversation: &c, Binding: &binding, Turn: &t, RoutineRun: routineRun, Receipt: pa.Receipt{State: pa.EffectExecuting, ResourceID: t.ID}, Events: []pa.Event{{Fence: t.Fence, Kind: pa.TurnUpdated, Turn: &t}}})
	a.mu.Unlock()
	if err != nil {
		return err
	}
	if t.Origin == pa.BotRequestOrigin {
		p, e := r.collaboration()
		if e != nil {
			return e
		}
		if e = p.StartExchange(ctx, t.Fence, t.ID, t.ExchangeID); e != nil {
			return e
		}
	}
	a.mu.Lock()
	a.opening = true
	a.mu.Unlock()
	err = r.checkAttemptBoundary(ctx, a, preparedBot, c, t, true)
	if err == nil && handoffCheck != nil {
		err = handoffCheck(ctx)
	}
	if err == nil {
		err = r.checkNativeHistory(ctx, binding)
	}
	if err != nil {
		a.mu.Lock()
		a.opening = false
		a.mu.Unlock()
		return err
	}
	session, err := r.o.Provider.Open(ctx, OpenRequest{PrimaryAgent: bot.Agent, Bot: effectiveBot, Binding: binding, Directory: resolution.Directory, Resume: resume}, func(ctx context.Context, e ProtocolEvent) error { return r.incoming(ctx, a, e) })
	if session == nil && resume && !isolatedRoutine && pa.Code(err) == pa.ResumeUnsupported {
		// As in Codync, a native session that cannot be loaded is replaced by
		// a new one that receives the filtered history and memory; nothing
		// was sent to the old one, so nothing is repeated.
		resume = false
		binding.NativeSessionID = ""
		err = r.checkAttemptBoundary(ctx, a, preparedBot, c, t, true)
		if err == nil {
			err = r.checkNativeHistory(ctx, binding)
		}
		if err != nil {
			a.mu.Lock()
			a.opening = false
			a.mu.Unlock()
			return err
		}
		session, err = r.o.Provider.Open(ctx, OpenRequest{PrimaryAgent: bot.Agent, Bot: effectiveBot, Binding: binding, Directory: resolution.Directory, Resume: false}, func(ctx context.Context, e ProtocolEvent) error { return r.incoming(ctx, a, e) })
	}
	a.mu.Lock()
	a.opening = false
	a.closing = false
	a.session = session
	a.mu.Unlock()
	if session == nil {
		if err == nil {
			err = runtimeFailure(pa.ExecutionEffectUnknown, "missing_session")
		}
		// Unknown native Open without a handle retains ownership, even for
		// quota. The host can separately prove Open was never entered; that fact
		// permits releasing an unused slot but never entering fallback.
		var notStarted *OpenNotStarted
		if !errors.As(err, &notStarted) {
			r.retain(a, err)
		}
		return err
	}
	cleaned := false
	cleanupConfirmed := false
	defer func() {
		if !cleaned {
			closeErr := r.closeSession(a, session)
			resultErr = errors.Join(resultErr, closeErr)
			cleaned = true
			cleanupConfirmed = closeErr == nil
		}
		if resultErr != nil {
			resultErr = errors.Join(resultErr, r.holdAttempt(a, t, resultErr, cleanupConfirmed))
		}
	}()
	if err != nil {
		if r.routineRefusedBeforeSend(ctx, a.key, t, err) {
			closeErr := r.closeSession(a, session)
			cleaned, cleanupConfirmed = true, closeErr == nil
			if closeErr != nil {
				return errors.Join(err, closeErr)
			}
			return &routineNotStarted{cause: err}
		}
		safe, e := r.refuseBeforeSend(ctx, a, &t, binding, err)
		if e != nil {
			return e
		}
		if !safe {
			return err
		}
		// ownedSession.Close seals tools before waiting for native cleanup.
		e = r.closeSession(a, session)
		cleaned = true
		cleanupConfirmed = e == nil
		if e != nil {
			return e
		}
		rest, e := r.attemptRest(ctx, bot, t)
		if e != nil {
			return e
		}
		if e = r.appendAttempt(ctx, a, &t, pa.AttemptClosed, nil, rest); e != nil {
			return e
		}
		// Recursion is bounded by the validated unique configured identities, never
		// a same-agent retry; ctx and the process slot belong to the logical turn.
		return r.executeAttempt(ctx, a, t, &preparedBot)
	}
	a.mu.Lock()
	if a.stopped.Load() || ctx.Err() != nil {
		a.mu.Unlock()
		return runtimeFailure(pa.ExecutionEffectUnknown, "stopped_before_send")
	}
	bot, err = r.o.Repository.Bot(ctx, a.key)
	if err == nil {
		err = r.authorized(ctx, a.key, bot.Project)
	}
	if err == nil {
		binding.NativeSessionID = session.SessionID()
		cmd = r.command(a.key, pa.SendOperation, "runtime-bound-"+t.ID+"-"+binding.Fence.BindingID, binding)
		_, err = r.mutate(ctx, bot, Mutation{Command: cmd, Binding: &binding, Receipt: pa.Receipt{State: pa.EffectExecuting, ResourceID: t.ID}})
	}
	a.mu.Unlock()
	if err != nil {
		return err
	}
	if err = r.appendAttempt(ctx, a, &t, pa.AttemptOpened, nil, nil); err != nil {
		return err
	}
	var pending PendingPrompt
	if err = r.authorized(ctx, a.key, bot.Project); err != nil {
		return err
	}

	var files []PromptFile
	for _, file := range input.Files {
		if r.o.Inputs == nil {
			return runtimeFailure(pa.Unsupported, "input_files")
		}
		path, e := r.o.Inputs.SentPath(ctx, input.Fence, file)
		if e != nil {
			return e
		}
		files = append(files, PromptFile{File: file, Path: path})
	}
	if err = r.appendAttempt(ctx, a, &t, pa.AttemptSending, nil, nil); err != nil {
		return err
	}
	err = r.o.Memory.WithCurrent(ctx, a.key, current.Revision, func(filtered pa.ContextSnapshot) error {
		if err := pa.CheckContext(filtered, current); err != nil {
			return err
		}
		var handoff *SessionHandoff
		if !resume && !isolatedRoutine {
			var err error
			handoff, err = newSessionHandoffForAgent(export, t, filtered, effectiveAgent)
			if err != nil {
				return err
			}
		}
		promptText := input.Text
		if carryKind != "" && !isolatedRoutine {
			kind, after := carryKind, carryAfter
			if !resume {
				kind, after = "recap", 0
			}
			var prefix string
			var err error
			handoff, prefix, err = fallbackOpening(export, t, filtered, effectiveAgent, fromAgent, kind, after)
			if err != nil {
				return err
			}
			promptText = prefix + promptText
		}
		// WithCurrent may wait behind a memory writer. Check again at the
		// actual send boundary rather than trusting the pre-lock snapshot.
		if err := r.authorized(ctx, a.key, bot.Project); err != nil {
			return err
		}
		var e error
		images, e := r.images(a.key, input.Attachments)
		if e != nil {
			return e
		}
		// The sending fact was persisted before acquiring the memory lock.
		// Recheck the exact authority at the external write boundary.
		if e = r.checkAttemptBoundary(ctx, a, preparedBot, c, t, false); e != nil {
			return e
		}
		if handoffCard != nil {
			if handoff == nil {
				return runtimeFailure(pa.ContextStale, "handoff_native_opening")
			}
			if e = handoffCheck(ctx); e != nil {
				return e
			}
			text, claimErr := r.o.Repository.(HandoffRepository).ClaimHandoffText(ctx, *handoffCard)
			if claimErr != nil {
				return claimErr
			}
			if e = handoffCheck(ctx); e != nil {
				return e
			}
			handoff.editedSummary = &text
		}
		pending, e = session.Send(ctx, PromptRequest{Turn: t, Text: promptText, Context: filtered, Quote: quote, Handoff: handoff, Images: images, Files: files, MemoryChanged: resume && memoryChanged})
		return e
	})
	if err != nil {
		return err
	}
	if pending == nil {
		return runtimeFailure(pa.ExecutionEffectUnknown, "missing_pending_prompt")
	}
	if err = r.appendAttempt(ctx, a, &t, pa.AttemptSent, nil, nil); err != nil {
		return err
	}
	result, err := pending.Wait(ctx)
	if err != nil {
		quota := terminalQuota(t, err)
		if quota == nil || !quota.NoOutput || a.stopped.Load() || ctx.Err() != nil {
			return err
		}
		closeErr := r.closeSession(a, session)
		cleaned, cleanupConfirmed = true, closeErr == nil
		if closeErr != nil {
			return errors.Join(closeErr, err)
		}
		continued, transitionErr := r.closeQuotaAttempt(ctx, a, &t, binding, quota.Failure.Reason)
		if transitionErr != nil || !continued {
			return errors.Join(err, transitionErr)
		}
		if _, selectErr := r.selectStep(ctx, preparedBot, t); selectErr != nil {
			return err
		}
		return r.executeAttempt(ctx, a, t, &preparedBot)
	}
	if err = r.appendAttempt(ctx, a, &t, pa.AttemptAnswered, nil, nil); err != nil {
		return err
	}
	err = r.closeSession(a, session)
	cleaned = true
	cleanupConfirmed = err == nil
	if err != nil {
		return err
	}
	if err = r.appendAttempt(ctx, a, &t, pa.AttemptClosed, nil, nil); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped.Load() {
		return runtimeFailure(pa.ExecutionEffectUnknown, "cancelled")
	}
	if err = r.authorized(ctx, a.key, bot.Project); err != nil {
		return err
	}
	err = r.finishLocked(ctx, a, t, pa.TurnSucceeded, result.FinalText, nil)
	if err != nil && (t.RoutineRunID != "" || t.ExchangeID != "") {
		a.completion = &routineCompletion{turn: t, text: result.FinalText}
		r.retain(a, err)
	}
	return err
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
	if source.Room != nil {
		return nil, runtimeFailure(pa.ScopeDenied, "quote_turn")
	}
	if source.Fence.Key != fence.Key || source.Fence.Project != fence.Project || source.Fence.ConversationID != fence.ConversationID {
		return nil, runtimeFailure(pa.ScopeDenied, "quote_turn")
	}
	for _, sourceID := range []string{message.ID, source.ID, source.MessageID} {
		if slices.Contains(memory.ForgottenSourceIDs, sourceID) {
			return nil, runtimeFailure(pa.ContextStale, "forgotten_quote")
		}
	}
	// An acknowledged unknown turn keeps the same provenance doubt.
	effectUnknown := source.State == pa.TurnUnknown || source.Failure != nil && source.Failure.Code == pa.ExecutionEffectUnknown
	if memory.ForgottenThrough > 0 && (source.MemoryRevision < memory.ForgottenThrough || effectUnknown) {
		return nil, runtimeFailure(pa.ContextStale, "quote_memory_revision")
	}
	if source.MemoryRevision > memory.Revision {
		return nil, runtimeFailure(pa.ContextStale, "quote_memory_revision")
	}
	text := message.Text
	if text == "" {
		for i, file := range message.Files {
			if i > 0 {
				text += " · "
			}
			text += file.Name
		}
	}
	if strings.TrimSpace(text) == "" && len(message.Attachments) > 0 {
		text = "[Image attachment / 圖片附件]"
	}
	if err := pa.ValidateMessage(text); err != nil {
		return nil, err
	}
	return &PromptQuote{MessageID: message.ID, Text: text}, nil
}

func (r *Engine) finishLocked(ctx context.Context, a *runtimeActor, t pa.Turn, state pa.TurnState, text string, cause error) error {
	for {
		err := r.finishOnceLocked(ctx, a, t, state, text, cause)
		if !collaborationAggregateConflict(err) {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// Rebuild only this uncommitted terminal mutation from current facts.
		// This does not reopen, Send, or repeat any native effect.
	}
}

func (r *Engine) finishOnceLocked(ctx context.Context, a *runtimeActor, t pa.Turn, state pa.TurnState, text string, cause error) error {
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
	if state == pa.TurnSucceeded && t.Network != nil {
		if r.o.NetworkCheck == nil {
			state = pa.TurnUnknown
			cause = runtimeFailure(pa.Unsupported, "network_gate")
		} else if e := r.o.NetworkCheck(ctx, t); e != nil {
			state = pa.TurnUnknown
			cause = e
		}
	}
	if state == pa.TurnSucceeded {
		if err = pa.CheckFence(t.Fence, currentFence(bot, c)); err != nil {
			return err
		}
	}
	t.State = state
	if cause != nil {
		field := "execution"
		if state == pa.TurnUnknown {
			field = r.unknownField()
		}
		// Preserve a planned restart's recovery marker. Other interruptions
		// retain allowlisted diagnostics without changing their effect state.
		if field == "execution" {
			field = executionFailureField(cause)
		}
		t.Failure = &pa.Failure{Code: pa.Code(cause), Field: field}
	}
	if c.ActiveTurnID == t.ID {
		c.ActiveTurnID = ""
	}
	var messages []pa.Message
	if state == pa.TurnSucceeded && t.Origin != pa.BotRequestOrigin {
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
	if state == pa.TurnSucceeded {
		if notice := attemptSwitchNotice(export, t, bot.Agent, bot.LoginProfile); notice != "" {
			messages = append(messages, pa.Message{ID: "switch-" + t.ID, Fence: t.Fence, TurnID: t.ID, Kind: pa.NoticeMessage, Text: notice, At: r.o.Clock()})
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
		offer.State = terminalPermissionState(offer, state, r.o.Clock())
		permissions = append(permissions, offer)
		p := offer
		events = append(events, pa.Event{Fence: t.Fence, Kind: pa.PermissionAnswered, Permission: &p})
	}
	for i := range messages {
		events = append(events, pa.Event{Fence: t.Fence, Kind: pa.MessageCreated, Message: &messages[i]})
	}
	reacted := false
	for _, record := range export.Reactions {
		if record.MessageID == t.MessageID && record.Actor.Kind == "bot" && record.Actor.Bot != nil && *record.Actor.Bot == bot.Key && len(record.Actor.Emojis) == 1 {
			for _, receipt := range export.Receipts {
				if receipt.Command.Operation == pa.ReactOperation && strings.HasPrefix(receipt.Command.IdempotencyKey, "native-react-"+t.ID+"-") && receipt.ResourceID == t.MessageID && receipt.State == pa.EffectDelivered {
					reacted = true
				}
			}
		}
	}
	if state == pa.TurnSucceeded && len(messages) == 0 && text == "" && !reacted {
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
	var run *pa.RoutineRun
	if t.RoutineRunID != "" {
		if export.Bot.Key == (pa.BotKey{}) {
			export, err = r.o.Repository.Export(ctx, a.key)
			if err != nil {
				return err
			}
		}
		for _, v := range export.RoutineRuns {
			if v.ID == t.RoutineRunID {
				copy := v
				copy.State = state
				copy.NotStartedProved = false
				at := r.o.Clock()
				copy.FinishedAt = &at
				copy.Published = state != pa.TurnUnknown
				run = &copy
				break
			}
		}
		if run == nil {
			return runtimeFailure(pa.NotFound, "routine_run")
		}
	}
	clearRest := pa.Agent("")
	if state == pa.TurnSucceeded && len(t.Attempts) > 0 {
		clearRest = t.Attempts[len(t.Attempts)-1].Agent
	}
	clearProfile := ""
	if len(t.Attempts) > 0 {
		clearProfile = t.Attempts[len(t.Attempts)-1].LoginProfile
	}
	m := Mutation{ClearLoginProfile: clearProfile, ClearAgentRest: clearRest, Command: cmd, Conversation: &c, Turn: &t, RoutineRun: run, Messages: messages, Permissions: permissions, Receipt: pa.Receipt{State: effect, ResourceID: t.ID}, Events: events}
	if run != nil && run.Source == "follow_up" {
		for _, d := range export.Routines {
			if d.ID == run.RoutineID && !d.Deleted {
				d.Deleted, d.Enabled = true, false
				d.Version++
				m.Routine = &d
				break
			}
		}
	}
	if t.ExchangeID != "" {
		var p CollaborationRepository
		p, err = r.collaboration()
		if err != nil {
			return err
		}
		m, err = r.prepareMutation(ctx, bot, m)
		if err != nil {
			return err
		}
		_, err = p.FinishExchange(ctx, m, text)
	} else if t.Room != nil {
		err = r.finishRoomTurn(ctx, bot, m, text)
	} else {
		_, err = r.mutate(ctx, bot, m)
	}
	return err
}

func (r *Engine) incoming(ctx context.Context, a *runtimeActor, e ProtocolEvent) error {
	for {
		err := r.incomingOnce(ctx, a, e)
		if !collaborationAggregateConflict(err) {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// The event identity and receipt stay fixed. Reauthorization, fence,
		// state, permission and replay checks all run again before Commit.
	}
}

func (r *Engine) incomingOnce(ctx context.Context, a *runtimeActor, e ProtocolEvent) error {
	a.mu.Lock()
	locked := true
	defer func() {
		if locked {
			a.mu.Unlock()
		}
	}()
	if a.stopped.Load() || e.ID == "" || e.TurnID != a.turnID {
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
	if c.ActiveTurnID != e.TurnID {
		return runtimeFailure(pa.StaleVersion, "incoming_active_turn")
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
	if pa.IsPeerRequest(t.Origin) && (e.Message != "" || e.Output != nil) {
		return runtimeFailure(pa.ScopeDenied, "bot_request_tools")
	}
	cmd := r.command(a.key, pa.SendOperation, "runtime-event-"+t.ID+"-"+e.ID, e)
	if rec, err := r.replay(ctx, cmd, pa.SendOperation); err != nil {
		return err
	} else if rec != nil {
		return nil
	}
	m := Mutation{Command: cmd, Receipt: pa.Receipt{State: pa.EffectDelivered, ResourceID: e.ID}}
	count := 0
	permissionWaitStarted := false
	if e.Message != "" {
		count++
		if err = pa.ValidateMessage(e.Message); err != nil {
			return err
		}
		if e.Output != nil {
			if err := pa.ValidateOutput(*e.Output); err != nil {
				return err
			}
		}
		msg := pa.Message{ID: "explicit-" + t.ID + "-" + e.ID, Fence: e.Fence, TurnID: t.ID, Kind: pa.AssistantMessage, Text: e.Message, Output: e.Output, At: r.o.Clock()}
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
		if strings.HasPrefix(p.NativeRequestID, sourcePermissionPrefix) || strings.HasPrefix(p.ID, sourcePermissionPrefix) {
			return runtimeFailure(pa.ScopeDenied, "source_permission_namespace")
		}
		if a.pendingPermissions >= capacity.MaximumBotPermissions {
			return runtimeFailure(pa.CapacityFull, "pending_permissions")
		}
		if a.closing {
			return runtimeFailure(pa.StaleGeneration, "permission_session")
		}
		if p.Fence != e.Fence || p.TurnID != t.ID || p.NativeRequestID == "" || p.ID == "" {
			return runtimeFailure(pa.ScopeDenied, "permission")
		}
		p.State = pa.PermissionPending
		m.Permissions = []pa.PermissionOffer{p}
		permissionWaitStarted = t.State != pa.TurnWaitingPermission
		t.State = pa.TurnWaitingPermission
		m.Turn = &t
		m.Events = append(m.Events, pa.Event{Fence: e.Fence, Kind: pa.PermissionOffered, Permission: &p})
	}
	if count != 1 {
		return runtimeFailure(pa.Invalid, "incoming_payload")
	}
	tool := e.Permission != nil || e.Trace != nil && (e.Trace.Category == "tool_call" || e.Trace.Category == "tool_call_update" || e.Trace.Category == "tool_activity")
	if (!e.ToolDiscovery && observeAttempt(&t, tool)) || permissionWaitStarted {
		m.Turn = &t
		m.Events = append(m.Events, pa.Event{Fence: e.Fence, Kind: pa.TurnUpdated, Turn: &t})
	}
	_, err = r.mutate(ctx, bot, m)
	if err != nil {
		return err
	}
	if e.Permission != nil {
		a.pendingPermissions++
		a.permissionFence = e.Fence
	}
	// As Codync's "Approve automatically", an Approve Bot allows each request
	// with the agent's allow-once option; AutoRead allows only read-only ones.
	if e.Permission != nil && (bot.PermissionPolicy == pa.AutoRead || bot.PermissionPolicy == pa.Approve) {
		for _, option := range e.Permission.Options {
			if option.Allow && option.Kind == pa.AllowOnce && (option.ReadOnly || bot.PermissionPolicy == pa.Approve) {
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
	if a.stopped.Load() || q.TurnID != c.ActiveTurnID || q.TurnID == "" {
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
	workMutation := Mutation{Command: q.Command, Receipt: pa.Receipt{State: pa.EffectUnknown, ResourceID: q.TurnID}}
	turn, readErr := r.o.Repository.Turn(ctx, a.key, q.TurnID)
	if readErr != nil {
		a.mu.Unlock()
		return pa.WorkLink{}, readErr
	}
	if observeAttempt(&turn, true) {
		workMutation.Turn = &turn
		workMutation.Events = []pa.Event{{Fence: fence, Kind: pa.TurnUpdated, Turn: &turn}}
	}
	_, err = r.mutate(ctx, bot, workMutation)
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
	if link.Fence != fence || link.TurnID != q.TurnID || link.ID == "" || link.TaskID == "" {
		return link, runtimeFailure(pa.ScopeDenied, "work_link")
	}
	export, err := r.o.Repository.Export(ctx, a.key)
	if err != nil {
		return link, err
	}
	for _, persisted := range export.WorkLinks {
		if persisted.ID == link.ID {
			if persisted.Fence != fence || persisted.TurnID != q.TurnID || persisted.TaskID != link.TaskID {
				return link, runtimeFailure(pa.ScopeDenied, "persisted_work_owner")
			}
			return persisted, nil
		}
	}
	return link, runtimeFailure(pa.OutcomeUnknown, "work_not_persisted")
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
	// A card raised in a member's room turn is the member's own (its fence,
	// checked above), and is answerable only while that room still wants
	// the turn.
	if err = r.recheckRoomCard(ctx, p); err != nil {
		return pa.Receipt{}, err
	}
	if q.Command.Key != q.Answer.Fence.Key || a.turnID != p.TurnID || a.session == nil || a.stopped.Load() || a.closing {
		return pa.Receipt{}, runtimeFailure(pa.StaleGeneration, "permission_session")
	}
	if strings.HasPrefix(p.NativeRequestID, sourcePermissionPrefix) {
		return r.answerSourcePermission(ctx, a, bot, p, q)
	}
	// Persist unknown before the external write. A crash here is inspectable,
	// never an automatic repeat of a native permission response.
	p.State = pa.PermissionUnknown
	p.SelectedOptionID = q.Answer.OptionID
	receipt, err := r.mutate(ctx, bot, Mutation{Command: q.Command, Permissions: []pa.PermissionOffer{p}, Receipt: pa.Receipt{State: pa.EffectUnknown, ResourceID: p.ID}, Events: []pa.Event{{Fence: p.Fence, Kind: pa.PermissionAnswered, Permission: &p}}})
	if err != nil {
		return pa.Receipt{}, err
	}
	a.pendingPermissions--
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
	events := []pa.Event{{Fence: p.Fence, Kind: pa.PermissionAnswered, Permission: &p}}
	if a.pendingPermissions == 0 {
		t.State = pa.TurnRunning
		events = append(events, pa.Event{Fence: p.Fence, Kind: pa.TurnUpdated, Turn: &t})
	}
	cmd := r.command(a.key, pa.AnswerOperation, "runtime-answer-"+p.ID, p)
	delivered, err := r.mutate(ctx, bot, Mutation{Command: cmd, Turn: &t, Permissions: []pa.PermissionOffer{p}, Receipt: pa.Receipt{State: pa.EffectDelivered, ResourceID: p.ID}, Events: events})
	if err != nil {
		return receipt, err
	}
	// The person's answer reached Claude Code: report that, not the unknown
	// receipt written before the native write, which locked the chat.
	return delivered, nil
}

func (r *Engine) Stop(ctx context.Context, q StopRequest) (receipt pa.Receipt, err error) {
	// Registered before acquire/unlock defers: filesystem cleanup runs only
	// after every runtime lock is released, including exact receipt replays.
	defer func() {
		if err == nil {
			err = r.discardTakenBackInputs(ctx, q, receipt)
		}
	}()
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
		return r.unqueueLocked(ctx, a, bot, q)
	}
	a.stopped.Store(true)
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
		// Codync's own-chat Stop keeps a late room turn queued: the member
		// still owes that room its answer (pa.AfterOwnChatStop).
		if keepsAfterOwnChatStop(t, a.turnID) {
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
	if err = r.cancelDueFollowUpsLocked(ctx, a); err != nil {
		return pa.Receipt{}, err
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
	receipt, err = r.mutate(ctx, bot, Mutation{Command: q.Command, Receipt: pa.Receipt{State: state, ResourceID: q.TurnID}})
	if a.session != nil {
		_ = a.session.Cancel(ctx)
	}
	return receipt, err
}

// unqueueLocked is Codync's unqueue (host/src/agent/bot/steer.rs): stopping a
// turn that is still queued takes that one message back before it is sent,
// and leaves the running turn and the rest of the queue alone. The turn ends
// cancelled with no failure, which is how a removed message differs from one
// a Stop cancelled. The caller holds a.mu.
func (r *Engine) unqueueLocked(ctx context.Context, a *runtimeActor, bot pa.Bot, q StopRequest) (pa.Receipt, error) {
	pending, err := r.o.Repository.PendingTurns(ctx, a.key)
	if err != nil {
		return pa.Receipt{}, err
	}
	for _, t := range pending {
		if t.ID != q.TurnID {
			continue
		}
		if t.State != pa.TurnQueued || a.turnID == t.ID {
			break
		}
		if err = r.finishLocked(ctx, a, t, pa.TurnCancelled, "", nil); err != nil {
			return pa.Receipt{}, err
		}
		if bot, err = r.o.Repository.Bot(ctx, a.key); err != nil {
			return pa.Receipt{}, err
		}
		return r.mutate(ctx, bot, Mutation{Command: q.Command, Receipt: pa.Receipt{State: pa.EffectDelivered, ResourceID: t.ID}})
	}
	return pa.Receipt{}, runtimeFailure(pa.StaleVersion, "stop_turn")
}

func (r *Engine) Disable(ctx context.Context, project pa.ProjectSelection) error {
	return r.shutdown(ctx, &project)
}

// images reads a message's pictures; a picture that cannot be read fails the
// turn before anything is sent, rather than sending the text alone.
func (r *Engine) images(key pa.BotKey, attachments []pa.Attachment) ([]PromptImage, error) {
	if len(attachments) == 0 {
		return nil, nil
	}
	if r.o.Attachments == nil {
		return nil, runtimeFailure(pa.Unsupported, "attachments")
	}
	out := make([]PromptImage, 0, len(attachments))
	for _, a := range attachments {
		data, err := r.o.Attachments.Read(key, a)
		if err != nil {
			return nil, runtimeFailure(pa.NotFound, "attachment")
		}
		out = append(out, PromptImage{MediaType: a.MediaType, Data: data})
	}
	return out, nil
}

// ResumeInterrupted resumes, as Recover does, a turn the daemon's own
// shutdown cut off (RestartField) when it is the Bot's only pending turn and
// its message is under an hour old. Anything else waits for the person.
func (r *Engine) ResumeInterrupted(ctx context.Context, key pa.BotKey) error {
	pending, err := r.o.Repository.PendingTurns(ctx, key)
	if err != nil || len(pending) != 1 {
		return err
	}
	t := pending[0]
	if pa.IsPeerRequest(t.Origin) || len(t.Attempts) > 0 || t.RoutineRunID != "" || t.State != pa.TurnUnknown || t.Failure == nil || t.Failure.Field != RestartField {
		return nil
	}
	x, err := r.o.Repository.Export(ctx, key)
	if err != nil {
		return err
	}
	if interruptedPermission(t, x.Permissions) {
		return nil
	}
	for _, m := range x.Messages {
		if t.RoutineRunID == "" && m.ID == t.MessageID && r.o.Clock().Sub(m.At) < staleResume {
			return r.resumeInterrupted(ctx, key, t)
		}
	}
	return nil
}

// currentForStart re-reads the Bot and its conversation just before a turn
// starts. Only the versions may differ from what the start was prepared
// against: the project, agent, permission policy, instructions, conversation
// identity, generation, binding and active turn must all be unchanged.
func (r *Engine) currentForStart(ctx context.Context, key pa.BotKey, prepared pa.Bot, preparedConversation pa.Conversation) (pa.Bot, pa.Conversation, error) {
	bot, err := r.o.Repository.Bot(ctx, key)
	if err != nil {
		return pa.Bot{}, pa.Conversation{}, err
	}
	c, err := r.o.Repository.Conversation(ctx, key, bot.ConversationID)
	if err != nil {
		return pa.Bot{}, pa.Conversation{}, err
	}
	if !sameExecutionConfig(bot, prepared) ||
		c.ID != preparedConversation.ID || c.Generation != preparedConversation.Generation ||
		c.BindingID != preparedConversation.BindingID || c.ActiveTurnID != preparedConversation.ActiveTurnID {
		return pa.Bot{}, pa.Conversation{}, runtimeFailure(pa.StaleVersion, "start_changed")
	}
	return bot, c, nil
}

func sameExecutionConfig(a, b pa.Bot) bool {
	return a.Key == b.Key && a.Project == b.Project && a.Agent == b.Agent && a.LoginProfile == b.LoginProfile && pa.SameExtensions(a.Extensions, b.Extensions) && a.PermissionPolicy == b.PermissionPolicy && a.Mode == b.Mode && a.FallbackAgents == b.FallbackAgents && a.FallbackSteps == b.FallbackSteps && a.Instructions == b.Instructions && pa.SameRuntimeSettings(a.RuntimeSettings, b.RuntimeSettings) && a.ConversationID == b.ConversationID
}
