package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/artifacts"
	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline-go/internal/adapters/transcript"
	"github.com/sainteye/clawdline-go/internal/contract"
	"github.com/sainteye/clawdline-go/internal/domain/capacity"
)

// pictures is everything this daemon keeps of pictures, and the Swift app's
// store it may read.
type pictures struct {
	store      *artifacts.Store
	drops      *artifacts.Drops
	pasteboard artifacts.Pasteboard
	swift      *swiftstore.SessionImages
}

func newPictures(stateDir string) pictures {
	// The two stores run at the capacity register's limits, which are the
	// Swift app's numbers unless CLAWDLINE_NEXT_CAPACITY lowers them.
	store := artifacts.NewStore(stateDir)
	store.Policy.MaxCount = int(CapacityLimit(capacity.ArtifactsImages))
	store.Policy.MaxTotalBytes = int(CapacityLimit(capacity.ArtifactsImageSize))
	drops := artifacts.NewDrops(stateDir)
	drops.Keep = int(CapacityLimit(capacity.ArtifactsDrops))
	return pictures{
		store:      store,
		drops:      drops,
		pasteboard: artifacts.NewPasteboard(),
		swift:      swiftstore.OpenImages(swiftstore.ImageDir()),
	}
}

// changed hands f every picture either store keeps.
func (p pictures) changed(f func()) {
	p.store.OnStored(f)
	p.drops.OnStored(f)
}

// sendBodyLimit is the Swift server's `bodyLimit`: twenty megabytes, which the
// page's own bounds (5 MB a picture, 15 MB together) leave room under.
const sendBodyLimit = 20 << 20

// imageRoute is GET /v1/artifacts/images/{id}: same-origin bytes for a
// reference a transcript already published. The URL carries an opaque id and
// nothing else; no path or file name crosses this route.
//
// This daemon's own store answers first. An id it has never held is asked of
// the Swift app's store, read-only, because transcripts written while that app
// was running point there.
func (s *Server) imageRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "an image is read with GET")
		return
	}
	// One segment, split off the routed string and decoded once (routePath,
	// gate.go): an image id is a name, never a path.
	id := decodeSegment(strings.TrimPrefix(routePath(r), "/v1/artifacts/images/"))
	if id == "" || strings.Contains(id, "/") {
		writeRefusal(w, http.StatusNotFound, "artifact_not_found", "No image artifact named that.")
		return
	}
	now := time.Now()
	found := s.pictures.store.Lookup(id, now)
	data, state := found.Data, found.State
	if state == artifacts.Missing {
		_, bytes, swiftState := s.pictures.swift.Lookup(id, now)
		switch swiftState {
		case swiftstore.ImageLive:
			data, state = bytes, artifacts.Live
		case swiftstore.ImageExpired:
			state = artifacts.Expired
		}
	}
	switch state {
	case artifacts.Live:
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(data)
		}
	case artifacts.Expired:
		if found.Evicted {
			// Same code, so every reader that branches on it still does; the
			// sentence says which of the two happened (limits N15).
			writeRefusal(w, http.StatusGone, "artifact_expired",
				"That image artifact was let go to make room for newer pictures: this Mac keeps the newest "+
					strconv.Itoa(s.pictures.store.Policy.MaxCount)+".")
			return
		}
		writeRefusal(w, http.StatusGone, "artifact_expired", "That image artifact has expired or been pruned.")
	default:
		writeRefusal(w, http.StatusNotFound, "artifact_not_found", "No image artifact named that.")
	}
}

