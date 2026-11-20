package projectassistant

import (
	"regexp"
	"unicode/utf8"
)

const (
	MaxInputBytes      = 100 << 20
	MaxInputChunkBytes = 384 << 10
)

// InputFile is durable sent metadata. It contains no client-supplied path.
type InputFile struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

var uploadID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func ValidUploadID(id string) bool { return uploadID.MatchString(id) }

func ValidateInput(f InputFile) error {
	if !ValidUploadID(f.ID) || !OutputName(f.Name) || !utf8.ValidString(f.Name) || f.Bytes < 0 || f.Bytes > MaxInputBytes || !attachmentID.MatchString(f.SHA256) {
		return fail(Invalid, "input_file")
	}
	return nil
}

func ValidateInputSend(text string, pictures []Attachment, ids []string) error {
	if len(ids) == 0 {
		return ValidateSend(text, pictures)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !ValidUploadID(id) || seen[id] {
			return fail(Invalid, "upload_id")
		}
		seen[id] = true
	}
	// Preserve every picture validation, including size and type. A
	// files-only message has no text but still obeys the text byte bound.
	if len(pictures) == 0 {
		return validateUserText(text, false)
	}
	return ValidateSend(text, pictures)
}
