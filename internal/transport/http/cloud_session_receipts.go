package http

import (
	"context"
	"errors"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

// CloudSessionReceipts shares the daemon's durable SQLite handle with the
// Cloud bridge; opening a second store would give admissions a second owner.
func (s *Server) CloudSessionReceipts() *store.Store { return s.store }

// CloudAdmitExecution delegates to the execution feature's fresh terminal
// observation and admission. A build without it fails closed until both land.
func (s *Server) CloudAdmitExecution(ctx context.Context, machine, session, generation string) error {
	gate, ok := any(s).(interface {
		AdmitExecutionTarget(context.Context, string, string, string) error
	})
	if !ok {
		return errors.New("execution admission is unavailable")
	}
	return gate.AdmitExecutionTarget(ctx, machine, session, generation)
}
