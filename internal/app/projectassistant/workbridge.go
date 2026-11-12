package projectassistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type WorkBridgeOptions struct {
	Repository Repository
	Broker     *orchestrator.Broker
	Clock      func() time.Time
}

// BrokerWorkBridge owns only preparation receipts and source projections. The
// broker owns task admission/effects; Runtime owns the original command receipt.
// No queue, timer, model call or automatic unknown-effect replay lives here.
type BrokerWorkBridge struct {
	o          WorkBridgeOptions
	dispatchMu sync.Mutex
	syncMu     sync.Mutex
}

var _ WorkBridge = (*BrokerWorkBridge)(nil)

func bridgeFailure(code pa.ErrorCode, field string) error {
	return &pa.Failure{Code: code, Field: field}
}

func NewWorkBridge(o WorkBridgeOptions) (*BrokerWorkBridge, error) {
	if o.Repository == nil || o.Broker == nil || o.Broker.AssistantAuthority == nil || o.Clock == nil {
		return nil, bridgeFailure(pa.Invalid, "work_bridge")
	}
	return &BrokerWorkBridge{o: o}, nil
}

func bridgeCommand(key pa.BotKey, id string, payload any) pa.Command {
	body, _ := json.Marshal(payload)
	digest := sha256.Sum256(body)
	return pa.Command{Key: key, Operation: pa.DispatchOperation, IdempotencyKey: id, ContentHash: hex.EncodeToString(digest[:])}
}

func bridgeTaskMissing(err error) bool {
	var refusal orchestrator.Refusal
	return errors.As(err, &refusal) && refusal.Status == http.StatusNotFound && refusal.Code == "not_found"
}

func (b *BrokerWorkBridge) commit(ctx context.Context, bot pa.Bot, m Mutation) (pa.Receipt, error) {
	m.ExpectedVersion = bot.Version
	bot.Version++
	bot.UpdatedAt = b.o.Clock()
	m.Bot = &bot
	m.Receipt.Command = m.Command
	m.Receipt.ID = m.Command.IdempotencyKey
	for i := range m.Events {
		m.Events[i].Version = pa.ContractVersion
		m.Events[i].ID = m.Command.IdempotencyKey + "-event-" + m.Events[i].ID
		m.Events[i].At = b.o.Clock()
	}
	return b.o.Repository.Commit(ctx, m)
}

func (b *BrokerWorkBridge) Dispatch(ctx context.Context, q pa.WorkRequest) (pa.WorkLink, error) {
	b.dispatchMu.Lock()
	defer b.dispatchMu.Unlock()
	if err := b.o.Broker.ValidateAssistantWork(ctx, q); err != nil {
		return pa.WorkLink{}, err
	}
	id := orchestrator.AssistantTaskID(q.Command)
	// A task accepted before a crash is recovered from broker facts even if
	// the source WorkLink transaction never happened. No launch is repeated.
	if snapshot, err := b.o.Broker.AssistantStatus(ctx, q.Actor, id); err == nil {
		if snapshot.Owner.Command != q.Command || snapshot.Owner.RequestHash != orchestrator.AssistantRequestHash(q) {
			return pa.WorkLink{}, bridgeFailure(pa.IdempotencyConflict, "work_command")
		}
		if err = b.Sync(ctx, snapshot); err != nil {
			return pa.WorkLink{}, err
		}
		return snapshot.Link, nil
	} else if !bridgeTaskMissing(err) {
		return pa.WorkLink{}, err
	}
	// This namespace is separate from Engine's q.Command. An Engine receipt
	// written before the FIRST bridge call is expected and is not a replay.
	prepare := bridgeCommand(q.Command.Key, "bridge-prepare-"+id, orchestrator.AssistantRequestHash(q))
	prior, err := b.o.Repository.Receipt(ctx, prepare)
	if err != nil {
		return pa.WorkLink{}, err
	}
	if _, err = pa.DecideReplay(prepare, prior); err != nil {
		return pa.WorkLink{}, err
	}
	if prior != nil {
		return pa.WorkLink{}, bridgeFailure(pa.OutcomeUnknown, "work_preparation")
	}
	bot, err := b.o.Repository.Bot(ctx, q.Command.Key)
	if err != nil {
		return pa.WorkLink{}, err
	}
	if bot.Project != q.Actor.Fence.Project || bot.ConversationID != q.Actor.Fence.ConversationID {
		return pa.WorkLink{}, bridgeFailure(pa.ScopeDenied, "work_bot")
	}
	if _, err = b.commit(ctx, bot, Mutation{Command: prepare, Receipt: pa.Receipt{State: pa.EffectUnknown, ResourceID: id}}); err != nil {
		return pa.WorkLink{}, err
	}
	accepted, err := b.o.Broker.DispatchAssistant(ctx, q)
	if err != nil {
		return pa.WorkLink{}, err
	}
	if accepted.Record.AssistantOwner == nil || accepted.Record.Worktree == nil || accepted.Record.Worktree.Path == "" {
		return pa.WorkLink{}, bridgeFailure(pa.OutcomeUnknown, "work_acceptance")
	}
	// Status rereads the durable row rather than turning a launch response
	// into completion evidence. Sync must commit before the link is returned.
	return b.Status(ctx, q.Actor, id)
}

