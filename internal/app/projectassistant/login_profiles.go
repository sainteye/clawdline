package projectassistant

import (
	"context"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// LoginProfiles serializes selection, removal and native process holds on the
// source. It never accepts launch configuration from the HTTP caller.
type LoginProfiles interface {
	List(context.Context, pa.Agent) ([]pa.LoginProfile, error)
	Apply(context.Context, pa.LoginProfileAction, func(context.Context) error) (pa.LoginProfile, error)
	WithSelection(context.Context, pa.Bot, func() error) error
	Acquire(context.Context, pa.Bot) ([]string, func(), error)
	Quarantine(context.Context, pa.Bot) error
	Suspend(context.Context) error
	Close(context.Context) error
}

type LoginProcess interface {
	Wait(context.Context) error
	Close(context.Context) error
}
type LoginNative interface {
	SignOut(context.Context, pa.Agent, []string) error
	Start(context.Context, pa.Agent, []string) (LoginProcess, error)
	Status(context.Context, pa.Agent, []string) (string, error)
}

func (s *Service) LoginProfiles(ctx context.Context, q MemoryReadRequest, agent pa.Agent) ([]pa.LoginProfile, func(context.Context) error, error) {
	_, _, _, epoch, err := s.memoryOwner(ctx, q)
	if err != nil {
		return nil, nil, err
	}
	if s.opts.LoginProfiles == nil {
		return nil, nil, serviceFailure(pa.Unsupported, "login_profiles")
	}
	out, err := s.opts.LoginProfiles.List(ctx, agent)
	check := func(ctx context.Context) error {
		_, _, _, now, e := s.memoryOwner(ctx, q)
		if e != nil {
			return e
		}
		if now != epoch {
			return serviceFailure(pa.Revoked, "login_profiles_source")
		}
		return ctx.Err()
	}
	if err == nil {
		err = check(ctx)
	}
	return out, check, err
}

func (s *Service) LoginProfileAction(ctx context.Context, q pa.LoginProfileAction) (pa.LoginProfile, func(context.Context) error, error) {
	b, a, resolved, epoch, err := s.memoryOwner(ctx, MemoryReadRequest{Key: q.Command.Key, ConversationID: q.Fence.ConversationID})
	if err != nil {
		return pa.LoginProfile{}, nil, err
	}
	if _, err = s.authorizeEffect(ctx, b.Key, true, a); err != nil {
		return pa.LoginProfile{}, nil, err
	}
	if s.opts.LoginProfiles == nil {
		return pa.LoginProfile{}, nil, serviceFailure(pa.Unsupported, "login_profiles")
	}
	if p, ok := s.opts.LoginProfiles.(interface{ CheckProject(string) error }); ok {
		if e := p.CheckProject(resolved.Directory); e != nil {
			return pa.LoginProfile{}, nil, e
		}
	}
	payload := q
	payload.Command = pa.Command{}
	q.Command, err = canonicalCommand(q.Command, pa.LoginProfileOperation, a.PrincipalID, payload)
	if err != nil {
		return pa.LoginProfile{}, nil, err
	}
	check := func(ctx context.Context) error {
		current, _, _, now, e := s.memoryOwner(ctx, MemoryReadRequest{Key: b.Key, ConversationID: b.ConversationID})
		if e != nil {
			return e
		}
		if now != epoch || current.Project != b.Project {
			return serviceFailure(pa.Revoked, "login_profiles_source")
		}
		_, e = s.authorizeEffect(ctx, b.Key, true, a)
		return e
	}
	admit := func(ctx context.Context) error {
		if e := check(ctx); e != nil {
			return e
		}
		current, e := s.opts.Repository.Bot(ctx, b.Key)
		if e != nil {
			return e
		}
		if current.Version != q.ExpectedVersion {
			return serviceFailure(pa.StaleVersion, "bot")
		}
		c, e := s.opts.Repository.Conversation(ctx, b.Key, b.ConversationID)
		if e != nil {
			return e
		}
		now := currentFence(current, c)
		if q.Fence != now {
			return serviceFailure(pa.StaleGeneration, "login_profile_fence")
		}
		if now.BindingID == "" && now.Generation == 0 {
			return nil
		}
		return pa.CheckFence(q.Fence, now)
	}
	out, err := s.opts.LoginProfiles.Apply(ctx, q, admit)
	return out, check, err
}

func (s *Service) validateLoginSelection(ctx context.Context, b pa.Bot) error {
	if s.opts.LoginProfiles == nil {
		return serviceFailure(pa.Unsupported, "login_profiles")
	}
	resolved, e := s.opts.Projects.Resolve(ctx, pa.ProjectSelection{MachineID: b.Key.MachineID, ProjectID: b.Project.ProjectID})
	if e != nil {
		return e
	}
	if resolved.Scope != b.Project || !resolved.Current || !resolved.Available {
		return serviceFailure(pa.ScopeDenied, "login_profile_project")
	}
	if profiles, ok := s.opts.LoginProfiles.(interface{ CheckProject(string) error }); ok {
		return profiles.CheckProject(resolved.Directory)
	}
	return serviceFailure(pa.Unsupported, "login_profile_project")
}
