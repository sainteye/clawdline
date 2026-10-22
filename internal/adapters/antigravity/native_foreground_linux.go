package antigravity

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

func nativeForeground(ctx context.Context, pid int) error {
	data, err := nativeProcFile(ctx, fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return err
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return nativeIdentityError("antigravity_native_identity_invalid", nil)
	}
	f := strings.Fields(string(data[end+1:]))
	if len(f) < 6 {
		return nativeIdentityError("antigravity_native_identity_invalid", nil)
	}
	group, a := strconv.Atoi(f[2])
	foreground, b := strconv.Atoi(f[5])
	tty, c := strconv.Atoi(f[4])
	if a != nil || b != nil || c != nil || tty == 0 || group <= 0 || group != foreground {
		return nativeIdentityError("antigravity_native_not_foreground", nil)
	}
	return nativeContext(ctx)
}
