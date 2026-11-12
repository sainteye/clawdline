package projectassistant

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Authority comes from the current local/device authentication boundary, never
// from a request body. ReaderID names a principal shared across its devices.
type Authority struct {
	PrincipalID           string
	ReaderID              string
	AuthorizationRevision string
	Feature               pa.FeatureState
	Source                pa.SourceStatus
}

// ReaderRepository is a projection port: a neutral roster must not consult
// another principal's read cursor. PA-03 implements it on the same Store.
type ReaderRepository interface {
	ListForReader(context.Context, pa.ListBots, string) (pa.BotPage, error)
}

type ServiceOptions struct {
	Repository Repository
	Memory     Memory
	Events     EventFeed
	Runtime    Runtime
	Projects   ProjectResolver
	Authorize  func(context.Context, pa.BotKey, bool) (Authority, error)
	Clock      func() time.Time
	ID         func() string
}

// Service has no startup side effects. Configuration, history, memory and
// projection operations never call Runtime or initialize a provider process.
type Service struct {
	opts   ServiceOptions
	reader ReaderRepository
}

func NewService(opts ServiceOptions) (*Service, error) {
	r, ok := opts.Repository.(ReaderRepository)
	if !ok || opts.Memory == nil || opts.Events == nil || opts.Runtime == nil || opts.Projects == nil || opts.Authorize == nil || opts.Clock == nil || opts.ID == nil {
		return nil, errors.New("project assistants require repository, reader projection, memory, events, runtime, projects, authority, clock and IDs")
	}
	return &Service{opts: opts, reader: r}, nil
}

func serviceFailure(code pa.ErrorCode, field string) error {
	return &pa.Failure{Code: code, Field: field}
}

func (s *Service) authorize(ctx context.Context, key pa.BotKey, write bool) (Authority, error) {
	if err := ctx.Err(); err != nil {
		return Authority{}, err
	}
	a, err := s.opts.Authorize(ctx, key, write)
	if err != nil {
		return Authority{}, err
	}
	if a.PrincipalID == "" || a.ReaderID == "" || a.AuthorizationRevision == "" || key.MachineID == "" || a.Source.MachineID != key.MachineID {
		return Authority{}, serviceFailure(pa.ScopeDenied, "authority")
	}
	if err = pa.CheckSource(a.Source, write); err != nil {
		return Authority{}, err
	}
	if a.Feature.Access() == pa.Hidden {
		return Authority{}, serviceFailure(pa.Disabled, "feature")
	}
	if write {
		err = a.Feature.CheckWrite()
	}
	return a, err
}

// canonicalCommand deliberately discards the supplied content hash. It binds
// intent to the authenticated principal, not to a particular device or grant
// revision (renewing a grant must not cause an accepted send to execute again).
func canonicalCommand(c pa.Command, operation pa.Operation, principal string, payload any) (pa.Command, error) {
	if c.Operation != operation {
		return c, serviceFailure(pa.Invalid, "operation")
	}
	b, err := json.Marshal(struct {
		Principal string       `json:"principal"`
		Key       pa.BotKey    `json:"key"`
		Operation pa.Operation `json:"operation"`
		Payload   any          `json:"payload"`
	}{principal, c.Key, operation, payload})
	if err != nil {
		return c, err
	}
	hash := sha256.Sum256(b)
	c.ContentHash = hex.EncodeToString(hash[:])
	_, err = pa.DecideReplay(c, nil)
	return c, err
}

