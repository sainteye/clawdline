package artifacts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Store is `SessionImageArtifactStore`, owned by this daemon: normalized PNGs
// under opaque ids, each with a metadata record, expiring after a day and kept
// as a tombstone for a week so "expired" and "never heard of it" stay two
// different answers.
//
// The directory is an ownership boundary. Every name removed here is built from
// a validated id, and nothing ever follows an input path back to its source.
// The metadata is written in the Swift app's own format, so the two stores can
// be read by the same rules.
type Store struct {
	Dir    string
	Policy Policy
}

// storeLock is process-wide, as the Swift store's is: two Store values over one
// directory are still one directory.
var storeLock sync.Mutex

// NewStore is the store under this daemon's own directory.
func NewStore(stateDir string) *Store {
	return &Store{Dir: filepath.Join(stateDir, "session-images"), Policy: ProductionPolicy}
}

// Stored is one picture the store accepted.
type Stored struct {
	Artifact Artifact
	File     string
}

// State is what a lookup found.
type State int

const (
	Missing State = iota
	Expired
	Live
)

// Found is a lookup's answer. Data is filled only by Lookup.
type Found struct {
	State    State
	Artifact Artifact
	Data     []byte
}

// Marker is the literal a session pastes into its own reply to show a stored
// picture: `<clawdline-image id="…">`, and only that spelling. Empty for
// anything that is not an id, so a route can never hand back a string the
// transcript reader would refuse.
func Marker(id string) string {
	if !IsID(id) {
		return ""
	}
	return MarkerOpening + id + MarkerClosing
}

const (
	MarkerOpening = `<clawdline-image id="`
	MarkerClosing = `">`
)

type metadata struct {
	Artifact  Artifact `json:"artifact"`
	CreatedAt float64  `json:"createdAt"`
	DeletedAt *float64 `json:"deletedAt,omitempty"`
}

// metadataLimit bounds one record. A real one is under 300 bytes.
const metadataLimit = 64 << 10

// PathsFrom reads the `images` array both image-storing routes accept: one
// `{"path": "…"}` object per picture, 1…6 of them, and nothing else.
func PathsFrom(raw json.RawMessage, p Policy) ([]string, error) {
	var images []map[string]json.RawMessage
	bad := Refusal{Status: http.StatusBadRequest, Code: "bad_request",
		Message: "images must be a non-empty bounded array of local paths."}
	if json.Unmarshal(raw, &images) != nil || len(images) == 0 || len(images) > p.MaxImagesPerMessage {
		return nil, bad
	}
	paths := make([]string, 0, len(images))
	for _, image := range images {
		var path string
		field, ok := image["path"]
		if len(image) != 1 || !ok || json.Unmarshal(field, &path) != nil || path == "" {
			return nil, Refusal{Status: http.StatusBadRequest, Code: "bad_request",
				Message: "Each image accepts only one string path field."}
		}
		paths = append(paths, path)
	}
	return paths, nil
}

// ImportPaths prepares every input before writing any output, so a bad second
// picture cannot leave a first one behind, and every refusal happens before
// anything crosses an API boundary.
func (s *Store) ImportPaths(ctx context.Context, paths []string, now time.Time) ([]Stored, error) {
	p := s.Policy
	if len(paths) == 0 || len(paths) > p.MaxImagesPerMessage {
		return nil, Refusal{Status: http.StatusBadRequest, Code: "bad_request",
			Message: "images must contain 1…6 local files."}
	}
	prepared := make([]Normalized, 0, len(paths))
	total := 0
	for _, path := range paths {
		n, err := s.prepare(ctx, path)
		if err != nil {
			return nil, err
		}
		total += len(n.PNG)
		prepared = append(prepared, n)
	}
	if total > p.MaxTotalBytes {
		return nil, Refusal{Status: http.StatusRequestEntityTooLarge, Code: "image_too_large",
			Message: "Those normalized images exceed the artifact-store byte limit."}
	}

	storeLock.Lock()
	defer storeLock.Unlock()
	if err := ensurePrivateDir(s.Dir); err != nil {
		return nil, storageFailed("Clawdline could not open its image-artifact store.")
	}
	s.pruneLocked(now)
	written := []Stored{}
	for _, item := range prepared {
		a := Artifact{
			ID: newUUID(), MediaType: "image/png", ByteCount: len(item.PNG),
			Width: item.Width, Height: item.Height, ExpiresAt: now.Unix() + p.TTLSeconds,
		}
		file := s.imagePath(a.ID)
		err := writePrivate(file, item.PNG)
		if err == nil {
			err = s.writeMetadata(metadata{Artifact: a, CreatedAt: seconds(now)})
			if err != nil {
				_ = os.Remove(file)
			}
		}
		if err != nil {
			// Nothing from a refused batch has been published, so it is removed
			// whole rather than kept as tombstones.
			for _, w := range written {
				_ = os.Remove(w.File)
				_ = os.Remove(s.metadataPath(w.Artifact.ID))
			}
			return nil, storageFailed("Clawdline could not persist the image artifacts.")
		}
		written = append(written, Stored{Artifact: a, File: file})
	}
	s.pruneLocked(now)
	return written, nil
}

