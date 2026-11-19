package projectassistant

import (
	"context"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type backgroundChannelKey struct{}

type backgroundOwnership interface {
	BackgroundOwner(context.Context, pa.BotKey) (pa.BotKey, error)
}

// Only conversation operations can resolve a private worker. A worker inherits
// the owner's current human access, never authority from its identifier.
func (s *Service) backgroundChannel(ctx context.Context, key pa.BotKey) (context.Context, bool, error) {
	repo, ok := s.opts.Repository.(backgroundOwnership)
	if !ok {
		return ctx, false, nil
	}
	owner, err := repo.BackgroundOwner(ctx, key)
	if err != nil || owner.BotID == "" {
		return ctx, false, err
	}
	return context.WithValue(ctx, backgroundChannelKey{}, key), true, nil
}
