package projectassistanthost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	network "github.com/sainteye/clawdline/internal/app/botnetwork"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// toolScope is what every native tool of one opened turn shares: the turn
// and binding it serves, the engine and work bridge it calls, and the
// lifetime that seals the tools when the turn ends. bound checks a call
// still belongs to that turn; journal records it before its effect.
type toolScope struct {
	h          *Host
	openCtx    context.Context
	q          app.OpenRequest
	deadline   time.Time
	bounded    bool
	taskOwner  pa.BotKey
	c          orchestrator.AssistantCurrent
	turnID     string
	turn       pa.Turn
	r          *app.Engine
	b          *app.BrokerWorkBridge
	actor      pa.AssistantExecutionActor
	life       *workLifetime
	bound      func(context.Context) (context.Context, context.CancelFunc, error)
	journal    func(context.Context, string) error
	invocation atomic.Uint64
}

func (h *Host) tools(openCtx context.Context, q app.OpenRequest) (*workLifetime, NativeWork, error) {
	s, err := h.openToolScope(openCtx, q)
	if err != nil {
		return nil, NativeWork{}, err
	}
	s.watchLifetime()
	turn := s.turn
	// Only a same-machine exchange puts a card in this Bot's main chat. A
	// request from another machine's Bot keeps memory refused outright.
	tools := NativeWork{memoryCard: turn.Origin == pa.BotRequestOrigin || turn.Origin == pa.BotReplyOrigin}
	s.bindLeadTools(&tools)
	s.bindCharterTool(&tools)
	s.bindCharterConflictTool(&tools)
	s.bindSourceApproval(&tools)
	if err := h.bindServerTools(q, s.life, &tools, s.bound, s.journal); err != nil {
		s.life.invalidate()
		return nil, NativeWork{}, err
	}
	s.bindChatTools(&tools)
	s.bindTeamTools(&tools)
	s.bindRoutineTool(&tools)
	s.bindOutputTool(&tools)
	s.bindMemoryTools(&tools)
	s.bindBrowserTools(&tools)
	s.bindBackgroundTool(&tools)
	s.bindDispatchTools(&tools)
	s.bindWorkStatusTools(&tools)
	s.restrictBackgroundThread(&tools)
	return s.life, tools, nil
}

// openToolScope checks that the opened binding still owns the active turn
// and signs the work capability the turn may use.
func (h *Host) openToolScope(openCtx context.Context, q app.OpenRequest) (*toolScope, error) {
	deadline, bounded := openCtx.Deadline()
	taskOwner, err := h.store.BackgroundOwner(openCtx, q.Bot.Key)
	if err != nil {
		return nil, err
	}
	c, err := h.current(openCtx, q.Bot.Key)
	if err != nil {
		return nil, err
	}
	if c.Fence != q.Binding.Fence || c.ActiveTurnID == "" || c.BindingRevoked {
		return nil, failure(pa.StaleGeneration, "native_work_binding")
	}
	if err = c.Feature.CheckWrite(); err != nil {
		return nil, err
	}
	if err = pa.CheckSource(c.Source, true); err != nil {
		return nil, err
	}
	turnID := c.ActiveTurnID
	turn, err := h.store.Turn(openCtx, q.Bot.Key, turnID)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	r, a, b := h.engine, h.authority, h.bridge
	h.mu.Unlock()
	if r == nil || a == nil || b == nil {
		return nil, failure(pa.Disabled, "native_work")
	}
	// Signing occurs only from this actual provider Open. The authority may
	// refuse dispatch while chat remains usable under a missing work policy.
	var actor pa.AssistantExecutionActor
	if c.DispatchAllowed || c.ProjectDispatchAllowed || c.ProjectDispatchAskable {
		expires := deadline
		if !bounded {
			// An open prompt does not extend a signed Board capability.
			expires = h.o.Clock().Add(h.o.Limits.ControlWindow())
		}
		actor, err = a.Issue(openCtx, c.Fence, []pa.ActorOperation{pa.DispatchWork, pa.ReadWork}, expires)
		if err != nil {
			return nil, err
		}
	}
	return &toolScope{h: h, openCtx: openCtx, q: q, deadline: deadline, bounded: bounded, taskOwner: taskOwner, c: c, turnID: turnID, turn: turn, r: r, b: b, actor: actor}, nil
}

