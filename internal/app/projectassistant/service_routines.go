package projectassistant

import (
	"context"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type RoutineQuery struct {
	Key                                                     pa.BotKey
	Project                                                 pa.ProjectScope
	ConversationID, RoutineID, Cursor, AuthorizationBinding string
	PageSize                                                int
}

type RoutineRunResult struct {
	Run     pa.RoutineRun `json:"run"`
	Result  string        `json:"result"`
	Failure *pa.Failure   `json:"failure,omitempty"`
}

type RoutinePage struct {
	Fence       pa.Fence           `json:"fence"`
	Definitions []pa.Routine       `json:"definitions"`
	Runs        []RoutineRunResult `json:"runs"`
	NextCursor  string             `json:"next_cursor"`
	Cursors     []string           `json:"-"`
}

type HumanRoutineReader interface {
	ReadRoutines(context.Context, RoutineQuery) (RoutinePage, error)
}
type HumanRoutineRepository interface {
	RoutineDefinition(context.Context, pa.BotKey, string) (pa.Routine, error)
	QueuedRoutineRuns(context.Context, pa.BotKey, string) ([]pa.RoutineRun, error)
}

type RoutineReadResult struct {
	Page   RoutinePage
	Source pa.SourceStatus
	check  func(context.Context) error
}

func (r RoutineReadResult) Revalidate(ctx context.Context) error {
	if r.check == nil {
		return serviceFailure(pa.ScopeDenied, "routine_publication")
	}
	return r.check(ctx)
}

func (s *Service) routineProject(ctx context.Context, bot pa.Bot) error {
	selection := pa.ProjectSelection{MachineID: bot.Project.MachineID, ProjectID: bot.Project.ProjectID}
	p, err := s.opts.Projects.Resolve(ctx, selection)
	if err != nil {
		return err
	}
	if err = pa.ValidateTarget(selection, p); err != nil {
		return err
	}
	if p.Scope != bot.Project {
		return serviceFailure(pa.ScopeDenied, "routine_project")
	}
	return nil
}

func (s *Service) RoutinePage(ctx context.Context, q RoutineQuery) (RoutineReadResult, error) {
	if err := pa.CheckPage(q.PageSize, false); err != nil {
		return RoutineReadResult{}, err
	}
	a, err := s.readViewAuthority(ctx, q.Key)
	if err != nil {
		return RoutineReadResult{}, err
	}
	reader, ok := s.opts.ReadViews.(HumanRoutineReader)
	if !ok {
		return RoutineReadResult{}, serviceFailure(pa.Unsupported, "routine_reads")
	}
	binding := readViewBinding(a)
	q.AuthorizationBinding = binding
	check := func(ctx context.Context) error {
		current, e := s.readViewAuthority(ctx, q.Key)
		if e != nil {
			return e
		}
		if readViewBinding(current) != binding {
			return serviceFailure(pa.Revoked, "routine_authority_changed")
		}
		return ctx.Err()
	}
	page, err := reader.ReadRoutines(ctx, q)
	if err != nil {
		return RoutineReadResult{}, err
	}
	if page.Fence.Key != q.Key || page.Fence.Project != q.Project || page.Fence.ConversationID != q.ConversationID {
		return RoutineReadResult{}, serviceFailure(pa.ScopeDenied, "routine_scope")
	}
	if err = check(ctx); err != nil {
		return RoutineReadResult{}, err
	}
	return RoutineReadResult{Page: page, Source: a.Source, check: check}, nil
}

// HumanRoutine never forges an active native turn. Metadata changes need no
// provider or free process slot. The source store owns the atomic CAS/receipt.
func (s *Service) HumanRoutine(ctx context.Context, q HumanRoutineCommand) (pa.Receipt, error) {
	a, err := s.authorize(ctx, q.Command.Key, true)
	if err != nil {
		return pa.Receipt{}, err
	}
	if q.Fence.Key != q.Command.Key {
		return pa.Receipt{}, serviceFailure(pa.ScopeDenied, "routine_fence")
	}
	if q.ExpectedRevision < 0 || q.Request.Action == "save" && (q.Request.TimeoutSeconds < 1 || q.Request.TimeoutSeconds > pa.MaxRoutineTimeoutSeconds || len(q.Request.Triggers) < 1 || len(q.Request.Triggers) > pa.MaxRoutineTriggers || q.Request.IntervalSeconds != 0) {
		return pa.Receipt{}, serviceFailure(pa.Invalid, "routine_shape")
	}
	c, err := canonicalCommand(q.Command, pa.EditOperation, a.PrincipalID, struct {
		Action   string
		Fence    pa.Fence
		Revision int64
		Request  RoutineRequest
	}{q.Request.Action, q.Fence, q.ExpectedRevision, q.Request})
	if err != nil {
		return pa.Receipt{}, err
	}
	if prior, e := s.replay(ctx, c); e != nil || prior != nil {
		if e != nil {
			return pa.Receipt{}, e
		}
		if _, e = s.authorizeEffect(ctx, c.Key, true, a); e != nil {
			return pa.Receipt{}, e
		}
		return *prior, nil
	}
	repo, ok := s.opts.Repository.(HumanRoutineRepository)
	if !ok {
		return pa.Receipt{}, serviceFailure(pa.Unsupported, "routine_store")
	}
	bot, err := s.opts.Repository.Bot(ctx, c.Key)
	if err != nil {
		return pa.Receipt{}, err
	}
	conversation, err := s.opts.Repository.Conversation(ctx, c.Key, bot.ConversationID)
	if err != nil {
		return pa.Receipt{}, err
	}
	f := currentFence(bot, conversation)
	if err = checkHumanRoutineFence(q.Fence, f); err != nil {
		return pa.Receipt{}, err
	}
	if err = s.routineProject(ctx, bot); err != nil {
		return pa.Receipt{}, err
	}
	if q.Request.Action == "test" && conversation.BindingID == "" {
		return pa.Receipt{}, serviceFailure(pa.ResumeUnsupported, "routine_main_session")
	}
	defs := []pa.Routine{}
	if q.Request.ID != "" {
		d, e := repo.RoutineDefinition(ctx, c.Key, q.Request.ID)
		if e != nil {
			return pa.Receipt{}, e
		}
		if d.Project != f.Project || d.ConversationID != f.ConversationID {
			return pa.Receipt{}, serviceFailure(pa.ScopeDenied, "routine_owner")
		}
		if d.Version != q.ExpectedRevision {
			return pa.Receipt{}, serviceFailure(pa.StaleVersion, "routine_revision")
		}
		defs = append(defs, d)
	} else if q.ExpectedRevision != 0 || q.Request.Action != "save" {
		return pa.Receipt{}, serviceFailure(pa.Invalid, "routine_revision")
	}
	if q.Request.Action == "save" || q.Request.Action == "test" || q.Request.Action == "enable" && q.Request.Enabled != nil && *q.Request.Enabled {
		capabilities, ok := s.opts.Runtime.(HumanRoutineCapabilities)
		if !ok {
			return pa.Receipt{}, serviceFailure(pa.Unsupported, "routine_capabilities")
		}
		if err = capabilities.CheckHumanRoutine(ctx, bot, f); err != nil {
			return pa.Receipt{}, err
		}
	}
	runs := []pa.RoutineRun{}
	if q.Request.Action == "test" {
		runs, err = repo.QueuedRoutineRuns(ctx, c.Key, q.Request.ID)
		if err != nil {
			return pa.Receipt{}, err
		}
		if len(runs) > 0 && runs[0].Source != "test" && !defs[0].Enabled {
			return pa.Receipt{}, serviceFailure(pa.StaleVersion, "routine_queued_schedule")
		}
	}
	m, err := prepareRoutineMutation(f, q.Request, c, defs, runs, s.opts.Clock(), s.opts.ID, func(d pa.Routine) pa.RoutineRun {
		return pa.RoutineRun{Source: "schedule", ID: s.opts.ID(), RoutineID: d.ID, Key: d.Key, Project: d.Project, ConversationID: d.ConversationID, State: pa.TurnQueued, CreatedAt: s.opts.Clock(), TimeoutSeconds: d.TimeoutSeconds}
	})
	if err != nil {
		return pa.Receipt{}, err
	}
	if _, err = s.authorizeEffect(ctx, c.Key, true, a); err != nil {
		return pa.Receipt{}, err
	}
	if err = s.routineProject(ctx, bot); err != nil {
		return pa.Receipt{}, err
	}
	if _, err = s.authorizeEffect(ctx, c.Key, true, a); err != nil {
		return pa.Receipt{}, err
	}
	m.ExpectedVersion = bot.Version
	bot.Version++
	bot.UpdatedAt = s.opts.Clock()
	m.Bot = &bot
	m.Receipt.ID = s.opts.ID()
	m.Receipt.Command = c
	return s.opts.Repository.Commit(ctx, m)
}

// HumanRoutinePublication binds serialization to the same human grant. An
// effect may already have committed when it fails; callers retain the command.
func (s *Service) HumanRoutinePublication(ctx context.Context, key pa.BotKey) (func(context.Context) error, error) {
	a, err := s.authorize(ctx, key, true)
	if err != nil {
		return nil, err
	}
	binding := readViewBinding(a)
	return func(ctx context.Context) error {
		current, err := s.authorize(ctx, key, true)
		if err != nil {
			return serviceFailure(pa.OutcomeUnknown, "routine_publication")
		}
		if readViewBinding(current) != binding {
			return serviceFailure(pa.OutcomeUnknown, "routine_publication")
		}
		return ctx.Err()
	}, nil
}

// An idle, never-started Bot has no execution binding. Human metadata still
// carries the exact host-issued zero-generation fence; it creates no binding.
func checkHumanRoutineFence(got, current pa.Fence) error {
	if got.Key != current.Key || got.Project != current.Project || got.ConversationID != current.ConversationID {
		return serviceFailure(pa.ScopeDenied, "routine_fence")
	}
	if got.BindingID != current.BindingID || got.Generation != current.Generation {
		return serviceFailure(pa.StaleGeneration, "routine_fence")
	}
	if current.BindingID == "" && current.Generation == 0 {
		return pa.ValidateScope(current.Project)
	}
	return pa.CheckFence(got, current)
}
