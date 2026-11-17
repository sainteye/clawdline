package projectassistant

import (
	"context"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// NetworkSettingsStore is a human management port, never a native Bot tool.
type NetworkSettingsStore interface {
	NetworkSettings(context.Context, pa.NetworkScope) (pa.NetworkSettings, error)
	SaveNetworkSettings(context.Context, pa.NetworkSettingsWrite, pa.NetworkCapabilities) (pa.NetworkSettings, error)
}

type NetworkSettingsRequest struct {
	Command          pa.Command
	Fence            pa.Fence
	ExpectedVersion  int64
	ExpectedRevision int64
	Patch            pa.NetworkSettingsPatch
}

func unavailableNetworkCapabilities() pa.NetworkCapabilities {
	c := pa.NetworkCapability{State: pa.NetworkUnavailable, Reason: "network_not_configured"}
	return pa.NetworkCapabilities{Peer: c, Assistants: c}
}

func (s *Service) networkAccount(ctx context.Context, machine string) (string, error) {
	if s.opts.NetworkAccount != nil {
		return s.opts.NetworkAccount(ctx)
	}
	return "unconfigured:" + machine, nil
}

func (s *Service) networkCapabilities(ctx context.Context, account, machine string) pa.NetworkCapabilities {
	if s.opts.NetworkCapabilities == nil {
		return unavailableNetworkCapabilities()
	}
	return s.opts.NetworkCapabilities(ctx, account, machine)
}

// NetworkSettings derives account and Project from current authenticated source
// authority. The wire cannot choose an account or create network authorization.
func (s *Service) NetworkSettings(ctx context.Context, q MemoryReadRequest) (pa.NetworkSettingsView, func(context.Context) error, error) {
	b, a, _, _, err := s.memoryOwner(ctx, q)
	if err != nil {
		return pa.NetworkSettingsView{}, nil, err
	}
	p := s.opts.NetworkSettings
	if p == nil {
		return pa.NetworkSettingsView{}, nil, serviceFailure(pa.Unsupported, "network_settings")
	}
	account, err := s.networkAccount(ctx, b.Key.MachineID)
	if err != nil || account == "" {
		if err == nil {
			err = serviceFailure(pa.Unsupported, "network_account")
		}
		return pa.NetworkSettingsView{}, nil, err
	}
	scope := pa.NetworkScope{AccountID: account, Key: b.Key, Project: b.Project}
	settings, err := p.NetworkSettings(ctx, scope)
	if err != nil {
		return pa.NetworkSettingsView{}, nil, err
	}
	c, err := s.opts.Repository.Conversation(ctx, b.Key, b.ConversationID)
	if err != nil {
		return pa.NetworkSettingsView{}, nil, err
	}
	out := pa.NetworkSettingsView{Fence: pa.Fence{Key: b.Key, Project: b.Project, ConversationID: c.ID, BindingID: c.BindingID, Generation: c.Generation}, BotVersion: b.Version, Settings: settings, Capabilities: s.networkCapabilities(ctx, account, b.Key.MachineID)}
	check := func(ctx context.Context) error {
		_, e := s.authorizeEffect(ctx, b.Key, false, a)
		if e != nil {
			return e
		}
		now, e := s.opts.Repository.Bot(ctx, b.Key)
		if e != nil {
			return e
		}
		if now.Project != b.Project || now.ConversationID != b.ConversationID {
			return serviceFailure(pa.Revoked, "network_scope")
		}
		accountNow, e := s.networkAccount(ctx, b.Key.MachineID)
		if e != nil {
			return e
		}
		if accountNow != scope.AccountID || s.networkCapabilities(ctx, accountNow, b.Key.MachineID) != out.Capabilities {
			return serviceFailure(pa.Revoked, "network_source_changed")
		}
		settingsNow, e := p.NetworkSettings(ctx, scope)
		if e != nil {
			return e
		}
		if settingsNow != out.Settings {
			return serviceFailure(pa.StaleVersion, "network_settings_changed")
		}
		return nil
	}
	return out, check, nil
}

func (s *Service) SaveNetworkSettings(ctx context.Context, q NetworkSettingsRequest) (pa.NetworkSettingsView, func(context.Context) error, error) {
	a, err := s.authorize(ctx, q.Command.Key, true)
	if err != nil {
		return pa.NetworkSettingsView{}, nil, err
	}
	if q.Command.Operation != pa.NetworkSettingsOperation || q.Fence.Key != q.Command.Key {
		return pa.NetworkSettingsView{}, nil, serviceFailure(pa.Invalid, "network_command")
	}
	b, err := s.opts.Repository.Bot(ctx, q.Command.Key)
	if err != nil {
		return pa.NetworkSettingsView{}, nil, err
	}
	if b.Project != q.Fence.Project || b.ConversationID != q.Fence.ConversationID {
		return pa.NetworkSettingsView{}, nil, serviceFailure(pa.ScopeDenied, "network_scope")
	}
	if _, err = s.authorizeEffect(ctx, b.Key, true, a); err != nil {
		return pa.NetworkSettingsView{}, nil, err
	}
	p := s.opts.NetworkSettings
	if p == nil {
		return pa.NetworkSettingsView{}, nil, serviceFailure(pa.Unsupported, "network_settings")
	}
	account, err := s.networkAccount(ctx, b.Key.MachineID)
	if err != nil || account == "" {
		if err == nil {
			err = serviceFailure(pa.Unsupported, "network_account")
		}
		return pa.NetworkSettingsView{}, nil, err
	}
	scope := pa.NetworkScope{AccountID: account, Key: b.Key, Project: b.Project}
	caps := s.networkCapabilities(ctx, account, b.Key.MachineID)
	_, err = p.SaveNetworkSettings(ctx, pa.NetworkSettingsWrite{Scope: scope, Fence: q.Fence, ExpectedBotVersion: q.ExpectedVersion, ExpectedRevision: q.ExpectedRevision, Patch: q.Patch}, caps)
	if err != nil {
		return pa.NetworkSettingsView{}, nil, err
	}
	out, check, err := s.NetworkSettings(ctx, MemoryReadRequest{Key: b.Key, ConversationID: b.ConversationID})
	if err != nil {
		return out, nil, err
	}
	return out, func(ctx context.Context) error {
		if _, e := s.authorizeEffect(ctx, b.Key, true, a); e != nil {
			return e
		}
		return check(ctx)
	}, nil
}
