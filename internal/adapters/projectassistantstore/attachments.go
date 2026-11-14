package projectassistantstore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Attachments keeps the pictures sent to Bots, one directory per Bot and one
// file per picture named by its SHA-256, so the same picture sent twice is
// stored once and a message names its pictures by content. The aggregate
// keeps only the digest, media type and size. Deleting a Bot's data removes
// its directory with it.
type Attachments struct{ Dir string }

func (s Attachments) botDir(key pa.BotKey) string {
	sum := sha256.Sum256([]byte(key.MachineID + "\x00" + key.BotID))
	return filepath.Join(s.Dir, hex.EncodeToString(sum[:]))
}

// Put stores data for key and describes it. mediaType must already be one the
// domain accepts; the caller decoded and normalized the picture.
func (s Attachments) Put(key pa.BotKey, mediaType string, data []byte) (pa.Attachment, error) {
	sum := sha256.Sum256(data)
	a := pa.Attachment{ID: hex.EncodeToString(sum[:]), MediaType: mediaType, Bytes: int64(len(data))}
	if err := pa.ValidateSend("", []pa.Attachment{a}); err != nil {
		return a, err
	}
	dir := s.botDir(key)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return a, failure(pa.Invalid, "attachment_store")
	}
	path := filepath.Join(dir, a.ID)
	if info, err := os.Stat(path); err == nil && info.Size() == a.Bytes {
		return a, nil
	}
	f, err := os.CreateTemp(dir, ".attachment-")
	if err != nil {
		return a, failure(pa.Invalid, "attachment_store")
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		return a, failure(pa.Invalid, "attachment_store")
	}
	return a, nil
}

// Read returns a stored picture after checking its size and digest, so a
// changed or missing file is refused rather than sent or shown.
func (s Attachments) Read(key pa.BotKey, a pa.Attachment) ([]byte, error) {
	if err := pa.ValidateSend("", []pa.Attachment{a}); err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Join(s.botDir(key), a.ID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, failure(pa.NotFound, "attachment")
		}
		return nil, failure(pa.Invalid, "attachment")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, pa.MaxAttachmentBytes+1))
	if err != nil || int64(len(data)) != a.Bytes {
		return nil, failure(pa.Invalid, "attachment")
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != a.ID {
		return nil, failure(pa.Invalid, "attachment")
	}
	return data, nil
}

// ReadByID returns a stored picture by digest for display, with its media
// type sniffed from the bytes; the digest is checked as Read checks it.
func (s Attachments) ReadByID(key pa.BotKey, id string) ([]byte, string, error) {
	if !validDigest(id) {
		return nil, "", failure(pa.Invalid, "attachment")
	}
	f, err := os.Open(filepath.Join(s.botDir(key), id))
	if err != nil {
		return nil, "", failure(pa.NotFound, "attachment")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, pa.MaxAttachmentBytes+1))
	if err != nil || len(data) == 0 || len(data) > pa.MaxAttachmentBytes {
		return nil, "", failure(pa.Invalid, "attachment")
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != id {
		return nil, "", failure(pa.Invalid, "attachment")
	}
	mediaType := http.DetectContentType(data)
	switch mediaType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return data, mediaType, nil
	}
	return nil, "", failure(pa.Invalid, "attachment_type")
}

func validDigest(id string) bool {
	if len(id) != 64 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
