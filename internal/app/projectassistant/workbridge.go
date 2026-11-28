package projectassistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
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

// Keep broker diagnostics at their owner boundary. Assistant transports carry
// the stable assistant code and retry metadata, rather than raw English copy.
func bridgeBrokerFailure(err error) error {
	var refusal orchestrator.Refusal
	if errors.As(err, &refusal) && refusal.Status == http.StatusTooManyRequests && refusal.Code == "over_capacity" {
		retry, _ := refusal.Extra["retry_after"].(int)
		return &pa.Failure{Code: pa.CapacityFull, Field: "work_dispatch", RetryAfterSeconds: int64(retry)}
	}
	return err
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
		return pa.WorkLink{}, bridgeBrokerFailure(err)
	}
	if q.Choice.Mode == pa.DispatchModeRoot {
		// The Root Assignment receipt keyed on this command is the replay
		// guard: a retry returns the same Root and opens nothing.
		a, _, err := b.o.Broker.DispatchAssistantRoot(ctx, q)
		if err != nil {
			return pa.WorkLink{}, bridgeBrokerFailure(err)
		}
		b.syncMu.Lock()
		defer b.syncMu.Unlock()
		// Re-read the durable Root and existing WorkLink under syncMu so a
		// concurrent close or replay never rewinds a terminal WorkLink.
		latest, err := b.o.Broker.RootAssignmentByID(ctx, a.ID)
		if err != nil {
			return pa.WorkLink{}, err
		}
		if latest.BotOwner == nil {
			return pa.WorkLink{}, bridgeFailure(pa.ScopeDenied, "work_snapshot")
		}
		export, err := b.o.Repository.Export(ctx, latest.BotOwner.Fence.Key)
		if err != nil {
			return pa.WorkLink{}, err
		}
		for i := range export.WorkLinks {
			prev := export.WorkLinks[i]
			if prev.ID == latest.ID && isTerminalWorkState(prev.State) {
				return prev, nil
			}
		}
		link := orchestrator.AssistantRootLink(latest)
		return link, b.persistExportLink(ctx, export, latest.BotOwner.Fence, link, "", nil)
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
		return pa.WorkLink{}, bridgeBrokerFailure(err)
	}
	if accepted.Record.AssistantOwner == nil || accepted.Record.Worktree == nil || accepted.Record.Worktree.Path == "" {
		return pa.WorkLink{}, &pa.Failure{Code: pa.OutcomeUnknown, Field: "work_acceptance", TaskID: id}
	}
	// Status rereads the durable row rather than turning a launch response
	// into completion evidence. Sync must commit before the link is returned.
	return b.Status(ctx, q.Actor, id)
}

// SyncRootDelivery marks a Bot-opened Root delivered in the Bot's chat, with
// the Root's report as its result message, so the ordinary completion turn
// can quote it. It is the host's AssistantRootDelivered step, never a route.
func (b *BrokerWorkBridge) SyncRootDelivery(ctx context.Context, d orchestrator.AssistantRootDelivery) (pa.WorkLink, error) {
	a, err := b.o.Broker.RootAssignmentByID(ctx, d.AssignmentID)
	if err != nil {
		return pa.WorkLink{}, err
	}
	if a.BotOwner == nil || a.BotOwner.Fence != d.Owner.Fence || a.BotOwner.Command != d.Owner.Command {
		return pa.WorkLink{}, bridgeFailure(pa.ScopeDenied, "work_snapshot")
	}
	link := orchestrator.AssistantRootLink(a)
	link.State, link.ResultMessageID = pa.WorkCompleted, "work-result-"+a.ID
	link.FinishedAt = d.At.UTC()
	result := strings.TrimSpace(d.Summary)
	if result == "" {
		result = string(pa.WorkCompleted)
	}
	b.syncMu.Lock()
	defer b.syncMu.Unlock()
	return link, b.persistLink(ctx, a.BotOwner.Fence, link, result, nil)
}

func isTerminalWorkState(state pa.WorkState) bool {
	return state == pa.WorkCompleted || state == pa.WorkLanded || state == pa.WorkFailed || state == pa.WorkUnknown
}

