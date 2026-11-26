package projectassistant

import (
	"context"
	"strings"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// submitState is what Submit has established about one submission. Every
// step after resolveSubmissionFiles runs under the actor lock that Submit
// holds for the whole admission.
type submitState struct {
	s     Submission
	files []pa.InputFile
	owner InputOwner
	bot   pa.Bot
	c     pa.Conversation
	// lost are running turns whose worker was lost; their peer replies are
	// woken after the actor lock is released.
	lost           []pa.Turn
	pending        []pa.Turn
	unknown        []pa.Turn
	recoverySource string
	acknowledge    []pa.Turn
	person         bool
}

func (r *Engine) Submit(ctx context.Context, s Submission) (pa.Receipt, error) {
	if err := r.checkSubmission(ctx, s); err != nil {
		return pa.Receipt{}, err
	}
	// Resolve and hash outside the actor. Receipt replay precedes file IO so an
	// uncertain durable acknowledgement never submits another native effect.
	st := &submitState{s: s, owner: s.InputOwner}
	if replayed, err := r.resolveSubmissionFiles(ctx, st); err != nil || replayed != nil {
		if err != nil {
			return pa.Receipt{}, err
		}
		return *replayed, nil
	}
	a, err := r.acquire(s.Command.Key)
	if err != nil {
		return pa.Receipt{}, notAdmitted(err)
	}
	defer r.release(a)
	// A lost worker's peer reply wakes its sender after this lock is released.
	defer func() {
		for _, t := range st.lost {
			r.wakeExchangeReply(t)
		}
	}()
	a.mu.Lock()
	defer a.mu.Unlock()
	return r.submitLocked(ctx, a, st)
}

// checkSubmission refuses a submission whose shape or sender cannot be
// admitted, before any state is read.
func (r *Engine) checkSubmission(ctx context.Context, s Submission) error {
	if err := s.Feature.CheckWrite(); err != nil {
		return err
	}
	if err := pa.CheckSource(s.Source, true); err != nil {
		return err
	}
	if s.Command.Key != s.Message.Fence.Key || s.Source.MachineID != s.Command.Key.MachineID {
		return runtimeFailure(pa.ScopeDenied, "submission")
	}
	if err := pa.ValidateInputSend(s.Message.Text, s.Message.Attachments, s.Message.UploadIDs); err != nil {
		return err
	}
	if s.Message.Handoff != nil {
		if err := pa.ValidateHandoffRef(*s.Message.Handoff); err != nil {
			return err
		}
		if s.Notice || s.Network != nil || s.Room != nil || s.AuthorityCheck == nil {
			return runtimeFailure(pa.ScopeDenied, "handoff_input")
		}
	}
	if s.Network != nil && (!s.Notice || s.Room != nil || len(s.Message.Attachments) > 0 || len(s.Message.UploadIDs) > 0 || s.Message.QuoteMessageID != "") {
		return runtimeFailure(pa.ScopeDenied, "network_submission")
	}
	if s.Room != nil {
		if err := r.admitRoomSubmission(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

// resolveSubmissionFiles resolves uploaded files against the Bot's current
// configuration. A replayed receipt means the submission was already
// accepted and nothing else is done.
func (r *Engine) resolveSubmissionFiles(ctx context.Context, st *submitState) (*pa.Receipt, error) {
	s := st.s
	if len(s.Message.UploadIDs) == 0 {
		return nil, nil
	}
	if receipt, e := r.replay(ctx, s.Command, pa.SendOperation); e != nil || receipt != nil {
		return receipt, e
	}
	if r.o.Inputs == nil {
		return nil, runtimeFailure(pa.Unsupported, "input_files")
	}
	if s.InputCheck == nil {
		return nil, runtimeFailure(pa.ScopeDenied, "input_authority")
	}
	config, e := r.o.Repository.Export(ctx, s.Command.Key)
	if e != nil {
		return nil, e
	}
	st.owner.Revision = config.Bot.Instructions.Revision
	st.owner.Lifecycle = inputLifecycle(config)
	for _, id := range s.Message.UploadIDs {
		file, _, e := r.o.Inputs.Resolve(ctx, s.Message.Fence, id, st.owner)
		if e != nil {
			return nil, e
		}
		st.files = append(st.files, file)
	}
	return nil, nil
}

func (r *Engine) submitLocked(ctx context.Context, a *runtimeActor, st *submitState) (pa.Receipt, error) {
	if replayed, err := r.readSubmissionTargetLocked(ctx, a, st); err != nil || replayed != nil {
		if err != nil {
			return pa.Receipt{}, err
		}
		return *replayed, nil
	}
	if err := r.checkSubmissionInputsLocked(ctx, a, st); err != nil {
		return pa.Receipt{}, err
	}
	if err := r.reconcileLostWorkerForSubmitLocked(ctx, a, st); err != nil {
		return pa.Receipt{}, err
	}
	if err := r.queueSubmissionLocked(ctx, a, st); err != nil {
		return pa.Receipt{}, err
	}
	if err := r.admitSubmissionLocked(ctx, a, st); err != nil {
		return pa.Receipt{}, err
	}
	return r.commitSubmissionLocked(ctx, a, st)
}

// readSubmissionTargetLocked reads the Bot and conversation the submission
// is for and binds it to the live fence. A replayed receipt means the
// submission was already accepted.
func (r *Engine) readSubmissionTargetLocked(ctx context.Context, a *runtimeActor, st *submitState) (*pa.Receipt, error) {
	bot, err := r.o.Repository.Bot(ctx, a.key)
	if err != nil {
		return nil, err
	}
	r.setProject(a, bot.Project)
	if err = r.authorized(ctx, a.key, bot.Project); err != nil {
		return nil, err
	}
	if receipt, e := r.replay(ctx, st.s.Command, pa.SendOperation); e != nil || receipt != nil {
		return receipt, e
	}
	c, err := r.o.Repository.Conversation(ctx, a.key, bot.ConversationID)
	if err != nil {
		return nil, err
	}
	liveFence := currentFence(bot, c)
	if err = pa.CheckSendFence(st.s.Message.Fence, liveFence); err != nil {
		// Typed text targets the conversation, which survives a native
		// process rebind. Inputs carrying permissions or files still need
		// the exact current execution generation.
		if e := r.checkPlainMessageFence(ctx, st.s, liveFence); e != nil {
			return nil, e
		}
		st.s.Message.Fence = liveFence
	}
	st.bot, st.c = bot, c
	return nil, nil
}

// checkSubmissionInputsLocked checks uploaded files are still owned by this
// configuration and unbound, and that a quoted message can be quoted.
func (r *Engine) checkSubmissionInputsLocked(ctx context.Context, a *runtimeActor, st *submitState) error {
	s, bot, c := st.s, st.bot, st.c
	if len(st.files) > 0 {
		if e := s.InputCheck(ctx); e != nil {
			return e
		}
		if bot.Instructions.Revision != st.owner.Revision {
			return runtimeFailure(pa.StaleVersion, "upload_configuration")
		}
		x, e := r.o.Repository.Export(ctx, a.key)
		if e != nil {
			return e
		}
		if inputLifecycle(x) != st.owner.Lifecycle {
			return runtimeFailure(pa.StaleVersion, "upload_stopped")
		}
		for _, m := range x.Messages {
			for _, old := range m.Files {
				for _, file := range st.files {
					if old.ID == file.ID {
						return runtimeFailure(pa.IdempotencyConflict, "upload_bound")
					}
				}
			}
		}
	}
	if s.Message.QuoteMessageID != "" {
		export, e := r.o.Repository.Export(ctx, a.key)
		if e != nil {
			return e
		}
		memory, e := r.o.Memory.Current(ctx, a.key)
		if e != nil {
			return e
		}
		if _, e = resolvePromptQuote(export, s.Message.QuoteMessageID, currentFence(bot, c), memory); e != nil {
			return e
		}
	}
	return nil
}

// reconcileLostWorkerForSubmitLocked records a running row with no worker as
// unknown. The worker ended on an error, or this start has not recovered it
// yet; it is never replayed, and new messages are not refused behind it.
func (r *Engine) reconcileLostWorkerForSubmitLocked(ctx context.Context, a *runtimeActor, st *submitState) error {
	if a.working {
		return nil
	}
	lost, err := r.reconcileLostWorkerLocked(ctx, a.key)
	st.lost = lost
	if err != nil {
		return err
	}
	if len(lost) == 0 {
		return nil
	}
	if st.bot, err = r.o.Repository.Bot(ctx, a.key); err != nil {
		return err
	}
	st.c, err = r.o.Repository.Conversation(ctx, a.key, st.bot.ConversationID)
	return err
}

// queueSubmissionLocked decides where the submission stands against the
// queue: which unknown turns it acknowledges, or why it is not admitted.
func (r *Engine) queueSubmissionLocked(ctx context.Context, a *runtimeActor, st *submitState) error {
	s := st.s
	pending, err := r.o.Repository.PendingTurns(ctx, a.key)
	if err != nil {
		return err
	}
	st.pending = pending
	// An unknown turn is never replayed. A new message is a new turn: it
	// cannot repeat an old one, so the person's own message is never refused
	// for one. It acknowledges the unknown turns and waits in the queue.
	waiting := 0
	for _, t := range pending {
		if t.State == pa.TurnUnknown {
			st.unknown = append(st.unknown, t)
			continue
		}
		if source := pa.RestartSourceTurn(t); source != "" {
			st.recoverySource = source
		}
		waiting++
	}
	if s.LeadCall && (!st.bot.Lead || a.working || st.c.ActiveTurnID != "" || len(pending) > 0) {
		return runtimeFailure(pa.ReadOnly, "lead_call_busy")
	}
	st.person = personMessage(s)
	if len(st.unknown) > 0 && s.Network != nil {
		// Nothing was admitted; the network sender retries.
		return runtimeFailure(pa.CapacityFull, "network_unknown_barrier")
	}
	if s.RestartSource != "" {
		unknown := st.unknown
		if !s.Notice || s.Command.IdempotencyKey != pa.RestartContinuationPrefix+s.RestartSource ||
			len(unknown) != 1 || unknown[0].ID != s.RestartSource || !restartInterrupted(unknown[0]) {
			return runtimeFailure(pa.ContextStale, "restart_source")
		}
		return nil
	}
	if len(st.unknown) == 0 {
		return nil
	}
	return st.acknowledgeUnknown(waiting)
}

// acknowledgeUnknown chooses the unknown turns a submission settles, or
// refuses one that may not pass them.
func (st *submitState) acknowledgeUnknown(waiting int) error {
	s := st.s
	review := s.Notice && strings.HasPrefix(s.Command.IdempotencyKey, pa.WorkCompletionPrefix)
	switch {
	case st.person:
		// A queued or running automatic continuation still owns its
		// source; the message waits behind it in the existing queue.
		for _, t := range st.unknown {
			if t.ID != st.recoverySource {
				st.acknowledge = append(st.acknowledge, t)
			}
		}
	case review:
		// A dispatched task's review waits behind the unknown turn; the
		// person or a declined restart continuation releases the queue.
	case waiting == 0:
		st.acknowledge = st.unknown
	case s.Room != nil:
		// Nothing was admitted; the room ticket is offered again later.
		return runtimeFailure(pa.CapacityFull, "room_unknown_barrier")
	case !s.Notice:
		// Compaction and other input without the person's grant does not
		// settle what the person has not seen. Nothing was admitted.
		return runtimeFailure(pa.CapacityFull, "unknown_turn_barrier")
	default:
		// A host notice waits behind the queue without settling what the
		// person has not seen.
	}
	return nil
}

// admitSubmissionLocked checks the Bot can run the submission now and that
// the sender's grant still holds after the capability read.
func (r *Engine) admitSubmissionLocked(ctx context.Context, a *runtimeActor, st *submitState) error {
	s := st.s
	if s.Compact {
		if err := r.admitCompaction(ctx, a, st.bot, st.c, st.pending, s); err != nil {
			return err
		}
	}
	// Capability admission is local evidence inspection, not a model request.
	caps, err := r.o.Provider.Capabilities(ctx, st.bot)
	if err != nil {
		return err
	}
	if err = pa.CheckExecutionCapabilities(caps, currentFence(st.bot, st.c)); err != nil {
		return err
	}
	if err = r.authorized(ctx, a.key, st.bot.Project); err != nil {
		return err
	}
	// The capability read can take seconds; admit against the current Bot
	// when only its version moved, as a turn's start does.
	if st.bot, st.c, err = r.currentForStart(ctx, a.key, st.bot, st.c); err != nil {
		return err
	}
	// The sender's grant can change while capabilities are read; nothing,
	// not even an acknowledgement of an unknown turn, is written under the
	// grant the request arrived with once it has been replaced.
	if s.AuthorityCheck != nil {
		if err = s.AuthorityCheck(ctx); err != nil {
			return err
		}
	}
	if len(st.files) > 0 && st.bot.Instructions.Revision != st.owner.Revision {
		return runtimeFailure(pa.StaleVersion, "upload_configuration")
	}
	return nil
}

// commitSubmissionLocked writes the message and its queued turn, with any
// acknowledgements and the decision it answers, and starts the queue.
func (r *Engine) commitSubmissionLocked(ctx context.Context, a *runtimeActor, st *submitState) (pa.Receipt, error) {
	s, bot := st.s, st.bot
	var binding *pa.ExecutionBinding
	if st.c.BindingID == "" {
		memory, e := r.o.Memory.Current(ctx, a.key)
		if e != nil {
			return pa.Receipt{}, e
		}
		st.c.BindingID = r.o.ID()
		st.c.Generation = 1
		b := pa.ExecutionBinding{Fence: currentFence(bot, st.c), Agent: bot.Agent, LoginProfile: bot.LoginProfile, MemoryRevision: memory.Revision, ForgottenThrough: memory.ForgottenThrough, InstructionRevision: bot.Instructions.Revision}
		binding = &b
	}
	var acknowledged []string
	var events []pa.Event
	for _, t := range st.acknowledge {
		if st.c.ActiveTurnID == t.ID {
			// A stale pointer at an unknown turn holds no running work.
			st.c.ActiveTurnID = ""
		}
		t = t.AcknowledgeUnknown()
		acknowledged = append(acknowledged, t.ID)
		events = append(events, pa.Event{Fence: t.Fence, Kind: pa.TurnUpdated, Turn: &t})
	}
	if len(st.files) > 0 {
		if e := s.InputCheck(ctx); e != nil {
			return pa.Receipt{}, e
		}
	}
	fence := currentFence(bot, st.c)
	turn, kind, err := r.submissionTurn(st, fence)
	if err != nil {
		return pa.Receipt{}, err
	}
	decisions, decisionEvents, err := submissionDecision(s, &turn)
	if err != nil {
		return pa.Receipt{}, err
	}
	message := pa.Message{ID: turn.MessageID, Fence: fence, TurnID: turn.ID, Kind: kind, Text: s.Message.Text, QuoteMessageID: s.Message.QuoteMessageID, At: r.o.Clock(), Attachments: s.Message.Attachments, Files: st.files}
	if s.Decision != nil {
		message.DecisionReply = pa.NewDecisionReply(*s.Decision)
	}
	switch {
	case s.RestartSource != "":
		message.Notice = &pa.NoticeRef{Key: pa.NoticeRestartContinuation, Cause: pa.WorkCompletionTask(st.unknown[0])}
	case pa.WorkCompletionTask(turn) != "":
		message.Notice = &pa.NoticeRef{Key: pa.NoticeWorkReview, Cause: pa.WorkCompletionTask(turn)}
	}
	events = append(events, pa.Event{Fence: fence, Kind: pa.MessageCreated, Message: &message}, pa.Event{Fence: fence, Kind: pa.TurnUpdated, Turn: &turn})
	receipt, err := r.commitHandoffAwareInput(ctx, bot, s, Mutation{
		Command: s.Command, Conversation: &st.c, Binding: binding, Turn: &turn, AcknowledgeUnknown: acknowledged,
		Messages: []pa.Message{message}, Permissions: decisions, Receipt: pa.Receipt{State: pa.EffectAccepted, ResourceID: turn.ID},
		Events: append(events, decisionEvents...),
	})
	if err != nil {
		return pa.Receipt{}, err
	}
	if s.Message.Handoff != nil && receipt.ResourceID == turn.ID {
		a.handoffAuthority = handoffInputAuthority{TurnID: turn.ID, Check: afterRequest(ctx, s.AuthorityCheck)}
	}
	r.setProject(a, bot.Project)
	if receipt.ResourceID == turn.ID {
		r.trySteerFollowUpLocked(ctx, a, bot, st.c, turn, message, s)
	}
	r.kickLocked(a)
	return receipt, nil
}

// submissionTurn is the queued turn for a submission and the kind of the
// message it carries.
func (r *Engine) submissionTurn(st *submitState, fence pa.Fence) (pa.Turn, pa.MessageKind, error) {
	s, bot := st.s, st.bot
	turn := pa.Turn{ID: r.o.ID(), Fence: fence, MessageID: r.o.ID(), State: pa.TurnQueued, IdempotencyKey: s.Command.IdempotencyKey,
		ReleasedRestart: st.person && releasesRestart(st.acknowledge, st.recoverySource)}
	turn.Origin = pa.UserOrigin
	kind := pa.UserMessage
	if s.Notice {
		kind = pa.NoticeMessage
		turn.Origin = pa.HostNoticeOrigin
	}
	if s.LeadCall {
		if !bot.Lead || !s.Notice || s.AuthorityCheck == nil || s.Message.Text != pa.LeadCallGreeting ||
			!pa.ValidLeadCallKey(s.Command.IdempotencyKey) || s.Room != nil || s.Network != nil {
			return pa.Turn{}, "", runtimeFailure(pa.ScopeDenied, "lead_call")
		}
		turn.Origin = pa.LeadCallOrigin
		turn.MessageID = pa.LeadCallKeyPrefix + turn.MessageID
	}
	if s.CharterEdit != nil {
		if !bot.Lead || s.Notice || s.Compact || s.LeadCall || s.AuthorityCheck == nil || s.Room != nil || s.Network != nil || s.Decision != nil {
			return pa.Turn{}, "", runtimeFailure(pa.ScopeDenied, "charter_edit")
		}
		edit := *s.CharterEdit
		turn.CharterEdit = &edit
	}
	if s.CharterFix != nil {
		if s.CharterEdit != nil || s.Notice || s.Compact || s.LeadCall || s.AuthorityCheck == nil || s.Room != nil || s.Network != nil || s.Decision != nil || pa.ValidateCharterFix(*s.CharterFix) != nil {
			return pa.Turn{}, "", runtimeFailure(pa.ScopeDenied, "charter_fix")
		}
		fix := *s.CharterFix
		turn.CharterFix = &fix
	}
	if s.Compact {
		turn.Origin = pa.CompactOrigin
		kind = pa.NoticeMessage
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
	return turn, kind, nil
}

// submissionDecision is the answered permission a submission records, if it
// answers one.
func submissionDecision(s Submission, turn *pa.Turn) ([]pa.PermissionOffer, []pa.Event, error) {
	if s.Decision == nil {
		return nil, nil, nil
	}
	if s.Notice || s.Network != nil || s.Room != nil || s.LeadCall || s.AuthorityCheck == nil || s.Decision.Fence.Key != s.Command.Key || s.Decision.State != pa.PermissionResolved {
		return nil, nil, runtimeFailure(pa.ScopeDenied, "decision_input")
	}
	if s.Decision.Decision != nil {
		turn.DecisionExchangeID = s.Decision.Decision.ExchangeID
	}
	answered := *s.Decision
	return []pa.PermissionOffer{answered}, []pa.Event{{Fence: answered.Fence, Kind: pa.PermissionAnswered, Permission: &answered}}, nil
}
