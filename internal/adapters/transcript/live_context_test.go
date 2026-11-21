package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLiveContextReadsTheNewestTurnFromAShortTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.jsonl")
	// A long early history must not matter: only the newest turn says how full the window is.
	filler := strings.Repeat(`{"type":"user","message":{"role":"user","content":"`+strings.Repeat("x", 4000)+`"}}`+"\n", 400)
	newest := `{"type":"assistant","message":{"model":"claude-opus-4-1","role":"assistant","usage":{"input_tokens":1000,"cache_read_input_tokens":99000,"cache_creation_input_tokens":0,"output_tokens":10}}}` + "\n"
	if err := os.WriteFile(path, []byte(filler+newest), 0o600); err != nil {
		t.Fatal(err)
	}
	status := ClaudeStatusLine{Found: true, WindowTokens: 200000, HasWindowTokens: true}
	got, ok := LiveContext(path, "claude", status)
	if !ok {
		t.Fatal("a readable record with a stated window answered unknown")
	}
	if got.UsedPercent < 49 || got.UsedPercent > 51 {
		t.Fatalf("used %.1f%%, want about 50%% (100k of 200k)", got.UsedPercent)
	}
}

func TestLiveContextIsUnknownWithoutARecord(t *testing.T) {
	if _, ok := LiveContext(filepath.Join(t.TempDir(), "missing.jsonl"), "claude", ClaudeStatusLine{}); ok {
		t.Fatal("a missing record answered a context reading")
	}
}