// watchLifetime creates the lifetime that seals the tools, and the bound
// and journal checks every tool call goes through.
func (s *toolScope) watchLifetime() {
	h, openCtx, q, deadline, bounded, turn, turnID, r := s.h, s.openCtx, s.q, s.deadline, s.bounded, s.turn, s.turnID, s.r
	life := &workLifetime{}
	s.life = life
	life.sealed, life.seal = context.WithCancel(context.Background())
	life.watching(context.AfterFunc(openCtx, life.invalidate))
	bound := func(ctx context.Context) (context.Context, context.CancelFunc, error) {
		if pa.RestartSourceTurn(turn) != "" {
			return nil, nil, failure(pa.ReadOnly, "restart_continuation_tools")
		}
		if pa.IsLeadCallTurn(turn) {
			return nil, nil, failure(pa.ScopeDenied, "lead_call_tools")
		}
		life.used.Store(true)
		if life.insideSend(ctx) {
			return nil, nil, failure(pa.ExecutionEffectUnknown, "native_tool_during_send")
		}
		if life.closed.Load() || openCtx.Err() != nil {
			return nil, nil, failure(pa.Revoked, "native_work_lifetime")
		}
		var cancel context.CancelFunc
		if bounded {
			ctx, cancel = context.WithDeadline(ctx, deadline)
		} else {
			ctx, cancel = context.WithCancel(ctx)
		}
		stop := context.AfterFunc(openCtx, cancel)
		finish := func() { stop(); cancel() }
		current, e := h.current(ctx, q.Bot.Key)
		if e == nil && (!current.Feature.Enabled || current.BindingRevoked) {
			e = failure(pa.Revoked, "native_work")
		}
		if e == nil {
			e = pa.CheckSource(current.Source, true)
		}
		if e == nil && (current.Fence != q.Binding.Fence || current.ActiveTurnID != turnID) {
			e = failure(pa.StaleGeneration, "native_work_turn")
		}
		if e == nil && (life.closed.Load() || openCtx.Err() != nil || ctx.Err() != nil) {
			e = failure(pa.Revoked, "native_work_lifetime")
		}
		if e != nil {
			finish()
			life.invalidate()
			return nil, nil, e
		}
		return ctx, finish, nil
	}
	// Each actual Host callback owns one invocation identity. IDs are independent
	// of model titles/content and stay fixed across the journal's commit attempt.
	invocation := &s.invocation
	journal := func(ctx context.Context, tool string) error {
		id := "host-" + q.Binding.Fence.BindingID + "-" + tool + "-" + strconv.FormatUint(invocation.Add(1), 10)
		if life.closed.Load() || life.insideSend(ctx) || ctx.Err() != nil {
			return failure(pa.Revoked, "native_work_journal_lifetime")
		}
		if tool == "teamAvailable" {
			return r.RecordToolDiscovery(ctx, q.Binding.Fence, turnID, id)
		}
		return r.RecordToolActivity(ctx, q.Binding.Fence, turnID, id)
	}
	s.bound = bound
	s.journal = journal
}

// bindLeadTools gives a direct turn of the lead Bot the tools that manage other Bots.
func (s *toolScope) bindLeadTools(tools *NativeWork) {
	h, q, turn, turnID, r, taskOwner, life, bound, journal := s.h, s.q, s.turn, s.turnID, s.r, s.taskOwner, s.life, s.bound, s.journal
	if h.o.ManageBots != nil && taskOwner.BotID == "" && pa.DirectUserTurn(turn) {
		authorize := func(ctx context.Context, key pa.BotKey, write bool) (app.Authority, error) {
			if key.MachineID != q.Bot.Key.MachineID {
				return app.Authority{}, failure(pa.ScopeDenied, "machine")
			}
			manager, err := r.ManagerContext(ctx, q.Binding.Fence, turnID)
			if err != nil {
				return app.Authority{}, err
			}
			if !manager.Caller.Lead {
				return app.Authority{}, failure(pa.ScopeDenied, "lead_role")
			}
			feature, source, err := h.background(ctx, key, write)
			principal := "lead-bot:" + q.Bot.Key.BotID
			return app.Authority{PrincipalID: principal, ReaderID: principal, AuthorizationRevision: turnID, Feature: feature, Source: source}, err
		}
		tools.leadBotAvailable = func(ctx context.Context) error {
			ctx, done, err := bound(ctx)
			if err != nil {
				return err
			}
			defer done()
			_, err = authorize(ctx, q.Bot.Key, false)
			return err
		}
		tools.manageBots = func(ctx context.Context, request app.LeadBotRequest) (json.RawMessage, error) {
			ctx, done, err := bound(ctx)
			if err != nil {
				return nil, err
			}
			stop := context.AfterFunc(life.sealed, done)
			defer func() { stop(); done() }()
			if _, err := authorize(ctx, q.Bot.Key, request.Method != "GET"); err != nil {
				return nil, err
			}
			if err := journal(ctx, "manageBots"); err != nil {
				return nil, err
			}
			service, err := h.Service()
			if err != nil {
				return nil, err
			}
			return h.o.ManageBots(ctx, service.WithAuthority(authorize), q.Bot.Key, request)
		}
	}
}

// bindSourceApproval asks the person to approve a source action.
func (s *toolScope) bindSourceApproval(tools *NativeWork) {
	q, turnID, r, life, bound, journal := s.q, s.turnID, s.r, s.life, s.bound, s.journal
	tools.approveSource = func(ctx context.Context, action pa.PermissionAction, options []pa.PermissionOption) (pa.PermissionOption, error) {
		ctx, done, err := bound(ctx)
		if err != nil {
			return pa.PermissionOption{}, err
		}
		stop := context.AfterFunc(life.sealed, done)
		defer func() { stop(); done() }()
		if err := journal(ctx, "source-approval"); err != nil {
			return pa.PermissionOption{}, err
		}
		return r.RequestSourcePermission(ctx, app.SourcePermissionRequest{Fence: q.Binding.Fence, TurnID: turnID, MemoryRevision: q.Binding.MemoryRevision, Action: action, Options: options})
	}
}

