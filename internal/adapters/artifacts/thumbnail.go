package artifacts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"sync"
	"time"
)

// MaxThumbnailEdge is the longest side of a reference image's thumbnail, in
// pixels (limits N22, `cloud.spool_channel_bytes`).
//
// A Board card and a to-do row draw a reference at a few hundred CSS pixels
// at most, and 480 covers that at 2x on a phone. The number exists for the
// Cloud line: the full normalized PNG of one reference was 971,344 bytes
// (measured 2026-09-25), so three thumbnails' worth of full images filled the
// 4 MiB reply channel every machine read shares, and every read behind them
// went unanswered.
const MaxThumbnailEdge = 480

// thumbnailQuality is the JPEG quality a thumbnail is written at. It is a
// parameter, not a bound: 80 keeps a screenshot's text legible at card size.
const thumbnailQuality = 80

// ErrThumbnail is a stored image that could not be read back as a picture.
// The store only ever holds what Normalize wrote, so this is corruption and
// not a person's input.
var ErrThumbnail = errors.New("that stored image could not be read as a picture")

// Thumbnail is one reference image drawn small.
type Thumbnail struct {
	JPEG   []byte
	Width  int
	Height int
}

// MakeThumbnail draws a stored reference image with its long edge at most
// MaxThumbnailEdge and writes it as JPEG.
//
// **JPEG, and why not PNG.** Both are written by Go itself on every platform,
// with no cgo and no `sips`, which is the only property the brief asked for.
// What separates them is size: a reference is almost always a screenshot or a
// photograph, and the same 480-pixel picture is several times larger as PNG.
// Transparency is composited onto white, which is what a card draws behind
// it anyway.
//
// The resampling is an area average (every source pixel counts toward exactly
// the destination pixels it covers), written out here because the standard
// library has no scaler and a nearest-neighbour one turns a screenshot's text
// into noise. A picture already small enough is re-encoded at its own size:
// the answer is still a JPEG, so a reader never has to guess which it got.
func MakeThumbnail(stored []byte) (Thumbnail, error) {
	src, _, err := image.Decode(bytes.NewReader(stored))
	if err != nil {
		return Thumbnail{}, ErrThumbnail
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return Thumbnail{}, ErrThumbnail
	}
	tw, th := thumbnailSize(w, h)
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	downscale(dst, src)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: thumbnailQuality}); err != nil {
		return Thumbnail{}, ErrThumbnail
	}
	return Thumbnail{JPEG: out.Bytes(), Width: tw, Height: th}, nil
}

// thumbnailSize keeps the aspect ratio and never enlarges.
func thumbnailSize(w, h int) (int, int) {
	return longEdgeSize(w, h, MaxThumbnailEdge)
}

// downscale fills dst with the area average of src, composited onto white.
//
// Each destination pixel covers a rectangle of source pixels in fixed-point
// (1/256 of a pixel) coordinates; every source pixel is weighted by how much
// of it falls inside. The sums are 64-bit, so a 16,384-pixel side (Policy's
// dimension limit) cannot overflow them.
func downscale(dst *image.RGBA, src image.Image) {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	dw, dh := dst.Bounds().Dx(), dst.Bounds().Dy()
	const one = 256
	for dy := 0; dy < dh; dy++ {
		y0 := dy * sh * one / dh
		y1 := (dy + 1) * sh * one / dh
		for dx := 0; dx < dw; dx++ {
			x0 := dx * sw * one / dw
			x1 := (dx + 1) * sw * one / dw
			var r, g, bl, total int64
			for sy := y0 / one; sy*one < y1 && sy < sh; sy++ {
				wy := int64(min(y1, (sy+1)*one) - max(y0, sy*one))
				for sx := x0 / one; sx*one < x1 && sx < sw; sx++ {
					wx := int64(min(x1, (sx+1)*one) - max(x0, sx*one))
					weight := wx * wy
					cr, cg, cb, ca := src.At(sb.Min.X+sx, sb.Min.Y+sy).RGBA()
					// Premultiplied colour over white: c + (1-a)·white.
					white := int64(0xffff - ca)
					r += weight * (int64(cr) + white)
					g += weight * (int64(cg) + white)
					bl += weight * (int64(cb) + white)
					total += weight
				}
			}
			if total == 0 {
				total = 1
			}
			dst.SetRGBA(dx, dy, color.RGBA{
				R: uint8((r / total) >> 8), G: uint8((g / total) >> 8), B: uint8((bl / total) >> 8), A: 0xff,
			})
		}
	}
}

