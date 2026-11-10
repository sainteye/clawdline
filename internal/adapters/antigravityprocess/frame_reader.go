package antigravityprocess

import (
	"bufio"
	"errors"
	"io"
	"strconv"
)

// MinElidedBase64Run is the shortest run of base64 characters inside a JSON
// string that a received frame drops. A native image Read repeats the whole
// picture as base64 in rawOutput and in an image block; nothing here decodes
// or shows those bytes, so they are omitted while the line is read and never
// count against MaxFrameBytes, the transcript, or the wire trace.
const MinElidedBase64Run = 8 << 10

var errFrameTooLong = errors.New("frame too long")

// frameReader reads newline-delimited JSON-RPC frames. It keeps at most
// MinElidedBase64Run bytes of any run it may still drop, so memory stays
// within max plus one run however large the provider's line is.
type frameReader struct {
	r   *bufio.Reader
	max int
	out []byte
}

func newFrameReader(r io.Reader, max int) *frameReader {
	return &frameReader{r: bufio.NewReaderSize(r, 64<<10), max: max}
}

func base64Byte(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/' || c == '='
}

// next returns one frame without its newline. A run is only ever replaced
// inside a JSON string and never contains a quote or backslash other than an
// escaped slash, so the frame stays the same JSON with a shorter string.
func (f *frameReader) next() ([]byte, error) {
	f.out = f.out[:0]
	inString := false
	runStart, runBytes := -1, 0
	endRun := func() {
		if runStart >= 0 && runBytes >= MinElidedBase64Run {
			f.out = append(f.out[:runStart], "[clawdline omitted "+strconv.Itoa(runBytes)+" base64 characters]"...)
		}
		runStart, runBytes = -1, 0
	}
	addRun := func(b ...byte) {
		if runStart < 0 {
			runStart = len(f.out)
		}
		if runBytes < MinElidedBase64Run {
			f.out = append(f.out, b...)
		}
		runBytes += len(b)
	}
	for {
		c, err := f.r.ReadByte()
		if err != nil {
			endRun()
			if err == io.EOF && len(f.out) > 0 {
				return f.out, f.check()
			}
			return nil, err
		}
		if c == '\n' {
			endRun()
			return f.out, f.check()
		}
		switch {
		case !inString:
			inString = c == '"'
			f.out = append(f.out, c)
		case c == '"':
			endRun()
			inString = false
			f.out = append(f.out, c)
		case c == '\\':
			if next, err := f.r.Peek(1); err == nil && next[0] == '/' {
				_, _ = f.r.ReadByte()
				addRun('\\', '/')
				continue
			}
			endRun()
			f.out = append(f.out, c)
			if next, err := f.r.ReadByte(); err == nil && next != '\n' {
				f.out = append(f.out, next)
			} else if err == nil {
				return f.out, f.check()
			}
		case base64Byte(c):
			addRun(c)
		default:
			endRun()
			f.out = append(f.out, c)
		}
		if len(f.out) > f.max+MinElidedBase64Run {
			return nil, errFrameTooLong
		}
	}
}

func (f *frameReader) check() error {
	if len(f.out) > f.max {
		return errFrameTooLong
	}
	return nil
}
