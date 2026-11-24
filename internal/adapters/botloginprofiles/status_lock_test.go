package botloginprofiles

import (
	"context"
	"errors"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// stuckStatus is a native status read that never answers by itself: it waits
// until the caller's deadline or the test releases it.
type stuckStatus struct {
	entered chan struct{}
	release chan struct{}
}

func (s *stuckStatus) Start(context.Context, pa.Agent, []string) (app.LoginProcess, error) {
	return nil, errors.New("synthetic native start unavailable")
}
func (s *stuckStatus) Status(ctx context.Context, _ pa.Agent, _ []string) (string, error) {
	select {
	case s.entered <- struct{}{}:
	default:
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-s.release:
		return "", errors.New("synthetic status released")
	}
}
func (*stuckStatus) SignOut(context.Context, pa.Agent, []string) error { return nil }

func stuckFixture(t *testing.T) (*recoveryFixture, *Registry, *stuckStatus) {
	t.Helper()
	f := newRecoveryFixture(t)
	stuck := &stuckStatus{entered: make(chan struct{}, 1), release: make(chan struct{})}
	r, e := New(context.Background(), f.db, f.state, "synthetic-account", "machine", stuck)
	if e != nil {
		t.Fatal(e)
	}
	r.ProbeBackoff = 0
	return f, r, stuck
}

// A sign-in check whose native status read hangs must not freeze every other
// Bot: an execution acquiring a different signed-in profile proceeds at once.
func TestStuckSignInCheckDoesNotBlockOtherExecutions(t *testing.T) {
	ctx := context.Background()
	f, r, stuck := stuckFixture(t)
	t.Cleanup(func() { close(stuck.release) })
	checked, e := f.apply(r, "create", pa.LoginProfile{}, "")
	if e != nil {
		t.Fatal(e)
	}
	ready, e := f.apply(r, "create", pa.LoginProfile{}, "")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.db.Exec(`UPDATE project_assistant_login_profiles SET state='signed_in' WHERE id=?`, ready.ID); e != nil {
		t.Fatal(e)
	}
	checking := make(chan error, 1)
	go func() {
		_, e := f.apply(r, "check", current(t, r, checked.ID), "")
		checking <- e
	}()
	select {
	case <-stuck.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the check never read native status")
	}
	bot := pa.Bot{Key: pa.BotKey{MachineID: "machine", BotID: "other"}, Project: pa.ProjectScope{MachineID: "machine"}, Agent: pa.Codex, LoginProfile: ready.ID}
	acquired := make(chan error, 1)
	go func() {
		_, release, e := r.Acquire(ctx, bot)
		if e == nil {
			release()
		}
		acquired <- e
	}()
	select {
	case e = <-acquired:
		if e != nil {
			t.Fatal("another Bot could not start", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a stuck sign-in check froze another Bot's execution")
	}
}

// A native status read that never answers is bounded: the check reports the
// outcome as unconfirmed after its deadline and leaves the stored state alone.
func TestStuckSignInCheckEndsAtItsDeadline(t *testing.T) {
	f, r, stuck := stuckFixture(t)
	t.Cleanup(func() { close(stuck.release) })
	r.StatusTimeout = 20 * time.Millisecond
	p, e := f.apply(r, "create", pa.LoginProfile{}, "")
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		_, e := f.apply(r, "check", p, "")
		done <- e
	}()
	select {
	case e = <-done:
		if pa.Code(e) != pa.AuthProbeUnknown {
			t.Fatal("a timed-out read was reported as settled", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a stuck native status read had no deadline")
	}
	if state := current(t, r, p.ID).State; state != "signed_out" {
		t.Fatal("a timed-out read changed the stored state", state)
	}
}

// An execution on an unknown profile is refused as unconfirmed, never as
// signed out, so the person is pointed at checking sign-in, not signing in.
func TestUnknownProfileExecutionIsUnconfirmedNotSignedOut(t *testing.T) {
	f := newRecoveryFixture(t)
	r := f.open(t)
	p, e := f.apply(r, "create", pa.LoginProfile{}, "")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.db.Exec(`UPDATE project_assistant_login_profiles SET state='unknown'`); e != nil {
		t.Fatal(e)
	}
	bot := pa.Bot{Key: pa.BotKey{MachineID: "machine", BotID: "bot"}, Project: pa.ProjectScope{MachineID: "machine"}, Agent: pa.Codex, LoginProfile: p.ID}
	_, _, e = r.Acquire(context.Background(), bot)
	var failure *pa.Failure
	if !errors.As(e, &failure) || failure.Code != pa.OutcomeUnknown || failure.Field != "login_profile_state" {
		t.Fatal("an unknown profile was refused with the wrong reason", e)
	}
}