// imagesRoute is POST /v1/artifacts/images: a live session stores pictures to
// show in its own reply, and is handed back the marker to write there.
//
// Only this machine's orchestrator token may, as in the Swift app. A session
// cannot type into its own terminal — that would be handing itself an
// instruction — so this route only stores, and the session's own CLI writes
// the marker into its own transcript, where it is read back like any turn.
func (s *Server) imagesRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "pictures are stored with POST")
		return
	}
	if !machineAuthed(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "Storing a session image needs the orchestrator token.")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10+1))
	if err != nil || len(body) > 64<<10 {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "The closed body needs only images.")
		return
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || len(fields) != 1 || fields["images"] == nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "The closed body needs only images.")
		return
	}
	policy := s.pictures.store.Policy
	paths, err := artifacts.PathsFrom(fields["images"], policy)
	if err != nil {
		writePictureRefusal(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	now := time.Now()
	stored, err := s.pictures.store.ImportPaths(ctx, paths, now)
	if err != nil {
		writePictureRefusal(w, err)
		return
	}
	out := contract.ImageStoreResult{OK: true, At: time.Now().Unix(), Artifacts: []contract.StoredImage{}}
	for _, item := range stored {
		a := item.Artifact
		marker := artifacts.Marker(a.ID)
		if marker == "" {
			ids := make([]string, 0, len(stored))
			for _, st := range stored {
				ids = append(ids, st.Artifact.ID)
			}
			s.pictures.store.Delete(ids, now)
			writeRefusal(w, http.StatusInternalServerError, "encoding_failed",
				"The stored image reference could not be marked.")
			return
		}
		out.Artifacts = append(out.Artifacts, contract.StoredImage{
			ID: a.ID, MediaType: a.MediaType, ByteCount: int64(a.ByteCount),
			Width: int64(a.Width), Height: int64(a.Height), ExpiresAt: a.ExpiresAt, Marker: marker,
		})
	}
	s.recordPictures(ctx, len(stored))
	writeJSON(w, out)
}

func (s *Server) recordPictures(ctx context.Context, n int) {
	raw, _ := json.Marshal(map[string]any{"images": n})
	if s.store != nil {
		_ = s.store.Append(ctx, store.Event{Kind: "artifact.images", Payload: raw})
	}
}

// writePictureRefusal answers a store refusal with the status and code the
// store chose.
func writePictureRefusal(w http.ResponseWriter, err error) {
	if ref, ok := err.(artifacts.Refusal); ok {
		writeRefusal(w, ref.Status, ref.Code, ref.Message)
		return
	}
	writeRefusal(w, http.StatusInternalServerError, "artifact_storage_failed", err.Error())
}

// wireArtifacts is what one entry's pictures are now.
//
// A marker's id is resolved from the stores — this daemon's, then the Swift
// app's — and only from there; the cheap door, metadata and existence, never
// the bytes. An id neither store has is `unknown` and one that ran out is
// `expired`, each as a reference that describes no bytes. A session message's
// pictures are served as its envelope described them, as the Swift app serves
// them; their bytes are asked for by id like any other.
func (p pictures) wireArtifacts(e transcript.Entry, now time.Time) []contract.ImageArtifact {
	if len(e.ArtifactIDs) == 0 && len(e.Artifacts) == 0 {
		return nil
	}
	out := make([]contract.ImageArtifact, 0, len(e.ArtifactIDs)+len(e.Artifacts))
	for _, ref := range e.Artifacts {
		out = append(out, contract.ImageArtifact{
			ID: ref.ID, MediaType: ref.MediaType, ByteCount: int64(ref.ByteCount),
			Width: int64(ref.Width), Height: int64(ref.Height), ExpiresAt: ref.ExpiresAt,
		})
	}
	for _, id := range e.ArtifactIDs {
		out = append(out, p.resolve(id, now))
	}
	return out
}

func (p pictures) resolve(id string, now time.Time) contract.ImageArtifact {
	absent := func(state contract.ImageAbsence) contract.ImageArtifact {
		return contract.ImageArtifact{ID: id, MediaType: "image/png", ExpiresAt: 1, State: state}
	}
	if p.store != nil {
		switch found := p.store.Liveness(id, now); found.State {
		case artifacts.Live:
			a := found.Artifact
			return contract.ImageArtifact{ID: a.ID, MediaType: a.MediaType, ByteCount: int64(a.ByteCount),
				Width: int64(a.Width), Height: int64(a.Height), ExpiresAt: a.ExpiresAt}
		case artifacts.Expired:
			return absent(contract.ImageAbsenceExpired)
		}
	}
	switch rec, state := p.swift.Liveness(id, now); state {
	case swiftstore.ImageLive:
		return contract.ImageArtifact{ID: rec.ID, MediaType: rec.MediaType, ByteCount: int64(rec.ByteCount),
			Width: int64(rec.Width), Height: int64(rec.Height), ExpiresAt: rec.ExpiresAt}
	case swiftstore.ImageExpired:
		return absent(contract.ImageAbsenceExpired)
	}
	return absent(contract.ImageAbsenceUnknown)
}
