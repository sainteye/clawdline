package projectassistant

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
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
	NetworkAccount      func(context.Context) (string, error)
	NetworkSettings     NetworkSettingsStore
	NetworkCapabilities func(context.Context, string, string) pa.NetworkCapabilities
	LoginProfiles       LoginProfiles
	Extensions          ExtensionInventory
	ProjectMemory       func(context.Context, pa.ProjectResolution, string, ProjectMemoryPage) (pa.MemoryLayer, string, error)
	Configuration       func(context.Context, pa.Bot, string, string) (pa.ConfigurationCatalog, error)
	Repository          Repository
	ReadViews           ReadViews
	// ReadViewByteLimit is supplied by trusted composition from the existing
	// Cloud plaintext ceiling. It is never accepted from a wire request.
	ReadViewByteLimit int
	Memory            Memory
	Events            EventFeed
	Runtime           Runtime
	Projects          ProjectResolver
	// Rules is optional; without it the rules routes answer unsupported.
	Rules       RuleStore
	SourceRules SourceRuleStore
	DevServers  HumanDevServers
	// Deleter removes a Bot's stored records; without it Delete is unsupported.
	Deleter BotDeleter
	// Groups and Rooms are optional; without them the group operations
	// answer unsupported (service_groups.go).
	Groups    *GroupRooms
	Rooms     *RoomDriver
	Authorize func(context.Context, pa.BotKey, bool) (Authority, error)
	Clock     func() time.Time
	ID        func() string
}

// Service has no startup side effects. Configuration, history, memory and
// projection operations never call Runtime or initialize a provider process.
type Service struct {
	opts     ServiceOptions
	reader   ReaderRepository
	catalogs *configurationCache
}

func NewService(opts ServiceOptions) (*Service, error) {
	r, ok := opts.Repository.(ReaderRepository)
	if !ok || opts.Memory == nil || opts.Events == nil || opts.Runtime == nil || opts.Projects == nil || opts.Authorize == nil || opts.Clock == nil || opts.ID == nil {
		return nil, errors.New("project assistants require repository, reader projection, memory, events, runtime, projects, authority, clock and IDs")
	}
	return &Service{opts: opts, reader: r, catalogs: newConfigurationCache()}, nil
}

// WithAuthority binds the existing operations to another trusted, in-process
// principal without changing the authority used by ordinary HTTP callers.
func (s *Service) WithAuthority(authorize func(context.Context, pa.BotKey, bool) (Authority, error)) *Service {
	bound := *s
	bound.opts.Authorize = authorize
	return &bound
}

func serviceFailure(code pa.ErrorCode, field string) error {
	return &pa.Failure{Code: code, Field: field}
}

