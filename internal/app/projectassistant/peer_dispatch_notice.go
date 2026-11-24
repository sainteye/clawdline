package projectassistant

import (
	"context"
	"fmt"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// PeerDispatchNoticePrefix starts the turn and message ids of the notice a
// Bot's main chat shows when its private thread worker dispatched work at
// another Bot's request. The dispatched task attaches to that turn.
const PeerDispatchNoticePrefix = "peer-dispatch-"

// PeerDispatchNoticeText is the notice's whole person-facing text, in English
// and Taiwan Traditional Chinese like the other host notices.
func PeerDispatchNoticeText(requester, title string) string {
	en, zh := requester, requester
	if requester == "" {
		en, zh = "another Bot", "另一個 Bot"
	}
	if title == "" {
		title = "-"
	}
	return fmt.Sprintf("Dispatched task \"%s\" at %s's request. / 應 %s 的請求派出任務「%s」。", title, en, zh, title)
}

// AnchorPeerDispatch writes, once per task, a finished host-notice turn into
// the main chat key and returns its fence and turn. A task a thread worker
// dispatches is owned by that turn, so it shows, is read and wakes in the
// main chat exactly as a task dispatched there. Repeating the same task id
// returns the turn already written; nothing is written twice.
func (r *Engine) AnchorPeerDispatch(ctx context.Context, key pa.BotKey, taskID, text string) (pa.Fence, string, error) {
	if taskID == "" {
		return pa.Fence{}, "", runtimeFailure(pa.Invalid, "peer_dispatch_notice")
	}
	if err := pa.ValidateMessage(text); err != nil {
		return pa.Fence{}, "", err
	}
	a, err := r.acquire(key)
	if err != nil {
		return pa.Fence{}, "", err
	}
	defer r.release(a)
	a.mu.Lock()
	defer a.mu.Unlock()
	x, err := r.o.Repository.Export(ctx, key)
	if err != nil {
		return pa.Fence{}, "", err
	}
	if err = r.authorized(ctx, key, x.Bot.Project); err != nil {
		return pa.Fence{}, "", err
	}
	id := PeerDispatchNoticePrefix + taskID
	for _, t := range x.Turns {
		if t.ID == id {
			return t.Fence, t.ID, nil
		}
	}
	fence := currentFence(x.Bot, x.Conversation)
	turn := pa.Turn{ID: id, Fence: fence, MessageID: id, State: pa.TurnSucceeded, Origin: pa.HostNoticeOrigin,
		IdempotencyKey: id, MemoryRevision: x.Memory.Revision, Instructions: x.Bot.Instructions}
	message := pa.Message{ID: id, Fence: fence, TurnID: id, Kind: pa.NoticeMessage, Text: text, At: r.o.Clock()}
	_, err = r.mutate(ctx, x.Bot, Mutation{
		Command: r.command(key, pa.SendOperation, id, text), Turn: &turn, Messages: []pa.Message{message},
		Receipt: pa.Receipt{State: pa.EffectDelivered, ResourceID: id},
		Events:  []pa.Event{{Fence: fence, Kind: pa.MessageCreated, Message: &message}, {Fence: fence, Kind: pa.TurnUpdated, Turn: &turn}},
	})
	if err != nil {
		return pa.Fence{}, "", err
	}
	return fence, id, nil
}
