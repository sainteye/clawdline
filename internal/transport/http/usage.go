package http

import (
	"context"
	"log"
	"os"
	"sync"

	"github.com/sainteye/clawdline/internal/app"
)

var usageByServer sync.Map // *Server -> *app.UsageLedger

// usageLedger is this server's token ledger (docs/token-ledger.md): the
// reader StartUsage runs, and what a usage route will answer from.
func (s *Server) usageLedger() *app.UsageLedger {
	if u, ok := usageByServer.Load(s); ok {
		return u.(*app.UsageLedger)
	}
	home, _ := os.UserHomeDir()
	got, _ := usageByServer.LoadOrStore(s, app.NewUsageLedger(s.store, home))
	return got.(*app.UsageLedger)
}

// StartUsage runs the token ledger's reading passes. Without a home
// directory there is nothing to read, and the daemon says so and goes on.
func (s *Server) StartUsage(ctx context.Context) {
	u := s.usageLedger()
	if u.Home == "" {
		log.Printf("usage: no home directory; the token ledger is not read")
		return
	}
	go u.Run(ctx)
}
