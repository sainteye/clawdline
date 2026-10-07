package updater

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/release"
	"github.com/sainteye/clawdline/internal/contract"
)

// Daemon is the running daemon's side of a release install: the release
// check, GET /v1/update's answer, and starting an update.
type Daemon struct {
	Env     Env
	Exe     string
	Checker *Checker
	// AutoApply reads the per-machine setting; Busy says whether a session
	// this daemon started is working (or cannot be told). Either may be nil.
	AutoApply func() bool
	Busy      func(context.Context) bool
	Logf      func(format string, args ...any)
	// RetryEvery replaces AutoApplyRetrySecondsLimit (tests).
	RetryEvery time.Duration

	// wg is the background Execute, so a test or a shutdown can wait for it.
	wg sync.WaitGroup
	// waiting is set while an auto-apply that found a busy session looks
	// again every RetryEvery.
	waiting atomic.Bool
}

// NewDaemon is the daemon's updater for the release exe runs from.
func NewDaemon(env Env, exe string) *Daemon {
	from, _ := Running(env.Layout, exe)
	return &Daemon{Env: env, Exe: exe, Checker: &Checker{Env: env, Channel: ChannelFor(from)}}
}

func (d *Daemon) logf(format string, args ...any) {
	if d.Logf != nil {
		d.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// Status is GET /v1/update for a release install, from what is held and
// what is on disk; it never touches the network.
func (d *Daemon) Status() contract.UpdateStatus {
	from, _ := Running(d.Env.Layout, d.Exe)
	commit, at := buildOf(d.Env.Layout, from)
	out := contract.UpdateStatus{
		InstallKind: contract.UpdateInstallKindRelease,
		Channel:     d.Checker.Channel,
		Running:     contract.BuildStamp{Stamp: commit, CommittedAt: at, Version: from},
	}
	last := d.Checker.Last()
	if last.Base != "" {
		out.SourceURL = last.Base + "/manifest.json"
	}
	if !last.CheckedAt.IsZero() {
		out.CheckedAt = last.CheckedAt.Format(time.RFC3339)
	}
	if last.Err != nil {
		out.Error = last.Err.Error()
	}
	if d.AutoApply != nil {
		out.AutoApply = d.AutoApply()
	}
	if a, err := d.Env.ReadApply(); err == nil {
		out.Apply = &a
	} else {
		// Unknown is not idle: the state file is there and unreadable.
		out.Apply = &contract.UpdateApply{State: contract.UpdateApplyStateFailed,
			Error: &contract.UpdateApplyError{Code: CodeStateUnreadable, Detail: err.Error()}}
	}
	if last.Latest == nil {
		out.State = contract.UpdateStateUnknown
		switch e, ok := last.Err.(*Error); {
		case ok && e.Code == CodeNoReleasePublished:
			out.Reason = CodeNoReleasePublished
		case last.Err != nil:
			out.Reason = "the release manifest could not be read: " + last.Err.Error()
		default:
			out.Reason = "the release manifest has not been read yet"
		}
		return out
	}
	m := last.Latest
	out.Latest = contract.BuildStamp{Stamp: m.Commit, CommittedAt: m.CommittedAt, Version: m.Version, NotesURL: m.NotesURL}
	running, err := release.ParseVersion(from)
	if err != nil {
		out.State, out.Reason = contract.UpdateStateUnknown, "the running release's name is not a version: "+from
		return out
	}
	latest, _ := release.ParseVersion(m.Version)
	switch release.Compare(latest, running) {
	case 1:
		out.State = contract.UpdateStateUpdateAvailable
	case 0:
		out.State = contract.UpdateStateCurrent
	default:
		out.State = contract.UpdateStateAhead
	}
	return out
}

// Start begins an update: the refusals answer at once; the download and
// everything after it run in the background, recorded in status.json. A
// second Start while one runs is refused update_in_progress by the lock.
func (d *Daemon) Start(ctx context.Context, req Request) (*Plan, error) {
	plan, err := d.Env.Begin(ctx, d.Exe, req)
	if err != nil {
		return nil, err
	}
	// Recorded before the answer, so the caller's first read already names
	// the release it follows; Execute records it again as it starts.
	_ = d.Env.record(contract.UpdateApplyStateDownloading, plan.From, plan.Manifest.Version, nil, "")
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		// Not the request's context: the answer has gone, and the update
		// carries its own bounds (DownloadTimeoutSecondsLimit and the rest).
		if err := d.Env.Execute(context.Background(), plan); err != nil {
			d.logf("update %s → %s stopped: %v", plan.From, plan.Manifest.Version, err)
			return
		}
		d.logf("update %s → %s handed to the supervisor", plan.From, plan.Manifest.Version)
	}()
	return plan, nil
}

// Wait waits for an update this daemon started to reach its handoff.
func (d *Daemon) Wait() { d.wg.Wait() }

// Run checks for releases until ctx ends, and after each check installs a
// newer stable release by itself when the setting allows it and no session
// is busy. A busy machine waits for the next check. Beside it, an app bundle
// left staged is swapped in once the app quits (SettleApp).
func (d *Daemon) Run(ctx context.Context) {
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.settleApp(ctx)
	}()
	d.Checker.Run(ctx, d.maybeAutoApply)
}

// settleApp polls every AppSwapPollSecondsLimit until ctx ends; each poll
// reads status.json and does nothing more unless an app bundle is staged.
func (d *Daemon) settleApp(ctx context.Context) {
	t := time.NewTicker(AppSwapPollSecondsLimit * time.Second)
	defer t.Stop()
	for {
		if swapped, err := d.Env.SettleApp(ctx); err != nil {
			d.logf("staged app not swapped in: %v", err)
		} else if swapped {
			d.logf("the staged app replaced the installed one")
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (d *Daemon) maybeAutoApply(ctx context.Context, c Check) {
	if !d.tryAutoApply(ctx, c, true) || !d.waiting.CompareAndSwap(false, true) {
		return
	}
	// The next check is hours away. A busy session is waited for here
	// instead, looking again until it is idle or nothing is left to apply.
	go func() {
		defer d.waiting.Store(false)
		every := d.RetryEvery
		if every <= 0 {
			every = AutoApplyRetrySecondsLimit * time.Second
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(every):
			}
			if !d.tryAutoApply(ctx, d.Checker.Last(), false) {
				return
			}
		}
	}()
}

// tryAutoApply starts the update auto-apply allows, if any. It answers true
// only when the update is held back by a busy session, which is logged when
// say is set.
func (d *Daemon) tryAutoApply(ctx context.Context, c Check, say bool) bool {
	if d.AutoApply == nil || !d.AutoApply() || c.Latest == nil {
		return false
	}
	v, err := release.ParseVersion(c.Latest.Version)
	if err != nil || v.Pre != "" {
		return false // auto-apply takes stable releases only
	}
	if d.Status().State != contract.UpdateStateUpdateAvailable {
		return false
	}
	if failed, err := d.Env.HasFailed(c.Latest.Version); err != nil || failed {
		return false
	}
	if d.Busy != nil && d.Busy(ctx) {
		if say {
			d.logf("update %s is waiting: a session is busy", c.Latest.Version)
		}
		return true
	}
	if _, err := d.Start(ctx, Request{Version: c.Latest.Version, Auto: true}); err != nil {
		d.logf("auto-update to %s not started: %v", c.Latest.Version, err)
	}
	return false
}
