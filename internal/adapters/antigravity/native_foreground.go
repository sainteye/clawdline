package antigravity

import (
	"context"
	"github.com/sainteye/clawdline/internal/app/ports"
)

// VerifyNativeForeground brackets positive kernel foreground ownership with
// full birth/exec/image/parent/TTY identity reads. It conveys no send authority.
func VerifyNativeForeground(ctx context.Context, expected ports.AntigravityProcess) error {
	r := NewNativeProcessReader(nil)
	check := func() error {
		p, err := r.ReadAntigravityProcess(ctx, expected.PID)
		if err != nil {
			return err
		}
		want := expected
		want.TerminalID = ""
		if p != want || p.TTY == "" {
			return nativeIdentityError("antigravity_native_process_changed", nil)
		}
		return nil
	}
	if err := check(); err != nil {
		return err
	}
	if err := nativeForeground(ctx, expected.PID); err != nil {
		return err
	}
	if err := check(); err != nil {
		return err
	}
	return nativeForeground(ctx, expected.PID)
}
