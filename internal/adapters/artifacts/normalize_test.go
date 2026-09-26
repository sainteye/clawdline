package artifacts

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// gradient is a picture with no two neighbouring pixels alike, so an encoder
// has something to do.
func gradient(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x * 7), uint8(y * 5), uint8(x + y), 255})
		}
	}
	return img
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 82}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// pngBitDepth is the bit depth in a PNG's IHDR, or 0 for anything that is not
// a PNG.
func pngBitDepth(b []byte) int {
	if len(b) < 26 || !bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")) {
		return 0
	}
	return int(b[24])
}

// exifTIFF is a big-endian TIFF body carrying one orientation tag.
func exifTIFF(orientation uint16) []byte {
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08\x00\x01\x01\x12\x00\x03\x00\x00\x00\x01\x00\x00\x00\x00\x00\x00\x00\x00")
	binary.BigEndian.PutUint16(tiff[18:], orientation)
	return tiff
}

// withJPEGOrientation puts an Exif APP1 carrying orientation right after SOI.
func withJPEGOrientation(raw []byte, orientation uint16) []byte {
	payload := append([]byte("Exif\x00\x00"), exifTIFF(orientation)...)
	n := len(payload) + 2
	app1 := append([]byte{0xFF, 0xE1, byte(n >> 8), byte(n)}, payload...)
	return append(append(append([]byte{}, raw[:2]...), app1...), raw[2:]...)
}

// withPNGOrientation puts an eXIf chunk carrying orientation right after IHDR.
func withPNGOrientation(raw []byte, orientation uint16) []byte {
	body := exifTIFF(orientation)
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(body)))
	chunk = append(chunk, "eXIf"...)
	chunk = append(chunk, body...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
	const ihdrEnd = 8 + 8 + 13 + 4
	return append(append(append([]byte{}, raw[:ihdrEnd]...), chunk...), raw[ihdrEnd:]...)
}

