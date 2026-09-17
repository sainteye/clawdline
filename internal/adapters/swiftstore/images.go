package swiftstore

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// SessionImages is the Swift app's image-artifact store
// (`SessionImageArtifactStore`), read-only.
//
// A transcript written while the Swift app was the one running carries
// `<clawdline-image id="…">` markers and version-2 session messages whose
// pictures live there, and this daemon has no other copy of them. So a marker
// this daemon's own store does not know is looked up here, under this
// package's rules: files are opened O_RDONLY and nothing else. The Swift store
// turns an expired record into a tombstone when it reads one; this reader
// never does — it answers "expired" and leaves the record to its owner.
type SessionImages struct {
	dir string
}

// ImageDir is where the Swift app keeps those pictures: its caches directory
// (`CLAWDLINE_SESSION_IMAGE_DIR` moves it, as it moves the Swift app's).
func ImageDir() string {
	if v := os.Getenv("CLAWDLINE_SESSION_IMAGE_DIR"); v != "" {
		if len(v) > 1 && v[0] == '~' {
			if home, err := os.UserHomeDir(); err == nil {
				v = home + v[1:]
			}
		}
		return v
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "com.tsunamiworks.clawdline", "session-images")
}

// OpenImages prepares a reader. Nothing is opened until an id is asked for,
// and a directory that is not there is an ordinary answer: every id is unknown.
func OpenImages(dir string) *SessionImages { return &SessionImages{dir: dir} }

// ImageRecord is one picture's metadata, in the fields the wire carries.
type ImageRecord struct {
	ID        string `json:"id"`
	MediaType string `json:"mediaType"`
	ByteCount int    `json:"byteCount"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	ExpiresAt int64  `json:"expiresAt"`
}

// ImageState is what the Swift store knows about an id.
type ImageState int

const (
	ImageMissing ImageState = iota
	ImageExpired
	ImageLive
)

type imageMetadata struct {
	Artifact  ImageRecord `json:"artifact"`
	CreatedAt float64     `json:"createdAt"`
	DeletedAt *float64    `json:"deletedAt"`
}

// The Swift store's production bounds, which decide whether a record is a
// valid reference at all (`SessionImageArtifact.isValidReference`).
const (
	swiftImageMaxBytes     = 12 << 20
	swiftImageMaxDimension = 12_000
	swiftImageMaxPixels    = 40_000_000
	swiftImageMetadataMax  = 64 << 10
)

var swiftImageID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Liveness answers from the metadata and the file's existence, without
// reading the picture: the transcript asks this once per marker.
func (s *SessionImages) Liveness(id string, now time.Time) (ImageRecord, ImageState) {
	m, ok := s.metadata(id)
	if !ok {
		return ImageRecord{}, ImageMissing
	}
	if m.DeletedAt != nil || now.Unix() >= m.Artifact.ExpiresAt {
		return ImageRecord{}, ImageExpired
	}
	if info, err := os.Stat(s.imagePath(id)); err != nil || !info.Mode().IsRegular() {
		return ImageRecord{}, ImageExpired
	}
	return m.Artifact, ImageLive
}

// Lookup is the picture itself. A file that is not the size its record says
// is not that picture, and is answered as expired — as the Swift store answers
// it, without the tombstone that store would write.
func (s *SessionImages) Lookup(id string, now time.Time) (ImageRecord, []byte, ImageState) {
	record, state := s.Liveness(id, now)
	if state != ImageLive {
		return ImageRecord{}, nil, state
	}
	f, err := os.Open(s.imagePath(id))
	if err != nil {
		return ImageRecord{}, nil, ImageExpired
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, swiftImageMaxBytes+1))
	if err != nil || len(data) != record.ByteCount {
		return ImageRecord{}, nil, ImageExpired
	}
	return record, data, ImageLive
}

func (s *SessionImages) imagePath(id string) string { return filepath.Join(s.dir, id+".png") }

func (s *SessionImages) metadata(id string) (imageMetadata, bool) {
	if s == nil || s.dir == "" || !swiftImageID.MatchString(id) {
		return imageMetadata{}, false
	}
	f, err := os.Open(filepath.Join(s.dir, id+".json"))
	if err != nil {
		return imageMetadata{}, false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, swiftImageMetadataMax+1))
	if err != nil || len(data) > swiftImageMetadataMax {
		return imageMetadata{}, false
	}
	var m imageMetadata
	if json.Unmarshal(data, &m) != nil || m.Artifact.ID != id {
		return imageMetadata{}, false
	}
	a := m.Artifact
	valid := a.MediaType == "image/png" &&
		a.ByteCount > 0 && a.ByteCount <= swiftImageMaxBytes &&
		a.Width > 0 && a.Height > 0 &&
		a.Width <= swiftImageMaxDimension && a.Height <= swiftImageMaxDimension &&
		a.Width <= swiftImageMaxPixels/a.Height &&
		a.ExpiresAt > 0
	if !valid {
		return imageMetadata{}, false
	}
	return m, true
}
