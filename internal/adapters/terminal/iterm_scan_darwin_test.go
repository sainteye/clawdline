//go:build darwin

package terminal

import (
	"context"
	"errors"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// With iTerm2 scanning off, nothing that lists, reads, types into or presses
// keys in an iTerm2 session reaches osascript, and the listing says it was
// turned off rather than answering an empty list. Every refusal comes before
// the first byte. The test itself sends no Apple Event: each call returns
// before any script would start, which the zero osascript count proves.
func TestITermScanOffSendsNoAppleEvent(t *testing.T) {
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
