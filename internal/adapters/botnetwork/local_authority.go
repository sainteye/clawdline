package botnetwork

import (
	"context"

	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// CheckBotAuthority is the controlled local-account authority used by the two
// independent source fixtures. It reads actual durable consent, not an allow
// callback. It does not contact Cloud. Production composition must supply the
// separate live account/pair authorizer required by the network contract.
func (s *Store) CheckBotAuthority(ctx context.Context, h wire.BotHeader) error {
	g, err := s.Grant(ctx, h.AuthorityID)
	if err != nil {
		return err
	}
	if g.Revoked || g.Epoch != h.AuthorityEpoch {
		return refuse(pa.Revoked, "network_local_consent")
	}
	if g.AccountID != h.Source.AccountID || g.Pair.PairID != h.PairID {
		return refuse(pa.ScopeDenied, "network_local_consent")
	}
	a, b := g.Source, g.Target
	// Native generation advances independently; conversation identity cannot move.
	a.Generation = h.Source.Generation
	b.Generation = h.Target.Generation
	if a != h.Source || b != h.Target {
		return refuse(pa.ScopeDenied, "network_local_consent")
	}
	return nil
}
