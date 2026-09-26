package artifacts

import (
	"image"
	"image/draw"
)

// longEdgeSize is w×h with its longer side at most edge, keeping the aspect
// ratio and never enlarging.
func longEdgeSize(w, h, edge int) (int, int) {
	long := max(w, h)
	if long <= edge {
		return w, h
	}
	return max(1, (w*edge+long/2)/long), max(1, (h*edge+long/2)/long)
}

// scaleDown draws src at dw×dh by area average, keeping its transparency.
//
// Each destination pixel is the average of the source pixels it covers, each
// weighted by how much of it falls inside, in fixed point (1/256 of a pixel).
// The average is taken over premultiplied colour, so a transparent pixel's
// colour does not bleed into its neighbours. One source row is converted at a
// time — the standard library's own fast conversion for a JPEG's YCbCr — so a
// twelve-thousand-pixel picture costs a row of working memory, not a second
// copy of itself. The sums are 64-bit: 255 × 256² × a 12,000-pixel side
// squared is far inside them.
func scaleDown(src image.Image, dw, dh int) *image.RGBA {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	row := image.NewRGBA(image.Rect(0, 0, sw, 1))
	acc := make([]uint64, sw*4)
	const one = 256
	for dy := 0; dy < dh; dy++ {
		y0 := dy * sh * one / dh
		y1 := (dy + 1) * sh * one / dh
		clear(acc)
		for sy := y0 / one; sy*one < y1 && sy < sh; sy++ {
			wy := uint64(min(y1, (sy+1)*one) - max(y0, sy*one))
			draw.Draw(row, row.Bounds(), src, image.Pt(sb.Min.X, sb.Min.Y+sy), draw.Src)
			for i, v := range row.Pix {
				acc[i] += uint64(v) * wy
			}
		}
		for dx := 0; dx < dw; dx++ {
			x0 := dx * sw * one / dw
			x1 := (dx + 1) * sw * one / dw
			var sum [4]uint64
			for sx := x0 / one; sx*one < x1 && sx < sw; sx++ {
				wx := uint64(min(x1, (sx+1)*one) - max(x0, sx*one))
				for c := 0; c < 4; c++ {
					sum[c] += acc[sx*4+c] * wx
				}
			}
			total := uint64(x1-x0) * uint64(y1-y0)
			if total == 0 {
				total = 1
			}
			p := dst.PixOffset(dx, dy)
			for c := 0; c < 4; c++ {
				dst.Pix[p+c] = uint8((sum[c] + total/2) / total)
			}
		}
	}
	return dst
}
