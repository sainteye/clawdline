package artifacts

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
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
	PNG    []byte
	Width  int
	Height int
}

// Normalize decodes raster bytes and writes them out again as PNG.
//
// **Re-encoded rather than trusted**, for the Swift app's two reasons: a
// photograph from an iPhone is HEIC and a terminal program asked to read one
// gets a file it does not want, and the cheapest way to be sure bytes that
// arrived over a network are a picture is to draw them — what does not survive
// that was not one.
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
	if _, _, err := image.DecodeConfig(bytes.NewReader(raw)); errors.Is(err, image.ErrFormat) {
		converted, err := platformDecode(ctx, raw)
		if err != nil {
			log.Printf("image: not decoded: %v", err)
			return Normalized{}, unsupported()
		}
		raw = converted
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return Normalized{}, unsupported()
	}
	if cfg.Width > p.MaxDimension || cfg.Height > p.MaxDimension || cfg.Width > p.MaxPixels/cfg.Height {
		return Normalized{}, Refusal{Status: http.StatusRequestEntityTooLarge, Code: "image_too_large",
			Message: "One decoded image exceeds the pixel or dimension limit."}
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return Normalized{}, unsupported()
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return Normalized{}, unsupported()
	}
	if out.Len() > p.MaxEncodedBytes {
		return Normalized{}, Refusal{Status: http.StatusRequestEntityTooLarge, Code: "image_too_large",
			Message: "One normalized image exceeds the per-image byte limit."}
	}
	b := img.Bounds()
	return Normalized{PNG: out.Bytes(), Width: b.Dx(), Height: b.Dy()}, nil
}

func unsupported() Refusal {
	return Refusal{Status: http.StatusUnsupportedMediaType, Code: "unsupported_image",
		Message: "One file was not a supported decodable raster image."}
}

// decodeTimeout bounds the platform decoder. A picture that takes longer than
// this to convert is not one somebody is waiting to send.
const decodeTimeout = 20 * time.Second
