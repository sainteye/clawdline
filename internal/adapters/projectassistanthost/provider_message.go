package projectassistanthost

import (
	"context"
	"errors"

	featurestore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

var _ app.WorkMessageTools = NativeWork{}

// messageWorkTool binds message_work to the execution's own signed actor. The
// broker then decides from the task record whether this conversation, or the
// Bot that owns this peer-request thread, owns a running work Session; the
// model never supplies a terminal, an owner or an identity.
//
// On a turn answering another Bot, the text is prefixed with who asked, so the
// worker reads a relayed request from a peer rather than its dispatcher's own
// words. Neither is the person's authorization.
func messageWorkTool(turn pa.Turn, actor pa.AssistantExecutionActor, b *app.BrokerWorkBridge,
	bound func(context.Context) (context.Context, context.CancelFunc, error),
	journal func(context.Context, string) error,
	requester func(context.Context) string,
) func(context.Context, string, string, string) (pa.WorkMessageReceipt, error) {
	return func(ctx context.Context, taskID, text, key string) (pa.WorkMessageReceipt, error) {
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return pa.WorkMessageReceipt{}, e
		}
		defer cancel()
		if actor.CapabilityID == "" {
			return pa.WorkMessageReceipt{}, failure(pa.ScopeDenied, "dispatch_policy")
		}
		if pa.IsPeerRequest(turn.Origin) {
			text = relayedWorkMessage(requester(ctx), text)
		}
		if e = journal(ctx, "message_work"); e != nil {
			return pa.WorkMessageReceipt{}, e
		}
		receipt, e := b.Message(ctx, actor, taskID, text, key)
		return receipt, peerWorkRefusal(turn, e)
	}
}

// peerWorkRefusal tells a Bot answering another Bot why a task is out of
// reach: only work this Bot itself dispatched is, so the next step is to ask
// the Bot that dispatched it. Every other answer passes through unchanged.
func peerWorkRefusal(turn pa.Turn, err error) error {
	var f *pa.Failure
	if pa.IsPeerRequest(turn.Origin) && errors.As(err, &f) && f.Code == pa.ScopeDenied && f.Field == "work_owner" {
		return failure(pa.ScopeDenied, "work_dispatched_by_another_bot")
	}
	return err
}

// mainChatOnlyMessageWork answers message_work on a Bot's background thread
// that is not answering another Bot. The work belongs to the Bot's own chat,
// so the refusal names where to send the message from instead.
func mainChatOnlyMessageWork(context.Context, string, string, string) (pa.WorkMessageReceipt, error) {
	return pa.WorkMessageReceipt{}, failure(pa.ScopeDenied, "message_work_from_main_chat")
}

// relayedWorkMessage names the Bot whose request the dispatcher passes on. A
// requester on another machine, or one no longer readable, is named generically.
func relayedWorkMessage(requester, text string) string {
	if requester == "" {
		return "轉述另一個 Bot 的請求／relaying a request from another Bot:\n" + text
	}
	return "轉述 " + requester + " 的請求／relaying a request from " + requester + ":\n" + text
}

// peerRequester reads the name of the local Bot that sent this peer request
// from the exchange record. A network request carries no local Bot name.
func (h *Host) peerRequester(ctx context.Context, key pa.BotKey, turn pa.Turn) string {
	if turn.Origin != pa.BotRequestOrigin || turn.ExchangeID == "" {
		return ""
	}
	x, err := h.store.Exchange(ctx, key, turn.ExchangeID)
	if err != nil {
		return ""
	}
	sender, err := h.store.Bot(ctx, x.Source.Key)
	if err != nil {
		return ""
	}
	return sender.Name
}

// peerWorkOwner resolves a private thread worker to the conversation of the
// Bot that durably owns it, the one that dispatched that Bot's work. A Bot's
// own chat, a missing owner or a changed Project resolves to nothing, which
// leaves the broker comparing the actor's own conversation only.
func peerWorkOwner(ctx context.Context, st *featurestore.Store, worker pa.Bot) pa.Fence {
	owner, err := st.BackgroundOwner(ctx, worker.Key)
	if err != nil || owner.BotID == "" || owner.MachineID != worker.Key.MachineID {
		return pa.Fence{}
	}
	bot, err := st.Bot(ctx, owner)
	if err != nil || bot.Project != worker.Project || bot.ConversationID == "" {
		return pa.Fence{}
	}
	return pa.Fence{Key: owner, Project: bot.Project, ConversationID: bot.ConversationID}
}

func (w NativeWork) MessageWork(ctx context.Context, taskID, text, key string) (pa.WorkMessageReceipt, error) {
	if w.messageWork == nil {
		return pa.WorkMessageReceipt{}, failure(pa.Disabled, "native_work")
	}
	return w.messageWork(ctx, taskID, text, key)
}