func storageFailed(msg string) Refusal {
	return Refusal{Status: http.StatusInternalServerError, Code: "artifact_storage_failed", Message: msg}
}

// prepare is `SessionImageArtifactStore.prepare`: one normalized absolute
// path to one regular file, then the same bounds the send path keeps.
func (s *Store) prepare(ctx context.Context, path string) (Normalized, error) {
	badPath := func(msg string) Refusal {
		return Refusal{Status: http.StatusBadRequest, Code: "invalid_image_path", Message: msg}
	}
	if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return Normalized{}, badPath("Each image path must be one normalized absolute local path.")
	}
	// Cleaning does not resolve links, so macOS's `/private/tmp/…` spelling,
	// which the Swift store accepts as an alias, is already clean here.
	if filepath.Clean(path) != path {
		return Normalized{}, badPath("Each image path must be one normalized absolute local path.")
	}
	// Lstat: a link is not a regular file, and following one is following an
	// input path somewhere else.
	info, err := os.Lstat(path)
	if err != nil {
		return Normalized{}, badPath("One local image file could not be read.")
	}
	if !info.Mode().IsRegular() {
		return Normalized{}, badPath("Each image path must name one regular local file.")
	}
	if info.Size() <= 0 || info.Size() > int64(s.Policy.MaxInputBytes) {
		return Normalized{}, Refusal{Status: http.StatusRequestEntityTooLarge, Code: "image_too_large",
			Message: "One source image exceeds the per-image byte limit."}
	}
	f, err := os.Open(path)
	if err != nil {
		return Normalized{}, badPath("One local image file could not be read.")
	}
	raw, err := io.ReadAll(io.LimitReader(f, int64(s.Policy.MaxInputBytes)+1))
	_ = f.Close()
	if err != nil || len(raw) > s.Policy.MaxInputBytes {
		return Normalized{}, Refusal{Status: http.StatusRequestEntityTooLarge, Code: "image_too_large",
			Message: "One source image exceeds the per-image byte limit."}
	}
	return Normalize(ctx, raw, s.Policy)
}

// Lookup is the bytes for a live id. A file that is not the size its record
// says is not that picture any more, and turns the record into a tombstone.
func (s *Store) Lookup(id string, now time.Time) Found {
	if !IsID(id) {
		return Found{State: Missing}
	}
	storeLock.Lock()
	defer storeLock.Unlock()
	found := s.livenessLocked(id, now)
	if found.State != Live {
		return found
	}
	data, err := readBounded(s.imagePath(id), s.Policy.MaxEncodedBytes)
	if err != nil || len(data) != found.Artifact.ByteCount {
		if m, ok := s.readMetadata(id); ok {
			s.tombstoneLocked(m, now)
		}
		return Found{State: Expired}
	}
	found.Data = data
	return found
}

// Liveness is the cheap door: metadata and file existence, never the PNG. The
// transcript asks it once per marker, so it sweeps nothing.
func (s *Store) Liveness(id string, now time.Time) Found {
	if !IsID(id) {
		return Found{State: Missing}
	}
	storeLock.Lock()
	defer storeLock.Unlock()
	return s.livenessLocked(id, now)
}

func (s *Store) livenessLocked(id string, now time.Time) Found {
	m, ok := s.readMetadata(id)
	if !ok {
		return Found{State: Missing}
	}
	_, statErr := os.Stat(s.imagePath(id))
	if m.DeletedAt != nil || now.Unix() >= m.Artifact.ExpiresAt || statErr != nil {
		if m.DeletedAt == nil {
			s.tombstoneLocked(m, now)
		}
		return Found{State: Expired}
	}
	return Found{State: Live, Artifact: m.Artifact}
}

// Delete turns published references into tombstones.
func (s *Store) Delete(ids []string, now time.Time) {
	storeLock.Lock()
	defer storeLock.Unlock()
	for _, id := range ids {
		if !IsID(id) {
			continue
		}
		if m, ok := s.readMetadata(id); ok {
			_ = os.Remove(s.imagePath(id))
			if m.DeletedAt == nil {
				s.tombstoneLocked(m, now)
			}
		}
	}
	s.pruneTombstonesLocked(now)
}

