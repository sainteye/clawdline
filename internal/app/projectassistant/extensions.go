package projectassistant

import (
	"context"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/squad"
)

// Definitions and launch URLs belong to the source, never the editable Bot.
type NativeConnector struct {
	Ref        pa.ExtensionRef
	Name, URL  string
	Executable string
	Arguments  []string
	// Credential is a source-owned late binding, not native configuration.
	Credential                      func(context.Context) ([]byte, error)
	Quarantine                      func(context.Context) error
	ReleaseCredential               func(context.Context) error
	CredentialHeader, CredentialEnv string
}
type NativeExtensions struct {
	Skills     string
	Connectors []NativeConnector
	Folders    []squad.Skill
	// State is trusted application storage, never a client-selected path.
	State string
}
type ExtensionInventory interface {
	Catalog(context.Context, pa.Bot, string) (pa.ExtensionCatalog, error)
	Resolve(context.Context, pa.Bot, string) (NativeExtensions, error)
}
type ExtensionGuard interface {
	CheckExtension(context.Context, pa.ExtensionRef) error
}

type ConnectorCredentials interface {
	Apply(context.Context, pa.Bot, pa.ConnectorCredentialAction, []byte, bool, func(context.Context) error) (pa.ConnectorCredential, error)
}

func (s *Service) ConnectorCredential(ctx context.Context, q pa.ConnectorCredentialAction, value []byte, secret bool) (pa.ConnectorCredential, func(context.Context) error, error) {
	b, a, _, epoch, err := s.memoryOwner(ctx, MemoryReadRequest{Key: q.Command.Key, ConversationID: q.Fence.ConversationID})
	if err != nil {
		return pa.ConnectorCredential{}, nil, err
	}
	if _, err = s.authorizeEffect(ctx, b.Key, true, a); err != nil {
		return pa.ConnectorCredential{}, nil, err
	}
	store, ok := s.opts.Extensions.(ConnectorCredentials)
	if !ok {
		return pa.ConnectorCredential{}, nil, serviceFailure(pa.Unsupported, "connector_credentials")
	}
	q.Authority = epoch
	payload := q
	payload.Command = pa.Command{}
	q.Command, err = canonicalCommand(q.Command, pa.ConnectorCredentialOperation, a.PrincipalID, payload)
	if err != nil {
		return pa.ConnectorCredential{}, nil, err
	}
	check := func(ctx context.Context) error {
		current, _, _, now, e := s.memoryOwner(ctx, MemoryReadRequest{Key: b.Key, ConversationID: b.ConversationID})
		if e != nil {
			return e
		}
		if epoch != now || current.Project != b.Project || current.Agent != b.Agent {
			return serviceFailure(pa.Revoked, "connector_source")
		}
		if _, e = s.authorizeEffect(ctx, b.Key, true, a); e != nil {
			return e
		}
		selected := false
		for _, ref := range current.Extensions.Connectors {
			if ref == q.Ref {
				selected = true
			}
		}
		if !selected {
			return serviceFailure(pa.Revoked, "connector_selection")
		}
		// Recheck the trusted definition without fetching a secret or starting
		// a process. The catalog is also available for missing credentials.
		catalog, e := s.opts.Extensions.Catalog(ctx, current, "")
		if e != nil {
			return e
		}
		for _, offer := range catalog.Items {
			if offer.ID == q.Ref.ID && offer.Fingerprint == q.Ref.Fingerprint {
				return nil
			}
		}
		return serviceFailure(pa.Revoked, "connector_definition")
	}
	admit := func(ctx context.Context) error {
		if e := check(ctx); e != nil {
			return e
		}
		current, e := s.opts.Repository.Bot(ctx, b.Key)
		if e != nil {
			return e
		}
		// Bot.Version moves with chat activity; the fence, authority epoch
		// and source checks are what this action depends on.
		conversation, e := s.opts.Repository.Conversation(ctx, b.Key, b.ConversationID)
		if e != nil {
			return e
		}
		if q.Fence != currentFence(current, conversation) {
			return serviceFailure(pa.StaleGeneration, "connector_fence")
		}
		return nil
	}
	out, err := store.Apply(ctx, b, q, value, secret, admit)
	return out, check, err
}

func (s *Service) Extensions(ctx context.Context, q MemoryReadRequest) (pa.ExtensionCatalog, func(context.Context) error, error) {
	b, _, resolved, epoch, e := s.memoryOwner(ctx, q)
	if e != nil {
		return pa.ExtensionCatalog{}, nil, e
	}
	if s.opts.Extensions == nil {
		return pa.ExtensionCatalog{}, nil, serviceFailure(pa.Unsupported, "bot_extensions")
	}
	catalog, e := s.opts.Extensions.Catalog(ctx, b, resolved.Directory)
	check := func(ctx context.Context) error {
		_, _, _, now, e := s.memoryOwner(ctx, q)
		if e != nil {
			return e
		}
		if epoch != now {
			return serviceFailure(pa.Revoked, "extensions_source")
		}
		return ctx.Err()
	}
	if e == nil {
		e = check(ctx)
	}
	return catalog, check, e
}
func (s *Service) validateExtensions(ctx context.Context, b pa.Bot) error {
	if len(b.Extensions.Skills)+len(b.Extensions.Connectors) == 0 {
		return nil
	}
	if s.opts.Extensions == nil {
		return serviceFailure(pa.Unsupported, "bot_extensions")
	}
	resolved, e := s.opts.Projects.Resolve(ctx, pa.ProjectSelection{MachineID: b.Key.MachineID, ProjectID: b.Project.ProjectID})
	if e != nil {
		return e
	}
	if resolved.Scope != b.Project || !resolved.Current || !resolved.Available {
		return serviceFailure(pa.ScopeDenied, "extensions_project")
	}
	_, e = s.opts.Extensions.Resolve(ctx, b, resolved.Directory)
	return e
}
