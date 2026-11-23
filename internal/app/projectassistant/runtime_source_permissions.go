package projectassistant

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

const sourcePermissionPrefix = "clawdline-source-"
const teamReviewAllow = "approve-team-change"
const teamReviewDeny = "decline-team-change"

// DispatchPermissionPrefix names a host-owned cross-Project dispatch card. Its
// suffix is derived from the turn and idempotency key so a resent call finds
// the same card and its stored answer instead of asking again.
const DispatchPermissionPrefix = sourcePermissionPrefix + "dispatch-"

// SourcePermissionRequest comes only from a source-authenticated Host callback.
// The command handler defines the scope and may offer only choices it can honor.
// Returning AllowAlways is a choice, not proof that its rule was installed:
// that handler must durably save a visible/revocable exact rule before acting.
type SourcePermissionRequest struct {
	Fence          pa.Fence
	TurnID         string
	MemoryRevision int64
	Action         pa.PermissionAction
	Options        []pa.PermissionOption
}

type sourcePermission struct {
	request      SourcePermissionRequest
	offer        pa.PermissionOffer
	ready        chan struct{}
	requestDone  <-chan struct{}
	option       pa.PermissionOption
	completed    bool
	abandoned    bool
	counted      bool
	teamDecision func(context.Context, bool, pa.SourceStatus) error
}

// sourcePermissionCurrent holds actor.mu. A memory save alone is harmless;
// Forget invalidates a request derived from an older captured context.
func (r *Engine) sourcePermissionCurrent(ctx context.Context, a *runtimeActor, q SourcePermissionRequest) (pa.Bot, pa.Turn, error) {
	if err := ctx.Err(); err != nil {
		return pa.Bot{}, pa.Turn{}, err
	}
	if a.stopped.Load() || a.closing || a.session == nil || q.TurnID == "" || a.turnID != q.TurnID {
		return pa.Bot{}, pa.Turn{}, runtimeFailure(pa.StaleGeneration, "source_permission_turn")
	}
	bot, err := r.o.Repository.Bot(ctx, a.key)
	if err != nil {
		return bot, pa.Turn{}, err
	}
	if err := r.authorized(ctx, a.key, bot.Project); err != nil {
		return bot, pa.Turn{}, err
	}
	c, err := r.o.Repository.Conversation(ctx, a.key, bot.ConversationID)
	if err != nil {
		return bot, pa.Turn{}, err
	}
	if err := pa.CheckFence(q.Fence, currentFence(bot, c)); err != nil {
		return bot, pa.Turn{}, err
	}
	if c.ActiveTurnID != q.TurnID {
		return bot, pa.Turn{}, runtimeFailure(pa.StaleVersion, "source_permission_turn")
	}
	turn, err := r.o.Repository.Turn(ctx, a.key, q.TurnID)
	if err != nil {
		return bot, turn, err
	}
	if turn.State != pa.TurnRunning && turn.State != pa.TurnWaitingPermission {
		return bot, turn, runtimeFailure(pa.StaleVersion, "source_permission_turn")
	}
	binding, err := r.o.Repository.Binding(ctx, a.key, q.Fence.BindingID)
	if err != nil {
		return bot, turn, err
	}
	if binding.Revoked || binding.Fence != q.Fence || q.MemoryRevision < 0 || q.MemoryRevision > binding.MemoryRevision {
		return bot, turn, runtimeFailure(pa.ContextStale, "source_permission_context")
	}
	memory, err := r.o.Memory.Current(ctx, a.key)
	if err != nil {
		return bot, turn, err
	}
	if memory.Key != a.key || memory.Project != q.Fence.Project || memory.ForgottenThrough < 0 {
		return bot, turn, runtimeFailure(pa.ScopeDenied, "source_permission_memory")
	}
	if memory.ForgottenThrough > q.MemoryRevision {
		return bot, turn, runtimeFailure(pa.ContextStale, "source_permission_context")
	}
	return bot, turn, nil
}

// RequestSourcePermission uses the existing durable card/answer path. Waiting
// holds no actor, memory or SQLite lock. No provider Answer RPC is issued for
// this namespace, and no choice is released before its answer commit succeeds.
func (r *Engine) RequestSourcePermission(ctx context.Context, q SourcePermissionRequest) (pa.PermissionOption, error) {
	return r.requestSourcePermission(ctx, q, "", nil, false)
}