// bindChatTools binds the reaction, network and connector tools.
func (s *toolScope) bindChatTools(tools *NativeWork) {
	h, q, turn, bound, journal, invocation := s.h, s.q, s.turn, s.bound, s.journal, &s.invocation
	tools.react = func(ctx context.Context, emoji, to string) (pa.Receipt, error) {
		if (turn.Origin != pa.UserOrigin && turn.Origin != "") || turn.Room != nil || turn.Network != nil || to != "" {
			return pa.Receipt{}, failure(pa.ScopeDenied, "reaction_origin")
		}
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return pa.Receipt{}, e
		}
		defer cancel()
		if _, e = pa.ReactionEmoji(emoji); e != nil {
			return pa.Receipt{}, e
		}
		if e = journal(ctx, "react"); e != nil {
			return pa.Receipt{}, e
		}
		// Source policy and binding are checked again after the pre-effect journal.
		_, done, e := bound(ctx)
		if e != nil {
			return pa.Receipt{}, e
		}
		done()
		id := "native-react-" + turn.ID + "-" + strconv.FormatUint(invocation.Add(1), 10)
		hash := sha256.Sum256([]byte(emoji))
		result, e := h.store.React(ctx, app.ReactionWrite{ReactionRequest: app.ReactionRequest{Command: pa.Command{Key: q.Bot.Key, Operation: pa.ReactOperation, IdempotencyKey: id, ContentHash: hex.EncodeToString(hash[:])}, Fence: q.Binding.Fence, MessageID: turn.MessageID, Emoji: emoji, Present: true}, Actor: pa.ReactionActor{ID: "bot-" + q.Bot.Key.MachineID + "-" + q.Bot.Key.BotID, Kind: "bot", Bot: &q.Bot.Key, Name: q.Bot.Name}, NativeTurnID: turn.ID, NativeBot: &q.Bot, ReceiptID: h.o.ID(), EventID: h.o.ID()})
		if e != nil {
			return pa.Receipt{}, e
		}
		_, done, e = bound(ctx)
		if e != nil {
			return pa.Receipt{}, e
		}
		done()
		return result, nil
	}

	tools.network = func(ctx context.Context, request app.NetworkToolRequest) (network.Response, error) {
		if pa.IsPeerRequest(turn.Origin) || turn.Origin == pa.BotReplyOrigin {
			return network.Response{}, failure(pa.ScopeDenied, "network_peer_tools")
		}
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return network.Response{}, e
		}
		defer cancel()
		if h.o.Network == nil || h.o.NetworkTransport == nil {
			return network.Response{}, failure(pa.Unsupported, "network_unconfigured")
		}
		if e = journal(ctx, "networkBot"); e != nil {
			return network.Response{}, e
		}
		source := wire.BotEndpoint{AccountID: h.o.Network.AccountID(), MachineID: q.Bot.Key.MachineID, ProjectID: q.Bot.Project.ProjectID, BotID: q.Bot.Key.BotID, ConversationID: q.Binding.Fence.ConversationID, Generation: q.Binding.Fence.Generation}
		if request.Kind == "routes" {
			return h.o.Network.Routes(ctx, source, h.o.NetworkTransport)
		}
		transport, e := h.o.NetworkTransport(ctx, request.AuthorityID)
		if e != nil {
			return network.Response{}, e
		}
		result, e := h.o.Network.Call(ctx, network.Call{RequestID: request.RequestID, CorrelationID: request.CorrelationID, Kind: request.Kind, AuthorityID: request.AuthorityID, Source: source, Target: request.Target, Text: request.Text}, transport)
		if e != nil {
			return network.Response{}, e
		}
		if _, finish, e := bound(ctx); e != nil {
			return network.Response{}, e
		} else {
			finish()
		}
		return result, nil
	}
	tools.extension = func(ctx context.Context, ref pa.ExtensionRef) error {
		ctx, done, e := bound(ctx)
		if e != nil {
			return e
		}
		defer done()
		if h.o.Extensions == nil {
			return failure(pa.Unsupported, "bot_extensions")
		}
		selected := false
		for _, v := range q.Bot.Extensions.Connectors {
			if v == ref {
				selected = true
			}
		}
		if !selected {
			return failure(pa.ScopeDenied, "selected_connector")
		}
		if e = journal(ctx, "connector"); e != nil {
			return e
		}
		_, e = h.capturedExtensions(ctx, q)
		return e
	}
}

