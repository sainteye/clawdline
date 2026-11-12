package browserbridge

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDisablingBrowserCancelsQueuedAgentCommands(t *testing.T) {
	b, ctx := connected(t)
	done := make(chan error, 1)
	go func() {
		_, err := b.Do(ctx, "c1", "open", json.RawMessage(`{"url":"https://example.com"}`))
		done <- err
	}()
	deadline := time.Now().Add(time.Second)
	for b.Status().Pending == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if b.Status().Pending != 1 {
		t.Fatal("command never queued")
	}
	b.SetEnabled(false)
	if err := <-done; err != Disabled {
		t.Fatalf("queued command: %v", err)
	}
	if b.Status().Pending != 0 {
		t.Fatal("disabled command stayed queued")
	}
	if _, err := b.Do(ctx, "c1", "tabs", nil); err != Disabled {
		t.Fatalf("new command: %v", err)
	}
	b.SetEnabled(true)
	fakeHost(ctx, b, func(c Command) Result { return Result{ID: c.ID, OK: true, Value: json.RawMessage(`[]`)} })
	if _, err := b.Do(ctx, "c1", "tabs", nil); err != nil {
		t.Fatal(err)
	}
	if err := b.SetStopped(true); err != nil {
		t.Fatal(err)
	}
	b.SetEnabled(false)
	b.SetEnabled(true)
	if _, err := b.Do(ctx, "c1", "tabs", nil); err != Stopped {
		t.Fatalf("permission toggle resumed emergency stop: %v", err)
	}
}

func TestBrowserOffOnCycleDiscardsAnInFlightReply(t *testing.T) {
	b, ctx := connected(t)
	done := make(chan error, 1)
	go func() {
		data, err := b.Do(ctx, "c1", "open", json.RawMessage(`{"url":"https://example.com"}`))
		if len(data) != 0 {
			done <- ExtensionErrorForTest{}
			return
		}
		done <- err
	}()
	var command Command
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if commands := b.Poll(ctx, HostInfo{Version: "test"}); len(commands) != 0 {
			command = commands[0]
			break
		}
	}
	if command.ID == "" {
		t.Fatal("host received no command")
	}
	b.SetEnabled(false)
	b.SetEnabled(true)
	b.Deliver([]Result{{ID: command.ID, OK: true, Tab: 5, Value: json.RawMessage(`{"text":"late page"}`)}})
	if err := <-done; err != Disabled {
		t.Fatalf("late reply: %v", err)
	}
	if len(b.Status().Tabs) != 0 {
		t.Fatal("late reply claimed a tab after permission was revoked")
	}
	rows, _ := b.Records("c1", 10)
	if len(rows) != 1 || rows[0].OK || rows[0].Error != string(Disabled) {
		t.Fatalf("late success recorded: %+v", rows)
	}
}

func TestNativeModeWithdrawsUnsentViewerTextAcrossOffOn(t *testing.T) {
	b, ctx := connected(t)
	b.owner[5] = "c1"
	b.SetPersonControl(5, true)
	done := make(chan error, 1)
	go func() { _, err := b.PersonDo(ctx, "type", 5, json.RawMessage(`{"text":"unsent"}`)); done <- err }()
	deadline := time.Now().Add(time.Second)
	for b.Status().Pending == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if b.Status().Pending != 1 {
		t.Fatal("input never queued")
	}
	b.SetMode("native")
	b.SetMode("isolated")
	if commands := b.Poll(ctx, HostInfo{}); len(commands) != 0 {
		t.Fatalf("revoked viewer text could execute after re-enable: %+v", commands)
	}
	if err := <-done; err != Disabled {
		t.Fatalf("queued text: %v", err)
	}
}
