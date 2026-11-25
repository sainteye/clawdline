package projectassistant

import (
	"context"
	"strings"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"github.com/sainteye/clawdline/internal/productcopy"
)

// PeerDispatchNoticePrefix starts the turn and message ids of the notice a
// Bot's main chat shows when its private thread worker dispatched work at
// another Bot's request. The dispatched task attaches to that turn.
const PeerDispatchNoticePrefix = "peer-dispatch-"

// PeerDispatchNoticeText is the notice's whole person-facing text in the
// daemon's product language, rendered once when the work is dispatched; an
// absent or unsupported language uses English. The requester is named by the
// part of its display name before "｜", so a role suffix does not lengthen it.
func PeerDispatchNoticeText(language, requester, title string) string {
	requester = PeerDispatchRequesterName(requester)
	if requester == "" {
		requester = productcopy.Format(language, "peer_dispatch.another_bot", nil)
	}
	if title == "" {
		title = "-"
	}
	return productcopy.Format(language, "peer_dispatch.notice", map[string]string{"requester": requester, "title": title})
}

// PeerDispatchRequesterName is a Bot display name cut before its first "｜"
// and trimmed of surrounding space.
func PeerDispatchRequesterName(name string) string {
	if i := strings.Index(name, "｜"); i >= 0 {
		name = name[:i]
	}
	return strings.TrimSpace(name)
}

// PeerDispatchFence resolves the main chat before admission without writing
// a notice. A replay retains the fence of its already admitted dispatch.
func (r *Engine) PeerDispatchFence(ctx context.Context, owner pa.Fence, turnID string) (pa.Fence, error) {
	x, err := r.peerDispatchChat(ctx, owner)
	if err != nil {
		return pa.Fence{}, err
	}
	for _, t := range x.Turns {
		if t.ID == turnID {
			return t.Fence, nil
		}
	}
	return currentFence(x.Bot, x.Conversation), nil
}

func (r *Engine) peerDispatchChat(ctx context.Context, owner pa.Fence) (Export, error) {
	x, err := r.o.Repository.Export(ctx, owner.Key)
	if err != nil {
		return Export{}, err
	}
	if x.Bot.Project != owner.Project || x.Bot.ConversationID != owner.ConversationID {
		return Export{}, runtimeFailure(pa.StaleVersion, "work_owner")
	}
	return x, r.authorized(ctx, owner.Key, x.Bot.Project)
}

// RecordPeerDispatchFailure writes a separate, finished card for a refusal
// that left no task in the broker. It cannot own or wake any work.
func (r *Engine) RecordPeerDispatchFailure(ctx context.Context, q pa.WorkRequest, taskID, code, field string) error {
	a, err := r.acquire(q.OwnerFence.Key)
	if err != nil {
		return err
	}
	defer r.release(a)
	a.mu.Lock()
	defer a.mu.Unlock()
	x, err := r.peerDispatchChat(ctx, q.OwnerFence)
	if err != nil {
		return err
	}
	id := "peer-dispatch-failed-" + taskID
	for _, t := range x.Turns {
		if t.ID == id || t.ID == q.OwnerTurnID {
			return nil
		}
	}
	fence := currentFence(x.Bot, x.Conversation)
	turn, message := peerDispatchNotice(x, fence, id, taskID, q.Title, q.PeerDispatch, r.o.Clock(), code, field)
	_, err = r.mutate(ctx, x.Bot, Mutation{
		Command: r.command(fence.Key, pa.SendOperation, id, message.Text), Turn: &turn, Messages: []pa.Message{message},
		Receipt: pa.Receipt{State: pa.EffectDelivered, ResourceID: id},
		Events:  []pa.Event{{Fence: fence, Kind: pa.MessageCreated, Message: &message}, {Fence: fence, Kind: pa.TurnUpdated, Turn: &turn}},
	})
	return err
}

func peerDispatchNotice(x Export, fence pa.Fence, id, taskID, title string, requester pa.PeerDispatchAttribution, at time.Time, code, field string) (pa.Turn, pa.Message) {
	ref := &pa.NoticeRef{Key: pa.NoticePeerDispatch, Cause: taskID, Requester: requester.Requester, TaskTitle: title}
	if pa.ValidateAppearance(requester.Appearance) == nil {
		ref.RequesterAppearance = &requester.Appearance
	}
	var text string
	if code == "" {
		text = PeerDispatchNoticeText(requester.Language, requester.Requester, title)
	} else {
		ref.Key, ref.FailureCode, ref.FailureField = pa.NoticePeerDispatchFailed, code, field
		text = productcopy.Format(requester.Language, "peer_dispatch.failed", nil)
	}
	turn := pa.Turn{ID: id, Fence: fence, MessageID: id, State: pa.TurnSucceeded, Origin: pa.HostNoticeOrigin,
		IdempotencyKey: id, MemoryRevision: x.Memory.Revision, Instructions: x.Bot.Instructions}
	return turn, pa.Message{ID: id, Fence: fence, TurnID: id, Kind: pa.NoticeMessage, Text: text, Notice: ref, At: at}
}
