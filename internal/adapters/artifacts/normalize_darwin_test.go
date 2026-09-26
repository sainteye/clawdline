//go:build darwin

package artifacts

import (
	"context"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A HEIC is a photograph: it is stored as a JPEG, upright. `sips` converts it
// to a PNG without turning the pixels and leaves the orientation in an eXIf
// chunk (measured 2026-09-26), so this is the whole path a phone's picture
// takes when the browser did not shrink it first.
func TestAHEICIsStoredAsAnUprightJPEG(t *testing.T) {
	dir := t.TempDir()
	jpg, heic := filepath.Join(dir, "in.jpg"), filepath.Join(dir, "in.heic")
	if err := os.WriteFile(jpg, withJPEGOrientation(encodeJPEG(t, halves(64, 32)), 6), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("/usr/bin/sips", "-s", "format", "heic", jpg, "--out", heic).CombinedOutput(); err != nil {
		t.Skipf("this machine's sips cannot write a HEIC: %v: %s", err, out)
	}
	raw, err := os.ReadFile(heic)
	if err != nil {
		t.Fatal(err)
	}
	n, err := Normalize(context.Background(), raw, ProductionPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if n.MediaType != MediaTypeJPEG {
		t.Fatalf("a HEIC was stored as %s", n.MediaType)
	}
	img := decoded(t, n.Data)
	if n.Width != 32 || n.Height != 64 || !near(img.At(16, 8), color.RGBA{255, 0, 0, 255}) {
		t.Fatalf("not upright: %dx%d, top %v", n.Width, n.Height, img.At(16, 8))
	}
}
