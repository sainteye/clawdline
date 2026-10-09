package transcript

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func appendRaw(t *testing.T, path, raw string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(raw); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func texts(entries []Entry) string {
	var out []string
	for _, e := range entries {
		out = append(out, e.Text)
	}
	return strings.Join(out, ",")
}

// A page that read the record whole and then reads only what was appended
// holds what a second whole read would have shown, and a row still being
// written waits until it is whole.
func TestAnAfterReadIsWhatWasAppendedAndNothingHalfWritten(t *testing.T) {
	path := writeRecord(t, claudeRow("user", "first"), claudeRow("user", "second"))
	whole, err := ReadClaudeBefore(path, 200, 0)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if whole.NextAfter != info.Size() {
		t.Fatalf("a whole read's cursor is %d, want the record's end %d", whole.NextAfter, info.Size())
	}
	nothing, err := ReadClaudeAfter(path, 200, whole.NextAfter)
	if err != nil || len(nothing.Entries) != 0 || nothing.NextAfter != whole.NextAfter {
		t.Fatalf("nothing appended: %+v, %v", nothing, err)
	}

	appendRaw(t, path, `{"type":"user","timestamp":"2026-09-16T10:00:01Z","message":{"role":"user","content":"third"}}`+"\n"+
		`{"type":"assistant","timestamp":"2026-09-16T10:00:02Z","message":{"role":"assistant","content":[{"type":"text","text":"fourth"},{"type":"text","text":"fifth"}]}}`+"\n"+
		`{"type":"user","timestamp":"2026-09-16T10:00:03Z","message":{"role":"user","content":"sixth, half`)
	added, err := ReadClaudeAfter(path, 200, whole.NextAfter)
	if err != nil {
		t.Fatal(err)
	}
	if got := texts(added.Entries); got != "third,fourth,fifth" {
		t.Fatalf("appended rows read as %q", got)
	}
	again, _ := ReadClaudeBefore(path, 200, 0)
	if got := texts(append(whole.Entries, added.Entries...)); got != texts(again.Entries) {
		t.Fatalf("merged %q, a whole read says %q", got, texts(again.Entries))
	}
	if added.NextAfter != again.NextAfter || added.NextAfter >= mustSize(t, path) {
		t.Fatalf("the cursor %d passed the half-written row (whole read %d, size %d)", added.NextAfter, again.NextAfter, mustSize(t, path))
	}

	appendRaw(t, path, `"}}`+"\n")
	last, err := ReadClaudeAfter(path, 200, added.NextAfter)
	if err != nil || texts(last.Entries) != "sixth, half" || last.NextAfter != mustSize(t, path) {
		t.Fatalf("the finished row: %+v, %v", last, err)
	}
}

func mustSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

// Every cursor an incremental read cannot answer truthfully is refused, so
// the page falls back to reading the newest page whole.
func TestAStaleAfterCursorIsRefused(t *testing.T) {
	path := writeRecord(t, claudeRow("user", "first"), claudeRow("user", "second"))
	end := mustSize(t, path)
	for name, after := range map[string]int64{"past the end": end + 1, "inside a row": 3, "negative": -1} {
		if _, err := ReadClaudeAfter(path, 200, after); !errors.Is(err, ErrCursorStale) {
			t.Errorf("%s: %v, want ErrCursorStale", name, err)
		}
	}
	if _, err := ReadClaudeAfter(path, 1, 0); !errors.Is(err, ErrCursorStale) {
		t.Errorf("more new entries than one page: %v, want ErrCursorStale", err)
	}
	appendRaw(t, path, `{"type":"user","timestamp":"2026-09-16T10:00:01Z","isMeta":true,"origin":{"kind":"peer","name":"other","body":"hello"},"message":{"role":"user","content":"hello"}}`+"\n")
	if _, err := ReadClaudeAfter(path, 200, end); !errors.Is(err, ErrCursorStale) {
		t.Errorf("a peer message among the new rows: %v, want ErrCursorStale", err)
	}
	if _, err := ReadClaudeAfter(path+".missing", 200, 0); !errors.Is(err, ErrNoRecord) {
		t.Errorf("no record: %v, want ErrNoRecord", err)
	}
}

func TestACodexAfterReadUsesTheSameCursor(t *testing.T) {
	path := writeRecord(t, codexItem(m{"type": "UserMessage", "content": []m{{"type": "text", "text": "first"}}}))
	whole, err := ReadCodexBefore(path, 200, 0)
	if err != nil || whole.NextAfter != mustSize(t, path) {
		t.Fatalf("whole: %+v, %v", whole, err)
	}
	more := writeRecord(t, codexItem(m{"type": "UserMessage", "content": []m{{"type": "text", "text": "second"}}}))
	raw, err := os.ReadFile(more)
	if err != nil {
		t.Fatal(err)
	}
	appendRaw(t, path, string(raw))
	added, err := ReadCodexAfter(path, 200, whole.NextAfter)
	if err != nil || texts(added.Entries) != "second" || added.NextAfter != mustSize(t, path) {
		t.Fatalf("appended: %+v, %v", added, err)
	}
}
