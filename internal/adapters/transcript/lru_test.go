package transcript

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// The cache lets go of the entry used longest ago, not the one written first,
// and counts every entry it lets go of.
func TestTheLRULetsGoOfTheLeastRecentlyUsed(t *testing.T) {
	c := newLRU[int](capacity.CacheTranscriptUsage)
	c.setLimit(2)
	c.put("a", 1)
	c.put("b", 2)
	if _, ok := c.get("a"); !ok { // a is now the most recent
		t.Fatal("a is missing")
	}
	c.put("c", 3) // b goes
	if _, ok := c.get("b"); ok {
		t.Fatal("b was kept, though it was used longest ago")
	}
	for _, k := range []string{"a", "c"} {
		if _, ok := c.get(k); !ok {
			t.Fatalf("%s was let go", k)
		}
	}
	c.put("c", 4) // a replacement is not an insertion
	r := c.reading()
	if r.Used != 2 || r.Counters.Evicted != 1 || r.Counters.LastActionAt.IsZero() {
		t.Fatalf("reading: %+v", r)
	}
}

// limits N18: the ledger remembered every transcript it was ever asked about.
// Through the real read path, a ledger at its limit holds its limit, lets go
// of one per new transcript, and a transcript it let go of is counted again
// from the start and comes out the same.
func TestTheLedgerHoldsItsLimit(t *testing.T) {
	dir := t.TempDir()
	l := NewLedger()
	l.SetLimit(3)
	line := `{"type":"assistant","message":{"model":"m","usage":{"input_tokens":5,"output_tokens":7}}}` + "\n"
	paths := make([]string, 5)
	for i := range paths {
		paths[i] = filepath.Join(dir, fmt.Sprintf("s%d.jsonl", i))
		if err := os.WriteFile(paths[i], []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	first, err := l.Claude(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths[1:] {
		if _, err := l.Claude(p); err != nil {
			t.Fatal(err)
		}
	}
	r := l.Reading()
	if r.Used != 3 || r.Counters.Evicted != 2 {
		t.Fatalf("reading after five transcripts at a limit of three: %+v", r)
	}
	again, err := l.Claude(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if again.Total() != first.Total() || again.Total() == 0 {
		t.Fatalf("a transcript counted again reads %d, first %d", again.Total(), first.Total())
	}
}
