package main

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/icon"
)

func TestPublishedLaunchImagesMatchDaemon(t *testing.T) {
	if err := generate(filepath.Join("..", "..", "..", "..", ".."), true); err != nil {
		t.Fatal(err)
	}
}

func TestCompressionMayDifferButPixelsAndDimensionsMustMatch(t *testing.T) {
	body, ok := icon.Splash(64, 128)
	if !ok {
		t.Fatal("refused")
	}
	decoded, err := png.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	encode := func(img image.Image) []byte {
		t.Helper()
		var out bytes.Buffer
		encoder := png.Encoder{CompressionLevel: png.NoCompression}
		if err := encoder.Encode(&out, img); err != nil {
			t.Fatal(err)
		}
		return out.Bytes()
	}
	uncompressed := encode(decoded)
	if bytes.Equal(body, uncompressed) {
		t.Fatal("fixture does not exercise different PNG encodings")
	}
	if !samePixels(uncompressed, body) {
		t.Fatal("different compression rejected an identical launch image")
	}
	changed := image.NewNRGBA(decoded.Bounds())
	draw.Draw(changed, changed.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	changed.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	if samePixels(encode(changed), body) {
		t.Fatal("a wrong pixel passed")
	}
	changed.SetNRGBA(0, 0, color.NRGBA{R: 0x0e, G: 0x0e, B: 0x11, A: 128})
	if samePixels(encode(changed), body) {
		t.Fatal("wrong transparency passed")
	}
	if samePixels(encode(image.NewNRGBA(image.Rect(0, 0, 64, 127))), body) {
		t.Fatal("wrong dimensions passed")
	}
}

func TestMissingAndHTMLLaunchImagesAreRejected(t *testing.T) {
	root := t.TempDir()
	console := filepath.Join(root, "web", "console")
	public := filepath.Join(console, "public")
	if err := os.MkdirAll(public, 0755); err != nil {
		t.Fatal(err)
	}
	page := []byte(`<link rel="apple-touch-startup-image" href="/splash-16x32.png">`)
	if err := os.WriteFile(filepath.Join(console, "index.html"), page, 0644); err != nil {
		t.Fatal(err)
	}
	if err := generate(root, true); err == nil {
		t.Fatal("a missing asset passed")
	}
	if err := os.WriteFile(filepath.Join(public, "splash-16x32.png"), []byte("<html>SPA fallback</html>"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := generate(root, true); err == nil {
		t.Fatal("the HTML fallback passed as a launch image")
	}
	if err := generate(root, false); err != nil {
		t.Fatal(err)
	}
	if err := generate(root, true); err != nil {
		t.Fatal(err)
	}
}

func TestPublishedApplicationIconsMatchRenderer(t *testing.T) {
	if err := generateBrand(filepath.Join("..", "..", "..", "..", ".."), true); err != nil {
		t.Fatal(err)
	}
}

func TestBrandIconsRejectBrokenContainers(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"web/console/public", "shell/darwin/Resources"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := generateBrand(root, false); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(root, "web/console/public/favicon.ico")
	body, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	body[18] = 0
	if err := os.WriteFile(name, body, 0644); err != nil {
		t.Fatal(err)
	}
	if err := generateBrand(root, true); err == nil {
		t.Fatal("invalid ICO image offset passed")
	}
	if err := generateBrand(root, false); err != nil {
		t.Fatal(err)
	}
	name = filepath.Join(root, "shell/darwin/Resources/Clawdline.icns")
	body, err = os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	body[12] = 255
	if err := os.WriteFile(name, body, 0644); err != nil {
		t.Fatal(err)
	}
	if err := generateBrand(root, true); err == nil {
		t.Fatal("invalid ICNS chunk passed")
	}
}