// bindTeamTools binds the tools that reach the Bot's team, other Bots, the person and Bot requests.
func (s *toolScope) bindTeamTools(tools *NativeWork) {
	h, q, turn, turnID, r, bound, journal := s.h, s.q, s.turn, s.turnID, s.r, s.bound, s.journal
	tools.teamAvailable = func(ctx context.Context) error {
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return e
		}
		defer cancel()
		if e = journal(ctx, "teamAvailable"); e != nil {
			return e
		}
		_, e = r.ManagerContext(ctx, q.Binding.Fence, turnID)
		return e
	}
	tools.team = func(ctx context.Context, action string, args json.RawMessage) (any, error) {
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return nil, e
		}
		defer cancel()
		if e = journal(ctx, "team"); e != nil {
			return nil, e
		}
		h.mu.Lock()
		team := h.team
		h.mu.Unlock()
		if team == nil {
			return nil, failure(pa.Unsupported, "manage_team")
		}
		return team.Call(ctx, q.Binding.Fence, turnID, action, args)
	}
	tools.listBots = func(ctx context.Context, request pa.BotDirectoryQuery) (pa.BotDirectoryPage, error) {
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return pa.BotDirectoryPage{}, e
		}
		defer cancel()
		if e = journal(ctx, "listBots"); e != nil {
			return pa.BotDirectoryPage{}, e
		}
		return r.ListBots(ctx, q.Binding.Fence, turnID, request)
	}
	tools.messageBot = func(ctx context.Context, request pa.BotMessageRequest) (pa.Exchange, error) {
		named := pa.BotRequestFrom(ctx)
		cards := h.botRequestCards()
		if _, e := botRequestDispatchScope(ctx, cards, nil, q.Bot.Key.BotID, turn, named); e != nil {
			return pa.Exchange{}, e
		}
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return pa.Exchange{}, e
		}
		defer cancel()
		if e = journal(ctx, "messageBot"); e != nil {
			return pa.Exchange{}, e
		}
		if named != "" {
			// The recipient joins the card's chain before it is messaged, so
			// the work it dispatches for the card is accepted (§7.1).
			if cards == nil {
				return pa.Exchange{}, failure(pa.Unsupported, "bot_request_store")
			}
			if _, e = cards.JoinBotRequestChain(ctx, named, q.Bot.Key.BotID, request.TargetBotID, h.o.Clock()); e != nil {
				return pa.Exchange{}, botRequestStoreError(e)
			}
		}
		return r.MessageBot(ctx, q.Binding.Fence, turnID, request)
	}
	// ask_person stays available in a peer request and in a background
	// worker: the question lands in the visible owner's main chat.
	tools.askPerson = func(ctx context.Context, request pa.AskPersonRequest) (pa.PermissionOffer, error) {
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return pa.PermissionOffer{}, e
		}
		defer cancel()
		if e = journal(ctx, "askPerson"); e != nil {
			return pa.PermissionOffer{}, e
		}
		return r.AskPerson(ctx, q.Binding.Fence, turnID, request)
	}
	tools.listQuestions = func(ctx context.Context) ([]pa.PendingQuestion, error) {
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return nil, e
		}
		defer cancel()
		return r.ListQuestions(ctx, q.Binding.Fence, turnID)
	}
	tools.withdrawQuestion = func(ctx context.Context, request pa.WithdrawQuestionRequest) (pa.PermissionOffer, error) {
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return pa.PermissionOffer{}, e
		}
		defer cancel()
		if e = journal(ctx, "withdrawQuestion"); e != nil {
			return pa.PermissionOffer{}, e
		}
		return r.WithdrawQuestion(ctx, q.Binding.Fence, turnID, request)
	}
	// The card tools stay available in a Bot-to-Bot request (§7.2): a
	// reviewer reached by message_bot can still record its verdict and hand
	// the baton on. Creation checks its own person authority.
	tools.botRequest = func(ctx context.Context, name string, arguments json.RawMessage) (any, error) {
		if h.o.Broker == nil || h.o.Broker.Store == nil {
			return nil, failure(pa.Unsupported, "bot_request_store")
		}
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return nil, e
		}
		defer cancel()
		if e = journal(ctx, "botRequest"); e != nil {
			return nil, e
		}
		return botRequestTool(ctx, h.o.Broker.Store, h.store, botRequestTurn{Bot: q.Bot, Conversation: q.Binding.Fence.ConversationID, Turn: turn, Directory: q.Directory}, q.Bot.Key.MachineID, h.o.ID(), h.o.Clock(), name, arguments)
	}
}

