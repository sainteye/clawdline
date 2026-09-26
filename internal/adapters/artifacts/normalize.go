package artifacts

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"log"
	"net/http"
	"strings"
	"time"
)

// ErrDecoderUnavailable is what a platform with no decoder for a format says.
// It is answered to the caller as `unsupported_image`, the same refusal a
// corrupt file gets: "this machine cannot read that kind of picture" and "that
// is not a picture" are both answered by not accepting it, and the log keeps
// which one it was.
var ErrDecoderUnavailable = errors.New("this platform has no decoder for that image format")

// DecodeDataURL is `RemoteServer.decodeDataURL`: `data:image/…;base64,AAAA…` to
// the bytes, and nothing else. A `file:` or `http:` here would be somebody
// making this daemon fetch things for them, so anything that is not a base64
// `data:` URL comes back false. Characters outside the alphabet are ignored, as
// Foundation's `ignoreUnknownCharacters` ignores them. The claimed media type is
// never read: it is a string somebody sent.
func DecodeDataURL(text string) ([]byte, bool) {
	if !strings.HasPrefix(text, "data:") {
		return nil, false
	}
	comma := strings.IndexByte(text, ',')
	if comma < 0 || !strings.Contains(text[:comma], ";base64") {
		return nil, false
	}
	body := text[comma+1:]
	clean := make([]byte, 0, len(body))
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/' {
			clean = append(clean, c)
		}
	}
	// Foundation accepts unpadded input once the unknown characters are gone;
	// so does this, by decoding without padding.
	out, err := base64.RawStdEncoding.DecodeString(string(clean))
	if err != nil {
		return nil, false
	}
	return out, true
}

// Normalized is one picture after it has been drawn and written out again.
type Normalized struct {
	// Data is the picture as stored, in MediaType: a photograph is a JPEG, and
	// anything else an 8-bit PNG.
	Data      []byte
	MediaType string
	Width     int
	Height    int
}

// The two media types a normalized picture is written in, and so the only two
// a store here records.
const (
	MediaTypePNG  = "image/png"
	MediaTypeJPEG = "image/jpeg"
)

// Extension is the file name ending for a media type Normalize writes: `.jpg`
// for a JPEG and `.png` for everything else, which is every picture stored
// before photographs stayed JPEG.
func Extension(mediaType string) string {
	if mediaType == MediaTypeJPEG {
		return ".jpg"
	}
	return ".png"
}

// sniffMediaType is the media type of bytes Normalize wrote, read from their
// first bytes: a JPEG starts FF D8 FF, and everything else it writes is a PNG.
func sniffMediaType(data []byte) string {
	if bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}) {
		return MediaTypeJPEG
	}
	return MediaTypePNG
}

// MaxLongEdge is the longest side, in pixels, of any picture this daemon
// stores. It is the console's `LONG_EDGE` (web/console/src/legacy/shots-bridge.ts),
// kept here too because not every picture passes through a browser first: a
// Mac pasteboard, the HEIC fallback that skips the browser's shrink, and a
// marked copy arrive at their own size. A larger picture is scaled down, not
// refused.
const MaxLongEdge = 1600

// photoQuality is the JPEG quality a photograph is written at. It is a
// parameter, not a bound: the console already sent it at 0.82, and 90 keeps a
// second encoding from adding visible loss to the first.
const photoQuality = 90

