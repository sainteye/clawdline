package http

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/sainteye/clawdline/internal/adapters/artifacts"
	"image"
	"net/http"
	"strconv"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// The opt-in view is also the Cloud reply name. An old daemon refuses this
// field and the viewer retries the original request once.
func transcriptView(view string) (compact bool, known string, valid bool) {
	if view == "" {
		return false, "", true
	}
	parts := strings.Split(view, ":")
	if parts[0] != "tools-v1" && parts[0] != "tools-v1z" && parts[0] != "tools-v2" && parts[0] != "tools-v2z" && parts[0] != "delta-v1" && parts[0] != "delta-v1z" {
		return false, "", false
	}
	if len(parts) == 1 {
		return true, "", true
	}
	if len(parts) != 2 || len(parts[1]) != sha256.Size*2 {
		return false, "", false
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return false, "", false
	}
	return true, parts[1], true
}
func compactToolEntry(row contract.TranscriptEntry, e transcript.Entry, scope string) contract.TranscriptEntry {
	a := e.Activity
	if a == nil || a.ToolResult == nil || a.ToolResult.Reference == "" || scope == "" {
		return row
	}
	t := a.ToolResult
	row.Activity.Result = ""
	row.Activity.ToolResult = &contract.TranscriptToolResult{Reference: t.ReferenceFor(scope), ToolID: t.ToolID, Preview: t.Preview, TextBytes: int64(t.TextBytes), RawBytes: int64(t.RawBytes), Images: int64(t.Images), UnknownBlocks: int64(t.UnknownBlocks), SourceTruncated: t.SourceTruncated}
	return row
}
func transcriptVersion(page contract.TranscriptPage, scope string) string {
	page.Version = ""
	raw, _ := json.Marshal(page)
	hash := sha256.Sum256(append([]byte(scope+"\x00"), raw...))
	return hex.EncodeToString(hash[:])
}
func (s *Server) transcriptToolRead(w http.ResponseWriter, r *http.Request, id string, item session.Session, view string) {
	parts := strings.Split(view, ":")
	if len(parts) != 4 || (parts[0] != "tool-v1" && parts[0] != "tool-v1z") || (parts[3] != "text" && parts[3] != "raw" && !strings.HasPrefix(parts[3], "image-")) || item.Assistant != session.AssistantCodex {
		writeRefusal(w, 400, "invalid_tool_result", "name a stored tool result and representation")
		return
	}
	if toolResultScope(id, item) == "" {
		writeRefusal(w, 409, "execution_source_unknown", "the Session execution cannot be verified")
		return
	}
	result, err := transcript.ReadToolResult(s.recordPath(item), parts[1]+":"+parts[2], toolResultScope(id, item))
	if err != nil {
		writeRefusal(w, 409, "tool_result_stale", "the stored tool result changed or is unavailable; read the conversation again")
		return
	}
	if strings.HasPrefix(parts[3], "image-") {
		s.transcriptToolImage(w, r, id, result, parts[3])
		return
	}
	value := result.Text()
	if parts[3] == "raw" {
		value = result.Raw()
	}
	clipped := transcript.ClipUTF8(value, transcriptBudget)
	if !s.admitPinnedTarget(w, r, id) {
		return
	}
	writeJSON(w, map[string]any{"reference": result.Reference, "representation": parts[3], "text": clipped, "bytes": len(value), "truncated": len(clipped) < len(value), "sourceTruncated": result.SourceTruncatedFor(parts[3])})
}

func toolResultScope(id string, item session.Session) string {
	fingerprint := executionFingerprint(item, liveOf(item))
	if fingerprint == "" {
		return ""
	}
	return id + "\x00" + item.ConversationID + "\x00" + fingerprint
}

// Only complete embedded raster bytes are eligible. Never fetch a URL or
// decode a provider's middle-truncated Base64 as if it were an original.
func (s *Server) transcriptToolImage(w http.ResponseWriter, r *http.Request, id string, result *transcript.ToolResult, variant string) {
	parts := strings.Split(variant, "-")
	if len(parts) != 3 || (parts[2] != "thumb" && parts[2] != "original") {
		writeRefusal(w, 400, "invalid_tool_result", "name an image variant")
		return
	}
	index, err := strconv.Atoi(parts[1])
	url, ok := result.ImageURL(index)
	if err != nil || !ok || !strings.HasPrefix(url, "data:image/") {
		writeRefusal(w, 404, "tool_image_unavailable", "this stored image is unavailable")
		return
	}
	comma := strings.IndexByte(url, ',')
	if comma < 0 || !strings.HasSuffix(url[:comma], ";base64") {
		writeRefusal(w, 404, "tool_image_unavailable", "this stored image is unavailable")
		return
	}
	data, err := base64.StdEncoding.Strict().DecodeString(url[comma+1:])
	if err != nil || len(data) > transcript.ReadBudget {
		writeRefusal(w, 422, "tool_image_unavailable", "the provider did not retain a complete supported image")
		return
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	p := artifacts.ProductionPolicy
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > p.MaxDimension || cfg.Height > p.MaxDimension || cfg.Width > p.MaxPixels/cfg.Height || (format != "jpeg" && format != "png" && format != "gif") {
		writeRefusal(w, 422, "tool_image_unavailable", "the provider did not retain a complete supported image")
		return
	}
	if _, _, err = image.Decode(bytes.NewReader(data)); err != nil {
		writeRefusal(w, 422, "tool_image_unavailable", "the provider did not retain a complete supported image")
		return
	}
	if parts[2] == "thumb" {
		thumb, err := artifacts.MakeThumbnail(data)
		if err != nil || len(thumb.JPEG) > transcriptBudget {
			writeRefusal(w, 422, "tool_image_unavailable", "this stored image could not be drawn small")
			return
		}
		data = thumb.JPEG
		format = "jpeg"
	}
	if !s.admitPinnedTarget(w, r, id) {
		return
	}
	writeJSON(w, map[string]any{"reference": result.Reference, "representation": variant, "dataURL": "data:image/" + format + ";base64," + base64.StdEncoding.EncodeToString(data), "bytes": len(data)})
}
