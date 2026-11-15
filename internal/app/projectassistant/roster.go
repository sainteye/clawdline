package projectassistant

import (
	"context"
	"sync"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// RosterWakeups is implemented by the same repository instance used by every
// production writer. It carries no Bot content and is never durable authority.
type RosterWakeups interface {
	SubscribeRoster(string) (<-chan struct{}, func() bool, func(), error)
}

type RosterIdentity struct {
	Key            pa.BotKey
	Project        pa.ProjectScope
	ConversationID string
	Generation     int64
	BindingID      string
	Removed        bool
}

type RosterQuery struct {
	Request              pa.RosterRequest
	AuthorizationBinding string
	ReaderID             string
	ByteLimit            int
}

type RosterPage struct {
	Frame      pa.RosterFrame
	Identities []RosterIdentity
	More       bool
}

// The read adapter borrows the existing query-only pool and signing epoch.
// Each call ends its SQLite transaction before returning facts to a transport.
type RosterReadViews interface {
	ReadRoster(context.Context, RosterQuery) (RosterPage, error)
	ValidateRoster(context.Context, []RosterIdentity) error
}

type RosterResult struct {
	Page      RosterPage
	check     func(context.Context) error
	authority func(context.Context) error
}

func (r RosterResult) RevalidateAuthority(ctx context.Context) error {
	if r.authority == nil {
		return serviceFailure(pa.ScopeDenied, "roster_publication")
	}
	return r.authority(ctx)
}

func rosterBinding(a Authority) string {
	// HasData is an availability hint, not an authority change while enabled.
	// The first Bot created on an empty machine must reach its live roster.
	if a.Feature.Enabled {
		a.Feature.HasData = false
	}
	return readViewBinding(a)
}

func (r RosterResult) Revalidate(ctx context.Context) error {
	if r.check == nil {
		return serviceFailure(pa.ScopeDenied, "roster_publication")
	}
	return r.check(ctx)
}

// RosterSubscription owns the admission slot until Close. Subscribe happens
// before the first snapshot read; a commit in that gap cannot be lost.
type RosterSubscription struct {
	Wakeups <-chan struct{}
	Lagged  func() bool
	close   func()
	once    sync.Once
	read    func(context.Context, string) (RosterResult, error)
	check   func(context.Context) error
}

func (r *RosterSubscription) Close()                               { r.once.Do(r.close) }
func (r *RosterSubscription) Revalidate(ctx context.Context) error { return r.check(ctx) }
func (r *RosterSubscription) Read(ctx context.Context, cursor string) (RosterResult, error) {
	return r.read(ctx, cursor)
}

func (s *Service) SubscribeRoster(ctx context.Context, request pa.RosterRequest) (*RosterSubscription, error) {
	key := pa.BotKey{MachineID: request.MachineID}
	a, err := s.readViewAuthority(ctx, key)
	if err != nil {
		return nil, err
	}
	wakeups, ok := s.opts.Repository.(RosterWakeups)
	views, vok := s.opts.ReadViews.(RosterReadViews)
	if !ok || !vok || s.opts.ReadViewByteLimit <= 0 {
		return nil, serviceFailure(pa.Unsupported, "roster_feed")
	}
	binding := rosterBinding(a)
	check := func(ctx context.Context) error {
		current, err := s.readViewAuthority(ctx, key)
		if err != nil {
			return err
		}
		if rosterBinding(current) != binding {
			return serviceFailure(pa.Revoked, "roster_authority_changed")
		}
		return ctx.Err()
	}
	project := func(ctx context.Context, scope pa.ProjectScope) error {
		if scope.MachineID != request.MachineID || request.ProjectID != "" && request.ProjectID != scope.ProjectID {
			return serviceFailure(pa.ScopeDenied, "roster_project")
		}
		resolved, err := s.opts.Projects.Resolve(ctx, pa.ProjectSelection{MachineID: scope.MachineID, ProjectID: scope.ProjectID})
		if err != nil {
			return err
		}
		if !resolved.Current || !resolved.Available || resolved.Scope != scope {
			return serviceFailure(pa.ScopeDenied, "roster_project")
		}
		return nil
	}
	// Even an empty Project-scoped feed requires a currently readable Project.
	if request.ProjectID != "" {
		p, err := s.opts.Projects.Resolve(ctx, pa.ProjectSelection{MachineID: request.MachineID, ProjectID: request.ProjectID})
		if err != nil {
			return nil, err
		}
		if err = project(ctx, p.Scope); err != nil {
			return nil, err
		}
	}
	ch, lagged, close, err := wakeups.SubscribeRoster(request.MachineID)
	if err != nil {
		return nil, err
	}
	sub := &RosterSubscription{Wakeups: ch, Lagged: lagged, close: close, check: check}
	sub.read = func(ctx context.Context, cursor string) (RosterResult, error) {
		if err := check(ctx); err != nil {
			return RosterResult{}, err
		}
		q := request
		q.Cursor = cursor
		page, err := views.ReadRoster(ctx, RosterQuery{Request: q, AuthorizationBinding: binding, ReaderID: a.ReaderID, ByteLimit: s.opts.ReadViewByteLimit})
		if err != nil {
			return RosterResult{}, err
		}
		for i := range page.Frame.Changes {
			if row := page.Frame.Changes[i].Summary; row != nil {
				row.Source = a.Source
			}
		}
		authority := func(ctx context.Context) error {
			if err := check(ctx); err != nil {
				return err
			}
			for _, id := range page.Identities {
				current, err := s.readViewAuthority(ctx, id.Key)
				if err != nil {
					return err
				}
				if rosterBinding(current) != binding {
					return serviceFailure(pa.Revoked, "roster_row_authority")
				}
				if err = project(ctx, id.Project); err != nil {
					return err
				}
			}
			return nil
		}
		fence := func(ctx context.Context) error {
			if err := authority(ctx); err != nil {
				return err
			}
			return views.ValidateRoster(ctx, page.Identities)
		}
		if err = fence(ctx); err != nil {
			return RosterResult{}, err
		}
		return RosterResult{Page: page, check: fence, authority: authority}, nil
	}
	return sub, nil
}