// Normalize decodes raster bytes and writes them out again: a photograph as
// JPEG, anything else as an 8-bit PNG, with its long edge at most MaxLongEdge.
//
// **Re-encoded rather than trusted**, for the Swift app's two reasons: a
// photograph from an iPhone is HEIC and a terminal program asked to read one
// gets a file it does not want, and the cheapest way to be sure bytes that
// arrived over a network are a picture is to draw them — what does not survive
// that was not one.
//
// **A photograph stays a JPEG, and is always re-encoded.** Writing a JPEG out
// as PNG made a 146 KB phone photo a 1.19 MB 16-bit PNG (Go's PNG encoder has
// no 8-bit case for the YCbCr a JPEG decodes to), and that file is what the
// drops cache, the image store, the Board's blobs and the phone all carried.
// The original bytes are not kept even when they are small enough: the pixels
// are decoded anyway, a terminal program reading the file does not honour an
// EXIF orientation tag, and an original carries its camera metadata — where it
// was taken — into a file handed to an assistant. So the orientation is drawn
// into the pixels and the metadata is left behind. A picture decoded by the
// platform (HEIC, TIFF, WebP…) is a photograph too when it has no
// transparency; one with transparency stays a PNG, because a JPEG has none.
//
// **A screenshot stays PNG, at 8 bits.** PNG and GIF are written as 8-bit
// PNG whatever they decoded to; a 16-bit PNG is reduced to 8 bits, which is
// all a screen shows.
//
// The dimensions are read before the pixels, so a small file that claims forty
// thousand pixels a side is refused before anything is allocated for it.
// Go reads PNG, JPEG and GIF on every platform; anything else goes to the
// platform's own decoder (`sips` on macOS) and is refused where there is none.
func Normalize(ctx context.Context, raw []byte, p Policy) (Normalized, error) {
	if len(raw) == 0 {
		return Normalized{}, unsupported()
	}
	if len(raw) > p.MaxInputBytes {
		return Normalized{}, Refusal{Status: http.StatusRequestEntityTooLarge, Code: "image_too_large",
			Message: "One source image exceeds the per-image byte limit."}
	}
	platform := false
	if _, _, err := image.DecodeConfig(bytes.NewReader(raw)); errors.Is(err, image.ErrFormat) {
		converted, err := platformDecode(ctx, raw)
		if err != nil {
			log.Printf("image: not decoded: %v", err)
			return Normalized{}, unsupported()
		}
		raw, platform = converted, true
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return Normalized{}, unsupported()
	}
	if cfg.Width > p.MaxDimension || cfg.Height > p.MaxDimension || cfg.Width > p.MaxPixels/cfg.Height {
		return Normalized{}, Refusal{Status: http.StatusRequestEntityTooLarge, Code: "image_too_large",
			Message: "One decoded image exceeds the pixel or dimension limit."}
	}
	img, format, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return Normalized{}, unsupported()
	}
	photo := format == "jpeg" || platform && isOpaque(img)
	// The tag is read from the bytes that were decoded: a JPEG's APP1, or the
	// eXIf chunk `sips` carries a HEIC's orientation over into without turning
	// the pixels (measured 2026-09-26: a HEIC tagged 6 came back 64×32 with
	// Orientation 6 beside it).
	orientation := exifOrientation(raw, format)
	if b := img.Bounds(); max(b.Dx(), b.Dy()) > MaxLongEdge {
		w, h := longEdgeSize(b.Dx(), b.Dy(), MaxLongEdge)
		img = scaleDown(img, w, h)
	}
	img = orient(img, orientation)
	var out bytes.Buffer
	mediaType := MediaTypePNG
	if photo {
		mediaType = MediaTypeJPEG
		err = jpeg.Encode(&out, img, &jpeg.Options{Quality: photoQuality})
	} else {
		err = png.Encode(&out, eightBit(img))
	}
	if err != nil {
		return Normalized{}, unsupported()
	}
	if out.Len() > p.MaxEncodedBytes {
		return Normalized{}, Refusal{Status: http.StatusRequestEntityTooLarge, Code: "image_too_large",
			Message: "One normalized image exceeds the per-image byte limit."}
	}
	b := img.Bounds()
	return Normalized{Data: out.Bytes(), MediaType: mediaType, Width: b.Dx(), Height: b.Dy()}, nil
}

// isOpaque answers whether every pixel is fully opaque. Every decoded image
// type in the standard library answers it itself.
func isOpaque(img image.Image) bool {
	if o, ok := img.(interface{ Opaque() bool }); ok {
		return o.Opaque()
	}
	return false
}

// eightBit is img in a type Go's PNG encoder writes at 8 bits a sample. The
// encoder picks 16 bits for any type it has no 8-bit case for — a JPEG's
// YCbCr, a CMYK, a 16-bit PNG's own types — so everything but the four it
// writes at 8 bits is drawn into one that it does.
func eightBit(img image.Image) image.Image {
	switch m := img.(type) {
	case *image.Gray, *image.Paletted, *image.RGBA, *image.NRGBA:
		return m
	case *image.Gray16:
		g := image.NewGray(m.Bounds())
		draw.Draw(g, g.Bounds(), m, m.Bounds().Min, draw.Src)
		return g
	}
	n := image.NewNRGBA(img.Bounds())
	draw.Draw(n, n.Bounds(), img, img.Bounds().Min, draw.Src)
	return n
}

func unsupported() Refusal {
	return Refusal{Status: http.StatusUnsupportedMediaType, Code: "unsupported_image",
		Message: "One file was not a supported decodable raster image."}
}

// decodeTimeout bounds the platform decoder. A picture that takes longer than
// this to convert is not one somebody is waiting to send.
const decodeTimeout = 20 * time.Second
