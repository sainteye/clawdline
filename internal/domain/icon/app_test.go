package icon

import (
	"bytes"
	"image"
	"image/color"
	_ "image/png"
	"math"
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

// TestTheCreatureIsCentredAndTheRightWayUp.
//
// Right way up is the assertion worth making: the rows are written top-down and
// AppKit's origin is at the bottom, so a port that forgets the flip draws a
// creature standing on its head and every pixel of it is still the right
// colour. Clawd's top two rows are inset by two cells and its bottom rows have
// a gap in the middle — legs — so the two ends are told apart by what is at the
// centre column.
func TestTheCreatureIsCentredAndTheRightWayUp(t *testing.T) {
	t.Parallel()
	const w, h = 750, 1334
	body, ok := Splash(w, h)
	if !ok {
		t.Fatal("refused")
	}
	img := decode(t, body)

	columns, lines := float64(len(appRows[0])), float64(len(appRows))
	cell := math.Round(float64(min(w, h)) * 0.26 / columns)
	originX := (float64(w) - cell*columns) / 2
	originY := (float64(h) - cell*lines) / 2

	// The middle of a given cell, in this image's coordinates.
	at := func(row, column int) (int, int) {
		x := originX + (float64(column)+0.5)*cell
		bottom := originY + (lines-1-float64(row))*cell
		y := float64(h) - (bottom + cell/2)
		return int(x), int(y)
	}
	body16 := color.NRGBA{0xD9, 0x77, 0x57, 0xff}
	ground := color.NRGBA{0x0E, 0x0E, 0x11, 0xff}
	eye := color.NRGBA{0x14, 0x14, 0x16, 0xff}

	for _, want := range []struct {
		row, column int
		colour      color.NRGBA
		why         string
	}{
		{0, 8, ground, "the top row is empty"},
		{1, 8, body16, "the second row is the top of the head"},
		{3, 5, eye, "the left eye"},
		{3, 10, eye, "the right eye"},
		{5, 0, body16, "the widest row reaches the edge"},
		{10, 8, ground, "the gap between the legs is on the bottom row"},
		{10, 3, body16, "and a leg is beside it"},
	} {
		x, y := at(want.row, want.column)
		r, g, b, a := rgba(img.At(x, y))
		wr, wg, wb, wa := rgba(want.colour)
		if r != wr || g != wg || b != wb || a != wa {
			t.Errorf("%s (row %d column %d at %d,%d): #%02x%02x%02x/%d, want #%02x%02x%02x/%d",
				want.why, want.row, want.column, x, y, r, g, b, a, wr, wg, wb, wa)
		}
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
