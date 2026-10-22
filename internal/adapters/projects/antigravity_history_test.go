package projects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
)

func syntheticAntigravityHistory(t *testing.T, home, id, text, stamp string) string {
	t.Helper()
	path, ok := transcript.AntigravityPath(home, id)
	if !ok {
		t.Fatal("invalid synthetic path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"step_index": 0, "source": "USER_EXPLICIT", "type": "USER_INPUT", "status": "DONE", "created_at": stamp, "content": "<USER_REQUEST>" + text + "</USER_REQUEST><ADDITIONAL_METADATA>synthetic clock only</ADDITIONAL_METADATA>"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAntigravityHistoryRequiresExactWorkspaceEvidence(t *testing.T) {
	home := t.TempDir()
	place := Place{Path: filepath.Join(home, "project")}
	a, b := "71000000-0000-4000-8000-000000000001", "71000000-0000-4000-8000-000000000002"
	first := syntheticAntigravityHistory(t, home, a, "A belongs here", "2026-10-01T12:00:00Z")
	syntheticAntigravityHistory(t, home, b, "B belongs elsewhere", "2026-10-02T12:00:00Z")
	if _, err := AntigravityPast(context.Background(), home, place, nil, PastTitles{}, nil, 20); err == nil {
		t.Fatal("missing evidence became empty known history")
	}
	bindings := map[string][]string{a: {place.Path}, b: {place.Path + "-other"}}
	rows, err := AntigravityPast(context.Background(), home, place, map[string]bool{first: true}, PastTitles{}, bindings, 20)
	if err != nil || len(rows) != 1 || rows[0].ID != a || rows[0].Title != "A belongs here" || !rows[0].Live || rows[0].At.Format(time.RFC3339) != "2026-10-01T12:00:00Z" {
		t.Fatalf("history=%+v %v", rows, err)
	}
	// A touched older file must not acquire the other conversation's time.
	now := time.Now()
	if err := os.Chtimes(first, now, now); err != nil {
		t.Fatal(err)
	}
	bindings[b] = []string{place.Path}
	rows, err = AntigravityPast(context.Background(), home, place, nil, PastTitles{}, bindings, 20)
	if err != nil || len(rows) != 2 || rows[0].ID != b {
		t.Fatalf("history order=%+v %v", rows, err)
	}
}

func TestAntigravityHistoryDoesNotTraverseArtifactsOrIncludeChildren(t *testing.T) {
	home := t.TempDir()
	place := Place{Path: filepath.Join(home, "project")}
	id := "72000000-0000-4000-8000-000000000001"
	path := syntheticAntigravityHistory(t, home, id, BriefingOpening+" synthetic task", "2026-10-01T12:00:00Z")
	artifact := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(path))), "artifacts", "secret.jsonl")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("malformed unrelated artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err := AntigravityPast(context.Background(), home, place, nil, PastTitles{}, map[string][]string{id: {place.Path}}, 20)
	if err != nil || len(rows) != 0 {
		t.Fatalf("child/artifact shown: %+v %v", rows, err)
	}
	if _, err := os.Stat(artifact); err != nil {
		t.Fatal("provider artifact changed")
	}
}

func TestAntigravityHistoryBoundsAndCancellationAreTyped(t *testing.T) {
	home := t.TempDir()
	place := Place{Path: filepath.Join(home, "project")}
	id := "73000000-0000-4000-8000-000000000001"
	path := syntheticAntigravityHistory(t, home, id, "one", "2026-10-01T12:00:00Z")
	for _, test := range []struct {
		bounds AntigravityHistoryBounds
		code   string
	}{
		{AntigravityHistoryBounds{Bytes: 1}, "byte_budget_exceeded"},
		{AntigravityHistoryBounds{Duration: time.Nanosecond}, "time_budget_exceeded"},
	} {
		_, err := AntigravityPast(context.Background(), home, place, nil, PastTitles{}, map[string][]string{id: {place.Path}}, 20, test.bounds)
		var typed *AntigravityHistoryError
		if !errors.As(err, &typed) || typed.Code != test.code {
			t.Fatalf("bound %s became success: %v", test.code, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := AntigravityPast(ctx, home, place, nil, PastTitles{}, map[string][]string{id: {place.Path}}, 20); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation swallowed: %v", err)
	}
	brain := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(path))))
	for i := 0; i < AntigravityHistoryEntryLimit; i++ {
		if err := os.Mkdir(filepath.Join(brain, fmt.Sprintf("unrelated-%04d", i)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	_, err := AntigravityPast(context.Background(), home, place, nil, PastTitles{}, map[string][]string{}, 20)
	var typed *AntigravityHistoryError
	if !errors.As(err, &typed) || typed.Code != "entry_budget_exceeded" {
		t.Fatalf("scan was partial success: %v", err)
	}
}

func TestAntigravityHistoryOpeningIsOneBoundedUnicodeLine(t *testing.T) {
	text := strings.Repeat("字", AntigravityHistoryTitleLimit+1) + "\nprivate extra line"
	got := antigravityHistoryOpening(text)
	if got != strings.Repeat("字", AntigravityHistoryTitleLimit)+"…" {
		t.Fatalf("unbounded title: %q", got)
	}
	if got := antigravityHistoryOpening("hello\nextra"); got != "hello" {
		t.Fatalf("multiline title: %q", got)
	}
}