// bindRoutineTool binds the routine tool, which also reports a routine webhook's public status.
func (s *toolScope) bindRoutineTool(tools *NativeWork) {
	h, q, turn, turnID, r, bound, journal := s.h, s.q, s.turn, s.turnID, s.r, s.bound, s.journal
	tools.routines = func(ctx context.Context, request app.RoutineRequest) (app.RoutineResult, error) {
		if pa.IsPeerRequest(turn.Origin) && request.Action != "list" {
			return app.RoutineResult{}, failure(pa.ScopeDenied, "bot_request_tools")
		}
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return app.RoutineResult{}, e
		}
		defer cancel()
		if e = journal(ctx, "routines"); e != nil {
			return app.RoutineResult{}, e
		}
		if request.Action != "webhook_status" {
			return r.Routine(ctx, q.Binding.Fence, turnID, request)
		}
		// Reuse the owning execution's hook read and keep credentials inside
		// the Host while waiting for the source worker outside the actor lock.
		request.Action = "hook"
		hook, e := r.Routine(ctx, q.Binding.Fence, turnID, request)
		if e != nil {
			return app.RoutineResult{}, e
		}
		result := app.RoutineResult{ID: hook.ID, Webhook: &app.RoutineWebhookStatus{PublicStatus: "not_ready"}}
		if h.o.RoutineWebhookURL == nil {
			return result, nil
		}
		publicURL := h.o.RoutineWebhookURL(ctx, hook.ID, hook.Hook.Key, true)
		current, e := r.Routine(ctx, q.Binding.Fence, turnID, request)
		if e != nil {
			return app.RoutineResult{}, e
		}
		if publicURL != "" && current.Hook.Key == hook.Hook.Key &&
			h.o.RoutineWebhookURL(ctx, hook.ID, hook.Hook.Key, false) == publicURL {
			result.Webhook.PublicURL = publicURL
			result.Webhook.PublicStatus = "ready"
		}
		return result, nil
	}
}

// bindOutputTool binds the tool that publishes a file or an HTML artifact into the chat.
func (s *toolScope) bindOutputTool(tools *NativeWork) {
	h, q, turn, turnID, r, bound, journal := s.h, s.q, s.turn, s.turnID, s.r, s.bound, s.journal
	tools.output = func(ctx context.Context, id string, request app.OutputRequest) (pa.Output, error) {
		if pa.IsPeerRequest(turn.Origin) {
			return pa.Output{}, failure(pa.ScopeDenied, "bot_request_tools")
		}
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return pa.Output{}, e
		}
		defer cancel()
		if e = journal(ctx, "output"); e != nil {
			return pa.Output{}, e
		}
		if id == "" || len(id) > pa.MaxNameRunes || request.Path == "" || !request.HTML && request.Notify != nil {
			return pa.Output{}, failure(pa.Invalid, "output_request")
		}
		payload, _ := json.Marshal(struct {
			Request app.OutputRequest
			HTML    bool
		}{request, request.HTML})
		digest := sha256.Sum256(payload)
		requestHash := hex.EncodeToString(digest[:])
		identity := sha256.Sum256([]byte(turnID + "\x00" + id))
		outputID := hex.EncodeToString(identity[:])
		x, e := h.store.Export(ctx, q.Bot.Key)
		if e != nil {
			return pa.Output{}, e
		}
		for _, m := range x.Messages {
			if m.Output != nil && m.Output.ID == outputID {
				if m.Output.RequestHash != requestHash {
					return pa.Output{}, failure(pa.IdempotencyConflict, "output_request")
				}
				return *m.Output, nil
			}
		}
		name := request.Name
		if name == "" {
			name = filepath.Base(request.Path)
		}
		artifactID := ""
		if request.HTML {
			artifactID = request.Artifact
			name = strings.TrimSpace(request.Title)
			if artifactID != "" {
				found := false
				for _, o := range app.PublishedOutputs(x.DisplayMessages()) {
					if o.ArtifactID == artifactID {
						found = true
						if name == "" {
							name = o.Name
						}
						break
					}
				}
				if !found {
					return pa.Output{}, failure(pa.NotFound, "artifact")
				}
			} else {
				artifactID = outputID
			}
		}
		if request.HTML && !pa.ArtifactTitle(name) || !request.HTML && !pa.OutputName(name) {
			return pa.Output{}, failure(pa.Invalid, "output_name")
		}
		check := func() error {
			c, done, err := bound(ctx)
			if err != nil {
				return err
			}
			defer done()
			return c.Err()
		}
		out, e := h.o.Outputs.Snapshot(ctx, q.Bot.Key, q.Directory, request.Path, request.HTML, check)
		if e != nil {
			return out, e
		}
		published := false
		defer func() {
			if !published {
				// A lost SQL commit acknowledgement is not proof of rollback.
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), h.o.Limits.CleanupTimeout)
				defer cancel()
				saved, err := h.store.Export(cleanup, q.Bot.Key)
				if err != nil {
					return
				}
				for _, m := range saved.Messages {
					if m.Output != nil && m.Output.SnapshotID == out.SnapshotID {
						return
					}
				}
				_ = h.o.Outputs.Discard(q.Bot.Key, out)
			}
		}()
		out.ID, out.Name = outputID, name
		out.ArtifactID = artifactID
		out.Quiet = request.HTML && request.Artifact != "" && request.Notify != nil && !*request.Notify
		out.RequestHash = requestHash
		event := app.ProtocolEvent{ID: "output-" + id, Fence: q.Binding.Fence, TurnID: turnID, Message: name, Output: &out}
		if e = h.prepareOutputEvent(ctx, turn.Room, &event, check); e != nil {
			return pa.Output{}, e
		}
		e = r.Publish(ctx, event)
		published = e == nil
		return out, e
	}
}

