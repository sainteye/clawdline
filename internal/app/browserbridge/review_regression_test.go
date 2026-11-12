package browserbridge

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestStoppedHostPollWaitsAndPersonInputWakesIt(t *testing.T) {
	b, ctx := connected(t)
	b.pollWait = time.Second
	b.owner[5], b.current["c1"] = "c1", 5
	if err := b.SetStopped(true); err != nil {
		t.Fatal(err)
	}
	if err := b.SetPersonControl(5, true); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	waitCtx, cancel := context.WithTimeout(ctx, 40*time.Millisecond)
	defer cancel()
	if commands := b.Poll(waitCtx, HostInfo{Version: "test"}); len(commands) != 0 {
		t.Fatal(commands)
	}
	if time.Since(started) < 30*time.Millisecond {
		t.Fatal("stopped empty poll returned without waiting")
	}
	poll := make(chan []Command, 1)
	go func() { poll <- b.Poll(ctx, HostInfo{Version: "test"}) }()
	done := make(chan error, 1)
	go func() { _, err := b.PersonDo(ctx, "key", 5, json.RawMessage(`{"key":"Enter"}`)); done <- err }()
	select {
	case commands := <-poll:
		if len(commands) != 1 || !commands[0].Person {
			t.Fatal(commands)
		}
		b.Deliver([]Result{{ID: commands[0].ID, Tab: 5, OK: true}})
	case <-time.After(time.Second):
		t.Fatal("person input did not wake stopped poll")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSetupProbeCannotEnableOrConsumeQueuedInput(t *testing.T) {
	b, ctx := connected(t)
	b.host = HostInfo{}
	b.pollWait = time.Millisecond
	b.SetEnabled(false)
	if !b.ProbeSetupHost(ctx, HostInfo{Mode: "isolated", Version: "setup"}) || !b.SetupConnected() {
		t.Fatal("dedicated readiness not observable")
	}
	if b.AcceptsHost(HostInfo{Mode: "isolated"}) || b.Status().Connected {
		t.Fatal("readiness granted command access")
	}
	if b.ProbeSetupHost(ctx, HostInfo{Mode: "user_chrome"}) {
		t.Fatal("daily Chrome used dedicated setup probe")
	}
	b.now = func() time.Time { return time.Now().Add((HostStaleSeconds + 1) * time.Second) }
	if b.SetupConnected() {
		t.Fatal("stale setup host remained ready")
	}
	b.now = time.Now
	b.SetEnabled(true)
	if b.SetupConnected() {
		t.Fatal("setup probe replaced operational host handshake")
	}
	if b.ProbeSetupHost(ctx, HostInfo{Mode: "isolated"}) {
		t.Fatal("enabled channel used readiness-only path")
	}
}
