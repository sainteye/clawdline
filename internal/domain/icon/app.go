package icon

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

var markGround = color.NRGBA{0x0E, 0x0E, 0x11, 0xff} // #0E0E11

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

	scale := float64(min(width, height)) * 0.26 / appArtwork.width
	drawMark(img, (float64(width)-appArtwork.width*scale)/2,
		(float64(height)-appArtwork.height*scale)/2, scale)
	return encode(img)
}

// AppIcon renders a rounded application tile from the same mark as Splash.
// Rounded tiles, warm lighting and the solid underline are shared at every size.
func AppIcon(size int) ([]byte, bool) {
	if size <= 0 || size > SplashLimit {
		return nil, false
	}
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	fillRect(img, 0, 0, float64(size), float64(size), markGround)
	scale := float64(size) * 0.78 / appArtwork.width
	drawMark(img, (float64(size)-appArtwork.width*scale)/2,
		(float64(size)-appArtwork.height*scale)/2, scale)
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

// drawMark fits the canonical SVG's rectangles into the existing tile or splash.
// The rounded edge uses pixel coverage; eye apertures and hand gaps stay empty.
func drawMark(img *image.NRGBA, originX, originY, scale float64) {
	for _, rect := range appArtwork.rects {
		x0 := originX + (rect.x-appArtwork.x)*scale
		y0 := originY + (rect.y-appArtwork.y)*scale
		x1, y1 := x0+rect.width*scale, y0+rect.height*scale
		if rect.radius == 0 {
			fillRect(img, x0, y0, x1, y1, rect.paint)
			continue
		}
		radius := rect.radius * scale
		for y := max(int(math.Floor(y0)), 0); y < min(int(math.Ceil(y1)), img.Bounds().Dy()); y++ {
			for x := max(int(math.Floor(x0)), 0); x < min(int(math.Ceil(x1)), img.Bounds().Dx()); x++ {
				px, py := float64(x)+0.5, float64(y)+0.5
				cx := math.Max(x0+radius, math.Min(px, x1-radius))
				cy := math.Max(y0+radius, math.Min(py, y1-radius))
				coverage := math.Max(0, math.Min(1, radius+0.5-math.Hypot(px-cx, py-cy)))
				paint := rect.paint
				t := ((appArtwork.x+(px-originX)/scale-appArtwork.lightX)*appArtwork.lightDX +
					(appArtwork.y+(py-originY)/scale-appArtwork.lightY)*appArtwork.lightDY) /
					(appArtwork.lightDX*appArtwork.lightDX + appArtwork.lightDY*appArtwork.lightDY)
				shade := appArtwork.shade(t)
				light := func(channel uint8) uint8 {
					v := float64(channel)
					if shade > 0 {
						v += (255 - v) * shade
					} else {
						v *= 1 + shade
					}
					return uint8(math.Round(v))
				}
				paint.R, paint.G, paint.B = light(paint.R), light(paint.G), light(paint.B)
				blend(img, x, y, paint, coverage)
			}
		}
	}
}

// fillRect paints an axis-aligned rectangle with exact coverage.
//
// An axis-aligned rectangle's coverage of a pixel is the product of its two
// overlaps, keeping the continuous green underline free of tile seams.
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
