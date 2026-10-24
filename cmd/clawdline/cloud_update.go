package main

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/release/updater"
)

// cloudUpdateGate serializes initial activation, settings reload and shutdown.
// The tick is the updater's existing local settle poll, not a new bound.
type cloudUpdateGate struct {
	mu      sync.Mutex
	ctx     context.Context
	ready   func() bool
	open    func() error
	started bool
}

func (g *cloudUpdateGate) reload() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.ctx.Err(); err != nil {
		return err
	}
	if !g.ready() {
		return errors.New("The Cloud connection is not available in this daemon.")
	}
	if err := g.ctx.Err(); err != nil {
		return err
	}
	err := g.open()
	g.started = true
	return err
}

func (g *cloudUpdateGate) resume(ticks <-chan time.Time) {
	g.mu.Lock()
	started := g.started
	g.mu.Unlock()
	if started {
		return
	}
	for {
		select {
		case <-g.ctx.Done():
			return
		case <-ticks:
			_ = g.reload()
			g.mu.Lock()
			started := g.started
			g.mu.Unlock()
			if started {
				return
			}
		}
	}
}

func cloudAdmission(dir string) func() bool {
	env, err := updater.DefaultEnv(dir)
	if err != nil {
		return func() bool { return false }
	}
	exe, err := os.Executable()
	if err != nil {
		return func() bool { return false }
	}
	return env.CloudAdmission(exe, updater.Served{Version: version, Commit: buildRevision()})
}
