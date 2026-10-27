package http

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/documents"
	"github.com/sainteye/clawdline/internal/contract"
)

// Videos share the document locator and root gate, not the inert-text reader.
// A chunk is JSON containing base64 bytes both locally and over encrypted Cloud.
func (s *Server) servePublishedFile(w http.ResponseWriter, r *http.Request, root, scope, task, path string) {
	if !strings.HasSuffix(strings.ToLower(path), ".mp4") {
		s.serveDocument(w, root, path)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	query := r.URL.Query()
	offset, err := strconv.ParseInt(query.Get("offset"), 10, 64)
	valid := err == nil && len(query["offset"]) == 1 && len(query["revision"]) == 1
	for key := range query {
		if key != "offset" && key != "revision" {
			valid = false
		}
	}
	if !valid {
		writeAuthRefusal(w, 400, "video_range_invalid", "Read a video with an explicit chunk offset and revision.")
		return
	}
	chunk, err := documents.ReadVideo(r.Context(), root, path, offset, query.Get("revision"))
	if err != nil {
		status, code, message := documents.VideoRefusal(err)
		writeAuthRefusal(w, status, code, message)
		return
	}
	out := contract.VideoChunk{Scope: contract.DocumentSource(scope), Path: path, MediaType: "video/mp4", TotalBytes: chunk.TotalBytes, Offset: chunk.Offset, Revision: chunk.Revision, ByteCount: int64(chunk.ByteCount), Data: ""}
	// The generated wire type keeps JSON byte-count and identity fields explicit.
	out.Data = base64.StdEncoding.EncodeToString(chunk.Data)
	if scope == "task" {
		out.Task = task
	}
	writeJSON(w, out)
}
