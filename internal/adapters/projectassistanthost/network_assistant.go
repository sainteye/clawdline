package projectassistanthost

import (
	"context"
	"encoding/json"

	network "github.com/sainteye/clawdline/internal/app/botnetwork"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func (n NetworkHost) CheckAssistant(ctx context.Context, target wire.BotEndpoint, write bool) (network.AssistantTargetState, error) {
	if err := n.check(ctx, target, write, true); err != nil {
		return network.AssistantTargetState{}, err
	}
	feature, source, err := n.Host.background(ctx, pa.BotKey{MachineID: target.MachineID, BotID: target.BotID}, write)
	return network.AssistantTargetState{FeatureEnabled: feature.Enabled, SourceWritable: source.Writable && source.Fresh, TargetCurrent: err == nil, TargetReachable: err == nil}, err
}
func (n NetworkHost) DirectoryAssistant(ctx context.Context, target wire.BotEndpoint, allowed []string) ([]network.Entry, error) {
	if _, err := n.CheckAssistant(ctx, target, false); err != nil {
		return nil, err
	}
	for _, id := range allowed {
		if id == target.BotID {
			b, err := n.Host.store.Bot(ctx, pa.BotKey{MachineID: target.MachineID, BotID: id})
			return []network.Entry{{Endpoint: target, Name: b.Name}}, err
		}
	}
	return nil, failure(pa.ScopeDenied, "assistant_bot_scope")
}
func (n NetworkHost) SubmitAssistant(ctx context.Context, call wire.AssistantCall, principal wire.VerifiedAssistant, text string, body []byte) (string, error) {
	if n.Host.o.Assistants == nil || principal.PrincipalID() == "" {
		return "", failure(pa.Unsupported, "assistant_unconfigured")
	}
	fresh, _, err := n.Host.o.Assistants.Verify(ctx, call, body)
	if err != nil {
		return "", err
	}
	if fresh != principal {
		return "", failure(pa.ScopeDenied, "assistant_principal")
	}
	key := pa.BotKey{MachineID: call.Target.MachineID, BotID: call.Target.BotID}
	current, err := n.Host.current(ctx, key)
	if err != nil {
		return "", err
	}
	feature, source, err := n.Host.background(ctx, key, true)
	if err != nil {
		return "", err
	}
	r, err := n.Host.runtime()
	if err != nil {
		return "", err
	}
	captured := pa.NetworkWork{Assistant: &pa.AssistantWork{Call: call, Body: string(body), PrincipalID: principal.PrincipalID()}}
	encoded, _ := json.Marshal(call)
	identity := network.Digest([]byte(principal.PrincipalID() + "\n" + call.GrantID + "\n" + call.RequestID))
	receipt, err := r.Submit(ctx, app.Submission{Command: pa.Command{Key: key, Operation: pa.SendOperation, IdempotencyKey: "assistant-" + identity, ContentHash: network.Digest(encoded)}, Notice: true, Network: &captured, Feature: feature, Source: source, Message: pa.SendMessage{Fence: current.Fence, Text: text}, AuthorityCheck: func(ctx context.Context) error { return n.Host.checkNetworkTurn(ctx, pa.Turn{Network: &captured}) }})
	return receipt.ResourceID, err
}
func (n NetworkHost) ResultAssistant(ctx context.Context, r network.AssistantReceipt) (network.AssistantReceipt, error) {
	if _, err := n.CheckAssistant(ctx, r.Call.Target, false); err != nil {
		return r, err
	}
	if r.State == "unknown" || r.State == "refused" || r.State == "reply_stored" {
		return r, nil
	}
	key := pa.BotKey{MachineID: r.Call.Target.MachineID, BotID: r.Call.Target.BotID}
	x, err := n.Host.store.Export(ctx, key)
	if err != nil {
		return r, err
	}
	for _, t := range x.Turns {
		if t.ID == r.TurnID {
			if t.Network == nil || t.Network.Assistant == nil || t.Network.Assistant.Call != r.Call || t.Network.Assistant.PrincipalID != r.PrincipalID {
				return r, failure(pa.ScopeDenied, "assistant_turn")
			}
			switch t.State {
			case pa.TurnSucceeded:
				r.State = "reply_stored"
				for _, m := range x.Messages {
					if m.TurnID == t.ID && m.Kind == pa.AssistantMessage {
						r.Reply = pa.CollaborationReply(m.Text)
					}
				}
			case pa.TurnUnknown:
				r.State = "unknown"
				r.Failure = t.Failure
			case pa.TurnFailed, pa.TurnCancelled:
				r.State = "failed"
				r.Failure = t.Failure
			case pa.TurnRunning, pa.TurnWaitingPermission:
				r.State = "source_executing"
			}
			return r, nil
		}
	}
	return r, failure(pa.OutcomeUnknown, "assistant_turn")
}
