package antigravity

import "context"

func nativeForeground(ctx context.Context, pid int) error {
	if err := nativeContext(ctx); err != nil {
		return err
	}
	p, err := darwinProcessInfo(pid)
	if err != nil {
		return err
	}
	if p.Eproc.Tdev == -1 || p.Eproc.Pgid <= 0 || p.Eproc.Pgid != p.Eproc.Tpgid {
		return nativeIdentityError("antigravity_native_not_foreground", nil)
	}
	return nativeContext(ctx)
}
