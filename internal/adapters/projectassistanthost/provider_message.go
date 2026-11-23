package projectassistanthost

import (
	"context"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

var _ app.WorkMessageTools = NativeWork{}

// messageWorkTool binds message_work to the execution's own signed actor. The
// broker then decides from the task record whether this conversation owns a
// running work Session; the model never supplies a terminal or identity.
func messageWorkTool(turn pa.Turn, actor pa.AssistantExecutionActor, b *app.BrokerWorkBridge,
	bound func(context.Context) (context.Context, context.CancelFunc, error),
	journal func(context.Context, string) error,
) func(context.Context, string, string, string) (pa.WorkMessageReceipt, error) {
	return func(ctx context.Context, taskID, text, key string) (pa.WorkMessageReceipt, error) {
		if pa.IsPeerRequest(turn.Origin) {
			return pa.WorkMessageReceipt{}, failure(pa.ScopeDenied, "peer_message_work")
		}
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return pa.WorkMessageReceipt{}, e
		}
		defer cancel()
		if actor.CapabilityID == "" {
			return pa.WorkMessageReceipt{}, failure(pa.ScopeDenied, "dispatch_policy")
		}
		if e = journal(ctx, "message_work"); e != nil {
			return pa.WorkMessageReceipt{}, e
		}
		return b.Message(ctx, actor, taskID, text, key)
	}
}

func (w NativeWork) MessageWork(ctx context.Context, taskID, text, key string) (pa.WorkMessageReceipt, error) {
	if w.messageWork == nil {
		return pa.WorkMessageReceipt{}, failure(pa.Disabled, "native_work")
	}
	return w.messageWork(ctx, taskID, text, key)
}
