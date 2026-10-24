//go:build darwin

package terminal

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// With iTerm2 scanning off, nothing that lists, reads, types into or presses
// keys in an iTerm2 session reaches osascript, and the listing says it was
// turned off rather than answering an empty list. Every refusal comes before
// the first byte. The test itself sends no Apple Event: each call returns
// before any script would start, which the zero osascript count proves.
func TestITermScanOffSendsNoAppleEvent(t *testing.T) {
	enableITermForTest(t)
	SetITermScan(false)
	t.Cleanup(func() { SetITermScan(true) })
	before, _ := OsascriptReading()

	i := NewITerm()
	i.list = func(context.Context) ([]byte, error) {
		t.Fatal("the listing ran with iTerm2 scanning off")
		return nil, nil
	}
	inv, err := i.Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if inv.Disabled != ITermScanDisabledBy || inv.Complete || len(inv.Sessions) != 0 || inv.Provenance != "iterm" {
		t.Fatalf("the disabled reading: %+v", inv)
	}

	s := session.Session{ID: "0A1B2C3D-0000-4000-8000-000000000001", Backend: session.BackendITerm}
	ctx := context.Background()
	for name, err := range map[string]error{
		"send":      i.Send(ctx, s, "hello"),
		"type":      i.Type(ctx, s, "hello"),
		"keystroke": i.Keystroke(ctx, s, keyEscape),
		"interrupt": i.Interrupt(ctx, s),
		"close":     i.Close(ctx, s),
	} {
		var off ITermScanOff
		var unsent Unsent
		if !errors.As(err, &off) || !errors.As(err, &unsent) {
			t.Errorf("%s with scanning off answered %v, want ITermScanOff (Unsent)", name, err)
		}
	}
	if text, ok, failed := i.CaptureWithFailure(ctx, s); text != "" || ok || failed {
		t.Fatalf("a screen read with scanning off answered %q ok=%v failed=%v", text, ok, failed)
	}

	after, _ := OsascriptReading()
	b, _, _, _ := before.Runs()
	a, _, _, _ := after.Runs()
	if a != b {
		t.Fatalf("%d osascript runs started with iTerm2 scanning off", a-b)
	}
}

// The switch stops the session list's polling; it does not stop what one
// action needs. An action marks its context (ports.Asked), and that context
// passes the switch whether it is on or off. A plain context — every list
// reader's — passes only while scanning is on, which the test above holds to
// zero Apple Events.
func TestAnAskedActionPassesTheITermScanSwitch(t *testing.T) {
	enableITermForTest(t)
	t.Cleanup(func() { SetITermScan(true) })
	plain, asked := context.Background(), ports.Asked(context.Background())
	for _, on := range []bool{true, false} {
		SetITermScan(on)
		if !itermMayAct(asked) {
			t.Errorf("scanning on=%v: an asked action was refused", on)
		}
		if itermMayAct(plain) != on {
			t.Errorf("scanning on=%v: a poll answered %v", on, itermMayAct(plain))
		}
	}
}

// Whether a row only the process table saw is iTerm2's is read from the
// process table, never from iTerm2: it is asked with scanning off, before any
// Apple Event may be sent. A table that could not be read holds nothing.
func TestITermHoldsATTYOnlyWhenTheProcessTableSaysSo(t *testing.T) {
	enableITermForTest(t)
	i := NewITerm()
	i.ptys = func(context.Context) (itermPTYs, bool) {
		return itermPTYs{ttys: map[string]bool{"ttys037": true}, hosts: 1}, true
	}
	ctx := context.Background()
	for tty, want := range map[string]bool{"ttys037": true, "/dev/ttys037": true, "ttys038": false, "": false} {
		if got := i.HoldsTTY(ctx, tty); got != want {
			t.Errorf("HoldsTTY(%q) = %v, want %v", tty, got, want)
		}
	}
	i.ptys = func(context.Context) (itermPTYs, bool) { return itermPTYs{}, false }
	if i.HoldsTTY(ctx, "ttys037") {
		t.Fatal("an unreadable process table was taken to hold the tty")
	}
}

func TestDisabledIntegrationRefusesEveryEntryWithoutAppleEvents(t *testing.T) {
	previous := ITermIntegration()
	SetITermIntegration(false)
	t.Cleanup(func() { SetITermIntegration(previous) })
	before, _ := OsascriptReading()
	ctx := ports.Asked(context.Background())
	i := NewITerm()
	i.list = func(context.Context) ([]byte, error) { t.Fatal("disabled listing ran"); return nil, nil }
	i.ptys = func(context.Context) (itermPTYs, bool) {
		t.Fatal("disabled tty discovery ran")
		return itermPTYs{}, false
	}
	s := session.Session{ID: "0A1B2C3D-0000-4000-8000-000000000001", Backend: session.BackendITerm}
	inv, err := i.Inventory(ctx)
	if err != nil || inv.Disabled != ITermScanDisabledBy {
		t.Fatalf("disabled inventory: %+v %v", inv, err)
	}
	if i.HoldsTTY(ctx, "ttys037") {
		t.Fatal("disabled integration claimed a tty")
	}
	if reach := (Launcher{}).itermReach(ctx); reach.state != ports.CapabilityUnavailable || reach.reason != "iTerm2 integration is turned off in Settings" {
		t.Fatalf("disabled integration capability: %+v", reach)
	}
	if running, err := (Launcher{}).ITermRunning(ctx); running || err != nil {
		t.Fatal("disabled integration detected iTerm2")
	}
	_, openErr := (Launcher{}).NewITermTab(ctx, "echo must-not-run")
	_, closeErr := (Launcher{}).CloseITermChild(ctx, s.ID, time.Now())
	for name, err := range map[string]error{
		"send": i.Send(ctx, s, "hello"), "type": i.Type(ctx, s, "hello"),
		"key": i.Keystroke(ctx, s, keyEscape), "interrupt": i.Interrupt(ctx, s),
		"close": i.Close(ctx, s), "reveal": i.Reveal(ctx, s, true),
		"tmux reveal": itermRevealTmuxPane(ctx, "1", true), "tty reveal": itermRevealTTY(ctx, "ttys037", true),
		"activate": itermActivate(ctx), "open": openErr, "child close": closeErr,
	} {
		var unsent Unsent
		if !errors.As(err, &unsent) {
			t.Errorf("%s did not refuse before sending: %v", name, err)
		}
	}
	if text, ok, failed := i.CaptureWithFailure(ctx, s); text != "" || ok || failed {
		t.Fatal("disabled capture ran")
	}
	if seen, _ := findITermSession(ctx, s.ID); seen != sightingUnknown {
		t.Fatal("disabled close verification ran")
	}
	after, _ := OsascriptReading()
	a, _, _, _ := before.Runs()
	b, _, _, _ := after.Runs()
	if a != b {
		t.Fatalf("disabled integration started %d scripts", b-a)
	}
}
