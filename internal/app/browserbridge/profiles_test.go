package browserbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/browserpolicy"
)

func profilesPolicy() browserpolicy.Policy {
	return browserpolicy.Policy{Order: []string{"native", "isolated", "user_chrome"}, Automatic: map[string]bool{"native": true, "isolated": false, "user_chrome": false}}
}

func profilesHost(t *testing.T, h *Profiles, mode string, handle func(Command) Result) *Bridge {
	t.Helper()
	b, err := h.Channel(mode)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	probe, doneProbe := context.WithCancel(ctx)
	doneProbe()
	b.Poll(probe, HostInfo{Mode: mode, Version: "fixture"})
	done := make(chan struct{})
	next := 9
	go func() {
		defer close(done)
		for {
			commands := b.Poll(ctx, HostInfo{Mode: mode, Version: "fixture"})
			if ctx.Err() != nil {
				return
			}
			for _, command := range commands {
				var result Result
				if handle != nil {
					result = handle(command)
				} else {
					tab := command.Tab
					if command.Op == "open" {
						tab = next
						next++
					}
					value, _ := json.Marshal(map[string]any{"profile": mode, "tab": tab})
					result = Result{ID: command.ID, Tab: tab, OK: true, Value: value}
				}
				if err := h.Deliver(mode, []Result{result}); err != nil {
					return
				}
			}
		}
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("profile fixture host did not stop")
		}
	})
	return b
}

