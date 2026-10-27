package cloudops

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/documents"
)

func publicationPath(value any) (string, bool) {
	if path, ok := documentPath(value); ok {
		return path, true
	}
	path, ok := value.(string)
	if !ok || !strings.HasSuffix(strings.ToLower(path), ".mp4") {
		return "", false
	}
	// Reuse the lexical document rules without widening the inert reader.
	_, ok = documentPath(path[:len(path)-4] + ".txt")
	return path, ok && len(path) <= 512
}
func publicationMaximumBytes(path string) int64 {
	if strings.HasSuffix(strings.ToLower(path), ".mp4") {
		return documents.MaximumVideoBytes
	}
	return documentMaximumBytes
}

func init() {
	register(op{name: "video", read: true, shape: shapeVideo,
		decode: func(b body) (plan, bool) {
			if !b.hasOneOf([]string{"type", "session", "request", "scope", "task", "path", "offset", "revision"}, []string{"type", "session", "request", "scope", "task", "path", "offset", "revision", "expected_generation"}) {
				return plan{}, false
			}
			path, ok := publicationPath(b["path"])
			if !ok || !strings.HasSuffix(strings.ToLower(path), ".mp4") {
				return plan{}, false
			}
			offset, ok := b.integer("offset")
			if !ok || offset < 0 || offset >= documents.MaximumVideoBytes || offset%documents.MaximumVideoChunkBytes != 0 {
				return plan{}, false
			}
			revision, ok := b.str("revision")
			if !ok || len(revision) > 128 || !printable(revision) || (offset > 0 && revision == "") {
				return plan{}, false
			}
			scope, _ := b.str("scope")
			task, ok := b.str("task")
			if !ok || !(scope == "project" && task == "" || scope == "task" && isTaskID(task)) {
				return plan{}, false
			}
			request, ok := requestName(b["request"])
			if !ok {
				return plan{}, false
			}
			p, ok := pinnedSessionPlan(b, "read:"+request)
			if !ok {
				return plan{}, false
			}
			p.request, p.scope, p.task, p.path, p.videoOffset, p.videoRevision = request, scope, task, path, offset, revision
			return p, true
		}, route: func(p plan) LocalRequest {
			route := "/v1/sessions/" + segment(p.target) + "/documents/" + p.scope
			if p.scope == "task" {
				route += "/" + segment(p.task)
			}
			return LocalRequest{Method: "GET", Path: route + "/" + escapedDocumentPath(p.path), Query: map[string]string{"offset": strconv.FormatInt(p.videoOffset, 10), "revision": p.videoRevision}}
		},
	})
}

func shapeVideo(p plan, res LocalResponse) (json.RawMessage, Refusal) {
	invalid := func() (json.RawMessage, Refusal) {
		return nil, Refusal{Status: 502, Code: "video_answer_invalid", Message: "The video chunk did not match the requested file.", Layer: layerRoute}
	}
	if res.ContentType != "application/json" || len(res.Body) > 2*documents.MaximumVideoChunkBytes {
		return invalid()
	}
	var out map[string]any
	if json.Unmarshal(res.Body, &out) != nil {
		return invalid()
	}
	keys := 8
	if p.scope == "task" {
		keys++
	}
	offset, okOffset := jsonInt(out["offset"])
	count, okCount := jsonInt(out["byte_count"])
	total, okTotal := jsonInt(out["total_bytes"])
	revision, okRevision := out["revision"].(string)
	data, okData := out["data"].(string)
	if len(out) != keys || out["scope"] != p.scope || out["path"] != p.path || (p.scope == "task" && out["task"] != p.task) || out["media_type"] != "video/mp4" || !okOffset || offset != p.videoOffset || !okCount || count <= 0 || count > documents.MaximumVideoChunkBytes || !okTotal || total <= offset || total > documents.MaximumVideoBytes || count != min(int64(documents.MaximumVideoChunkBytes), total-offset) || !okRevision || revision == "" || len(revision) > 128 || !printable(revision) || (p.videoRevision != "" && revision != p.videoRevision) || !okData {
		return invalid()
	}
	bytes, err := base64.StdEncoding.Strict().DecodeString(data)
	if err != nil || int64(len(bytes)) != count || base64.StdEncoding.EncodeToString(bytes) != data {
		return invalid()
	}
	return mustJSON(out), Refusal{}
}