func (s *Store) tombstoneLocked(m metadata, now time.Time) {
	_ = os.Remove(s.imagePath(m.Artifact.ID))
	at := seconds(now)
	m.DeletedAt = &at
	_ = s.writeMetadata(m)
}

func (s *Store) imagePath(id string) string    { return filepath.Join(s.Dir, id+".png") }
func (s *Store) metadataPath(id string) string { return filepath.Join(s.Dir, id+".json") }

func (s *Store) writeMetadata(m metadata) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return writePrivate(s.metadataPath(m.Artifact.ID), data)
}

func (s *Store) readMetadata(id string) (metadata, bool) {
	if !IsID(id) {
		return metadata{}, false
	}
	data, err := readBounded(s.metadataPath(id), metadataLimit)
	if err != nil {
		return metadata{}, false
	}
	var m metadata
	if json.Unmarshal(data, &m) != nil || m.Artifact.ID != id || !m.Artifact.Valid(s.Policy) {
		return metadata{}, false
	}
	return m, true
}

func (s *Store) allMetadata() []metadata {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil
	}
	out := []metadata{}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		if m, ok := s.readMetadata(id); ok {
			out = append(out, m)
		}
	}
	return out
}

// pruneLocked is `pruneUnlocked`: orphans in the id namespace go, expired and
// missing pictures become tombstones, and the oldest live ones go until the
// count and the bytes are back under the policy.
func (s *Store) pruneLocked(now time.Time) {
	if entries, err := os.ReadDir(s.Dir); err == nil {
		for _, e := range entries {
			name := e.Name()
			if id, ok := strings.CutSuffix(name, ".png"); ok && IsID(id) {
				if _, ok := s.readMetadata(id); !ok {
					_ = os.Remove(s.imagePath(id))
				}
			} else if id, ok := strings.CutSuffix(name, ".json"); ok && IsID(id) {
				if _, ok := s.readMetadata(id); !ok {
					_ = os.Remove(s.metadataPath(id))
					_ = os.Remove(s.imagePath(id))
				}
			}
		}
	}
	records := s.allMetadata()
	live := []metadata{}
	for _, m := range records {
		if m.DeletedAt != nil {
			continue
		}
		_, statErr := os.Stat(s.imagePath(m.Artifact.ID))
		if now.Unix() >= m.Artifact.ExpiresAt || statErr != nil {
			s.tombstoneLocked(m, now)
			continue
		}
		live = append(live, m)
	}
	sort.Slice(live, func(i, j int) bool {
		if live[i].CreatedAt != live[j].CreatedAt {
			return live[i].CreatedAt < live[j].CreatedAt
		}
		return live[i].Artifact.ID < live[j].Artifact.ID
	})
	bytes := 0
	for _, m := range live {
		bytes += m.Artifact.ByteCount
	}
	for len(live) > 0 && (len(live) > s.Policy.MaxCount || bytes > s.Policy.MaxTotalBytes) {
		oldest := live[0]
		live = live[1:]
		bytes -= oldest.Artifact.ByteCount
		s.tombstoneLocked(oldest, now)
	}
	s.pruneTombstonesLocked(now)
}

func (s *Store) pruneTombstonesLocked(now time.Time) {
	at := seconds(now)
	tombstones := func() ([]metadata, int) {
		records := s.allMetadata()
		out := []metadata{}
		for _, m := range records {
			if m.DeletedAt != nil {
				out = append(out, m)
			}
		}
		sort.Slice(out, func(i, j int) bool { return *out[i].DeletedAt < *out[j].DeletedAt })
		return out, len(records)
	}
	dead, _ := tombstones()
	for _, m := range dead {
		boundary := max(float64(m.Artifact.ExpiresAt), *m.DeletedAt) + float64(s.Policy.TombstoneTTLSeconds)
		if at >= boundary {
			_ = os.Remove(s.metadataPath(m.Artifact.ID))
		}
	}
	dead, count := tombstones()
	excess := max(0, count-s.Policy.MaxMetadataCount)
	for _, m := range dead[:min(excess, len(dead))] {
		_ = os.Remove(s.metadataPath(m.Artifact.ID))
	}
}

func seconds(t time.Time) float64 { return float64(t.UnixNano()) / 1e9 }

// readBounded reads a whole file that must not be larger than limit.
func readBounded(path string, limit int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, io.ErrUnexpectedEOF
	}
	return data, nil
}