// bindMemoryTools binds the artifact listing and the memory tools.
func (s *toolScope) bindMemoryTools(tools *NativeWork) {
	h, q, turn, turnID, r, bound, journal := s.h, s.q, s.turn, s.turnID, s.r, s.bound, s.journal
	tools.artifacts = func(ctx context.Context) ([]app.ArtifactInfo, error) {
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return nil, e
		}
		defer cancel()
		if e = journal(ctx, "artifacts"); e != nil {
			return nil, e
		}
		x, e := h.store.Export(ctx, q.Bot.Key)
		if e != nil {
			return nil, e
		}
		out := []app.ArtifactInfo{}
		for _, o := range app.PublishedOutputs(x.DisplayMessages()) {
			if o.ArtifactID != "" {
				path, err := h.o.Outputs.Path(q.Bot.Key, o)
				if err != nil {
					return nil, err
				}
				out = append(out, app.ArtifactInfo{Output: o, Path: path})
			}
		}
		return out, nil
	}
	tools.memoryIndex = func(ctx context.Context) (app.MemoryIndex, error) {
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return app.MemoryIndex{}, e
		}
		defer cancel()
		if e = journal(ctx, "memory"); e != nil {
			return app.MemoryIndex{}, e
		}
		return r.MemoryIndex(ctx, q.Bot.Key)
	}
	tools.memory = func(ctx context.Context) ([]pa.MemoryEntry, error) {
		index, e := tools.memoryIndex(ctx)
		return index.Entries, e
	}
	tools.memoryDetail = func(ctx context.Context, id string) (pa.MemoryDetail, error) {
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return pa.MemoryDetail{}, e
		}
		defer cancel()
		if e = journal(ctx, "memory-detail"); e != nil {
			return pa.MemoryDetail{}, e
		}
		return r.MemoryDetail(ctx, q.Bot.Key, id)
	}
	tools.edit = func(ctx context.Context, id string, edit app.MemoryEdit) error {
		if e := peerMemoryEdit(turn.Origin, tools.memoryCard, edit); e != nil {
			return e
		}
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return e
		}
		defer cancel()
		if e = journal(ctx, "edit"); e != nil {
			return e
		}
		return writeMemory(ctx, r, tools.memoryCard, q, turnID, id, edit)
	}
}

// bindBrowserTools binds the browser tools, scoped to the binding that owns the tabs.
func (s *toolScope) bindBrowserTools(tools *NativeWork) {
	h, q, turn, life, bound, journal := s.h, s.q, s.turn, s.life, s.bound, s.journal
	owner := BrowserOwner(q.Binding.Fence, q.Binding.ForgottenThrough)
	browser := h.o.Browser
	browserBound := func(ctx context.Context) (context.Context, context.CancelFunc, error) {
		if browser == nil || browser.Status == nil || browser.Command == nil {
			return nil, nil, failure(pa.Disabled, "browser_tools")
		}
		if e := browserScope(q.Binding, turn); e != nil {
			return nil, nil, e
		}
		ctx, done, e := bound(ctx)
		if e != nil {
			return nil, nil, e
		}
		stop := context.AfterFunc(life.sealed, done)
		return ctx, func() { stop(); done() }, nil
	}
	tools.browserStatus = func(ctx context.Context) (app.BrowserStatus, error) {
		ctx, cancel, e := browserBound(ctx)
		if e != nil {
			return app.BrowserStatus{}, e
		}
		defer cancel()
		if e = journal(ctx, "browserStatus"); e != nil {
			return app.BrowserStatus{}, e
		}
		return browser.Status(ctx, owner)
	}
	tools.browserCommand = func(ctx context.Context, command app.BrowserCommand) (app.BrowserResult, error) {
		ctx, cancel, e := browserBound(ctx)
		if e != nil {
			return app.BrowserResult{}, e
		}
		defer cancel()
		if e = journal(ctx, "browserCommand"); e != nil {
			return app.BrowserResult{}, e
		}
		// A Bot hands a page to the person by keeping its tab: release would
		// drop the Bot as owner, and the person could no longer reach the tab
		// from the Bot's chat. Close still ends a tab the Bot is done with.
		if command.Op == "release" {
			return app.BrowserResult{Value: botTabKept}, nil
		}
		return browser.Command(ctx, owner, command)
	}
}

// bindBackgroundTool lets a turn that may delegate start a background thread.
func (s *toolScope) bindBackgroundTool(tools *NativeWork) {
	h, q, turn, turnID, taskOwner, bound, journal := s.h, s.q, s.turn, s.turnID, s.taskOwner, s.bound, s.journal
	if taskOwner.BotID == "" && pa.CanDelegateBackground(turn) {
		tools.background = func(ctx context.Context, id, title, brief string) (pa.BackgroundTask, error) {
			ctx, cancel, err := bound(ctx)
			if err != nil {
				return pa.BackgroundTask{}, err
			}
			defer cancel()
			if err = pa.ValidateBackgroundBrief(title, brief); err != nil {
				return pa.BackgroundTask{}, err
			}
			if err = journal(ctx, "background"); err != nil {
				return pa.BackgroundTask{}, err
			}
			payload, _ := json.Marshal([3]string{turnID, title, brief})
			hash := sha256.Sum256(payload)
			command := pa.Command{
				Key: q.Bot.Key, Operation: pa.DispatchOperation, IdempotencyKey: "background-" + turnID + "-" + id,
				ContentHash: hex.EncodeToString(hash[:]),
			}
			return h.store.AdmitBackgroundTask(ctx, command, pa.BackgroundTask{
				ID: h.o.ID(), Fence: q.Binding.Fence, OriginTurnID: turnID,
				Title: title, Brief: brief, CreatedAt: h.o.Clock(),
			})
		}
	}
}

