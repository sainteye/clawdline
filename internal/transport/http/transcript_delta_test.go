package http

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func deltaPage(texts ...string) contract.TranscriptPage {
	p := contract.TranscriptPage{ID: "session", Evidence: contract.EvidenceTranscript, Signature: "revision", Entries: []contract.TranscriptEntry{}}
	for _, text := range texts {
		p.Entries = append(p.Entries, contract.TranscriptEntry{Role: "assistant", Text: text})
	}
	p.Version = transcriptVersion(p, "scope")
	return p
}
func applyTestDelta(t *testing.T, base contract.TranscriptPage, patch contract.TranscriptDelta) contract.TranscriptPage {
	t.Helper()
	if patch.BaseVersion != base.Version {
		t.Fatal("wrong baseline")
	}
	rows := []contract.TranscriptEntry{}
	cursor := int64(0)
	for _, edit := range patch.Delta {
		rows = append(rows, base.Entries[cursor:edit.Start]...)
		rows = append(rows, edit.Entries...)
		cursor = edit.Start + edit.DeleteCount
	}
	rows = append(rows, base.Entries[cursor:]...)
	result := patch.Page
	result.Entries = rows
	return result
}
func TestTranscriptDeltaReproducesWholePagesAcrossEditsAndRepeats(t *testing.T) {
	for _, tc := range []struct {
		name          string
		before, after []string
	}{
		{"append", []string{"a", "b"}, []string{"a", "b", "c"}},
		{"identical append", []string{"same", "same"}, []string{"same", "same", "same"}},
		{"tool completion", []string{"before", "pending", "after"}, []string{"before", "complete", "after"}},
		{"history rewrite", []string{"old", "second"}, []string{"replacement", "second"}},
		{"remove", []string{"a", "b", "c"}, []string{"a", "c"}},
		{"rolling window", []string{"a", "b", "c"}, []string{"b", "c", "d"}},
		{"reorder", []string{"a", "b", "c"}, []string{"c", "b", "a"}},
		{"empty", []string{"a"}, []string{}},
		{"same", []string{"a"}, []string{"a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, page := deltaPage(tc.before...), deltaPage(tc.after...)
			got := applyTestDelta(t, base, transcriptDelta(base, page))
			if !reflect.DeepEqual(got, page) {
				t.Fatalf("patch differs from whole page: %+v", got)
			}
		})
	}
	// Metadata and artifact updates must participate even when visible text matches.
	base, page := deltaPage("same"), deltaPage("same")
	page.Entries[0].Activity = &contract.TranscriptActivity{Kind: "called", Status: "completed"}
	page.Signature = "new revision"
	page.NextBefore = 123
	page.Version = transcriptVersion(page, "scope")
	if got := applyTestDelta(t, base, transcriptDelta(base, page)); !reflect.DeepEqual(got, page) {
		t.Fatal("completion or cursor metadata lost")
	}
}
func TestTranscriptBaselinesEvictWithoutLosingFullReadFallback(t *testing.T) {
	s := &Server{}
	base := deltaPage(strings.Repeat("a", 4000), "old")
	s.transcriptDeltaAnswer(base, "", "one", 200)
	next := deltaPage(strings.Repeat("a", 4000), "new")
	if _, ok := s.transcriptDeltaAnswer(next, base.Version, "one", 200).(contract.TranscriptDelta); !ok {
		t.Fatal("small edit sent a full page")
	}
	for _, scope := range []string{"another machine", "another session", "another execution"} {
		if _, ok := s.transcriptDeltaAnswer(next, base.Version, scope, 200).(contract.TranscriptPage); !ok {
			t.Fatal("foreign baseline reused")
		}
	}
	if _, ok := s.transcriptDeltaAnswer(next, base.Version, "one", 40).(contract.TranscriptPage); !ok {
		t.Fatal("different limit reused baseline")
	}
	for i := 0; i < transcriptDeltaPagesLimit; i++ {
		s.transcriptDeltaAnswer(deltaPage(fmt.Sprint(i)), "", "pressure", 200)
	}
	if s.transcriptBaselines.reading(true).Used > transcriptDeltaPagesLimit {
		t.Fatal("page bound exceeded")
	}
	if _, ok := s.transcriptDeltaAnswer(next, base.Version, "one", 200).(contract.TranscriptPage); !ok {
		t.Fatal("evicted baseline did not fall back")
	}
	var cache transcriptBaselines
	cache.put("first", make([]byte, transcriptDeltaBytesLimit-1))
	cache.put("second", []byte("next"))
	if cache.get("first") != nil || cache.reading(false).Used != 4 {
		t.Fatal("byte pressure retained the old baseline")
	}
	cache.put("oversized", make([]byte, transcriptDeltaBytesLimit+1))
	if cache.get("oversized") != nil {
		t.Fatal("oversized baseline retained")
	}
	tiny := deltaPage("x")
	s.transcriptDeltaAnswer(tiny, "", "small", 200)
	if _, ok := s.transcriptDeltaAnswer(deltaPage("y"), tiny.Version, "small", 200).(contract.TranscriptPage); !ok {
		t.Fatal("a larger patch should send a full page")
	}
}
func TestVersionedDeltaRouteMatchesARewrittenNativePage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	item := session.Session{ID: "%7", Assistant: session.AssistantCodex, PID: os.Getpid(), ConversationID: "delta-fixture"}
	path := filepath.Join(home, ".codex", "sessions", "rollout-"+item.ConversationID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	write := func(last string) {
		t.Helper()
		var raw []byte
		for _, text := range []string{strings.Repeat("kept ", 1000), last} {
			row, _ := json.Marshal(map[string]any{"type": "event_msg", "payload": map[string]any{"type": "item_completed", "item": map[string]any{"type": "AgentMessage", "content": []map[string]any{{"type": "text", "text": text}}}}})
			raw = append(append(raw, row...), '\n')
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	s := paneServer(t, &pane{s: item})
	s.executionMachineID = "fixture"
	read := func(view string) []byte {
		t.Helper()
		w := httptest.NewRecorder()
		s.transcriptRoute(w, httptest.NewRequest("GET", "/v1/transcript?session=%257&view="+view, nil))
		if w.Code != 200 {
			t.Fatalf("read: %d %s", w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	write("pending")
	var base contract.TranscriptPage
	if err := json.Unmarshal(read("tools-v2"), &base); err != nil {
		t.Fatal(err)
	}
	if len(base.Entries) != 2 {
		t.Fatalf("fixture was not read: %d", len(base.Entries))
	}
	var same map[string]any
	json.Unmarshal(read("delta-v1:"+base.Version), &same)
	if same["unchanged"] != true {
		t.Fatal("quiet page sent again")
	}
	write("complete")
	var patch contract.TranscriptDelta
	if err := json.Unmarshal(read("delta-v1:"+base.Version), &patch); err != nil || patch.BaseVersion == "" {
		t.Fatalf("delta absent: %v", err)
	}
	var whole contract.TranscriptPage
	json.Unmarshal(read("tools-v2"), &whole)
	if got := applyTestDelta(t, base, patch); !reflect.DeepEqual(got, whole) {
		t.Fatal("rewrite differs from full route")
	}
	// Lost response: retrying from the same base is deterministic and cannot duplicate rows.
	var retry contract.TranscriptDelta
	json.Unmarshal(read("delta-v1:"+base.Version), &retry)
	if !reflect.DeepEqual(patch, retry) {
		t.Fatal("retried update changed")
	}
	s.transcriptBaselines = transcriptBaselines{}
	var full contract.TranscriptPage
	json.Unmarshal(read("delta-v1:"+base.Version), &full)
	if !reflect.DeepEqual(full, whole) {
		t.Fatal("restart did not recover with a full page")
	}
}

func TestRollingAndExpandedProviderRowsStayIncremental(t *testing.T) {
	values := make([]string, 201)
	for i := range values {
		values[i] = fmt.Sprintf("row-%d", i)
	}
	base := deltaPage(values[:200]...)
	page := deltaPage(values[1:]...)
	patch := transcriptDelta(base, page)
	added, removed := 0, 0
	for _, edit := range patch.Delta {
		added += len(edit.Entries)
		removed += int(edit.DeleteCount)
	}
	if added != 1 || removed != 1 {
		t.Fatal("rolling window retransmitted retained rows")
	}
	// One provider record can expand past the requested 200 entries.
	expanded := deltaPage(values...)
	if got := applyTestDelta(t, base, transcriptDelta(base, expanded)); len(got.Entries) != 201 {
		t.Fatal("expanded provider row was lost")
	}
	s := &Server{}
	large := deltaPage(make([]string, transcriptDeltaRowsLimit+1)...)
	if _, ok := s.transcriptDeltaAnswer(large, "", "scope", 200).(contract.TranscriptPage); !ok || s.transcriptBaselines.reading(true).Used != 0 {
		t.Fatal("oversized comparison was retained")
	}
}

func TestTranscriptBaselinesRetainManySmallVersionsWithinTheByteBudget(t *testing.T) {
	var c transcriptBaselines
	for i := 0; i < 128; i++ {
		c.put(fmt.Sprint(i), []byte("small page"))
	}
	if c.get("0") == nil {
		t.Fatal("small baseline evicted with ample byte budget")
	}
	c.put("new", []byte("small page"))
	if c.get("1") != nil || c.get("0") == nil {
		t.Fatal("the least recently used version was not evicted")
	}
}
