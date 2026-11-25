package projectassistant

import (
	"context"
	"fmt"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

const resumePrompt = "[Clawdline restarted on this computer and interrupted you mid-task. Continue from the verified conversation already loaded. First inspect what finished. Do not repeat a write, send, dispatch or other external action from the previous turn. If an action's outcome cannot be verified, explain what is still open.]"
const staleResume = time.Hour

// restartNotRepeatedPrompt opens the first prompt after an interrupted turn
// was settled without a continuation; it carries the meaning of resumePrompt.
const restartNotRepeatedPrompt = "[Clawdline restarted on this computer and interrupted your previous turn. It was not repeated. Before acting on the message below, first inspect what finished. Do not repeat a write, send, dispatch or other external action from the interrupted turn. If an action's outcome cannot be verified, explain what is still open.]"

// restartReleasedPrompt opens the person's message that released a queue
// held by an interrupted turn. It runs ahead of the automatic work that
// waited in that queue, so it names how much is still to be delivered.
func restartReleasedPrompt(held int) string {
	text := "[Clawdline restarted on this computer and interrupted your previous turn. It was not repeated. The person's message below released the waiting queue and runs first. Before acting on it, first inspect what finished. Do not repeat a write, send, dispatch or other external action from the interrupted turn. If an action's outcome cannot be verified, explain what is still open."
	if held > 0 {
		text += fmt.Sprintf(" %d automatic item(s), such as work reports or other Bots' messages, are still queued and will be delivered after this message; do not act on their likely contents before you receive them.", held)
	}
	return text + "]"
}

// releasesRestart reports whether acknowledging these unknown turns releases
// a restart hold. A continuation that still owns recovery keeps its place.
func releasesRestart(acknowledged []pa.Turn, recoverySource string) bool {
	for _, t := range acknowledged {
		if recoverySource == "" && restartInterrupted(t) {
			return true
		}
	}
	return false
}

// restartPromptHeader opens the first prompt after an interrupted turn was
// settled unrepeated, so it inspects what finished before it acts; empty
// otherwise. It ends with a blank line before the prompt's own text.
func restartPromptHeader(x Export, t pa.Turn, isolatedRoutine bool) string {
	switch {
	case isolatedRoutine:
		return ""
	case t.ReleasedRestart:
		return restartReleasedPrompt(heldAutomaticWork(x, t.ID)) + "\n\n"
	case pa.RestartSourceTurn(t) == "" && restartNotRepeatedHeader(x, t):
		return restartNotRepeatedPrompt + "\n\n"
	}
	return ""
}

// heldAutomaticWork counts the automatic work still queued besides turnID.
func heldAutomaticWork(x Export, turnID string) int {
	held := 0
	for _, t := range x.Turns {
		if t.ID != turnID && pa.HeldAutomaticWork(t) {
			held++
		}
	}
	return held
}

// releaseLateReview applies the restart decision again once a review is
// queued: a review admitted after the restart waits behind the interrupted
// turn exactly like work already queued when the restart was decided, and
// is released the same way instead of waiting for the person's next message.
func (r *Engine) releaseLateReview(ctx context.Context, key pa.BotKey) error {
	if err := r.ResumeInterrupted(ctx, key); err != nil {
		return err
	}
	r.wakeBot(key)
	return nil
}

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
// outcome remains unknown until that continuation actually succeeds. When
// the continuation is declined, a queue waiting behind the interrupted turn
// is released instead (releaseDeclinedRestart); nothing is repeated.
func (r *Engine) ResumeInterrupted(ctx context.Context, key pa.BotKey) error {
	pending, err := r.o.Repository.PendingTurns(ctx, key)
	if err != nil {
		return err
	}
	t, ok := restartSource(pending)
	if !ok {
		return r.declined(ctx, key, pending)
	}
	command := r.command(key, pa.SendOperation, pa.RestartContinuationPrefix+t.ID, t.ID)
	if receipt, err := r.o.Repository.Receipt(ctx, command); err != nil || receipt != nil {
		// An admitted continuation is the queue head (restartQueueHead).
		return err
	}
	x, err := r.o.Repository.Export(ctx, key)
	if err != nil {
		return err
	}
	task := pa.WorkCompletionTask(t)
	if !restartOrigin(t) || t.StartedAt == nil || len(t.Attempts) > 0 || restartNativeMode(x.Bot.Agent) == "" ||
		interruptedPermission(t, x.Permissions) || openRestartTool(x, t) {
		return r.declined(ctx, key, pending)
	}
	if card, e := r.handoffForTurn(ctx, t); e != nil || card != nil {
		return e
	}
	binding, err := r.o.Repository.Binding(ctx, key, t.Fence.BindingID)
	if err != nil {
		return nil
	}
	if !matchesRestartBinding(x, t.ID, binding) {
		return r.declined(ctx, key, pending)
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
				return r.declined(ctx, key, pending)
			}
			quote = message.QuoteMessageID
		}
		_, err = r.Submit(ctx, Submission{Command: command, Feature: feature, Source: source, Notice: true, RestartSource: t.ID,
			Message: pa.SendMessage{Fence: currentFence(x.Bot, x.Conversation), Text: resumePrompt, QuoteMessageID: quote}})
		return err
	}
	return r.declined(ctx, key, pending)
}

