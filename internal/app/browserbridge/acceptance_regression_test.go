package browserbridge

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHandbackWithdrawsQueuedPersonInput(t *testing.T) {
	b, ctx := connected(t)
	b.SetPersonControl(1, true)
	done := make(chan error, 1)
	go func() {
		_, err := b.PersonDo(ctx, "type", 1, json.RawMessage(`{"text":"synthetic-person-input"}`))
		done <- err
	}()
	for b.Status().Pending == 0 {
		time.Sleep(time.Millisecond)
	}
	b.SetPersonControl(1, false)
	if commands := b.Poll(ctx, HostInfo{Version: "test"}); len(commands) != 0 {
		t.Fatalf("input survived handback: %+v", commands)
	}
	if err := <-done; err != NotTakenOver {
		t.Fatalf("refusal: %v", err)
	}
}

func TestLatePersonReplyCannotCrossHandbackAndRetake(t *testing.T) {
	b, ctx := connected(t)
	b.SetPersonControl(1, true)
	started, release := make(chan struct{}), make(chan struct{})
	fakeHost(ctx, b, func(c Command) Result {
		close(started)
		<-release
		return Result{ID: c.ID, OK: true, Value: json.RawMessage(`{"typed":1}`)}
	})
	done := make(chan error, 1)
	go func() {
		value, err := b.PersonDo(ctx, "type", 1, json.RawMessage(`{"text":"x"}`))
		if len(value) > 0 {
			done <- ExtensionErrorForTest{}
			return
		}
		done <- err
	}()
	<-started
	b.SetPersonControl(1, false)
	b.SetPersonControl(1, true)
	close(release)
	if err := <-done; err != NotTakenOver {
		t.Fatalf("late reply: %v", err)
	}
}

func TestStopSaveFailureIsTypedAndExplicitRetryPersists(t *testing.T) {
	dir := t.TempDir()
	b, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	b.SetMuted("c1", true)
	backup := dir + "-moved"
	if err = os.Rename(dir, backup); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(dir, []byte("unavailable"), 0600); err != nil {
		t.Fatal(err)
	}
	failed := b.SetStopped(true)
	if failed != StateWriteFailed || !b.Status().Stopped || !b.Status().StateError {
		t.Fatalf("stop falsely acknowledged: %v %+v", failed, b.Status())
	}
	if err = os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(backup, dir); err != nil {
		t.Fatal(err)
	}
	// A failed persistence request is not a durable stop receipt. Retry the same
	// stop after storage recovers; only the successful response promises restart protection.
	if err = b.SetStopped(true); err != nil || b.Status().StateError {
		t.Fatalf("retry: %v", err)
	}
	again, err := Open(dir, nil)
	if err != nil || !again.Status().Stopped {
		t.Fatalf("saved stop lost: %v", err)
	}
}

func TestSecretMetadataIsAbsentFromAuditActivityAndDisk(t *testing.T) {
	for _, ok := range []bool{true, false} {
		t.Run(map[bool]string{true: "success", false: "refused"}[ok], func(t *testing.T) {
			dir := t.TempDir()
			b, err := Open(dir, nil)
			if err != nil {
				t.Fatal(err)
			}
			b.host.LastPoll = b.now().Unix()
			b.pollWait = time.Millisecond
			b.owner[1] = "c1"
			b.current["c1"] = 1
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fakeHost(ctx, b, func(c Command) Result {
				return Result{ID: c.ID, Tab: 1, OK: ok, URL: "https://example.com/?q=synthetic-secret", Target: "synthetic-secret", Error: "synthetic-secret was rejected", Thumb: "AAAA"}
			})
			_, _ = b.Do(ctx, "c1", "type", json.RawMessage(`{"text":"synthetic-secret","secret":true}`))
			rows, _ := b.Records("c1", 10)
			activities := b.RecentActivity("c1", 10)
			out, _ := json.Marshal([]any{rows, activities})
			disk, err := os.ReadFile(filepath.Join(dir, JournalFile))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(out), "synthetic-secret") || strings.Contains(string(disk), "synthetic-secret") {
				t.Fatal("secret remains in metadata")
			}
			if len(rows) != 1 || rows[0].TextLen != 16 || !rows[0].Password {
				t.Fatalf("audit facts: %+v", rows)
			}
		})
	}
}

func TestPasswordFieldSuccessDropsStrayError(t *testing.T) {
	b, ctx := connected(t)
	b.owner[1] = "c1"
	b.current["c1"] = 1
	fakeHost(ctx, b, func(c Command) Result {
		return Result{ID: c.ID, Tab: 1, OK: true, Password: true, Error: "synthetic-password", URL: "https://example.com/synthetic-password", Target: "synthetic-password", Thumb: "AAAA"}
	})
	if _, err := b.Do(ctx, "c1", "type", json.RawMessage(`{"text":"synthetic-password"}`)); err != nil {
		t.Fatal(err)
	}
	rows, _ := b.Records("c1", 1)
	out, _ := json.Marshal([]any{rows, b.RecentActivity("c1", 1)})
	if strings.Contains(string(out), "synthetic-password") || rows[0].Error != "" {
		t.Fatal("a successful password input retained arbitrary error metadata")
	}
}
