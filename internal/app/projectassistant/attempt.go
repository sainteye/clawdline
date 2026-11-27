package projectassistant

import (
	"context"
	"errors"
	"slices"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// attemptStart is what the locked start of one attempt read, decided and
// committed. Everything after the start runs without the actor lock and
// reads its inputs from here.
type attemptStart struct {
	turn pa.Turn
	// prepared is the Bot as admitted. A fallback attempt and every boundary
	// check compare against it; bot follows the current Bot as the start and
	// the session binding are committed.
	prepared     pa.Bot
	bot          pa.Bot
	effective    pa.Bot // the Bot as the selected fallback step runs it
	step         selectedFallbackStep
	conversation pa.Conversation
	resolution   pa.ProjectResolution
	caps         pa.Capabilities
	current      pa.MemoryRevision
	snapshot     pa.ContextSnapshot
	export       Export
	input        *pa.Message
	quote        *PromptQuote
	binding      pa.ExecutionBinding
	handoffCard  *pa.HandoffCard
	handoffCheck func(context.Context) error
	routineRun   *pa.RoutineRun

	isolatedRoutine bool
	resume          bool
	fromAgent       pa.Agent
	resumedFrom     string
	carryKind       string
	carryAfter      int64
	memoryChanged   bool
	compacted       bool
	roleChanged     bool
	priorUsage      pa.ContextSample
	// charterVersion and charterText are the Project's team charter as this
	// attempt read it; charterChanged says a resumed session heard another
	// version and its next prompt carries this one once.
	charterVersion int64
	charterText    string
	charterChanged bool
	// charterCheck says this prompt asks the Bot, once for this version, to
	// compare the charter with its own memory and instructions.
	charterCheck bool
}

// attemptCleanup closes an opened native session at most once and remembers
// whether the close was confirmed, which decides how a failed attempt is held.
type attemptCleanup struct {
	r         *Engine
	a         *runtimeActor
	session   ProviderSession
	done      bool
	confirmed bool
}

func (c *attemptCleanup) close() error {
	err := c.r.closeSession(c.a, c.session)
	c.done = true
	c.confirmed = err == nil
	return err
}

func (r *Engine) executeAttempt(ctx context.Context, a *runtimeActor, t pa.Turn, original *pa.Bot) (resultErr error) {
	a.mu.Lock()
	s, err := r.startAttemptLocked(ctx, a, t, original)
	a.mu.Unlock()
	if err != nil {
		return err
	}
	if s.turn.Origin == pa.BotRequestOrigin {
		if err = r.startPeerExchange(ctx, s.turn); err != nil {
			return err
		}
	}
	session, entered, err := r.openAttemptSession(ctx, a, s)
	if !entered {
		return err
	}
	if session == nil {
		return r.missingAttemptSession(a, err)
	}
	cleanup := &attemptCleanup{r: r, a: a, session: session}
	defer func() {
		if !cleanup.done {
			resultErr = errors.Join(resultErr, cleanup.close())
		}
		if resultErr != nil {
			resultErr = errors.Join(resultErr, r.holdAttempt(a, s.turn, resultErr, cleanup.confirmed))
		}
	}()
	if err != nil {
		return r.retryAfterRefusedOpen(ctx, a, s, cleanup, err)
	}
	if err = r.bindAttemptSession(ctx, a, s, session); err != nil {
		return err
	}
	if err = r.appendAttempt(ctx, a, &s.turn, pa.AttemptOpened, nil, nil); err != nil {
		return err
	}
	if err = r.authorized(ctx, a.key, s.bot.Project); err != nil {
		return err
	}
	files, err := r.promptFiles(ctx, s.input)
	if err != nil {
		return err
	}
	if err = r.appendAttempt(ctx, a, &s.turn, pa.AttemptSending, nil, nil); err != nil {
		return err
	}
	pending, err := r.sendAttemptPrompt(ctx, a, s, session, files)
	if err != nil {
		return err
	}
	if pending == nil {
		return runtimeFailure(pa.ExecutionEffectUnknown, "missing_pending_prompt")
	}
	if err = r.appendAttempt(ctx, a, &s.turn, pa.AttemptSent, nil, nil); err != nil {
		return err
	}
	result, err := pending.Wait(ctx)
	if err != nil {
		return r.continueAfterQuota(ctx, a, s, cleanup, err)
	}
	return r.finishAnsweredAttempt(ctx, a, s, cleanup, result)
}

// startAttemptLocked admits the turn, prepares its execution and commits the
// start. The caller holds a.mu for the whole call and releases it once,
// whatever the outcome; nothing here unlocks.
func (r *Engine) startAttemptLocked(ctx context.Context, a *runtimeActor, t pa.Turn, original *pa.Bot) (*attemptStart, error) {
	if a.stopped.Load() || ctx.Err() != nil {
		return nil, runtimeFailure(pa.ExecutionEffectUnknown, "cancelled_before_start")
	}
	s := &attemptStart{turn: t}
	if err := r.admitAttemptLocked(ctx, a, s, original); err != nil {
		return nil, err
	}
	if err := r.readAttemptEnvironmentLocked(ctx, a, s, original == nil); err != nil {
		return nil, err
	}
	if err := r.readAttemptInputLocked(ctx, a, s); err != nil {
		return nil, err
	}
	if err := r.chooseAttemptBindingLocked(ctx, s); err != nil {
		return nil, err
	}
	if err := r.commitAttemptStartLocked(ctx, a, s); err != nil {
		return nil, err
	}
	return s, nil
}

// admitAttemptLocked checks that the turn may still run for this Bot and
// conversation, before anything is resolved for it.
func (r *Engine) admitAttemptLocked(ctx context.Context, a *runtimeActor, s *attemptStart, original *pa.Bot) error {
	t := s.turn
	bot, err := r.o.Repository.Bot(ctx, a.key)
	if err != nil {
		return err
	}
	if err = r.authorized(ctx, a.key, bot.Project); err != nil {
		return err
	}
	handoffCard, err := r.handoffForTurn(ctx, t)
	if err != nil {
		return err
	}
	if handoffCard != nil {
		if a.handoffAuthority.TurnID != t.ID || a.handoffAuthority.Check == nil {
			return runtimeFailure(pa.Revoked, "handoff_authority")
		}
		s.handoffCheck = a.handoffAuthority.Check
	}
	s.handoffCard = handoffCard
	if original != nil && !sameExecutionConfig(bot, *original) {
		return runtimeFailure(pa.StaleVersion, "fallback_configuration")
	}
	c, err := r.o.Repository.Conversation(ctx, a.key, bot.ConversationID)
	if err != nil {
		return err
	}
	// Queued turns were admitted against a previous idle snapshot. Bind them
	// to the current process only after checking their immutable ownership;
	// rotating a process must not strand the rest of the durable queue.
	if pa.IsLeadCallTurn(t) && !bot.Lead {
		return runtimeFailure(pa.Revoked, "lead_call")
	}
	if t.Fence.Key != bot.Key || t.Fence.Project != bot.Project || t.Fence.ConversationID != c.ID || t.Fence.Generation > c.Generation {
		return runtimeFailure(pa.ScopeDenied, "queued_fence")
	}
	if c.ActiveTurnID != "" && !(c.ActiveTurnID == t.ID && t.State == pa.TurnRunning && pa.SafeAttemptClosed(t)) {
		return runtimeFailure(pa.OutcomeUnknown, "active_turn")
	}
	// A room turn runs only while the room still wants it: checked here,
	// before any native effect, so a refusal ends a queued turn as failed.
	if t.Room != nil && t.State == pa.TurnQueued {
		if err = r.admitRoomTurn(ctx, *t.Room); err != nil {
			return err
		}
	}
	s.prepared, s.bot, s.conversation = bot, bot, c
	return nil
}

// readAttemptEnvironmentLocked resolves the project, the fallback step that
// runs this attempt, its capabilities, and the memory and history it starts
// from.
func (r *Engine) readAttemptEnvironmentLocked(ctx context.Context, a *runtimeActor, s *attemptStart, firstAttempt bool) error {
	bot, t := s.bot, s.turn
	selection := pa.ProjectSelection{MachineID: bot.Project.MachineID, ProjectID: bot.Project.ProjectID}
	resolution, err := r.o.ProjectResolver.Resolve(ctx, selection)
	if err == nil {
		err = pa.ValidateTarget(selection, resolution)
	}
	if err == nil && resolution.Scope != bot.Project {
		err = runtimeFailure(pa.ScopeDenied, "project")
	}
	if err != nil {
		return err
	}
	step, err := r.selectStep(ctx, bot, t)
	if err != nil {
		return err
	}
	effective := bot
	effective.Agent = step.Agent
	effective.LoginProfile = step.LoginProfile
	if step.Agent != bot.Agent || step.LoginProfile != bot.LoginProfile {
		effective.RuntimeSettings = pa.RuntimeSettings{}
	}
	caps, err := r.o.Provider.Capabilities(ctx, effective)
	if err == nil {
		err = pa.CheckExecutionCapabilities(caps, t.Fence)
	}
	if err != nil {
		return err
	}
	current, err := r.o.Memory.Current(ctx, a.key)
	if err != nil {
		return err
	}
	if s.handoffCard != nil {
		if err = checkHandoffExecution(*s.handoffCard, t, bot, currentFence(bot, s.conversation), current, firstAttempt); err == nil {
			err = s.handoffCheck(ctx)
		}
		if err != nil {
			return err
		}
	}
	snapshot, err := r.o.Memory.Snapshot(ctx, a.key)
	if err != nil {
		return err
	}
	export, err := r.o.Repository.Export(ctx, a.key)
	if err != nil {
		return err
	}
	if r.o.Charter != nil {
		// Read before anything is sent: a turn never runs on a charter it
		// could not read.
		if s.charterVersion, s.charterText, err = r.o.Charter.DeliveredCharter(ctx, bot); err != nil {
			return err
		}
	}
	s.resolution, s.step, s.effective, s.caps = resolution, step, effective, caps
	s.current, s.snapshot, s.export = current, snapshot, export
	return nil
}

// readAttemptInputLocked finds the queued message, checks who may send it,
// and composes the prompt text a routine or restart adds to it.
func (r *Engine) readAttemptInputLocked(ctx context.Context, a *runtimeActor, s *attemptStart) error {
	t, bot, c, export := s.turn, s.bot, s.conversation, s.export
	var input *pa.Message
	for i := range export.Messages {
		if export.Messages[i].ID == t.MessageID {
			msg := export.Messages[i]
			input = &msg
			break
		}
	}
	if input == nil {
		return runtimeFailure(pa.NotFound, "queued_message")
	}
	if input.Fence.Key != a.key || input.Fence.Project != bot.Project || input.Fence.ConversationID != c.ID || input.Kind != pa.UserMessage && input.Kind != pa.NoticeMessage {
		return runtimeFailure(pa.ScopeDenied, "queued_message")
	}
	if err := r.quotedCollaborationPrompt(ctx, t, input); err != nil {
		return err
	}
	peerWorker := t.Origin == pa.BotRequestOrigin && input.Exchange != nil &&
		input.Exchange.RequestWorkerID == a.key.BotID && input.Exchange.Recipient() == (pa.ExchangeEndpoint{Key: a.key, ConversationID: c.ID})
	if pa.IsPeerRequest(t.Origin) && (bot.Hidden && !peerWorker || len(input.Files) > 0 || len(input.Attachments) > 0) {
		return runtimeFailure(pa.ScopeDenied, "collaboration_recipient")
	}
	if task := pa.WorkCompletionTask(t); task != "" {
		if err := checkWorkCompletion(export, task, input.QuoteMessageID, true); err != nil {
			return err
		}
	}
	quote, err := resolvePromptQuote(export, input.QuoteMessageID, currentFence(bot, c), s.current)
	if err != nil {
		return err
	}
	mainID := c.MainBindingID
	if mainID == "" {
		mainID = c.BindingID
	}
	binding, err := r.o.Repository.Binding(ctx, a.key, mainID)
	if err != nil {
		return err
	}
	isolatedRoutine := t.RoutineRunID != ""
	if t.RoutineRunID != "" {
		if !s.caps.Resume {
			return runtimeFailure(pa.ResumeUnsupported, "routine_main_session")
		}
		d, run, e := routineForTurn(export, t)
		if e != nil {
			return e
		}
		if run.State != pa.TurnQueued {
			return runtimeFailure(pa.OutcomeUnknown, "routine_run")
		}
		input.Text = routinePrompt(d, s.current, run.Event)
		isolatedRoutine = !d.FollowUp
		run.Instruction = d.Instruction
		run.TimeoutSeconds = d.TimeoutSeconds
		s.routineRun = &run
	}
	if t.RoutineRunID != "" || t.Origin == pa.UserOrigin || t.Origin == "" {
		input.Text = pa.WithBotMentionHint(input.Text)
	}
	if t.CharterEdit != nil {
		// The charter the person asked about is read when the turn runs, so
		// the lead proposes against the version that is current then.
		if r.o.Charter == nil || !bot.Lead || !pa.DirectUserTurn(t) {
			return runtimeFailure(pa.ScopeDenied, "charter_edit")
		}
		scope := pa.ProjectScope{MachineID: bot.Project.MachineID, ProjectID: t.CharterEdit.ProjectID}
		switch t.CharterEdit.Target {
		case pa.CharterEditWorkflow:
			source, ok := r.o.Charter.(CharterWorkflowSource)
			if !ok {
				return runtimeFailure(pa.Unsupported, "charter_workflow")
			}
			version, flow, err := source.CurrentCharterWorkflow(ctx, scope)
			if err != nil {
				return err
			}
			input.Text = pa.CharterWorkflowEditContext(t.CharterEdit.ProjectID, version, flow, input.Text)
		case pa.CharterEditText:
			version, text, err := r.o.Charter.CurrentCharter(ctx, scope)
			if err != nil {
				return err
			}
			input.Text = pa.CharterEditContext(t.CharterEdit.ProjectID, version, text, input.Text)
		default:
			return runtimeFailure(pa.ScopeDenied, "charter_edit")
		}
	}
	if t.CharterFix != nil {
		// The report travels as the turn's own mark, set only by the charter
		// page's resolve route; the message text grants nothing.
		if !pa.DirectUserTurn(t) || pa.ValidateCharterFix(*t.CharterFix) != nil {
			return runtimeFailure(pa.ScopeDenied, "charter_fix")
		}
		input.Text = pa.CharterFixContext(*t.CharterFix) + input.Text
	}
	// The first prompt after an interrupted turn was settled unrepeated
	// inspects what finished before it acts.
	input.Text = restartPromptHeader(export, t, isolatedRoutine) + input.Text
	if isolatedRoutine && (binding.Agent != s.effective.Agent || binding.Revoked) {
		return runtimeFailure(pa.Unsupported, "routine_effective_agent")
	}
	s.input, s.quote, s.binding, s.isolatedRoutine = input, quote, binding, isolatedRoutine
	return nil
}

// chooseAttemptBindingLocked picks the native session this attempt resumes,
// or decides it starts fresh, and what a fallback carries over.
func (r *Engine) chooseAttemptBindingLocked(ctx context.Context, s *attemptStart) error {
	t, bot, binding := s.turn, s.bot, s.binding
	agent, profile := s.effective.Agent, s.effective.LoginProfile
	s.fromAgent = binding.Agent
	var resumedFrom, carryKind string
	var carryAfter int64
	// Each configured identity keeps its own native conversation. A switch
	// chooses only its newest confirmed-clean binding, never the active
	// alternate's history. All execution authority is minted afresh below.
	if s.handoffCard == nil && !s.isolatedRoutine && pa.HasFallbacks(bot) && (binding.Agent != agent || binding.LoginProfile != profile) {
		carryKind = "recap"
		if parked, seen, ok := parkedAttempt(s.export, t, agent, profile); ok {
			binding = parked
			binding.Revoked = false // old fence stays revoked; only the new copy is used
			resumedFrom, carryAfter = parked.Fence.BindingID, seen
		}
	}
	if binding.Agent != agent || binding.LoginProfile != profile || binding.Revoked {
		binding.Agent = agent
		binding.LoginProfile = profile
		binding.NativeSessionID = ""
		binding.Revoked = false
	}
	// A person's new-session boundary invalidates every earlier native binding.
	if s.export.Conversation.SessionResetGeneration > 0 && binding.Fence.Generation <= s.export.Conversation.SessionResetGeneration {
		binding.NativeSessionID = ""
	}
	// Check the selected history, including a parked fallback, before an
	// adapter can attempt a load or fall back to a fresh open in the same
	// history directory. Isolated routines, Forget and changed instructions
	// use separate history partitions through the existing filtered-start path.
	if !s.isolatedRoutine && binding.InstructionRevision == bot.Instructions.Revision && binding.ForgottenThrough == s.current.ForgottenThrough {
		if err := r.checkNativeHistory(ctx, binding); err != nil {
			return err
		}
	}
	decision, err := pa.DecideResume(binding, s.current, s.snapshot, s.caps)
	if err != nil {
		return err
	}
	// Every fresh process has a new generation. Unsafe native history is never
	// loaded, including after forgetting or changing fixed instructions.
	resume := s.handoffCard == nil && (len(t.Attempts) == 0 || resumedFrom != "") && !s.isolatedRoutine && decision == pa.ResumeExisting && binding.InstructionRevision == bot.Instructions.Revision
	if sourceID := pa.RestartSourceTurn(t); sourceID != "" {
		if !resume || !matchesRestartBinding(s.export, sourceID, binding) {
			return runtimeFailure(pa.ResumeUnsupported, "restart_history")
		}
		s.effective.Mode = restartNativeMode(agent)
		if s.effective.Mode == "" {
			return runtimeFailure(pa.Unsupported, "restart_mode")
		}
		s.effective.Extensions = pa.ExtensionSelection{}
	}
	if t.Origin == pa.CompactOrigin && (!resume || agent != bot.Agent) {
		// Nothing has been sent: the queued turn ends failed and the next
		// message runs normally. A fresh session has nothing to compact.
		return runtimeFailure(pa.Unsupported, compactNoSession)
	}
	if resumedFrom != "" && resume {
		carryKind = "catch_up"
	} else {
		resumedFrom, carryAfter = "", 0
	}
	s.binding, s.resume = binding, resume
	s.resumedFrom, s.carryKind, s.carryAfter = resumedFrom, carryKind, carryAfter
	return nil
}

// commitAttemptStartLocked mints the attempt's binding and fence and records
// the turn as running, against the current Bot and conversation.
func (r *Engine) commitAttemptStartLocked(ctx context.Context, a *runtimeActor, s *attemptStart) error {
	// Preparation can block on capability, memory or history reads. Authority
	// checked before those reads does not authorize a later execution start.
	if err := r.authorized(ctx, a.key, s.bot.Project); err != nil {
		return err
	}
	// Preparation can take seconds, and the Bot's version also moves for
	// writes that do not concern this start, such as a reader's position.
	// Start from the current Bot and conversation when nothing this turn
	// depends on has changed; anything that has stays a stale refusal.
	bot, c, err := r.currentForStart(ctx, a.key, s.bot, s.conversation)
	if err != nil {
		return err
	}
	if c.MainBindingID == "" {
		c.MainBindingID = c.BindingID
	}
	t, binding := s.turn, s.binding
	// Every start mints a new binding ID; the previous turn of the resumed
	// session ran under the one being replaced here.
	resumedBindingID := c.BindingID
	c.Generation++
	c.BindingID = r.o.ID()
	if s.isolatedRoutine {
		binding.SessionSlot = "routine-" + t.RoutineRunID
	} else {
		binding.SessionSlot = ""
		c.MainBindingID = c.BindingID
	}
	t.Fence = currentFence(bot, c)
	binding.Fence = t.Fence
	binding.PrimarySupersededAt = 0
	binding.Revoked = false
	if !s.resume {
		binding.NativeSessionID = ""
	}
	// A resumed session last heard memory at its binding's revision, and the
	// machine's shared memory under its binding's digest.
	sharedDigest := ""
	if r.o.SharedMemory != nil {
		sharedDigest = sharedMemoryDigest(r.o.SharedMemory(ctx, bot))
	}
	s.memoryChanged = binding.MemoryRevision != s.current.Revision || binding.SharedMemoryDigest != sharedDigest
	s.compacted = compactedBeforeTurn(s.export, t, resumedBindingID)
	// A /compact prompt delivers no memory, so the session's memory revision
	// stays where it was and the next prompt still carries any change.
	if t.Origin != pa.CompactOrigin {
		binding.MemoryRevision = s.current.Revision
		binding.ForgottenThrough = s.current.ForgottenThrough
		binding.SharedMemoryDigest = sharedDigest
		// A charter change never replaces the native session: a resumed one
		// hears the new version once, in this prompt.
		s.charterChanged = s.resume && binding.CharterVersion != s.charterVersion
		binding.CharterVersion = s.charterVersion
		t.CharterVersion = s.charterVersion
		// The binding remembers the version it was asked to check, so a fresh
		// or resumed session asks once per version. A Bot's own background
		// helpers and isolated routines have nothing of their own to compare.
		s.charterCheck = s.charterVersion > binding.CharterChecked && !s.isolatedRoutine && !bot.Hidden
		if s.charterCheck {
			binding.CharterChecked = s.charterVersion
		}
	}
	leadRole := "member"
	if bot.Lead {
		leadRole = "lead"
	}
	s.roleChanged = s.resume && binding.LeadRole != leadRole
	binding.LeadRole = leadRole
	binding.InstructionRevision = bot.Instructions.Revision
	t.State = pa.TurnRunning
	started := r.o.Clock().UTC()
	if t.StartedAt == nil {
		t.StartedAt = &started
	}
	t.MemoryRevision = s.current.Revision
	t.Instructions = bot.Instructions
	c.ActiveTurnID = t.ID
	s.priorUsage = c.ContextUsage
	c.ContextUsage = pa.ContextSample{}
	c.MemoryRevision = s.current.Revision
	if pa.HasFallbacks(bot) {
		t.Attempts = append(slices.Clone(t.Attempts), r.openingAttempt(s, bot, t))
	}
	s.bot, s.conversation, s.turn, s.binding = bot, c, t, binding
	cmd := r.command(a.key, pa.SendOperation, "runtime-start-"+t.ID+"-"+binding.Fence.BindingID, t)
	if s.routineRun != nil {
		s.routineRun.State = pa.TurnRunning
		s.routineRun.NotStartedProved = false
		s.routineRun.BindingID = binding.Fence.BindingID
	}
	_, err = r.mutate(ctx, bot, Mutation{Command: cmd, Conversation: &s.conversation, Binding: &s.binding, Turn: &s.turn, RoutineRun: s.routineRun, Receipt: pa.Receipt{State: pa.EffectExecuting, ResourceID: t.ID}, Events: []pa.Event{{Fence: t.Fence, Kind: pa.TurnUpdated, Turn: &s.turn}}})
	return err
}

// openingAttempt is the ledger fact for an attempt that is about to open.
func (r *Engine) openingAttempt(s *attemptStart, bot pa.Bot, t pa.Turn) pa.AttemptFact {
	ordinal := 1
	if len(t.Attempts) > 0 {
		ordinal = t.Attempts[len(t.Attempts)-1].Ordinal + 1
	}
	agent, profile := s.effective.Agent, s.effective.LoginProfile
	attempt := pa.AttemptFact{Ordinal: ordinal, Agent: agent, LoginProfile: profile, Fence: t.Fence, Stage: pa.AttemptOpening, Effect: "unknown", ResumeBindingID: s.resumedFrom, CarryKind: s.carryKind, CarryAfterSeq: s.carryAfter}
	if t.Room != nil {
		attempt.Fallback = agent != bot.Agent || profile != bot.LoginProfile
		attempt.PrimaryRestReason = s.step.primaryRestReason
	}
	return attempt
}

func (r *Engine) startPeerExchange(ctx context.Context, t pa.Turn) error {
	p, err := r.collaboration()
	if err != nil {
		return err
	}
	return p.StartExchange(ctx, t.Fence, t.ID, t.ExchangeID)
}

// openAttemptSession opens the native session while the actor shows it as
// opening. entered is false when a boundary check refused before Open was
// called; the caller then returns the refusal as it is.
func (r *Engine) openAttemptSession(ctx context.Context, a *runtimeActor, s *attemptStart) (ProviderSession, bool, error) {
	a.mu.Lock()
	a.opening = true
	a.mu.Unlock()
	session, entered, err := r.openNativeSession(ctx, a, s)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.opening = false
	if entered {
		a.closing = false
		a.session = session
	}
	return session, entered, err
}

func (r *Engine) openNativeSession(ctx context.Context, a *runtimeActor, s *attemptStart) (ProviderSession, bool, error) {
	t := s.turn
	err := r.checkAttemptBoundary(ctx, a, s.prepared, s.conversation, t, true)
	if err == nil && s.handoffCheck != nil {
		err = s.handoffCheck(ctx)
	}
	if err == nil {
		err = r.checkNativeHistory(ctx, s.binding)
	}
	if err != nil {
		return nil, false, err
	}
	session, err := r.o.Provider.Open(ctx, OpenRequest{PrimaryAgent: s.bot.Agent, Bot: s.effective, Binding: s.binding, Directory: s.resolution.Directory, Resume: s.resume, RestartReadOnly: pa.RestartSourceTurn(t) != ""}, func(ctx context.Context, e ProtocolEvent) error { return r.incoming(ctx, a, e) })
	if session != nil || !s.resume || s.isolatedRoutine || t.Origin == pa.CompactOrigin || pa.RestartSourceTurn(t) != "" || pa.Code(err) != pa.ResumeUnsupported {
		return session, true, err
	}
	// As in Codync, a native session that cannot be loaded is replaced by
	// a new one that receives the filtered history and memory; nothing
	// was sent to the old one, so nothing is repeated.
	s.resume = false
	s.binding.NativeSessionID = ""
	err = r.checkAttemptBoundary(ctx, a, s.prepared, s.conversation, t, true)
	if err == nil {
		err = r.checkNativeHistory(ctx, s.binding)
	}
	if err != nil {
		return nil, false, err
	}
	session, err = r.o.Provider.Open(ctx, OpenRequest{PrimaryAgent: s.bot.Agent, Bot: s.effective, Binding: s.binding, Directory: s.resolution.Directory, Resume: false}, func(ctx context.Context, e ProtocolEvent) error { return r.incoming(ctx, a, e) })
	return session, true, err
}

func (r *Engine) missingAttemptSession(a *runtimeActor, err error) error {
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

// retryAfterRefusedOpen handles an Open that returned a session and an
// error: a routine refused before sending ends unstarted, and a refusal
// proved safe moves on to the next configured identity.
func (r *Engine) retryAfterRefusedOpen(ctx context.Context, a *runtimeActor, s *attemptStart, cleanup *attemptCleanup, err error) error {
	if r.routineRefusedBeforeSend(ctx, a.key, s.turn, err) {
		if closeErr := cleanup.close(); closeErr != nil {
			return errors.Join(err, closeErr)
		}
		return &routineNotStarted{cause: err}
	}
	safe, e := r.refuseBeforeSend(ctx, a, &s.turn, s.binding, err)
	if e != nil {
		return e
	}
	if !safe {
		return err
	}
	// ownedSession.Close seals tools before waiting for native cleanup.
	if e = cleanup.close(); e != nil {
		return e
	}
	rest, e := r.attemptRest(ctx, s.bot, s.turn)
	if e != nil {
		return e
	}
	if e = r.appendAttempt(ctx, a, &s.turn, pa.AttemptClosed, nil, rest); e != nil {
		return e
	}
	// Recursion is bounded by the validated unique configured identities, never
	// a same-agent retry; ctx and the process owner belong to the logical turn.
	return r.executeAttempt(ctx, a, s.turn, &s.prepared)
}

// bindAttemptSession records the opened native session on the binding.
func (r *Engine) bindAttemptSession(ctx context.Context, a *runtimeActor, s *attemptStart, session ProviderSession) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped.Load() || ctx.Err() != nil {
		return runtimeFailure(pa.ExecutionEffectUnknown, "stopped_before_send")
	}
	s.binding.NativeSessionID = session.SessionID()
	cmd := r.command(a.key, pa.SendOperation, "runtime-bound-"+s.turn.ID+"-"+s.binding.Fence.BindingID, s.binding)
	var err error
	for {
		s.bot, _, err = r.currentForStart(ctx, a.key, s.prepared, s.conversation)
		if err != nil {
			return err
		}
		if err = r.authorized(ctx, a.key, s.bot.Project); err != nil {
			return err
		}
		_, err = r.mutate(ctx, s.bot, Mutation{Command: cmd, Binding: &s.binding, Receipt: pa.Receipt{State: pa.EffectExecuting, ResourceID: s.turn.ID}})
		if !collaborationAggregateConflict(err) {
			return err
		}
		// A work result can advance the aggregate while the session opens.
		// Retry only the uncommitted binding after checking its owner again.
		if err = ctx.Err(); err != nil {
			return err
		}
	}
}

// sendAttemptPrompt sends the prompt under the memory revision the start
// recorded, checking authority again at the send boundary.
func (r *Engine) sendAttemptPrompt(ctx context.Context, a *runtimeActor, s *attemptStart, session ProviderSession, files []PromptFile) (PendingPrompt, error) {
	var pending PendingPrompt
	err := r.o.Memory.WithCurrent(ctx, a.key, s.current.Revision, func(filtered pa.ContextSnapshot) error {
		if err := pa.CheckContext(filtered, s.current); err != nil {
			return err
		}
		// Shared memory is read before the Bot's own memory, which wins. It is
		// the person's machine setting, not a fact of this Bot's memory, so it
		// joins the delivered text only after the provenance checks: a Bot with
		// no memory of its own, such as every request worker, still opens.
		// The team charter comes first and wins over both memories.
		shared := ""
		if s.turn.Origin != pa.CompactOrigin {
			shared = pa.CharterOpening(s.charterVersion, s.charterText)
		}
		if r.o.SharedMemory != nil {
			shared += sharedMemoryBlock(r.o.SharedMemory(ctx, s.bot))
		}
		promptText, handoff, err := attemptPrompt(s, filtered)
		if err != nil {
			return err
		}
		if handoff != nil {
			handoff.shareMemory(shared)
		}
		filtered.Text = shared + filtered.Text
		// WithCurrent may wait behind a memory writer. Check again at the
		// actual send boundary rather than trusting the pre-lock snapshot.
		if err = r.authorized(ctx, a.key, s.bot.Project); err != nil {
			return err
		}
		images, err := r.images(a.key, s.input.Attachments)
		if err != nil {
			return err
		}
		// The sending fact was persisted before acquiring the memory lock.
		// Recheck the exact authority at the external write boundary.
		if err = r.checkAttemptBoundary(ctx, a, s.prepared, s.conversation, s.turn, false); err != nil {
			return err
		}
		if s.handoffCard != nil {
			if err = r.claimAttemptHandoff(ctx, s, handoff); err != nil {
				return err
			}
		}
		if s.turn.Origin == pa.CompactOrigin {
			pending, err = session.Send(ctx, PromptRequest{Turn: s.turn, Text: compactCommand, Context: filtered, Compact: true})
			return err
		}
		pending, err = session.Send(ctx, PromptRequest{Turn: s.turn, Text: promptText, Context: filtered, Quote: s.quote, Handoff: handoff, Images: images, Files: files, MemoryChanged: s.resume && s.memoryChanged, Compacted: s.resume && s.compacted})
		return err
	})
	if err == nil && r.o.Charter != nil && s.turn.Origin != pa.CompactOrigin {
		// The sent version feeds the charter's readers; the prompt is
		// already sent, so a failed note does not fail the turn.
		_ = r.o.Charter.CharterDelivered(context.WithoutCancel(ctx), s.bot, s.turn.ID, s.charterVersion)
		if recorder, ok := r.o.Charter.(CharterCheckRecorder); ok && s.charterCheck {
			_ = recorder.CharterChecked(context.WithoutCancel(ctx), s.bot, s.turn.ID, s.charterVersion)
		}
	}
	return pending, err
}

// attemptPrompt is the text sent for the turn and the handoff a fresh or
// fallback session opens with.
func attemptPrompt(s *attemptStart, filtered pa.ContextSnapshot) (string, *SessionHandoff, error) {
	var handoff *SessionHandoff
	if !s.resume && !s.isolatedRoutine {
		var err error
		handoff, err = newSessionHandoffForAgent(s.export, s.turn, filtered, s.effective.Agent)
		if err != nil {
			return "", nil, err
		}
	}
	text := s.input.Text
	if s.roleChanged {
		role := "You are now an ordinary Bot. Your previous lead role has ended."
		if s.bot.Lead {
			role = pa.LeadBrief
		}
		text = "<agent_profile_update>" + role + "</agent_profile_update>\n\n" + text
	}
	if s.carryKind != "" && !s.isolatedRoutine {
		kind, after := s.carryKind, s.carryAfter
		if !s.resume {
			kind, after = "recap", 0
		}
		var prefix string
		var err error
		handoff, prefix, err = fallbackOpening(s.export, s.turn, filtered, s.effective.Agent, s.fromAgent, kind, after)
		if err != nil {
			return "", nil, err
		}
		text = prefix + text
	}
	// The check follows whatever charter text this prompt or the session
	// already holds; it never repeats the text.
	check := ""
	if s.charterCheck {
		check = pa.CharterCheck(s.charterVersion)
	}
	switch {
	case handoff == nil && !s.resume && s.charterVersion > 0:
		// An isolated routine opens without a handoff; its prompt carries
		// the charter itself.
		text = pa.CharterOpening(s.charterVersion, s.charterText) + "\n" + check + text
	case handoff == nil && s.charterChanged && !s.memoryChanged && !s.compacted:
		// A memory update or compaction redelivery already repeats the
		// whole context, charter included.
		text = pa.CharterUpdate(s.charterVersion, s.charterText) + check + text
	default:
		text = check + text
	}
	return text, handoff, nil
}

func (r *Engine) claimAttemptHandoff(ctx context.Context, s *attemptStart, handoff *SessionHandoff) error {
	if handoff == nil {
		return runtimeFailure(pa.ContextStale, "handoff_native_opening")
	}
	if err := s.handoffCheck(ctx); err != nil {
		return err
	}
	text, err := r.o.Repository.(HandoffRepository).ClaimHandoffText(ctx, *s.handoffCard)
	if err != nil {
		return err
	}
	if err = s.handoffCheck(ctx); err != nil {
		return err
	}
	handoff.editedSummary = &text
	return nil
}

// continueAfterQuota moves a turn whose agent ran out of quota before any
// output to the next configured identity; any other wait error is returned.
func (r *Engine) continueAfterQuota(ctx context.Context, a *runtimeActor, s *attemptStart, cleanup *attemptCleanup, err error) error {
	quota := terminalQuota(s.turn, err)
	if quota == nil || !quota.NoOutput || a.stopped.Load() || ctx.Err() != nil {
		return err
	}
	if closeErr := cleanup.close(); closeErr != nil {
		return errors.Join(closeErr, err)
	}
	continued, transitionErr := r.closeQuotaAttempt(ctx, a, &s.turn, s.binding, quota.Failure.Reason)
	if transitionErr != nil || !continued {
		return errors.Join(err, transitionErr)
	}
	if _, selectErr := r.selectStep(ctx, s.prepared, s.turn); selectErr != nil {
		return err
	}
	return r.executeAttempt(ctx, a, s.turn, &s.prepared)
}

// finishAnsweredAttempt closes the session of an answered prompt and
// records the turn as finished.
func (r *Engine) finishAnsweredAttempt(ctx context.Context, a *runtimeActor, s *attemptStart, cleanup *attemptCleanup, result PromptResult) error {
	if err := r.appendAttempt(ctx, a, &s.turn, pa.AttemptAnswered, nil, nil); err != nil {
		return err
	}
	session := cleanup.session
	usage := completedContextSample(session, s.turn)
	compactedNotice := compactionNotice(session, s.turn, r.o.Clock())
	if s.turn.Origin == pa.CompactOrigin {
		compactedNotice = withPriorUsage(compactedNotice, session, s.priorUsage)
	}
	if err := cleanup.close(); err != nil {
		return err
	}
	if err := r.appendAttempt(ctx, a, &s.turn, pa.AttemptClosed, nil, nil); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped.Load() {
		return runtimeFailure(pa.ExecutionEffectUnknown, "cancelled")
	}
	t := s.turn
	if err := r.authorized(ctx, a.key, s.bot.Project); err != nil {
		return err
	}
	if t.Origin == pa.CompactOrigin {
		// The agent's own words about compacting are not a chat reply; the
		// notice says what happened. An answered /compact that reported no
		// compaction is a definite failure, never a silent success.
		if compactedNotice == nil {
			return r.finishWithContextLocked(ctx, a, t, pa.TurnFailed, "", "", runtimeFailure(pa.Unsupported, compactNotReported), usage)
		}
		result = PromptResult{}
	}
	a.compacted = compactedNotice
	err := r.finishWithContextLocked(ctx, a, t, pa.TurnSucceeded, result.FinalText, result.answer(), nil, usage)
	a.compacted = nil
	if err != nil && (t.RoutineRunID != "" || t.ExchangeID != "") {
		a.completion = &routineCompletion{turn: t, text: result.FinalText, answer: result.answer()}
		r.retain(a, err)
	}
	return err
}