// bindDispatchTools binds the chat send tool and the tool that dispatches Board work.
func (s *toolScope) bindDispatchTools(tools *NativeWork) {
	h, q, turn, turnID, r, c, actor, taskOwner, bound, journal := s.h, s.q, s.turn, s.turnID, s.r, s.c, s.actor, s.taskOwner, s.bound, s.journal
	if h.o.DispatchPolicy != nil {
		tools.dispatchKinds = func(ctx context.Context) []string {
			p, err := h.o.DispatchPolicy(ctx, q.Bot.Key)
			if err != nil {
				return nil
			}
			return pa.DispatchKindsFor(p.Shared, q.Bot.Project.ProjectID)
		}
	}
	tools.send = func(ctx context.Context, id, text string) error {
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return e
		}
		defer cancel()
		if e = journal(ctx, "send"); e != nil {
			return e
		}
		return r.Publish(ctx, app.ProtocolEvent{ID: id, Fence: q.Binding.Fence, TurnID: turnID, Message: text})
	}
	tools.notify = func(ctx context.Context, id, text string) error {
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return e
		}
		defer cancel()
		if e = journal(ctx, "notifyPerson"); e != nil {
			return e
		}
		return r.Publish(ctx, app.ProtocolEvent{ID: id, Fence: q.Binding.Fence, TurnID: turnID, Message: text, ForPerson: true})
	}
	tools.dispatch = func(ctx context.Context, id, title, brief string, claims []string, project, persona, boardItemID string, continueOnCompletion bool) (pa.WorkLink, error) {
		botRequestID := pa.BotRequestFrom(ctx)
		if e := dispatchScope(ctx, h.botRequestCards(), q.Bot.Key.BotID, turn, botRequestID); e != nil {
			return pa.WorkLink{}, e
		}
		// A request from another machine has no dispatch authority.
		if turn.Origin == pa.NetworkRequestOrigin {
			return pa.WorkLink{}, failure(pa.ScopeDenied, "peer_dispatch")
		}
		if continueOnCompletion && !directUserTurn(turn) {
			return pa.WorkLink{}, failure(pa.ScopeDenied, "work_completion_origin")
		}
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return pa.WorkLink{}, e
		}
		defer cancel()
		if actor.CapabilityID == "" {
			return pa.WorkLink{}, failure(pa.ScopeDenied, "dispatch_policy")
		}
		if boardItemID != "" {
			if !directUserTurn(turn) || !orchestrator.IsTaskID(boardItemID) {
				return pa.WorkLink{}, failure(pa.ScopeDenied, "board_item_id")
			}
			exported, err := h.store.Export(ctx, q.Bot.Key)
			if err != nil {
				return pa.WorkLink{}, err
			}
			named := slices.ContainsFunc(exported.Messages, func(message pa.Message) bool {
				return message.ID == turn.MessageID && message.Kind == pa.UserMessage &&
					slices.Contains(strings.FieldsFunc(message.Text, func(r rune) bool {
						return !(r == '-' || r >= 'a' && r <= 'f' || r >= '0' && r <= '9')
					}), boardItemID)
			})
			if !named {
				return pa.WorkLink{}, failure(pa.ScopeDenied, "board_item_id")
			}
		}
		if project != "" && (turn.Origin == pa.BotRequestOrigin || turn.Origin == pa.BotReplyOrigin || pa.WorkCompletionTask(turn) != "") {
			// Use the same target and receipt hash as an omitted project
			// when the registry resolves the name to this Bot's own Project.
			if folders, ok := h.o.Projects.(app.TeamProjectResolver); ok {
				if own, err := folders.ResolveTeamFolder(ctx, q.Bot.Key.MachineID, project); err == nil && own.Scope == q.Binding.Fence.Project {
					project = ""
				}
			}
		}
		var target pa.ProjectScope
		if project != "" {
			if !c.ProjectDispatchAllowed && !c.ProjectDispatchAskable {
				if !directUserTurn(turn) {
					// The person must ask this Bot directly; no card is offered
					// on a turn another Bot, a room or a routine started.
					return pa.WorkLink{}, failure(pa.ScopeDenied, "dispatch_project_direct")
				}
				return pa.WorkLink{}, failure(pa.ScopeDenied, "dispatch_project")
			}
			folders := h.o.Projects.(app.TeamProjectResolver)
			resolved, err := folders.ResolveTeamFolder(ctx, q.Bot.Key.MachineID, project)
			if err != nil {
				return pa.WorkLink{}, err
			}
			target = resolved.Scope
			if target.Kind != pa.RepositoryProject {
				return pa.WorkLink{}, failure(pa.ScopeDenied, "dispatch_project")
			}
			if err = pa.ValidateTarget(pa.ProjectSelection{MachineID: q.Bot.Key.MachineID, ProjectID: target.ProjectID}, resolved); err != nil {
				return pa.WorkLink{}, err
			}
			if !c.ProjectDispatchAllowed {
				ask := dispatchAsk{Fence: q.Binding.Fence, TurnID: turnID, MemoryRevision: q.Binding.MemoryRevision, BotName: q.Bot.Name, Key: id, Title: title, Claims: claims, Target: resolved}
				if err = h.askDispatch(ctx, r, ask); err != nil {
					return pa.WorkLink{}, err
				}
			}
		}
		choice := pa.DispatchChoiceFrom(ctx)
		if e = journal(ctx, "dispatch"); e != nil {
			return pa.WorkLink{}, e
		}
		payload, _ := json.Marshal(struct {
			Fence                pa.Fence
			Turn, Brief          string
			Claims               []string
			Title                string            `json:",omitempty"`
			TargetProject        pa.ProjectScope   `json:"target_project,omitzero"`
			Persona              string            `json:",omitempty"`
			BoardItemID          string            `json:"board_item_id,omitempty"`
			ContinueOnCompletion bool              `json:"continue_on_completion,omitempty"`
			Choice               pa.DispatchChoice `json:"choice,omitzero"`
			BotRequestID         string            `json:"bot_request_id,omitempty"`
		}{q.Binding.Fence, turnID, brief, claims, title, target, persona, boardItemID, continueOnCompletion, choice, botRequestID})
		hash := sha256.Sum256(payload)
		command := pa.Command{Key: q.Bot.Key, Operation: pa.DispatchOperation, IdempotencyKey: id, ContentHash: hex.EncodeToString(hash[:])}
		request := pa.WorkRequest{Actor: actor, Command: command, TurnID: turnID, Title: title, Brief: brief, Claims: slices.Clone(claims), TargetProject: target, Persona: persona, BoardItemID: boardItemID, BotRequestID: botRequestID, ContinueOnCompletion: continueOnCompletion, Choice: choice}
		if taskOwner.BotID != "" && turn.Origin == pa.BotRequestOrigin {
			return h.dispatchPeerWork(ctx, r, c.WorkOwner, turn, request)
		}
		return r.DispatchWork(ctx, request)
	}
}

