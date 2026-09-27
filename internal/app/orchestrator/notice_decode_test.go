package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// The notice typed into a root's tab is read back by the transcript decoder,
// whose key set is closed. When `leftovers` was added here and not there, every
// notice with a leftover fell back to the person's own words, and the
// conversation showed the raw envelope instead of a card. Whatever this
// package writes, that one must read.
func TestEveryFinishedNoticeIsReadBackAsANotice(t *testing.T) {
	b := &Broker{Tasks: taskdir.New(t.TempDir())}
	base := Record{ID: "a7000000-0000-4000-8000-000000000001", Title: "half of it", State: StateSuccess,
		Notice: &Notice{ID: "a7000000-0000-4000-8000-00000000000a"}}
	cases := map[string]*taskdir.Result{
		"nothing left over": {Status: "success"},
		"two left over": {Status: "success", Leftovers: []work.Leftover{
			{Title: "the four daemon defects"}, {Title: "the half-done feature"},
		}},
	}
	for name, result := range cases {
		r := base
		r.Result = result
		wire, err := b.NoticeWire(context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		row, err := json.Marshal(map[string]any{"type": "user", "timestamp": "2026-09-27T10:00:00Z",
			"message": map[string]any{"role": "user", "content": wire}})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "record.jsonl")
		if err := os.WriteFile(path, append(row, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		page, _ := transcript.ReadClaude(path, 10)
		if len(page.Entries) != 1 || page.Entries[0].Kind != transcript.KindNotice || page.Entries[0].Notice == nil {
			t.Fatalf("%s: the notice was read back as %+v", name, page.Entries)
		}
		if got, want := page.Entries[0].Notice.Leftovers, int64(len(result.Leftovers)); got != want {
			t.Errorf("%s: read back %d leftovers, wrote %d", name, got, want)
		}
	}
}