// ThumbnailCache holds thumbnails already drawn, by the stored image they were
// drawn from, up to a byte budget (the register's `cache.image_thumbs`).
//
// **Derived on demand and cached, not stored beside the original.** Storing a
// thumbnail at upload would put a second blob per image in the durable store,
// under a migration, with every image already uploaded owed a backfill; and
// the store's own image-byte bound would then be spending part of itself on
// copies that can always be drawn again. A bounded in-memory cache keeps the
// store exactly as bounded as it was, costs one decode per image per process,
// and forgets nothing that cannot be rebuilt. Past the budget the thumbnail
// used longest ago is let go and the next read draws it again.
//
// The key is the image id **and a digest of its bytes**, so a cache entry can
// never answer for different bytes under a reused id: the route reads the
// stored image first either way (which is also what makes a deleted image a
// 404 and not a cached picture).
type ThumbnailCache struct {
	mu      sync.Mutex
	limit   int
	held    int
	tick    uint64
	entries map[string]*thumbEntry
	evicted int64
	lastAt  time.Time
}

type thumbEntry struct {
	thumb Thumbnail
	used  uint64
}

// NewThumbnailCache holds up to limit bytes of thumbnails. A limit of zero or
// less caches nothing, and every read draws its thumbnail again.
func NewThumbnailCache(limit int) *ThumbnailCache {
	return &ThumbnailCache{limit: limit, entries: map[string]*thumbEntry{}}
}

// Get answers the thumbnail for one stored image, drawing it on a miss.
//
// A nil cache holds nothing and draws every thumbnail it is asked for.
func (c *ThumbnailCache) Get(id string, stored []byte) (Thumbnail, error) {
	if c == nil {
		return MakeThumbnail(stored)
	}
	sum := sha256.Sum256(stored)
	key := id + "\x00" + hex.EncodeToString(sum[:])
	c.mu.Lock()
	if e, ok := c.entries[key]; ok {
		c.tick++
		e.used = c.tick
		thumb := e.thumb
		c.mu.Unlock()
		return thumb, nil
	}
	c.mu.Unlock()

	// Drawn outside the lock: two readers of one image may both draw it, and
	// that costs a decode, where holding the lock would make every other
	// image wait behind this one.
	thumb, err := MakeThumbnail(stored)
	if err != nil {
		return Thumbnail{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(thumb.JPEG) > c.limit {
		return thumb, nil
	}
	if prior, ok := c.entries[key]; ok {
		c.held -= len(prior.thumb.JPEG)
	}
	c.tick++
	c.entries[key] = &thumbEntry{thumb: thumb, used: c.tick}
	c.held += len(thumb.JPEG)
	for c.held > c.limit {
		oldest, oldestUsed := "", uint64(0)
		for k, e := range c.entries {
			if oldest == "" || e.used < oldestUsed {
				oldest, oldestUsed = k, e.used
			}
		}
		c.held -= len(c.entries[oldest].thumb.JPEG)
		delete(c.entries, oldest)
		c.evicted++
		c.lastAt = time.Now()
	}
	return thumb, nil
}

// Reading is the cache's register row: bytes held now, and how many
// thumbnails were let go to stay under the budget.
func (c *ThumbnailCache) Reading() (held int64, entries int, evicted int64, lastAt time.Time) {
	if c == nil {
		return 0, 0, 0, time.Time{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return int64(c.held), len(c.entries), c.evicted, c.lastAt
}