func (s *Service) replay(ctx context.Context, c pa.Command) (*pa.Receipt, error) {
	r, err := s.opts.Repository.Receipt(ctx, c)
	if err != nil {
		return nil, err
	}
	if _, err = pa.DecideReplay(c, r); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Service) Create(ctx context.Context, c pa.Command, settings pa.CreateBot) (pa.Bot, pa.Receipt, error) {
	a, err := s.authorize(ctx, c.Key, true)
	if err != nil {
		return pa.Bot{}, pa.Receipt{}, err
	}
	if settings.Target.MachineID != c.Key.MachineID {
		return pa.Bot{}, pa.Receipt{}, serviceFailure(pa.ScopeDenied, "target_machine")
	}
	c, err = canonicalCommand(c, pa.CreateOperation, a.PrincipalID, settings)
	if err != nil {
		return pa.Bot{}, pa.Receipt{}, err
	}
	if prior, e := s.replay(ctx, c); e != nil || prior != nil {
		if e != nil {
			return pa.Bot{}, pa.Receipt{}, e
		}
		bot, e := s.opts.Repository.Bot(ctx, c.Key)
		return bot, *prior, e
	}
	resolved, err := s.opts.Projects.Resolve(ctx, settings.Target)
	if err != nil {
		return pa.Bot{}, pa.Receipt{}, err
	}
	if err = pa.ValidateCreate(settings, resolved); err != nil {
		return pa.Bot{}, pa.Receipt{}, err
	}
	now := s.opts.Clock().UTC()
	bot := pa.Bot{Key: c.Key, Project: resolved.Scope, ConversationID: s.opts.ID(), Name: settings.Name,
		Appearance: settings.Appearance, Agent: settings.Agent, PermissionPolicy: settings.PermissionPolicy,
		Instructions: pa.InstructionSnapshot{Revision: 1, Text: settings.Instructions, RoleSnapshotRef: settings.RoleSnapshotRef},
		Version:      1, CreatedAt: now, UpdatedAt: now}
	conversation := pa.Conversation{Key: c.Key, ID: bot.ConversationID, MemoryRevision: 1, Version: 1}
	summary := pa.BotSummary{Bot: bot, ActivityAt: now, Source: a.Source, Version: 1}
	fence := pa.Fence{Key: bot.Key, Project: bot.Project, ConversationID: bot.ConversationID}
	r, err := s.opts.Repository.Commit(ctx, Mutation{Command: c, ExpectedVersion: 0, Bot: &bot, Conversation: &conversation,
		Receipt: pa.Receipt{ID: s.opts.ID(), Command: c, State: pa.EffectAccepted, ResourceID: bot.Key.BotID},
		Events:  []pa.Event{{Version: pa.ContractVersion, ID: s.opts.ID(), Fence: fence, Kind: pa.BotCreated, At: now, Bot: &summary}}})
	if err == nil {
		// A simultaneous identical create may have won with different generated
		// IDs. Return the committed conversation, never a discarded candidate.
		bot, err = s.opts.Repository.Bot(ctx, c.Key)
	}
	return bot, r, err
}

func (s *Service) Edit(ctx context.Context, c pa.Command, settings pa.EditBot) (pa.Bot, pa.Receipt, error) {
	a, err := s.authorize(ctx, c.Key, true)
	if err != nil {
		return pa.Bot{}, pa.Receipt{}, err
	}
	c, err = canonicalCommand(c, pa.EditOperation, a.PrincipalID, settings)
	if err != nil {
		return pa.Bot{}, pa.Receipt{}, err
	}
	if prior, e := s.replay(ctx, c); e != nil || prior != nil {
		if e != nil {
			return pa.Bot{}, pa.Receipt{}, e
		}
		bot, e := s.opts.Repository.Bot(ctx, c.Key)
		return bot, *prior, e
	}
	bot, err := s.opts.Repository.Bot(ctx, c.Key)
	if err != nil {
		return pa.Bot{}, pa.Receipt{}, err
	}
	next, err := pa.ApplyEdit(bot, settings)
	if err != nil {
		return pa.Bot{}, pa.Receipt{}, err
	}
	next.UpdatedAt = s.opts.Clock().UTC()
	conversation, err := s.opts.Repository.Conversation(ctx, c.Key, bot.ConversationID)
	if err != nil {
		return pa.Bot{}, pa.Receipt{}, err
	}
	fence := pa.Fence{Key: c.Key, Project: bot.Project, ConversationID: conversation.ID, BindingID: conversation.BindingID, Generation: conversation.Generation}
	summary := pa.BotSummary{Bot: next, Source: a.Source, Version: next.Version}
	r, err := s.opts.Repository.Commit(ctx, Mutation{Command: c, ExpectedVersion: bot.Version, Bot: &next,
		Receipt: pa.Receipt{ID: s.opts.ID(), Command: c, State: pa.EffectAccepted, ResourceID: bot.Key.BotID},
		Events:  []pa.Event{{Version: pa.ContractVersion, ID: s.opts.ID(), Fence: fence, Kind: pa.BotUpdated, At: next.UpdatedAt, Bot: &summary}}})
	return next, r, err
}

type listCursor struct {
	Binding string `json:"binding"`
	Cursor  string `json:"cursor"`
}

