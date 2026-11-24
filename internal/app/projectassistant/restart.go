package projectassistant

import (
	"context"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

const resumePrompt = "[Clawdline restarted on this computer and interrupted you mid-task. Continue from the verified conversation already loaded. First inspect what finished. Do not repeat a write, send, dispatch or other external action from the previous turn. If an action's outcome cannot be verified, explain what is still open.]"
const staleResume = time.Hour

func restartInterrupted(t pa.Turn) bool {
	return t.State == pa.TurnUnknown && t.Failure != nil && (t.Failure.Field == RestartField || t.Failure.Field == RestartConfirmedField)
}

func restartOrigin(t pa.Turn) bool {
	return (t.Origin == pa.UserOrigin || t.Origin == "" || pa.WorkCompletionTask(t) != "") &&
		t.Room == nil && t.Network == nil && t.DecisionExchangeID == "" && t.RoutineRunID == ""
}

func restartNativeMode(agent pa.Agent) string {
	switch agent {
	case pa.ClaudeCode, pa.Gemini:
		return "plan"
	case pa.Codex:
		return "read-only"
	}
	return ""
}

func restartSource(pending []pa.Turn) (pa.Turn, bool) {
	var found pa.Turn
	for _, t := range pending {
		switch t.State {
		case pa.TurnRunning, pa.TurnWaitingPermission:
			return pa.Turn{}, false
		case pa.TurnUnknown:
			if !restartInterrupted(t) || t.Failure.Field != RestartConfirmedField || pa.RestartSourceTurn(t) != "" || found.ID != "" {
				return pa.Turn{}, false
			}
			found = t
		}
	}
	return found, found.ID != ""
}

// ResumeInterrupted admits one continuation ahead of the queue. The old
// outcome remains unknown until that continuation actually succeeds.
func (r *Engine) ResumeInterrupted(ctx context.Context, key pa.BotKey) error {
	pending, err := r.o.Repository.PendingTurns(ctx, key)
	if err != nil {
		return err
	}
	t, ok := restartSource(pending)
	if !ok {
		return nil
	}
	command := r.command(key, pa.SendOperation, pa.RestartContinuationPrefix+t.ID, t.ID)
	if receipt, err := r.o.Repository.Receipt(ctx, command); err != nil || receipt != nil {
		return err
	}
	x, err := r.o.Repository.Export(ctx, key)
	if err != nil {
		return err
	}
	task := pa.WorkCompletionTask(t)
	if !restartOrigin(t) || t.StartedAt == nil || len(t.Attempts) > 0 || restartNativeMode(x.Bot.Agent) == "" ||
		interruptedPermission(t, x.Permissions) || openRestartTool(x, t) {
		return nil
	}
	if card, e := r.handoffForTurn(ctx, t); e != nil || card != nil {
		return e
	}
	binding, err := r.o.Repository.Binding(ctx, key, t.Fence.BindingID)
	if err != nil || !matchesRestartBinding(x, t.ID, binding) {
		return nil
	}
	feature, source, err := r.o.Authorize(ctx, key)
	if err != nil {
		return err
	}
	if err = r.checkNativeHistory(ctx, binding); err != nil {
		return err
	}
	for _, message := range x.Messages {
		if message.ID != t.MessageID || r.o.Clock().Sub(message.At) >= staleResume {
			continue
		}
		quote := ""
		if task != "" {
			if err = checkWorkCompletion(x, task, message.QuoteMessageID, true); err != nil {
				return nil
			}
			quote = message.QuoteMessageID
		}
		_, err = r.Submit(ctx, Submission{Command: command, Feature: feature, Source: source, Notice: true, RestartSource: t.ID,
			Message: pa.SendMessage{Fence: currentFence(x.Bot, x.Conversation), Text: resumePrompt, QuoteMessageID: quote}})
		return err
	}
	return nil
}

func matchesRestartBinding(x Export, sourceID string, binding pa.ExecutionBinding) bool {
	if binding.Revoked || binding.NativeSessionID == "" || binding.Agent != x.Bot.Agent || binding.LoginProfile != x.Bot.LoginProfile ||
		binding.InstructionRevision != x.Bot.Instructions.Revision || binding.ForgottenThrough != x.Memory.ForgottenThrough ||
		binding.Fence.BindingID != x.Conversation.BindingID ||
		x.Conversation.SessionResetGeneration > 0 && binding.Fence.Generation <= x.Conversation.SessionResetGeneration ||
		!sameSessionOwner(binding.Fence, currentFence(x.Bot, x.Conversation)) {
		return false
	}
	for _, t := range x.Turns {
		if t.ID == sourceID {
			return restartInterrupted(t) && t.Fence == binding.Fence && t.MemoryRevision >= x.Memory.ForgottenThrough
		}
	}
	return false
}

// A trace with no terminal tool update, or a dispatch still in the unknown
// segment between preparation and a broker receipt, needs inspection.
func openRestartTool(x Export, t pa.Turn) bool {
	tool := map[string]bool{}
	for _, event := range x.Events {
		if tr := event.Trace; tr != nil && tr.TurnID == t.ID && tr.Category == "tool_call" {
			if tr.ID == "" && !tr.Complete {
				return true
			}
			tool[tr.ID] = tr.Complete
		}
	}
	for _, complete := range tool {
		if !complete {
			return true
		}
	}
	return restartDispatchUnconfirmed(x, t.ID)
}

// The durable admission receipt identifies the queue entry across restarts.
// An interrupted recovery itself is left for inspection, never replayed.
func (r *Engine) restartQueueHead(ctx context.Context, key pa.BotKey, pending []pa.Turn) (*pa.Turn, bool, error) {
	old, ok := restartSource(pending)
	if !ok {
		return nil, false, nil
	}
	command := r.command(key, pa.SendOperation, pa.RestartContinuationPrefix+old.ID, old.ID)
	receipt, err := r.o.Repository.Receipt(ctx, command)
	if err != nil || receipt == nil {
		return nil, false, err
	}
	resume, err := r.o.Repository.Turn(ctx, key, receipt.ResourceID)
	if err != nil || pa.RestartSourceTurn(resume) != old.ID {
		return nil, false, err
	}
	if resume.State == pa.TurnQueued && resume.StartedAt == nil {
		return &resume, false, nil
	}
	if resume.State != pa.TurnSucceeded {
		return nil, false, nil
	}
	for {
		bot, err := r.o.Repository.Bot(ctx, key)
		if err != nil {
			return nil, false, err
		}
		old.State = pa.TurnFailed
		old.Failure = &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: pa.RestartResumedField}
		_, err = r.mutate(ctx, bot, Mutation{
			Command: r.command(key, pa.SendOperation, "runtime-restart-resolved-"+old.ID, resume.ID), Turn: &old,
			Receipt: pa.Receipt{State: pa.EffectUnknown, ResourceID: old.ID},
			Events:  []pa.Event{{Fence: old.Fence, Kind: pa.TurnUpdated, Turn: &old}},
		})
		if !collaborationAggregateConflict(err) || ctx.Err() != nil {
			return nil, err == nil, err
		}
	}
}
