package artifacts

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// Store is `SessionImageArtifactStore`, owned by this daemon: normalized
// pictures (`<id>.png`, or `<id>.jpg` for a photograph) under opaque ids, each
// with a metadata record, expiring after a day and kept as a tombstone for a
// week so "expired" and "never heard of it" stay two different answers.
//
// The directory is an ownership boundary. Every name removed here is built from
// a validated id, and nothing ever follows an input path back to its source.
// The metadata is written in the Swift app's own format, so the two stores can
// be read by the same rules.
type Store struct {
	Dir    string
	Policy Policy

	// What the store let go at its limits, since this process began, and
	// who hears that it stored something (limits N15). Guarded by storeLock.
	evictedCount int64
	evictedBytes int64
	evictedAt    time.Time
	stored       func()
}

// Evicted is the reason a tombstone gives when the picture was let go to make
// room, rather than because its day was up or it was deleted.
const Evicted = "evicted"

// OnStored hands f every successful store, after the store has let go of its
// lock. It is how the capacity register measures this store the moment it
// grows, so the warning that it is filling comes before the store that makes
// it let the oldest go. f must not block.
func (s *Store) OnStored(f func()) {
	storeLock.Lock()
	s.stored = f
	storeLock.Unlock()
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
	// Evicted says an expired picture was let go to make room for newer
	// ones, not because its day was up: the reason a reader is owed.
	Evicted bool
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
	// Reason is why a tombstone is one, when it is not the ordinary expiry:
	// `evicted`. The Swift app's records have no such field; this daemon's
	// store is its own directory, and a record without it reads as before.
	Reason string `json:"reason,omitempty"`
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
		total += len(n.Data)
		prepared = append(prepared, n)
	}
	if total > p.MaxTotalBytes {
		return nil, Refusal{Status: http.StatusRequestEntityTooLarge, Code: "image_too_large",
			Message: "Those normalized images exceed the artifact-store byte limit."}
	}

	storeLock.Lock()
	unlocked := false
	unlock := func() {
		if !unlocked {
			unlocked = true
			storeLock.Unlock()
		}
	}
	defer unlock()
	if err := EnsurePrivateDir(s.Dir); err != nil {
		return nil, storageFailed("Clawdline could not open its image-artifact store.")
	}
	s.pruneLocked(now)
	written := []Stored{}
	for _, item := range prepared {
		a := Artifact{
			ID: newUUID(), MediaType: item.MediaType, ByteCount: len(item.Data),
			Width: item.Width, Height: item.Height, ExpiresAt: now.Unix() + p.TTLSeconds,
		}
		file := s.imagePath(a)
		err := WritePrivate(file, item.Data)
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
	stored := s.stored
	unlock()
	if stored != nil {
		stored()
	}
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
	data, err := readBounded(s.imagePath(found.Artifact), s.Policy.MaxEncodedBytes)
	if err != nil || len(data) != found.Artifact.ByteCount {
		if m, ok := s.readMetadata(id); ok {
			s.tombstoneLocked(m, now)
		}
		return Found{State: Expired}
	}
	found.Data = data
	return found
}

// Liveness is the cheap door: metadata and file existence, never the picture. The
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
	_, statErr := os.Stat(s.imagePath(m.Artifact))
	if m.DeletedAt != nil || now.Unix() >= m.Artifact.ExpiresAt || statErr != nil {
		if m.DeletedAt == nil {
			s.tombstoneLocked(m, now)
		}
		return Found{State: Expired, Evicted: m.Reason == Evicted}
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
			_ = os.Remove(s.imagePath(m.Artifact))
			if m.DeletedAt == nil {
				s.tombstoneLocked(m, now)
			}
		}
	}
	s.pruneTombstonesLocked(now)
}

func (s *Store) tombstoneLocked(m metadata, now time.Time) {
	_ = os.Remove(s.imagePath(m.Artifact))
	at := seconds(now)
	m.DeletedAt = &at
	_ = s.writeMetadata(m)
}

