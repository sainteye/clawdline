//go:build windows

package process

import (
	"context"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/session"
)

type PS struct{}

func New() *PS { return &PS{} }

// Scan is not implemented on Windows yet.
//
// It answers with an incomplete reading rather than an empty one. An empty
// inventory would claim that nothing is running, which is a statement this
// adapter is in no position to make; an incomplete one says only that it could
// not look, which is true and is what a reader needs to know.
func (p *PS) Scan(ctx context.Context) (session.Inventory, error) {
	return session.Inventory{
		ObservedAt: time.Now(),
		Provenance: "ps",
		Complete:   false,
		Notes:      []string{"the Windows process scan is not implemented yet"},
	}, nil
}