// restartNotRepeatedPrefix names the chat notice that an interrupted turn
// was settled without being repeated; one per turn, so the ID is stable.
const restartNotRepeatedPrefix = "restart-not-repeated-"

// releaseDeclinedRestart settles turns a verified restart cut off when no
// continuation will run for them, so the queue behind them drains without
// waiting for the person's next message. Only a turn whose native cleanup was
// confirmed and that holds no open permission qualifies, and only when every
// unknown turn does; nothing runs meanwhile and something waits behind them.
// The old execution receipt stays unknown, a visible notice says the turn was
// not repeated, and the next prompt starts with restartNotRepeatedPrompt.
func (r *Engine) releaseDeclinedRestart(ctx context.Context, key pa.BotKey, pending []pa.Turn) (bool, error) {
	var unknown []pa.Turn
	queued := false
	for _, t := range pending {
		switch t.State {
		case pa.TurnRunning, pa.TurnWaitingPermission:
			return false, nil
		case pa.TurnUnknown:
			if !restartInterrupted(t) || t.Failure.Field != RestartConfirmedField || pa.RestartSourceTurn(t) != "" {
				return false, nil
			}
			unknown = append(unknown, t)
		case pa.TurnQueued, pa.TurnAccepted:
			if t.Steering || pa.RestartSourceTurn(t) != "" {
				return false, nil
			}
			queued = true
		}
	}
	if len(unknown) == 0 || !queued {
		return false, nil
	}
	for _, t := range unknown {
		x, err := r.o.Repository.Export(ctx, key)
		if err != nil {
			return false, err
		}
		if interruptedPermission(t, x.Permissions) {
			return false, nil
		}
		old := t
		old.State = pa.TurnFailed
		old.Failure = &pa.Failure{Code: pa.ExecutionEffectUnknown, Field: pa.RestartNotRepeatedField}
		c := x.Conversation
		if c.ActiveTurnID == t.ID {
			c.ActiveTurnID = ""
		}
		id := restartNotRepeatedPrefix + t.ID
		msg := pa.Message{ID: id, Fence: t.Fence, TurnID: t.ID, Kind: pa.NoticeMessage, At: r.o.Clock(),
			Text:   "Clawdline restarted while this turn was running. It was not repeated; the Bot continues with the next message and first checks what finished. / Clawdline 重新啟動時這個回合還在執行。它沒有重做；Bot 會繼續處理下一則訊息，並先確認哪些已經完成。",
			Notice: &pa.NoticeRef{Key: pa.NoticeRestartNotRepeated, Cause: t.ID}}
		_, err = r.mutate(ctx, x.Bot, Mutation{
			Command: r.command(key, pa.SendOperation, id, t.ID), Conversation: &c, Turn: &old,
			Messages: []pa.Message{msg}, Receipt: pa.Receipt{State: pa.EffectUnknown, ResourceID: t.ID},
			Events: []pa.Event{{Fence: old.Fence, Kind: pa.TurnUpdated, Turn: &old}, {Fence: t.Fence, Kind: pa.MessageCreated, Message: &msg}},
		})
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

func (r *Engine) declined(ctx context.Context, key pa.BotKey, pending []pa.Turn) error {
	_, err := r.releaseDeclinedRestart(ctx, key, pending)
	return err
}

// restartNotRepeatedHeader reports whether turn is the first to start after a
// declined restart continuation was settled, so its prompt inspects first.
func restartNotRepeatedHeader(x Export, turn pa.Turn) bool {
	var notice *pa.Message
	for i := range x.Messages {
		m := &x.Messages[i]
		if m.Notice != nil && m.Notice.Key == pa.NoticeRestartNotRepeated && (notice == nil || m.Seq > notice.Seq) {
			notice = m
		}
	}
	if notice == nil {
		return false
	}
	for _, t := range x.Turns {
		if t.ID != turn.ID && t.StartedAt != nil && !t.StartedAt.Before(notice.At) {
			return false
		}
	}
	return true
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
	if resume.State == pa.TurnFailed || resume.State == pa.TurnCancelled {
		// The continuation ended without settling its source; nothing will
		// resume it, so the queue behind it is released without a repeat.
		released, err := r.releaseDeclinedRestart(ctx, key, pending)
		return nil, released, err
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
