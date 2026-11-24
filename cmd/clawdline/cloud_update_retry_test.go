package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

// TestCloudUpdateGateRetriesAFailedOpenWithoutARestart injects one failed
// Cloud line Open (a transient settings read error) and then lets it succeed.
// The line must come up from the background retry alone, with no settings
// save and no restart.
func TestCloudUpdateGateRetriesAFailedOpenWithoutARestart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var opens atomic.Int32
	opened := make(chan struct{}, 4)
	g := &cloudUpdateGate{ctx: ctx, ready: func() bool { return true }, open: func() error {
		if opens.Add(1) == 1 {
			return errors.New("injected: read settings: input/output error")
		}
		opened <- struct{}{}
		return nil
	}}
	if err := g.reload(); err == nil {
		t.Fatal("the injected Open failure was not returned")
	}
	ticks := make(chan time.Time)
	done := make(chan struct{})
	go func() { g.resume(ticks); close(done) }()
	select {
	case ticks <- time.Now().Add(time.Hour):
	case <-done:
		t.Fatal("the gate gave up after one failed Open: nothing retries until settings are saved or Clawdline restarts")
	case <-time.After(2 * time.Second):
		t.Fatal("the gate took no retry tick after a failed Open")
	}
	select {
	case <-opened:
	case <-time.After(2 * time.Second):
		t.Fatalf("the retry did not open the line (%d opens)", opens.Load())
	}
	cancel()
	<-done
}

// TestCloudUpdateGateWaitsOutItsBackoff checks that a retry is not taken on
// every tick: a tick before the wait has passed opens nothing.
func TestCloudUpdateGateWaitsOutItsBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var opens atomic.Int32
	g := &cloudUpdateGate{ctx: ctx, ready: func() bool { return true }, open: func() error {
		opens.Add(1)
		return errors.New("injected: spool open failed")
	}}
	start := time.Now()
	_ = g.attempt(start)
	ticks := make(chan time.Time)
	done := make(chan struct{})
	go func() { g.resume(ticks); close(done) }()
	ticks <- start.Add(cloudtransport.OpenRetryDelay(1) - time.Second)
	ticks <- start.Add(cloudtransport.OpenRetryDelay(1))
	ticks <- start.Add(cloudtransport.OpenRetryDelay(1) + time.Second) // still inside the second, doubled wait
	cancel()
	<-done
	if got := opens.Load(); got != 2 {
		t.Fatalf("opens = %d, want 2: the first Open and one retry once its wait passed", got)
	}
}

// TestCloudUpdateGateStopsOnAnAuthoritativeRefusal: a line refused for good
// (no identity on this machine) is not retried, and the refusal is not turned
// into an error for the settings route.
func TestCloudUpdateGateStopsOnAnAuthoritativeRefusal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var opens atomic.Int32
	g := &cloudUpdateGate{ctx: ctx, ready: func() bool { return true }, final: cloudtransport.OpenRefusedForGood,
		open: func() error {
			opens.Add(1)
			return cloudLineNotUp{adaptercloud.ErrNoIdentity}
		}}
	if err := g.reload(); err != nil {
		t.Fatalf("a line the status route explains was refused to the settings route: %v", err)
	}
	ticks := make(chan time.Time)
	done := make(chan struct{})
	go func() { g.resume(ticks); close(done) }()
	for i := 1; i <= 3; i++ {
		ticks <- time.Now().Add(time.Duration(i) * time.Hour)
	}
	cancel()
	<-done
	if got := opens.Load(); got != 1 {
		t.Fatalf("opens = %d, want 1: an authoritative refusal was retried", got)
	}
}

// TestALineThatIsNotUpIsRetriedButNotReportedAsAnError: Open's link that is
// on but failed (a key file that cannot be read) returns nil to the caller, as
// before, and is retried.
func TestALineThatIsNotUpIsRetriedButNotReportedAsAnError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var opens atomic.Int32
	g := &cloudUpdateGate{ctx: ctx, ready: func() bool { return true }, final: cloudtransport.OpenRefusedForGood,
		open: func() error {
			if opens.Add(1) == 1 {
				return cloudLineNotUp{errors.New("injected: read device key: input/output error")}
			}
			return nil
		}}
	if err := g.reload(); err != nil {
		t.Fatalf("reload answered %v for a line the status route explains", err)
	}
	ticks := make(chan time.Time)
	done := make(chan struct{})
	go func() { g.resume(ticks); close(done) }()
	ticks <- time.Now().Add(time.Hour)
	ticks <- time.Now().Add(2 * time.Hour)
	cancel()
	<-done
	if got := opens.Load(); got != 2 {
		t.Fatalf("opens = %d, want 2", got)
	}
}