// SyncRootClosure settles a Bot-opened Root's WorkLink when the Root closes
// without a delivery report (or failed to open), without posting a delivery
// result message or waking a review turn. Already delivered links are kept.
func (b *BrokerWorkBridge) SyncRootClosure(ctx context.Context, c orchestrator.AssistantRootClosure) error {
	a, err := b.o.Broker.RootAssignmentByID(ctx, c.AssignmentID)
	if err != nil {
		return err
	}
	if a.BotOwner == nil || a.BotOwner.Fence != c.Owner.Fence || a.BotOwner.Command != c.Owner.Command {
		return bridgeFailure(pa.ScopeDenied, "work_snapshot")
	}
	if a.BotOwner.DeliveredAt != 0 {
		return nil
	}
	if a.Closure == nil && a.State != orchestrator.AssignmentFailed {
		return nil
	}
	return b.syncRootTerminal(ctx, a)
}

// ReconcileRoot repairs a Bot-opened Root WorkLink stuck in an active state
// after the Root closed, failed, or already recorded delivery. It never wakes
// a review turn and preserves existing delivered/landed projections.
func (b *BrokerWorkBridge) ReconcileRoot(ctx context.Context, assignmentID string) error {
	a, err := b.o.Broker.RootAssignmentByID(ctx, assignmentID)
	if err != nil {
		return err
	}
	if a.BotOwner == nil {
		return nil
	}
	if a.BotOwner.DeliveredAt == 0 && a.Closure == nil && a.State != orchestrator.AssignmentFailed {
		return nil
	}
	return b.syncRootTerminal(ctx, a)
}

func (b *BrokerWorkBridge) syncRootTerminal(ctx context.Context, a orchestrator.RootAssignment) error {
	b.syncMu.Lock()
	defer b.syncMu.Unlock()
	export, err := b.o.Repository.Export(ctx, a.BotOwner.Fence.Key)
	if err != nil {
		return err
	}
	var previous *pa.WorkLink
	for i := range export.WorkLinks {
		if export.WorkLinks[i].ID == a.ID {
			previous = &export.WorkLinks[i]
			break
		}
	}
	if previous != nil && (previous.State == pa.WorkCompleted || previous.State == pa.WorkLanded) {
		return nil
	}
	link := orchestrator.AssistantRootLink(a)
	result := ""
	if a.BotOwner.DeliveredAt > 0 {
		if previous != nil {
			link.Landing = previous.Landing
			link.LandingNote = previous.LandingNote
		}
		hasResultMessage := false
		for _, m := range export.Messages {
			if m.ID == link.ResultMessageID {
				hasResultMessage = true
				break
			}
		}
		if !hasResultMessage {
			if summary, at := b.rootDeliveryReceipt(ctx, a); summary != "" {
				result = summary
				if !at.IsZero() {
					link.FinishedAt = at.UTC()
				}
			} else {
				link.ResultMessageID = ""
			}
		}
	}
	return b.persistExportLink(ctx, export, a.BotOwner.Fence, link, result, nil)
}

func (b *BrokerWorkBridge) rootDeliveryReceipt(ctx context.Context, a orchestrator.RootAssignment) (string, time.Time) {
	if b.o.Broker == nil || b.o.Broker.Store == nil || a.Executor == nil || a.Executor.TerminalID == "" {
		return "", time.Time{}
	}
	events, err := b.o.Broker.Store.LatestEvents(ctx, "session.delivered", []string{a.Executor.TerminalID})
	if err != nil {
		return "", time.Time{}
	}
	event, ok := events[a.Executor.TerminalID]
	if !ok {
		return "", time.Time{}
	}
	var receipt struct {
		Conversation string `json:"conversation"`
		Summary      string `json:"summary"`
		At           int64  `json:"at"`
	}
	if json.Unmarshal(event.Payload, &receipt) != nil {
		return "", time.Time{}
	}
	if receipt.At < a.Executor.OpenedAt {
		return "", time.Time{}
	}
	if a.Closure != nil && a.Closure.ConversationID != "" && receipt.Conversation != "" && receipt.Conversation != a.Closure.ConversationID {
		return "", time.Time{}
	}
	summary := strings.TrimSpace(receipt.Summary)
	if summary == "" {
		return "", time.Time{}
	}
	return summary, time.Unix(receipt.At, 0).UTC()
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
	return b.persistLink(ctx, snapshot.Owner.Fence, snapshot.Link, snapshot.Result, &r)
}

