package icon

import (
	"bytes"
	"image"
	"image/color"
	_ "image/png"
	"testing"
)

func decode(t *testing.T, body []byte) image.Image {
	t.Helper()
	img, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("the launch image does not decode: %v", err)
	}
	return img
}

func rgba(c color.Color) (int, int, int, int) {
	r, g, b, a := c.RGBA()
	return int(r >> 8), int(g >> 8), int(b >> 8), int(a >> 8)
}

// TestASplashIsTheSizeItWasAskedFor, because the tag that asks for it is
// matched on the exact geometry of a screen.
func TestASplashIsTheSizeItWasAskedFor(t *testing.T) {
	t.Parallel()
	for _, size := range [][2]int{{1179, 2556}, {2556, 1179}, {750, 1334}, {1320, 2868}} {
		body, ok := Splash(size[0], size[1])
		if !ok {
			t.Fatalf("%dx%d: refused", size[0], size[1])
		}
		bounds := decode(t, body).Bounds()
		if bounds.Dx() != size[0] || bounds.Dy() != size[1] {
			t.Errorf("%dx%d came out %v", size[0], size[1], bounds)
		}
	}
}

// TestASplashIsOnTheManifestsGround. A splash that hands over to a different
// shade is a flash, and a flash is the thing it exists to remove.
func TestASplashIsOnTheManifestsGround(t *testing.T) {
	t.Parallel()
	body, ok := Splash(750, 1334)
	if !ok {
		t.Fatal("refused")
	}
	img := decode(t, body)
	for _, at := range [][2]int{{0, 0}, {749, 0}, {0, 1333}, {749, 1333}, {10, 600}} {
		r, g, b, a := rgba(img.At(at[0], at[1]))
		if r != 0x0E || g != 0x0E || b != 0x11 || a != 0xff {
			t.Errorf("at %v the ground is #%02x%02x%02x alpha %d, want #0e0e11 opaque", at, r, g, b, a)
		}
	}
}

// The selected silhouette has empty eyes, separated hands and an unbroken line.
func TestTheCreatureIsCentredAndTheRightWayUp(t *testing.T) {
	t.Parallel()
	const w, h = 800, 800
	body, _ := Splash(w, h)
	img := decode(t, body)
	scale := float64(w) * 0.26 / 208
	left, top := (w-208*scale)/2, (h-143*scale)/2
	at := func(x, y float64) color.Color { return img.At(int(left+(x-16)*scale), int(top+(y-54)*scale)) }
	for _, point := range [][2]float64{{60, 72}, {180, 72}, {31, 120}, {209, 120}, {108, 144}} {
		r, g, b, a := rgba(at(point[0], point[1]))
		if r <= g || g <= b || a != 255 {
			t.Errorf("body or hand at %v lost its warm orange: %v", point, at(point[0], point[1]))
		}
	}
	for _, point := range [][2]float64{{84, 96}, {156, 96}, {45, 120}, {195, 120}, {72, 144}, {120, 168}} {
		if got := color.NRGBAModel.Convert(at(point[0], point[1])); got != markGround {
			t.Errorf("eye, tile gap or floating-hand gap at %v is filled: %v", point, got)
		}
	}
	for _, x := range []float64{26, 72, 120, 168, 214} {
		if got := color.NRGBAModel.Convert(at(x, 186)); got != (color.NRGBA{0x4c, 0xcb, 0x7f, 255}) {
			t.Errorf("green line at %v changed: %v", x, got)
		}
	}
	_, topGreen, _, _ := rgba(at(60, 72))
	_, bottomGreen, _, _ := rgba(at(180, 144))
	if topGreen <= bottomGreen {
		t.Error("warm lighting no longer falls from upper left to lower right")
	}
}

// TestASplashIsTheSameTwice. Nothing here may depend on a clock or a map's
// order: the same geometry has to give a browser the same bytes.
func TestASplashIsTheSameTwice(t *testing.T) {
	t.Parallel()
	one, ok := Splash(390, 844)
	if !ok {
		t.Fatal("refused")
	}
	two, _ := Splash(390, 844)
	if !bytes.Equal(one, two) {
		t.Error("two draws of one geometry differ")
	}
}

func TestAppIconsKeepDetachedHandsAtBrowserAndDockSizes(t *testing.T) {
	for _, size := range []int{64, 512} {
		body, ok := AppIcon(size)
		if !ok {
			t.Fatal("icon refused")
		}
		img := decode(t, body)
		scale := float64(size) * 0.78 / 208
		left, top := (float64(size)-208*scale)/2, (float64(size)-143*scale)/2
		for _, x := range []float64{45.5, 194.5} {
			px, py := int(left+(x-16)*scale), int(top+(120-54)*scale)
			if got := color.NRGBAModel.Convert(img.At(px, py)); got != markGround {
				t.Errorf("%dpx hand is connected at %v: %v", size, x, got)
			}
		}
	}
}

// TestASplashRefusesWhatIsNotAScreen. A device asks for the geometry of its own
// screen; anything past the limit is somebody probing for a way to make this
// machine allocate.
func TestASplashRefusesWhatIsNotAScreen(t *testing.T) {
	t.Parallel()
	for _, size := range [][2]int{{0, 100}, {100, 0}, {-1, 100}, {SplashLimit + 1, 100}, {100, SplashLimit + 1}} {
		if _, ok := Splash(size[0], size[1]); ok {
			t.Errorf("%dx%d was drawn", size[0], size[1])
		}
	}
	if _, ok := Splash(SplashLimit, 1); !ok {
		t.Error("the limit itself was refused")
	}
}
