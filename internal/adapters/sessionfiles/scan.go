package sessionfiles

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
)

func intText(n int64) string { return strconv.FormatInt(n, 10) }

// Only native success receipts count. Tool calls, user text, shell stdout and
// an mtime near the conversation are not proof that this Session wrote a file.
func scan(ctx context.Context, out *index, cwd, record, provider string) (bool, string, error) {
	coverage := "published_only"
	if provider != "claude" && provider != "codex" {
		return false, coverage, nil
	}
	coverage = "native_and_published"
	if record == "" {
		return false, "record_unavailable", nil
	}
	f, err := os.Open(record)
	if err != nil {
		return false, "record_unavailable", nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return false, "record_unavailable", nil
	}
	// The first complete row is a generation fence for replacement/truncation.
	head := bufio.NewReader(io.LimitReader(f, transcript.ReadBudget))
	first, err := head.ReadBytes('\n')
	if err != nil && err != io.EOF {
		return false, coverage, ErrUnavailable
	}
	prefix := digest(string(first))
	if out.Record != record || out.Offset > info.Size() || (out.Prefix != "" && out.Prefix != prefix) {
		out.Offset = 0
		out.Pending = nil
		out.RecordIncomplete = false
	}
	out.Record = record
	out.Prefix = prefix
	if _, err = f.Seek(out.Offset, io.SeekStart); err != nil {
		return false, coverage, ErrUnavailable
	}
	// At most one registered transcript budget per request. Stop before a
	// partial last row so appending its suffix will make it visible next time.
	raw, err := io.ReadAll(io.LimitReader(f, transcript.ReadBudget))
	if err != nil {
		return false, coverage, ErrUnavailable
	}
	consumed := 0
	for len(raw) > 0 {
		if err = ctx.Err(); err != nil {
			return false, coverage, err
		}
		end := bytes.IndexByte(raw, '\n')
		if end < 0 {
			break
		}
		line := raw[:end]
		if len(bytes.TrimSpace(line)) > 0 && !json.Valid(line) {
			out.RecordIncomplete = true
		}
		anchor := out.Offset + int64(consumed)
		for _, change := range nativeReceipts(line, provider, out) {
			if !safeNativePath(cwd, change.path) {
				continue
			}
			at := change.at
			found, e := publishedFile(cwd, change.path, change.kind, "native", at, anchor)
			if e != nil {
				continue
			}
			if _, e = merge(out, found); e == ErrFull {
				out.Full = true
			} else if e != nil {
				return false, coverage, e
			}
		}
		consumed += end + 1
		raw = raw[end+1:]
	}
	if consumed == 0 && info.Size()-out.Offset >= transcript.ReadBudget {
		return false, "record_row_too_large", nil
	}
	out.Offset += int64(consumed)
	return out.Offset < info.Size(), coverage, nil
}
func safeNativePath(cwd, path string) bool {
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	rel, err := filepath.Rel(cwd, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.HasPrefix(part, ".") || part == "node_modules" || part == "vendor" {
			return false
		}
	}
	// Resolve directories as well as the leaf: a workspace symlink is not an
	// automatic publication grant to the private directory it points into.
	root, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return false
	}
	actual, err := filepath.EvalSymlinks(path)
	if os.IsNotExist(err) {
		parent, e := filepath.EvalSymlinks(filepath.Dir(path))
		if e != nil {
			return false
		}
		actual = filepath.Join(parent, filepath.Base(path))
	} else if err != nil {
		return false
	}
	rel, err = filepath.Rel(root, actual)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

type receipt struct {
	path, kind string
	at         int64
}

