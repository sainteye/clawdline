package projectassistant

import (
	"context"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Definitions and launch URLs belong to the source, never the editable Bot.
type NativeConnector struct {
	Ref       pa.ExtensionRef
	Name, URL string
}
type NativeExtensions struct {
	Skills     string
	Connectors []NativeConnector
}
type ExtensionInventory interface {
	Catalog(context.Context, pa.Bot, string) (pa.ExtensionCatalog, error)
	Resolve(context.Context, pa.Bot, string) (NativeExtensions, error)
}
type ExtensionGuard interface {
	CheckExtension(context.Context, pa.ExtensionRef) error
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
