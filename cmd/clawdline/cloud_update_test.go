package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestCloudUpdateGateResumesAndSharesReloadAdmission(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var ready atomic.Bool
	opened := make(chan struct{}, 1)
	g := &cloudUpdateGate{ctx: ctx, ready: ready.Load, open: func() error { opened <- struct{}{}; return nil }}
	if err := g.reload(); err == nil {
		t.Fatal("pending reload was accepted")
	}
	select {
	case <-opened:
		t.Fatal("pending opened Cloud")
	default:
	}
	ticks := make(chan time.Time)
	done := make(chan struct{})
	go func() { g.resume(ticks); close(done) }()
	ready.Store(true)
	ticks <- time.Time{}
	<-opened
	<-done
	if err := g.reload(); err != nil {
		t.Fatal(err)
	}
	<-opened
	cancel()
	if err := g.reload(); err == nil {
		t.Fatal("shutdown accepted reload")
	}
	select {
	case <-opened:
		t.Fatal("shutdown opened Cloud")
	default:
	}
}

func TestCloudUpdateGateCancellationDuringAdmissionNeverOpens(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	g := &cloudUpdateGate{ctx: ctx, ready: func() bool { cancel(); return true }, open: func() error { t.Fatal("opened after shutdown"); return nil }}
	if err := g.reload(); err == nil {
		t.Fatal("shutdown was not observed")
	}
	done := make(chan struct{})
	go func() { g.resume(make(chan time.Time)); close(done) }()
	<-done
}

func TestCloudUpdateGateConcurrentShutdownWinsAdmission(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	admitting, continueAdmission := make(chan struct{}), make(chan struct{})
	g := &cloudUpdateGate{
		ctx:   ctx,
		ready: func() bool { close(admitting); <-continueAdmission; return true },
		open:  func() error { t.Error("shutdown raced through to Cloud Open"); return nil },
	}
	done := make(chan error, 1)
	go func() { done <- g.reload() }()
	<-admitting
	cancel()
	close(continueAdmission)
	if err := <-done; err == nil {
		t.Fatal("concurrent shutdown was not observed")
	}
}