func (b *BrokerWorkBridge) Status(ctx context.Context, actor pa.AssistantExecutionActor, taskID string) (pa.WorkLink, error) {
	snapshot, err := b.o.Broker.AssistantStatus(ctx, actor, taskID)
	if err != nil {
		return pa.WorkLink{}, err
	}
	if err = b.Sync(ctx, snapshot); err != nil {
		return pa.WorkLink{}, err
	}
	return snapshot.Link, nil
}

// Sync is the host-only Broker.AssistantWorkChanged callback, never a client
// route. It re-reads broker provenance and current state before writing. A
// failed transaction leaves the broker facts queryable through Status.
func (b *BrokerWorkBridge) Sync(ctx context.Context, snapshot orchestrator.AssistantWorkSnapshot) error {
	b.syncMu.Lock()
	defer b.syncMu.Unlock()
	r, _, err := b.o.Broker.Record(ctx, snapshot.Link.TaskID)
	if err != nil {
		return err
	}
	if r.AssistantOwner == nil || *r.AssistantOwner != snapshot.Owner {
		return bridgeFailure(pa.ScopeDenied, "work_snapshot")
	}
	snapshot = orchestrator.AssistantWorkSnapshotOf(r)
	export, err := b.o.Repository.Export(ctx, snapshot.Owner.Fence.Key)
	if err != nil {
		return err
	}
	if export.Bot.Project != snapshot.Owner.Fence.Project || export.Bot.ConversationID != snapshot.Owner.Fence.ConversationID {
		return bridgeFailure(pa.ScopeDenied, "work_projection")
	}
	var previous *pa.WorkLink
	for i := range export.WorkLinks {
		if export.WorkLinks[i].ID == snapshot.Link.ID {
			previous = &export.WorkLinks[i]
			break
		}
	}
	if previous != nil && *previous == snapshot.Link {
		return nil
	}
	link := snapshot.Link
	command := bridgeCommand(snapshot.Owner.Fence.Key, "bridge-work-"+link.ID+"-"+string(link.State)+"-"+link.LandingCommit, link)
	// A later Board binding can change a link without changing its work state.
	// Include the complete canonical projection digest; identical snapshots
	// retain one receipt, while each new durable projection has its own key.
	command.IdempotencyKey += "-" + command.ContentHash
	m := Mutation{Command: command, Receipt: pa.Receipt{State: pa.EffectDelivered, ResourceID: link.ID}, WorkLinks: []pa.WorkLink{link}, Events: []pa.Event{{ID: "work", Fence: link.Fence, Kind: pa.WorkUpdated, Work: &link}}}
	if snapshot.Result != "" {
		found := false
		for _, message := range export.Messages {
			if message.ID == link.ResultMessageID {
				found = true
				break
			}
		}
		if !found {
			if err = pa.ValidateMessage(snapshot.Result); err != nil {
				return err
			}
			conversation := export.Conversation
			conversation.LastMessageSeq++
			message := pa.Message{ID: link.ResultMessageID, Fence: link.Fence, TurnID: link.TurnID, Seq: conversation.LastMessageSeq, Kind: pa.WorkMessage, Text: snapshot.Result, At: b.o.Clock()}
			m.Conversation = &conversation
			m.Messages = []pa.Message{message}
			m.Events = append(m.Events, pa.Event{ID: "result", Fence: link.Fence, Kind: pa.MessageCreated, Message: &message})
		}
	}
	_, err = b.commit(ctx, export.Bot, m)
	return err
}