func (s *Service) authorize(ctx context.Context, key pa.BotKey, write bool) (Authority, error) {
	if err := ctx.Err(); err != nil {
		return Authority{}, err
	}
	authorizedKey := key
	if repo, ok := s.opts.Repository.(backgroundOwnership); ok {
		owner, err := repo.BackgroundOwner(ctx, key)
		if err != nil {
			return Authority{}, err
		}
		if owner.BotID != "" {
			if ctx.Value(backgroundChannelKey{}) != key {
				return Authority{}, serviceFailure(pa.NotFound, "bot")
			}
			authorizedKey = owner
		}
	}
	a, err := s.opts.Authorize(ctx, authorizedKey, write)
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

// Preparation may resolve a Project or read storage after admission. Recheck
// authority immediately before handing an effect to its owning port. This is
// not a transaction with an external authorization store or a lock held while
// the port runs. Runtime retains its execution fences, and HTTP still checks
// authority before publishing a response.
func (s *Service) authorizeEffect(ctx context.Context, key pa.BotKey, write bool, expected Authority) (Authority, error) {
	current, err := s.authorize(ctx, key, write)
	if err != nil {
		return Authority{}, err
	}
	return sameEffectAuthority(expected, current)
}

func sameEffectAuthority(expected, current Authority) (Authority, error) {
	if current.PrincipalID != expected.PrincipalID || current.ReaderID != expected.ReaderID {
		return Authority{}, serviceFailure(pa.ScopeDenied, "effect_principal")
	}
	if current.AuthorizationRevision != expected.AuthorizationRevision {
		return Authority{}, serviceFailure(pa.StaleVersion, "effect_authorization")
	}
	return current, nil
}

func (s *Service) authorizeObservationEffect(ctx context.Context, key pa.BotKey, expected Authority) error {
	current, err := s.authorizeObservation(ctx, key)
	if err != nil {
		return err
	}
	_, err = sameEffectAuthority(expected, current)
	return err
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
	if c.Key.BotID == "default-lead" {
		return pa.Bot{}, pa.Receipt{}, serviceFailure(pa.ScopeDenied, "host_bot")
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
	resolved, err := s.resolveCreateWorkspace(ctx, c, settings)
	if err != nil {
		return pa.Bot{}, pa.Receipt{}, err
	}
	if err = pa.ValidateCreate(settings, resolved); err != nil {
		return pa.Bot{}, pa.Receipt{}, err
	}
	now := s.opts.Clock().UTC()
	bot := pa.Bot{Key: c.Key, Project: resolved.Scope, ConversationID: s.opts.ID(), Name: settings.Name,
		Appearance: settings.Appearance, Agent: settings.Agent, FallbackAgents: pa.FallbackConfigOf(settings.FallbackAgents), FallbackSteps: pa.FallbackStepConfigOf(settings.FallbackSteps), PermissionPolicy: settings.PermissionPolicy, Mode: settings.Mode, ManageTeam: settings.ManageTeam,
		ReplyInput:   pa.ReplyInputSteer,
		Instructions: pa.InstructionSnapshot{Revision: 1, Text: settings.Instructions, RoleSnapshotRef: settings.RoleSnapshotRef},
		Version:      1, CreatedAt: now, UpdatedAt: now}
	conversation := pa.Conversation{Key: c.Key, ID: bot.ConversationID, MemoryRevision: 1, Version: 1}
	summary := pa.BotSummary{Bot: bot, ActivityAt: now, Source: a.Source, Version: 1}
	fence := pa.Fence{Key: bot.Key, Project: bot.Project, ConversationID: bot.ConversationID}
	a, err = s.authorizeEffect(ctx, c.Key, true, a)
	if err != nil {
		return pa.Bot{}, pa.Receipt{}, err
	}
	summary.Source = a.Source
	var r pa.Receipt
	commit := func() error {
		var e error
		r, e = s.opts.Repository.Commit(ctx, Mutation{Command: c, ExpectedVersion: 0, Bot: &bot, Conversation: &conversation,
			Receipt: pa.Receipt{ID: s.opts.ID(), Command: c, State: pa.EffectAccepted, ResourceID: bot.Key.BotID},
			Events:  []pa.Event{{Version: pa.ContractVersion, ID: s.opts.ID(), Fence: fence, Kind: pa.BotCreated, At: now, Bot: &summary}}})
		return e
	}
	if pa.HasLoginSelection(bot) {
		if err = s.validateLoginSelection(ctx, bot); err != nil {
			return pa.Bot{}, pa.Receipt{}, err
		}
		if s.opts.LoginProfiles == nil {
			return pa.Bot{}, pa.Receipt{}, serviceFailure(pa.Unsupported, "login_profiles")
		}
		err = s.opts.LoginProfiles.WithSelection(ctx, bot, commit)
	} else {
		err = commit()
	}
	if err == nil {
		// A simultaneous identical create may have won with different generated
		// IDs. Return the committed conversation, never a discarded candidate.
		bot, err = s.opts.Repository.Bot(ctx, c.Key)
	}
	return bot, r, err
}

func (s *Service) resolveCreateWorkspace(ctx context.Context, c pa.Command, settings pa.CreateBot) (pa.ProjectResolution, error) {
	if settings.Target.ProjectID != "" {
		if pa.IsPersonalWorkspaceID(settings.Target.ProjectID) {
			return pa.ProjectResolution{}, serviceFailure(pa.ScopeDenied, "personal_workspace")
		}
		return s.opts.Projects.Resolve(ctx, settings.Target)
	}
	provisioner, ok := s.opts.Projects.(PersonalWorkspaceProvisioner)
	if !ok {
		return pa.ProjectResolution{}, serviceFailure(pa.Unsupported, "personal_workspace")
	}
	selection := pa.WorkspaceForCreate(c)
	expected := pa.PersonalScope(selection)
	if err := pa.ValidateCreate(settings, pa.ProjectResolution{Scope: expected, Directory: selection.ProjectID, Current: true, Available: true}); err != nil {
		return pa.ProjectResolution{}, err
	}
	resolved, err := provisioner.ProvisionPersonalWorkspace(ctx, selection)
	if err == nil && resolved.Scope != expected {
		err = serviceFailure(pa.ScopeDenied, "personal_workspace")
	}
	return resolved, err
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
	if settings.Roster != nil && settings.Roster.NewSession {
		if settings.Roster.DesignateLead || settings.Roster.Pinned != nil || settings.Roster.Section != nil {
			return pa.Bot{}, pa.Receipt{}, serviceFailure(pa.Invalid, "new_session")
		}
		resetter, ok := s.opts.Runtime.(SessionResetter)
		if !ok {
			return pa.Bot{}, pa.Receipt{}, serviceFailure(pa.Unsupported, "new_session")
		}
		if _, err = s.authorizeEffect(ctx, c.Key, true, a); err != nil {
			return pa.Bot{}, pa.Receipt{}, err
		}
		r, err := resetter.NewSession(ctx, c, settings.ExpectedVersion)
		if err != nil {
			return pa.Bot{}, pa.Receipt{}, err
		}
		bot, err := s.opts.Repository.Bot(ctx, c.Key)
		return bot, r, err
	}
	// An edit that names its base is rebased on the Bot read for each attempt;
	// a turn or memory write that lands between that read and the commit
	// advances the version only, so the attempt is repeated, a bounded number
	// of times, against a fresh read. The slow configuration and extension
	// probes are not repeated when the rebased result still matches what they
	// accepted, so a retry costs a read and a commit.
	var validated *pa.Bot
	for attempt := 1; ; attempt++ {
		next, r, err := s.editOnce(ctx, c, a, settings, &validated)
		if err == nil || settings.Base == nil || attempt >= pa.MaxSettingsRebaseAttempts || pa.Code(err) != pa.StaleVersion || pa.SettingsConflicts(err) != nil {
			return next, r, err
		}
	}
}

// sameProbedSettings reports whether the configuration and extension probes
// that accepted one result also cover the other.
func sameProbedSettings(a, b pa.Bot) bool {
	return a.Agent == b.Agent && a.LoginProfile == b.LoginProfile && pa.SameRuntimeSettings(a.RuntimeSettings, b.RuntimeSettings) && pa.SameExtensions(a.Extensions, b.Extensions)
}

func (s *Service) editOnce(ctx context.Context, c pa.Command, a Authority, settings pa.EditBot, validated **pa.Bot) (pa.Bot, pa.Receipt, error) {
	bot, err := s.opts.Repository.Bot(ctx, c.Key)
	if err != nil {
		return pa.Bot{}, pa.Receipt{}, err
	}
	settings, err = pa.RebaseEdit(bot, settings)
	if err != nil {
		return pa.Bot{}, pa.Receipt{}, err
	}
	next, err := pa.ApplyEdit(bot, settings)
	if err != nil {
		return pa.Bot{}, pa.Receipt{}, err
	}
	if pa.HasLoginSelection(next) {
		if err = s.validateLoginSelection(ctx, next); err != nil {
			return pa.Bot{}, pa.Receipt{}, err
		}
	}
	// Validate every referenced identity before any model/configuration probe.
	// WithSelection checks again at the publication transaction after slow reads.
	if pa.HasLoginSelection(next) {
		if s.opts.LoginProfiles == nil {
			return pa.Bot{}, pa.Receipt{}, serviceFailure(pa.Unsupported, "login_profiles")
		}
		if err = s.opts.LoginProfiles.WithSelection(ctx, next, func() error { return nil }); err != nil {
			return pa.Bot{}, pa.Receipt{}, err
		}
	}
	probed := *validated != nil && sameProbedSettings(**validated, next)
	if !probed && settings.RuntimeSettings != nil && !pa.SameRuntimeSettings(next.RuntimeSettings, bot.RuntimeSettings) {
		if err = s.validateConfiguration(ctx, next); err != nil {
			return pa.Bot{}, pa.Receipt{}, err
		}
	}
	if !probed && settings.Extensions != nil {
		if err = s.validateExtensions(ctx, next); err != nil {
			return pa.Bot{}, pa.Receipt{}, err
		}
	}
	accepted := next
	*validated = &accepted
	next.UpdatedAt = s.opts.Clock().UTC()
	conversation, err := s.opts.Repository.Conversation(ctx, c.Key, bot.ConversationID)
	if err != nil {
		return pa.Bot{}, pa.Receipt{}, err
	}
	fence := pa.Fence{Key: c.Key, Project: bot.Project, ConversationID: conversation.ID, BindingID: conversation.BindingID, Generation: conversation.Generation}
	summary := pa.BotSummary{Bot: next, Source: a.Source, Version: next.Version}
	a, err = s.authorizeEffect(ctx, c.Key, true, a)
	if err != nil {
		return pa.Bot{}, pa.Receipt{}, err
	}
	summary.Source = a.Source
	var r pa.Receipt
	commit := func() error {
		var e error
		r, e = s.opts.Repository.Commit(ctx, Mutation{Command: c, ExpectedVersion: bot.Version, Bot: &next, DesignateLead: settings.Roster != nil && settings.Roster.DesignateLead,
			Receipt: pa.Receipt{ID: s.opts.ID(), Command: c, State: pa.EffectAccepted, ResourceID: bot.Key.BotID},
			Events:  []pa.Event{{Version: pa.ContractVersion, ID: s.opts.ID(), Fence: fence, Kind: pa.BotUpdated, At: next.UpdatedAt, Bot: &summary}}})
		return e
	}
	if s.opts.LoginProfiles != nil {
		err = s.opts.LoginProfiles.WithSelection(ctx, next, commit)
	} else if pa.HasLoginSelection(next) {
		err = serviceFailure(pa.Unsupported, "login_profiles")
	} else {
		err = commit()
	}
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
	if reader, ok := s.opts.Repository.(interface {
		ConversationForChat(context.Context, pa.BotKey, string) (pa.Conversation, error)
	}); ok && err == nil {
		conversation, err = reader.ConversationForChat(ctx, key, bot.ConversationID)
	}
	return bot, conversation, a.Source, err
}

func (s *Service) History(ctx context.Context, q HistoryQuery) (MessagePage, error) {
	a, err := s.authorize(ctx, q.Key, false)
	if err != nil {
		return MessagePage{}, err
	}
	if err := pa.CheckPage(q.PageSize, false); err != nil {
		return MessagePage{}, err
	}
	if q.BeforeSeq < 0 {
		return MessagePage{}, serviceFailure(pa.Invalid, "before_seq")
	}
	page, e := s.opts.Repository.Messages(ctx, q)
	for i := range page.Items {
		personalizeReaction(&page.Items[i], a.ReaderID)
	}
	return page, e
}

func (s *Service) Events(ctx context.Context, q pa.EventQuery) (pa.EventPage, error) {
	a, err := s.authorize(ctx, q.Key, false)
	if err != nil {
		return pa.EventPage{}, err
	}
	if err := pa.CheckPage(q.PageSize, true); err != nil {
		return pa.EventPage{}, err
	}
	if q.AfterSeq < 0 {
		return pa.EventPage{}, serviceFailure(pa.Invalid, "after_seq")
	}
	page, e := s.opts.Events.Read(ctx, q)
	for i := range page.Items {
		personalizeReaction(page.Items[i].Message, a.ReaderID)
	}
	return page, e
}

func (s *Service) Export(ctx context.Context, key pa.BotKey) (Export, error) {
	if _, err := s.authorize(ctx, key, false); err != nil {
		return Export{}, err
	}
	return s.opts.Repository.Export(ctx, key)
}

// SendReceipt returns admission evidence for one browser send in its original chat.
func (s *Service) SendReceipt(ctx context.Context, key pa.BotKey, conversation, id string) (pa.Receipt, error) {
	if !validBrowserSendID(id) || conversation == "" {
		return pa.Receipt{}, serviceFailure(pa.Invalid, "send_identity")
	}
	ctx, _, err := s.backgroundChannel(ctx, key)
	if err != nil {
		return pa.Receipt{}, err
	}
	a, err := s.authorize(ctx, key, false)
	if err != nil {
		return pa.Receipt{}, err
	}
	store, ok := s.opts.Repository.(interface {
		SendReceipt(context.Context, pa.BotKey, string, string) (pa.Receipt, error)
	})
	if !ok {
		return pa.Receipt{}, serviceFailure(pa.Unsupported, "send_receipt")
	}
	receipt, err := store.SendReceipt(ctx, key, conversation, id)
	if err != nil {
		return pa.Receipt{}, err
	}
	_, err = s.authorizeEffect(ctx, key, false, a)
	return receipt, err
}

func validBrowserSendID(id string) bool {
	if len(id) != len("00000000-0000-0000-0000-000000000000") {
		return false
	}
	for i, ch := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if ch != '-' {
				return false
			}
		} else if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

func (s *Service) Send(ctx context.Context, c pa.Command, message pa.SendMessage) (pa.Receipt, error) {
	ctx, thread, err := s.backgroundChannel(ctx, c.Key)
	if err != nil {
		return pa.Receipt{}, err
	}
	if thread && (len(message.Attachments) != 0 || len(message.UploadIDs) != 0 || message.QuoteMessageID != "" || message.Handoff != nil) {
		return pa.Receipt{}, serviceFailure(pa.Unsupported, "background_thread_input")
	}
	a, err := s.authorize(ctx, c.Key, true)
	if err != nil {
		return pa.Receipt{}, err
	}
	if c.ClientAttemptID != "" && (!validBrowserSendID(c.IdempotencyKey) || !validBrowserSendID(c.ClientAttemptID)) {
		return pa.Receipt{}, serviceFailure(pa.Invalid, "client_attempt_id")
	}
	if c.Key != message.Fence.Key {
		return pa.Receipt{}, serviceFailure(pa.ScopeDenied, "message_key")
	}
	if err = pa.ValidateInputSend(message.Text, message.Attachments, message.UploadIDs); err != nil {
		return pa.Receipt{}, err
	}
	c, err = canonicalCommand(c, pa.SendOperation, a.PrincipalID, message)
	if err != nil {
		return pa.Receipt{}, err
	}
	a, err = s.authorizeEffect(ctx, c.Key, true, a)
	if err != nil {
		return pa.Receipt{}, err
	}
	var inputCheck func(context.Context) error
	if len(message.UploadIDs) > 0 {
		inputCheck = func(ctx context.Context) error {
			current, e := s.authorizeEffect(ctx, c.Key, true, a)
			if e != nil {
				return e
			}
			if readViewBinding(current) != readViewBinding(a) {
				return serviceFailure(pa.StaleVersion, "input_authority")
			}
			return ctx.Err()
		}
	}
	authorityCheck := func(ctx context.Context) error {
		_, e := s.authorizeEffect(ctx, c.Key, true, a)
		return e
	}
	return s.opts.Runtime.Submit(ctx, Submission{Command: c, Message: message, InputOwner: InputOwner{Authority: readViewBinding(a)}, InputCheck: inputCheck, AuthorityCheck: authorityCheck, Feature: a.Feature, Source: a.Source})
}

// SendCharterEdit sends the person's words from a charter page to the lead
// Bot as an ordinary message of theirs, marked as a change request for one
// Project's charter so the lead's prompt carries that charter and the lead
// can propose a new version. The mark grants nothing: proposing is the lead's
// own tool in any direct turn, and only the person applies a proposal.
func (s *Service) SendCharterEdit(ctx context.Context, c pa.Command, fence pa.Fence, text string, edit pa.CharterEdit) (pa.Receipt, error) {
	a, err := s.authorize(ctx, c.Key, true)
	if err != nil {
		return pa.Receipt{}, err
	}
	if fence.Key != c.Key {
		return pa.Receipt{}, serviceFailure(pa.ScopeDenied, "message_key")
	}
	if err = pa.ValidateCharterRequest(text); err != nil {
		return pa.Receipt{}, err
	}
	if err = pa.ValidateCharterProject(edit.ProjectID); err != nil {
		return pa.Receipt{}, err
	}
	message := pa.SendMessage{Fence: fence, Text: text}
	// A retry composes a fresh fence and may see a newer charter; the request
	// is the person's words for this Project.
	c, err = canonicalCommand(c, pa.SendOperation, a.PrincipalID, struct {
		Text    string
		Charter string
	}{text, edit.ProjectID})
	if err != nil {
		return pa.Receipt{}, err
	}
	check := func(ctx context.Context) error {
		_, err := s.authorizeEffect(ctx, c.Key, true, a)
		return err
	}
	if err = check(ctx); err != nil {
		return pa.Receipt{}, err
	}
	return s.opts.Runtime.Submit(ctx, Submission{Command: c, Message: message, InputOwner: InputOwner{Authority: readViewBinding(a)},
		AuthorityCheck: check, Feature: a.Feature, Source: a.Source, CharterEdit: &edit})
}

// CompactNow asks an idle Bot to compact its native context now. The prompt
// is the agent's own command; the person supplies no text.
func (s *Service) CompactNow(ctx context.Context, c pa.Command, fence pa.Fence) (pa.Receipt, error) {
	a, err := s.authorize(ctx, c.Key, true)
	if err != nil {
		return pa.Receipt{}, err
	}
	if fence.Key != c.Key {
		return pa.Receipt{}, serviceFailure(pa.ScopeDenied, "message_key")
	}
	c, err = canonicalCommand(c, pa.SendOperation, a.PrincipalID, struct {
		Fence   pa.Fence
		Compact bool
	}{fence, true})
	if err != nil {
		return pa.Receipt{}, err
	}
	check := func(ctx context.Context) error {
		_, err := s.authorizeEffect(ctx, c.Key, true, a)
		return err
	}
	if err = check(ctx); err != nil {
		return pa.Receipt{}, err
	}
	return s.opts.Runtime.Submit(ctx, Submission{Command: c, Message: pa.SendMessage{Fence: fence, Text: compactRequestText},
		Feature: a.Feature, Source: a.Source, AuthorityCheck: check, Compact: true})
}

func (s *Service) LeadCallStarted(ctx context.Context, c pa.Command, fence pa.Fence) (pa.Receipt, error) {
	a, err := s.authorize(ctx, c.Key, true)
	if err != nil {
		return pa.Receipt{}, err
	}
	if fence.Key != c.Key || !pa.ValidLeadCallKey(c.IdempotencyKey) {
		return pa.Receipt{}, serviceFailure(pa.ScopeDenied, "lead_call")
	}
	c, err = canonicalCommand(c, pa.SendOperation, a.PrincipalID, struct {
		Fence pa.Fence
		Call  bool
	}{fence, true})
	if err != nil {
		return pa.Receipt{}, err
	}
	check := func(ctx context.Context) error {
		_, err := s.authorizeEffect(ctx, c.Key, true, a)
		return err
	}
	return s.opts.Runtime.Submit(ctx, Submission{Command: c, Message: pa.SendMessage{Fence: fence, Text: pa.LeadCallGreeting},
		Feature: a.Feature, Source: a.Source, AuthorityCheck: check, Notice: true, LeadCall: true})
}

func (s *Service) Stop(ctx context.Context, request StopRequest) (pa.Receipt, error) {
	ctx, _, err := s.backgroundChannel(ctx, request.Command.Key)
	if err != nil {
		return pa.Receipt{}, err
	}
	a, err := s.authorize(ctx, request.Command.Key, true)
	if err != nil {
		return pa.Receipt{}, err
	}
	if request.Command.Key != request.Fence.Key {
		return pa.Receipt{}, serviceFailure(pa.ScopeDenied, "stop_key")
	}
	request.Command, err = canonicalCommand(request.Command, pa.StopOperation, a.PrincipalID, struct {
		Fence   pa.Fence
		TurnID  string
		Release bool `json:",omitempty"`
	}{request.Fence, request.TurnID, request.ReleaseUnconfirmed})
	if err != nil {
		return pa.Receipt{}, err
	}
	if _, err = s.authorizeEffect(ctx, request.Command.Key, true, a); err != nil {
		return pa.Receipt{}, err
	}
	return s.opts.Runtime.Stop(ctx, request)
}

func (s *Service) SendNow(ctx context.Context, request SendNowRequest) (pa.Receipt, error) {
	ctx, _, err := s.backgroundChannel(ctx, request.Command.Key)
	if err != nil {
		return pa.Receipt{}, err
	}
	a, err := s.authorize(ctx, request.Command.Key, true)
	if err != nil {
		return pa.Receipt{}, err
	}
	if request.Command.Key != request.Fence.Key || request.TurnID == "" {
		return pa.Receipt{}, serviceFailure(pa.ScopeDenied, "send_now_key")
	}
	request.Command, err = canonicalCommand(request.Command, pa.SendNowOperation, a.PrincipalID, struct {
		Fence  pa.Fence
		TurnID string
	}{request.Fence, request.TurnID})
	if err != nil {
		return pa.Receipt{}, err
	}
	if _, err = s.authorizeEffect(ctx, request.Command.Key, true, a); err != nil {
		return pa.Receipt{}, err
	}
	return s.opts.Runtime.SendNow(ctx, request)
}

func (s *Service) Answer(ctx context.Context, c pa.Command, answer pa.PermissionAnswer) (pa.Receipt, error) {
	// Nothing before the runtime port commits anything, so an untyped
	// failure here (a cancelled request, a store read) is a refusal the
	// caller may retry, never an unknown outcome.
	ctx, _, err := s.backgroundChannel(ctx, c.Key)
	if err != nil {
		return pa.Receipt{}, pa.NotSent(err, "answer_channel")
	}
	a, err := s.authorize(ctx, c.Key, true)
	if err != nil {
		return pa.Receipt{}, pa.NotSent(err, "answer_authority")
	}
	if c.Key != answer.Fence.Key {
		return pa.Receipt{}, serviceFailure(pa.ScopeDenied, "permission_key")
	}
	c, err = canonicalCommand(c, pa.AnswerOperation, a.PrincipalID, answer)
	if err != nil {
		return pa.Receipt{}, pa.NotSent(err, "answer_command")
	}
	a, err = s.authorizeEffect(ctx, c.Key, true, a)
	if err != nil {
		return pa.Receipt{}, pa.NotSent(err, "answer_authority")
	}
	// Only a decision answer carries text or a suggested-reply option; a
	// native permission answer goes to the runtime unchanged.
	if answer.Text != "" || strings.HasPrefix(answer.OptionID, pa.DecisionOptionPrefix) {
		offer, e := s.opts.Repository.Permission(ctx, c.Key, answer.OfferID)
		if e == nil && offer.Kind == pa.DecisionOfferKind {
			return s.answerDecision(ctx, c, answer, offer, a)
		}
		// A suggested-reply id belongs to a decision card. When the card
		// cannot be read, say so instead of trying it as a native answer.
		if e != nil {
			return pa.Receipt{}, pa.NotSent(e, "decision_offer")
		}
		if answer.Text != "" {
			return pa.Receipt{}, serviceFailure(pa.Invalid, "permission_text")
		}
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
	if err = s.authorizeObservationEffect(ctx, c.Key, a); err != nil {
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
	if err = s.authorizeObservationEffect(ctx, c.Key, a); err != nil {
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
	if _, err = s.authorizeEffect(ctx, request.Command.Key, true, a); err != nil {
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
	if _, err = s.authorizeEffect(ctx, request.Command.Key, true, a); err != nil {
		return pa.MemoryRevision{}, err
	}
	return s.opts.Memory.Save(ctx, request)
}

// AuthorizeRead admits reading a Bot's stored pictures with the same current
// access as reading its messages.
func (s *Service) AuthorizeRead(ctx context.Context, key pa.BotKey) error {
	_, err := s.authorize(ctx, key, false)
	return err
}

// Rules combines the source's exact command rules with the agent's Project
// rules. A failed store never becomes an apparently complete empty result.
func (s *Service) Rules(ctx context.Context, key pa.BotKey) ([]pa.PermissionRule, error) {
	if _, err := s.authorize(ctx, key, false); err != nil {
		return nil, err
	}
	out := []pa.PermissionRule{}
	if s.opts.SourceRules != nil {
		bot, err := s.opts.Repository.Bot(ctx, key)
		if err != nil {
			return nil, err
		}
		rules, err := s.opts.SourceRules.List(ctx, key, bot.Project)
		if err != nil {
			return nil, err
		}
		out = append(out, rules...)
	}
	dir, err := s.ruleDirectory(ctx, key)
	if err != nil {
		if s.opts.SourceRules != nil && pa.Code(err) == pa.Unsupported {
			return out, nil
		}
		return nil, err
	}
	rules, err := s.opts.Rules.List(dir)
	if err != nil {
		return nil, err
	}
	return append(out, rules...), nil
}

// RemoveRule revokes one standing rule for the Bot's Project.
func (s *Service) RemoveRule(ctx context.Context, c pa.Command, rule pa.PermissionRule) error {
	a, err := s.authorize(ctx, c.Key, true)
	if err != nil {
		return err
	}
	if err = pa.ValidateRule(rule); err != nil {
		return err
	}
	if c, err = canonicalCommand(c, pa.RuleRemoveOperation, a.PrincipalID, rule); err != nil {
		return err
	}
	if _, err = s.authorizeEffect(ctx, c.Key, true, a); err != nil {
		return err
	}
	if s.opts.SourceRules != nil && s.opts.SourceRules.Handles(rule) {
		bot, err := s.opts.Repository.Bot(ctx, c.Key)
		if err != nil {
			return err
		}
		return s.opts.SourceRules.Remove(ctx, c.Key, bot.Project, rule)
	}
	dir, err := s.ruleDirectory(ctx, c.Key)
	if err != nil {
		return err
	}
	return s.opts.Rules.Remove(dir, rule)
}

func (s *Service) ruleDirectory(ctx context.Context, key pa.BotKey) (string, error) {
	if s.opts.Rules == nil {
		return "", serviceFailure(pa.Unsupported, "rules")
	}
	bot, err := s.opts.Repository.Bot(ctx, key)
	if err != nil {
		return "", err
	}
	if bot.Agent != pa.ClaudeCode {
		return "", serviceFailure(pa.Unsupported, "rules_agent")
	}
	selection := pa.ProjectSelection{MachineID: bot.Project.MachineID, ProjectID: bot.Project.ProjectID}
	resolved, err := s.opts.Projects.Resolve(ctx, selection)
	if err == nil {
		err = pa.ValidateTarget(selection, resolved)
	}
	if err == nil && (resolved.Scope != bot.Project || resolved.Directory == "") {
		err = serviceFailure(pa.ScopeDenied, "rules_project")
	}
	return resolved.Directory, err
}

// SetHidden hides the Bot from the list or brings it back, as Codync's Hide
// from list: its chat, memory and settings stay.
func (s *Service) SetHidden(ctx context.Context, c pa.Command, hidden bool) (pa.Bot, pa.Receipt, error) {
	a, err := s.authorize(ctx, c.Key, true)
	if err != nil {
		return pa.Bot{}, pa.Receipt{}, err
	}
	if c, err = canonicalCommand(c, pa.VisibilityOperation, a.PrincipalID, hidden); err != nil {
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
	if bot.Lead && hidden {
		return pa.Bot{}, pa.Receipt{}, serviceFailure(pa.ScopeDenied, "lead_sidebar")
	}
	next := bot
	next.Hidden = hidden
	next.Version++
	next.UpdatedAt = s.opts.Clock().UTC()
	conversation, err := s.opts.Repository.Conversation(ctx, c.Key, bot.ConversationID)
	if err != nil {
		return pa.Bot{}, pa.Receipt{}, err
	}
	if a, err = s.authorizeEffect(ctx, c.Key, true, a); err != nil {
		return pa.Bot{}, pa.Receipt{}, err
	}
	fence := pa.Fence{Key: c.Key, Project: bot.Project, ConversationID: conversation.ID, BindingID: conversation.BindingID, Generation: conversation.Generation}
	summary := pa.BotSummary{Bot: next, Source: a.Source, Version: next.Version}
	r, err := s.opts.Repository.Commit(ctx, Mutation{Command: c, ExpectedVersion: bot.Version, Bot: &next,
		Receipt: pa.Receipt{ID: s.opts.ID(), Command: c, State: pa.EffectAccepted, ResourceID: bot.Key.BotID},
		Events:  []pa.Event{{Version: pa.ContractVersion, ID: s.opts.ID(), Fence: fence, Kind: pa.BotUpdated, At: next.UpdatedAt, Bot: &summary}}})
	return next, r, err
}

// Delete removes the Bot and its conversation, as Codync's Delete; files it
// changed in the Project stay. A Bot still working is refused, so no native
// process is left running for a Bot that no longer exists.
func (s *Service) Delete(ctx context.Context, c pa.Command) error {
	if s.opts.Deleter == nil {
		return serviceFailure(pa.Unsupported, "delete")
	}
	a, err := s.authorize(ctx, c.Key, true)
	if err != nil {
		return err
	}
	if c, err = canonicalCommand(c, pa.DeleteOperation, a.PrincipalID, c.Key); err != nil {
		return err
	}
	pending, err := s.opts.Repository.PendingTurns(ctx, c.Key)
	if err != nil {
		return err
	}
	for _, t := range pending {
		if t.State != pa.TurnUnknown {
			return serviceFailure(pa.StaleVersion, "delete_busy")
		}
	}
	if _, err = s.authorizeEffect(ctx, c.Key, true, a); err != nil {
		return err
	}
	return s.opts.Deleter.DeleteBot(ctx, c.Key)
}
