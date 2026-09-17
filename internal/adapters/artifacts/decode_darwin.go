//go:build darwin

package artifacts

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// platformDecode hands a format Go cannot read (HEIC, TIFF, WebP, BMP…) to
// `sips`, the command-line face of the same ImageIO the Swift app's
// `NSBitmapImageRep` uses, and reads the PNG it writes. The bytes go through a
// private directory that is removed before this returns.
func platformDecode(ctx context.Context, raw []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, decodeTimeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "clawdline-next-decode-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	in := filepath.Join(dir, "in")
	out := filepath.Join(dir, "out.png")
	if err := os.WriteFile(in, raw, 0o600); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/sips", "-s", "format", "png", in, "--out", out)
	if said, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("sips could not convert it: %w: %s", err, said)
	}
	return os.ReadFile(out)
}
