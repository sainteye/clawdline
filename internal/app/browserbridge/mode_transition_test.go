package browserbridge

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestModePersistenceAndCloseAdmissionHaveOneOrder(t *testing.T) {
	b, ctx := connected(t)
	b.owner[5], b.current["c1"] = "c1", 5
	path := filepath.Join(t.TempDir(), "mode")
	if err := os.WriteFile(path, []byte("isolated"), 0600); err != nil {
		t.Fatal(err)
	}
	entered, commit := make(chan struct{}), make(chan struct{})
	received, finishClose := make(chan struct{}), make(chan struct{})
	defer close(finishClose)
	fakeHost(ctx, b, func(c Command) Result { close(received); <-finishClose; return Result{ID: c.ID, Tab: 5, OK: true} })
	switched := make(chan error, 1)
	go func() {
		switched <- b.ApplyMode("user_chrome", func() error {
			err := os.WriteFile(path, []byte("user_chrome"), 0600)
			close(entered)
			<-commit
			return err
		})
	}()
	<-entered
	closing := make(chan error, 1)
	go func() { _, err := b.Do(ctx, "c1", "close", nil); closing <- err }()
	// Keep the persistence boundary open while a concurrent command attempts
	// admission. This is failure injection, not a timeout used by the product.
	select {
	case <-received:
	case <-time.After(100 * time.Millisecond):
	}
	close(commit)
	err := <-switched
	data, _ := os.ReadFile(path)
	if err != nil && string(data) == "user_chrome" {
		t.Fatalf("refused mode but persisted it: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := <-closing; err == nil {
		t.Fatal("an old-profile close crossed the mode transition")
	}
}

func TestARejectedModeNeverCallsItsPersistenceCallback(t *testing.T) {
	b, ctx := connected(t)
	b.owner[5], b.current["c1"] = "c1", 5
	started, finish := make(chan struct{}), make(chan struct{})
	fakeHost(ctx, b, func(c Command) Result { close(started); <-finish; return Result{ID: c.ID, Tab: 5, OK: true} })
	done := make(chan error, 1)
	go func() { _, err := b.Do(ctx, "c1", "close", nil); done <- err }()
	<-started
	called := false
	err := b.ApplyMode("user_chrome", func() error { called = true; return nil })
	close(finish)
	if closeErr := <-done; closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != Closing || called {
		t.Fatalf("rejected choice wrote settings: %v called=%v", err, called)
	}
}

func TestFailedModeConfigPersistenceKeepsTheCurrentProfile(t *testing.T) {
	b, _ := connected(t)
	b.owner[5], b.current["c1"] = "c1", 5
	failure := errors.New("injected config write failure")
	if err := b.ApplyMode("user_chrome", func() error { return failure }); err != failure {
		t.Fatal(err)
	}
	if !b.AcceptsHost(HostInfo{Mode: "isolated"}) || b.AcceptsHost(HostInfo{Mode: "user_chrome"}) || len(b.Status().Tabs) != 1 {
		t.Fatal("failed config write changed active profile")
	}
}
