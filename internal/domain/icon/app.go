package icon

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// The application mark matches the compact console brand: an eight-by-four
// face with a connected base. Large raster assets add a subtle half-cell grid;
// small interface marks remain solid.
var appRows = [...]string{
	".######.",
	".#o##o#.",
	"########",
	".######.",
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
	// Whole-pixel edges avoid accidental antialiasing seams; the fine grid
	// is added deliberately inside the orange cells by drawMark.
	// On an odd-sized screen, the centre is at most half a pixel away.
	originX := math.Round((float64(width) - cell*columns) / 2)
	originY := math.Round((float64(height) - cell*lines) / 2)
	drawMark(img, height, originX, originY, cell, cell, true)
	return encode(img)
}

// AppIcon renders a rounded application tile from the same mark as Splash.
// The caller selects the fine grid for large assets and a solid mark for tabs.
func AppIcon(size int, grid bool) ([]byte, bool) {
	if size <= 0 || size > SplashLimit {
		return nil, false
	}
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	fillRect(img, 0, 0, float64(size), float64(size), markGround)
	cell := float64(size) * 0.78 / float64(len(appRows[0]))
	x := (float64(size) - cell*float64(len(appRows[0]))) / 2
	y := (float64(size) - cell*float64(len(appRows))) / 2
	drawMark(img, size, x, y, cell, cell, grid)
	radius := float64(size) * 0.20
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			cx := math.Max(radius, math.Min(float64(px)+0.5, float64(size)-radius))
			cy := math.Max(radius, math.Min(float64(py)+0.5, float64(size)-radius))
			distance := math.Hypot(float64(px)+0.5-cx, float64(py)+0.5-cy)
			coverage := math.Max(0, math.Min(1, radius+0.5-distance))
			img.Pix[img.PixOffset(px, py)+3] = uint8(math.Round(coverage * 255))
		}
	}
	return encode(img)
}

// drawMark paints the creature's cells.
//
// The rows are written top-down and AppKit's origin is at the bottom, so
// `originY` counts from the other end and a row's rectangle is flipped back
// into this image's top-left coordinates here. Getting that wrong draws the
// creature upside down, which is a thing that has happened to somebody.
func drawMark(img *image.NRGBA, height int, originX, originY, cell, size float64, grid bool) {
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
			if grid && character == '#' {
				line := size * 0.018
				for _, offset := range []float64{0, size / 2, size} {
					at := math.Min(offset, size-line)
					darkenRect(img, x+at, top, x+at+line, top+size)
					darkenRect(img, x, top+at, x+size, top+at+line)
				}
			}
		}
	}
}

// darkenRect shades the already rendered body rather than compositing an
// opaque line over antialiased edges. Eyes and the tile keep their own colors.
func darkenRect(img *image.NRGBA, x0, y0, x1, y1 float64) {
	bounds := img.Bounds()
	for y := max(int(math.Floor(y0)), bounds.Min.Y); y < min(int(math.Ceil(y1)), bounds.Max.Y); y++ {
		for x := max(int(math.Floor(x0)), bounds.Min.X); x < min(int(math.Ceil(x1)), bounds.Max.X); x++ {
			at := img.PixOffset(x, y)
			if img.Pix[at] <= img.Pix[at+1] || img.Pix[at] <= img.Pix[at+2] {
				continue
			}
			cover := overlap(float64(x), float64(x+1), x0, x1) * overlap(float64(y), float64(y+1), y0, y1)
			for c := 0; c < 3; c++ {
				img.Pix[at+c] = uint8(math.Round(float64(img.Pix[at+c]) * (1 - 0.22*cover)))
			}
		}
	}
}

// fillRect paints an axis-aligned rectangle with exact coverage.
//
// An axis-aligned rectangle's coverage of a pixel is the product of its two
// overlaps. Splash aligns the grid to whole pixels to avoid compositing seams.
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
