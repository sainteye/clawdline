package botnetwork

import (
	"context"

	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
)

// Route discloses only an existing dedicated grant's immutable destination.
// The remote directory still checks current target admission over signed wire.
type Route struct {
	AuthorityID string           `json:"authority_id"`
	Target      wire.BotEndpoint `json:"target"`
}

func (s *Service) Routes(ctx context.Context, source wire.BotEndpoint, resolve func(context.Context, string) (Transport, error)) (Response, error) {
	if err := s.o.Host.Check(ctx, source, true); err != nil {
		return Response{}, err
	}
	grants, err := s.o.Repository.Outgoing(ctx, source)
	if err != nil {
		return Response{}, err
	}
	out := Response{Routes: []Route{}}
	for _, g := range grants {
		h := wire.BotHeader{Source: source, Target: g.Target, PairID: g.Pair.PairID, AuthorityID: g.ID, AuthorityEpoch: g.Epoch}
		if _, e := s.authorize(ctx, h); e == nil {
			if resolve == nil {
				continue
			}
			transport, e := resolve(ctx, g.ID)
			if e != nil {
				continue
			}
			result, e := s.Call(ctx, Call{RequestID: Digest([]byte("route-directory-v1\n" + g.ID)), Kind: "directory", AuthorityID: g.ID, Source: source, Target: g.Target}, transport)
			if e != nil {
				continue
			}
			for _, entry := range result.Bots {
				out.Routes = append(out.Routes, Route{AuthorityID: g.ID, Target: entry.Endpoint})
			}
		}
	}
	if err = s.o.Host.Check(ctx, source, false); err != nil {
		return Response{}, err
	}
	return out, nil
}
