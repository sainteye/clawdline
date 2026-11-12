package browserbridge

import (
	"encoding/json"
	"testing"
	"time"
)

func TestOnlyTheSelectedChromeHostReceivesCommands(t *testing.T) {
	b, ctx := connected(t)
	b.owner[5], b.current["c1"] = "c1", 5
	if err := b.SetMode("user_chrome"); err != nil {
		t.Fatal(err)
	}
	if len(b.Status().Tabs) != 0 || b.Status().Connected {
		t.Fatal("old profile stayed visible")
	}
	if b.AcceptsHost(HostInfo{}) || b.AcceptsHost(HostInfo{Mode: "isolated"}) {
		t.Fatal("old host can consume user Chrome commands")
	}
	b.Poll(ctx, HostInfo{Mode: "user_chrome", Version: "test"})
	done := make(chan error, 1)
	go func() {
		_, err := b.Do(ctx, "c1", "open", json.RawMessage(`{"url":"https://example.com"}`))
		done <- err
	}()
	deadline := time.Now().Add(time.Second)
	for b.Status().Pending == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if commands := b.Poll(ctx, HostInfo{Mode: "isolated"}); len(commands) != 0 {
		t.Fatal("wrong Chrome received a command")
	}
	commands := b.Poll(ctx, HostInfo{Mode: "user_chrome"})
	if len(commands) != 1 {
		t.Fatalf("selected Chrome received %v", commands)
	}
	b.Deliver([]Result{{ID: commands[0].ID, OK: true, Tab: 9}})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	b.SetMode("native")
	if b.AcceptsHost(HostInfo{Mode: "user_chrome"}) || b.Status().Connected {
		t.Fatal("native choice left Clawdline access on")
	}
	if _, err := b.Do(ctx, "c1", "tabs", nil); err != Disabled {
		t.Fatal(err)
	}
	b.SetMode("user_chrome")
	if len(b.Status().Tabs) != 1 {
		t.Fatal("off/on lost the same profile's tabs")
	}
}

func TestProfileSwitchDiscardsOldPersonFramesEvenWhenTabIDsRepeat(t *testing.T) {
	b, ctx := connected(t)
	b.owner[5] = "c1"
	b.SetPersonControl(5, true)
	done := make(chan error, 1)
	go func() {
		data, err := b.PersonDo(ctx, "screenshot", 5, nil)
		if len(data) > 0 {
			done <- ExtensionErrorForTest{}
			return
		}
		done <- err
	}()
	var command Command
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if commands := b.Poll(ctx, HostInfo{}); len(commands) > 0 {
			command = commands[0]
			break
		}
	}
	if command.ID == "" {
		t.Fatal("no screenshot queued")
	}
	if err := b.SetMode("user_chrome"); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	b.owner[5] = "c1"
	b.mu.Unlock()
	if err := b.SetPersonControlForMode(5, true, "user_chrome"); err != nil {
		t.Fatal(err)
	}
	b.Deliver([]Result{{ID: command.ID, OK: true, Tab: 5, Value: json.RawMessage(`{"image":"old profile"}`)}})
	if err := <-done; err != WrongMode {
		t.Fatalf("old frame crossed modes: %v", err)
	}
}

func TestAStaleViewerCannotActOnAnotherProfileWithTheSameTabNumber(t *testing.T) {
	b, ctx := connected(t)
	b.SetMode("user_chrome")
	b.Poll(ctx, HostInfo{Mode: "user_chrome", Version: "test"})
	b.mu.Lock()
	b.owner[5] = "c1"
	b.mu.Unlock()
	if err := b.SetPersonControlForMode(5, true, "isolated"); err != WrongMode {
		t.Fatalf("stale takeover: %v", err)
	}
	if _, err := b.PersonDoForMode(ctx, "screenshot", 5, nil, "isolated"); err != WrongMode {
		t.Fatalf("stale picture: %v", err)
	}
	if _, err := b.Do(ctx, "c1", "click", json.RawMessage(`{"tab":99,"x":1,"y":1}`)); err != OtherSessionsTab {
		t.Fatalf("unowned daily Chrome tab: %v", err)
	}
	if b.Status().Pending != 0 {
		t.Fatal("stale operation reached the host queue")
	}
}