func TestExplicitSessionBrowserDoesNotChangeAutomaticPolicy(t *testing.T) {
	h, err := OpenProfiles("", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	profilesHost(t, h, "isolated", nil)
	policy := profilesPolicy()
	before, _ := json.Marshal(policy)
	if _, err := h.DoSession(context.Background(), "one", "open", json.RawMessage(`{"url":"https://example.com"}`), policy, "isolated", true); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(policy)
	if string(before) != string(after) {
		t.Fatal("explicit choice changed global policy")
	}
	if _, err := h.DoSession(context.Background(), "two", "open", json.RawMessage(`{"url":"https://example.com"}`), policy, "", false); err != NativeTools {
		t.Fatalf("another Session inherited the override: %v", err)
	}
	if _, err := h.DoSession(context.Background(), "one", "open", json.RawMessage(`{"url":"https://example.com"}`), policy, "user_chrome", true); err != SetupRequired {
		t.Fatalf("unconfigured Chrome was usable: %v", err)
	}
	mode, _, _ := h.SelectedStatus("one", policy)
	if mode != "isolated" {
		t.Fatal("failed setup changed selected browser")
	}
}

func TestChromeProfilesHaveIndependentTabNumbersAndPersonControl(t *testing.T) {
	h, err := OpenProfiles("", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := profilesHost(t, h, "isolated", nil)
	profilesHost(t, h, "user_chrome", nil)
	policy := profilesPolicy()
	for _, row := range []struct{ session, mode string }{{"one", "isolated"}, {"two", "user_chrome"}} {
		if _, err := h.DoSession(context.Background(), row.session, "open", json.RawMessage(`{"url":"https://example.com"}`), policy, row.mode, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.SetPersonControlForMode(9, true, "isolated"); err != nil {
		t.Fatal(err)
	}
	value, err := h.DoSession(context.Background(), "two", "read", nil, policy, "", false)
	if err != nil || !json.Valid(value) {
		t.Fatalf("other profile was disturbed: %s %v", value, err)
	}
	if _, err := h.DoSession(context.Background(), "one", "open", json.RawMessage(`{"url":"https://example.com"}`), policy, "user_chrome", true); err != PersonControlling {
		t.Fatalf("moved a person's active page: %v", err)
	}
	if err := a.SetPersonControlForMode(9, false, "isolated"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.DoSession(context.Background(), "one", "open", json.RawMessage(`{"url":"https://example.com"}`), policy, "user_chrome", true); err != nil {
		t.Fatal(err)
	}
	if _, err := h.TabChannel("isolated", 9); err != WrongMode {
		t.Fatalf("old phone frame still controls previous profile: %v", err)
	}
	mode, status, _ := h.SelectedStatus("two", policy)
	if mode != "user_chrome" || len(status.Tabs) != 1 || status.Tabs[0].Tab != 9 || status.Tabs[0].Mode != "user_chrome" {
		t.Fatalf("other Session tab changed: %s %+v", mode, status)
	}
}

func TestSessionBrowserSelectionCannotCrossAnAdmittedCommand(t *testing.T) {
	h, err := OpenProfiles("", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	received, finish := make(chan struct{}), make(chan struct{})
	profilesHost(t, h, "isolated", func(c Command) Result {
		close(received)
		<-finish
		return Result{ID: c.ID, Tab: 9, OK: true, Value: json.RawMessage(`{"tab":9}`)}
	})
	profilesHost(t, h, "user_chrome", nil)
	policy := profilesPolicy()
	done := make(chan error, 1)
	go func() {
		_, err := h.DoSession(context.Background(), "one", "open", json.RawMessage(`{"url":"https://example.com"}`), policy, "isolated", true)
		done <- err
	}()
	<-received
	_, err = h.DoSession(context.Background(), "one", "open", json.RawMessage(`{"url":"https://example.com"}`), policy, "user_chrome", true)
	close(finish)
	if err != SessionBusy {
		t.Fatalf("switched while the first operation was admitted: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestProfileSelectionsAndStopSurviveRestartWithoutRewritingUnknownState(t *testing.T) {
	dir := t.TempDir()
	h, err := OpenProfiles(dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	profilesHost(t, h, "isolated", nil)
	policy := profilesPolicy()
	if _, err := h.DoSession(context.Background(), "one", "open", json.RawMessage(`{"url":"https://example.com"}`), policy, "isolated", true); err != nil {
		t.Fatal(err)
	}
	if err := h.SetStopped(true); err != nil {
		t.Fatal(err)
	}
	restored, err := OpenProfiles(dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	mode, status, _ := restored.SelectedStatus("one", policy)
	if mode != "isolated" || !status.Stopped || len(status.Tabs) != 1 {
		t.Fatalf("restart lost state: %s %+v", mode, status)
	}
	if _, err := restored.DoSession(context.Background(), "one", "read", nil, policy, "isolated", true); err != Stopped {
		t.Fatalf("explicit choice resumed emergency stop: %v", err)
	}
	path := filepath.Join(dir, "browser-profiles.json")
	corrupt := []byte(`{"primary":"unexpected","selected":{},"stopped":false}`)
	if err := os.WriteFile(path, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenProfiles(dir, nil, nil); err == nil {
		t.Fatal("invalid stored router state accepted")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(corrupt) {
		t.Fatal("unknown state was overwritten at startup")
	}
}

func TestUnknownLegacyResultsAreNotAcknowledgedInAnotherProfile(t *testing.T) {
	h, err := OpenProfiles("", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Deliver("", []Result{{ID: "unknown-close", Tab: 9, OK: true, Value: json.RawMessage(`{"closed":true}`)}}); !errors.Is(err, WrongMode) {
		t.Fatalf("unknown legacy close acknowledged: %v", err)
	}
}

func TestAnAdmittedPhoneReadPreventsChangingItsSessionBrowser(t *testing.T) {
	h, err := OpenProfiles("", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	received, finish := make(chan struct{}), make(chan struct{})
	profilesHost(t, h, "isolated", func(c Command) Result {
		if c.Op == "screenshot" {
			close(received)
			<-finish
		}
		return Result{ID: c.ID, Tab: 9, OK: true, Value: json.RawMessage(`{"tab":9}`)}
	})
	profilesHost(t, h, "user_chrome", nil)
	policy := profilesPolicy()
	if _, err := h.DoSession(context.Background(), "one", "open", json.RawMessage(`{"url":"https://example.com"}`), policy, "isolated", true); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := h.PersonDo(context.Background(), "isolated", "screenshot", 9, nil); done <- err }()
	<-received
	_, err = h.DoSession(context.Background(), "one", "open", json.RawMessage(`{"url":"https://example.com"}`), policy, "user_chrome", true)
	close(finish)
	if err != SessionBusy {
		t.Fatalf("phone operation crossed browser selection: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSessionChoiceCapacityRefusesWithoutEvictingExistingChoices(t *testing.T) {
	h, err := OpenProfiles("", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	policy := profilesPolicy()
	for n := 0; n < MaxSessionBrowsers; n++ {
		if _, err := h.DoSession(context.Background(), fmt.Sprint("c", n), "read", nil, policy, "native", true); err != NativeTools {
			t.Fatalf("choice %d: %v", n, err)
		}
	}
	if _, err := h.DoSession(context.Background(), "overflow", "read", nil, policy, "native", true); err != SessionChoicesFull {
		t.Fatalf("overflow: %v", err)
	}
	if h.SelectionCount() != MaxSessionBrowsers {
		t.Fatal("capacity changed existing choices")
	}
	if choice := h.selected["c0"]; choice.Mode != "native" || !choice.Explicit {
		t.Fatal("old explicit choice evicted")
	}
}

func TestFailedSelectorPersistenceAdmitsNoPageCommand(t *testing.T) {
	h, err := OpenProfiles("", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	b := profilesHost(t, h, "isolated", nil)
	h.path = filepath.Join(t.TempDir(), "missing", "state.json")
	_, err = h.DoSession(context.Background(), "one", "open", json.RawMessage(`{"url":"https://example.com"}`), profilesPolicy(), "isolated", true)
	if err != StateWriteFailed {
		t.Fatalf("failed state was accepted: %v", err)
	}
	if len(b.Status().Tabs) != 0 || h.SelectionCount() != 0 {
		t.Fatal("failed selector save changed a page or choice")
	}
}

func TestProfileStateReadHonorsItsByteBoundAndPreservesFutureFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "browser-profiles.json")
	raw := []byte(`{"version":1,"primary":"isolated","selected":{},"stopped":false}`)
	exact := append(append([]byte{}, raw...), bytes.Repeat([]byte(" "), MaxProfileStateBytes-len(raw))...)
	if err := os.WriteFile(path, exact, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenProfiles(dir, nil, nil); err != nil {
		t.Fatalf("exact byte bound: %v", err)
	}
	oversized := append(exact, ' ')
	if err := os.WriteFile(path, oversized, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenProfiles(dir, nil, nil); err == nil {
		t.Fatal("oversized selector state accepted")
	}
	future := []byte(`{"version":1,"primary":"isolated","selected":{},"stopped":false,"future_grant":true}`)
	if err := os.WriteFile(path, future, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenProfiles(dir, nil, nil); err == nil {
		t.Fatal("unknown future authority field accepted")
	}
	data, _ := os.ReadFile(path)
	if !bytes.Equal(data, future) {
		t.Fatal("future state was rewritten")
	}
}

func TestAPersonsActiveTabIsNotReplacedByAnotherOpenInTheSameSession(t *testing.T) {
	h, err := OpenProfiles("", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	profilesHost(t, h, "isolated", nil)
	policy := profilesPolicy()
	if _, err := h.DoSession(context.Background(), "one", "open", json.RawMessage(`{"url":"https://example.com"}`), policy, "isolated", true); err != nil {
		t.Fatal(err)
	}
	if err := h.SetPersonControl("isolated", 9, true); err != nil {
		t.Fatal(err)
	}
	if _, err := h.DoSession(context.Background(), "one", "open", json.RawMessage(`{"url":"https://example.com/other"}`), policy, "isolated", true); err != PersonControlling {
		t.Fatalf("new open replaced the person's current page: %v", err)
	}
}

func TestAutomaticSelectionSkipsOnlyBeforeTheSessionStarts(t *testing.T) {
	h, err := OpenProfiles("", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	profilesHost(t, h, "isolated", nil)
	policy := profilesPolicy()
	policy.Order = []string{"user_chrome", "isolated", "native"}
	policy.Automatic["user_chrome"], policy.Automatic["isolated"] = true, true
	raw, err := h.DoSession(t.Context(), "one", "open", json.RawMessage(`{"url":"https://example.com"}`), policy, "", false)
	if err != nil || !bytes.Contains(raw, []byte(`"profile":"isolated"`)) {
		t.Fatalf("unavailable first browser was not skipped: %s %v", raw, err)
	}
	profilesHost(t, h, "user_chrome", nil)
	mode, _, _ := h.SelectedStatus("one", policy)
	if mode != "isolated" {
		t.Fatal("later availability switched a started Session")
	}
	mode, _, _ = h.SelectedStatus("two", policy)
	if mode != "user_chrome" {
		t.Fatal("new Session did not follow the priority")
	}
}

func TestFailedProfileStopCanBeSavedAgainWithoutResuming(t *testing.T) {
	dir := t.TempDir()
	h, err := OpenProfiles(dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	original := h.path
	h.path = filepath.Join(dir, "missing", "state.json")
	if err := h.SetStopped(true); err != StateWriteFailed {
		t.Fatalf("failed stop was acknowledged: %v", err)
	}
	if err := h.SetStopped(false); err != StateWriteFailed {
		t.Fatalf("unsaved stop resumed: %v", err)
	}
	h.path = original
	if err := h.SetStopped(true); err != nil {
		t.Fatalf("retry after storage recovery: %v", err)
	}
	policy := profilesPolicy()
	if _, err := h.DoSession(t.Context(), "one", "open", nil, policy, "isolated", true); err != Stopped {
		t.Fatalf("retry resumed: %v", err)
	}
	if err := h.SetStopped(false); err != nil {
		t.Fatal(err)
	}
	restarted, err := OpenProfiles(dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, status, _ := restarted.SelectedStatus("one", policy)
	if status.Stopped {
		t.Fatal("durable resume was lost")
	}
}

func TestClosedSessionChoiceIsRemovedWithoutClosingItsTabs(t *testing.T) {
	h, err := OpenProfiles(t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	b := profilesHost(t, h, "isolated", nil)
	policy := profilesPolicy()
	if _, err := h.DoSession(t.Context(), "one", "open", json.RawMessage(`{"url":"https://example.com"}`), policy, "isolated", true); err != nil {
		t.Fatal(err)
	}
	if err := h.SetPersonControl("isolated", 9, true); err != nil {
		t.Fatal(err)
	}
	if err := h.ForgetClosedSession("one"); err != PersonControlling {
		t.Fatalf("active takeover lost its choice: %v", err)
	}
	if err := h.SetPersonControl("isolated", 9, false); err != nil {
		t.Fatal(err)
	}
	if err := h.ForgetClosedSession("one"); err != nil {
		t.Fatal(err)
	}
	if h.SelectionCount() != 0 || len(b.Status().Tabs) != 1 {
		t.Fatal("closed choice cleanup removed a tab or retained the choice")
	}
	mode, _, _ := h.SelectedStatus("one", policy)
	if mode != "native" {
		t.Fatal("closed Session retained explicit choice")
	}
}