func receipts(line []byte, provider string) []receipt {
	var row map[string]json.RawMessage
	if json.Unmarshal(line, &row) != nil {
		return nil
	}
	at := int64(0)
	if t, e := time.Parse(time.RFC3339Nano, str(row, "timestamp")); e == nil {
		at = t.Unix()
	}
	if provider == "claude" {
		if str(row, "type") != "user" {
			return nil
		}
		var result map[string]json.RawMessage
		if json.Unmarshal(row["toolUseResult"], &result) != nil {
			return nil
		}
		var message struct {
			Content []struct {
				Type  string `json:"type"`
				Error bool   `json:"is_error"`
			}
		}
		if json.Unmarshal(row["message"], &message) != nil {
			return nil
		}
		success := false
		for _, b := range message.Content {
			if b.Type == "tool_result" && !b.Error {
				success = true
			}
		}
		if !success {
			return nil
		}
		path := str(result, "filePath")
		if path == "" {
			return nil
		}
		kind := "modified"
		switch str(result, "type") {
		case "create":
			kind = "created"
		case "update":
		default:
			if result["structuredPatch"] == nil {
				return nil
			}
		}
		return []receipt{{path, kind, at}}
	}
	if str(row, "type") != "event_msg" {
		return nil
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(row["payload"], &payload) != nil || str(payload, "type") != "patch_apply_end" || string(payload["success"]) != "true" {
		return nil
	}
	var changes map[string]struct {
		Type string `json:"type"`
		Move string `json:"move_path"`
	}
	if json.Unmarshal(payload["changes"], &changes) != nil {
		return nil
	}
	out := []receipt{}
	for path, c := range changes {
		kind := "modified"
		switch c.Type {
		case "add":
			kind = "created"
		case "update":
		case "delete":
			continue
		default:
			continue
		}
		if c.Move != "" {
			path = c.Move
		}
		out = append(out, receipt{path, kind, at})
	}
	return out
}
func str(obj map[string]json.RawMessage, key string) string {
	var s string
	_ = json.Unmarshal(obj[key], &s)
	return s
}

// Match a direct Codex apply_patch call to its successful native result. Calls
// survive a checkpoint; arbitrary shell output is never treated as a receipt.
func nativeReceipts(line []byte, provider string, out *index) []receipt {
	if provider != "codex" {
		return receipts(line, provider)
	}
	var row map[string]json.RawMessage
	if json.Unmarshal(line, &row) != nil {
		return nil
	}
	if str(row, "type") != "response_item" {
		return receipts(line, provider)
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(row["payload"], &payload) != nil {
		return nil
	}
	typ := str(payload, "type")
	id := str(payload, "call_id")
	if typ == "custom_tool_call" && str(payload, "name") == "apply_patch" && id != "" {
		if out.Pending == nil {
			out.Pending = map[string][]pendingChange{}
		}
		if len(out.Pending) >= MaximumFiles {
			out.Full = true
			return nil
		}
		changes := []pendingChange{}
		for _, line := range strings.Split(str(payload, "input"), "\n") {
			switch {
			case strings.HasPrefix(line, "*** Add File: "):
				changes = append(changes, pendingChange{strings.TrimPrefix(line, "*** Add File: "), "created"})
			case strings.HasPrefix(line, "*** Update File: "):
				changes = append(changes, pendingChange{strings.TrimPrefix(line, "*** Update File: "), "modified"})
			case strings.HasPrefix(line, "*** Move to: ") && len(changes) > 0:
				changes[len(changes)-1].Path = strings.TrimPrefix(line, "*** Move to: ")
			}
			if len(changes) > MaximumFiles {
				out.Full = true
				return nil
			}
		}
		out.Pending[id] = changes
		return nil
	}
	if typ != "custom_tool_call_output" {
		return nil
	}
	changes := out.Pending[id]
	delete(out.Pending, id)
	if len(changes) == 0 || !strings.HasPrefix(str(payload, "output"), "Success. Updated the following files:") {
		return nil
	}
	at := int64(0)
	if t, e := time.Parse(time.RFC3339Nano, str(row, "timestamp")); e == nil {
		at = t.Unix()
	}
	found := []receipt{}
	for _, c := range changes {
		found = append(found, receipt{c.Path, c.Kind, at})
	}
	return found
}