// imagePath is where a picture's bytes are: `<id>.png`, or `<id>.jpg` for a
// photograph. Every picture stored before photographs stayed JPEG is a PNG
// and keeps its name.
func (s *Store) imagePath(a Artifact) string {
	return filepath.Join(s.Dir, a.ID+Extension(a.MediaType))
}

func (s *Store) metadataPath(id string) string { return filepath.Join(s.Dir, id+".json") }

// imageFileID is the id in a picture file's name, and whether the name is one
// this store writes.
func imageFileID(name string) (string, bool) {
	for _, ext := range []string{".png", ".jpg"} {
		if id, ok := strings.CutSuffix(name, ext); ok && IsID(id) {
			return id, true
		}
	}
	return "", false
}

func (s *Store) writeMetadata(m metadata) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return WritePrivate(s.metadataPath(m.Artifact.ID), data)
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
			if id, ok := imageFileID(name); ok {
				// A picture with no record, or one its record names under the
				// other ending, is nobody's.
				if m, ok := s.readMetadata(id); !ok || s.imagePath(m.Artifact) != filepath.Join(s.Dir, name) {
					_ = os.Remove(filepath.Join(s.Dir, name))
				}
			} else if id, ok := strings.CutSuffix(name, ".json"); ok && IsID(id) {
				if _, ok := s.readMetadata(id); !ok {
					_ = os.Remove(s.metadataPath(id))
					for _, ext := range []string{".png", ".jpg"} {
						_ = os.Remove(filepath.Join(s.Dir, id+ext))
					}
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
		_, statErr := os.Stat(s.imagePath(m.Artifact))
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
	// Past a limit the oldest live picture goes, and every one that goes is
	// counted against the limit that made it go, logged, and tombstoned with
	// the reason, so whoever asks for it later is told it was let go to make
	// room rather than that its day was up (limits N15). The register has
	// already heard the store was filling: it measures on every store.
	var byCount, byBytes int64
	for len(live) > 0 && (len(live) > s.Policy.MaxCount || bytes > s.Policy.MaxTotalBytes) {
		oldest := live[0]
		if len(live) > s.Policy.MaxCount {
			byCount++
		} else {
			byBytes++
		}
		live = live[1:]
		bytes -= oldest.Artifact.ByteCount
		oldest.Reason = Evicted
		s.tombstoneLocked(oldest, now)
	}
	if byCount+byBytes > 0 {
		s.evictedCount += byCount
		s.evictedBytes += byBytes
		s.evictedAt = now
		log.Printf("images: let go of %d picture(s) still shown in a conversation to make room (the store keeps %d, %d bytes); asking for one now answers that it was let go",
			byCount+byBytes, s.Policy.MaxCount, s.Policy.MaxTotalBytes)
	}
	s.pruneTombstonesLocked(now)
}

// Readings are the `artifacts.images` and `artifacts.image_bytes` rows: the
// live pictures by count and by bytes, and what each limit has let go.
//
// A directory listing and a stat per picture, never a metadata record read:
// a live picture is one whose `<id>.png` or `<id>.jpg` is still there, because letting one
// go removes the file and keeps the record as a tombstone. A picture whose
// day is up but that nobody has asked for since is still counted: it is
// still taking the room.
func (s *Store) Readings(now time.Time) (count, bytes capacity.Reading) {
	storeLock.Lock()
	defer storeLock.Unlock()
	entries, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		count = capacity.Reading{Known: true, Note: "no picture has been stored on this machine"}
		bytes = count
	} else if err != nil {
		return capacity.Unmeasured(err.Error()), capacity.Unmeasured(err.Error())
	} else {
		count, bytes = capacity.Reading{Known: true}, capacity.Reading{Known: true}
		for _, e := range entries {
			if _, ok := imageFileID(e.Name()); !ok || !e.Type().IsRegular() {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			count.Used++
			bytes.Used += info.Size()
		}
	}
	count.Counters = capacity.Counters{Evicted: s.evictedCount, LastActionAt: s.evictedAt}
	bytes.Counters = capacity.Counters{Evicted: s.evictedBytes, LastActionAt: s.evictedAt}
	return count, bytes
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
