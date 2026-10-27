package cloudops

import (
	"bytes"
	"compress/gzip"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

const ReadPlaintextBytesLimit = 8 << 20

// gzipReadPayload runs before the transport seals and signs. Only an explicit
// versioned view opts in; retained rows and old viewers keep JSON plaintext.
func gzipReadPayload(payload []byte) []byte {
	if len(payload) > int(capacity.Default("cloud.read_plaintext_bytes")) {
		return payload
	}
	var out bytes.Buffer
	out.Write([]byte{'g', 'z', '1', 0})
	writer, _ := gzip.NewWriterLevel(&out, 6)
	if _, err := writer.Write(payload); err != nil {
		return payload
	}
	if writer.Close() != nil || out.Len() >= len(payload) {
		return payload
	}
	return out.Bytes()
}
