package transcript

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const syntheticAntigravityID = "70000000-0000-4000-8000-000000000001"

func antigravityTestPath(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	path, ok := AntigravityPath(home, syntheticAntigravityID)
	if !ok {
		t.Fatal("synthetic native path refused")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	return home, path
}

func antigravityTestRow(index int, source, typ, content string, extra map[string]any) string {
	row := map[string]any{"step_index": index, "source": source, "type": typ, "status": "DONE", "created_at": "2026-10-01T12:00:00Z"}
	if content != "" {
		row["content"] = content
	}
	for k, v := range extra {
		row[k] = v
	}
	data, _ := json.Marshal(row)
	return string(data) + "\n"
}

func antigravityTestUser(index int, text string) string {
	return antigravityTestRow(index, "USER_EXPLICIT", "USER_INPUT", "<USER_REQUEST>\n"+text+"\n</USER_REQUEST>\n<ADDITIONAL_METADATA>private-metadata</ADDITIONAL_METADATA>\n<USER_SETTINGS_CHANGE>private-settings</USER_SETTINGS_CHANGE>", nil)
}

func antigravityTestPlanner(index int, text string, input, output, cache int64) string {
	return antigravityTestRow(index, "MODEL", "PLANNER_RESPONSE", text, map[string]any{"input_tokens": input, "output_tokens": output, "cache_read_tokens": cache})
}

func writeAntigravityTest(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAntigravityNativeRolesToolsAndPrivacy(t *testing.T) {
	_, path := antigravityTestPath(t)
	data := antigravityTestUser(0, "請檢查範例 <node> 程式碼") +
		antigravityTestRow(1, "SYSTEM_SDK", "SYSTEM_MESSAGE", "private-hook", nil) +
		antigravityTestRow(2, "SYSTEM", "SYSTEM_MESSAGE", "private-system", nil) +
		antigravityTestRow(3, "MODEL", "PLANNER_RESPONSE", "Inspecting the sample.", map[string]any{"thinking": "private-reasoning", "tool_calls": []any{map[string]any{"name": "edit_file", "args": map[string]any{"Path": "sample.txt", "Target": "old", "Replacement": "new"}}}}) +
		antigravityTestRow(4, "MODEL", "GENERIC", "tool output retained", map[string]any{"status": "INVALID", "error": "synthetic invalid argument"}) +
		antigravityTestRow(5, "MODEL", "PLANNER_RESPONSE", "The native tool failed.", nil)
	writeAntigravityTest(t, path, data)
	page, err := ReadAntigravityBefore(path, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 5 {
		t.Fatalf("entries=%+v", page.Entries)
	}
	if page.Entries[0].Kind != KindUser || page.Entries[0].Text != "請檢查範例 <node> 程式碼" {
		t.Fatalf("user=%+v", page.Entries[0])
	}
	call, result := page.Entries[2], page.Entries[3]
	if call.Kind != KindTool || call.Tool != "edit_file" || len(call.FileChanges) != 0 || *call.Activity.Status != "pending" {
		t.Fatalf("planner implied success: %+v", call)
	}
	if result.Kind != KindToolResult || !strings.Contains(result.Text, "tool output retained") || !strings.Contains(result.Text, "synthetic invalid argument") || *result.Activity.Status != "INVALID" {
		t.Fatalf("native failure lost: %+v", result)
	}
	if call.Before >= result.Before {
		t.Fatal("native ordering lost")
	}
	native, err := ReadAntigravityNativeBefore(path, 100, 0)
	if err != nil || len(native.Native) != 5 {
		t.Fatalf("native identities=%+v %v", native, err)
	}
	for i, want := range []int64{0, 3, 3, 4, 5} {
		if native.Native[i].StepIndex != want {
			t.Fatalf("entry %d lost native step %d: %+v", i, want, native.Native[i])
		}
	}
	if native.Native[3].Source != "MODEL" || native.Native[3].Type != "GENERIC" || native.Native[3].Status != "INVALID" {
		t.Fatalf("invented native role: %+v", native.Native[3])
	}
	for _, e := range page.Entries {
		for _, secret := range []string{"private-hook", "private-system", "private-reasoning", "private-metadata", "private-settings"} {
			if strings.Contains(e.Text, secret) {
				t.Fatalf("leaked %s", secret)
			}
		}
	}
}

func TestAntigravityEnvelopeNeverFallsBackToRawContext(t *testing.T) {
	for _, content := range []string{"no wrapper: private metadata", "<USER_REQUEST>unfinished private metadata", "<USER_REQUEST>one</USER_REQUEST><USER_REQUEST>two</USER_REQUEST>", "<USER_REQUEST>one<USER_REQUEST>two</USER_REQUEST>", "<USER_REQUEST>one</USER_REQUEST><ADDITIONAL_METADATA>unfinished"} {
		t.Run(content, func(t *testing.T) {
			_, path := antigravityTestPath(t)
			writeAntigravityTest(t, path, antigravityTestRow(0, "USER_EXPLICIT", "USER_INPUT", content, nil))
			_, err := ReadAntigravityBefore(path, 100, 0)
			var typed *AntigravityRecordError
			if !errors.As(err, &typed) || typed.Code != "user_request_invalid" {
				t.Fatalf("unsafe envelope accepted: %v", err)
			}
		})
	}
}

func TestAntigravityUsageIsPerPlannerAndUnknownStaysAbsent(t *testing.T) {
	_, path := antigravityTestPath(t)
	data := antigravityTestUser(0, "count the sample") + antigravityTestPlanner(1, "one", 5, 3, 2) + antigravityTestRow(2, "MODEL", "GENERIC", "result", map[string]any{"input_tokens": 900}) + antigravityTestPlanner(3, "two", 7, 4, 0)
	writeAntigravityTest(t, path, data)
	for i := 0; i < 2; i++ {
		facts, err := NewRecordFacts().Read(path, "antigravity")
		if err != nil {
			t.Fatal(err)
		}
		if facts.Usage == nil || facts.Usage.Total != 21 || facts.Usage.Input != 12 || facts.Usage.Output != 7 || facts.Usage.CacheRead != 2 || facts.Usage.HasCost || facts.Model != "" || facts.Context != nil || facts.Fill != nil {
			t.Fatalf("facts=%+v usage=%+v", facts, facts.Usage)
		}
	}
	writeAntigravityTest(t, path, antigravityTestPlanner(0, "zero", 0, 0, 0))
	info, err := ReadAntigravityInfo(path)
	if err != nil || info.Usage == nil || info.Usage.Total != 0 || info.Usage.HasCost {
		t.Fatalf("known zero lost: %+v %v", info, err)
	}
	writeAntigravityTest(t, path, data+antigravityTestRow(4, "MODEL", "PLANNER_RESPONSE", "missing counters", nil))
	info, err = ReadAntigravityInfo(path)
	if err != nil || info.Usage != nil {
		t.Fatalf("incomplete usage became a total: %+v %v", info, err)
	}
	writeAntigravityTest(t, path, antigravityTestPlanner(0, "overflow", math.MaxInt64, 1, 0))
	info, err = ReadAntigravityInfo(path)
	if err != nil || info.Usage != nil {
		t.Fatalf("overflow fabricated usage: %+v %v", info, err)
	}
}

func TestAntigravityCursorsFenceEveryOldByteAndFileGeneration(t *testing.T) {
	_, path := antigravityTestPath(t)
	initial := antigravityTestUser(0, "one") + antigravityTestPlanner(1, "two", 1, 1, 0)
	writeAntigravityTest(t, path, initial)
	whole, err := ReadAntigravityBefore(path, 1, 0)
	if err != nil || len(whole.Entries) != 1 || whole.NextBefore == 0 {
		t.Fatalf("newest=%+v %v", whole, err)
	}
	older, err := ReadAntigravityBefore(path, 1, whole.NextBefore, whole.Signature)
	if err != nil || len(older.Entries) != 1 || older.Entries[0].Text != "one" {
		t.Fatalf("older=%+v %v", older, err)
	}
	if _, err := ReadAntigravityBefore(path, 1, whole.NextBefore); !errors.Is(err, ErrCursorStale) {
		t.Fatalf("bare before authorized: %v", err)
	}
	writeAntigravityTest(t, path, initial+antigravityTestPlanner(2, "three", 1, 1, 0))
	added, err := ReadAntigravityAfter(path, 20, whole.NextAfter, whole.Signature)
	if err != nil || len(added.Entries) != 1 || added.Entries[0].Text != "three" {
		t.Fatalf("append=%+v %v", added, err)
	}
	for _, change := range []string{strings.Replace(initial, "one", "ONE", 1), initial[:len(initial)/2], initial + "{"} {
		writeAntigravityTest(t, path, change)
		if _, err := ReadAntigravityAfter(path, 20, whole.NextAfter, whole.Signature); err == nil {
			t.Fatal("rewrite/truncation/partial authorized old cursor")
		}
	}
	writeAntigravityTest(t, path, initial)
	fresh, err := ReadAntigravityBefore(path, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	writeAntigravityTest(t, path+".replacement", initial)
	if err := os.Rename(path+".replacement", path); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAntigravityAfter(path, 100, fresh.NextAfter, fresh.Signature); !errors.Is(err, ErrCursorStale) {
		t.Fatalf("replacement authorized: %v", err)
	}
	if _, err := ReadAntigravityAfter(path, 100, 0); !errors.Is(err, ErrCursorStale) {
		t.Fatalf("bare after authorized: %v", err)
	}
}

func TestAntigravityMalformedAndOversizedRecordsAreTyped(t *testing.T) {
	for name, data := range map[string]string{
		"partial":          antigravityTestUser(0, "one") + "{",
		"unterminated-row": strings.TrimSuffix(antigravityTestUser(0, "one"), "\n"),
		"json":             "not-json\n",
		"duplicate":        antigravityTestUser(0, "one") + antigravityTestPlanner(0, "two", 1, 1, 0),
		"oversized":        strings.Repeat("x", ReadBudget+1),
		"negative":         antigravityTestPlanner(0, "two", -1, 1, 0),
	} {
		t.Run(name, func(t *testing.T) {
			_, path := antigravityTestPath(t)
			writeAntigravityTest(t, path, data)
			page, err := ReadAntigravityBefore(path, 100, 0)
			var typed *AntigravityRecordError
			if !errors.As(err, &typed) || len(page.Entries) != 0 {
				t.Fatalf("not typed unreadable: %+v %v", page, err)
			}
		})
	}
}

func TestAntigravityPathRefusesTraversalAndSymlinkEscapes(t *testing.T) {
	home, path := antigravityTestPath(t)
	for _, id := range []string{"", "../escape", syntheticAntigravityID + "/x", "70000000-0000-4000-8000-00000000000g"} {
		if got, ok := AntigravityPath(home, id); ok || got != "" {
			t.Fatalf("unsafe id=%q path=%q", id, got)
		}
	}
	writeAntigravityTest(t, path, antigravityTestUser(0, "one"))
	outside := filepath.Join(t.TempDir(), "foreign.jsonl")
	writeAntigravityTest(t, outside, antigravityTestUser(0, "foreign"))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if got, ok := AntigravityPath(home, syntheticAntigravityID); ok || got != "" {
		t.Fatalf("symlink resolved: %q", got)
	}
	if _, err := ReadAntigravityInfo(path); err == nil {
		t.Fatal("read foreign symlink")
	}
	if _, err := ReadAntigravityInfo(outside); err == nil {
		t.Fatal("read non-native path")
	}
}

func TestAntigravityDirectorySymlinksNeverAuthorizeReads(t *testing.T) {
	for depth := 0; depth < 6; depth++ {
		t.Run(strconv.Itoa(depth), func(t *testing.T) {
			home, path := antigravityTestPath(t)
			writeAntigravityTest(t, path, antigravityTestUser(0, "owned"))
			component := filepath.Dir(path)
			for i := 0; i < depth; i++ {
				component = filepath.Dir(component)
			}
			if err := os.Rename(component, component+".saved"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(t.TempDir(), component); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if _, ok := AntigravityPath(home, syntheticAntigravityID); ok {
				t.Fatal("directory symlink accepted")
			}
			if _, err := ReadAntigravityInfo(path); err == nil {
				t.Fatal("directory escape read")
			}
		})
	}
}

func TestAntigravityReadErrorsKeepAbsenceAndPermissionDistinct(t *testing.T) {
	home, path := antigravityTestPath(t)
	if _, err := ReadAntigravityInfo(path); !errors.Is(err, ErrNoRecord) {
		t.Fatalf("absence=%v", err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	writeAntigravityTest(t, path, antigravityTestUser(0, "owned"))
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	_, err := ReadAntigravityInfo(path)
	var unreadable *UnreadableError
	if !errors.As(err, &unreadable) || errors.Is(err, ErrNoRecord) || strings.Contains(err.Error(), home) || strings.Contains(err.Error(), filepath.Base(path)) {
		t.Fatalf("permission error not safe: %v", err)
	}
}

func TestAntigravityActivityUsesNativeTimeNotTouches(t *testing.T) {
	_, path := antigravityTestPath(t)
	writeAntigravityTest(t, path, antigravityTestUser(0, "one"))
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}
	info, err := ReadAntigravityInfo(path)
	if err != nil || info.LastActivity.Format(time.RFC3339) != "2026-10-01T12:00:00Z" {
		t.Fatalf("activity=%+v %v", info, err)
	}
}

func TestAntigravityFactsCacheInvalidatesSameSizePreservedMtimeRewrite(t *testing.T) {
	_, path := antigravityTestPath(t)
	writeAntigravityTest(t, path, antigravityTestPlanner(0, "one", 10, 1, 0))
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	cache := NewRecordFacts()
	first, err := cache.Read(path, "antigravity")
	if err != nil || first.Usage == nil || first.Usage.Input != 10 {
		t.Fatalf("initial=%+v %v", first, err)
	}
	writeAntigravityTest(t, path, antigravityTestPlanner(0, "one", 90, 1, 0))
	if err := os.Chtimes(path, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	second, err := cache.Read(path, "antigravity")
	if err != nil || second.Usage == nil || second.Usage.Input != 90 {
		t.Fatalf("stale same-size usage=%+v %v", second, err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			facts, err := cache.Read(path, "antigravity")
			if err != nil || facts.Usage == nil || facts.Usage.Input != 90 {
				t.Errorf("concurrent facts=%+v %v", facts, err)
			}
		}()
	}
	wg.Wait()
	facts, err := AntigravityFacts(path, "synthetic-observed-model")
	if err != nil || facts.Model != "synthetic-observed-model" || facts.Usage.Model != facts.Model || facts.Usage.HasCost || facts.Context != nil {
		t.Fatalf("observed model facts=%+v %v", facts, err)
	}
}