func cursorBinding(a Authority, q pa.ListBots) string {
	b, _ := json.Marshal([]string{a.PrincipalID, a.ReaderID, a.AuthorizationRevision, q.MachineID, q.Query})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (s *Service) List(ctx context.Context, q pa.ListBots) (pa.BotPage, error) {
	a, err := s.authorize(ctx, pa.BotKey{MachineID: q.MachineID}, false)
	if err != nil {
		return pa.BotPage{}, err
	}
	if err = pa.CheckPage(q.PageSize, false); err != nil {
		return pa.BotPage{}, err
	}
	binding := cursorBinding(a, q)
	if q.Cursor != "" {
		var cursor listCursor
		b, e := base64.RawURLEncoding.DecodeString(q.Cursor)
		if e != nil || json.Unmarshal(b, &cursor) != nil || cursor.Binding != binding {
			return pa.BotPage{}, serviceFailure(pa.CursorExpired, "list_cursor")
		}
		q.Cursor = cursor.Cursor
	}
	page, err := s.reader.ListForReader(ctx, q, a.ReaderID)
	if err != nil {
		return pa.BotPage{}, err
	}
	for i := range page.Items {
		if page.Items[i].Bot.Key.MachineID != q.MachineID {
			return pa.BotPage{}, serviceFailure(pa.ScopeDenied, "list_source")
		}
		page.Items[i].Source = a.Source
	}
	page.Sources = []pa.SourceStatus{a.Source}
	if page.Items == nil {
		page.Items = []pa.BotSummary{}
	}
	if page.NextCursor != "" {
		b, _ := json.Marshal(listCursor{Binding: binding, Cursor: page.NextCursor})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(b)
	}
	return page, nil
}

func (s *Service) Get(ctx context.Context, key pa.BotKey) (pa.Bot, pa.Conversation, pa.SourceStatus, error) {
	a, err := s.authorize(ctx, key, false)
	if err != nil {
		return pa.Bot{}, pa.Conversation{}, pa.SourceStatus{}, err
	}
	bot, err := s.opts.Repository.Bot(ctx, key)
	if err != nil {
		return pa.Bot{}, pa.Conversation{}, a.Source, err
	}
	conversation, err := s.opts.Repository.Conversation(ctx, key, bot.ConversationID)
	return bot, conversation, a.Source, err
}

func (s *Service) History(ctx context.Context, q HistoryQuery) (MessagePage, error) {
	if _, err := s.authorize(ctx, q.Key, false); err != nil {
		return MessagePage{}, err
	}
	if err := pa.CheckPage(q.PageSize, false); err != nil {
		return MessagePage{}, err
	}
	if q.BeforeSeq < 0 {
		return MessagePage{}, serviceFailure(pa.Invalid, "before_seq")
	}
	return s.opts.Repository.Messages(ctx, q)
}

func (s *Service) Events(ctx context.Context, q pa.EventQuery) (pa.EventPage, error) {
	if _, err := s.authorize(ctx, q.Key, false); err != nil {
		return pa.EventPage{}, err
	}
	if err := pa.CheckPage(q.PageSize, true); err != nil {
		return pa.EventPage{}, err
	}
	if q.AfterSeq < 0 {
		return pa.EventPage{}, serviceFailure(pa.Invalid, "after_seq")
	}
	return s.opts.Events.Read(ctx, q)
}

func (s *Service) Export(ctx context.Context, key pa.BotKey) (Export, error) {
	if _, err := s.authorize(ctx, key, false); err != nil {
		return Export{}, err
	}
	return s.opts.Repository.Export(ctx, key)
}

func (s *Service) Send(ctx context.Context, c pa.Command, message pa.SendMessage) (pa.Receipt, error) {
	a, err := s.authorize(ctx, c.Key, true)
	if err != nil {
		return pa.Receipt{}, err
	}
	if c.Key != message.Fence.Key {
		return pa.Receipt{}, serviceFailure(pa.ScopeDenied, "message_key")
	}
	if err = pa.ValidateMessage(message.Text); err != nil {
		return pa.Receipt{}, err
	}
	c, err = canonicalCommand(c, pa.SendOperation, a.PrincipalID, message)
	if err != nil {
		return pa.Receipt{}, err
	}
	return s.opts.Runtime.Submit(ctx, Submission{Command: c, Message: message, Feature: a.Feature, Source: a.Source})
}

func (s *Service) Stop(ctx context.Context, request StopRequest) (pa.Receipt, error) {
	a, err := s.authorize(ctx, request.Command.Key, true)
	if err != nil {
		return pa.Receipt{}, err
	}
	if request.Command.Key != request.Fence.Key {
		return pa.Receipt{}, serviceFailure(pa.ScopeDenied, "stop_key")
	}
	request.Command, err = canonicalCommand(request.Command, pa.StopOperation, a.PrincipalID, struct {
		Fence  pa.Fence
		TurnID string
	}{request.Fence, request.TurnID})
	if err != nil {
		return pa.Receipt{}, err
	}
	return s.opts.Runtime.Stop(ctx, request)
}

func (s *Service) Answer(ctx context.Context, c pa.Command, answer pa.PermissionAnswer) (pa.Receipt, error) {
	a, err := s.authorize(ctx, c.Key, true)
	if err != nil {
		return pa.Receipt{}, err
	}
	if c.Key != answer.Fence.Key {
		return pa.Receipt{}, serviceFailure(pa.ScopeDenied, "permission_key")
	}
	c, err = canonicalCommand(c, pa.AnswerOperation, a.PrincipalID, answer)
	if err != nil {
		return pa.Receipt{}, err
	}
	return s.opts.Runtime.Answer(ctx, AnswerRequest{Command: c, Answer: answer, Feature: a.Feature, Source: a.Source})
}

func (s *Service) MarkRead(ctx context.Context, c pa.Command, cursor pa.ReadCursor) (pa.ReadCursor, error) {
	a, err := s.authorizeObservation(ctx, c.Key)
	if err != nil {
		return pa.ReadCursor{}, err
	}
	if c.Key != cursor.Key {
		return pa.ReadCursor{}, serviceFailure(pa.ScopeDenied, "read_key")
	}
	cursor.ReaderID = a.ReaderID
	c, err = canonicalCommand(c, pa.ReadOperation, a.PrincipalID, cursor)
	if err != nil {
		return pa.ReadCursor{}, err
	}
	return s.opts.Repository.MarkRead(ctx, c, cursor)
}

func (s *Service) Observe(ctx context.Context, c pa.Command, receiptID string, seq int64) (pa.Receipt, error) {
	a, err := s.authorizeObservation(ctx, c.Key)
	if err != nil {
		return pa.Receipt{}, err
	}
	c, err = canonicalCommand(c, pa.ObserveOperation, a.PrincipalID, struct {
		ReceiptID string
		EventSeq  int64
	}{receiptID, seq})
	if err != nil {
		return pa.Receipt{}, err
	}
	return s.opts.Repository.Observe(ctx, c, receiptID, seq)
}

// Read/observed metadata belongs to the authenticated reader. It does not edit
// a Bot, grant a native permission or launch work, including in retained history.
// A cached/offline source cannot accept a new observation.
func (s *Service) authorizeObservation(ctx context.Context, key pa.BotKey) (Authority, error) {
	a, err := s.authorize(ctx, key, false)
	if err == nil && (a.Source.State != pa.SourceOnline || !a.Source.Fresh) {
		err = serviceFailure(pa.Offline, "read_observation")
	}
	return a, err
}

func (s *Service) Forget(ctx context.Context, request ForgetRequest) (pa.MemoryRevision, error) {
	a, err := s.authorize(ctx, request.Command.Key, true)
	if err != nil {
		return pa.MemoryRevision{}, err
	}
	bot, err := s.opts.Repository.Bot(ctx, request.Command.Key)
	if err != nil {
		return pa.MemoryRevision{}, err
	}
	if request.Project != bot.Project {
		return pa.MemoryRevision{}, serviceFailure(pa.ScopeDenied, "memory_project")
	}
	request.Command, err = canonicalCommand(request.Command, pa.ForgetOperation, a.PrincipalID, struct {
		Project          pa.ProjectScope
		ExpectedRevision int64
		SourceIDs        []string
	}{request.Project, request.ExpectedRevision, request.SourceIDs})
	if err != nil {
		return pa.MemoryRevision{}, err
	}
	return s.opts.Memory.Forget(ctx, request)
}

func (s *Service) SaveMemory(ctx context.Context, request MemoryWrite) (pa.MemoryRevision, error) {
	a, err := s.authorize(ctx, request.Command.Key, true)
	if err != nil {
		return pa.MemoryRevision{}, err
	}
	bot, err := s.opts.Repository.Bot(ctx, request.Command.Key)
	if err != nil {
		return pa.MemoryRevision{}, err
	}
	if request.Entry.Key != bot.Key || request.Entry.Project != bot.Project || request.Entry.ID == "" {
		return pa.MemoryRevision{}, serviceFailure(pa.ScopeDenied, "memory_entry")
	}
	if err = pa.ValidateMessage(request.Entry.Text); err != nil {
		return pa.MemoryRevision{}, err
	}
	// This endpoint saves the person's literal entry. A client cannot assert
	// provenance from other Bots, transcripts or a supposedly native summary.
	request.Entry.Sources = []pa.MemorySource{{ID: "manual:" + request.Entry.ID, Key: bot.Key, Project: bot.Project, Kind: "human", Revision: request.ExpectedRevision}}
	request.Entry.Revision = request.ExpectedRevision
	request.Entry.Forgotten = false
	request.Command, err = canonicalCommand(request.Command, pa.MemorySaveOperation, a.PrincipalID, struct {
		Entry            pa.MemoryEntry
		ExpectedRevision int64
	}{request.Entry, request.ExpectedRevision})
	if err != nil {
		return pa.MemoryRevision{}, err
	}
	return s.opts.Memory.Save(ctx, request)
}
