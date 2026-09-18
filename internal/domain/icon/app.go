package icon

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// The app's own mark, drawn rather than shipped — `RemoteIcon.swift`.
//
// Drawn, because a pixel creature is nine hundred bytes of source and any file
// of it would be a resampled version of the same thing at one particular size.
//
// **The art is written down here rather than read from the mascot pack**, and
// that is deliberate even though it duplicates nine lines. A pack is a
// preference — somebody can install `mochi` and the character in the notch
// changes with it, which is the whole point of packs. What a browser tab is
// showing is not a preference; it is *this app*, and the icon of a thing should
// not become a different icon because its owner liked a different animal.
//
// Only the launch image is drawn here. The tab icon and the home-screen icons
// are **copied**, byte for byte, into the console's bundle: those are three
// small files with a rounded tile in them, and AppKit's rasteriser for that one
// curve is not something a port can match to the pixel. A launch image is
// axis-aligned rectangles on a flat ground, so this one is a port rather than a
// copy — and it has to be, because there are twenty geometries and the list
// grows every autumn.

// appRows is Clawd, standing. Sixteen wide and eleven tall, `#` body, `o` eye,
// `.` nothing — the same grid `Resources/mascots/clawd.json` draws from.
var appRows = [...]string{
	"................",
	"..############..",
	"..############..",
	"..###oo##oo###..",
	"..###oo##oo###..",
	"################",
	"################",
	"..############..",
	"..############..",
	"..##.##..##.##..",
	"..##.##..##.##..",
}

var (
	markBody   = color.NRGBA{0xD9, 0x77, 0x57, 0xff} // #D97757
	markEye    = color.NRGBA{0x14, 0x14, 0x16, 0xff} // #141416
	markGround = color.NRGBA{0x0E, 0x0E, 0x11, 0xff} // #0E0E11
)

// SplashLimit is the largest launch image this will draw. A device asks for the
// geometry of its own screen, so anything past this is not a device.
const SplashLimit = 4096

// Splash is a launch image, at whatever pixel size a device happens to be.
//
// **iOS still does not build one from the manifest**, ten years in:
// `background_color` is what Android uses, and Safari wants
// `apple-touch-startup-image` with a media query per device — twenty of them,
// matched on width, height, pixel ratio and orientation. Without them a
// home-screen app opens onto whatever Safari decides to draw, which on a dark
// page is a black rectangle for as long as the page takes to arrive, and reads
// as a hang.
//
// The ground matches the manifest's `background_color` exactly, so the moment
// the page does arrive nothing changes colour — a splash that hands over to a
// different shade is a flash, and a flash is the thing this exists to remove.
func Splash(width, height int) ([]byte, bool) {
	if width <= 0 || height <= 0 || width > SplashLimit || height > SplashLimit {
		return nil, false
	}
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	fillRect(img, 0, 0, float64(width), float64(height), markGround)

	// A quarter of the short edge. Big enough to be the thing you see, small
	// enough that it is still a launch screen rather than a poster.
	columns := float64(len(appRows[0]))
	lines := float64(len(appRows))
	cell := math.Round(float64(min(width, height)) * 0.26 / columns)
	originX := (float64(width) - cell*columns) / 2
	originY := (float64(height) - cell*lines) / 2
	drawMark(img, height, originX, originY, cell, cell)
	return encode(img)
}

// drawMark paints the creature's cells.
//
// The rows are written top-down and AppKit's origin is at the bottom, so
// `originY` counts from the other end and a row's rectangle is flipped back
// into this image's top-left coordinates here. Getting that wrong draws the
// creature upside down, which is a thing that has happened to somebody.
func drawMark(img *image.NRGBA, height int, originX, originY, cell, size float64) {
	lines := float64(len(appRows))
	for row, line := range appRows {
		for column, character := range line {
			var paint color.NRGBA
			switch character {
			case '#':
				paint = markBody
			case 'o':
				paint = markEye
			default:
				continue
			}
			x := originX + float64(column)*cell
			bottom := originY + (lines-1-float64(row))*cell
			top := float64(height) - (bottom + size)
			fillRect(img, x, top, x+size, top+size, paint)
		}
	}
}

// fillRect paints an axis-aligned rectangle with exact coverage.
//
// Exact, rather than rounded to whole pixels, because the origins are halves:
// a 1179-wide screen with a 19-point cell leaves the creature starting at
// x=437.5, and snapping that would move the mark half a pixel off centre on
// most of the twenty geometries. An axis-aligned rectangle's coverage of a
// pixel is the product of its two overlaps, so this is the answer rather than
// an approximation of it.
func fillRect(img *image.NRGBA, x0, y0, x1, y1 float64, paint color.NRGBA) {
	bounds := img.Bounds()
	left := max(int(math.Floor(x0)), bounds.Min.X)
	right := min(int(math.Ceil(x1)), bounds.Max.X)
	top := max(int(math.Floor(y0)), bounds.Min.Y)
	bottom := min(int(math.Ceil(y1)), bounds.Max.Y)
	for y := top; y < bottom; y++ {
		coverY := overlap(float64(y), float64(y+1), y0, y1)
		if coverY <= 0 {
			continue
		}
		for x := left; x < right; x++ {
			cover := coverY * overlap(float64(x), float64(x+1), x0, x1)
			if cover <= 0 {
				continue
			}
			blend(img, x, y, paint, cover)
		}
	}
}

func overlap(a0, a1, b0, b1 float64) float64 {
	lo, hi := math.Max(a0, b0), math.Min(a1, b1)
	if hi <= lo {
		return 0
	}
	return hi - lo
}

// blend is source-over, on an image whose ground is already opaque.
func blend(img *image.NRGBA, x, y int, paint color.NRGBA, cover float64) {
	if cover > 1 {
		cover = 1
	}
	i := img.PixOffset(x, y)
	over := func(src, dst uint8) uint8 {
		return uint8(math.Round(float64(src)*cover + float64(dst)*(1-cover)))
	}
	img.Pix[i+0] = over(paint.R, img.Pix[i+0])
	img.Pix[i+1] = over(paint.G, img.Pix[i+1])
	img.Pix[i+2] = over(paint.B, img.Pix[i+2])
	alpha := float64(paint.A)*cover + float64(img.Pix[i+3])*(1-cover)
	img.Pix[i+3] = uint8(math.Round(alpha))
}

func encode(img image.Image) ([]byte, bool) {
	var out bytes.Buffer
	if png.Encode(&out, img) != nil {
		return nil, false
	}
	return out.Bytes(), true
}
