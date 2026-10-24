//go:build !darwin && !linux

package antigravity

import "context"

func nativeForeground(context.Context, int) error {
	return nativeIdentityError("antigravity_native_process_unsupported", nil)
}
