//go:build !darwin

package artifacts

import "context"

// platformDecode has nothing to call outside macOS: PNG, JPEG and GIF are read
// by Go itself, and every other format is refused as `unsupported_image` with
// ErrDecoderUnavailable in the log rather than accepted and sent on unread.
func platformDecode(ctx context.Context, raw []byte) ([]byte, error) {
	return nil, ErrDecoderUnavailable
}