// bindWorkStatusTools binds the tools that observe and message dispatched work.
func (s *toolScope) bindWorkStatusTools(tools *NativeWork) {
	h, q, turn, b, actor, bound, journal := s.h, s.q, s.turn, s.b, s.actor, s.bound, s.journal
	// One turn gets one observation of a running task. Further observations
	// belong to the completion callback or a later status question.
	var workStatusMu sync.Mutex
	pendingWorkSeen := make(map[string]struct{})
	tools.status = func(ctx context.Context, taskID string) (pa.WorkLink, error) {
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return pa.WorkLink{}, e
		}
		defer cancel()
		if actor.CapabilityID == "" {
			return pa.WorkLink{}, failure(pa.ScopeDenied, "dispatch_policy")
		}
		workStatusMu.Lock()
		defer workStatusMu.Unlock()
		if _, seen := pendingWorkSeen[taskID]; seen {
			return pa.WorkLink{}, failure(pa.ReadOnly, "work_completion_pending")
		}
		if e = journal(ctx, "status"); e != nil {
			return pa.WorkLink{}, e
		}
		link, e := b.Status(ctx, actor, taskID)
		if e == nil && link.ResultMessageID == "" {
			pendingWorkSeen[taskID] = struct{}{}
		}
		return link, peerWorkRefusal(turn, e)
	}
	tools.messageWork = messageWorkTool(turn, actor, b, bound, journal, func(ctx context.Context) string {
		return h.peerRequester(ctx, q.Bot.Key, turn)
	})
	tools.workReceipt = func(ctx context.Context, idempotencyKey string) (app.WorkReceipt, error) {
		ctx, cancel, e := bound(ctx)
		if e != nil {
			return app.WorkReceipt{}, e
		}
		defer cancel()
		if actor.CapabilityID == "" {
			return app.WorkReceipt{}, failure(pa.ScopeDenied, "dispatch_policy")
		}
		if e = journal(ctx, "status"); e != nil {
			return app.WorkReceipt{}, e
		}
		return b.Receipt(ctx, actor, idempotencyKey)
	}
}

// restrictBackgroundThread removes the tools a background thread may not use.
func (s *toolScope) restrictBackgroundThread(tools *NativeWork) {
	turn, taskOwner := s.turn, s.taskOwner
	if taskOwner.BotID != "" {
		tools.routines = nil
		if turn.Origin != pa.BotRequestOrigin {
			tools.dispatch = nil
		}
		if !pa.IsPeerRequest(turn.Origin) {
			// A thread answering another Bot reaches its owner's work through
			// the broker's resolved owner; other background threads do not.
			tools.messageWork = mainChatOnlyMessageWork
		}
		tools.teamAvailable = nil
		tools.team = nil
		tools.listBots = nil
		tools.messageBot = nil
	}
}
