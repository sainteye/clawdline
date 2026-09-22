package transcript

import (
	"testing"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// The cache lets go of the entry used longest ago, not the one written first,
// and counts every entry it lets go of.
func TestTheLRULetsGoOfTheLeastRecentlyUsed(t *testing.T) {
	c := newLRU[int](capacity.CacheTranscriptTitles)
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
