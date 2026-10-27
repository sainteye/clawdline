package http

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

const transcriptInlineToolBytesLimit = 1200
const transcriptToolPreviewRunesLimit = 240
const transcriptPatchDetailLinesLimit = 18

var transcriptHunk = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+\d+(?:,\d+)? @@`)

func (s *Server) entryDetailScope(id string, item session.Session) string {
	scope := toolResultScope(id, item)
	if scope == "" {
		return ""
	}
	return s.executionMachine() + "\x00" + scope
}

func entryDetailReference(row contract.TranscriptEntry, offset int64, scope string) string {
	raw, _ := json.Marshal(row)
	digest := sha256.Sum256(append([]byte("entry-body-v1\x00"+scope+"\x00"), raw...))
	return strconv.FormatInt(offset, 10) + ":" + hex.EncodeToString(digest[:])
}

func changeStats(change contract.TranscriptFileChange) *contract.TranscriptChangeStats {
	text := change.UnifiedDiff
	unified := text != ""
	if !unified {
		text = change.Content
	}

	count := strings.Count(text, "\n")
	if text != "" && !strings.HasSuffix(text, "\n") {
		count++
	}
	out := &contract.TranscriptChangeStats{LineCount: int64(count)}
	if !unified {
		if change.Kind == "delete" {
			out.Deletions = out.LineCount
		} else {
			out.Additions = out.LineCount
		}
		return out
	}
	inHunk := false
	for rest := text; rest != ""; {
		line, tail, _ := strings.Cut(rest, "\n")
		rest = tail
		if transcriptHunk.MatchString(line) {
			inHunk = true
			continue
		}
		if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "diff ") || strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "\\ No newline") {
			continue
		}
		if inHunk && strings.HasPrefix(line, "+") {
			out.Additions++
		}
		if inHunk && strings.HasPrefix(line, "-") {
			out.Deletions++
		}
	}
	return out
}

// Version 2 only: omit the body a folded tool row or large patch cannot show.
// Questions, plans, activities and formal attachments keep their existing readers.
func compactEntryDetails(row contract.TranscriptEntry, e transcript.Entry, scope string) contract.TranscriptEntry {
	if scope == "" || row.Role != "tool" || row.Activity != nil || len(row.Plan) > 0 || row.Notice != nil || strings.HasPrefix(row.Text, "\x01ask\x01") {
		return row
	}
	total := int64(0)
	patchBytes := 0
	stats := make([]*contract.TranscriptChangeStats, len(row.FileChanges))
	for i, change := range row.FileChanges {
		patchBytes += len(change.UnifiedDiff) + len(change.Content)
		stats[i] = changeStats(change)
		total += stats[i].LineCount
	}
	patch := len(row.FileChanges) > 0
	if patch && total <= transcriptPatchDetailLinesLimit && patchBytes <= transcriptInlineToolBytesLimit || !patch && len(row.Text) <= transcriptInlineToolBytesLimit {
		return row
	}
	original := row
	if patch {
		original.FileChanges = append([]contract.TranscriptFileChange(nil), row.FileChanges...)
	}
	row.Detail = &contract.TranscriptEntryDetail{Reference: entryDetailReference(row, e.Before, scope), TextBytes: int64(len(row.Text)), LineCount: total}
	if patch {
		for i := range row.FileChanges {
			row.FileChanges[i].UnifiedDiff = ""
			row.FileChanges[i].Content = ""
			row.FileChanges[i].Stats = stats[i]
		}
	} else {

		count := 0
		for at := range row.Text {
			if count == transcriptToolPreviewRunesLimit {
				row.Text = row.Text[:at]
				break
			}
			count++
		}
	}
	if patch {
		before, _ := json.Marshal(original)
		after, _ := json.Marshal(row)
		if len(after) >= len(before) {
			return original
		}
	}
	return row
}

func (s *Server) transcriptEntryDetail(w http.ResponseWriter, r *http.Request, id string, item session.Session, view string) {
	parts := strings.Split(view, ":")
	if len(parts) != 3 || parts[0] != "entry-v1" && parts[0] != "entry-v1z" || len(parts[2]) != sha256.Size*2 || item.Assistant != session.AssistantCodex && item.Assistant != session.AssistantClaude {
		writeRefusal(w, 400, "invalid_tool_result", "name a stored tool result and representation")
		return
	}
	offset, err := strconv.ParseInt(parts[1], 10, 64)
	if _, bad := hex.DecodeString(parts[2]); err != nil || offset < 0 || bad != nil {
		writeRefusal(w, 400, "invalid_tool_result", "name a stored tool result and representation")
		return
	}
	scope := s.entryDetailScope(id, item)
	if scope == "" {
		writeRefusal(w, 409, "execution_source_unknown", "the Session execution cannot be verified")
		return
	}
	candidates, err := transcript.ReadRowEntries(s.recordPath(item), string(item.Assistant), offset)
	if err == nil {
		for _, entry := range candidates {
			row := transcriptEntry(entry)
			if row.Role == "tool" && entryDetailReference(row, offset, scope) == parts[1]+":"+parts[2] {
				if !s.admitPinnedTarget(w, r, id) {
					return
				}
				writeJSON(w, map[string]any{"reference": parts[1] + ":" + parts[2], "entry": row})
				return
			}
		}
	}
	writeRefusal(w, 409, "tool_result_stale", "the stored tool result changed or is unavailable; read the conversation again")
}
