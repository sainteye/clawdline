package browserbridge

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestCloseOnlyClosesAnOwnedTabOutsidePersonControl(t *testing.T) {
	b, ctx := connected(t)
	b.owner[5], b.owner[7], b.current["c1"] = "c1", "c1", 5
	fakeHost(ctx, b, func(c Command) Result {
		return Result{ID: c.ID, Tab: c.Tab, OK: true, Value: json.RawMessage(`{"closed":true}`)}
	})
	if _, err := b.Do(ctx, "c2", "close", json.RawMessage(`{"tab":5}`)); err != OtherSessionsTab {
		t.Fatalf("other Session: %v", err)
	}
	if _, err := b.Do(ctx, "c1", "close", json.RawMessage(`{"tab":99}`)); err != OtherSessionsTab {
		t.Fatalf("unowned tab: %v", err)
	}
	b.SetPersonControl(5, true)
	if _, err := b.Do(ctx, "c1", "close", nil); err != PersonControlling {
		t.Fatalf("person's tab: %v", err)
	}
	b.SetPersonControl(5, false)
	if _, err := b.Do(ctx, "c1", "close", nil); err != nil {
		t.Fatal(err)
	}
	if b.owner[5] != "" || b.current["c1"] != 7 {
		t.Fatalf("closed tab remained current: %v %v", b.owner, b.current)
	}
}

func TestLatePageDataCannotRestoreAClosedTab(t *testing.T) {
	b, ctx := connected(t)
	b.owner[5], b.current["c1"] = "c1", 5
	waitCommand := func() Command {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if commands := b.Poll(ctx, HostInfo{Version: "test"}); len(commands) > 0 {
				return commands[0]
			}
		}
		t.Fatal("no queued command")
		return Command{}
	}
	readDone := make(chan error, 1)
	go func() {
		value, err := b.Do(ctx, "c1", "read", nil)
		if len(value) > 0 {
			readDone <- ExtensionErrorForTest{}
			return
		}
		readDone <- err
	}()
	read := waitCommand()
	closed := make(chan error, 1)
	go func() { _, err := b.Do(ctx, "c1", "close", nil); closed <- err }()
	closeCommand := waitCommand()
	b.Deliver([]Result{{ID: closeCommand.ID, Tab: 5, OK: true, Value: json.RawMessage(`{"closed":true}`)}})
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	b.Deliver([]Result{{ID: read.ID, Tab: 5, OK: true, Value: json.RawMessage(`{"text":"old page"}`)}})
	if err := <-readDone; err != NoTab {
		t.Fatalf("late data: %v", err)
	}
	if b.current["c1"] != 0 || len(b.Status().Tabs) != 0 {
		t.Fatal("closed tab was resurrected")
	}
}

func TestAClosingTabCannotBeAcknowledgedAsTakenOver(t *testing.T) {
	b, ctx := connected(t)
	b.owner[5], b.current["c1"] = "c1", 5
	started, release := make(chan struct{}), make(chan struct{})
	removed := false
	fakeHost(ctx, b, func(c Command) Result {
		close(started)
		<-release
		removed = true
		return Result{ID: c.ID, Tab: 5, OK: true, Value: json.RawMessage(`{"closed":true}`)}
	})
	done := make(chan error, 1)
	go func() { _, err := b.Do(ctx, "c1", "close", nil); done <- err }()
	<-started
	if err := b.SetPersonControl(5, true); err != Closing {
		t.Fatalf("takeover acknowledged before close settled: %v", err)
	}
	b.SetEnabled(false)
	b.SetEnabled(true)
	if err := b.SetPersonControl(5, true); err != Closing {
		t.Fatalf("toggle discarded close reservation: %v", err)
	}
	close(release)
	if err := <-done; err != Disabled {
		t.Fatalf("old result crossed permission epoch: %v", err)
	}
	if !removed || len(b.Status().Tabs) != 0 {
		t.Fatal("confirmed closure did not remove ownership")
	}
}

func TestUncertainCloseSurvivesTimeoutAndRestartUntilHostAnswers(t *testing.T) {
	b, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	b.host.LastPoll = b.now().Unix()
	b.timeout = 20 * time.Millisecond
	b.owner[5], b.current["c1"] = "c1", 5
	b.pollWait = time.Millisecond
	done := make(chan error, 1)
	go func() { _, err := b.Do(t.Context(), "c1", "close", nil); done <- err }()
	var command Command
	for command.ID == "" {
		if commands := b.Poll(t.Context(), HostInfo{Version: "test"}); len(commands) > 0 {
			command = commands[0]
		}
	}
	if err := <-done; err != TimedOut {
		t.Fatal(err)
	}
	again, err := Open(filepath.Dir(b.tabsPath), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := again.SetPersonControl(5, true); err != Closing {
		t.Fatalf("restart forgot uncertain close: %v", err)
	}
	again.Deliver([]Result{{ID: command.ID, Tab: 5, OK: true}})
	if len(again.Status().Tabs) != 0 {
		t.Fatal("late close result left stale tab")
	}
}

func TestCloseReceiptIsNotAcknowledgedBeforeControlStateIsSaved(t *testing.T) {
	b, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	b.host.LastPoll = b.now().Unix()
	b.owner[5], b.current["c1"] = "c1", 5
	b.timeout = 20 * time.Millisecond
	b.pollWait = time.Millisecond
	done := make(chan error, 1)
	go func() { _, err := b.Do(t.Context(), "c1", "close", nil); done <- err }()
	var c Command
	for c.ID == "" {
		if commands := b.Poll(t.Context(), HostInfo{Version: "test"}); len(commands) > 0 {
			c = commands[0]
		}
	}
	<-done
	path := b.tabsPath
	b.tabsPath = filepath.Join(t.TempDir(), "missing", "state.json")
	result := []Result{{ID: c.ID, Tab: 5, OK: true}}
	if err := b.Deliver(result); err == nil {
		t.Fatal("failed state save was acknowledged")
	}
	if err := b.SetPersonControl(5, true); err != Closing {
		t.Fatalf("failed receipt released close: %v", err)
	}
	b.tabsPath = path
	if err := b.Deliver(result); err != nil {
		t.Fatal(err)
	}
	again, err := Open(filepath.Dir(path), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Status().Tabs) != 0 {
		t.Fatal("acknowledged receipt was not durable")
	}
	if err := again.Deliver(result); err != nil {
		t.Fatal(err)
	}
}