// RequestHumanPermission asks the person on a card that no Bot permission
// policy may answer automatically. id must carry DispatchPermissionPrefix.
func (r *Engine) RequestHumanPermission(ctx context.Context, q SourcePermissionRequest, id string) (pa.PermissionOption, error) {
	if !strings.HasPrefix(id, DispatchPermissionPrefix) || len(id) == len(DispatchPermissionPrefix) {
		return pa.PermissionOption{}, runtimeFailure(pa.Invalid, "source_permission")
	}
	return r.requestSourcePermission(ctx, q, id, nil, true)
}

// ReviewTeamPlan presents a host-owned, explicitly human team decision on the
// same durable channel as the other permission cards.
func (r *Engine) ReviewTeamPlan(ctx context.Context, plan TeamPlan, execution ManagerExecution, decide func(context.Context, bool, pa.SourceStatus) error) error {
	turn, err := r.o.Repository.Turn(ctx, execution.Fence.Key, execution.TurnID)
	if err != nil {
		return err
	}
	kind := "team_create"
	if plan.Operation == pa.EditOperation {
		kind = "team_update"
	}
	q := SourcePermissionRequest{
		Fence: execution.Fence, TurnID: execution.TurnID, MemoryRevision: turn.MemoryRevision,
		Action:  pa.PermissionAction{Title: plan.TargetName, ToolKind: kind, Detail: plan.Detail},
		Options: []pa.PermissionOption{{ID: teamReviewAllow, Kind: pa.AllowOnce, Allow: true}, {ID: teamReviewDeny, Kind: pa.RejectOnce}},
	}
	_, err = r.requestSourcePermission(ctx, q, plan.ID, decide, true)
	return err
}

