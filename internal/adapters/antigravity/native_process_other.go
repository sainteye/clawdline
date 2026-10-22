//go:build !darwin && !linux

package antigravity

import "context"

func readNativeProcessSample(ctx context.Context, _ int) (nativeProcessSample, error) {
	if err := nativeContext(ctx); err != nil {
		return nativeProcessSample{}, err
	}
	return nativeProcessSample{}, nativeIdentityError("antigravity_native_process_unsupported", nil)
}
