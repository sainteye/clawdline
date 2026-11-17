package projectassistanthost

import (
	"context"
	"encoding/json"

	network "github.com/sainteye/clawdline/internal/app/botnetwork"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// NetworkHost is composed only with an independently configured Bot rail.
// It never authenticates through the human request-context authority.
type NetworkHost struct {
	Host      *Host
	AccountID string
}

func (n NetworkHost) Check(ctx context.Context, e wire.BotEndpoint, write bool) error {
	return n.check(ctx, e, write, false)
}
func (n NetworkHost) check(ctx context.Context, e wire.BotEndpoint, write, assistant bool) error {
	h := n.Host
	if h == nil || n.AccountID == "" || e.AccountID != n.AccountID || e.MachineID != h.o.MachineID {
		return failure(pa.ScopeDenied, "network_host")
	}
	key := pa.BotKey{MachineID: e.MachineID, BotID: e.BotID}
	_, _, err := h.background(ctx, key, write)
	if err != nil {
		return err
	}
	h.mu.Lock()
	st := h.store
	h.mu.Unlock()
	if st == nil {
		return failure(pa.Disabled, "network_store")
	}
	x, err := st.Export(ctx, key)
	if err != nil {
		return err
	}
	b, c := x.Bot, x.Conversation
	if b.Hidden || b.Project.ProjectID != e.ProjectID || c.ID != e.ConversationID {
		return failure(pa.ScopeDenied, "network_bot")
	}
	resolved, err := h.o.Projects.Resolve(ctx, pa.ProjectSelection{MachineID: b.Project.MachineID, ProjectID: b.Project.ProjectID})
	if err != nil {
		return err
	}
	if err = pa.ValidateTarget(pa.ProjectSelection{MachineID: b.Project.MachineID, ProjectID: b.Project.ProjectID}, resolved); err != nil {
		return err
	}
	if resolved.Scope != b.Project {
		return failure(pa.ScopeDenied, "network_project")
	}
	current := pa.Fence{Key: b.Key, Project: b.Project, ConversationID: c.ID, BindingID: c.BindingID, Generation: c.Generation}
	for _, binding := range x.Bindings {
		if binding.Fence == current && binding.Revoked {
			return failure(pa.Revoked, "network_binding")
		}
	}
	// One aggregate snapshot keeps a legitimate native binding transition from
	// looking like a revoked capture. Only the admitted turn may explain it.
	if current.Generation != e.Generation {
		matched := false
		for _, t := range x.Turns {
			if t.Network != nil && (t.Network.Header.Target == e || t.Network.Assistant != nil && t.Network.Assistant.Call.Target == e) && t.Fence == current {
				matched = true
				break
			}
		}
		if !matched {
			return failure(pa.StaleGeneration, "network_capture")
		}
	}
	settings, err := st.NetworkSettings(ctx, pa.NetworkScope{AccountID: n.AccountID, Key: key, Project: b.Project})
	if err != nil {
		return err
	}
	if !assistant && !settings.OfferedToNetwork || assistant && !settings.ReachableByAssistants {
		return failure(pa.Revoked, "network_offer")
	}
	return nil
}
func (n NetworkHost) Directory(ctx context.Context, target wire.BotEndpoint) ([]network.Entry, error) {
	if e := n.Check(ctx, target, false); e != nil {
		return nil, e
	}
	b, e := n.Host.store.Bot(ctx, pa.BotKey{MachineID: target.MachineID, BotID: target.BotID})
	if e != nil {
		return nil, e
	}
	return []network.Entry{{Endpoint: target, Name: b.Name}}, nil
}
func (n NetworkHost) Submit(ctx context.Context, h wire.BotHeader, text string) (string, error) {
	if e := n.Check(ctx, h.Target, true); e != nil {
		return "", e
	}
	key := pa.BotKey{MachineID: h.Target.MachineID, BotID: h.Target.BotID}
	host := n.Host
	current, e := host.current(ctx, key)
	if e != nil {
		return "", e
	}
	f, s, e := host.background(ctx, key, true)
	if e != nil {
		return "", e
	}
	r, e := host.runtime()
	if e != nil {
		return "", e
	}
	b, _ := json.Marshal(h)
	sum := network.Digest(b)
	command := pa.Command{Key: key, Operation: pa.SendOperation, IdempotencyKey: "network-" + network.Digest([]byte(h.Source.MachineID+"\n"+h.Source.BotID+"\n"+h.RequestID)), ContentHash: sum}
	receipt, e := r.Submit(ctx, app.Submission{Command: command, Feature: f, Source: s, Notice: true, Network: &pa.NetworkWork{Header: h}, AuthorityCheck: func(ctx context.Context) error { return host.checkNetworkHeader(ctx, h) }, Message: pa.SendMessage{Fence: current.Fence, Text: text}})
	return receipt.ResourceID, e
}
func (n NetworkHost) Result(ctx context.Context, r network.Receipt) (network.Receipt, error) {
	if e := n.Check(ctx, r.Header.Target, false); e != nil {
		return r, e
	}
	if r.State == "unknown" || r.State == "refused" || r.State == "reply_stored" {
		return r, nil
	}
	key := pa.BotKey{MachineID: r.Header.Target.MachineID, BotID: r.Header.Target.BotID}
	x, e := n.Host.store.Export(ctx, key)
	if e != nil {
		return r, e
	}
	for _, t := range x.Turns {
		if t.ID == r.TurnID {
			if t.Network == nil || t.Network.Header != r.Header {
				return r, failure(pa.ScopeDenied, "network_turn")
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
	return r, failure(pa.OutcomeUnknown, "network_turn")
}
func (h *Host) checkNetworkHeader(ctx context.Context, header wire.BotHeader) error {
	if h.o.Network == nil {
		return failure(pa.Unsupported, "network_unconfigured")
	}
	return h.o.Network.CheckRequest(ctx, header)
}
func (h *Host) checkNetworkTurn(ctx context.Context, t pa.Turn) error {
	if t.Network == nil {
		return failure(pa.ScopeDenied, "network_turn")
	}
	if t.Network.Assistant != nil {
		if h.o.Assistants == nil {
			return failure(pa.Unsupported, "assistant_unconfigured")
		}
		a := t.Network.Assistant
		p, _, e := h.o.Assistants.Verify(ctx, a.Call, []byte(a.Body))
		if e != nil {
			return e
		}
		if p.PrincipalID() != a.PrincipalID {
			return failure(pa.ScopeDenied, "assistant_principal")
		}
		return nil
	}
	return h.checkNetworkHeader(ctx, t.Network.Header)
}

func (w NativeWork) NetworkBot(ctx context.Context, q app.NetworkToolRequest) (network.Response, error) {
	if w.network == nil {
		return network.Response{}, failure(pa.Unsupported, "network_unconfigured")
	}
	return w.network(ctx, q)
}
