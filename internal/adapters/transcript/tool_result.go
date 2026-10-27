package transcript

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/artifacts"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

const ToolPreviewBytesLimit = 1024

// Image indexes changed when automatic metadata stopped being visible output.
const toolProjectionIdentity = "explicit-images-v1"

// ToolResult is a projection of stored output, never an assertion that the
// provider retained the original unabridged response.
type ToolResult struct {
	Reference          string
	ToolID             string
	Failed             bool
	Preview            string
	TextBytes          int
	RawBytes           int
	Images             int
	UnknownBlocks      int
	SourceTruncated    bool
	text               string
	raw                string
	imageURLs          []string
	rawSourceTruncated bool
}

func projectToolResult(result object) *ToolResult {
	if result == nil {
		return nil
	}
	raw, _ := json.Marshal(result)
	out := &ToolResult{raw: string(raw), RawBytes: len(raw)}
	out.Failed, _ = result.boolean("isError")
	var texts []string
	var visit func(json.RawMessage, bool)
	visit = func(content json.RawMessage, unwrap bool) {
		var blocks []object
		if json.Unmarshal(content, &blocks) != nil {
			texts = append(texts, string(content))
			out.UnknownBlocks++
			return
		}
		for _, block := range blocks {
			kind, _ := block.str("type")
			switch kind {
			case "text":
				value, ok := block.str("text")
				if !ok {
					out.UnknownBlocks++
					texts = append(texts, mustJSON(block))
					continue
				}
				if unwrap {
					var inner object
					if json.Unmarshal([]byte(value), &inner) == nil && inner != nil {
						var nested []object
						if json.Unmarshal(inner["content"], &nested) == nil && nested != nil {
							if failed, _ := inner.boolean("isError"); failed {
								out.Failed = true
							}
							visit(inner["content"], false)
							continue
						}
					}
				}
				texts = append(texts, value)
			case "image":
				data, _ := block.str("data")
				mime, _ := block.str("mimeType")
				if url, ok := completeToolImage(data, mime); ok {
					out.Images++
					out.imageURLs = append(out.imageURLs, url)
				}
			default:
				out.UnknownBlocks++
				texts = append(texts, mustJSON(block))
			}
		}
	}
	visit(result["content"], true)
	out.text = strings.TrimSpace(strings.Join(texts, "\n\n"))
	out.TextBytes = len(out.text)
	out.Preview = ClipUTF8(out.text, int(capacity.Default("session.tool_preview_bytes")))
	// Codex can truncate the native record before this reader sees it.
	out.SourceTruncated = storedContentTruncated(out.text)
	out.rawSourceTruncated = storedContentTruncated(out.raw)
	return out
}
func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

// Automatic toolSurface screenshots stay in raw output only. Explicit image
// content is offered only when the stored raster can actually be decoded.
func completeToolImage(encoded, mime string) (string, bool) {
	if mime != "image/png" && mime != "image/jpeg" && mime != "image/gif" {
		return "", false
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(data) > ReadBudget {
		return "", false
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	p := artifacts.ProductionPolicy
	if err != nil || "image/"+format != mime || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > p.MaxDimension || cfg.Height > p.MaxDimension || cfg.Width > p.MaxPixels/cfg.Height {
		return "", false
	}
	if _, _, err = image.Decode(bytes.NewReader(data)); err != nil {
		return "", false
	}
	return "data:" + mime + ";base64," + encoded, true
}

func storedContentTruncated(value string) bool {
	return strings.Contains(value, " chars truncated") || strings.Contains(value, " tokens truncated")
}

// SourceTruncatedFor scopes the warning to the representation being read.
func (t *ToolResult) SourceTruncatedFor(representation string) bool {
	if representation == "raw" {
		return t.rawSourceTruncated
	}
	return t.SourceTruncated
}

// ClipUTF8 keeps a byte budget without splitting a code point.
func ClipUTF8(value string, budget int) string {
	if len(value) <= budget {
		return value
	}
	for budget > 0 && !utf8.RuneStart(value[budget]) {
		budget--
	}
	return value[:budget]
}
func toolReference(offset int64, raw string) string {
	h := sha256.Sum256([]byte(raw))
	return strconv.FormatInt(offset, 10) + ":" + hex.EncodeToString(h[:])
}

// ReadToolResult reads only the named row in the Session's own record. The
// content hash rejects replaced/edited rows even when an offset is reused.
func ReadToolResult(path, reference, scope string) (*ToolResult, error) {
	parts := strings.Split(reference, ":")
	if len(parts) != 2 || len(parts[1]) != sha256.Size*2 {
		return nil, ErrCursorStale
	}
	offset, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || offset < 0 {
		return nil, ErrCursorStale
	}
	if _, err = hex.DecodeString(parts[1]); err != nil {
		return nil, ErrCursorStale
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, recordError(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, recordError(err)
	}
	if offset >= info.Size() {
		return nil, ErrCursorStale
	}
	if offset > 0 {
		var prev [1]byte
		if _, err = f.ReadAt(prev[:], offset-1); err != nil || prev[0] != '\n' {
			return nil, ErrCursorStale
		}
	}
	r := bufio.NewReader(io.NewSectionReader(f, offset, ReadBudget))
	line, err := r.ReadBytes('\n')
	if err != nil || len(line) > ReadBudget {
		return nil, ErrCursorStale
	}
	entries := codexEntries(bytes.TrimSuffix(line, []byte{'\n'}))
	for _, entry := range entries {
		if a := entry.Activity; a != nil && a.ToolResult != nil {
			result := a.ToolResult
			if toolReference(offset, toolProjectionIdentity+"\x00"+scope+"\x00"+result.ToolID+"\x00"+result.raw) != reference {
				return nil, ErrCursorStale
			}
			result.Reference = reference
			return result, nil
		}
	}
	return nil, fmt.Errorf("%w", ErrNotFound)
}

func (t *ToolResult) Text() string { return t.text }
func (t *ToolResult) Raw() string  { return t.raw }

func (t *ToolResult) ReferenceFor(scope string) string {
	offset, _ := strconv.ParseInt(strings.Split(t.Reference, ":")[0], 10, 64)
	return toolReference(offset, toolProjectionIdentity+"\x00"+scope+"\x00"+t.ToolID+"\x00"+t.raw)
}

func (t *ToolResult) ImageURL(index int) (string, bool) {
	if index < 0 || index >= len(t.imageURLs) {
		return "", false
	}
	return t.imageURLs[index], true
}