// persistLink writes one broker-derived link, and its result message once,
// into the owning Bot's chat. Callers hold syncMu.
func (b *BrokerWorkBridge) persistLink(ctx context.Context, owner pa.Fence, link pa.WorkLink, result string, record *orchestrator.Record) error {
	export, err := b.o.Repository.Export(ctx, owner.Key)
	if err != nil {
		return err
	}
	return b.persistExportLink(ctx, export, owner, link, result, record)
}

func (b *BrokerWorkBridge) persistExportLink(ctx context.Context, export Export, owner pa.Fence, link pa.WorkLink, result string, record *orchestrator.Record) error {
	var err error
	if export.Bot.Project != owner.Project || export.Bot.ConversationID != owner.ConversationID {
		return bridgeFailure(pa.ScopeDenied, "work_projection")
	}
	var previous *pa.WorkLink
	for i := range export.WorkLinks {
		if export.WorkLinks[i].ID == link.ID {
			previous = &export.WorkLinks[i]
			break
		}
	}
	if previous != nil && *previous == link {
		return nil
	}
	command := bridgeCommand(owner.Key, "bridge-work-"+link.ID+"-"+string(link.State)+"-"+link.LandingCommit, link)
	// A later Board binding can change a link without changing its work state.
	// Include the complete canonical projection digest; identical snapshots
	// retain one receipt, while each new durable projection has its own key.
	command.IdempotencyKey += "-" + command.ContentHash
	m := Mutation{Command: command, Receipt: pa.Receipt{State: pa.EffectDelivered, ResourceID: link.ID}, WorkLinks: []pa.WorkLink{link}}
	conversation := export.Conversation
	var anchorSeq int64
	anchorMessageID := ""
	for _, turn := range export.Turns {
		if turn.ID == link.TurnID {
			anchorMessageID = turn.MessageID
			break
		}
	}
	if anchorMessageID != "" {
		for _, message := range export.Messages {
			if message.ID == anchorMessageID {
				anchorSeq = message.Seq
				break
			}
		}
	} else if record != nil && link.TurnID == PeerDispatchNoticePrefix+link.TaskID {
		turn, message := peerDispatchNotice(export, owner, link.TurnID, link.TaskID, link.Title, record.AssistantOwner.PeerDispatch, record.CreatedAt.UTC(), "", "")
		conversation.LastMessageSeq++
		anchorSeq = conversation.LastMessageSeq
		message.Seq = anchorSeq
		m.Turn = &turn
		m.Messages = append(m.Messages, message)
		m.Events = append(m.Events,
			pa.Event{ID: "peer-dispatch", Fence: owner, Kind: pa.MessageCreated, Message: &message},
			pa.Event{ID: "peer-turn", Fence: owner, Kind: pa.TurnUpdated, Turn: &turn},
		)
	}
	m.Events = append(m.Events, pa.Event{ID: "work", Fence: link.Fence, Kind: pa.WorkUpdated, Work: &link, WorkAnchorMessageSeq: anchorSeq})
	if result != "" {
		found := false
		for _, message := range export.Messages {
			if message.ID == link.ResultMessageID {
				found = true
				break
			}
		}
		if !found {
			if err = pa.ValidateMessage(result); err != nil {
				return err
			}
			conversation.LastMessageSeq++
			message := pa.Message{ID: link.ResultMessageID, Fence: link.Fence, TurnID: link.TurnID, Seq: conversation.LastMessageSeq, Kind: pa.WorkMessage, Text: result, At: b.o.Clock()}
			m.Messages = append(m.Messages, message)
			m.Events = append(m.Events, pa.Event{ID: "result", Fence: link.Fence, Kind: pa.MessageCreated, Message: &message})
		}
	}
	if len(m.Messages) > 0 {
		m.Conversation = &conversation
	}
	_, err = b.commit(ctx, export.Bot, m)
	return err
}
