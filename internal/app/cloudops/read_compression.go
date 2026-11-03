package cloudops

import (
	"bytes"
	"compress/gzip"
	"encoding/json"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

const ReadPlaintextBytesLimit = 8 << 20

// gzipReadPayload runs before the transport seals and signs. Only an explicit
// versioned view or negotiated JSON read opts in; old viewers keep JSON plaintext.
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

// Compression is a representation of selected JSON reads, never a new authority
// or a relaxation of their decoders. Media and mutating words are excluded.
func compressedReadWord(word string) bool {
	switch word {
	case "work.v2.cards", "work.v2.items", "work.v2.item", "work.v2.search", "work.v2.proposals", "work.v2.session-todos", "work.v2.human-interventions", "personas", "projects", "project-folders", "schedules", "snippets", "provider-quotas", "capacity", "skills", "info", "sessions.list":
		return true
	}
	return false
}

const readCompressionSuffix = ".encoding.gzip-v1"

func negotiateReadCompression(parsed body) (body, bool) {
	encoding, ok := parsed.str("accept_encoding")
	word, _ := parsed.str("type")
	operation, known := catalog[word]
	if !ok || encoding != "gzip-v1" || !known || !operation.read || !compressedReadWord(word) {
		return parsed, false
	}
	original := parsed
	parsed = make(body, len(original)-1)
	for key, value := range original {
		if key != "accept_encoding" {
			parsed[key] = value
		}
	}
	return parsed, true
}

func compressedReadAnswer(answer Answer) Answer {
	if !answer.Published() {
		return answer
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(answer.Payload, &object) != nil {
		return answer
	}
	answer.Name += readCompressionSuffix
	object["read"], _ = json.Marshal(answer.Name)
	plain, err := json.Marshal(object)
	if err != nil {
		return answer
	}
	answer.Payload = gzipReadPayload(plain)
	return answer
}
