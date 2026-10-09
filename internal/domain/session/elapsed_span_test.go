package session

import "testing"

func TestElapsedSpanIsTheClockElapsedReads(t *testing.T) {
	for _, c := range []struct {
		line, clock, without string
		seconds              int
	}{
		{"Thinking… (1m 12s · ↑ 3k tokens)", "1m 12s", "Thinking… ( · ↑ 3k tokens)", 72},
		{"Working (11m 38s • esc to interrupt)", "11m 38s", "Working ( • esc to interrupt)", 698},
		{"Hullaballooing… (40s · ↓ 3.1k tokens · thinking)", "40s", "Hullaballooing… ( · ↓ 3.1k tokens · thinking)", 40},
		{"Baking… (1h 2m 3s)", "1h 2m 3s", "Baking… ()", 3723},
	} {
		start, end, seconds, ok := ElapsedSpan(c.line)
		if !ok || c.line[start:end] != c.clock || seconds != c.seconds {
			t.Errorf("%q: span %q, %d s, %v", c.line, c.line[start:end], seconds, ok)
		}
		if got, _ := Elapsed(c.line); got != c.seconds {
			t.Errorf("%q: Elapsed %d", c.line, got)
		}
		if got := WithoutElapsed(c.line); got != c.without {
			t.Errorf("%q: without the clock %q", c.line, got)
		}
	}
	for _, line := range []string{"Reading (3 stages)", "Thinking…", ""} {
		if _, _, _, ok := ElapsedSpan(line); ok {
			t.Errorf("%q has no clock", line)
		}
		if WithoutElapsed(line) != line {
			t.Errorf("%q changed without a clock", line)
		}
	}
	// Two readings of one turn are the same line without their clocks.
	if WithoutElapsed("Thinking… (1m 12s)") != WithoutElapsed("Thinking… (2m 3s)") {
		t.Error("one turn's lines differ without their clocks")
	}
}
