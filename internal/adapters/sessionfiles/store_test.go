package sessionfiles

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/documents"
)

func fixture(t *testing.T) (*Store, string, string) {
	t.Helper()
	cwd := t.TempDir()
	path := filepath.Join(cwd, "plan.md")
	if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	return New(t.TempDir()), cwd, path
}
func writeRows(t *testing.T, path string, rows ...any) {
	t.Helper()
	var b strings.Builder
	for _, row := range rows {
		raw, _ := json.Marshal(row)
		b.Write(raw)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0600); err != nil {
		t.Fatal(err)
	}
}
func claudeReceipt(path, kind string, failed bool) any {
	return map[string]any{"type": "user", "timestamp": "2026-10-29T01:00:00Z", "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "is_error": failed}}}, "toolUseResult": map[string]any{"type": kind, "filePath": path}}
}
func TestOutputsRequireSuccessfulReceiptsAndStayInTheirConversation(t *testing.T) {
	s, cwd, path := fixture(t)
	record := filepath.Join(cwd, "record.jsonl")
	writeRows(t, record, map[string]any{"type": "assistant", "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "name": "Write", "input": map[string]any{"file_path": path}}}}}, claudeReceipt(path, "create", true), claudeReceipt(path, "create", false), claudeReceipt(path, "update", false))
	got, err := s.List(context.Background(), "claude:a", cwd, record, "claude")
	if err != nil || len(got.Files) != 1 || got.Files[0].Updates != 2 || got.Files[0].Kind != "created" {
		t.Fatalf("receipt list: %+v %v", got, err)
	}
	again, err := New(filepath.Dir(s.Dir)).List(context.Background(), "claude:a", cwd, record, "claude")
	if err != nil || len(again.Files) != 1 || again.Files[0].Updates != 2 {
		t.Fatalf("restart/dedup: %+v %v", again, err)
	}
	other, err := s.List(context.Background(), "claude:b", cwd, "", "claude")
	if err != nil || len(other.Files) != 0 {
		t.Fatalf("another conversation inherited outputs: %+v %v", other, err)
	}
	if _, err = s.Read(context.Background(), "claude:b", got.Files[0].ID, 0, ""); err != ErrMissing {
		t.Fatalf("cross-conversation read: %v", err)
	}
}
func TestOutputsNeverInferFromUserTextOrEscapingSymlinks(t *testing.T) {
	s, cwd, path := fixture(t)
	outside := t.TempDir()
	private := filepath.Join(outside, "secret.md")
	os.WriteFile(private, []byte("private"), 0600)
	link := filepath.Join(cwd, "shortcut")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip(err)
	}
	record := filepath.Join(cwd, "record.jsonl")
	writeRows(t, record, map[string]any{"type": "user", "message": map[string]any{"content": "Write " + path}}, claudeReceipt(filepath.Join(link, "secret.md"), "create", false), claudeReceipt(private, "create", false))
	got, err := s.List(context.Background(), "a", cwd, record, "claude")
	if err != nil || len(got.Files) != 0 {
		t.Fatalf("unproven outputs: %+v %v", got, err)
	}
}
func TestOutputsReadIsRevisionBoundedAndRejectsSymlinkReplacement(t *testing.T) {
	s, cwd, path := fixture(t)
	os.WriteFile(path, []byte(strings.Repeat("x", documents.MaximumVideoChunkBytes+2)), 0600)
	file, err := s.Register("a", cwd, path, "published", "published", 1, -1)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Read(context.Background(), "a", file.ID, 0, "")
	if err != nil || len(first.Data) != documents.MaximumVideoChunkBytes {
		t.Fatalf("chunk: %+v %v", len(first.Data), err)
	}
	if _, err = s.Read(context.Background(), "a", file.ID, documents.MaximumVideoChunkBytes, ""); err != ErrRange {
		t.Fatalf("missing revision: %v", err)
	}
	os.WriteFile(path, []byte("changed"), 0600)
	if _, err = s.Read(context.Background(), "a", file.ID, 0, first.Revision); err != ErrChanged {
		t.Fatalf("changed file: %v", err)
	}
	other := filepath.Join(cwd, "private.txt")
	os.WriteFile(other, []byte("secret"), 0600)
	os.Remove(path)
	if err = os.Symlink(other, path); err != nil {
		t.Skip(err)
	}
	if _, err = s.Read(context.Background(), "a", file.ID, 0, ""); err != ErrMissing {
		t.Fatalf("followed replacement symlink: %v", err)
	}
	list, err := s.List(context.Background(), "a", cwd, "", "other")
	if err != nil || len(list.Files) != 1 || list.Files[0].State != "missing" {
		t.Fatalf("lost missing-file record: %+v %v", list, err)
	}
}
func TestOutputsPartialTailAndCodexFailedPatch(t *testing.T) {
	s, cwd, path := fixture(t)
	record := filepath.Join(cwd, "record.jsonl")
	patch := func(success bool) any {
		return map[string]any{"type": "event_msg", "timestamp": "2026-10-29T01:00:00Z", "payload": map[string]any{"type": "patch_apply_end", "success": success, "changes": map[string]any{path: map[string]any{"type": "add"}}}}
	}
	writeRows(t, record, patch(false))
	f, _ := os.OpenFile(record, os.O_APPEND|os.O_WRONLY, 0600)
	raw, _ := json.Marshal(patch(true))
	f.Write(raw[:len(raw)/2])
	f.Close()
	got, err := s.List(context.Background(), "a", cwd, record, "codex")
	if err != nil || len(got.Files) != 0 || !got.More {
		t.Fatalf("partial: %+v %v", got, err)
	}
	f, _ = os.OpenFile(record, os.O_APPEND|os.O_WRONLY, 0600)
	f.Write(raw[len(raw)/2:])
	f.WriteString("\n")
	f.Close()
	got, err = s.List(context.Background(), "a", cwd, record, "codex")
	if err != nil || len(got.Files) != 1 || got.More {
		t.Fatalf("completed: %+v %v", got, err)
	}
}
func TestCorruptIndexIsNotAnEmptySuccess(t *testing.T) {
	s, cwd, _ := fixture(t)
	os.MkdirAll(s.Dir, 0700)
	os.WriteFile(s.path("a"), []byte("{broken"), 0600)
	if _, err := s.List(context.Background(), "a", cwd, "", "other"); err != ErrUnavailable {
		t.Fatalf("corrupt store: %v", err)
	}
}
func TestOutputCapacityRetainsExistingRecords(t *testing.T) {
	s, cwd, path := fixture(t)
	f, err := s.Register("a", cwd, path, "published", "published", 1, -1)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := s.load("a")
	for len(out.Files) < MaximumFiles {
		next := f
		next.ID = digest(intText(int64(len(out.Files))))
		out.Files = append(out.Files, next)
	}
	if err = s.save(out); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(cwd, "extra.txt")
	os.WriteFile(other, []byte("x"), 0600)
	if _, err = s.Register("a", cwd, other, "published", "published", 1, -1); err != ErrFull {
		t.Fatalf("full: %v", err)
	}
	listed, e := s.List(context.Background(), "a", cwd, "", "other")
	if e != nil || !listed.Partial {
		t.Fatalf("full-index receipt was lost: %+v %v", listed, e)
	}
	if _, err = s.Read(context.Background(), "a", f.ID, 0, ""); err != nil {
		t.Fatalf("existing record evicted: %v", err)
	}
}

func TestCodexPatchCallsNeedTheirOwnSuccessfulResult(t *testing.T) {
	s, cwd, path := fixture(t)
	record := filepath.Join(cwd, "record.jsonl")
	call := map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "call_id": "write-1", "input": "*** Begin Patch\n*** Add File: " + path + "\n+x\n*** End Patch"}}
	writeRows(t, record, call)
	got, err := s.List(context.Background(), "a", cwd, record, "codex")
	if err != nil || len(got.Files) != 0 {
		t.Fatalf("unexecuted call became output: %+v %v", got, err)
	}
	f, _ := os.OpenFile(record, os.O_APPEND|os.O_WRONLY, 0600)
	raw, _ := json.Marshal(map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call_output", "call_id": "write-1", "output": "Success. Updated the following files:\nA " + path}})
	f.Write(append(raw, '\n'))
	f.Close()
	got, err = New(filepath.Dir(s.Dir)).List(context.Background(), "a", cwd, record, "codex")
	if err != nil || len(got.Files) != 1 {
		t.Fatalf("successful resumed receipt: %+v %v", got, err)
	}
	again, err := s.List(context.Background(), "a", cwd, record, "codex")
	if err != nil || again.Files[0].Updates != 1 {
		t.Fatalf("replayed receipt: %+v %v", again, err)
	}
}

func TestExplicitPublicationCountsActualFileChangesAndRetainsKind(t *testing.T) {
	s, cwd, path := fixture(t)
	first, err := s.Register("a", cwd, path, "created", "published", 1, -1)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.Register("a", cwd, path, "created", "published", 2, -1)
	if err != nil || again.Updates != 1 {
		t.Fatalf("retry added a false edit: %+v %v", again, err)
	}
	os.WriteFile(path, []byte("different bytes"), 0600)
	changed, err := s.Register("a", cwd, path, "modified", "published", 3, -1)
	if err != nil || changed.Updates != 2 || changed.FirstAt != first.FirstAt || changed.Kind != "created" {
		t.Fatalf("actual change missing: %+v %v", changed, err)
	}
}

func TestNativeHistoryRetainsAlreadyDeletedOutputAndUnknownTime(t *testing.T) {
	s, cwd, path := fixture(t)
	os.Remove(path)
	record := filepath.Join(cwd, "record.jsonl")
	writeRows(t, record, map[string]any{"type": "user", "message": map[string]any{"content": []any{map[string]any{"type": "tool_result"}}}, "toolUseResult": map[string]any{"type": "create", "filePath": path}})
	got, err := s.List(context.Background(), "a", cwd, record, "claude")
	if err != nil || len(got.Files) != 1 || got.Files[0].State != "missing" || got.Files[0].UpdatedAt != 0 {
		t.Fatalf("historical output: %+v %v", got, err)
	}
}
func TestMalformedNativeRowsMakeCoverageIncomplete(t *testing.T) {
	s, cwd, _ := fixture(t)
	record := filepath.Join(cwd, "record.jsonl")
	os.WriteFile(record, []byte("{broken}\n"), 0600)
	got, err := s.List(context.Background(), "a", cwd, record, "codex")
	if err != nil || got.Coverage != "record_incomplete" {
		t.Fatalf("corrupt native rows were an empty success: %+v %v", got, err)
	}
}
