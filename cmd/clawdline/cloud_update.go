package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/release/updater"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

// cloudUpdateGate serializes initial activation, settings reload and shutdown,
// and keeps retrying a line whose Open failed.
// The tick is the updater's existing local settle poll, not a new bound; the
// retry waits are cloudtransport's LineOpenRetry ladder.
//
// Until 2026-11-24 a failed Open still marked the line started, so one
// transient settings or key read failure kept the phone and app.clawdline.com
// away until settings were saved again or Clawdline restarted. Now only an
// Open that succeeded, or one refused for good (final), ends the retries.
type cloudUpdateGate struct {
	mu    sync.Mutex
	ctx   context.Context
	ready func() bool
	open  func() error
	// final reports an Open failure that retrying cannot change; nil retries
	// every failure.
	final func(error) bool
	logf  func(format string, args ...any)

	started  bool
	failures int
	retryAt  time.Time
	lastLog  string
}

// cloudLineNotUp is an Open that produced a line the status route can explain
// but nothing can run. reload answers it as nil, as it always has, so turning
// the switch on before signing in is not refused; the gate retries it.
type cloudLineNotUp struct{ err error }

func (e cloudLineNotUp) Error() string { return e.err.Error() }
func (e cloudLineNotUp) Unwrap() error { return e.err }

func (g *cloudUpdateGate) reload() error { return g.attempt(time.Now()) }

func (g *cloudUpdateGate) attempt(now time.Time) error {
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
	var notUp cloudLineNotUp
	quiet := errors.As(err, &notUp)
	switch {
	case err == nil:
		if g.failures > 0 {
			g.note("", fmt.Sprintf("cloud: the line opened after %d failed attempt(s)", g.failures))
		}
		g.started, g.failures, g.retryAt = true, 0, time.Time{}
	case g.final != nil && g.final(err):
		g.note("final: "+err.Error(), "cloud: the line is off and is not retried until settings change or somebody signs in again: "+err.Error())
		g.started, g.failures, g.retryAt = true, 0, time.Time{}
	default:
		g.started = false
		g.failures++
		wait := cloudtransport.OpenRetryDelay(g.failures)
		g.retryAt = now.Add(wait)
		g.note(err.Error(), fmt.Sprintf("cloud: the line could not be opened and is tried again in %s, and at least once every %ds until it opens: %v",
			wait, cloudtransport.LineOpenRetryMaxSecondsLimit, err))
	}
	if quiet {
		return nil
	}
	return err
}

// note logs line unless the previous note had the same key, so a line failing
// the same way for hours leaves one line rather than one per retry.
func (g *cloudUpdateGate) note(key, line string) {
	if key == g.lastLog {
		return
	}
	g.lastLog = key
	if g.logf != nil {
		g.logf("%s", line)
	}
}

// resume runs until shutdown. It opens a line that was not admitted yet, and
// retries a failed Open once its wait has passed.
func (g *cloudUpdateGate) resume(ticks <-chan time.Time) {
	for {
		select {
		case <-g.ctx.Done():
			return
		case now := <-ticks:
			g.mu.Lock()
			due := !g.started && !now.Before(g.retryAt)
			g.mu.Unlock()
			if due {
				_ = g.attempt(now)
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
