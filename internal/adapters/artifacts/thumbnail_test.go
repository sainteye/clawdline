package artifacts

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// screenshotPNG draws something shaped like what a reference image usually
// is — a screenshot: flat panels, lines of text-sized strokes, a gradient —
// and writes it as the PNG the store would hold. A random-noise picture would
// measure a JPEG's worst case, which is not the case a card draws.
func screenshotPNG(t testing.TB, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.NRGBA{R: 246, G: 246, B: 248, A: 255}
			switch {
			case x < w/5:
				c = color.NRGBA{R: 30, G: 34, B: 42, A: 255}
				if y%28 < 10 && x > 20 && x < w/5-20 && (x/7+y/28)%5 != 0 {
					c = color.NRGBA{R: 200, G: 204, B: 212, A: 255}
				}
			case y < 60:
				c = color.NRGBA{R: uint8(40 + x*120/w), G: 90, B: uint8(200 - x*80/w), A: 255}
			case y%22 < 9 && x%11 < 8 && (x/37+y/22)%7 != 0:
				c = color.NRGBA{R: 40, G: 44, B: 52, A: 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// A 1600 × 873 reference comes back as a JPEG no longer than 480 pixels on
// its long side, in proportion, and a small fraction of the PNG it was drawn
// from. The sizes are logged, because the number is the point of it.
func TestAThumbnailIsSmallAndInProportion(t *testing.T) {
	stored := screenshotPNG(t, 1600, 873)
	thumb, err := MakeThumbnail(stored)
	if err != nil {
		t.Fatal(err)
	}
	if thumb.Width != 480 || thumb.Height != 262 {
		t.Fatalf("a 1600x873 image was drawn at %dx%d", thumb.Width, thumb.Height)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(thumb.JPEG))
	if err != nil || format != "jpeg" || cfg.Width != 480 || cfg.Height != 262 {
		t.Fatalf("the thumbnail reads back as %s %dx%d: %v", format, cfg.Width, cfg.Height, err)
	}
	t.Logf("1600x873: stored PNG %d bytes, thumbnail JPEG %d bytes (%dx%d)",
		len(stored), len(thumb.JPEG), thumb.Width, thumb.Height)
	// A thumbnail is a small answer: it must fit under the Cloud spool's
	// large-answer threshold (256 KiB) as base64, or it would be held to the
	// same reserve the full images are.
	if encoded := (len(thumb.JPEG) + 2) / 3 * 4; encoded > 256<<10 {
		t.Fatalf("the thumbnail is %d bytes as base64", encoded)
	}
}

// Never enlarged, and a portrait image keeps its long side as its height.
func TestAThumbnailIsNeverLargerThanItsImage(t *testing.T) {
	for _, c := range []struct{ w, h, tw, th int }{
		{200, 100, 200, 100},
		{873, 1600, 262, 480},
		{480, 480, 480, 480},
		{5000, 3, 480, 1},
	} {
		thumb, err := MakeThumbnail(screenshotPNG(t, c.w, c.h))
		if err != nil {
			t.Fatal(err)
		}
		if thumb.Width != c.tw || thumb.Height != c.th {
			t.Errorf("%dx%d drew %dx%d, want %dx%d", c.w, c.h, thumb.Width, thumb.Height, c.tw, c.th)
		}
	}
}

// Transparency is composited onto white, which is what a card draws behind
// it; a JPEG has no alpha to keep it in.
func TestATransparentImageIsDrawnOnWhite(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	var raw bytes.Buffer
	if err := png.Encode(&raw, img); err != nil {
		t.Fatal(err)
	}
	thumb, err := MakeThumbnail(raw.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(thumb.JPEG))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := decoded.At(5, 5).RGBA()
	if r>>8 < 250 || g>>8 < 250 || b>>8 < 250 {
		t.Fatalf("a transparent pixel was drawn as %d,%d,%d", r>>8, g>>8, b>>8)
	}
	if _, err := MakeThumbnail([]byte("not a picture")); err == nil {
		t.Fatal("bytes that are not a picture were drawn")
	}
}

// The cache holds at most its budget, lets the thumbnail used longest ago go,
// and never answers for different bytes under the same id.
func TestTheThumbnailCacheIsBoundedAndKeyedByBytes(t *testing.T) {
	a, b := screenshotPNG(t, 900, 600), screenshotPNG(t, 600, 900)
	one, err := MakeThumbnail(a)
	if err != nil {
		t.Fatal(err)
	}
	two, err := MakeThumbnail(b)
	if err != nil {
		t.Fatal(err)
	}
	// Room for either, not for both.
	budget := max(len(one.JPEG), len(two.JPEG)) + 10
	cache := NewThumbnailCache(budget)
	if _, err := cache.Get("a", a); err != nil {
		t.Fatal(err)
	}
	if held, entries, _, _ := cache.Reading(); entries != 1 || held != int64(len(one.JPEG)) {
		t.Fatalf("after one: %d bytes in %d", held, entries)
	}
	other, err := cache.Get("a", b)
	if err != nil {
		t.Fatal(err)
	}
	if other.Width != 320 || other.Height != 480 {
		t.Fatalf("the same id with other bytes answered the first thumbnail: %dx%d", other.Width, other.Height)
	}
	held, entries, evicted, _ := cache.Reading()
	if held > int64(budget) || entries != 1 || evicted != 1 {
		t.Fatalf("past its budget the cache holds %d bytes in %d entries, %d let go", held, entries, evicted)
	}
	var none *ThumbnailCache
	if _, err := none.Get("a", a); err != nil {
		t.Fatalf("a nil cache draws: %v", err)
	}
}

func BenchmarkThumbnail1600x873(b *testing.B) {
	stored := screenshotPNG(b, 1600, 873)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := MakeThumbnail(stored); err != nil {
			b.Fatal(err)
		}
	}
}
