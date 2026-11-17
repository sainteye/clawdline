package projectassistant

import (
	"context"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func (s *Service) Configuration(ctx context.Context, q MemoryReadRequest, agent pa.Agent, model string, profiles ...string) (pa.ConfigurationCatalog, func(context.Context) error, error) {
	if err := pa.ValidateRuntimeSettings(pa.RuntimeSettings{Model: model}); err != nil {
		return pa.ConfigurationCatalog{}, nil, err
	}
	b, authority, resolved, epoch, err := s.memoryOwner(ctx, q)
	if err != nil {
		return pa.ConfigurationCatalog{}, nil, err
	}
	if !authority.Feature.Enabled {
		return pa.ConfigurationCatalog{}, nil, serviceFailure(pa.Disabled, "configuration_feature")
	}
	if agent != "" {
		if agent != pa.ClaudeCode && agent != pa.Codex {
			return pa.ConfigurationCatalog{}, nil, serviceFailure(pa.Unsupported, "agent")
		}
		if b.Agent != agent {
			b.LoginProfile = ""
		}
		b.Agent = agent
	}
	if len(profiles) > 0 {
		if !pa.ValidLoginProfileID(profiles[0]) {
			return pa.ConfigurationCatalog{}, nil, serviceFailure(pa.Invalid, "login_profile")
		}
		b.LoginProfile = profiles[0]
	}
	if b.LoginProfile != "" {
		if err = s.validateLoginSelection(ctx, b); err != nil {
			return pa.ConfigurationCatalog{}, nil, err
		}
	}
	if s.opts.Configuration == nil {
		return pa.ConfigurationCatalog{}, nil, serviceFailure(pa.Unsupported, "configuration")
	}
	// Catalog reads do not apply this Bot’s saved option choices. The human
	// Save path validates them in its own disposable native session.
	b.RuntimeSettings = pa.RuntimeSettings{}
	out, err := s.opts.Configuration(ctx, b, resolved.Directory, model)
	if err != nil {
		return out, nil, err
	}
	if out.Agent != b.Agent {
		return out, nil, serviceFailure(pa.ScopeDenied, "configuration_agent")
	}
	check := func(ctx context.Context) error {
		_, _, _, now, err := s.memoryOwner(ctx, q)
		if err != nil {
			return err
		}
		if now != epoch {
			return serviceFailure(pa.Revoked, "configuration_source")
		}
		return ctx.Err()
	}
	return out, check, check(ctx)
}

func (s *Service) validateConfiguration(ctx context.Context, bot pa.Bot) error {
	if s.opts.Configuration == nil {
		return serviceFailure(pa.Unsupported, "configuration")
	}
	resolved, err := s.opts.Projects.Resolve(ctx, pa.ProjectSelection{MachineID: bot.Key.MachineID, ProjectID: bot.Project.ProjectID})
	if err != nil {
		return err
	}
	if err = pa.ValidateTarget(pa.ProjectSelection{MachineID: bot.Key.MachineID, ProjectID: bot.Project.ProjectID}, resolved); err != nil {
		return err
	}
	if resolved.Scope != bot.Project {
		return serviceFailure(pa.ScopeDenied, "configuration_project")
	}
	actual, err := s.opts.Configuration(ctx, bot, resolved.Directory, bot.RuntimeSettings.Model)
	if err != nil {
		return err
	}
	if actual.Agent != bot.Agent {
		return serviceFailure(pa.ScopeDenied, "configuration_agent")
	}
	for id, value := range bot.RuntimeSettings.Options {
		found := false
		for _, o := range actual.Options {
			if o.ID == id && o.Category != "model" {
				for _, v := range o.Choices {
					found = found || v.Value == value
				}
			}
		}
		if !found {
			return serviceFailure(pa.Unsupported, "model_option")
		}
	}
	return nil
}
