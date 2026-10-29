package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/sessionfiles"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func outputPath(r *http.Request) (string, string, bool) {
	rest, ok := strings.CutPrefix(routePath(r), "/v1/sessions/")
	if !ok {
		return "", "", false
	}
	p := strings.Split(rest, "/")
	if len(p) < 2 || len(p) > 3 || p[0] == "" || p[1] != "outputs" {
		return "", "", false
	}
	file := ""
	if len(p) == 3 {
		file = decodeSegment(p[2])
		if len(file) != 64 {
			return "", "", false
		}
	}
	return decodeSegment(p[0]), file, true
}
func outputKey(item session.Session) string {
	if item.ConversationID == "" {
		return ""
	}
	return string(item.Assistant) + ":" + item.ConversationID
}
func (s *Server) sessionOutputs(w http.ResponseWriter, r *http.Request, id, file string) {
	if r.Method != http.MethodGet && !(r.Method == http.MethodPost && file == "") {
		writeRefusal(w, 405, "method_not_allowed", "Outputs are read with GET and registered with POST.")
		return
	}
	if r.Method == http.MethodPost && !machineAuthed(r) {
		writeRefusal(w, 403, "forbidden", "Only the machine may register Session outputs.")
		return
	}
	if !s.admitPinnedTarget(w, r, id) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	item, err := s.actions().FindForRead(ctx, id)
	if err != nil {
		writeActionRefusal(w, err)
		return
	}
	key := outputKey(item)
	if key == "" {
		writeRefusal(w, 409, "output_identity_unavailable", "The Session conversation identity is unavailable.")
		return
	}
	store := sessionfiles.New(s.cfg.Dir)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodPost {
		var input struct {
			Path string `json:"path"`
			Kind string `json:"kind"`
		}
		decoder := json.NewDecoder(io.LimitReader(r.Body, sendBodyLimit))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || input.Path == "" || (input.Kind != "published" && input.Kind != "created" && input.Kind != "modified") {
			writeRefusal(w, 400, "output_path_invalid", "Register an exact output path and its kind.")
			return
		}
		found, e := store.Register(key, item.CWD, input.Path, input.Kind, "published", 0, -1)
		if e != nil {
			outputRefusal(w, e)
			return
		}
		found.ContentRevision = ""
		found.Root = ""
		found.Relative = ""
		writeJSON(w, found)
		return
	}
	if file == "" {
		answer, e := store.List(ctx, key, item.CWD, s.recordPath(item), string(item.Assistant))
		if e != nil {
			outputRefusal(w, e)
			return
		}
		if !s.admitPinnedTarget(w, r, id) {
			return
		}
		writeJSON(w, answer)
		return
	}
	query := r.URL.Query()
	offset, e := strconv.ParseInt(query.Get("offset"), 10, 64)
	if e != nil || len(query["offset"]) != 1 || len(query["revision"]) != 1 {
		outputRefusal(w, sessionfiles.ErrRange)
		return
	}
	for k := range query {
		if k != "offset" && k != "revision" && k != "size" {
			outputRefusal(w, sessionfiles.ErrRange)
			return
		}
	}
	var answer sessionfiles.Chunk
	if query.Get("size") == "thumb" && len(query["size"]) == 1 && offset == 0 && query.Get("revision") == "" {
		answer, e = store.Thumbnail(ctx, key, file)
	} else if query.Get("size") == "" && len(query["size"]) <= 1 {
		answer, e = store.Read(ctx, key, file, offset, query.Get("revision"))
	} else {
		outputRefusal(w, sessionfiles.ErrRange)
		return
	}
	if e != nil {
		outputRefusal(w, e)
		return
	}
	if !s.admitPinnedTarget(w, r, id) {
		return
	}
	writeJSON(w, answer)
}
func outputRefusal(w http.ResponseWriter, err error) {
	status, code, message := 503, "output_index_unavailable", "The Session output index could not be read or saved."
	switch {
	case errors.Is(err, sessionfiles.ErrFull):
		status, code, message = 409, "output_index_full", "The output index is full. Existing records are retained."
	case errors.Is(err, sessionfiles.ErrInvalid):
		status, code, message = 400, "output_path_invalid", "An output must be an exact regular file."
	case errors.Is(err, sessionfiles.ErrMissing):
		status, code, message = 404, "output_not_found", "The original output file is no longer available."
	case errors.Is(err, sessionfiles.ErrChanged):
		status, code, message = 409, "output_changed", "The output changed while it was being read. Open it again."
	case errors.Is(err, sessionfiles.ErrLarge):
		status, code, message = 413, "output_too_large", "This output exceeds the download size limit."
	case errors.Is(err, sessionfiles.ErrRange):
		status, code, message = 416, "output_range_invalid", "Read an output with an explicit chunk offset and revision."
	}
	writeRefusal(w, status, code, message)
}