func (r *Engine) requestSourcePermission(ctx context.Context, q SourcePermissionRequest, id string, teamDecision func(context.Context, bool, pa.SourceStatus) error, human bool) (pa.PermissionOption, error) {
	deadline, ok := ctx.Deadline()
	if !ok || !r.o.Clock().Before(deadline) || q.Action.Shortened || strings.TrimSpace(q.Action.Title) == "" || strings.TrimSpace(q.Action.ToolKind) == "" || len(q.Options) == 0 {
		return pa.PermissionOption{}, runtimeFailure(pa.Invalid, "source_permission")
	}
	raw, err := json.Marshal(q)
	if err != nil || len(raw) > capacity.MaximumBotPermissionActionBytes {
		return pa.PermissionOption{}, runtimeFailure(pa.CapacityFull, "permission_action")
	}
	// Own every slice/pointer that outlives this invocation's caller.
	if err := json.Unmarshal(raw, &q); err != nil {
		return pa.PermissionOption{}, err
	}
	ids := map[string]bool{}
	kinds := map[pa.PermissionOptionKind]bool{}
	for _, option := range q.Options {
		allow := option.Kind == pa.AllowOnce || option.Kind == pa.AllowAlways
		known := allow || option.Kind == pa.RejectOnce || option.Kind == pa.RejectAlways
		if option.ID == "" || ids[option.ID] || kinds[option.Kind] || !known || allow != option.Allow {
			return pa.PermissionOption{}, runtimeFailure(pa.Invalid, "permission_option")
		}
		ids[option.ID], kinds[option.Kind] = true, true
	}
	r.mu.Lock()
	a := r.actors[q.Fence.Key]
	if a != nil {
		a.users++
	}
	r.mu.Unlock()
	if a == nil {
		return pa.PermissionOption{}, runtimeFailure(pa.StaleGeneration, "source_permission_actor")
	}
	defer r.release(a)
	a.mu.Lock()
	bot, turn, err := r.sourcePermissionCurrent(ctx, a, q)
	if err != nil {
		a.mu.Unlock()
		return pa.PermissionOption{}, err
	}
	if a.sourcePermissions == nil {
		a.sourcePermissions = map[string]*sourcePermission{}
	}
	for id, entry := range a.sourcePermissions {
		if entry.offer.TurnID != a.turnID || entry.offer.Fence != q.Fence {
			delete(a.sourcePermissions, id)
		}
	}
	if len(a.sourcePermissions) >= capacity.MaximumBotServerCallerReceipts {
		a.mu.Unlock()
		return pa.PermissionOption{}, runtimeFailure(pa.CapacityFull, "source_permissions")
	}
	var anchor int64
	if teamDecision != nil {
		conversation, err := r.o.Repository.Conversation(ctx, a.key, q.Fence.ConversationID)
		if err != nil {
			a.mu.Unlock()
			return pa.PermissionOption{}, err
		}
		anchor = conversation.LastMessageSeq
	}
	if id == "" {
		id = sourcePermissionPrefix + r.o.ID()
	}
	if a.sourcePermissions[id] != nil {
		a.mu.Unlock()
		return pa.PermissionOption{}, runtimeFailure(pa.IdempotencyConflict, "source_permission")
	}
	entry := &sourcePermission{request: q, ready: make(chan struct{}), requestDone: ctx.Done(), teamDecision: teamDecision, offer: pa.PermissionOffer{AnchorMessageSeq: anchor, ID: id, NativeRequestID: id, Fence: q.Fence, TurnID: q.TurnID, Action: &q.Action, Options: q.Options, ExpiresAt: deadline, State: pa.PermissionPending}}
	a.sourcePermissions[id] = entry
	turn.State = pa.TurnWaitingPermission
	command := r.command(a.key, pa.SendOperation, id, entry.offer)
	_, err = r.mutate(ctx, bot, Mutation{Command: command, Turn: &turn, Permissions: []pa.PermissionOffer{entry.offer}, Receipt: pa.Receipt{State: pa.EffectAccepted, ResourceID: id}, Events: []pa.Event{{Fence: q.Fence, Kind: pa.PermissionOffered, Permission: &entry.offer}, {Fence: q.Fence, Kind: pa.TurnUpdated, Turn: &turn}}})
	if err == nil {
		a.pendingPermissions++
		a.permissionFence = q.Fence
		entry.counted = true
	}
	a.mu.Unlock()
	delivered := false
	defer func() {
		if !delivered {
			r.abandonSourcePermission(a, entry)
		}
	}()
	if err != nil {
		return pa.PermissionOption{}, err
	}
	// Match the existing native card policy, never infer it from an Agent name
	// or sandbox label. Automatic approval uses only an eligible once option.
	if !human && (bot.PermissionPolicy == pa.Approve || bot.PermissionPolicy == pa.AutoRead) {
		for _, option := range q.Options {
			if option.Allow && option.Kind == pa.AllowOnce && (option.ReadOnly || bot.PermissionPolicy == pa.Approve) {
				answer := pa.PermissionAnswer{Fence: q.Fence, OfferID: id, TurnID: q.TurnID, OptionID: option.ID}
				if _, err := r.Answer(ctx, AnswerRequest{Command: r.command(a.key, pa.AnswerOperation, "source-auto-"+id, answer), Answer: answer}); err != nil {
					return pa.PermissionOption{}, err
				}
				break
			}
		}
	}
	select {
	case <-entry.ready:
		a.mu.Lock()
		_, _, err := r.sourcePermissionCurrent(ctx, a, q)
		option := entry.option
		a.mu.Unlock()
		if err != nil {
			return pa.PermissionOption{}, err
		}
		delivered = true
		return option, nil
	case <-ctx.Done():
		return pa.PermissionOption{}, ctx.Err()
	case <-a.lifetime.Done():
		return pa.PermissionOption{}, runtimeFailure(pa.Revoked, "source_permission_actor")
	}
}

