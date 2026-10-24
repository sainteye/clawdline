package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/artifacts"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// pictures is everything this daemon keeps of pictures.
type pictures struct {
	store      *artifacts.Store
	drops      *artifacts.Drops
	pasteboard artifacts.Pasteboard
}

func newPictures(stateDir string) pictures {
	// The two stores run at the capacity register's limits unless
	// CLAWDLINE_NEXT_CAPACITY lowers them. The picture store's are the Swift
	// app's numbers; the drop cache's byte cap is this daemon's own (N16).
	store := artifacts.NewStore(stateDir)
	store.Policy.MaxCount = int(CapacityLimit(capacity.ArtifactsImages))
	store.Policy.MaxTotalBytes = int(CapacityLimit(capacity.ArtifactsImageSize))
	drops := artifacts.NewDrops(stateDir)
	drops.MaxBytes = CapacityLimit(capacity.ArtifactsDrops)
	return pictures{
		store:      store,
		drops:      drops,
		pasteboard: artifacts.NewPasteboard(),
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
	// An r/ content read fixes the Session execution before bytes leave this
	// machine. A reused Session ID cannot lend its image to the old request.
	if r.Header.Get("X-Clawdline-Target-Machine") != "" || r.Header.Get("X-Clawdline-Execution-Generation") != "" {
		session := r.URL.Query().Get("session")
		if session == "" {
			writeRefusal(w, http.StatusBadRequest, "execution_target_required", "An image read needs its Session.")
			return
		}
		if !s.admitPinnedTarget(w, r, session) {
			return
		}
	}
	query := r.URL.Query()
	if values, present := query["size"]; present && (len(values) != 1 || values[0] != "thumb") {
		writeRefusal(w, http.StatusBadRequest, "bad_query", "An image is read whole, or with size=thumb.")
		return
	}
	now := time.Now()
	found := s.pictures.store.Lookup(id, now)
	data, state, mediaType := found.Data, found.State, found.Artifact.MediaType
	switch state {
	case artifacts.Live:
		if query.Get("size") == "thumb" {
			thumbnail, err := s.thumbs.Get(id, data)
			if err != nil {
				writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable", "The image could not be drawn small.")
				return
			}
			data, mediaType = thumbnail.JPEG, "image/jpeg"
		}
		if r.Header.Get("X-Clawdline-Target-Machine") != "" || r.Header.Get("X-Clawdline-Execution-Generation") != "" {
			if !s.admitPinnedTarget(w, r, r.URL.Query().Get("session")) {
				return
			}
		}
		w.Header().Set("Content-Type", mediaType)
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
			writeRawRefusal(w, http.StatusGone, "artifact_expired",
				"That image artifact was let go to make room for newer pictures: this machine keeps the newest "+
					strconv.Itoa(s.pictures.store.Policy.MaxCount)+".")
			return
		}
		writeRefusal(w, http.StatusGone, "artifact_expired", "That image artifact has expired or been pruned.")
	default:
		writeRefusal(w, http.StatusNotFound, "artifact_not_found", "No image artifact named that.")
	}
}

// imagesRoute is POST /v1/artifacts/images: a live session stores pictures to
// show the person, and is handed back their markers.
//
// Only this machine's orchestrator token may, as in the Swift app. A session
// cannot type into its own terminal — that would be handing itself an
// instruction — so this route only stores. With `terminal`, the store keeps
// where the pictures were shown, and the transcript route shows them under
// that Session's next reply (images_shown.go); without it, a picture appears
// only where a reply carries its marker, which a model may summarise away.
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
	if json.Unmarshal(body, &fields) != nil || fields["images"] == nil || len(fields) > 2 ||
		(len(fields) == 2 && fields["terminal"] == nil) {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "The closed body takes images, and a terminal.")
		return
	}
	var session string
	if raw := fields["terminal"]; raw != nil && (json.Unmarshal(raw, &session) != nil || session == "") {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "terminal is a Session's terminal id.")
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
	var shown artifacts.Shown
	if session != "" {
		item, err := s.actions().FindForRead(ctx, session)
		if err != nil {
			writeRefusal(w, http.StatusNotFound, "session_not_found", "No Session on this machine has that terminal id.")
			return
		}
		record := s.recordPath(item)
		offset, ok := recordEnd(record)
		if !ok {
			writeRefusal(w, http.StatusConflict, "session_has_no_record",
				"That Session has no conversation record yet, so there is no reply to show the pictures under.")
			return
		}
		shown = artifacts.Shown{Record: record, Offset: offset}
	}
	now := time.Now()
	stored, err := s.pictures.store.ImportShown(ctx, paths, now, shown)
	if err != nil {
		writePictureRefusal(w, err)
		return
	}
	out := contract.ImageStoreResult{OK: true, At: time.Now().Unix(), Artifacts: []contract.StoredImage{}, Terminal: session}
	if session == "" {
		// An agent that called this route the old way is told, in the answer it
		// reads, that a 200 here has shown the person nothing.
		out.Note = unshownNote
	}
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

// unshownNote is the answer's word to a request that named no Session.
const unshownNote = "Stored, and shown nowhere yet: no terminal was named, so these pictures appear only where a reply " +
	"carries their marker, and a model may keep that prose only as a summary without it. Run `clawdline image send <picture>` " +
	"from the Session instead, or send `terminal` with its terminal id; they then appear under its reply with no marker."

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
	writeRawRefusal(w, http.StatusInternalServerError, "artifact_storage_failed", err.Error())
}

// wireArtifacts is what one entry's pictures are now.
//
// A marker's id is resolved from this daemon's store and only from there; the
// cheap door, metadata and existence, never the bytes. An id the store does
// not have is `unknown` and one that ran out is
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
	return absent(contract.ImageAbsenceUnknown)
}