// halves is w×h, the left half red and the right half blue.
func halves(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{255, 0, 0, 255}
			if x >= w/2 {
				c = color.RGBA{0, 0, 255, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func decoded(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("the stored picture does not decode: %v", err)
	}
	return img
}

func near(c color.Color, want color.RGBA) bool {
	r, g, b, _ := c.RGBA()
	d := func(a uint32, w uint8) bool { return int(a>>8)-int(w) < 40 && int(w)-int(a>>8) < 40 }
	return d(r, want.R) && d(g, want.G) && d(b, want.B)
}

// A photograph stays a JPEG. Before this, a 146 KB phone photograph came out
// a 1.19 MB 16-bit PNG.
func TestAJPEGIsStoredAsAJPEG(t *testing.T) {
	in := encodeJPEG(t, gradient(120, 80))
	n, err := Normalize(context.Background(), in, ProductionPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if n.MediaType != MediaTypeJPEG || !bytes.HasPrefix(n.Data, []byte{0xFF, 0xD8, 0xFF}) {
		t.Fatalf("a JPEG was stored as %s, starting % x", n.MediaType, n.Data[:8])
	}
	if n.Width != 120 || n.Height != 80 {
		t.Fatalf("%dx%d", n.Width, n.Height)
	}
	if b := decoded(t, n.Data).Bounds(); b.Dx() != 120 || b.Dy() != 80 {
		t.Fatalf("decoded %v", b)
	}
	if sniffMediaType(n.Data) != MediaTypeJPEG {
		t.Fatal("the drops cache would name this photograph .png")
	}
}

// A screenshot stays PNG, at 8 bits a sample.
func TestAScreenshotIsStoredAsAnEightBitPNG(t *testing.T) {
	for name, src := range map[string]image.Image{
		"rgba":     gradient(90, 60),
		"gray":     image.NewGray(image.Rect(0, 0, 30, 20)),
		"paletted": image.NewPaletted(image.Rect(0, 0, 30, 20), color.Palette{color.Black, color.White}),
		"with alpha": func() image.Image {
			img := image.NewNRGBA(image.Rect(0, 0, 30, 20))
			img.SetNRGBA(3, 3, color.NRGBA{200, 10, 10, 90})
			return img
		}(),
	} {
		n, err := Normalize(context.Background(), encodePNG(t, src), ProductionPolicy)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		// A palette of two is written at one bit, which is still not sixteen.
		if d := pngBitDepth(n.Data); n.MediaType != MediaTypePNG || d < 1 || d > 8 || name == "rgba" && d != 8 {
			t.Fatalf("%s: stored as %s at %d bits", name, n.MediaType, d)
		}
	}
}

// Nothing is ever written at 16 bits: not a JPEG's YCbCr, and not a 16-bit
// PNG, which is reduced to the 8 bits a screen shows. Every case here came out
// 16-bit before (a JPEG of 899 bytes was a PNG of 6,366; the red run is kept
// with this change's delivery).
func TestNothingIsStoredAtSixteenBits(t *testing.T) {
	deep := image.NewNRGBA64(image.Rect(0, 0, 40, 30))
	deepGray := image.NewGray16(image.Rect(0, 0, 40, 30))
	for y := 0; y < 30; y++ {
		for x := 0; x < 40; x++ {
			deep.SetNRGBA64(x, y, color.NRGBA64{uint16(x * 1500), uint16(y * 2000), 0x8000, 0xffff})
			deepGray.SetGray16(x, y, color.Gray16{uint16(x * 1500)})
		}
	}
	for name, in := range map[string][]byte{
		"jpeg":         encodeJPEG(t, gradient(64, 48)),
		"16-bit png":   encodePNG(t, deep),
		"16-bit gray":  encodePNG(t, deepGray),
		"large jpeg":   encodeJPEG(t, gradient(2000, 1200)),
		"large 16-bit": encodePNG(t, image.NewNRGBA64(image.Rect(0, 0, 1700, 20))),
	} {
		if name != "jpeg" && name != "large jpeg" && pngBitDepth(in) != 16 {
			t.Fatalf("%s: the source is not the 16-bit case it is here to pin", name)
		}
		n, err := Normalize(context.Background(), in, ProductionPolicy)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if d := pngBitDepth(n.Data); d == 16 {
			t.Fatalf("%s: stored as a 16-bit PNG of %d bytes", name, len(n.Data))
		}
		if n.MediaType == MediaTypePNG && pngBitDepth(n.Data) != 8 {
			t.Fatalf("%s: a PNG at %d bits", name, pngBitDepth(n.Data))
		}
	}
}

// The daemon bounds the long edge as the console does, for the pictures that
// never passed through a browser.
func TestTheLongEdgeIsBoundedOnTheDaemon(t *testing.T) {
	for _, c := range []struct {
		name      string
		in        []byte
		w, h      int
		mediaType string
	}{
		{"wide screenshot", encodePNG(t, gradient(3200, 800)), 1600, 400, MediaTypePNG},
		{"tall photograph", encodeJPEG(t, gradient(2000, 3000)), 1067, 1600, MediaTypeJPEG},
		{"already small", encodeJPEG(t, gradient(1600, 900)), 1600, 900, MediaTypeJPEG},
		{"small screenshot", encodePNG(t, gradient(300, 200)), 300, 200, MediaTypePNG},
	} {
		n, err := Normalize(context.Background(), c.in, ProductionPolicy)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		b := decoded(t, n.Data).Bounds()
		if n.Width != c.w || n.Height != c.h || b.Dx() != c.w || b.Dy() != c.h || n.MediaType != c.mediaType {
			t.Fatalf("%s: %dx%d %s (decoded %v), want %dx%d %s", c.name, n.Width, n.Height, n.MediaType, b, c.w, c.h, c.mediaType)
		}
	}
	if w, h := longEdgeSize(10, 7000, MaxLongEdge); w != 2 || h != 1600 {
		t.Fatalf("a sliver: %dx%d", w, h)
	}
}

// The scaler averages what each pixel covers, and keeps transparency without
// letting a transparent pixel's colour bleed.
func TestScaleDownAveragesTheAreaItCovers(t *testing.T) {
	checker := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if (x+y)%2 == 0 {
				checker.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
			} else {
				checker.SetRGBA(x, y, color.RGBA{0, 0, 0, 255})
			}
		}
	}
	got := scaleDown(checker, 2, 2)
	for _, p := range [][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
		if c := got.RGBAAt(p[0], p[1]); c.R < 127 || c.R > 128 || c.A != 255 {
			t.Fatalf("a checkerboard halved is grey: %v at %v", c, p)
		}
	}
	// Red beside transparent green: the colour stays red, half as opaque.
	half := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	half.SetNRGBA(0, 0, color.NRGBA{255, 0, 0, 255})
	half.SetNRGBA(1, 0, color.NRGBA{0, 255, 0, 0})
	c := color.NRGBAModel.Convert(scaleDown(half, 1, 1).At(0, 0)).(color.NRGBA)
	if c.G != 0 || c.R < 250 || c.A < 127 || c.A > 128 {
		t.Fatalf("red over nothing: %v", c)
	}
	// Three to two: every source pixel counts toward what it covers.
	ramp := image.NewGray(image.Rect(0, 0, 3, 1))
	ramp.Pix = []uint8{0, 90, 180}
	r := scaleDown(ramp, 2, 1)
	if a, b := r.RGBAAt(0, 0).R, r.RGBAAt(1, 0).R; a != 30 || b != 150 {
		t.Fatalf("a ramp of three in two: %d %d, want 30 150", a, b)
	}
}

// **The orientation decision.** A picture tagged with an EXIF orientation is
// stored with the turn drawn into its pixels and without the tag: a terminal
// program reading the file does not honour it, and the stored file carries no
// metadata from the camera at all.
func TestOrientationIsDrawnIntoThePixels(t *testing.T) {
	tagged := withJPEGOrientation(encodeJPEG(t, halves(64, 32)), 6)
	if exifOrientation(tagged, "jpeg") != 6 {
		t.Fatal("the fixture's tag is not read")
	}
	n, err := Normalize(context.Background(), tagged, ProductionPolicy)
	if err != nil {
		t.Fatal(err)
	}
	img := decoded(t, n.Data)
	if n.Width != 32 || n.Height != 64 || img.Bounds().Dx() != 32 {
		t.Fatalf("turned a quarter: %dx%d", n.Width, n.Height)
	}
	// A quarter clockwise puts the left (red) half on top.
	if !near(img.At(16, 8), color.RGBA{255, 0, 0, 255}) || !near(img.At(16, 56), color.RGBA{0, 0, 255, 255}) {
		t.Fatalf("top %v bottom %v", img.At(16, 8), img.At(16, 56))
	}
	if jpegEXIF(n.Data) != nil {
		t.Fatal("the stored JPEG still carries EXIF")
	}

	// A PNG's eXIf chunk, which is where `sips` leaves a HEIC's tag.
	flipped := withPNGOrientation(encodePNG(t, halves(40, 20)), 3)
	if exifOrientation(flipped, "png") != 3 {
		t.Fatal("the PNG fixture's tag is not read")
	}
	n, err = Normalize(context.Background(), flipped, ProductionPolicy)
	if err != nil {
		t.Fatal(err)
	}
	img = decoded(t, n.Data)
	if n.Width != 40 || !near(img.At(5, 10), color.RGBA{0, 0, 255, 255}) {
		t.Fatalf("upside down puts blue on the left: %v", img.At(5, 10))
	}

	// No tag, and a tag that is not an orientation, turn nothing.
	for _, raw := range [][]byte{encodeJPEG(t, halves(64, 32)), withJPEGOrientation(encodeJPEG(t, halves(64, 32)), 9)} {
		if n, err := Normalize(context.Background(), raw, ProductionPolicy); err != nil || n.Width != 64 || n.Height != 32 {
			t.Fatalf("untagged: %dx%d %v", n.Width, n.Height, err)
		}
	}
}

// Every one of the eight orientations, on a 2×2 picture [A B; C D].
func TestEveryOrientationTurnsTheWayItsTagSays(t *testing.T) {
	A, B, C, D := color.RGBA{1, 0, 0, 255}, color.RGBA{2, 0, 0, 255}, color.RGBA{3, 0, 0, 255}, color.RGBA{4, 0, 0, 255}
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	src.SetRGBA(0, 0, A)
	src.SetRGBA(1, 0, B)
	src.SetRGBA(0, 1, C)
	src.SetRGBA(1, 1, D)
	want := map[int][4]color.RGBA{
		1: {A, B, C, D},
		2: {B, A, D, C},
		3: {D, C, B, A},
		4: {C, D, A, B},
		5: {A, C, B, D},
		6: {C, A, D, B},
		7: {D, B, C, A},
		8: {B, D, A, C},
	}
	for o, w := range want {
		got := image.NewRGBA(image.Rect(0, 0, 2, 2))
		draw.Draw(got, got.Bounds(), orient(src, o), image.Point{}, draw.Src)
		have := [4]color.RGBA{got.RGBAAt(0, 0), got.RGBAAt(1, 0), got.RGBAAt(0, 1), got.RGBAAt(1, 1)}
		if have != w {
			t.Errorf("orientation %d: %v, want %v", o, have, w)
		}
	}
	wide := orient(image.NewRGBA(image.Rect(0, 0, 5, 2)), 8).Bounds()
	if wide.Dx() != 2 || wide.Dy() != 5 {
		t.Fatalf("a quarter turn swaps the sides: %v", wide)
	}
}

// The drops cache names a photograph `.jpg` and a screenshot `.png`, and those
// are still the only names it will ever remove.
func TestDropsNameAPhotographJPGAndRemoveOnlyTheirOwn(t *testing.T) {
	d := &Drops{Dir: filepath.Join(t.TempDir(), "drops"), Keep: 10}
	now := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	photo, _ := Normalize(context.Background(), encodeJPEG(t, gradient(20, 20)), ProductionPolicy)
	shot, _ := Normalize(context.Background(), encodePNG(t, gradient(20, 20)), ProductionPolicy)
	jpgPath, err := d.Store(photo.Data, now)
	if err != nil {
		t.Fatal(err)
	}
	pngPath, err := d.Store(shot.Data, now.Add(time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(jpgPath) != ".jpg" || filepath.Ext(pngPath) != ".png" {
		t.Fatalf("%s %s", jpgPath, pngPath)
	}
	if !dropName.MatchString(filepath.Base(jpgPath)) || !dropName.MatchString(filepath.Base(pngPath)) {
		t.Fatal("the cache does not recognise its own names")
	}
	base := "clawdline-20260926-100000-000-d0000001-0000-4000-8000-000000000001"
	for _, foreign := range []string{base + ".jpeg", base + ".gif", base + ".JPG", base + ".jpg.tmp", "photo.jpg",
		"x" + base + ".jpg", base + ".png.jpg"} {
		if dropName.MatchString(foreign) {
			t.Errorf("%s is not a name this cache writes", foreign)
		}
		if err := os.WriteFile(filepath.Join(d.Dir, foreign), []byte("theirs"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	d.Discard([]string{jpgPath, filepath.Join(d.Dir, base+".jpeg"), filepath.Join(d.Dir, "photo.jpg")})
	if _, err := os.Stat(jpgPath); !os.IsNotExist(err) {
		t.Fatal("the cache's own photograph was not discarded")
	}
	for _, kept := range []string{base + ".jpeg", "photo.jpg"} {
		if _, err := os.Stat(filepath.Join(d.Dir, kept)); err != nil {
			t.Fatalf("%s was removed: %v", kept, err)
		}
	}
}

// The image store keeps a photograph as `<id>.jpg` under image/jpeg, answers
// it, counts it, and still reads a PNG stored before.
func TestTheImageStoreKeepsAPhotographAsAJPEG(t *testing.T) {
	s := NewStore(t.TempDir())
	src := t.TempDir()
	photo := filepath.Join(src, "photo.jpg")
	if err := os.WriteFile(photo, encodeJPEG(t, gradient(50, 40)), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	stored, err := s.ImportPaths(context.Background(), []string{photo, writePNG(t, src, "shot.png", 3, 3)}, now)
	if err != nil {
		t.Fatal(err)
	}
	jpg, shot := stored[0], stored[1]
	if jpg.Artifact.MediaType != MediaTypeJPEG || filepath.Ext(jpg.File) != ".jpg" {
		t.Fatalf("photograph: %+v %s", jpg.Artifact, jpg.File)
	}
	if shot.Artifact.MediaType != MediaTypePNG || filepath.Ext(shot.File) != ".png" {
		t.Fatalf("screenshot: %+v %s", shot.Artifact, shot.File)
	}
	f := s.Lookup(jpg.Artifact.ID, now)
	if f.State != Live || f.Artifact.MediaType != MediaTypeJPEG || !bytes.HasPrefix(f.Data, []byte{0xFF, 0xD8}) {
		t.Fatalf("lookup: %v %+v", f.State, f.Artifact)
	}
	count, size := s.Readings(now)
	if count.Used != 2 || size.Used <= 0 {
		t.Fatalf("readings: %d pictures, %d bytes", count.Used, size.Used)
	}
	// A file under the other ending is nobody's, and the next store sweeps it.
	stray := filepath.Join(s.Dir, jpg.Artifact.ID+".png")
	if err := os.WriteFile(stray, []byte("stray"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportPaths(context.Background(), []string{writePNG(t, src, "again.png", 2, 2)}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Fatal("a picture under the wrong ending was kept")
	}
	if s.Lookup(jpg.Artifact.ID, now).State != Live {
		t.Fatal("the sweep took the photograph with it")
	}
	s.Delete([]string{jpg.Artifact.ID}, now)
	if _, err := os.Stat(jpg.File); !os.IsNotExist(err) {
		t.Fatal("a deleted photograph's file is still there")
	}
}