// answerSourcePermission holds actor.mu and performs no external IO. A rule
// choice is returned to its registered handler only after the durable answer.
func (r *Engine) answerSourcePermission(ctx context.Context, a *runtimeActor, bot pa.Bot, offer pa.PermissionOffer, q AnswerRequest) (pa.Receipt, error) {
	entry := a.sourcePermissions[offer.NativeRequestID]
	if entry == nil || entry.abandoned || entry.completed || offer.ID != entry.offer.ID || offer.Fence != entry.offer.Fence || offer.TurnID != entry.offer.TurnID || !reflect.DeepEqual(offer.Action, entry.offer.Action) || !reflect.DeepEqual(offer.Options, entry.offer.Options) {
		return pa.Receipt{}, runtimeFailure(pa.StaleGeneration, "source_permission")
	}
	select {
	case <-entry.requestDone:
		return pa.Receipt{}, runtimeFailure(pa.Revoked, "source_permission_lifetime")
	default:
	}
	_, turn, err := r.sourcePermissionCurrent(ctx, a, entry.request)
	if err != nil {
		return pa.Receipt{}, err
	}
	var selected pa.PermissionOption
	for _, option := range entry.offer.Options {
		if option.ID == q.Answer.OptionID {
			selected = option
		}
	}
	offer.State, offer.SelectedOptionID = pa.PermissionResolved, selected.ID
	var decisionErr error
	if entry.teamDecision != nil {
		decisionErr = entry.teamDecision(ctx, selected.Allow, q.Source)
		if decisionErr != nil {
			offer.State, offer.SelectedOptionID = pa.PermissionCancelled, ""
			if pa.Code(decisionErr) == pa.OutcomeUnknown {
				offer.State = pa.PermissionUnknown
			}
		}
		// A team mutation can edit the manager itself. Persist the answer
		// against the new settings, not the aggregate read before the decision.
		bot, err = r.o.Repository.Bot(ctx, a.key)
		if err != nil {
			return pa.Receipt{}, err
		}
	}
	if a.pendingPermissions <= 1 {
		turn.State = pa.TurnRunning
	}
	receipt, err := r.mutate(ctx, bot, Mutation{Command: q.Command, Turn: &turn, Permissions: []pa.PermissionOffer{offer}, Receipt: pa.Receipt{State: pa.EffectDelivered, ResourceID: offer.ID}, Events: []pa.Event{{Fence: offer.Fence, Kind: pa.PermissionAnswered, Permission: &offer}, {Fence: offer.Fence, Kind: pa.TurnUpdated, Turn: &turn}}})
	if err != nil {
		return receipt, err // Never turn a lost commit acknowledgement into a grant.
	}
	if entry.counted {
		retireSourcePermissionCount(a, entry)
	}
	entry.option, entry.completed = selected, true
	close(entry.ready)
	return receipt, decisionErr
}

func (r *Engine) abandonSourcePermission(a *runtimeActor, entry *sourcePermission) {
	ctx, cancel := context.WithTimeout(context.Background(), r.o.Limits.CleanupTimeout)
	defer cancel()
	a.mu.Lock()
	defer a.mu.Unlock()
	entry.abandoned = true
	if entry.completed {
		return
	}
	stored, err := r.o.Repository.Permission(ctx, a.key, entry.offer.ID)
	if err != nil {
		return
	}
	if stored.State != pa.PermissionPending {
		if entry.counted {
			retireSourcePermissionCount(a, entry)
		}
		return
	}
	bot, err := r.o.Repository.Bot(ctx, a.key)
	if err != nil {
		return
	}
	turn, err := r.o.Repository.Turn(ctx, a.key, stored.TurnID)
	if err != nil {
		return
	}
	if stored.Fence != entry.offer.Fence || stored.TurnID != entry.offer.TurnID {
		return
	}
	stored.State = pa.PermissionCancelled
	if !r.o.Clock().Before(stored.ExpiresAt) {
		stored.State = pa.PermissionExpired
	}
	var updated *pa.Turn
	events := []pa.Event{{Fence: stored.Fence, Kind: pa.PermissionAnswered, Permission: &stored}}
	remaining := a.pendingPermissions
	if entry.counted && a.permissionFence == stored.Fence && a.turnID == stored.TurnID && remaining > 0 {
		remaining--
	}
	if a.turnID == stored.TurnID && turn.Fence == stored.Fence && turn.State == pa.TurnWaitingPermission && remaining == 0 {
		turn.State = pa.TurnRunning
		updated = &turn
		events = append(events, pa.Event{Fence: turn.Fence, Kind: pa.TurnUpdated, Turn: &turn})
	}
	command := r.command(a.key, pa.AnswerOperation, "source-cancel-"+stored.ID, stored)
	if _, err := r.mutate(ctx, bot, Mutation{Command: command, Turn: updated, Permissions: []pa.PermissionOffer{stored}, Receipt: pa.Receipt{State: pa.EffectDelivered, ResourceID: stored.ID}, Events: events}); err == nil && entry.counted {
		retireSourcePermissionCount(a, entry)
	}
}

func retireSourcePermissionCount(a *runtimeActor, entry *sourcePermission) {
	if entry.counted && a.permissionFence == entry.offer.Fence && a.turnID == entry.offer.TurnID && a.pendingPermissions > 0 {
		a.pendingPermissions--
	}
	entry.counted = false
}
